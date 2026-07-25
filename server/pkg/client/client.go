// Package client — ADV-6: SDK Go do Regente.
//
// Fachada HAND-WRITTEN da superfície de integração CURADA (a mesma ~dúzia de
// rotas que um sistema externo realmente chama — ver roadmap API-1), não um
// wrapper dos ~137 handlers internos da SPA: query composta de instances,
// ciclo de vida (hold/release/cancel/rerun/set-ok/confirm), Force Order,
// ingest idempotente de eventos externos (D-3), status/relatório da daily,
// catálogo de jobTypes (ADV-1) e archives (ADV-5). O CLI `regente ops` é
// construído 100% sobre este pacote — o SDK é a fonte única do transporte.
//
//	cli := client.New("http://localhost:8080", os.Getenv("REGENTE_TOKEN"))
//	res, err := cli.QueryInstances(client.Query{Statuses: []string{"NOTOK"}})
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client — conexão com um server Regente. Zero estado além disso.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New cria um Client com timeout são. baseURL sem barra final.
func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Instance — espelho do JSON servido pela API (instances.go instanceRow).
type Instance struct {
	ID           string     `json:"id"`
	DefinitionID string     `json:"definitionId"`
	Team         string     `json:"team,omitempty"`
	OrderDate    string     `json:"orderDate"`
	Status       string     `json:"status"`
	ScheduledAt  time.Time  `json:"scheduledAt"`
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	FinishedAt   *time.Time `json:"finishedAt,omitempty"`
	AgentID      string     `json:"agentId,omitempty"`
	ExitCode     int        `json:"exitCode,omitempty"`
	Output       string     `json:"output,omitempty"`
	Forced       bool       `json:"forced,omitempty"`
	CarriedFrom  string     `json:"carriedFrom,omitempty"`
	Confirmed    bool       `json:"confirmed,omitempty"`
	CycleRuns    int        `json:"cycleRuns,omitempty"`
	DryRun       bool       `json:"dryRun,omitempty"`
}

// Query — espelho do corpo tipado de POST /api/instances/query (D-5).
// Tudo opcional; zero-value = a diária de hoje, sem filtro.
type Query struct {
	Date        string   `json:"date,omitempty"`
	From        string   `json:"from,omitempty"`
	To          string   `json:"to,omitempty"`
	Folders     []string `json:"folders,omitempty"`
	Statuses    []string `json:"statuses,omitempty"`
	Search      string   `json:"search,omitempty"`
	DefIDPrefix string   `json:"defIdPrefix,omitempty"`
	Forced      *bool    `json:"forced,omitempty"`
	Carried     *bool    `json:"carried,omitempty"`
	Late        *bool    `json:"late,omitempty"`
	GroupBy     string   `json:"groupBy,omitempty"`
	Limit       int      `json:"limit,omitempty"`
	Cursor      string   `json:"cursor,omitempty"`
}

// QueryResult — lista paginada OU agregação, conforme GroupBy.
type QueryResult struct {
	Items      []Instance `json:"items"`
	NextCursor string     `json:"nextCursor"`
	GroupBy    string     `json:"groupBy"`
	Groups     []Group    `json:"groups"`
	Total      int        `json:"total"`
}

