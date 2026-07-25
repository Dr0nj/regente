// regente-mcp — servidor MCP (Model Context Protocol) do Regente.
//
// Expõe os diferenciais determinísticos (Explain · Diff de Daily · Blast Radius ·
// Dry Run · Forecast) + summary/busca como TOOLS de LEITURA e um conjunto de
// TOOLS de ESCRITA operacional (hold/release/cancel/confirm/rerun/set-ok, force
// order, pause/resume de folder, bulk e ingest de evento) que um agente (Claude
// Desktop, etc.) chama. É uma FACHADA agent-native sobre a API REST — não toca o
// core do servidor. O LLM narra a verdade que o engine computa (não inventa); o
// cliente MCP pede aprovação humana em cada chamada (writes só com -allow-writes,
// marcadas destructiveHint).
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
	allowWrites := flag.Bool("allow-writes", false, "Exposes the write tools (hold/release/cancel/confirm/rerun/set-ok, force_order, pause/resume_folder, bulk_action, ingest_event); the MCP client still asks for per-call approval")
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
			"content": []map[string]interface{}{{"type": "text", "text": "error: " + err.Error()}},
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
			return "", fmt.Errorf("instanceId is required")
		}
		return id, nil
	}

	// gate — nega tools de escrita quando o servidor subiu sem -allow-writes.
	gate := func() error {
		if !m.allowWrites {
			return fmt.Errorf("writes disabled (start regente-mcp with -allow-writes)")
		}
		return nil
	}
	// writeID — boilerplate das ações unitárias por instância (gate + instanceId + POST).
	writeID := func(action string) (string, error) {
		if err := gate(); err != nil {
			return "", err
		}
		id, err := requireID()
		if err != nil {
			return "", err
		}
		return m.post("/api/instances/" + url.PathEscape(id) + "/" + action)
	}

	switch name {
	case "daily_summary":
		return m.get("/api/instances/summary" + qs(map[string]string{"date": str("date")}))
	case "forecast":
		if n := intArg(args, "days"); n > 1 {
			return m.get("/api/forecast/range" + qs(map[string]string{"from": str("date"), "days": strconv.Itoa(n)}))
		}
		return m.get("/api/forecast" + qs(map[string]string{"date": str("date")}))
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
			return "", fmt.Errorf("q (question) is required")
		}
		return m.postJSON("/api/query", map[string]string{"q": q})
	case "diff_daily":
		return m.get("/api/daily/diff" + qs(map[string]string{"from": str("from"), "to": str("to"), "folder": str("folder")}))
	case "dry_run":
		return m.get("/api/daily/dryrun" + qs(map[string]string{"date": str("date")}))
	// ── Escritas unitárias por instância (Control-M operator actions) ──
	case "hold_job":
		return writeID("hold")
	case "release_job":
		return writeID("release")
	case "cancel_job":
		return writeID("cancel")
	case "confirm_job":
		return writeID("confirm")
	case "rerun_job":
		return writeID("rerun")
	case "set_ok":
		return writeID("set-ok")
	// ── Force order (roda uma DEFINITION agora, ignorando deps) ──
	case "force_order":
		if err := gate(); err != nil {
			return "", err
		}
		defID := str("definitionId")
		if defID == "" {
			return "", fmt.Errorf("definitionId is required")
		}
		return m.post("/api/definitions/" + url.PathEscape(defID) + "/force")
	// ── Pausa/resume de WORKFLOW (folder inteira, estado preservado) ──
	case "pause_folder", "resume_folder":
		if err := gate(); err != nil {
			return "", err
		}
		folder := str("folder")
		if folder == "" {
			return "", fmt.Errorf("folder is required")
		}
		verb := "pause"
		if name == "resume_folder" {
			verb = "resume"
		}
		return m.post("/api/folders/" + url.PathEscape(folder) + "/" + verb + qs(map[string]string{"date": str("date")}))
	// ── Ação em LOTE sobre N instâncias (transacional por item no server) ──
	case "bulk_action":
		if err := gate(); err != nil {
			return "", err
		}
		action := str("action")
		ids := strSlice(args, "ids")
		if action == "" || len(ids) == 0 {
			return "", fmt.Errorf("action and ids[] are required")
		}
		return m.postJSON("/api/bulk/instances", map[string]interface{}{"action": action, "ids": ids})
	// ── Ingestão de evento externo (seta conditions e/ou force-ordena, idempotente) ──
	case "ingest_event":
		if err := gate(); err != nil {
			return "", err
		}
		conditions := strSlice(args, "conditions")
		if c := str("condition"); c != "" {
			conditions = append(conditions, c)
		}
		forceJob := str("forceJob")
		if len(conditions) == 0 && forceJob == "" {
			return "", fmt.Errorf("pass conditions[] and/or forceJob")
		}
		body := map[string]interface{}{}
		if len(conditions) > 0 {
			body["conditions"] = conditions
		}
		if forceJob != "" {
			body["forceJob"] = forceJob
		}
		for _, k := range []string{"id", "source", "kind", "date"} {
			if v := str(k); v != "" {
				body[k] = v
			}
		}
		return m.postJSON("/api/events/ingest", body)
	default:
		return "", fmt.Errorf("unknown tool: %q", name)
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
			"description": "Daily summary for a date (default today): total and counts by status and by folder. Use it to get the overview of the day.",
			"inputSchema": schema(map[string]interface{}{"date": strProp("YYYY-MM-DD (default today)")}),
			"annotations": readOnly,
		},
		{
			"name":        "forecast",
			"description": "Schedule forecast (dry-run WITHOUT materializing) using the SAME gating as the daily: who would be ordered on a date, in which topological wave (deps) and the resource peak. With days>1 it forecasts the WINDOW ahead (≥1 week), one report per day.",
			"inputSchema": schema(map[string]interface{}{
				"date": strProp("YYYY-MM-DD (default today; window start when days>1)"),
				"days": map[string]interface{}{"type": "integer", "description": "how many days to forecast from date (default 1; >1 uses /forecast/range)"},
			}),
			"annotations": readOnly,
		},
		{
			"name":        "list_instances",
			"description": "Lists the day's instances with filters (folder, status, id search). Use it to FIND a job's instanceId before explain_job/blast_radius.",
			"inputSchema": schema(map[string]interface{}{
				"date": strProp("YYYY-MM-DD (default today)"), "folder": strProp("exact folder/team"),
				"status": strProp("WAITING|RUNNING|OK|NOTOK|HELD|CANCELLED (comma-separated list)"),
				"q":      strProp("search in id/definition_id"),
				"limit":  map[string]interface{}{"type": "integer", "description": "row cap (default 200)"},
			}),
			"annotations": readOnly,
		},
		{
			"name":        "explain_job",
			"description": "Why a job did (not) run: structured per-instance gating — window, dependencies, conditions and missing resources. Kills the #1 question operators ask.",
			"inputSchema": schema(map[string]interface{}{"instanceId": strProp("the instance id (e.g. job-2026-06-24)")}, "instanceId"),
			"annotations": readOnly,
		},
		{
			"name":        "blast_radius",
			"description": "Impact of CANCELLING/holding a job now: downstream jobs that stop running in cascade, SLAs at risk and affected folders.",
			"inputSchema": schema(map[string]interface{}{"instanceId": strProp("the instance id")}, "instanceId"),
			"annotations": readOnly,
		},
		{
			"name":        "diff_daily",
			"description": "What changed between two dailies (default today vs the previous one): jobs added/removed/changed with a per-field diff (schedule, deps, resources).",
			"inputSchema": schema(map[string]interface{}{
				"from": strProp("YYYY-MM-DD (default: previous daily)"), "to": strProp("YYYY-MM-DD (default today)"), "folder": strProp("scope to a folder/team"),
			}),
			"annotations": readOnly,
		},
		{
			"name":        "dry_run",
			"description": "Simulates the daily for a future date WITHOUT materializing: who runs, who waits (after whom) and who never fires (and why).",
			"inputSchema": schema(map[string]interface{}{"date": strProp("YYYY-MM-DD (default tomorrow)")}),
			"annotations": readOnly,
		},
		{
			"name":        "job_neighborhood",
			"description": "LOCAL graph of a job: ancestors (what it depends on) and descendants (what depends on it) up to `radius` hops, with each one's status for the day. Neighborhood context before acting.",
			"inputSchema": schema(map[string]interface{}{
				"instanceId": strProp("the instance id"),
				"radius":     map[string]interface{}{"type": "integer", "description": "hops in each direction (default 1, max 4)"},
			}, "instanceId"),
			"annotations": readOnly,
		},
		{
			"name":        "root_cause",
			"description": "Root cause of a failure/block: walks up the chain of failed upstreams and points at the job that failed on its own and took down the rest. Answers 'why did this whole cluster stall?'.",
			"inputSchema": schema(map[string]interface{}{"instanceId": strProp("the instance id")}, "instanceId"),
			"annotations": readOnly,
		},
		{
			"name":        "event_log",
			"description": "Feed of the day's events (cross-instance): ordered/started/finished/retry/cyclic/set-ok/held/…, filterable by kind, actor, folder or instance. Operational timeline / audit.",
			"inputSchema": schema(map[string]interface{}{
				"date": strProp("YYYY-MM-DD (default today)"), "kind": strProp("comma-separated list"),
				"actor": strProp("scheduler|operator|agent (LIKE)"), "folder": strProp("exact folder/team"),
				"instance": strProp("exact instance_id"),
				"limit":    map[string]interface{}{"type": "integer", "description": "row cap (default 200)"},
			}),
			"annotations": readOnly,
		},
		{
			"name":        "query",
			"description": "Natural-language question about the day (PT/EN) → deterministic answer. E.g. 'what failed today in folder PIX', 'how many running', 'summary of the day'. Returns its interpretation alongside (no guessing).",
			"inputSchema": schema(map[string]interface{}{"q": strProp("the question as text")}, "q"),
			"annotations": readOnly,
		},
	}
	if m.allowWrites {
		instTool := func(action, desc string) map[string]interface{} {
			return map[string]interface{}{
				"name":        action,
				"description": desc,
				"inputSchema": schema(map[string]interface{}{"instanceId": strProp("the instance id (find it with list_instances)")}, "instanceId"),
				"annotations": destructive,
			}
		}
		out = append(out,
			instTool("hold_job", "Holds a job (Control-M Hold, any status except RUNNING) — becomes HELD; the original status is frozen and release restores it. DESTRUCTIVE ACTION — confirm with the operator first."),
			instTool("release_job", "Releases a HELD job back to the ORIGINAL status frozen by the hold (Control-M Release; legacy holds fall back to WAITING). DESTRUCTIVE ACTION — confirm with the operator first."),
			instTool("cancel_job", "Cancels a job for the day (becomes CANCELLED, terminal). DESTRUCTIVE ACTION — confirm with the operator first."),
			instTool("confirm_job", "Confirms a job waiting at the WAIT_CONFIRM gate (Control-M Confirm; def with confirm:true). DESTRUCTIVE ACTION — confirm with the operator first."),
			instTool("rerun_job", "Reruns a job (back to WAITING). DESTRUCTIVE ACTION — confirm with the operator first."),
			instTool("set_ok", "Marks a NOTOK/CANCELLED job as OK (Set OK), unblocking successors. DESTRUCTIVE ACTION — confirm with the operator first."),
			map[string]interface{}{
				"name":        "force_order",
				"description": "Force Order: orders and runs a DEFINITION NOW, outside the schedule and ignoring deps (Control-M Order/Force). Returns the created instanceId. DESTRUCTIVE ACTION — confirm with the operator first.",
				"inputSchema": schema(map[string]interface{}{"definitionId": strProp("the DEFINITION id (not the instance) — e.g. etl-sales")}, "definitionId"),
				"annotations": destructive,
			},
			map[string]interface{}{
				"name":        "pause_folder",
				"description": "Pauses a whole workflow: ALL of the folder's jobs for the day (any status except RUNNING, carry-over included) become HELD, state preserved (original status frozen; attempts/cycle/scheduled_at intact). DESTRUCTIVE ACTION — confirm with the operator first.",
				"inputSchema": schema(map[string]interface{}{"folder": strProp("exact folder/team"), "date": strProp("YYYY-MM-DD (default today)")}, "folder"),
				"annotations": destructive,
			},
			map[string]interface{}{
				"name":        "resume_folder",
				"description": "Resumes a paused workflow: everything held by the folder pause for the day goes back to the ORIGINAL status (Control-M Release folder). DESTRUCTIVE ACTION — confirm with the operator first.",
				"inputSchema": schema(map[string]interface{}{"folder": strProp("exact folder/team"), "date": strProp("YYYY-MM-DD (default today)")}, "folder"),
				"annotations": destructive,
			},
			map[string]interface{}{
				"name":        "bulk_action",
				"description": "Applies an action to SEVERAL instances at once (transactional PER ITEM on the server — a partial failure is reported item by item, it does not abort the batch). Max 500 ids.",
				"inputSchema": schema(map[string]interface{}{
					"action": strProp("hold|release|cancel|rerun|set-ok|confirm|delete (delete requires the job on HOLD)"),
					"ids":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "instance ids (max 500)"},
				}, "action", "ids"),
				"annotations": destructive,
			},
			map[string]interface{}{
				"name":        "ingest_event",
				"description": "Ingests an EXTERNAL event that unblocks jobs without polling: sets conditions (day scope) and/or forces a job. Idempotent by the emitter's `id` (a retry answers duplicate without re-applying). Pass conditions[] and/or forceJob.",
				"inputSchema": schema(map[string]interface{}{
					"conditions": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "conditions to set (e.g. SALES_FILE_OK)"},
					"condition":  strProp("a single condition (shortcut for conditions[])"),
					"forceJob":   strProp("the definition id to force-order"),
					"id":         strProp("the emitter's dedupe key (optional; without it every call applies)"),
					"source":     strProp("event source (e.g. sap, cdc) — forensic only"),
					"kind":       strProp("event type (e.g. file-arrived) — forensic only"),
					"date":       strProp("YYYY-MM-DD (default today, business tz)"),
				}),
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

// strSlice extrai uma lista de strings de um argumento — aceita array JSON
// (["a","b"]), []string ou uma string separada por vírgula (fallback amigável
// pra clientes que mandam "a,b"). Ignora vazios.
func strSlice(args map[string]interface{}, k string) []string {
	switch v := args[k].(type) {
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	case []string:
		return v
	case string:
		out := []string{}
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return nil
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
