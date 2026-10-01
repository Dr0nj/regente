package api

import (
	"encoding/json"
	"errors"
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
	"strings"
	"sync"
	"testing"
	"time"
)

func TestI08AttemptIntegration(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d := machineTestDB(t, dialect)
			store := storage.NewFileStore(t.TempDir(), false)
			if err := store.Save(domain.JobDefinition{ID: "job", Team: "test", JobType: "COMMAND", Confirm: true, Params: map[string]interface{}{"command": "original"}, ConditionsOutAdd: []string{"must-not-publish"}, Schedule: domain.Schedule{Enabled: true}}); err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
			clock := businessclock.Func(func() time.Time { return now })
			servers := []*httptest.Server{}
			for i := 0; i < 2; i++ {
				h := hub.New()
				s := scheduler.New(store, d, h, time.Hour)
				s.SetClock(clock)
				s.ReloadDefs()
				t.Cleanup(s.Stop)
				if i == 0 {
					s.RunDaily("2026-09-30")
				}
				srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: h, Scheduler: s, Store: store, Token: "test-token", ExecutionLab: true}))
				t.Cleanup(srv.Close)
				servers = append(servers, srv)
			}
			issue := func(id string, caps []string) issuedMachine {
				var cred issuedMachine
				raw := machineRequest(t, servers[0], "POST", "/api/agents/tokens", "test-token", map[string]any{"agentId": id, "environment": "", "capabilities": caps, "expiresAt": time.Now().Add(time.Hour)}, 200)
				if json.Unmarshal(raw, &cred) != nil || cred.Token == "" {
					t.Fatal("credencial inválida")
				}
				return cred
			}
			a := issue("worker-a", []string{"COMMAND", "EXECUTION_V2"})
			b := issue("worker-b", []string{"COMMAND", "EXECUTION_V2"})
			v1 := issue("worker-v1", []string{"COMMAND"})
			poll := "/api/agent/v2/poll?id=worker-a&env=&caps=COMMAND,EXECUTION_V2&protocol=2"
			machineRequest(t, servers[0], "GET", poll, "test-token", nil, 401)
			machineRequest(t, servers[0], "GET", "/api/agent/v2/poll?id=worker-v1&env=&caps=COMMAND&protocol=2", v1.Token, nil, 403)
			machineRequest(t, servers[0], "GET", strings.Replace(poll, "protocol=2", "protocol=1", 1), a.Token, nil, 426)
			var o execution.Order
			raw := machineRequest(t, servers[0], "POST", "/api/lab/orders", "test-token", map[string]string{"sourceInstanceId": "job-2026-09-30", "idempotencyKey": "copy"}, 200)
			json.Unmarshal(raw, &o)
			raw = machineRequest(t, servers[1], "POST", "/api/lab/orders", "test-token", map[string]string{"sourceInstanceId": "job-2026-09-30", "idempotencyKey": "copy"}, 200)
			var same execution.Order
			json.Unmarshal(raw, &same)
			if same.ID != o.ID {
				t.Fatal("ordem duplicada")
			}
			trigger := `CREATE TRIGGER intent_fault BEFORE INSERT ON execution_outbox BEGIN SELECT RAISE(ABORT,'synthetic outbox failure'); END`
			if dialect == db.Postgres {
				if _, err := d.Exec(`CREATE FUNCTION intent_fault_fn() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic outbox failure'; END $$`); err != nil {
					t.Fatal(err)
				}
				trigger = `CREATE TRIGGER intent_fault BEFORE INSERT ON execution_outbox FOR EACH ROW EXECUTE FUNCTION intent_fault_fn()`
			}
			if _, err := d.Exec(trigger); err != nil {
				t.Fatal(err)
			}
			body := map[string]string{"agentId": "worker-a", "idempotencyKey": "first", "intent": "start"}
			path := "/api/lab/orders/" + o.ID + "/attempts"
			machineRequest(t, servers[0], "POST", path, "test-token", body, 503)
			var n int
			d.QueryRow("SELECT COUNT(*) FROM execution_attempts").Scan(&n)
			if n != 0 {
				t.Fatal("tentativa sem intenção", n)
			}
			drop := "DROP TRIGGER intent_fault"
			if dialect == db.Postgres {
				drop += " ON execution_outbox"
			}
			if _, err := d.Exec(drop); err != nil {
				t.Fatal(err)
			}
			var first execution.Attempt
			raw = machineRequest(t, servers[0], "POST", path, "test-token", body, 200)
			json.Unmarshal(raw, &first)
			raw = machineRequest(t, servers[1], "POST", path, "test-token", body, 200)
			var replay execution.Attempt
			json.Unmarshal(raw, &replay)
			if replay.ExecutionID != first.ExecutionID {
				t.Fatal("replay criou execução")
			}
			var msg execution.Envelope
			raw = machineRequest(t, servers[1], "GET", poll, a.Token, nil, 200)
			json.Unmarshal(raw, &msg)
			if msg.ExecutionID != first.ExecutionID || msg.Definition.Params["command"] != "original" {
				t.Fatal(msg)
			}
			machineRequest(t, servers[0], "GET", poll, a.Token, nil, 204)
			// Simula claim entregue com resposta perdida: outro nó retoma do DB, não do bus.
			d.Exec("UPDATE execution_outbox SET lease_until=0,next_at=0")
			raw = machineRequest(t, servers[0], "GET", poll, a.Token, nil, 200)
			var redelivery execution.Envelope
			json.Unmarshal(raw, &redelivery)
			if redelivery.MessageID != msg.MessageID || redelivery.Fence != msg.Fence {
				t.Fatal("identidade mudou no reenvio")
			}
			identity := map[string]any{"protocol": 2, "executionId": first.ExecutionID, "fence": first.Fence}
			ack := func(kind string, want int) {
				body := map[string]any{"protocol": 2, "executionId": first.ExecutionID, "fence": first.Fence, "kind": kind}
				machineRequest(t, servers[0], "POST", "/api/agent/v2/ack", a.Token, body, want)
			}
			machineRequest(t, servers[0], "POST", "/api/agent/v2/ack", b.Token, map[string]any{"protocol": 2, "executionId": first.ExecutionID, "fence": first.Fence, "kind": "accepted"}, 409)
			ack("accepted", 200)
			ack("started", 200)
			machineRequest(t, servers[0], "POST", "/api/agent/v2/result", a.Token, map[string]any{"protocol": 2, "executionId": first.ExecutionID, "fence": first.Fence, "output": "missing exit"}, 400)
			machineRequest(t, servers[0], "POST", "/api/agent/v2/result", a.Token, map[string]any{"protocol": 2, "executionId": first.ExecutionID, "fence": first.Fence, "exitCode": 0}, 400)
			output := map[string]any{"protocol": 2, "executionId": first.ExecutionID, "fence": first.Fence, "seq": 1, "chunk": "first chunk"}
			machineRequest(t, servers[0], "POST", "/api/agent/v2/output", a.Token, output, 200)
			raw = machineRequest(t, servers[1], "POST", "/api/agent/v2/output", a.Token, output, 200)
			if !strings.Contains(string(raw), `"duplicate":true`) {
				t.Fatal(string(raw))
			}
			wrong := map[string]any{"protocol": 2, "executionId": first.ExecutionID, "fence": first.Fence + 1, "seq": 2, "chunk": "wrong fence"}
			machineRequest(t, servers[0], "POST", "/api/agent/v2/output", a.Token, wrong, 409)
			results := map[string]any{"protocol": 2, "executionId": first.ExecutionID, "fence": first.Fence, "exitCode": 1, "output": "failed"}
			// Dois resultados idênticos concorrentes: um commit, dois receipts válidos.
			start := make(chan struct{})
			outcomes := make(chan string, 2)
			var wg sync.WaitGroup
			for _, srv := range servers {
				wg.Add(1)
				go func(srv *httptest.Server) {
					defer wg.Done()
					<-start
					payload, _ := json.Marshal(results)
					req, _ := http.NewRequest("POST", srv.URL+"/api/agent/v2/result", strings.NewReader(string(payload)))
					req.Header.Set("Authorization", "Bearer "+a.Token)
					resp, err := srv.Client().Do(req)
					if err != nil {
						outcomes <- err.Error()
						return
					}
					resp.Body.Close()
					outcomes <- fmt.Sprint(resp.StatusCode)
				}(srv)
			}
			close(start)
			wg.Wait()
			close(outcomes)
			for status := range outcomes {
				if status != "200" {
					t.Fatal("resultado concorrente", status)
				}
			}
			d.QueryRow("SELECT COUNT(*) FROM execution_events WHERE kind='result_recorded'").Scan(&n)
			if n != 1 {
				t.Fatal("resultado duplicado", n)
			}
			body["idempotencyKey"], body["intent"] = "retry", "retry"
			raw = machineRequest(t, servers[1], "POST", path, "test-token", body, 200)
			var second execution.Attempt
			json.Unmarshal(raw, &second)
			if second.Attempt != 2 || second.Fence <= first.Fence {
				t.Fatal(second)
			}
			machineRequest(t, servers[0], "POST", "/api/agent/v2/result", a.Token, results, 200)
			ack("accepted", 200)
			ack("started", 200)
			output["seq"], output["chunk"] = 2, "late chunk"
			machineRequest(t, servers[0], "POST", "/api/agent/v2/output", a.Token, output, 409)
			raw = machineRequest(t, servers[0], "GET", "/api/lab/executions/"+second.ExecutionID, "test-token", nil, 200)
			json.Unmarshal(raw, &replay)
			if replay.State != "dispatch_pending" {
				t.Fatal("tentativa antiga alterou atual", replay)
			}
			machineRequest(t, servers[0], "POST", "/api/lab/orders/"+o.ID+"/cancel", "test-token", nil, 200)
			identity["executionId"], identity["fence"], identity["exitCode"], identity["output"] = second.ExecutionID, second.Fence, 0, "cancelled late"
			machineRequest(t, servers[0], "POST", "/api/agent/v2/result", a.Token, identity, 409)
			raw = machineRequest(t, servers[0], "GET", "/api/lab/executions/"+first.ExecutionID+"/output", "test-token", nil, 200)
			if !strings.Contains(string(raw), "first chunk") {
				t.Fatal(string(raw))
			}
			i08ConcurrencyAndRecovery(t, d, now)
			machineRequest(t, servers[0], "DELETE", "/api/agents/tokens/"+fmt.Sprint(a.ID), "test-token", nil, 200)
			machineRequest(t, servers[0], "GET", poll, a.Token, nil, 401)
			var status string
			d.QueryRow("SELECT status FROM instances WHERE id='job-2026-09-30'").Scan(&status)
			d.QueryRow("SELECT COUNT(*) FROM conditions").Scan(&n)
			if status != "WAITING" || n != 0 {
				t.Fatal("efeito escapou laboratório", status, n)
			}
			raw = machineRequest(t, servers[0], "GET", "/metrics", "test-token", nil, 200)
			if !strings.Contains(string(raw), "regente_execution_outbox") {
				t.Fatal("métricas ausentes")
			}
			h := hub.New()
			disabled := httptest.NewServer(NewRouter(Config{DB: d, Hub: h, Store: store, Token: "test-token"}))
			defer disabled.Close()
			machineRequest(t, disabled, "GET", "/api/lab/orders/"+o.ID, "test-token", nil, 404)
			production := httptest.NewServer(NewRouter(Config{DB: d, Hub: h, Store: store, Token: "test-token", ExecutionLab: true, RuntimePolicy: runtimeprofile.Config{Profile: "production"}}))
			defer production.Close()
			machineRequest(t, production, "GET", "/api/agent/v2/poll", a.Token, nil, 401)
		})
	}
}

