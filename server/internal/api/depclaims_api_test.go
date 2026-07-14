package api

// 2026-07-14 (report do usuário): com VÁRIAS cópias do mesmo job no dia
// (Order Force/rerun), o Monitoring desenha uma linha POR PAR de instances e
// precisa saber QUAL cópia do pai satisfez cada consumidor pra pintar verde só
// o par real. A API expõe esse detalhe em depsClaims [{from, parentInstanceId}]
// ao lado do depsSatisfied — ambos SEMPRE lista, nunca null (R10).

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestListInstances_DepsClaimsCarryParentInstance(t *testing.T) {
	srv, d := newOpsTestServer(t)
	today := time.Now().Format("2006-01-02")

	// Duas cópias do pai terminadas OK e dois consumidores; b-1 clamou o evento
	// emitido por a-1 — o detalhe tem que apontar a-1, não "qualquer cópia".
	seedInstanceFull(t, d, "a-1", "OK", 0, "", 0)
	seedInstanceFull(t, d, "a-2", "OK", 0, "", 0)
	seedInstanceFull(t, d, "b-1", "WAITING", 0, "", 0)
	seedInstanceFull(t, d, "b-2", "WAITING", 0, "", 0)

	if _, err := d.Exec(
		`INSERT INTO dep_events(def_id, instance_id, order_date, status) VALUES(?,?,?,?)`,
		"def-a-1", "a-1", today, "OK",
	); err != nil {
		t.Fatalf("seed dep_event: %v", err)
	}
	var eventID int64
	if err := d.QueryRow(`SELECT id FROM dep_events WHERE instance_id=?`, "a-1").Scan(&eventID); err != nil {
		t.Fatalf("event id: %v", err)
	}
	if _, err := d.Exec(
		`INSERT INTO dep_claims(event_id, consumer_instance_id, consumer_def_id, upstream_def_id) VALUES(?,?,?,?)`,
		eventID, "b-1", "def-b-1", "def-a-1",
	); err != nil {
		t.Fatalf("seed dep_claim: %v", err)
	}

	resp := doReq(t, srv.Client(), http.MethodGet, srv.URL+"/api/instances?date="+today, "test-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("list: esperava 200, veio %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// R10: lista sempre presente, nunca null.
	if strings.Contains(string(body), `"depsClaims":null`) {
		t.Fatalf("depsClaims serializou null (R10): %s", body)
	}

	var list []struct {
		ID            string   `json:"id"`
		DepsSatisfied []string `json:"depsSatisfied"`
		DepsClaims    []struct {
			From             string `json:"from"`
			ParentInstanceID string `json:"parentInstanceId"`
		} `json:"depsClaims"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	byID := map[string]int{}
	for i, ir := range list {
		byID[ir.ID] = i
	}

	b1 := list[byID["b-1"]]
	if len(b1.DepsSatisfied) != 1 || b1.DepsSatisfied[0] != "def-a-1" {
		t.Fatalf("b-1 depsSatisfied: esperava [def-a-1], veio %v", b1.DepsSatisfied)
	}
	if len(b1.DepsClaims) != 1 || b1.DepsClaims[0].From != "def-a-1" || b1.DepsClaims[0].ParentInstanceID != "a-1" {
		t.Fatalf("b-1 depsClaims: esperava [{def-a-1 a-1}], veio %v", b1.DepsClaims)
	}

	b2 := list[byID["b-2"]]
	if len(b2.DepsSatisfied) != 0 || len(b2.DepsClaims) != 0 {
		t.Fatalf("b-2 sem claim deveria vir com listas vazias, veio sat=%v claims=%v", b2.DepsSatisfied, b2.DepsClaims)
	}
}
