package api

import (
	"encoding/json"
	"fmt"
	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/bus"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
	"github.com/nats-io/nats.go"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/gorilla/websocket"
)

type webTestMessage struct {
	kind int
	raw  []byte
	err  error
}

// Um único leitor processa também os frames de controle. O pong prova que
// wsWeb já registrou o cliente e entrou em clientReader, sem sleeps ou reenvios.
type webTestConn struct {
	*websocket.Conn
	messages chan webTestMessage
	done     chan struct{}
	stop     sync.Once
}

func newWebTestConn(c *websocket.Conn) *webTestConn {
	w := &webTestConn{Conn: c, messages: make(chan webTestMessage), done: make(chan struct{})}
	go func() {
		for {
			kind, raw, err := c.ReadMessage()
			select {
			case w.messages <- webTestMessage{kind, raw, err}:
			case <-w.done:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return w
}

func (c *webTestConn) ReadMessage() (int, []byte, error) {
	select {
	case msg := <-c.messages:
		return msg.kind, msg.raw, msg.err
	case <-time.After(3 * time.Second):
		return 0, nil, os.ErrDeadlineExceeded
	}
}

func (c *webTestConn) Close() error {
	c.stop.Do(func() { close(c.done) })
	return c.Conn.Close()
}

func webTestConnect(t *testing.T, issue, connect *httptest.Server, token, query string) *webTestConn {
	t.Helper()
	var ticket struct{ Ticket string }
	raw := machineRequest(t, issue, "POST", "/api/auth/event-ticket", token, nil, 200)
	if err := json.Unmarshal(raw, &ticket); err != nil {
		t.Fatal(err)
	}
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(connect.URL, "http")+"/ws/web?ticket="+ticket.Ticket+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{}, 1)
	c.SetPongHandler(func(payload string) error {
		if payload == "i04-ready" {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
		return nil
	})
	w := newWebTestConn(c)
	t.Cleanup(func() { w.Close() })
	if err := c.WriteControl(websocket.PingMessage, []byte("i04-ready"), time.Now().Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("servidor não confirmou o registro do canal")
	}
	return w
}

func TestWebEventAuthorization(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d := machineTestDB(t, dialect)
			h := hub.New()
			srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: h}))
			defer srv.Close()
			seedInstance(t, d, "allowed", "A", "WAITING")
			seedInstance(t, d, "secret", "B", "WAITING")
			token := newOperatorToken(t, d, "reader", map[string]string{"A": "r"})
			c := webTestConnect(t, srv, srv, token, "")
			h.BroadcastWeb("instance.changed", map[string]any{"id": "secret", "status": "RUNNING", "private": "must-not-leak"})
			h.BroadcastWeb("instance.changed", map[string]any{"id": "allowed", "status": "OK"})
			_, raw, err := c.ReadMessage()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "must-not-leak") || !strings.Contains(string(raw), "allowed") {
				t.Fatalf("payload atravessou ACL: %s", raw)
			}
		})
	}
}

// eventTestTransport usa o mesmo contrato do barramento. PostgreSQL no gate
// obrigatório usa NATS real; SQLite local usa entrega síncrona determinística.
type eventTestTransport struct {
	mu   sync.Mutex
	subs map[string][]func([]byte)
}

func (f *eventTestTransport) Publish(subject string, raw []byte) error {
	f.mu.Lock()
	handlers := append([]func([]byte){}, f.subs[subject]...)
	f.mu.Unlock()
	for _, handler := range handlers {
		handler(append([]byte{}, raw...))
	}
	return nil
}
func (f *eventTestTransport) Subscribe(subject string, handler func([]byte)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subs[subject] = append(f.subs[subject], handler)
	return nil
}

type eventNATSTransport struct {
	nc     *nats.Conn
	prefix string
}

func (f *eventNATSTransport) Publish(subject string, raw []byte) error {
	if err := f.nc.Publish(f.prefix+subject, raw); err != nil {
		return err
	}
	return f.nc.FlushTimeout(3 * time.Second)
}
func (f *eventNATSTransport) Subscribe(subject string, handler func([]byte)) error {
	if _, err := f.nc.Subscribe(f.prefix+subject, func(m *nats.Msg) { handler(m.Data) }); err != nil {
		return err
	}
	return f.nc.FlushTimeout(3 * time.Second)
}

