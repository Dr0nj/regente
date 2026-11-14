package api

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/businessclock"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/execution"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/runtimeprofile"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
)

// O comando local resolve as UNCERTAIN que seguram slot — pelo mesmo motor da
// API: tentativa encerrada, instance projetada, decisão auditada e slot livre.
func TestLocalAdminResolvesUncertainExecutions(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d := machineTestDB(t, dialect)
			h := hub.New()
			store := storage.NewFileStore(t.TempDir(), false)
			for _, id := range []string{"job-a", "job-b"} {
				if err := store.Save(domain.JobDefinition{ID: id, Team: "test", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}, Params: map[string]interface{}{"command": "effect"}}); err != nil {
					t.Fatal(err)
				}
			}
			s := scheduler.New(store, d, h, time.Hour)
			defer s.Stop()
			s.SetClock(businessclock.Func(func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) }))
			s.RuntimePolicy = runtimeprofile.Config{Profile: "development", ExecutionMode: "durable"}
			e := execution.New(d, s.Now)
			s.AttachDurable(e)
			s.ReloadDefs()
			s.RunDaily("2026-09-30")
			srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: h, Scheduler: s, Store: store, Token: "test-token", RuntimePolicy: s.RuntimePolicy}))
			defer srv.Close()
			machineRequest(t, srv, "POST", "/api/agents/tokens", "test-token", map[string]any{"agentId": "worker", "environment": "", "capabilities": []string{"COMMAND", "EXECUTION_V2"}, "expiresAt": time.Now().Add(time.Hour)}, 200)
			if err := s.ReportAgentCapacity("worker", 2, 128, true); err != nil {
				t.Fatal(err)
			}
			executions := map[string]string{}
			for _, id := range []string{"job-a", "job-b"} {
				order, err := e.CreateRuntimeOrder(id + "-2026-09-30")
				if err != nil {
					t.Fatal(err)
				}
				a, err := e.Start(order.ID, "worker", "start-"+id, "start")
				if err != nil {
					t.Fatal(err)
				}
				e.Claim("worker")
				identity := execution.Identity{Protocol: 2, ExecutionID: a.ExecutionID, Fence: a.Fence, AgentID: a.AgentID}
				for _, phase := range []string{"accepted", "started"} {
					if _, err = e.Acknowledge(identity, phase); err != nil {
						t.Fatal(err)
					}
				}
				if _, err = e.AcknowledgeReason(identity, "uncertain", "synthetic remote receipt lost"); err != nil {
					t.Fatal(err)
				}
				executions[id] = a.ExecutionID
			}

			// Caminho de socket curto: o limite do AF_UNIX (~104 bytes) estoura no
			// t.TempDir() do Windows.
			dir, err := os.MkdirTemp("", "ra")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(dir) })
			sock := filepath.Join(dir, "a.sock")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err = ServeLocalAdmin(ctx, sock, LocalAdminHandler(d, s.DurableEngine)); err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" {
				if info, err := os.Stat(sock); err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("socket must be private to the service user", info, err)
				}
			}
			if err = ServeLocalAdmin(ctx, sock, LocalAdminHandler(d, s.DurableEngine)); err == nil {
				t.Fatal("a live socket must not be taken over")
			}

			if _, err = LocalAdminResolve(sock, LocalResolveRequest{Decision: "failed"}); err == nil || !strings.Contains(err.Error(), "reason is required") {
				t.Fatal("resolution without evidence must be refused", err)
			}
			if _, err = LocalAdminResolve(sock, LocalResolveRequest{Decision: "killed", Reason: "x"}); err == nil {
				t.Fatal("unknown decision must be refused")
			}
			dry, err := LocalAdminResolve(sock, LocalResolveRequest{DryRun: true})
			if err != nil || len(dry.Resolved) != 2 {
				t.Fatal("dry run must list both", dry, err)
			}
			var pending int
			d.QueryRow("SELECT COUNT(*) FROM execution_attempts WHERE state='uncertain'").Scan(&pending)
			if pending != 2 {
				t.Fatal("dry run recorded something", pending)
			}
			none, err := LocalAdminResolve(sock, LocalResolveRequest{Reason: "x", Agent: "other-agent"})
			if err != nil || len(none.Resolved)+len(none.Failed) != 0 {
				t.Fatal("agent filter ignored", none, err)
			}
			one, err := LocalAdminResolve(sock, LocalResolveRequest{Reason: "test endpoint, nothing left running", Executions: []string{executions["job-a"]}})
			if err != nil || len(one.Resolved) != 1 || len(one.Failed) != 0 || one.Resolved[0].InstanceID != "job-a-2026-09-30" {
				t.Fatal("single resolution", one, err)
			}
			rest, err := LocalAdminResolve(sock, LocalResolveRequest{Decision: "cancelled", Reason: "test endpoint, nothing left running"})
			if err != nil || len(rest.Resolved) != 1 || len(rest.Failed) != 0 {
				t.Fatal("bulk resolution", rest, err)
			}

			for id, want := range map[string]string{"job-a": "failed", "job-b": "cancelled"} {
				var state, by string
				if err = d.QueryRow("SELECT state,resolved_by FROM execution_attempts WHERE execution_id=?", executions[id]).Scan(&state, &by); err != nil || state != want || by != LocalAdminActor {
					t.Fatal("attempt not closed by the local admin", id, state, by, err)
				}
				var status string
				if err = d.QueryRow("SELECT status FROM instances WHERE id=?", id+"-2026-09-30").Scan(&status); err != nil || status == "UNCERTAIN" || status == "RUNNING" {
					t.Fatal("instance still holds the unresolved status", id, status, err)
				}
			}
			var decisions int
			d.QueryRow("SELECT COUNT(*) FROM execution_decisions WHERE actor=?", LocalAdminActor).Scan(&decisions)
			if decisions != 2 {
				t.Fatal("decisions not recorded", decisions)
			}
			for _, adm := range s.AgentAdmissions() {
				if adm.AgentID == "worker" && (adm.Uncertain != 0 || adm.InUse != 0) {
					t.Fatal("slots still held", adm)
				}
			}
			again, err := LocalAdminResolve(sock, LocalResolveRequest{Reason: "x"})
			if err != nil || len(again.Resolved)+len(again.Failed) != 0 {
				t.Fatal("nothing should remain", again, err)
			}
		})
	}
}
