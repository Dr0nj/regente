package api

// Regressão dos bugs de comportamento reportados em 2026-07-13 (docs/roadmap.md):
//
//	BUG-1  — Run Now sobre uma cópia Order Force (force_mode='order') converte
//	         pro bypass total (force_mode='') — sem isso a instance seguia presa
//	         em WAIT EVENT mesmo depois do Run Now.
//	BUG-2  — `confirmed` SOBREVIVE ao rerun: job já confirmado não volta pro
//	         gate CONFIRM (unitário e bulk).
//	BUG-3  — Set OK vale para WAITING (WAIT EVENT): conclui OK na hora, sem
//	         esperar o evento chegar.

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
)

func seedInstanceFull(t *testing.T, d *db.DB, id, status string, forced int, forceMode string, confirmed int) {
	t.Helper()
	if _, err := d.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, team, forced, force_mode, confirmed)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		id, "def-"+id, time.Now().Format("2006-01-02"), status, time.Now(), "", forced, forceMode, confirmed,
	); err != nil {
		t.Fatalf("seed instance %s: %v", id, err)
	}
}

// BUG-1: Run Now numa instance criada por Order Force limpa o force_mode —
// o ramo forced do tick (bypass total) passa a valer.
func TestRunNow_ClearsOrderForceMode(t *testing.T) {
	srv, d := newOpsTestServer(t)
	seedInstanceFull(t, d, "of-1", "WAITING", 1, "order", 0)

	resp := doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/instances/of-1/force", "test-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("run-now: esperava 200, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	var forced int
	var mode string
	if err := d.QueryRow(`SELECT forced, COALESCE(force_mode,'') FROM instances WHERE id=?`, "of-1").Scan(&forced, &mode); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if forced != 1 || mode != "" {
		t.Fatalf("Run Now deveria deixar forced=1 force_mode='' (bypass total), veio forced=%d mode=%q", forced, mode)
	}
}

// BUG-2: rerun preserva o confirmed — job já confirmado não re-exige Confirm.
func TestRerun_PreservesConfirmed(t *testing.T) {
	srv, d := newOpsTestServer(t)
	seedInstanceFull(t, d, "cf-1", "OK", 0, "", 1)

	resp := doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/instances/cf-1/rerun", "test-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("rerun: esperava 200, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	var status string
	var confirmed int
	if err := d.QueryRow(`SELECT status, COALESCE(confirmed,0) FROM instances WHERE id=?`, "cf-1").Scan(&status, &confirmed); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "WAITING" || confirmed != 1 {
		t.Fatalf("rerun deveria voltar WAITING mantendo confirmed=1, veio status=%s confirmed=%d", status, confirmed)
	}
}

// BUG-2 (bulk): mesma preservação no rerun em lote.
func TestBulkRerun_PreservesConfirmed(t *testing.T) {
	srv, d := newOpsTestServer(t)
	seedInstanceFull(t, d, "cfb-1", "OK", 0, "", 1)

	resp := doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/bulk/instances", "test-token",
		`{"action":"rerun","ids":["cfb-1"]}`)
	if resp.StatusCode != 200 {
		t.Fatalf("bulk rerun: esperava 200, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	var confirmed int
	if err := d.QueryRow(`SELECT COALESCE(confirmed,0) FROM instances WHERE id=?`, "cfb-1").Scan(&confirmed); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if confirmed != 1 {
		t.Fatalf("bulk rerun deveria manter confirmed=1, veio %d", confirmed)
	}
}

// 2026-07-14: o bypass do Run Now NÃO é pegajoso — rerun de instance com
// forced=1 e force_mode='' zera o forced (volta ao gating normal; sem isso o
// rerun re-disparava na hora, ignorando WAIT EVENT). A cópia de Order Force
// (mode='order') MANTÉM o forced — ela nunca teve janela real de agendamento.
func TestRerun_ClearsRunNowBypass(t *testing.T) {
	srv, d := newOpsTestServer(t)
	seedInstanceFull(t, d, "rn-1", "OK", 1, "", 0)      // Run Now que terminou
	seedInstanceFull(t, d, "ofc-1", "OK", 1, "order", 0) // cópia Order Force

	for _, id := range []string{"rn-1", "ofc-1"} {
		resp := doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/instances/"+id+"/rerun", "test-token", "")
		if resp.StatusCode != 200 {
			t.Fatalf("rerun %s: esperava 200, veio %d", id, resp.StatusCode)
		}
		resp.Body.Close()
	}

	var forced int
	if err := d.QueryRow(`SELECT forced FROM instances WHERE id=?`, "rn-1").Scan(&forced); err != nil {
		t.Fatalf("read back rn-1: %v", err)
	}
	if forced != 0 {
		t.Fatalf("rerun deveria zerar o forced do Run Now (mode=''), veio forced=%d", forced)
	}
	if err := d.QueryRow(`SELECT forced FROM instances WHERE id=?`, "ofc-1").Scan(&forced); err != nil {
		t.Fatalf("read back ofc-1: %v", err)
	}
	if forced != 1 {
		t.Fatalf("rerun da cópia Order Force deveria MANTER forced=1, veio %d", forced)
	}
}

// 2026-07-18: fim da tag "FORCED". O front distingue Run Now × Order Force pelo
// `forceMode` serializado — SÓ o Order Force (colocado na mão) ganha o selo 🖐
// MANUAL; Run Now força uma instance existente e NÃO leva tag. Garante que a
// lista expõe force_mode e que o omitempty esconde Run Now/daily (= sem selo).
func TestList_SerializesForceMode(t *testing.T) {
	srv, d := newOpsTestServer(t)
	seedInstanceFull(t, d, "of-1", "WAITING", 1, "order", 0) // Order Force → MANUAL
	seedInstanceFull(t, d, "rn-1", "RUNNING", 1, "", 0)      // Run Now → sem tag
	seedInstanceFull(t, d, "day-1", "WAITING", 0, "", 0)     // daily → sem tag

	date := time.Now().Format("2006-01-02")
	resp := doReq(t, srv.Client(), http.MethodGet, srv.URL+"/api/instances?date="+date, "test-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("list: esperava 200, veio %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	var list []instanceRow
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byID := map[string]instanceRow{}
	for _, r := range list {
		byID[r.ID] = r
	}
	if got := byID["of-1"].ForceMode; got != "order" {
		t.Fatalf("Order Force deveria serializar forceMode='order' (→ selo MANUAL), veio %q", got)
	}
	if got := byID["rn-1"].ForceMode; got != "" {
		t.Fatalf("Run Now NÃO deveria ter forceMode (sem tag), veio %q", got)
	}
	if got := byID["day-1"].ForceMode; got != "" {
		t.Fatalf("daily NÃO deveria ter forceMode, veio %q", got)
	}
}

// BUG-3: Set OK numa instance WAITING (WAIT COND) conclui OK na hora.
// (As saídas de condição do Set OK são cobertas em scheduler/setok_bugs_test.go
// e scheduler/conditions_unify_test.go.)
func TestSetOK_FromWaiting(t *testing.T) {
	srv, d := newOpsTestServer(t)
	seedInstanceFull(t, d, "we-1", "WAITING", 0, "", 0)

	resp := doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/instances/we-1/set-ok", "test-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("set-ok em WAITING: esperava 200, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	var status string
	if err := d.QueryRow(`SELECT status FROM instances WHERE id=?`, "we-1").Scan(&status); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "OK" {
		t.Fatalf("Set OK deveria flipar WAITING→OK, veio %s", status)
	}
}

// Set OK segue inválido para RUNNING (não sobrescreve execução em voo).
func TestSetOK_RejectsRunning(t *testing.T) {
	srv, d := newOpsTestServer(t)
	seedInstanceFull(t, d, "rn-1", "RUNNING", 0, "", 0)

	resp := doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/instances/rn-1/set-ok", "test-token", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("set-ok em RUNNING: esperava 400, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
}