func readWeb(t *testing.T, c *webTestConn, event, contains string) string {
	t.Helper()
	_, raw, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var ev struct {
		Event   string
		Payload json.RawMessage
	}
	if json.Unmarshal(raw, &ev) != nil || ev.Event != event || !strings.Contains(string(ev.Payload), contains) {
		t.Fatalf("evento inesperado: %s; queria %s/%s", raw, event, contains)
	}
	return string(raw)
}
func closedWeb(t *testing.T, c *webTestConn) {
	t.Helper()
	if _, raw, err := c.ReadMessage(); err == nil {
		t.Fatalf("canal revogado entregou: %s", raw)
	} else if e, ok := err.(interface{ Timeout() bool }); ok && e.Timeout() {
		t.Fatal("canal não revogou no prazo")
	}
}

func TestWebEventDistributed(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d := machineTestDB(t, dialect)
			var tr bus.Transport = &eventTestTransport{subs: map[string][]func([]byte){}}
			if dialect == db.Postgres {
				target := os.Getenv("REGENTE_TEST_NATS_URL")
				if target == "" {
					if os.Getenv("REGENTE_REQUIRE_INTEGRATION") == "1" {
						t.Fatal("NATS real obrigatório")
					}
					t.Skip("NATS real não configurado")
				}
				nc, err := nats.Connect(target)
				if err != nil {
					t.Fatal(err)
				}
				defer nc.Close()
				tr = &eventNATSTransport{nc: nc, prefix: "i04." + randID() + "."}
			}
			ha, hb := hub.New(), hub.New()
			ba, bb := bus.NewDistributed("a", ha, tr), bus.NewDistributed("b", hb, tr)
			if err := ba.Start(); err != nil {
				t.Fatal(err)
			}
			if err := bb.Start(); err != nil {
				t.Fatal(err)
			}
			defer ba.Stop()
			defer bb.Stop()
			store := storage.NewFileStore(t.TempDir(), false)
			sched := scheduler.New(store, d, ba, time.Hour)
			defer sched.Stop()
			a := httptest.NewServer(NewRouter(Config{DB: d, Hub: ha, Events: ba, Store: store, Scheduler: sched, Token: "lab-admin"}))
			b := httptest.NewServer(NewRouter(Config{DB: d, Hub: hb, Events: bb, Store: store, Token: "lab-admin"}))
			defer a.Close()
			defer b.Close()
			seedInstance(t, d, "public-a", "A", "HELD")
			seedInstance(t, d, "private-b", "B", "HELD")
			seedInstance(t, d, "a-prod", "A", "HELD")
			if _, err := d.Exec("UPDATE instances SET environment='prod' WHERE id='a-prod'"); err != nil {
				t.Fatal(err)
			}
			ta := newOperatorToken(t, d, "scope-a", map[string]string{"A": "r"})
			tb := newOperatorToken(t, d, "scope-b", map[string]string{"B": "r"})
			ca := webTestConnect(t, a, b, ta, "")
			cb := webTestConnect(t, b, a, tb, "")
			admin := webTestConnect(t, a, b, "lab-admin", "")
			publish := func(id string) {
				ba.BroadcastWeb("instance.changed", map[string]any{"id": id, "status": "OK", "unexpectedSecret": "never-on-wire"})
			}
			publish("private-b")
			publish("public-a")
			if raw := readWeb(t, ca, "instance.changed", "public-a"); strings.Contains(raw, "unexpectedSecret") {
				t.Fatal("campo não autorizado")
			}
			readWeb(t, cb, "instance.changed", "private-b")
			readWeb(t, admin, "instance.changed", "private-b")
			readWeb(t, admin, "instance.changed", "public-a")

			// Eventos desconhecidos e globais privados não podem anteceder a sentinela.
			ba.BroadcastWeb("future.private", map[string]string{"secret": "private-b"})
			ba.BroadcastWeb("instance.changed", map[string]string{"id": "missing"})
			ba.BroadcastWeb("condition.changed", map[string]string{"name": "private-b"})
			ba.BroadcastWeb("alert.fired", map[string]string{"instanceId": "private-b", "message": "private-b"})
			publish("public-a")
			readWeb(t, ca, "instance.changed", "public-a")

			// Lote misto só invalida: totais, atores e a lista interna não saem.
			ba.BroadcastWeb("instance.bulk", map[string]any{"_scopes": []webScope{{"A", ""}, {"B", ""}}, "total": 99, "actor": "private-b"})
			if raw := readWeb(t, ca, "instance.bulk", "{}"); strings.Contains(raw, "99") || strings.Contains(raw, "private-b") {
				t.Fatal("metadado de lote vazou")
			}

			// Settings contém segredos: mesmo admin só recebe uma invalidação vazia.
			ca.Close()
			cb.Close()
			admin.Close()
			ca = webTestConnect(t, a, b, ta, "")
			admin = webTestConnect(t, a, b, "lab-admin", "")
			ba.BroadcastWeb("settings.changed", map[string]string{"github_token": "secret-value", "smtp_password": "secret-value"})
			readWeb(t, ca, "settings.changed", "{}")
			readWeb(t, admin, "settings.changed", "{}")
			ca.Close()
			admin.Close()

			// Ambiente estreita o escopo; alteração da instance é avaliada no envio.
			env := webTestConnect(t, a, b, ta, "&environment=prod")
			publish("public-a")
			publish("private-b")
			publish("a-prod")
			readWeb(t, env, "instance.changed", "a-prod")
			if _, err := d.Exec("UPDATE instances SET environment='dev' WHERE id='a-prod'"); err != nil {
				t.Fatal(err)
			}
			publish("a-prod")
			ba.BroadcastWeb("daily.started", map[string]any{"orderDate": "2026-09-17", "created": 999, "commitSha": "private"})
			readWeb(t, env, "daily.started", `"orderDate":"2026-09-17"`)
			env.Close()

			// Tombstone real da API no A chega ao B depois do DELETE, sem scope interno.
			ca = webTestConnect(t, a, b, ta, "")
			machineRequest(t, a, "DELETE", "/api/instances/public-a", "lab-admin", nil, 200)
			raw := readWeb(t, ca, "instance.deleted", "public-a")
			if strings.Contains(raw, "_scope") {
				t.Fatal("envelope interno exposto")
			}
			seedInstance(t, d, "new-a", "A", "HELD")

			// ACL no A invalida imediatamente a política do canal no B.
			user, err := auth.Resolve(d, ta)
			if err != nil {
				t.Fatal(err)
			}
			machineRequest(t, a, "PUT", fmt.Sprintf("/api/users/%d/acls", user.ID), "lab-admin", []map[string]string{{"folder": "B", "perms": "r"}}, 200)
			publish("new-a")
			closedWeb(t, ca)
			ca = webTestConnect(t, a, b, ta, "")
			publish("new-a")
			publish("private-b")
			readWeb(t, ca, "instance.changed", "private-b")

			// Mudança de papel sem encerrar sessão exige subscription nova.
			if err := auth.SetRole(d, user.ID, auth.RoleAdmin); err != nil {
				t.Fatal(err)
			}
			closedWeb(t, ca)
			ca = webTestConnect(t, a, b, ta, "")
			publish("new-a")
			readWeb(t, ca, "instance.changed", "new-a")
			if err := auth.SetRole(d, user.ID, auth.RoleViewer); err != nil {
				t.Fatal(err)
			}
			publish("new-a")
			closedWeb(t, ca)
			ca = webTestConnect(t, a, b, ta, "")
			publish("new-a")
			publish("private-b")
			readWeb(t, ca, "instance.changed", "private-b")

			// Falha de DB ou sessão expirada não preserva o acesso do socket.
			if _, err := d.Exec("UPDATE sessions SET expires_at=? WHERE token=?", time.Now().Add(-time.Minute), auth.Digest(ta)); err != nil {
				t.Fatal(err)
			}
			publish("private-b")
			closedWeb(t, ca)
		})
	}
}

