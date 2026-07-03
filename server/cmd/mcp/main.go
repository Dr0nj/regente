// regente-mcp — servidor MCP (Model Context Protocol) do Regente.
//
// Expõe os diferenciais determinísticos (Explain · Diff de Daily · Blast Radius ·
// Dry Run) + summary/busca como TOOLS que um agente (Claude Desktop, etc.) chama.
// É uma FACHADA agent-native sobre a API REST — não toca o core do servidor. O LLM
// narra a verdade que o engine computa (não inventa); o cliente MCP pede aprovação
// humana em cada chamada (writes só com -allow-writes, marcadas destructiveHint).
//
// Transporte: JSON-RPC 2.0 newline-delimited sobre stdio (padrão do Claude Desktop).
// Pure-Go stdlib, zero dependência.
//
//	go build -o regente-mcp ./cmd/mcp
//	REGENTE_URL=http://localhost:8080 REGENTE_TOKEN=dev-token ./regente-mcp
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const protocolVersionDefault = "2024-11-05"

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type mcpServer struct {
	baseURL     string
	token       string
	http        *http.Client
	allowWrites bool
}

func main() {
	url := flag.String("url", envOr("REGENTE_URL", "http://localhost:8080"), "URL base do regente-server")
	token := flag.String("token", envOr("REGENTE_TOKEN", "dev-token"), "Bearer token da API")
	allowWrites := flag.Bool("allow-writes", false, "Expõe tools de escrita (rerun_job, set_ok); o cliente MCP ainda pede aprovação por chamada")
	flag.Parse()

	m := &mcpServer{
		baseURL:     strings.TrimRight(*url, "/"),
		token:       *token,
		http:        &http.Client{Timeout: 15 * time.Second},
		allowWrites: *allowWrites,
	}
	m.run(os.Stdin, os.Stdout)
}

// run — loop stdio: lê uma mensagem JSON-RPC por linha, despacha, responde (exceto
// notificações). Logs vão para stderr (stdout é o canal do protocolo).
func (m *mcpServer) run(in io.Reader, out io.Writer) {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	w := bufio.NewWriter(out)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var req rpcReq
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		resp, isNotification := m.handle(req)
		if isNotification {
			continue
		}
		b, _ := json.Marshal(resp)
		_, _ = w.Write(b)
		_ = w.WriteByte('\n')
		_ = w.Flush()
	}
}

func (m *mcpServer) handle(req rpcReq) (rpcResp, bool) {
	switch req.Method {
	case "initialize":
		ver := protocolVersionDefault
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(req.Params, &p) == nil && p.ProtocolVersion != "" {
			ver = p.ProtocolVersion // ecoa a versão do cliente (compat)
		}
		return m.ok(req.ID, map[string]interface{}{
			"protocolVersion": ver,
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]interface{}{"name": "regente-mcp", "version": "0.1.0"},
		}), false
	case "notifications/initialized", "notifications/cancelled":
		return rpcResp{}, true // notificação: sem resposta
	case "ping":
		return m.ok(req.ID, map[string]interface{}{}), false
	case "tools/list":
		return m.ok(req.ID, map[string]interface{}{"tools": m.tools()}), false
	case "tools/call":
		return m.callTool(req), false
	default:
		return m.fail(req.ID, -32601, "method not found: "+req.Method), false
	}
}

func (m *mcpServer) callTool(req rpcReq) rpcResp {
	var p struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return m.fail(req.ID, -32602, "invalid params")
	}
	text, err := m.dispatch(p.Name, p.Arguments)
	if err != nil {
		// Erro de tool: conteúdo com isError (convenção MCP), não erro de protocolo.
		return m.ok(req.ID, map[string]interface{}{
			"content": []map[string]interface{}{{"type": "text", "text": "erro: " + err.Error()}},
			"isError": true,
		})
	}
	return m.ok(req.ID, map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": text}},
	})
}