// Mesmo contrato concorrente nos dois drivers; cada motor representa um nó.
func i08ConcurrencyAndRecovery(t *testing.T, d *db.DB, now time.Time) {
	t.Helper()
	engines := []*execution.Engine{
		execution.New(d, func() time.Time { return now }),
		execution.New(d, func() time.Time { return now }),
	}
	orders := make([]execution.Order, 2)
	for i, e := range engines {
		e.MaxPending = 1
		var err error
		orders[i], err = e.CreateOrder("job-2026-09-30", fmt.Sprintf("admission-%d", i))
		if err != nil {
			t.Fatal(err)
		}
	}
	gate := make(chan struct{})
	outcomes := make(chan error, 2)
	var wg sync.WaitGroup
	for i, e := range engines {
		wg.Add(1)
		go func(i int, e *execution.Engine) {
			defer wg.Done()
			<-gate
			_, err := e.Start(orders[i].ID, "worker-a", "admit", "start")
			outcomes <- err
		}(i, e)
	}
	close(gate)
	wg.Wait()
	close(outcomes)
	success, capacity := 0, 0
	for err := range outcomes {
		if err == nil {
			success++
		} else if errors.Is(err, execution.ErrCapacity) {
			capacity++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || capacity != 1 {
		t.Fatal("limite global entre ordens", success, capacity)
	}
	var a execution.Attempt
	for _, o := range orders {
		current, err := engines[0].Order(o.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.CurrentExecution != "" {
			a, err = engines[0].Attempt(current.CurrentExecution)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	msg, err := engines[0].Claim("worker-a")
	if err != nil || msg == nil || msg.ExecutionID != a.ExecutionID {
		t.Fatal(msg, err)
	}
	id := execution.Identity{Protocol: 2, ExecutionID: a.ExecutionID, Fence: a.Fence, AgentID: "worker-a"}
	for _, kind := range []string{"accepted", "started"} {
		if _, err = engines[1].Acknowledge(id, kind); err != nil {
			t.Fatal(err)
		}
	}
	// Fechar a outbox não libera admissão enquanto o efeito permanece em curso.
	var blocked execution.Order
	for _, o := range orders {
		if o.ID != a.OrderID {
			blocked = o
		}
	}
	if _, err = engines[0].Start(blocked.ID, "worker-a", "still-blocked", "start"); !errors.Is(err, execution.ErrCapacity) {
		t.Fatal(err)
	}
	if _, err = engines[0].Cancel(a.OrderID); err != nil {
		t.Fatal(err)
	}
	gate = make(chan struct{})
	outcomes = make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-gate
		_, err := engines[0].Complete(execution.Result{Identity: id, Output: "race"})
		outcomes <- err
	}()
	go func() { defer wg.Done(); <-gate; _, err := engines[1].Acknowledge(id, "cancelled"); outcomes <- err }()
	close(gate)
	wg.Wait()
	close(outcomes)
	success, conflicts := 0, 0
	for err := range outcomes {
		if err == nil {
			success++
		} else if errors.Is(err, execution.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	state, err := engines[0].Attempt(a.ExecutionID)
	if err != nil {
		t.Fatal(err)
	}
	order, err := engines[0].Order(a.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if success != 1 || conflicts != 1 || order.State != state.State || state.LeaseUntil != 0 {
		t.Fatal("terminal não linearizável", success, conflicts, state, order)
	}
	// Lease expirada deixa resultado incerto: novo dispatch é bloqueado, prova tardia é aceita.
	next, err := engines[0].Start(blocked.ID, "worker-a", "after-terminal", "start")
	if err != nil {
		t.Fatal(err)
	}
	msg, err = engines[1].Claim("worker-a")
	if err != nil || msg == nil || msg.ExecutionID != next.ExecutionID {
		t.Fatal(msg, err)
	}
	id = execution.Identity{Protocol: 2, ExecutionID: next.ExecutionID, Fence: next.Fence, AgentID: "worker-a"}
	for _, kind := range []string{"accepted", "started"} {
		if _, err = engines[0].Acknowledge(id, kind); err != nil {
			t.Fatal(err)
		}
	}
	recovered := execution.New(d, func() time.Time { return now.Add(6 * time.Minute) })
	if n, err := recovered.Reconcile(); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err = recovered.Start(blocked.ID, "worker-a", "unsafe-rerun", "rerun"); !errors.Is(err, execution.ErrConflict) {
		t.Fatal(err)
	}
	if _, err = recovered.Complete(execution.Result{Identity: id, Output: "verified late"}); err != nil {
		t.Fatal(err)
	}
}