func TestWebEventPolicy(t *testing.T) {
	d := newTestDB(t)
	defer d.Close()
	store := storage.NewFileStore(t.TempDir(), false)
	s := &server{cfg: Config{DB: d, Store: store}}
	a := &webAccess{User: &auth.User{Role: auth.RoleViewer}, ACLs: []auth.FolderACL{{FolderName: "A", Perms: "r"}}}
	admin := &webAccess{User: &auth.User{Role: auth.RoleAdmin}}
	seedInstance(t, d, "a", "A", "HELD")
	seedInstance(t, d, "b", "B", "HELD")
	check := func(event string, payload any, allowed bool) {
		t.Helper()
		view := ""
		raw, _ := json.Marshal(map[string]any{"event": event, "payload": payload})
		got := s.filterWebEvent(a, nil, &view, raw)
		if (len(got) > 0) != allowed {
			t.Fatalf("%s: permitido=%v, payload=%s", event, allowed, got)
		}
	}
	for _, event := range []string{"instance.changed", "sla.breach", "alert.fired", "alert.changed"} {
		for _, id := range []string{"a", "b", "missing"} {
			check(event, map[string]string{"id": id, "instanceId": id, "message": "sample"}, id == "a")
		}
	}
	check("instance.deleted", map[string]any{"id": "gone", "_scope": webScope{"A", ""}}, true)
	check("instance.deleted", map[string]any{"id": "gone", "_scope": webScope{"B", ""}}, false)
	check("instance.deleted", map[string]any{"id": "gone"}, false)
	check("instance.bulk", map[string]any{"_scopes": []webScope{{"B", ""}}}, false)
	check("instance.bulk", map[string]any{"_scopes": []webScope{{"A", ""}, {"B", ""}}}, true)
	check("agent.changed", map[string]string{"id": "secret-agent"}, false)
	check("alert.fired", map[string]string{"workflowId": "unknown", "message": "private"}, false)
	for _, event := range []string{"git.drift", "variables.changed", "condition.changed", "unknown"} {
		check(event, map[string]string{"name": "secret"}, false)
	}
	for _, who := range []*webAccess{a, admin} {
		v := ""
		raw := s.filterWebEvent(who, nil, &v, []byte(`{"event":"settings.changed","payload":{"token":"secret"}}`))
		if string(raw) != `{"event":"settings.changed","payload":{}}` {
			t.Fatalf("settings vazou: %s", raw)
		}
		if s.filterWebEvent(who, nil, &v, []byte(`{"event":"unknown","payload":{"secret":true}}`)) != nil {
			t.Fatal("evento desconhecido aceito")
		}
	}

	// Um rename/delete/sync de outro folder não revela nem mesmo seu nome/SHA.
	defA := domain.JobDefinition{ID: "job-a", Team: "A", Label: "Visible"}
	defB := domain.JobDefinition{ID: "job-b", Team: "B", Label: "Hidden"}
	if err := store.Save(defA); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(defB); err != nil {
		t.Fatal(err)
	}
	view, err := s.workspaceView(a, nil)
	if err != nil {
		t.Fatal(err)
	}
	event := []byte(`{"event":"definition.changed","payload":{"reason":"git-sync","sha":"secret-sha"}}`)
	defB.Label = "Other private name"
	if err := store.Save(defB); err != nil {
		t.Fatal(err)
	}
	if got := s.filterWebEvent(a, nil, &view, event); got != nil {
		t.Fatalf("mudança oculta propagou: %s", got)
	}
	defA.Label = "Visible changed"
	if err := store.Save(defA); err != nil {
		t.Fatal(err)
	}
	if got := s.filterWebEvent(a, nil, &view, event); string(got) != `{"event":"_resync","payload":{}}` {
		t.Fatalf("refresh autorizado ausente: %s", got)
	}
	if got := s.filterWebEvent(a, nil, &view, event); got != nil {
		t.Fatal("refresh duplicado")
	}
	if err := store.RenameFolder("A", "private-renamed"); err != nil {
		t.Fatal(err)
	}
	if got := s.filterWebEvent(a, nil, &view, event); string(got) != `{"event":"_resync","payload":{}}` {
		t.Fatalf("rename não removeu visão antiga: %s", got)
	}

	if _, err := d.Exec(`UPDATE instances SET conds_in=? WHERE id=?`, `["SHARED@prev","PREFIX-LONG"]`, "a"); err != nil {
		t.Fatal(err)
	}
	check("condition.changed", map[string]string{"name": "SHARED"}, true)
	check("condition.unset", map[string]string{"name": "PREFIX"}, false)
	check("condition.set", map[string]string{"name": "%"}, false)

	// A substituição inválida não apaga as ACLs e não abre read-all.
	token := newOperatorToken(t, d, "replace", map[string]string{"A": "r"})
	u, err := auth.Resolve(d, token)
	if err != nil {
		t.Fatal(err)
	}
	if auth.ReplaceUserACLs(d, u.ID, []auth.FolderACL{{FolderName: "B", Perms: "r"}, {FolderName: "", Perms: "r"}}) == nil {
		t.Fatal("ACL inválida aceita")
	}
	if allowed, err := auth.CanReadFolder(d, u, "B"); err != nil || allowed {
		t.Fatal("falha de replace abriu acesso")
	}

	d.Close()
	if got := s.filterWebEvent(a, nil, &view, []byte(`{"event":"instance.changed","payload":{"id":"a"}}`)); got != nil {
		t.Fatal("DB indisponível permitiu evento")
	}
}

