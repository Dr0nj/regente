package api

// 2026-07-14 (report do usuário): job COMMAND com `sleep 60` rodava certo mas o
// Monitoring mostrava "No action configured" — a lista de /api/instances não
// carrega a action de propósito (payload de escala) e não existia detalhe por
// id. GET /api/instances/{id} expõe label/jobType/actionConfig extraídos da
// definition_snapshot: a foto congelada na ordem, a MESMA que o dispatch
// executa (defForInstance) — editar o job no Design depois não muda a resposta.

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

func TestGetInstance_ExposesFrozenAction(t *testing.T) {
	srv, d := newOpsTestServer(t)
	today := time.Now().Format("2006-01-02")

	def := domain.JobDefinition{
		ID: "sleepy", Label: "Sleepy Job", JobType: "COMMAND", Team: "FIN",
		Params: map[string]interface{}{"command": "sleep 60"},
	}
	snap, _ := json.Marshal(def)
	if _, err := d.Exec(
		`INSERT INTO instances(id, definition_id, team, order_date, status, scheduled_at, definition_snapshot, agent_id, exit_code, output)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		"sleepy-1", "sleepy", "FIN", today, "OK", time.Now(), string(snap), "agente-x", 0, "",
	); err != nil {
		t.Fatalf("seed instance: %v", err)
	}

	resp := doReq(t, srv.Client(), http.MethodGet, srv.URL+"/api/instances/sleepy-1", "test-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("detail: esperava 200, veio %d", resp.StatusCode)
	}
	var det struct {
		ID           string         `json:"id"`
		Team         string         `json:"team"`
		Label        string         `json:"label"`
		JobType      string         `json:"jobType"`
		ActionConfig map[string]any `json:"actionConfig"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&det); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()

	if det.ID != "sleepy-1" || det.Team != "FIN" {
		t.Fatalf("linha errada: %+v", det)
	}
	if det.Label != "Sleepy Job" || det.JobType != "COMMAND" {
		t.Fatalf("snapshot não refletido: label=%q jobType=%q", det.Label, det.JobType)
	}
	if cmd, _ := det.ActionConfig["command"].(string); cmd != "sleep 60" {
		t.Fatalf("actionConfig.command: esperava 'sleep 60', veio %v", det.ActionConfig)
	}

	// Id inexistente → 404.
	resp = doReq(t, srv.Client(), http.MethodGet, srv.URL+"/api/instances/nope", "test-token", "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("inexistente: esperava 404, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	// RBAC de leitura: operator SEM acesso à folder FIN não enxerga a instance
	// (404, não 403 — não vaza existência); COM leitura, 200.
	opSem := newOperatorToken(t, d, "op-sem-fin", map[string]string{"RISCO": "rw"})
	resp = doReq(t, srv.Client(), http.MethodGet, srv.URL+"/api/instances/sleepy-1", opSem, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("RBAC sem leitura: esperava 404, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	opCom := newOperatorToken(t, d, "op-com-fin", map[string]string{"FIN": "r"})
	resp = doReq(t, srv.Client(), http.MethodGet, srv.URL+"/api/instances/sleepy-1", opCom, "")
	if resp.StatusCode != 200 {
		t.Fatalf("RBAC com leitura: esperava 200, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// Instance sem snapshot (seed antigo): o detalhe responde a linha sem os campos
// do snapshot — o front cai na definition de desenho.
func TestGetInstance_NoSnapshotFallsBackEmpty(t *testing.T) {
	srv, d := newOpsTestServer(t)
	seedInstanceFull(t, d, "bare-1", "WAITING", 0, "", 0)

	resp := doReq(t, srv.Client(), http.MethodGet, srv.URL+"/api/instances/bare-1", "test-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("esperava 200, veio %d", resp.StatusCode)
	}
	var det struct {
		ID           string         `json:"id"`
		Label        string         `json:"label"`
		ActionConfig map[string]any `json:"actionConfig"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&det); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if det.ID != "bare-1" || det.Label != "" || det.ActionConfig != nil {
		t.Fatalf("sem snapshot deveria vir sem label/actionConfig: %+v", det)
	}
}