// dispatch mapeia tool → chamada HTTP na API REST.
func (m *mcpServer) dispatch(name string, args map[string]interface{}) (string, error) {
	str := func(k string) string { v, _ := args[k].(string); return v }
	requireID := func() (string, error) {
		id := str("instanceId")
		if id == "" {
			return "", fmt.Errorf("instanceId é obrigatório")
		}
		return id, nil
	}

	switch name {
	case "daily_summary":
		return m.get("/api/instances/summary" + qs(map[string]string{"date": str("date")}))
	case "list_instances":
		q := map[string]string{"date": str("date"), "folder": str("folder"), "status": str("status"), "q": str("q")}
		if n := intArg(args, "limit"); n > 0 {
			q["limit"] = strconv.Itoa(n)
		}
		return m.get("/api/instances" + qs(q))
	case "explain_job":
		id, err := requireID()
		if err != nil {
			return "", err
		}
		return m.get("/api/instances/" + url.PathEscape(id) + "/explain")
	case "blast_radius":
		id, err := requireID()
		if err != nil {
			return "", err
		}
		return m.get("/api/instances/" + url.PathEscape(id) + "/blast-radius")
	case "job_neighborhood":
		id, err := requireID()
		if err != nil {
			return "", err
		}
		q := map[string]string{}
		if n := intArg(args, "radius"); n > 0 {
			q["radius"] = strconv.Itoa(n)
		}
		return m.get("/api/instances/" + url.PathEscape(id) + "/neighborhood" + qs(q))
	case "root_cause":
		id, err := requireID()
		if err != nil {
			return "", err
		}
		return m.get("/api/instances/" + url.PathEscape(id) + "/rca")
	case "event_log":
		q := map[string]string{"date": str("date"), "kind": str("kind"), "actor": str("actor"), "folder": str("folder"), "instance": str("instance")}
		if n := intArg(args, "limit"); n > 0 {
			q["limit"] = strconv.Itoa(n)
		}
		return m.get("/api/events" + qs(q))
	case "query":
		q := str("q")
		if q == "" {
			return "", fmt.Errorf("q (pergunta) é obrigatório")
		}
		return m.postJSON("/api/query", map[string]string{"q": q})
	case "diff_daily":
		return m.get("/api/daily/diff" + qs(map[string]string{"from": str("from"), "to": str("to"), "folder": str("folder")}))
	case "dry_run":
		return m.get("/api/daily/dryrun" + qs(map[string]string{"date": str("date")}))
	case "rerun_job":
		if !m.allowWrites {
			return "", fmt.Errorf("escrita desabilitada (suba o servidor com -allow-writes)")
		}
		id, err := requireID()
		if err != nil {
			return "", err
		}
		return m.post("/api/instances/" + url.PathEscape(id) + "/rerun")
	case "set_ok":
		if !m.allowWrites {
			return "", fmt.Errorf("escrita desabilitada (suba o servidor com -allow-writes)")
		}
		id, err := requireID()
		if err != nil {
			return "", err
		}
		return m.post("/api/instances/" + url.PathEscape(id) + "/set-ok")
	default:
		return "", fmt.Errorf("tool desconhecida: %q", name)
	}
}

