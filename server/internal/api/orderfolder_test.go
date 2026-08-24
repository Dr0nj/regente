package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// postFolderOrder — POST /api/folders/{name}/order com (ou sem) bearer.
func postFolderOrder(t *testing.T, url, folder, token string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+"/api/folders/"+folder+"/order", nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// A rota existe e exige writer; folder sem definition publicada é 400 (erro
// visível), não 200 mudo.
func TestOrderFolder_RouteIsWriterGated(t *testing.T) {
	srv, _ := newOpsTestServer(t)

	if code, _ := postFolderOrder(t, srv.URL, "FIN", ""); code != http.StatusUnauthorized {
		t.Fatalf("sem token esperava 401, veio %d", code)
	}
	if code, _ := postFolderOrder(t, srv.URL, "FIN", "test-token"); code != http.StatusBadRequest {
		t.Fatalf("folder vazia esperava 400, veio %d", code)
	}
}

// Nome de folder percent-encoded tem que chegar DECODIFICADO no handler
// (urlName, não chi.URLParam cru): com "FIN%20OPS" cru o team nunca casaria com
// a definition e a ordem viraria um no-op silencioso — a mesma armadilha das
// rotas de condição/recurso por nome.
func TestOrderFolder_DecodesFolderName(t *testing.T) {
	srv, _ := newOpsTestServer(t)
	code, body := postFolderOrder(t, srv.URL, "FIN%20OPS", "test-token")
	if code != http.StatusBadRequest {
		t.Fatalf("esperava 400, veio %d (%s)", code, body)
	}
	// O erro cita a folder: tem que estar DECODIFICADA.
	if !strings.Contains(body, "FIN OPS") {
		t.Fatalf("o handler recebeu o nome ainda encodado — erro: %q", strings.TrimSpace(body))
	}
}
