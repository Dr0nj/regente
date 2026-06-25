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
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/instances/summary"):
			_, _ = w.Write([]byte(`{"date":"2026-06-24","total":3,"byStatus":{"OK":3}}`))
		case strings.HasSuffix(r.URL.Path, "/explain"):
			_, _ = w.Write([]byte(`{"runnable":false,"summary":"Nao roda: falta condition"}`))
		case strings.HasSuffix(r.URL.Path, "/rerun"):
			_, _ = w.Write([]byte(`{"id":"x","status":"WAITING"}`))
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
	for _, want := range []string{"daily_summary", "list_instances", "explain_job", "blast_radius", "diff_daily", "dry_run"} {
		if !ro[want] {
			t.Errorf("tool de leitura %q faltando", want)
		}
	}
	if ro["rerun_job"] || ro["set_ok"] {
		t.Error("writes NÃO deveriam aparecer sem -allow-writes")
	}

	rw := names(newM(ts, true))
	if !rw["rerun_job"] || !rw["set_ok"] {
		t.Error("writes deveriam aparecer com -allow-writes")
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
