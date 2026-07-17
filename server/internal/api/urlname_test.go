package api

// Regressão do decode do {name} da rota, agora no helper compartilhado urlName()
// (urlname.go). O front manda nomes de recurso via encodeURIComponent, então "@"
// vira "%40" e o espaço vira "%20"; o chi entrega o segmento AINDA encodado.
// Sem decodificar, put/delete viram NO-OP silencioso (HTTP 2xx, mas grava/apaga
// o nome ERRADO com "%40"/"%20" dentro). O mesmo bug que travava a lixeira de
// condições (963b8b9) era latente em variables, calendars, resources, folders e
// templates — estes testes cobrem o round-trip encodado por variables e
// calendars (os dois com store próprio, fáceis de exercitar de ponta a ponta).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
)

// newNamedResTestServer — router real com Variables e Calendars plugados (o
// newOpsTestServer padrão não os attacha, então os handlers cairiam em 503).
func newNamedResTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	d := newTestDB(t)
	t.Cleanup(func() { _ = d.Close() })
	h := hub.New()
	root := t.TempDir()
	store := storage.NewFileStore(root, false)
	sched := scheduler.New(store, d, h, time.Hour)
	t.Cleanup(sched.Stop)

	vs, err := storage.NewVariableStore(d)
	if err != nil {
		t.Fatalf("variable store: %v", err)
	}
	sched.AttachVariables(vs)
	sched.AttachCalendars(storage.NewCalendarStore(root))

	srv := httptest.NewServer(NewRouter(Config{DB: d, Hub: h, Token: "test-token", Scheduler: sched, Store: store}))
	t.Cleanup(srv.Close)
	return srv
}

// TestVariables_EncodedNameRoundTrip — PUT+DELETE de uma variável cujo nome tem
// "@" E espaço, pelo caminho encodado ("APP%40Prod%20Key"). Sem urlName() o Set
// gravaria "APP%40Prod%20Key" cru e o List denunciaria o nome errado.
func TestVariables_EncodedNameRoundTrip(t *testing.T) {
	srv := newNamedResTestServer(t)
	c := srv.Client()

	resp := doReq(t, c, http.MethodPut, srv.URL+"/api/variables/APP%40Prod%20Key", "test-token", `{"value":"v1"}`)
	if resp.StatusCode != 200 {
		t.Fatalf("put: esperava 200, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Deve gravar com "@" e espaço DECODIFICADOS (não "%40"/"%20").
	if vars := listVars(t, c, srv.URL); len(vars) != 1 || vars[0].Name != "APP@Prod Key" || vars[0].Value != "v1" {
		t.Fatalf("após put esperava [APP@Prod Key=v1], veio %+v", vars)
	}

	// DELETE pelo caminho encodado — o cenário que era NO-OP sem decode.
	resp = doReq(t, c, http.MethodDelete, srv.URL+"/api/variables/APP%40Prod%20Key", "test-token", "")
	if resp.StatusCode != 204 {
		t.Fatalf("delete: esperava 204, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	if vars := listVars(t, c, srv.URL); len(vars) != 0 {
		t.Fatalf("após delete esperava vazio, veio %+v", vars)
	}
}

// TestCalendars_EncodedNameRoundTrip — PUT+DELETE de um calendar cujo nome tem
// "@" E espaço, pelo caminho encodado ("HOL%40BR%202026"). Sem urlName() o Save
// escreveria o arquivo "HOL%40BR%202026.yaml" e o List traria o nome errado.
func TestCalendars_EncodedNameRoundTrip(t *testing.T) {
	srv := newNamedResTestServer(t)
	c := srv.Client()

	resp := doReq(t, c, http.MethodPut, srv.URL+"/api/calendars/HOL%40BR%202026", "test-token", `{}`)
	if resp.StatusCode != 200 {
		t.Fatalf("put: esperava 200, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	if cals := listCals(t, c, srv.URL); len(cals) != 1 || cals[0].Name != "HOL@BR 2026" {
		t.Fatalf("após put esperava [HOL@BR 2026], veio %+v", cals)
	}

	resp = doReq(t, c, http.MethodDelete, srv.URL+"/api/calendars/HOL%40BR%202026", "test-token", "")
	if resp.StatusCode != 204 {
		t.Fatalf("delete: esperava 204, veio %d", resp.StatusCode)
	}
	resp.Body.Close()

	if cals := listCals(t, c, srv.URL); len(cals) != 0 {
		t.Fatalf("após delete esperava vazio, veio %+v", cals)
	}
}

func listVars(t *testing.T, c *http.Client, base string) []storage.Variable {
	t.Helper()
	resp := doReq(t, c, http.MethodGet, base+"/api/variables", "test-token", "")
	defer resp.Body.Close()
	var out []storage.Variable
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode variables: %v", err)
	}
	return out
}

func listCals(t *testing.T, c *http.Client, base string) []domain.Calendar {
	t.Helper()
	resp := doReq(t, c, http.MethodGet, base+"/api/calendars", "test-token", "")
	defer resp.Body.Close()
	var out []domain.Calendar
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode calendars: %v", err)
	}
	return out
}
