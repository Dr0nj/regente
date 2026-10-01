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
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestI09RealAgentJournalLostReceiptAndUncertainRestart(t *testing.T) {
	realJournalRuntimeTest(t, false)
}
func TestI10RealAgentRuntimeLostReceiptAndUncertainRestart(t *testing.T) {
	realJournalRuntimeTest(t, true)
}
func realJournalRuntimeTest(t *testing.T, runtime bool) {
	agentBin := filepath.Join(t.TempDir(), "agent.exe")
	cmd := exec.Command("go", "build", "-o", agentBin, ".")
	cmd.Dir = "../../../agent"
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build agent: %v %s", err, out)
	}
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d := machineTestDB(t, dialect)
			store := storage.NewFileStore(t.TempDir(), false)
			var effects atomic.Int32
			gate := make(chan struct{})
			var wait atomic.Bool
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				effects.Add(1)
				if wait.Load() {
					<-gate
				}
				fmt.Fprint(w, "effect completed")
			}))
			defer target.Close()
			defer close(gate)
			def := domain.JobDefinition{ID: "job", Team: "test", JobType: "HTTP", Confirm: true, Params: map[string]interface{}{"url": target.URL, "method": "POST"}, Schedule: domain.Schedule{Enabled: true}}
			if err := store.Save(def); err != nil {
				t.Fatal(err)
			}
			h := hub.New()
			s := scheduler.New(store, d, h, time.Hour)
			s.SetClock(businessclock.Func(func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) }))
			if runtime {
				s.RuntimePolicy = runtimeprofile.Config{Profile: "development", ExecutionMode: "durable"}
				s.AttachDurable(execution.New(d, s.Now))
			}
			s.ReloadDefs()
			s.RunDaily("2026-09-30")
			defer s.Stop()
			srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: h, Scheduler: s, Store: store, Token: "test-token", ExecutionLab: !runtime, RuntimePolicy: s.RuntimePolicy}))
			defer srv.Close()
			var cred issuedMachine
			json.Unmarshal(machineRequest(t, srv, "POST", "/api/agents/tokens", "test-token", map[string]any{"agentId": "worker", "environment": "", "capabilities": []string{"HTTP", "EXECUTION_V2"}, "expiresAt": time.Now().Add(time.Hour)}, 200), &cred)
			var order execution.Order
			if runtime {
				if _, err := d.Exec("UPDATE instances SET confirmed=1 WHERE id='job-2026-09-30'"); err != nil {
					t.Fatal(err)
				}
				var err error
				order, err = s.DurableEngine().CreateRuntimeOrder("job-2026-09-30")
				if err != nil {
					t.Fatal(err)
				}
			} else {
				json.Unmarshal(machineRequest(t, srv, "POST", "/api/lab/orders", "test-token", map[string]string{"sourceInstanceId": "job-2026-09-30", "idempotencyKey": "copy"}, 200), &order)
			}
			var attempt execution.Attempt
			start := func(key, intent string) {
				t.Helper()
				if runtime {
 if err:=s.ReportAgentCapacity("worker",4,128,true);err!=nil{t.Fatal(err)}
					if intent != "start" {
						if _, err := s.DurableAction("operator", "job-2026-09-30", "rerun"); err != nil {
							t.Fatal(err)
						}
					}
					var err error
					attempt, err = s.DurableEngine().Start(order.ID, "worker", key, intent)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					json.Unmarshal(machineRequest(t, srv, "POST", "/api/lab/orders/"+order.ID+"/attempts", "test-token", map[string]string{"agentId": "worker", "idempotencyKey": key, "intent": intent}, 200), &attempt)
				}
			}
			start("first", "start")
			upstream, _ := url.Parse(srv.URL)
			proxy := httputil.NewSingleHostReverseProxy(upstream)
			var lose atomic.Bool
			lose.Store(true)
			lost := make(chan struct{}, 1)
			proxy.ModifyResponse = func(res *http.Response) error {
				if res.Request.URL.Path == "/api/agent/v2/result" && lose.Load() {
					select {
					case lost <- struct{}{}:
					default:
					}
					return fmt.Errorf("synthetic lost result receipt")
				}
				return nil
			}
			proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
				http.Error(w, "receipt unavailable", http.StatusServiceUnavailable)
			}
			edge := httptest.NewServer(proxy)
			defer edge.Close()
			journal := filepath.Join(t.TempDir(), "agent.db")
			run := func() *exec.Cmd {
				t.Helper()
				p := exec.Command(agentBin, "-transport", "v2", "-server", edge.URL, "-token", cred.Token, "-id", "worker", "-caps", "HTTP", "-journal", journal, "-concurrency", "1")
				p.Stdout = os.Stderr
				p.Stderr = os.Stderr
				if err := p.Start(); err != nil {
					t.Fatal(err)
				}
				return p
			}
			stop := func(p *exec.Cmd) { t.Helper(); _ = p.Process.Kill(); _ = p.Wait() }
			p := run()
			select {
			case <-lost:
			case <-time.After(15 * time.Second):
				stop(p)
				t.Fatal("result receipt boundary not reached")
			}
			stop(p)
			if effects.Load() != 1 {
				t.Fatal(effects.Load())
			}
			lose.Store(false)
			p = run()
			deadline := time.Now().Add(12 * time.Second)
			acknowledged := false
			for time.Now().Before(deadline) {
				journalDB, err := db.Open(db.SQLite, journal)
				if err != nil {
					t.Fatal(err)
				}
				var state string
				err = journalDB.QueryRow("SELECT state FROM journal_entries WHERE execution_id=?", attempt.ExecutionID).Scan(&state)
				journalDB.Close()
				if err == nil && state == "acked" {
					acknowledged = true
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			stop(p)
			if !acknowledged || effects.Load() != 1 {
				t.Fatal("result replay repeated effect or not confirmed", acknowledged, effects.Load())
			}
			var events int
			d.QueryRow("SELECT COUNT(*) FROM execution_events WHERE execution_id=? AND kind='result_recorded'", attempt.ExecutionID).Scan(&events)
			if events != 1 {
				t.Fatal(events)
			}
			wait.Store(true)
			start("second", "rerun")
			p = run()
			deadline = time.Now().Add(12 * time.Second)
			for effects.Load() < 2 && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			if effects.Load() != 2 {
				stop(p)
				t.Fatal("second effect did not start")
			}
			stop(p)
			p = run()
			defer stop(p)
			deadline = time.Now().Add(12 * time.Second)
			uncertain := false
			for time.Now().Before(deadline) {
				var state string
				d.QueryRow("SELECT state FROM execution_attempts WHERE execution_id=?", attempt.ExecutionID).Scan(&state)
				if state == "uncertain" {
					uncertain = true
					break
				}
				time.Sleep(20 * time.Millisecond)
			}
			if runtime {
				var status string
				d.QueryRow("SELECT status FROM instances WHERE id='job-2026-09-30'").Scan(&status)
				if status != "UNCERTAIN" {
					t.Fatal(status)
				}
			}
			if !uncertain || effects.Load() != 2 {
				t.Fatal("unknown effect replayed or not visible", uncertain, effects.Load())
			}
		})
	}
}
