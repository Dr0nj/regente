package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockRegente devolve um servidor HTTP que imita os endpoints do regente-server.
func mockRegente(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t" {
			http.Error(w, "unauth", 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// echo — devolve o método + path chamado, pra os testes de escrita
		// afirmarem que a tool bateu no endpoint certo com o verbo certo.
		echo := func() { _, _ = w.Write([]byte(`{"hit":"` + r.Method + " " + r.URL.Path + `"}`)) }
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/instances/summary"):
			_, _ = w.Write([]byte(`{"date":"2026-06-24","total":3,"byStatus":{"OK":3}}`))
		case strings.HasPrefix(r.URL.Path, "/api/forecast/range"):
			_, _ = w.Write([]byte(`{"hit":"range","days":"` + r.URL.Query().Get("days") + `"}`))
		case strings.HasPrefix(r.URL.Path, "/api/forecast"):
			_, _ = w.Write([]byte(`{"hit":"single"}`))
		case strings.HasSuffix(r.URL.Path, "/explain"):
			_, _ = w.Write([]byte(`{"runnable":false,"summary":"Nao roda: falta condition"}`))
		case strings.HasSuffix(r.URL.Path, "/rerun"):
			_, _ = w.Write([]byte(`{"id":"x","status":"WAITING"}`))
		// escritas unitárias, force, pause/resume, bulk, ingest → echo do método+path.
		case r.Method == http.MethodPost && (strings.HasSuffix(r.URL.Path, "/hold") ||
			strings.HasSuffix(r.URL.Path, "/release") || strings.HasSuffix(r.URL.Path, "/cancel") ||
			strings.HasSuffix(r.URL.Path, "/confirm") || strings.HasSuffix(r.URL.Path, "/set-ok") ||
			strings.HasSuffix(r.URL.Path, "/force") || strings.HasSuffix(r.URL.Path, "/pause") ||
			strings.HasSuffix(r.URL.Path, "/resume") || r.URL.Path == "/api/bulk/instances" ||
			r.URL.Path == "/api/events/ingest"):
			echo()
		default:
			http.Error(w, "not found", 404)
		}
	}))
}

func newM(ts *httptest.Server, writes bool) *mcpServer {
	return &mcpServer{baseURL: ts.URL, token: "t", http: ts.Client(), allowWrites: writes}
}

func toolsCall(m *mcpServer, name string, args map[string]interface{}) rpcResp {
	params, _ := json.Marshal(map[string]interface{}{"name": name, "arguments": args})
	resp, _ := m.handle(rpcReq{Method: "tools/call", ID: json.RawMessage(`1`), Params: params})
	return resp
}

func contentOf(r rpcResp) (text string, isErr bool) {
	res, _ := r.Result.(map[string]interface{})
	isErr, _ = res["isError"].(bool)
	if c, ok := res["content"].([]map[string]interface{}); ok && len(c) > 0 {
		text, _ = c[0]["text"].(string)
	}
	return
}

func TestMCP_Initialize(t *testing.T) {
	m := &mcpServer{}
	resp, notif := m.handle(rpcReq{Method: "initialize", ID: json.RawMessage(`1`),
		Params: json.RawMessage(`{"protocolVersion":"2025-06-18"}`)})
	if notif {
		t.Fatal("initialize não é notificação")
	}
	res := resp.Result.(map[string]interface{})
	if res["protocolVersion"] != "2025-06-18" {
		t.Fatalf("deveria ecoar a versão do cliente, veio %v", res["protocolVersion"])
	}
	if _, ok := res["capabilities"].(map[string]interface{})["tools"]; !ok {
		t.Fatal("deveria declarar capability 'tools'")
	}
}

func TestMCP_NotificationNoResponse(t *testing.T) {
	m := &mcpServer{}
	_, notif := m.handle(rpcReq{Method: "notifications/initialized"})
	if !notif {
		t.Fatal("notifications/initialized não deveria gerar resposta")
	}
}

