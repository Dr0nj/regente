// API-1 — o contrato openapi.yaml é um ESPELHO DECLARADO do router: este
// teste trava (a) spec que aponta pra rota que não existe mais e (b) a
// integridade da conversão YAML→JSON ordenada. Não valida payloads campo a
// campo (a spec é curada à mão de propósito); valida que o contrato não
// aponta pro vazio.
package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

// TestOpenAPI_SpecCasaComRouter — toda operação da spec existe no router chi
// (método + path idênticos, placeholders {id} inclusive).
func TestOpenAPI_SpecCasaComRouter(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(openAPIYAML, &doc); err != nil {
		t.Fatalf("openapi.yaml inválido: %v", err)
	}
	if len(doc.Paths) == 0 {
		t.Fatal("spec sem paths")
	}

	d := newTestDB(t)
	t.Cleanup(func() { _ = d.Close() })
	store := storage.NewFileStore(t.TempDir(), false)
	sched := scheduler.New(store, d, hub.New(), time.Hour)
	t.Cleanup(sched.Stop)
	router := NewRouter(Config{DB: d, Hub: hub.New(), Token: "test-token", Scheduler: sched, Store: store}).(chi.Router)

	routes := map[string]bool{}
	_ = chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes[method+" "+strings.TrimSuffix(route, "/")] = true
		// chi registra /api-docs/ com barra; normaliza os dois lados.
		routes[method+" "+route] = true
		return nil
	})

	methods := map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true}
	for path, ops := range doc.Paths {
		for method := range ops {
			if !methods[method] {
				continue // summary/description/parameters no nível do path
			}
			key := strings.ToUpper(method) + " " + path
			if !routes[key] {
				t.Errorf("spec documenta %s mas o router não tem essa rota — atualize openapi.yaml ou o router", key)
			}
		}
	}
}

// TestOpenAPI_JSONOrdenadoValido — a conversão node-a-node produz JSON válido
// com o conteúdo esperado.
func TestOpenAPI_JSONOrdenadoValido(t *testing.T) {
	out, err := openAPIJSON()
	if err != nil {
		t.Fatalf("conversão YAML→JSON: %v", err)
	}
	var parsed struct {
		OpenAPI string         `json:"openapi"`
		Info    map[string]any `json:"info"`
		Paths   map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("JSON gerado inválido: %v", err)
	}
	if !strings.HasPrefix(parsed.OpenAPI, "3.0") {
		t.Fatalf("openapi = %q, esperava 3.0.x", parsed.OpenAPI)
	}
	if len(parsed.Paths) == 0 {
		t.Fatal("JSON sem paths")
	}
	// Ordem curada preservada: /health (primeira rota autorada) tem que vir
	// antes de /api/instances/query no stream — json de map embaralharia.
	sHealth := strings.Index(string(out), `"/health"`)
	sQuery := strings.Index(string(out), `"/api/instances/query"`)
	if sHealth == -1 || sQuery == -1 || sHealth > sQuery {
		t.Fatalf("ordem dos paths não preservada (health@%d, query@%d)", sHealth, sQuery)
	}
}

// TestOpenAPI_ServidoPublico — /api-docs responde sem auth: viewer HTML,
// spec YAML e JSON, e o redirect da raiz.
func TestOpenAPI_ServidoPublico(t *testing.T) {
	srv, _ := newOpsTestServer(t)

	get := func(path string) (*http.Response, string) {
		t.Helper()
		resp, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return resp, string(b)
	}

	if resp, body := get("/api-docs/"); resp.StatusCode != 200 ||
		!strings.Contains(resp.Header.Get("Content-Type"), "text/html") ||
		!strings.Contains(body, "openapi.json") {
		t.Fatalf("viewer: status=%d ct=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if resp, body := get("/api-docs/openapi.yaml"); resp.StatusCode != 200 || !strings.Contains(body, "openapi: 3.0") {
		t.Fatalf("yaml: status=%d", resp.StatusCode)
	}
	if resp, body := get("/api-docs/openapi.json"); resp.StatusCode != 200 ||
		!strings.Contains(resp.Header.Get("Content-Type"), "application/json") ||
		!strings.Contains(body, `"openapi"`) {
		t.Fatalf("json: status=%d ct=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	// redirect /api-docs → /api-docs/ (client segue; tem que acabar no viewer)
	if resp, body := get("/api-docs"); resp.StatusCode != 200 || !strings.Contains(body, "API de integração") {
		t.Fatalf("redirect: status=%d", resp.StatusCode)
	}
	// Zero CDN: o viewer não referencia host externo.
	_, body := get("/api-docs/")
	for _, banned := range []string{"cdn.", "unpkg.com", "jsdelivr", "googleapis"} {
		if strings.Contains(body, banned) {
			t.Fatalf("viewer referencia recurso externo: %s", banned)
		}
	}
}
