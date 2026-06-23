package audit

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Emit grava uma linha JSON válida com os campos do evento.
func TestEmit_JSONLine(t *testing.T) {
	var buf bytes.Buffer
	s := &Sink{out: &buf, client: http.DefaultClient}
	s.Emit(Event{Type: "auth.login", Actor: "alice", Action: "login", Outcome: "success", IP: "1.2.3.4"})

	line := strings.TrimSpace(buf.String())
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Fatal("evento deveria terminar em newline (JSON lines)")
	}
	var got Event
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("linha não é JSON válido: %v — %q", err, line)
	}
	if got.Type != "auth.login" || got.Actor != "alice" || got.Outcome != "success" {
		t.Fatalf("campos errados: %+v", got)
	}
	if got.TS == "" {
		t.Fatal("TS deveria ser preenchido automaticamente")
	}
}

// receiver nil é no-op (não entra em pânico).
func TestEmit_NilSafe(t *testing.T) {
	var s *Sink
	s.Emit(Event{Type: "x"}) // não deve panicar
}

// Com URL configurada, faz POST do evento ao SIEM.
func TestEmit_PostsToSIEM(t *testing.T) {
	got := make(chan Event, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var e Event
		_ = json.Unmarshal(b, &e)
		got <- e
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := New(srv.URL)
	s.out = io.Discard
	s.Emit(Event{Type: "definition.write", Actor: "bob", Action: "save", Target: "team/job1", Outcome: "success"})

	select {
	case e := <-got:
		if e.Actor != "bob" || e.Target != "team/job1" {
			t.Fatalf("SIEM recebeu evento errado: %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SIEM não recebeu o POST do evento")
	}
}
