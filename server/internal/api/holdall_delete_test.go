package api

// Hold GERAL + Delete (2026-07-16, report do usuário):
//
//   - Hold (individual, bulk e pausa de folder) segura QUALQUER status exceto
//     RUNNING (e o próprio HELD); held_from_status congela o original e o
//     Release/Resume restaura ELE — não WAITING cego (que re-executaria um OK).
//   - A pausa de folder segura o dia INTEIRO, incluindo carry-over (o carry
//     avança order_date, então a carregada entra no mesmo WHERE do dia).
//   - Delete SÓ vale para job em HOLD (RUNNING nunca — não é segurável): remove
//     a ordem da tela e do state store. Claims seguem o SettleDepClaims:
//     consumo OK (mesmo escondido sob o hold) permanece gasto — invariante 2;
//     reserva não-consumida volta pro pool (R4).

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
)

// seedInstanceDay — seedInstance com order_date e carried_from controláveis
// (a pausa de folder filtra pelo dia; carry-over = carried_from ≠ '').
func seedInstanceDay(t *testing.T, d *db.DB, id, team, status, date, carriedFrom string) {
	t.Helper()
	if _, err := d.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, team, carried_from)
		 VALUES(?,?,?,?,?,?,?)`,
		id, "def-"+id, date, status, time.Now(), team, carriedFrom,
	); err != nil {
		t.Fatalf("seed instance %s: %v", id, err)
	}
}

func instanceState(t *testing.T, d *db.DB, id string) (status, heldFrom, scope string) {
	t.Helper()
	if err := d.QueryRow(
		`SELECT status, COALESCE(held_from_status,''), COALESCE(hold_scope,'') FROM instances WHERE id=?`, id,
	).Scan(&status, &heldFrom, &scope); err != nil {
		t.Fatalf("read instance %s: %v", id, err)
	}
	return status, heldFrom, scope
}

// Hold individual vale pra qualquer status não-RUNNING e o Release restaura o
// status ORIGINAL (não WAITING).
func TestHoldAll_ReleaseRestoresOriginalStatus(t *testing.T) {
	srv, d := newOpsTestServer(t)
	for _, tc := range []struct{ id, status string }{
		{"h-wait", "WAITING"}, {"h-ok", "OK"}, {"h-notok", "NOTOK"}, {"h-canc", "CANCELLED"},
	} {
		seedInstance(t, d, tc.id, "", tc.status)

		resp := doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/instances/"+tc.id+"/hold", "test-token", "")
		if resp.StatusCode != 200 {
			t.Fatalf("hold %s (%s): esperava 200, veio %d", tc.id, tc.status, resp.StatusCode)
		}
		resp.Body.Close()
		if st, from, _ := instanceState(t, d, tc.id); st != "HELD" || from != tc.status {
			t.Fatalf("hold %s: esperava HELD/held_from=%s, veio %s/%s", tc.id, tc.status, st, from)
		}

		resp = doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/instances/"+tc.id+"/release", "test-token", "")
		if resp.StatusCode != 200 {
			t.Fatalf("release %s: esperava 200, veio %d", tc.id, resp.StatusCode)
		}
		resp.Body.Close()
		if st, from, _ := instanceState(t, d, tc.id); st != tc.status || from != "" {
			t.Fatalf("release %s: esperava restaurar %s (held_from limpo), veio %s/%q", tc.id, tc.status, st, from)
		}
	}
}

// RUNNING não é segurável: unitário responde 409; bulk reporta o erro por item.
func TestHold_RunningRejected(t *testing.T) {
	srv, d := newOpsTestServer(t)
	seedInstance(t, d, "h-run", "", "RUNNING")

	resp := doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/instances/h-run/hold", "test-token", "")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("hold RUNNING: esperava 409, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/bulk/instances", "test-token",
		`{"action":"hold","ids":["h-run"]}`)
	var out bulkResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out.Ok != 0 || out.Failed != 1 {
		t.Fatalf("bulk hold RUNNING: esperava 0 ok / 1 failed, veio %d/%d", out.Ok, out.Failed)
	}
	if st, _, _ := instanceState(t, d, "h-run"); st != "RUNNING" {
		t.Fatalf("RUNNING não podia mudar, veio %s", st)
	}
}

// A pausa de folder segura o dia INTEIRO — todos os status não-RUNNING,
// incluindo a instance de CARRY-OVER — e o resume restaura cada status
// original. O hold individual pré-existente sobrevive ao ciclo pause/resume.
func TestFolderPause_AllStatusesIncludingCarryOver(t *testing.T) {
	srv, d := newOpsTestServer(t)
	const day = "2026-07-16"
	seedInstanceDay(t, d, "fp-wait", "FIN", "WAITING", day, "")
	seedInstanceDay(t, d, "fp-ok", "FIN", "OK", day, "")
	seedInstanceDay(t, d, "fp-carried", "FIN", "NOTOK", day, "2026-07-14") // carry-over do dia 14
	seedInstanceDay(t, d, "fp-run", "FIN", "RUNNING", day, "")
	seedInstanceDay(t, d, "fp-indiv", "FIN", "HELD", day, "") // hold individual (scope '')

	resp := doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/folders/FIN/pause?date="+day, "test-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("pause: esperava 200, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	for id, wantFrom := range map[string]string{"fp-wait": "WAITING", "fp-ok": "OK", "fp-carried": "NOTOK"} {
		st, from, scope := instanceState(t, d, id)
		if st != "HELD" || from != wantFrom || scope != "folder" {
			t.Fatalf("pause %s: esperava HELD/held_from=%s/scope=folder, veio %s/%s/%s", id, wantFrom, st, from, scope)
		}
	}
	if st, _, _ := instanceState(t, d, "fp-run"); st != "RUNNING" {
		t.Fatalf("pause não podia tocar RUNNING, veio %s", st)
	}
	if st, _, scope := instanceState(t, d, "fp-indiv"); st != "HELD" || scope != "" {
		t.Fatalf("pause não podia reescrever o hold individual, veio %s/scope=%q", st, scope)
	}

	resp = doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/folders/FIN/resume?date="+day, "test-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("resume: esperava 200, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	for id, want := range map[string]string{"fp-wait": "WAITING", "fp-ok": "OK", "fp-carried": "NOTOK"} {
		if st, from, _ := instanceState(t, d, id); st != want || from != "" {
			t.Fatalf("resume %s: esperava restaurar %s, veio %s (held_from=%q)", id, want, st, from)
		}
	}
	if st, _, scope := instanceState(t, d, "fp-indiv"); st != "HELD" || scope != "" {
		t.Fatalf("resume não podia liberar o hold individual, veio %s/scope=%q", st, scope)
	}
}

// Delete: 409 fora de HOLD (WAITING e RUNNING); em HOLD remove a linha e os
// instance_events. Bulk reporta por item.
func TestDelete_RequiresHold(t *testing.T) {
	srv, d := newOpsTestServer(t)
	seedInstance(t, d, "del-wait", "", "WAITING")
	seedInstance(t, d, "del-run", "", "RUNNING")

	for _, id := range []string{"del-wait", "del-run"} {
		resp := doReq(t, srv.Client(), http.MethodDelete, srv.URL+"/api/instances/"+id, "test-token", "")
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("delete %s fora de HOLD: esperava 409, veio %d", id, resp.StatusCode)
		}
		resp.Body.Close()
	}

	// Hold → Delete: some da tela e do store, events junto.
	resp := doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/instances/del-wait/hold", "test-token", "")
	resp.Body.Close()
	resp = doReq(t, srv.Client(), http.MethodDelete, srv.URL+"/api/instances/del-wait", "test-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("delete em HOLD: esperava 200, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
	var n int
	_ = d.QueryRow(`SELECT COUNT(*) FROM instances WHERE id='del-wait'`).Scan(&n)
	if n != 0 {
		t.Fatalf("delete devia remover a instance, sobraram %d", n)
	}
	_ = d.QueryRow(`SELECT COUNT(*) FROM instance_events WHERE instance_id='del-wait'`).Scan(&n)
	if n != 0 {
		t.Fatalf("delete devia remover os instance_events, sobraram %d", n)
	}

	// Bulk misto: um HELD (ok) + um WAITING (erro por item).
	seedInstance(t, d, "del-h", "", "HELD")
	seedInstance(t, d, "del-w2", "", "WAITING")
	resp = doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/bulk/instances", "test-token",
		`{"action":"delete","ids":["del-h","del-w2"]}`)
	var out bulkResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	if out.Ok != 1 || out.Failed != 1 {
		t.Fatalf("bulk delete misto: esperava 1 ok / 1 failed, veio %d/%d", out.Ok, out.Failed)
	}
}

// (Os claims/dep_events do schemaV15 foram aposentados pela unificação de
// condições — o "consumo" agora é o OutRemove aplicado no OK, coberto em
// scheduler/conditions_unify_test.go. Hold/delete não tocam o pool.)