func TestMCP_ToolsList_ReadOnlyByDefault(t *testing.T) {
	ts := mockRegente(t)
	defer ts.Close()

	names := func(m *mcpServer) map[string]bool {
		resp, _ := m.handle(rpcReq{Method: "tools/list", ID: json.RawMessage(`1`)})
		tools := resp.Result.(map[string]interface{})["tools"].([]map[string]interface{})
		out := map[string]bool{}
		for _, tl := range tools {
			out[tl["name"].(string)] = true
		}
		return out
	}

	ro := names(newM(ts, false))
	for _, want := range []string{"daily_summary", "forecast", "list_instances", "explain_job", "blast_radius", "diff_daily", "dry_run", "job_neighborhood", "root_cause", "event_log", "query"} {
		if !ro[want] {
			t.Errorf("tool de leitura %q faltando", want)
		}
	}
	writes := []string{"hold_job", "release_job", "cancel_job", "confirm_job", "rerun_job", "set_ok", "force_order", "pause_folder", "resume_folder", "bulk_action", "ingest_event"}
	for _, wname := range writes {
		if ro[wname] {
			t.Errorf("write %q NÃO deveria aparecer sem -allow-writes", wname)
		}
	}

	rw := names(newM(ts, true))
	for _, wname := range writes {
		if !rw[wname] {
			t.Errorf("write %q deveria aparecer com -allow-writes", wname)
		}
	}
}

// Cobre as escritas RICAS (MCP-1): gate por -allow-writes, endpoint/verbo certo,
// e validação de argumentos obrigatórios.
func TestMCP_RichWrites(t *testing.T) {
	ts := mockRegente(t)
	defer ts.Close()
	rw := newM(ts, true)

	// Todas as escritas bloqueiam sem -allow-writes.
	ro := newM(ts, false)
	for _, name := range []string{"hold_job", "cancel_job", "confirm_job", "force_order", "pause_folder", "bulk_action", "ingest_event"} {
		if _, isErr := contentOf(toolsCall(ro, name, map[string]interface{}{})); !isErr {
			t.Errorf("%s deveria bloquear sem -allow-writes", name)
		}
	}

	// Ações unitárias batem em POST /api/instances/{id}/<action>.
	unit := map[string]string{"hold_job": "/hold", "release_job": "/release", "cancel_job": "/cancel", "confirm_job": "/confirm", "set_ok": "/set-ok"}
	for name, suffix := range unit {
		text, isErr := contentOf(toolsCall(rw, name, map[string]interface{}{"instanceId": "job-1"}))
		if isErr || !strings.Contains(text, "POST /api/instances/job-1"+suffix) {
			t.Errorf("%s: isErr=%v text=%q", name, isErr, text)
		}
	}
	// ...e exigem instanceId.
	if _, isErr := contentOf(toolsCall(rw, "hold_job", map[string]interface{}{})); !isErr {
		t.Error("hold_job sem instanceId deveria falhar")
	}

	// force_order → POST /api/definitions/{id}/force, exige definitionId.
	text, isErr := contentOf(toolsCall(rw, "force_order", map[string]interface{}{"definitionId": "etl-vendas"}))
	if isErr || !strings.Contains(text, "POST /api/definitions/etl-vendas/force") {
		t.Errorf("force_order: isErr=%v text=%q", isErr, text)
	}
	if _, isErr := contentOf(toolsCall(rw, "force_order", map[string]interface{}{})); !isErr {
		t.Error("force_order sem definitionId deveria falhar")
	}

	// pause/resume folder → POST /api/folders/{name}/(pause|resume), exige folder.
	text, isErr = contentOf(toolsCall(rw, "pause_folder", map[string]interface{}{"folder": "PIX"}))
	if isErr || !strings.Contains(text, "POST /api/folders/PIX/pause") {
		t.Errorf("pause_folder: isErr=%v text=%q", isErr, text)
	}
	text, _ = contentOf(toolsCall(rw, "resume_folder", map[string]interface{}{"folder": "PIX"}))
	if !strings.Contains(text, "POST /api/folders/PIX/resume") {
		t.Errorf("resume_folder text=%q", text)
	}
	if _, isErr := contentOf(toolsCall(rw, "pause_folder", map[string]interface{}{})); !isErr {
		t.Error("pause_folder sem folder deveria falhar")
	}

	// bulk_action → POST /api/bulk/instances, exige action + ids[].
	text, isErr = contentOf(toolsCall(rw, "bulk_action", map[string]interface{}{"action": "hold", "ids": []interface{}{"a", "b"}}))
	if isErr || !strings.Contains(text, "POST /api/bulk/instances") {
		t.Errorf("bulk_action: isErr=%v text=%q", isErr, text)
	}
	if _, isErr := contentOf(toolsCall(rw, "bulk_action", map[string]interface{}{"action": "hold"})); !isErr {
		t.Error("bulk_action sem ids deveria falhar")
	}

	// ingest_event → POST /api/events/ingest; exige conditions[] e/ou forceJob.
	text, isErr = contentOf(toolsCall(rw, "ingest_event", map[string]interface{}{"conditions": []interface{}{"ARQ_OK"}}))
	if isErr || !strings.Contains(text, "POST /api/events/ingest") {
		t.Errorf("ingest_event: isErr=%v text=%q", isErr, text)
	}
	if _, isErr := contentOf(toolsCall(rw, "ingest_event", map[string]interface{}{"source": "sap"})); !isErr {
		t.Error("ingest_event sem conditions/forceJob deveria falhar")
	}
}

