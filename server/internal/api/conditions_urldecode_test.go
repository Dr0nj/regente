package api

// Regressão (report do usuário 2026-07-17): duas condições "@Odate" que não
// podiam ser deletadas pela lixeira do painel Condições.
//
// Raiz: o front manda o nome via encodeURIComponent, então "FOO@Odate" chega na
// rota como "FOO%40Odate". O chi entrega o {name} AINDA percent-encoded; sem
// decodificar, set/unset viravam NO-OP silencioso — a rota casava (HTTP 200, o
// toast dizia "deletada"), mas o DELETE ... WHERE name='FOO%40Odate' não batia
// com o "@" gravado e 0 linhas saíam. urlName() decodifica o param (o helper
// compartilhado que set/unset e os demais handlers de recurso-por-nome usam).

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Dr0nj/regente-server/internal/domain"
)

func TestConditions_EncodedNameRoundTrip(t *testing.T) {
	srv, _ := newOpsTestServer(t)
	c := srv.Client()

	// SET pelo caminho encodado da UI: "FOO@Odate" → "FOO%40Odate".
	resp := doReq(t, c, http.MethodPost, srv.URL+"/api/conditions/FOO%40Odate/set?scope=2026-07-17", "test-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("set: esperava 200, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Deve gravar com o "@" DECODIFICADO (não "%40").
	if conds := listConds(t, c, srv.URL); len(conds) != 1 || conds[0].Name != "FOO@Odate" || conds[0].ScopeDate != "2026-07-17" {
		t.Fatalf("após set esperava [FOO@Odate/2026-07-17], veio %+v", conds)
	}

	// UNSET pelo caminho encodado — o cenário que falhava (0 linhas removidas).
	resp = doReq(t, c, http.MethodPost, srv.URL+"/api/conditions/FOO%40Odate/unset?scope=2026-07-17", "test-token", "")
	if resp.StatusCode != 200 {
		t.Fatalf("unset: esperava 200, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	if conds := listConds(t, c, srv.URL); len(conds) != 0 {
		t.Fatalf("após unset esperava pool VAZIO, veio %+v", conds)
	}
}

func listConds(t *testing.T, c *http.Client, base string) []domain.Condition {
	t.Helper()
	resp := doReq(t, c, http.MethodGet, base+"/api/conditions?scope=*", "test-token", "")
	defer resp.Body.Close()
	var out []domain.Condition
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode conditions: %v", err)
	}
	return out
}
