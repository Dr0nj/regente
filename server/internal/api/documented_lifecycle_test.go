package api

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
)

// DOC-08: matriz pelo router real, inclusive agente offline e erro terminal.
// Complementa cancel_test.go (sinal, no-retry e resultado tardio).
func TestDocumentedCancelStates(t *testing.T) {
	for _, tc := range []struct {
		before, after string
		code          int
	}{
		{"RUNNING", "NOTOK", 200}, {"WAITING", "CANCELLED", 200},
		{"HELD", "CANCELLED", 200}, {"OK", "OK", 409},
		{"NOTOK", "NOTOK", 409}, {"CANCELLED", "CANCELLED", 409},
	} {
		t.Run(tc.before, func(t *testing.T) {
			srv, d := newOpsTestServer(t)
			seedInstanceFull(t, d, "job", tc.before, 0, "", 0)
			resp := doReq(t, srv.Client(), "POST", srv.URL+"/api/instances/job/cancel", "test-token", "")
			defer resp.Body.Close()
			if resp.StatusCode != tc.code {
				t.Fatalf("HTTP=%d, esperado %d", resp.StatusCode, tc.code)
			}
			if tc.code == 200 {
				var out map[string]string
				if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
					t.Fatal(err)
				}
				if out["id"] != "job" || out["status"] != tc.after {
					t.Fatalf("resposta: %v", out)
				}
			} else if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
				t.Fatal("erro não é text/plain")
			}
			var status string
			if err := d.QueryRow(`SELECT status FROM instances WHERE id='job'`).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if status != tc.after {
				t.Fatalf("persistido=%s, esperado %s", status, tc.after)
			}
			if tc.before == "RUNNING" {
				var exit int
				if err := d.QueryRow(`SELECT exit_code FROM instances WHERE id='job'`).Scan(&exit); err != nil || exit != -1 {
					t.Fatalf("exit=%d: %v", exit, err)
				}
			}
		})
	}
	// Preserva/documenta o comportamento atual; não inventa um 404 na spec.
	srv, _ := newOpsTestServer(t)
	resp := doReq(t, srv.Client(), "POST", srv.URL+"/api/instances/missing/cancel", "test-token", "")
	resp.Body.Close()
	if resp.StatusCode != 500 {
		t.Fatalf("lookup inexistente: %d", resp.StatusCode)
	}
}

// DOC-09: HTTP real + scheduler real, sem agentes/processos do usuário.
// O gate observado é o de condições; falta de agente é independente dele.
func TestDocumentedRerunPool(t *testing.T) {
	for _, scenario := range []string{"producer", "consumer_set_ok", "consumer_notok", "consumer_notok_input_removed", "consumer_without_out_remove"} {
		t.Run(scenario, func(t *testing.T) {
			d := newTestDB(t)
			t.Cleanup(func() { _ = d.Close() })
			h := hub.New()
			store := storage.NewFileStore(t.TempDir(), false)
			s := scheduler.New(store, d, h, time.Hour)
			t.Cleanup(s.Stop)
			srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: h, Store: store, Scheduler: s, Token: "test-token"}))
			t.Cleanup(srv.Close)
			date := time.Now().Format("2006-01-02")
			const cond = "A-TO-B"
			defs := []domain.JobDefinition{
				{ID: "A", JobType: "COMMAND", ConditionsOutAdd: []string{cond}},
				{ID: "B", JobType: "COMMAND", ConditionsIn: []string{cond}, ConditionsOutRemove: []string{cond}},
			}
			if scenario == "consumer_without_out_remove" {
				defs[1].ConditionsOutRemove = nil
			}
			snapshots := map[string]string{}
			for _, def := range defs {
				raw, err := json.Marshal(def)
				if err != nil {
					t.Fatal(err)
				}
				snapshots[def.ID] = string(raw)
				if _, err = d.Exec(`INSERT INTO instances(id,definition_id,order_date,status,scheduled_at,definition_snapshot) VALUES(?,?,?,?,?,?)`, def.ID, def.ID, date, "NOTOK", time.Now(), string(raw)); err != nil {
					t.Fatal(err)
				}
			}
			post := func(id, action string) {
				t.Helper()
				r := doReq(t, srv.Client(), "POST", srv.URL+"/api/instances/"+id+"/"+action, "test-token", "")
				defer r.Body.Close()
				if r.StatusCode != 200 {
					raw, _ := io.ReadAll(r.Body)
					t.Fatalf("%s/%s: %d %s", id, action, r.StatusCode, raw)
				}
			}
			post("A", "set-ok") // produtor publica a partir do snapshot
			target, wantPool := "B", true
			switch scenario {
			case "producer":
				target = "A"
			case "consumer_set_ok":
				post("B", "set-ok")
				wantPool = false
			case "consumer_without_out_remove":
				post("B", "set-ok")
			case "consumer_notok_input_removed":
				if err := s.Conditions().Unset(cond, date); err != nil {
					t.Fatal(err)
				}
				wantPool = false
			}
			post(target, "rerun")
			if got := s.Conditions().Has(cond, date); got != wantPool {
				t.Fatalf("rerun alterou pool: %v, esperado %v", got, wantPool)
			}
			for id, before := range snapshots {
				var after string
				if err := d.QueryRow(`SELECT definition_snapshot FROM instances WHERE id=?`, id).Scan(&after); err != nil {
					t.Fatal(err)
				}
				if after != before {
					t.Fatalf("M1: snapshot de %s mudou", id)
				}
			}
			if scenario == "producer" {
				var state string
				if err := d.QueryRow(`SELECT status FROM instances WHERE id='B'`).Scan(&state); err != nil || state != "NOTOK" {
					t.Fatalf("rerun do pai resetou filho: %s %v", state, err)
				}
				post("B", "rerun")
			}
			ex, err := s.Explain("B")
			if err != nil {
				t.Fatal(err)
			}
			blocked := false
			for _, b := range ex.Blockers {
				if b.Kind == scheduler.GateCondition {
					blocked = true
				}
			}
			if blocked == wantPool {
				t.Fatalf("gate não segue pool: blocked=%v pool=%v", blocked, wantPool)
			}
			if !wantPool {
				post("A", "rerun")
				post("A", "set-ok")
				if !s.Conditions().Has(cond, date) {
					t.Fatal("novo OK do produtor não recriou condição")
				}
			}
		})
	}
}