func TestWebEventOrigin(t *testing.T) {
	d := newTestDB(t)
	defer d.Close()
	srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: hub.New(), Token: "lab-admin", AppURL: "https://ui.example.test"}))
	defer srv.Close()
	for _, origin := range []string{"https://evil.test", "null", "https://ui.example.test.evil", "https://ui.example.test/path"} {
		var ticket struct{ Ticket string }
		json.Unmarshal(machineRequest(t, srv, "POST", "/api/auth/event-ticket", "lab-admin", nil, 200), &ticket)
		c, res, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/web?ticket="+ticket.Ticket, http.Header{"Origin": []string{origin}})
		if err == nil {
			c.Close()
			t.Fatalf("Origin recusado foi aceito: %s", origin)
		}
		if res == nil || res.StatusCode != 403 {
			t.Fatalf("Origin não recebeu 403: %v", res)
		}
		res.Body.Close()
		// Origin inválido não deve consumir o ticket.
		c, _, err = websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/web?ticket="+ticket.Ticket, http.Header{"Origin": []string{"https://ui.example.test"}})
		if err != nil {
			t.Fatal(err)
		}
		c.Close()
	}
}

func TestWebEventQueuedRevocation(t *testing.T) {
	// Simula writer atrasado: revogação acontece depois do enqueue, antes do write.
	for _, filterPresent := range []bool{true, false} {
		t.Run(fmt.Sprintf("filter_%v", filterPresent), func(t *testing.T) {
			d := newTestDB(t)
			defer d.Close()
			token := newOperatorToken(t, d, "queued", map[string]string{"A": "r"})
			s := &server{cfg: Config{DB: d}}
			initial, err := s.webAccess(auth.Digest(token))
			if err != nil {
				t.Fatal(err)
			}
			fingerprint := initial.fingerprint()
			ready, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer close(done)
				c := &hub.Client{Kind: hub.ClientWeb, Conn: conn, Send: make(chan []byte, 1), Authorize: func() bool {
					current, e := s.webAccess(auth.Digest(token))
					return e == nil && current.fingerprint() == fingerprint
				}}
				if filterPresent {
					c.FilterWeb = func(raw []byte) []byte { return raw }
				}
				c.Send <- []byte(`{"event":"instance.changed","payload":{"id":"sensitive-buffered"}}`)
				close(c.Send)
				close(ready)
				<-release
				clientWriter(c)
			}))
			defer srv.Close()
			c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			<-ready
			if filterPresent {
				if err := auth.ReplaceUserACLs(d, initial.User.ID, []auth.FolderACL{{FolderName: "B", Perms: "r"}}); err != nil {
					t.Fatal(err)
				}
			}
			close(release)
			reader := newWebTestConn(c)
			defer reader.Close()
			closedWeb(t, reader)
			<-done
		})
	}
}