// tools devolve a lista de tools (read-only sempre; writes só com -allow-writes).
func (m *mcpServer) tools() []map[string]interface{} {
	strProp := func(desc string) map[string]interface{} {
		return map[string]interface{}{"type": "string", "description": desc}
	}
	schema := func(props map[string]interface{}, required ...string) map[string]interface{} {
		s := map[string]interface{}{"type": "object", "properties": props}
		if len(required) > 0 {
			s["required"] = required
		}
		return s
	}
	readOnly := map[string]interface{}{"readOnlyHint": true}
	destructive := map[string]interface{}{"destructiveHint": true}

	out := []map[string]interface{}{
		{
			"name":        "daily_summary",
			"description": "Resumo da daily de uma data (default hoje): total e contagem por status e por folder. Use pra ter o panorama do dia.",
			"inputSchema": schema(map[string]interface{}{"date": strProp("YYYY-MM-DD (default hoje)")}),
			"annotations": readOnly,
		},
		{
			"name":        "list_instances",
			"description": "Lista instances do dia com filtros (folder, status, busca por id). Use pra ENCONTRAR o instanceId de um job antes de explain_job/blast_radius.",
			"inputSchema": schema(map[string]interface{}{
				"date": strProp("YYYY-MM-DD (default hoje)"), "folder": strProp("folder/team exato"),
				"status": strProp("WAITING|RUNNING|OK|NOTOK|HELD|CANCELLED (lista separada por vírgula)"),
				"q":      strProp("busca em id/definition_id"),
				"limit":  map[string]interface{}{"type": "integer", "description": "máx de linhas (default 200)"},
			}),
			"annotations": readOnly,
		},
		{
			"name":        "explain_job",
			"description": "Por que um job (não) rodou: gating estruturado por instância — janela, dependências, conditions e recursos que faltam. Mata a pergunta nº1 de quem opera.",
			"inputSchema": schema(map[string]interface{}{"instanceId": strProp("id da instance (ex.: job-2026-06-24)")}, "instanceId"),
			"annotations": readOnly,
		},
		{
			"name":        "blast_radius",
			"description": "Impacto de CANCELAR/segurar um job agora: jobs downstream que deixam de rodar em cascata, SLAs em risco e folders afetadas.",
			"inputSchema": schema(map[string]interface{}{"instanceId": strProp("id da instance")}, "instanceId"),
			"annotations": readOnly,
		},
		{
			"name":        "diff_daily",
			"description": "O que mudou entre duas diárias (default hoje vs a anterior): jobs adicionados/removidos/alterados com diff por-campo (schedule, deps, recursos).",
			"inputSchema": schema(map[string]interface{}{
				"from": strProp("YYYY-MM-DD (default: diária anterior)"), "to": strProp("YYYY-MM-DD (default hoje)"), "folder": strProp("escopa a uma folder/team"),
			}),
			"annotations": readOnly,
		},
		{
			"name":        "dry_run",
			"description": "Simula a daily de uma data futura SEM materializar: quem roda, quem espera (depois de quem) e quem nunca dispara (e por quê).",
			"inputSchema": schema(map[string]interface{}{"date": strProp("YYYY-MM-DD (default amanhã)")}),
			"annotations": readOnly,
		},
		{
			"name":        "job_neighborhood",
			"description": "Grafo LOCAL de um job: ancestrais (de quem depende) e descendentes (quem depende dele) até `radius` saltos, com o status de cada um no dia. Contexto de vizinhança antes de agir.",
			"inputSchema": schema(map[string]interface{}{
				"instanceId": strProp("id da instance"),
				"radius":     map[string]interface{}{"type": "integer", "description": "saltos em cada direção (default 1, máx 4)"},
			}, "instanceId"),
			"annotations": readOnly,
		},
		{
			"name":        "root_cause",
			"description": "Causa raiz de uma falha/bloqueio: sobe a cadeia de upstreams falhos e aponta o job que falhou por conta própria e derrubou o resto. Responde 'por que esse cluster inteiro travou?'.",
			"inputSchema": schema(map[string]interface{}{"instanceId": strProp("id da instance")}, "instanceId"),
			"annotations": readOnly,
		},
		{
			"name":        "event_log",
			"description": "Feed de eventos do dia (cross-instance): ordered/started/finished/retry/cyclic/set-ok/held/…, filtrável por kind, actor, folder ou instance. Timeline operacional / auditoria.",
			"inputSchema": schema(map[string]interface{}{
				"date": strProp("YYYY-MM-DD (default hoje)"), "kind": strProp("lista separada por vírgula"),
				"actor": strProp("scheduler|operator|agent (LIKE)"), "folder": strProp("folder/team exato"),
				"instance": strProp("instance_id exato"),
				"limit":    map[string]interface{}{"type": "integer", "description": "máx de linhas (default 200)"},
			}),
			"annotations": readOnly,
		},
		{
			"name":        "query",
			"description": "Pergunta em linguagem natural sobre o dia (PT/EN) → resposta determinística. Ex.: 'o que falhou hoje na folder PIX', 'quantos rodando', 'resumo do dia'. Devolve a interpretação junto (sem chute).",
			"inputSchema": schema(map[string]interface{}{"q": strProp("a pergunta em texto")}, "q"),
			"annotations": readOnly,
		},
	}
	if m.allowWrites {
		out = append(out,
			map[string]interface{}{
				"name":        "rerun_job",
				"description": "Re-executa um job (volta pra WAITING). AÇÃO DESTRUTIVA — confirme com o operador antes.",
				"inputSchema": schema(map[string]interface{}{"instanceId": strProp("id da instance")}, "instanceId"),
				"annotations": destructive,
			},
			map[string]interface{}{
				"name":        "set_ok",
				"description": "Marca um job NOTOK/CANCELLED como OK (Set OK), destravando sucessores. AÇÃO DESTRUTIVA — confirme com o operador antes.",
				"inputSchema": schema(map[string]interface{}{"instanceId": strProp("id da instance")}, "instanceId"),
				"annotations": destructive,
			},
		)
	}
	return out
}

// ── HTTP helpers ──

func (m *mcpServer) get(path string) (string, error)  { return m.call("GET", path) }
func (m *mcpServer) post(path string) (string, error) { return m.call("POST", path) }

// postJSON envia um corpo JSON (usado pelo tool `query`).
func (m *mcpServer) postJSON(path string, body interface{}) (string, error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", m.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+m.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func (m *mcpServer) call(method, path string) (string, error) {
	req, err := http.NewRequest(method, m.baseURL+path, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+m.token)
	resp, err := m.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return string(body), nil
}

// ── util ──

func (m *mcpServer) ok(id json.RawMessage, result interface{}) rpcResp {
	return rpcResp{JSONRPC: "2.0", ID: id, Result: result}
}
func (m *mcpServer) fail(id json.RawMessage, code int, msg string) rpcResp {
	return rpcResp{JSONRPC: "2.0", ID: id, Error: &rpcErr{Code: code, Message: msg}}
}

// qs monta uma query string só com os pares não-vazios.
func qs(params map[string]string) string {
	v := url.Values{}
	for k, val := range params {
		if val != "" {
			v.Set(k, val)
		}
	}
	if len(v) == 0 {
		return ""
	}
	return "?" + v.Encode()
}

func intArg(args map[string]interface{}, k string) int {
	switch n := args[k].(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
