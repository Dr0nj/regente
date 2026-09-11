package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/hub"
)

// Fixtures de transporte usam credencial de máquina, nunca bearer administrativo.
func newMachineToken(t *testing.T, d *db.DB, ids ...string) string {
	t.Helper()
	id := "http-worker"
	if len(ids) > 0 {
		id = ids[0]
	}
	if _, err := d.Exec("INSERT INTO machine_principals(agent_id,environment,capabilities) VALUES(?,?,?) ON CONFLICT(agent_id) DO NOTHING", id, "", "COMMAND"); err != nil {
		t.Fatal(err)
	}
	token := "rgta_" + randID()
	if _, err := d.Exec("INSERT INTO agent_tokens(token_hash, label, agent_id,expires_at) VALUES(?,?,?,?)", tokenDigest(token), "test agent", id, time.Now().Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	return token
}

func TestAgentAuthRejectsHumanCredentials(t *testing.T) {
	d := newTestDB(t)
	defer d.Close()
	router := NewRouter(Config{DB: d, Hub: hub.New(), Token: "test-token"})
	tokens := map[string]string{"missing": "", "unknown": "invalid-token", "legacy-admin": "test-token"}
	revoked := newMachineToken(t, d)
	if _, err := d.Exec("DELETE FROM agent_tokens WHERE token_hash=?", tokenDigest(revoked)); err != nil {
		t.Fatal(err)
	}
	tokens["revoked-agent"] = revoked
	for _, role := range []auth.Role{auth.RoleViewer, auth.RoleOperator, auth.RoleAdmin} {
		name := "human-" + string(role)
		if _, err := auth.CreateUser(d, name, "pw", role); err != nil {
			t.Fatal(err)
		}
		token, _, err := auth.Login(d, name, "pw")
		if err != nil {
			t.Fatal(err)
		}
		tokens[string(role)] = token
	}
	endpoints := []struct{ method, path string }{
		{http.MethodGet, "/ws/agent"},
		{http.MethodGet, "/api/agent/poll"},
		{http.MethodGet, "/api/agent/events"},
		{http.MethodPost, "/api/agent/result"},
		{http.MethodPost, "/api/agent/output"},
	}
	for name, token := range tokens {
		for _, endpoint := range endpoints {
			for _, carrier := range []string{"bearer", "query"} {
				t.Run(name+endpoint.path+"/"+carrier, func(t *testing.T) {
					// Corpo/id inválidos evitam efeitos se o gate regredir: auth deve negar antes.
					req := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader("{"))
					if carrier == "bearer" {
						req.Header.Set("Authorization", "Bearer "+token)
					} else {
						q := req.URL.Query()
						q.Set("token", token)
						req.URL.RawQuery = q.Encode()
					}
					response := httptest.NewRecorder()
					router.ServeHTTP(response, req)
					if response.Code != http.StatusUnauthorized {
						t.Fatalf("credencial humana/inválida passou o gate: status=%d, esperado=401", response.Code)
					}
				})
			}
		}
	}
}

func TestAgentTokenLifecycleAndHTTPResults(t *testing.T) {
	srv, d := newOpsTestServer(t)
	resp := doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/agents/tokens", "test-token", `{"label":"worker","agentId":"http-worker","environment":"","capabilities":["COMMAND"],"expiresAt":"`+time.Now().Add(time.Hour).UTC().Format(time.RFC3339)+`"}`)
	var created struct {
		ID    int64  `json:"id"`
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || created.ID == 0 || created.Token == "" {
		t.Fatalf("emissão de credencial falhou: status=%d", resp.StatusCode)
	}
	// No sentido inverso, token de máquina também não recebe privilégio humano.
	resp = doReq(t, srv.Client(), http.MethodGet, srv.URL+"/api/users", created.Token, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("token de máquina acessou API humana: %d", resp.StatusCode)
	}
	seedInstance(t, d, "machine-job", "ops", "RUNNING")
	if _, err := d.Exec("UPDATE instances SET agent_id=? WHERE id=?", "http-worker", "machine-job"); err != nil {
		t.Fatal(err)
	}
	resp = doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/agent/output", created.Token, `{"instanceId":"machine-job","chunk":"working"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("output autenticado: %d", resp.StatusCode)
	}
	var chunk string
	if err := d.QueryRow("SELECT chunk FROM instance_output WHERE instance_id=?", "machine-job").Scan(&chunk); err != nil || chunk != "working" {
		t.Fatalf("output não persistido: chunk=%q err=%v", chunk, err)
	}
	resp = doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/agent/result", created.Token, `{"instanceId":"machine-job","exitCode":0,"output":"done"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resultado autenticado: %d", resp.StatusCode)
	}
	var status string
	if err := d.QueryRow("SELECT status FROM instances WHERE id=?", "machine-job").Scan(&status); err != nil || status != "OK" {
		t.Fatalf("resultado não aplicado: status=%q err=%v", status, err)
	}
	// A revogação deve negar a próxima chamada HTTP.
	if _, err := d.Exec("DELETE FROM agent_tokens WHERE id=?", created.ID); err != nil {
		t.Fatal(err)
	}
	resp = doReq(t, srv.Client(), http.MethodPost, srv.URL+"/api/agent/result", created.Token, `{"instanceId":"machine-job","exitCode":1}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("credencial revogada aceitou resultado: %d", resp.StatusCode)
	}
}

func TestAgentHTTPPollWithMachineToken(t *testing.T) {
	d := newTestDB(t)
	defer d.Close()
	h := hub.New()
	token := newMachineToken(t, d)
	srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: h, Token: "test-token"}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/agent/poll?id=http-worker&caps=COMMAND", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	type outcome struct {
		response *http.Response
		err      error
	}
	result := make(chan outcome, 1)
	go func() { response, err := srv.Client().Do(req); result <- outcome{response, err} }()
	deadline := time.Now().Add(2 * time.Second)
	for !h.IsOnline("http-worker") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !h.IsOnline("http-worker") {
		t.Fatal("agente HTTP não registrou com credencial própria")
	}
	if sent, _ := h.Dispatch("http-worker", "COMMAND", "", []byte(`{"event":"dispatch","instanceId":"job"}`)); sent != hub.DispatchSent {
		t.Fatal("dispatch não enviado")
	}
	got := <-result
	if got.err != nil {
		t.Fatal(got.err)
	}
	defer got.response.Body.Close()
	var payload struct {
		InstanceID string `json:"instanceId"`
	}
	if err := json.NewDecoder(got.response.Body).Decode(&payload); err != nil || payload.InstanceID != "job" || got.response.StatusCode != http.StatusOK {
		t.Fatalf("poll não entregou dispatch: status=%d payload=%+v err=%v", got.response.StatusCode, payload, err)
	}
}