// forecast read tool: days>1 usa /forecast/range; senão /forecast.
func TestMCP_ForecastTool(t *testing.T) {
	ts := mockRegente(t)
	defer ts.Close()
	m := newM(ts, false)

	text, isErr := contentOf(toolsCall(m, "forecast", map[string]interface{}{"date": "2026-07-06"}))
	if isErr || !strings.Contains(text, `"hit":"single"`) {
		t.Fatalf("forecast 1 dia: isErr=%v text=%q", isErr, text)
	}
	text, isErr = contentOf(toolsCall(m, "forecast", map[string]interface{}{"date": "2026-07-06", "days": 7}))
	if isErr || !strings.Contains(text, `"hit":"range"`) || !strings.Contains(text, `"days":"7"`) {
		t.Fatalf("forecast 7 dias: isErr=%v text=%q", isErr, text)
	}
}

func TestMCP_CallReadTool(t *testing.T) {
	ts := mockRegente(t)
	defer ts.Close()
	m := newM(ts, false)

	text, isErr := contentOf(toolsCall(m, "daily_summary", map[string]interface{}{"date": "2026-06-24"}))
	if isErr || !strings.Contains(text, `"total":3`) {
		t.Fatalf("daily_summary: isErr=%v text=%q", isErr, text)
	}

	text, isErr = contentOf(toolsCall(m, "explain_job", map[string]interface{}{"instanceId": "job-1"}))
	if isErr || !strings.Contains(text, "falta condition") {
		t.Fatalf("explain_job: isErr=%v text=%q", isErr, text)
	}
}

func TestMCP_ExplainRequiresID(t *testing.T) {
	ts := mockRegente(t)
	defer ts.Close()
	text, isErr := contentOf(toolsCall(newM(ts, false), "explain_job", map[string]interface{}{}))
	if !isErr || !strings.Contains(text, "instanceId") {
		t.Fatalf("explain_job sem id deveria falhar, veio isErr=%v text=%q", isErr, text)
	}
}

func TestMCP_WriteBlockedByDefault(t *testing.T) {
	ts := mockRegente(t)
	defer ts.Close()

	text, isErr := contentOf(toolsCall(newM(ts, false), "rerun_job", map[string]interface{}{"instanceId": "x"}))
	if !isErr || !strings.Contains(text, "escrita desabilitada") {
		t.Fatalf("rerun sem -allow-writes deveria falhar, veio isErr=%v text=%q", isErr, text)
	}

	// Com writes habilitados, passa.
	text, isErr = contentOf(toolsCall(newM(ts, true), "rerun_job", map[string]interface{}{"instanceId": "x"}))
	if isErr || !strings.Contains(text, "WAITING") {
		t.Fatalf("rerun com -allow-writes deveria funcionar, veio isErr=%v text=%q", isErr, text)
	}
}

// End-to-end pelo loop stdio: initialize + tools/call em linhas JSON-RPC.
func TestMCP_RunStdioLoop(t *testing.T) {
	ts := mockRegente(t)
	defer ts.Close()
	m := newM(ts, false)

	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"daily_summary","arguments":{}}}`,
	}, "\n") + "\n")
	var out bytes.Buffer
	m.run(in, &out)

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 { // initialize + tools/call (a notificação não responde)
		t.Fatalf("esperava 2 respostas (notificação não responde), veio %d: %q", len(lines), out.String())
	}
	var call rpcResp
	if err := json.Unmarshal([]byte(lines[1]), &call); err != nil {
		t.Fatalf("parse resposta tools/call: %v", err)
	}
	res := call.Result.(map[string]interface{})
	content := res["content"].([]interface{})[0].(map[string]interface{})
	if !strings.Contains(content["text"].(string), `"total":3`) {
		t.Fatalf("conteúdo inesperado: %v", content["text"])
	}
}
