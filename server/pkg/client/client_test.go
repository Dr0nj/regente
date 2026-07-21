package client

// ADV-6 — SDK: transporte (bearer, JSON, erros) e a fachada curada contra um
// server fake httptest que valida rota/método/corpo.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeServer(t *testing.T) (*httptest.Server, *Client) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/instances/query", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-1" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type %q", ct)
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"i-1","definitionId":"job-a","status":"NOTOK","orderDate":"2026-07-10","scheduledAt":"2026-07-10T08:00:00Z"}],"nextCursor":"i-1"}`))
	})
	mux.HandleFunc("POST /api/instances/i-1/hold", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("POST /api/definitions/job-a/force", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("POST /api/events/ingest", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"e1","source":"ci","kind":"condition","applied":"condition set","receivedAt":"2026-07-10T08:00:00Z"}`))
	})
	mux.HandleFunc("GET /api/daily/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"orderDate":"2026-07-10","dailyAt":"00:00","timezone":"America/Sao_Paulo","lastRunDate":"2026-07-10","lastRunAt":"2026-07-10 03:00:00","serverNow":"2026-07-10T09:00:00-03:00"}`))
	})
	mux.HandleFunc("GET /api/archive", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"dir":"archive","archives":[{"file":"instances-2020-01-01.ndjson","day":"2020-01-01","sizeBytes":42,"modifiedAt":"2026-07-10T00:00:00Z"}]}`))
	})
	mux.HandleFunc("GET /api/archive/instances-2020-01-01.ndjson", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"i-old"}` + "\n"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rota inesperada "+r.Method+" "+r.URL.Path, 500)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, New(srv.URL, "tok-1")
}

func TestQueryInstances(t *testing.T) {
	_, c := fakeServer(t)
	res, err := c.QueryInstances(Query{Statuses: []string{"NOTOK"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].ID != "i-1" || res.Items[0].Status != "NOTOK" {
		t.Fatalf("items errados: %+v", res.Items)
	}
	if res.NextCursor != "i-1" {
		t.Fatalf("cursor não veio: %+v", res)
	}
}

func TestActionEForce(t *testing.T) {
	_, c := fakeServer(t)
	if err := c.Action("i-1", "hold"); err != nil {
		t.Fatalf("hold: %v", err)
	}
	if err := c.Action("i-1", "explodir"); err == nil || !strings.Contains(err.Error(), "inválida") {
		t.Fatalf("ação fora da lista curada deveria falhar no client: %v", err)
	}
	if err := c.ForceOrder("job-a"); err != nil {
		t.Fatalf("force: %v", err)
	}
}

func TestIngestEDailyStatus(t *testing.T) {
	_, c := fakeServer(t)
	res, err := c.Ingest(IngestEvent{ID: "e1", Source: "ci", Conditions: []string{"dados-ok"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Applied != "condition set" {
		t.Fatalf("applied errado: %+v", res)
	}
	st, err := c.DailyStatus()
	if err != nil {
		t.Fatal(err)
	}
	if st.Timezone != "America/Sao_Paulo" || st.OrderDate != "2026-07-10" {
		t.Fatalf("status errado: %+v", st)
	}
}

func TestArchivesEDownload(t *testing.T) {
	_, c := fakeServer(t)
	list, err := c.Archives()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Day != "2020-01-01" {
		t.Fatalf("lista errada: %+v", list)
	}
	var sb strings.Builder
	if err := c.DownloadArchive(list[0].File, &sb); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "i-old") {
		t.Fatalf("download errado: %q", sb.String())
	}
}

func TestErroHTTPVieraMensagem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "sem permissão nesta folder", http.StatusForbidden)
	}))
	defer srv.Close()
	c := New(srv.URL, "tok")
	_, err := c.QueryInstances(Query{})
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") || !strings.Contains(err.Error(), "sem permissão") {
		t.Fatalf("erro deveria carregar status+corpo: %v", err)
	}
}
