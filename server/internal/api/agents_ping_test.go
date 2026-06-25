package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/gorilla/websocket"
)

func postJSON(t *testing.T, srv *httptest.Server, path string, out any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+path, nil)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("POST %s → %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func TestPingAgent_OfflineAndRoundTrip(t *testing.T) {
	d := newTestDB(t)
	h := hub.New()
	cfg := Config{DB: d, Hub: h, Token: "test-token"}
	srv := httptest.NewServer(NewRouter(cfg))
	defer srv.Close()
	defer d.Close()

	// Agente inexistente → offline.
	var off pingResult
	postJSON(t, srv, "/api/agents/ghost/ping", &off)
	if off.Online || off.OK {
		t.Fatalf("ping em agente offline deveria dar online=false ok=false, veio %+v", off)
	}

	// Conecta um agente WS real que responde pong.
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/agent?token=test-token&id=ag1&caps=COMMAND&os=linux"
	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer c.Close()
	go func() {
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			var m struct {
				Event  string `json:"event"`
				PingID string `json:"pingId"`
			}
			if json.Unmarshal(msg, &m) == nil && m.Event == "ping" {
				_ = c.WriteMessage(websocket.TextMessage, []byte(`{"event":"pong","pingId":"`+m.PingID+`"}`))
			}
		}
	}()

	// Espera o registro no hub.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !h.IsOnline("ag1") {
		time.Sleep(10 * time.Millisecond)
	}
	if !h.IsOnline("ag1") {
		t.Fatal("agente WS não registrou no hub")
	}

	var on pingResult
	postJSON(t, srv, "/api/agents/ag1/ping", &on)
	if !on.OK || !on.Online {
		t.Fatalf("ping round-trip deveria dar ok=true, veio %+v", on)
	}
	if on.LatencyMs < 0 {
		t.Fatalf("latência inválida: %d", on.LatencyMs)
	}
}
