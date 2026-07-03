package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// ViewPoints salvos: criar, upsert por (owner,name), listar (meus + shared) e
// deletar com checagem de dono. O legacy token resolve para o actor "system".
func TestViewpoints_CRUD(t *testing.T) {
	srv, done := newTestServer(t)
	defer done()
	c := srv.Client()

	// 1) cria um viewpoint.
	resp, err := c.Do(authReq(t, http.MethodPost, srv.URL+"/api/viewpoints",
		`{"name":"PIX falhando","filters":{"folder":"PIX","status":"NOTOK"}}`))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("POST viewpoint esperava 200, veio %v err=%v", statusOf(resp), err)
	}
	var created struct {
		ID      int64 `json:"id"`
		Updated bool  `json:"updated"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	if created.ID == 0 || created.Updated {
		t.Fatalf("criação inesperada: %+v", created)
	}

	// 2) salvar de novo com o MESMO nome → upsert (updated=true), sem duplicar.
	resp, _ = c.Do(authReq(t, http.MethodPost, srv.URL+"/api/viewpoints",
		`{"name":"PIX falhando","filters":{"folder":"PIX","status":"NOTOK,SLA_BREACH"}}`))
	var upd struct {
		ID      int64 `json:"id"`
		Updated bool  `json:"updated"`
	}
	json.NewDecoder(resp.Body).Decode(&upd)
	resp.Body.Close()
	if !upd.Updated || upd.ID != created.ID {
		t.Fatalf("re-salvar deveria dar upsert no mesmo id; veio %+v", upd)
	}

	// 3) lista → exatamente 1 (o upsert não duplicou).
	var list []struct {
		ID      int64           `json:"id"`
		Name    string          `json:"name"`
		Filters json.RawMessage `json:"filters"`
	}
	getJSONAuth(t, c, srv.URL+"/api/viewpoints", &list)
	if len(list) != 1 || list[0].Name != "PIX falhando" {
		t.Fatalf("esperava 1 viewpoint, veio %+v", list)
	}

	// 4) nome vazio → 400.
	resp, _ = c.Do(authReq(t, http.MethodPost, srv.URL+"/api/viewpoints",
		`{"name":"  ","filters":{}}`))
	if resp.StatusCode != 400 {
		t.Fatalf("nome vazio esperava 400, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 5) deletar → some da lista.
	req := authReq(t, http.MethodDelete, srv.URL+"/api/viewpoints/"+itoa(created.ID), "")
	resp, _ = c.Do(req)
	if resp.StatusCode != 200 {
		t.Fatalf("DELETE esperava 200, veio %d", resp.StatusCode)
	}
	resp.Body.Close()
	getJSONAuth(t, c, srv.URL+"/api/viewpoints", &list)
	if len(list) != 0 {
		t.Fatalf("após delete a lista deveria estar vazia, veio %+v", list)
	}
}

func getJSONAuth(t *testing.T, c *http.Client, url string, out any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := c.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("GET %s → %v err=%v", url, statusOf(resp), err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
