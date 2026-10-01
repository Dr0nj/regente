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
	if resp, body := get("/api-docs"); resp.StatusCode != 200 || !strings.Contains(body, "integration API") {
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

// DOC-07/08: referências resolvíveis, security explícita e exemplos por estado.
// Não substitui um validador OpenAPI completo nem os testes de runtime.
func TestOpenAPI_DocumentedContracts(t *testing.T) {
	raw, err := openAPIJSON()
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var walk func(any)
	walk = func(node any) {
		switch n := node.(type) {
		case map[string]any:
			if ref, ok := n["$ref"].(string); ok {
				if !strings.HasPrefix(ref, "#/") {
					t.Fatalf("referência externa inesperada: %s", ref)
				}
				var target any = doc
				for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
					m, ok := target.(map[string]any)
					if !ok {
						t.Fatalf("referência inválida: %s", ref)
					}
					target, ok = m[part]
					if !ok {
						t.Fatalf("referência ausente: %s", ref)
					}
				}
			}
			for _, child := range n {
				walk(child)
			}
		case []any:
			for _, child := range n {
				walk(child)
			}
		}
	}
	walk(doc)
	security := doc["security"].([]any)
	if len(security) != 1 || security[0].(map[string]any)["bearerAuth"] == nil {
		t.Fatal("contrato curado deve declarar bearer de integração")
	}
	schemes := doc["components"].(map[string]any)["securitySchemes"].(map[string]any)
	if scheme := schemes["bearerAuth"].(map[string]any); scheme["type"] != "http" || scheme["scheme"] != "bearer" {
		t.Fatal("security scheme inválido")
	}
	paths := doc["paths"].(map[string]any)
	// Vírgula sem aspas num flow-map YAML inventava uma propriedade inválida.
	query := paths["/api/query"].(map[string]any)["post"].(map[string]any)
	qSchema := query["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["properties"].(map[string]any)["q"].(map[string]any)
	if len(qSchema) != 2 || qSchema["description"] != "The question, in natural language." {
		t.Fatalf("schema de q inválido: %v", qSchema)
	}
	for _, path := range []string{"/health", "/livez", "/readyz", "/metrics"} {
		op := paths[path].(map[string]any)["get"].(map[string]any)
		if sec, ok := op["security"].([]any); !ok || len(sec) != 0 {
			t.Fatalf("probe %s não está público", path)
		}
	}
	cancel := paths["/api/instances/{id}/cancel"].(map[string]any)["post"].(map[string]any)
	responses := cancel["responses"].(map[string]any)
	for _, code := range []string{"200", "401", "403", "409", "500"} {
		if responses[code] == nil {
			t.Errorf("cancel sem resposta %s", code)
		}
	}
	examples := responses["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["examples"].(map[string]any)
	for example, want := range map[string]string{"running": "NOTOK", "pending": "CANCELLED"} {
		if got := examples[example].(map[string]any)["value"].(map[string]any)["status"]; got != want {
			t.Errorf("%s: %v != %s", example, got, want)
		}
	}
}
