package scheduler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
)

func newTestDB(t *testing.T) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(db.SQLite, path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func TestAlertEngine_SeedDefaults(t *testing.T) {
	eng := NewAlertEngine(newTestDB(t), nil)
	eng.SeedDefaults()
	eng.SeedDefaults() // idempotente

	rules, err := eng.ListRules()
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	if len(rules) != len(defaultRules) {
		t.Fatalf("expected %d rules, got %d", len(defaultRules), len(rules))
	}
}

func TestAlertEngine_FailureFires(t *testing.T) {
	eng := NewAlertEngine(newTestDB(t), nil)
	eng.SeedDefaults()

	eng.Evaluate(AlertContext{
		WorkflowID:   "job-x",
		WorkflowName: "Job X",
		Status:       string(domain.StatusNotOK),
		DurationMs:   1200,
	})

	events, err := eng.ListEvents(50)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 fired event, got %d", len(events))
	}
	ev := events[0]
	if ev.RuleID != "rule-failure" || ev.Severity != "critical" {
		t.Fatalf("unexpected event: %+v", ev)
	}
	if ev.Acknowledged {
		t.Fatal("new event should be unacknowledged")
	}

	// success não dispara a regra de failure.
	eng2 := NewAlertEngine(newTestDB(t), nil)
	eng2.SeedDefaults()
	eng2.Evaluate(AlertContext{WorkflowID: "y", Status: string(domain.StatusOK), DurationMs: 500})
	evs2, _ := eng2.ListEvents(50)
	if len(evs2) != 0 {
		t.Fatalf("OK status should not fire failure rule, got %d events", len(evs2))
	}
}

func TestAlertEngine_Cooldown(t *testing.T) {
	eng := NewAlertEngine(newTestDB(t), nil)
	eng.SeedDefaults()
	ctx := AlertContext{WorkflowID: "z", Status: string(domain.StatusNotOK)}
	eng.Evaluate(ctx)
	eng.Evaluate(ctx) // dentro do cooldown → não dispara de novo

	events, _ := eng.ListEvents(50)
	if len(events) != 1 {
		t.Fatalf("cooldown should suppress the 2nd fire, got %d events", len(events))
	}
}

func TestAlertEngine_Acknowledge(t *testing.T) {
	eng := NewAlertEngine(newTestDB(t), nil)
	eng.SeedDefaults()
	eng.Evaluate(AlertContext{WorkflowID: "a", Status: string(domain.StatusNotOK)})

	n, _ := eng.UnacknowledgedCount()
	if n != 1 {
		t.Fatalf("expected 1 unacknowledged, got %d", n)
	}

	events, _ := eng.ListEvents(50)
	if err := eng.Acknowledge(events[0].ID); err != nil {
		t.Fatalf("ack: %v", err)
	}
	n, _ = eng.UnacknowledgedCount()
	if n != 0 {
		t.Fatalf("expected 0 unacknowledged after ack, got %d", n)
	}
}

func TestAlertEngine_RoutesToWebhook(t *testing.T) {
	database := newTestDB(t)
	got := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got <- body
	}))
	defer srv.Close()

	// Configura o sink global de webhook genérico.
	if _, err := database.Exec(`INSERT INTO settings(key,value) VALUES(?,?)`, "alert_webhook_url", srv.URL); err != nil {
		t.Fatal(err)
	}

	eng := NewAlertEngine(database, nil)
	eng.SeedDefaults()
	eng.Evaluate(AlertContext{WorkflowID: "wf", WorkflowName: "WF", Status: string(domain.StatusNotOK), DurationMs: 1500})

	select {
	case body := <-got:
		if body["event"] != "alert.fired" {
			t.Fatalf("payload inesperado: %+v", body)
		}
		if body["severity"] != "critical" || body["workflowId"] != "wf" {
			t.Fatalf("campos do alerta errados: %+v", body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("webhook não recebeu o alerta dentro do timeout")
	}
}

func TestAlertEngine_NoWebhookConfigured(t *testing.T) {
	// Sem sink configurado, route() é no-op e não quebra o disparo.
	eng := NewAlertEngine(newTestDB(t), nil)
	eng.SeedDefaults()
	eng.Evaluate(AlertContext{WorkflowID: "x", Status: string(domain.StatusNotOK)})
	if evs, _ := eng.ListEvents(10); len(evs) != 1 {
		t.Fatalf("evento deveria ter sido persistido mesmo sem webhook, veio %d", len(evs))
	}
}

func TestAlertEngine_ToggleDisablesRule(t *testing.T) {
	eng := NewAlertEngine(newTestDB(t), nil)
	eng.SeedDefaults()
	if err := eng.ToggleRule("rule-failure"); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	eng.Evaluate(AlertContext{WorkflowID: "b", Status: string(domain.StatusNotOK)})
	events, _ := eng.ListEvents(50)
	if len(events) != 0 {
		t.Fatalf("disabled rule should not fire, got %d events", len(events))
	}
}
