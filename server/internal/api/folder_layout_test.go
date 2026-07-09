package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Dr0nj/regente-server/internal/storage"
)

// UI-3 — PUT /api/folders/{name}/layout: grava/limpa o override de grade no
// stub .regente-folder.yaml e o GET /api/folders devolve o layout junto.
func TestSetFolderLayout_API(t *testing.T) {
	srv, _ := newOpsTestServer(t)

	do := func(method, path, body string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		return resp
	}

	if resp := do("POST", "/api/folders", `{"name":"PIX"}`); resp.StatusCode != 200 {
		t.Fatalf("create folder → %d", resp.StatusCode)
	}
	if resp := do("PUT", "/api/folders/PIX/layout", `{"columns":6,"maxRows":20}`); resp.StatusCode != 200 {
		t.Fatalf("set layout → %d", resp.StatusCode)
	}

	var folders []storage.FolderInfo
	getJSON(t, srv, "/api/folders", &folders)
	var pix *storage.FolderInfo
	for i := range folders {
		if folders[i].Name == "PIX" {
			pix = &folders[i]
		}
	}
	if pix == nil || pix.Layout == nil || pix.Layout.Columns != 6 || pix.Layout.MaxRows != 20 {
		t.Fatalf("GET /api/folders sem o layout gravado: %+v", pix)
	}

	// Fora do range → 400 (bug do caller, não clamp silencioso).
	if resp := do("PUT", "/api/folders/PIX/layout", `{"columns":999}`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("columns=999 devia dar 400, veio %d", resp.StatusCode)
	}

	// {} (ambos zero) remove o override.
	if resp := do("PUT", "/api/folders/PIX/layout", `{}`); resp.StatusCode != 200 {
		t.Fatalf("clear layout → %d", resp.StatusCode)
	}
	folders = nil
	getJSON(t, srv, "/api/folders", &folders)
	for _, f := range folders {
		if f.Name == "PIX" && f.Layout != nil {
			t.Fatalf("layout devia ter sido removido: %+v", f.Layout)
		}
	}
}

// UI-1 — offset no /api/instances/page: random-access pra lista virtualizada
// (salto de scrollbar pede a página N direto). Cursor presente ignora o offset.
func TestPageInstances_Offset(t *testing.T) {
	srv, date := scaleServer(t) // 6 instances, scheduled_at crescente (job-1..job-6)

	var page struct {
		Items      []instanceRow `json:"items"`
		NextCursor string        `json:"nextCursor"`
	}
	getJSON(t, srv, "/api/instances/page?date="+date+"&limit=2&offset=2", &page)
	if len(page.Items) != 2 || page.Items[0].ID != "job-3-"+date || page.Items[1].ID != "job-4-"+date {
		t.Fatalf("offset=2 limit=2 esperava [job-3, job-4], veio %+v", page.Items)
	}
	// nextCursor continua utilizável a partir do offset.
	if page.NextCursor == "" {
		t.Fatal("esperava nextCursor após página com offset")
	}

	// offset além do total → página vazia, sem erro.
	getJSON(t, srv, "/api/instances/page?date="+date+"&limit=3&offset=100", &page)
	if len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatalf("offset além do fim devia vir vazio, veio %+v", page.Items)
	}

	// cursor + offset → offset ignorado (keyset segue do cursor).
	getJSON(t, srv, "/api/instances/page?date="+date+"&limit=2", &page)
	first := page.NextCursor
	getJSON(t, srv, "/api/instances/page?date="+date+"&limit=2&cursor="+first+"&offset=4", &page)
	if len(page.Items) != 2 || page.Items[0].ID != "job-3-"+date {
		t.Fatalf("cursor devia vencer o offset (job-3 primeiro), veio %+v", page.Items)
	}
}