type Group struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// QueryInstances — POST /api/instances/query (query composta e tipada).
func (c *Client) QueryInstances(q Query) (*QueryResult, error) {
	var out QueryResult
	if err := c.do(http.MethodPost, "/api/instances/query", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Actions aceitas por Action — o ciclo de vida operacional de uma instance.
var instanceActions = map[string]bool{
	"hold": true, "release": true, "cancel": true, "rerun": true,
	"set-ok": true, "confirm": true,
}

// Action — POST /api/instances/{id}/{action}.
func (c *Client) Action(instanceID, action string) error {
	if !instanceActions[action] {
		return fmt.Errorf("invalid action %q (hold|release|cancel|rerun|set-ok|confirm)", action)
	}
	return c.do(http.MethodPost, "/api/instances/"+url.PathEscape(instanceID)+"/"+action, nil, nil)
}

// ForceOrder — POST /api/definitions/{id}/force (Control-M "Order Force":
// materializa a def PUBLICADA mais atual na diária de hoje).
func (c *Client) ForceOrder(definitionID string) error {
	return c.do(http.MethodPost, "/api/definitions/"+url.PathEscape(definitionID)+"/force", nil, nil)
}

// IngestEvent — corpo de POST /api/events/ingest (D-3: idempotente pela PK do
// emissor source+id; re-enviar o mesmo evento é no-op seguro).
type IngestEvent struct {
	ID         string   `json:"id"`
	Source     string   `json:"source"`
	Kind       string   `json:"kind,omitempty"`
	Condition  string   `json:"condition,omitempty"`
	Conditions []string `json:"conditions,omitempty"`
	ForceJob   string   `json:"forceJob,omitempty"`
	Date       string   `json:"date,omitempty"`
}

// IngestResult — o que o server fez com o evento ("applied" explica).
type IngestResult struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	Kind       string    `json:"kind"`
	Applied    string    `json:"applied"`
	ReceivedAt time.Time `json:"receivedAt"`
}

// Ingest — POST /api/events/ingest.
func (c *Client) Ingest(ev IngestEvent) (*IngestResult, error) {
	var out IngestResult
	if err := c.do(http.MethodPost, "/api/events/ingest", ev, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DailyStatus — GET /api/daily/status.
type DailyStatus struct {
	OrderDate   string `json:"orderDate"`
	DailyAt     string `json:"dailyAt"`
	Timezone    string `json:"timezone"`
	LastRunDate string `json:"lastRunDate"`
	LastRunAt   string `json:"lastRunAt"`
	ServerNow   string `json:"serverNow"`
}

func (c *Client) DailyStatus() (*DailyStatus, error) {
	var out DailyStatus
	if err := c.do(http.MethodGet, "/api/daily/status", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DailyReport — GET /api/daily/report (E5). O shape do relatório evolui com o
// produto; o SDK entrega o JSON cru decodificável pelo consumidor.
func (c *Client) DailyReport(date string) (json.RawMessage, error) {
	p := "/api/daily/report"
	if date != "" {
		p += "?date=" + url.QueryEscape(date)
	}
	var out json.RawMessage
	if err := c.do(http.MethodGet, p, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// JobTypes — GET /api/jobtypes (ADV-1: catálogo declarativo do registry, a
// base documental p/ SDK/docs). JSON cru: o schema é dirigido pelo server.
func (c *Client) JobTypes() (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(http.MethodGet, "/api/jobtypes", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Archive — item de GET /api/archive (ADV-5).
type Archive struct {
	File       string `json:"file"`
	Day        string `json:"day"`
	SizeBytes  int64  `json:"sizeBytes"`
	ModifiedAt string `json:"modifiedAt"`
}

// Archives — GET /api/archive (admin-only no server).
func (c *Client) Archives() ([]Archive, error) {
	var out struct {
		Archives []Archive `json:"archives"`
	}
	if err := c.do(http.MethodGet, "/api/archive", nil, &out); err != nil {
		return nil, err
	}
	return out.Archives, nil
}

// DownloadArchive — GET /api/archive/{file}, streaming pro writer.
func (c *Client) DownloadArchive(file string, w io.Writer) error {
	resp, err := c.raw(http.MethodGet, "/api/archive/"+url.PathEscape(file), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return httpError(resp)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// Health — GET /health (líder, versão, uptime...). JSON cru.
func (c *Client) Health() (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(http.MethodGet, "/health", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Get — escape hatch tipado pra rotas read-only fora da fachada curada
// (ex.: /api/instances/{id}/explain). Mantém o SDK pequeno sem fechar portas.
func (c *Client) Get(path string, out any) error {
	return c.do(http.MethodGet, path, nil, out)
}

// ─── transporte ──────────────────────────────────────────────────────────────

func (c *Client) raw(method, path string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, r)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	httpc := c.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 30 * time.Second}
	}
	return httpc.Do(req)
}

func (c *Client) do(method, path string, body, out any) error {
	resp, err := c.raw(method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpError(resp)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func httpError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	msg := strings.TrimSpace(string(b))
	if msg == "" {
		msg = resp.Status
	}
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
}
