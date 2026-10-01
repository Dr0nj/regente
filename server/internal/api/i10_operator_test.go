package api

import (
	"encoding/json"
	"fmt"
	"github.com/Dr0nj/regente-server/internal/businessclock"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/execution"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/runtimeprofile"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestI10OperatorAPIAndCLIContracts(t *testing.T) {
	cli := filepath.Join(t.TempDir(), "regente.exe")
	build := exec.Command("go", "build", "-o", cli, "./cmd/regente")
	build.Dir = "../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("CLI build: %v %s", err, out)
	}
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d := machineTestDB(t, dialect)
			h := hub.New()
			store := storage.NewFileStore(t.TempDir(), false)
			def := domain.JobDefinition{ID: "job", Team: "test", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}, Params: map[string]interface{}{"command": "effect"}}
			if err := store.Save(def); err != nil {
				t.Fatal(err)
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
			if err := s.ReportAgentCapacity("worker", 4, 128, true); err != nil {
				t.Fatal(err)
			}
			order, err := e.CreateRuntimeOrder("job-2026-09-30")
			if err != nil {
				t.Fatal(err)
			}
			a, err := e.Start(order.ID, "worker", "first", "start")
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
			for seq := int64(1); seq <= 130; seq++ {
				if _, err = e.Append(execution.Output{Identity: identity, Seq: seq, Chunk: "x"}); err != nil {
					t.Fatal(err)
				}
			}
			reader := newOperatorToken(t, d, "read", map[string]string{"test": "r"})
			writer := newOperatorToken(t, d, "write", map[string]string{"test": "rw"})
			other := newOperatorToken(t, d, "other", map[string]string{"other": "rw"})
			path := "/api/instances/job-2026-09-30"
			var output struct{ Text string }
			json.Unmarshal(machineRequest(t, srv, "GET", path+"/output", reader, nil, 200), &output)
			if output.Text != strings.Repeat("x", 130) {
				t.Fatal("live sysout truncated")
			}
			machineRequest(t, srv, "GET", path+"/executions", other, nil, 404)
			machineRequest(t, srv, "GET", path+"/output", other, nil, 404)
			if _, err = e.AcknowledgeReason(identity, "uncertain", "synthetic remote receipt lost"); err != nil {
				t.Fatal(err)
			}
			resolve := map[string]any{"fence": a.Fence, "idempotencyKey": "resolve", "decision": "succeeded", "reason": "effect verified stopped and complete", "effectStopped": true}
			route := "/api/executions/" + a.ExecutionID + "/resolve"
			machineRequest(t, srv, "POST", route, reader, resolve, 403)
			machineRequest(t, srv, "POST", route, other, resolve, 403)
			resolve["fence"] = a.Fence + 1
			machineRequest(t, srv, "POST", route, writer, resolve, 409)
			resolve["fence"] = a.Fence
			run := func(args ...string) []byte {
				t.Helper()
				args = append(args, "-server", srv.URL, "-token", writer)
				out, err := exec.Command(cli, args...).CombinedOutput()
				if err != nil {
					t.Fatalf("CLI: %v %s", err, out)
				}
				return out
			}
			if out := run("ops", "executions", "job-2026-09-30"); !strings.Contains(string(out), a.ExecutionID) || !strings.Contains(string(out), "synthetic remote receipt lost") {
				t.Fatal("CLI missing identity or reason", string(out))
			}
			args := []string{"ops", "resolve-execution", a.ExecutionID, "-fence", fmt.Sprint(a.Fence), "-key", "resolve", "-decision", "succeeded", "-reason", "effect verified stopped and complete", "-effect-stopped"}
			run(args...)
			run(args...)
			var count int
			d.QueryRow("SELECT COUNT(*) FROM execution_decisions").Scan(&count)
			if count != 1 {
				t.Fatal("duplicate CLI resolution", count)
			}
			effect := a.ExecutionID + "-hooks"
			body := map[string]any{"generation": 0, "idempotencyKey": "effect", "decision": "cancelled", "reason": "no global assignment required", "effectStopped": true}
			machineRequest(t, srv, "POST", "/api/execution-effects/"+effect+"/resolve", reader, body, 403)
			run("ops", "resolve-effect", effect, "-generation", "0", "-key", "effect", "-decision", "cancelled", "-reason", "no global assignment required", "-effect-stopped")
			machineRequest(t, srv, "GET", path+"/executions", reader, nil, 200)
			if _, err = s.DurableAction("operator", "job-2026-09-30", "rerun"); err != nil {
				t.Fatal(err)
			}
			second, err := e.Start(order.ID, "worker", "second", "rerun")
			if err != nil {
				t.Fatal(err)
			}
			if second.Attempt != 2 || second.Fence <= a.Fence {
				t.Fatal(second)
			}
			var history struct {
				Attempt  int
				Complete bool
			}
			json.Unmarshal(machineRequest(t, srv, "GET", path+"/output?attempt=1", reader, nil, 200), &history)
			if history.Attempt != 1 || !history.Complete {
				t.Fatal("old sysout not preserved")
			}
			machineRequest(t, srv, "POST", route, writer, resolve, 200)
		})
	}
}
