// Package serveragent — SERVER-AGENT: o agente padrão EMBUTIDO no processo do
// server. Todo server nasce com um agente capaz de executar chamadas de API
// (HTTP/REST) sem nenhum regente-agent externo instalado — quem quer disparar
// uma API pode rodar direto do SERVER-AGENT.
//
// Ele se registra no hub como um agente NORMAL (id "SERVER-AGENT"): aparece na
// tela de Agentes, é roteável por capability e pode ser PINADO no Design como
// qualquer outro. O "transporte" é um canal em memória: o Dispatch do hub
// escreve no Send e a goroutine daqui consome, executa e devolve o resultado
// pelo FinishInstance (mesmo contrato do ws handler de um agente real).
//
// Capabilities deliberadamente SÓ HTTP/REST: rodar COMMAND/SCRIPT no host do
// control plane por padrão seria um vazamento de privilégio — para isso existe
// o regente-agent de verdade (inclusive sandboxado, ver deploy/vps).
package serveragent

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
)

// ID — nome fixo do agente embutido (visível na tela de Agentes e no pin do Design).
const ID = "SERVER-AGENT"

// Finisher — devolve o resultado ao scheduler (mesmo papel do ws handler).
type Finisher func(instanceID string, status domain.InstanceStatus, exitCode int, output string)

// dispatchMsg — payload que o scheduler envia a qualquer agente.
type dispatchMsg struct {
	Event      string                 `json:"event"`
	InstanceID string                 `json:"instanceId"`
	JobType    string                 `json:"jobType"`
	Params     map[string]interface{} `json:"params"`
	Timeout    int                    `json:"timeout"`
}

// Start registra o SERVER-AGENT no hub (dispatch) E na tabela `agents` (a tela
// de Agentes e o seletor de pin do Design leem de lá) e sobe a goroutine executora.
func Start(h *hub.Hub, database *db.DB, finish Finisher) *hub.Client {
	host, _ := os.Hostname()
	c := &hub.Client{
		ID:           ID,
		Kind:         hub.ClientAgent,
		Send:         make(chan []byte, 64),
		Capabilities: []string{"HTTP", "REST"},
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		Host:         host,
		Version:      "embedded",
		Started:      time.Now().Format(time.RFC3339),
	}
	h.Register(c)
	upsertAgentRow(database, c)
	go func() {
		for raw := range c.Send {
			var msg dispatchMsg
			if err := json.Unmarshal(raw, &msg); err != nil || msg.Event != "dispatch" || msg.InstanceID == "" {
				continue
			}
			go func(m dispatchMsg) {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[server-agent] PANIC em %s recuperado: %v", m.InstanceID, r)
						finish(m.InstanceID, domain.StatusNotOK, -1, fmt.Sprintf("(panic no SERVER-AGENT: %v)", r))
					}
				}()
				h.Touch(ID)
				code, out := runHTTP(m.Params, m.Timeout)
				st := domain.StatusOK
				if code != 0 {
					st = domain.StatusNotOK
				}
				finish(m.InstanceID, st, code, out)
			}(msg)
		}
	}()
	return c
}

// upsertAgentRow — o mesmo upsert que o handshake do /ws/agent faz para agentes
// externos (recordAgentConnect): sem a linha na tabela `agents`, o GET /api/agents
// não lista o SERVER-AGENT (a presença/online vem do hub, mas a listagem é do DB).
func upsertAgentRow(database *db.DB, c *hub.Client) {
	if database == nil {
		return
	}
	caps := strings.Join(c.Capabilities, ",")
	res, err := database.Exec(
		`UPDATE agents SET os=?, arch=?, host=?, version=?, capabilities=?, started_at=CURRENT_TIMESTAMP,
		        connected_at=CURRENT_TIMESTAMP, last_seen_at=CURRENT_TIMESTAMP WHERE id=?`,
		c.OS, c.Arch, c.Host, c.Version, caps, c.ID,
	)
	if err != nil {
		log.Printf("[server-agent] upsert agents row: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := database.Exec(
			`INSERT INTO agents(id, os, arch, host, version, capabilities, started_at, connected_at, first_seen, last_seen_at, online)
			 VALUES(?,?,?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,0)`,
			c.ID, c.OS, c.Arch, c.Host, c.Version, caps,
		); err != nil {
			log.Printf("[server-agent] insert agents row: %v", err)
		}
	}
}

// runHTTP — espelho fiel do runREST do regente-agent (method/url/headers/body/
// expectStatus, inclusive o expectStatus escalar/CSV/lista do schema ADV-1),
// para o job HTTP se comportar IGUAL rodando no agente externo ou no embutido.
func runHTTP(params map[string]interface{}, timeoutSec int) (int, string) {
	method, _ := params["method"].(string)
	if method == "" {
		method = "GET"
	}
	urlStr, _ := params["url"].(string)
	if urlStr == "" {
		return -1, "missing 'url' param"
	}
	var body io.Reader
	if b, ok := params["body"].(string); ok && b != "" {
		body = strings.NewReader(b)
	}
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	req, err := http.NewRequest(strings.ToUpper(method), urlStr, body)
	if err != nil {
		return -1, err.Error()
	}
	if headers, ok := params["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			if sv, ok := v.(string); ok {
				req.Header.Set(k, sv)
			}
		}
	}
	res, err := client.Do(req)
	if err != nil {
		return -1, err.Error()
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	code := 0
	if res.StatusCode >= 400 {
		code = res.StatusCode
	}
	exp, _ := params["expectStatus"].([]interface{})
	if n, ok := toInt(params["expectStatus"]); ok {
		exp = []interface{}{n}
	}
	if s, ok := params["expectStatus"].(string); ok {
		for _, part := range strings.Split(s, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
				exp = append(exp, n)
			}
		}
	}
	if len(exp) > 0 {
		match := false
		for _, e := range exp {
			if n, ok := toInt(e); ok && n == res.StatusCode {
				match = true
				break
			}
		}
		if !match {
			code = res.StatusCode
			if code == 0 {
				code = -1
			}
		} else {
			code = 0
		}
	}
	return code, fmt.Sprintf("HTTP %d\n%s", res.StatusCode, string(raw))
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i), true
		}
	}
	return 0, false
}
