package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/execution"
)

// Administração LOCAL — socket Unix 0600 no diretório de dados.
//
// Por que existe (VPS, 14/11/2026): 4 execuções UNCERTAIN seguraram os slots do
// único agente por dias. A resolução só existia pela API com sessão de escritor,
// e quem estava na máquina (root, sem senha da UI à mão) não tinha como agir sem
// editar o banco — o que quebraria a trilha de auditoria assinada.
//
// O socket não é uma porta: só quem já tem acesso de escrita ao diretório de
// dados (o usuário do serviço e o root) consegue conectar — exatamente quem já
// pode trocar o banco inteiro. A decisão passa pelo MESMO Engine.Resolve da API:
// auditada (ator "local-admin"), projetada na instance e transmitida à UI.

// LocalAdminActor é o ator gravado na auditoria e em resolved_by.
const LocalAdminActor = "local-admin"

// LocalResolveRequest é o corpo de POST /executions/resolve-uncertain.
type LocalResolveRequest struct {
	Decision   string   `json:"decision"`   // failed | cancelled | succeeded
	Reason     string   `json:"reason"`     // evidência; obrigatória
	Agent      string   `json:"agent"`      // opcional: só as deste agente
	Executions []string `json:"executions"` // opcional: só estes execution IDs
	DryRun     bool     `json:"dryRun"`     // lista sem gravar
}

// LocalResolveItem é uma execução considerada pelo comando.
type LocalResolveItem struct {
	ExecutionID string `json:"executionId"`
	InstanceID  string `json:"instanceId"`
	AgentID     string `json:"agentId"`
	State       string `json:"state"`
	Error       string `json:"error,omitempty"`
}

// LocalResolveResponse separa o que foi gravado do que foi recusado.
type LocalResolveResponse struct {
	Decision string             `json:"decision"`
	DryRun   bool               `json:"dryRun"`
	Resolved []LocalResolveItem `json:"resolved"`
	Failed   []LocalResolveItem `json:"failed"`
}

// LocalAdminHandler devolve o handler servido no socket local. engine é lido a
// cada request porque o motor durável é anexado depois da criação do router.
func LocalAdminHandler(database *db.DB, engine func() *execution.Engine) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/executions/resolve-uncertain", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		base := engine()
		if base == nil {
			http.Error(w, "durable execution is not enabled on this server", http.StatusConflict)
			return
		}
		var req LocalResolveRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		req.Reason = strings.TrimSpace(req.Reason)
		if req.Decision == "" {
			req.Decision = "failed"
		}
		if req.Decision != "failed" && req.Decision != "cancelled" && req.Decision != "succeeded" {
			http.Error(w, "decision must be failed, cancelled or succeeded", http.StatusBadRequest)
			return
		}
		if !req.DryRun && (req.Reason == "" || len(req.Reason) > 2000) {
			http.Error(w, "reason is required (up to 2000 characters)", http.StatusBadRequest)
			return
		}
		items, err := unresolvedExecutions(database, req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := LocalResolveResponse{Decision: req.Decision, DryRun: req.DryRun, Resolved: []LocalResolveItem{}, Failed: []LocalResolveItem{}}
		if req.DryRun {
			out.Resolved = items
			writeJSON(w, 200, out)
			return
		}
		requestID := make([]byte, 16)
		if _, err := rand.Read(requestID); err != nil {
			http.Error(w, "request identity unavailable", http.StatusServiceUnavailable)
			return
		}
		// Mesma costura do scopedAudit: cópia do motor com a identidade de auditoria
		// deste pedido. Notify/Transition vêm junto, então a instance e a UI mudam.
		scoped := *base
		scoped.DB = database.WithAuditIdentity(db.AuditIdentity{Request: hex.EncodeToString(requestID), Actor: LocalAdminActor, Route: "LOCAL /executions/resolve-uncertain", IP: "local-socket"})
		for _, it := range items {
			a, err := scoped.Attempt(it.ExecutionID)
			if err == nil {
				key := make([]byte, 16)
				if _, err = rand.Read(key); err == nil {
					_, err = scoped.Resolve(it.ExecutionID, a.Fence, "local-"+hex.EncodeToString(key), LocalAdminActor, req.Decision, req.Reason, true)
				}
			}
			if err != nil {
				it.Error = err.Error()
				out.Failed = append(out.Failed, it)
				continue
			}
			out.Resolved = append(out.Resolved, it)
		}
		writeJSON(w, 200, out)
	})
	return mux
}

// unresolvedExecutions lista as tentativas que seguram slot sem desfecho, das
// ordens de runtime (as de laboratório não pertencem ao operador).
func unresolvedExecutions(database *db.DB, req LocalResolveRequest) ([]LocalResolveItem, error) {
	rows, err := database.Query(`SELECT a.execution_id,o.source_instance_id,a.agent_id,a.state
		FROM execution_attempts a JOIN lab_orders o ON o.id=a.order_id
		WHERE a.state IN ('uncertain','cancel_requested') AND o.runtime=1
		ORDER BY a.created_at,a.execution_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	want := map[string]bool{}
	for _, id := range req.Executions {
		want[id] = true
	}
	items := []LocalResolveItem{}
	for rows.Next() {
		var it LocalResolveItem
		if err := rows.Scan(&it.ExecutionID, &it.InstanceID, &it.AgentID, &it.State); err != nil {
			return nil, err
		}
		if (req.Agent != "" && it.AgentID != req.Agent) || (len(want) > 0 && !want[it.ExecutionID]) {
			continue
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// ServeLocalAdmin escuta no socket até ctx terminar. Socket velho de um processo
// morto é removido; um socket VIVO (outro nó no mesmo diretório) é respeitado.
func ServeLocalAdmin(ctx context.Context, path string, handler http.Handler) error {
	if conn, err := net.DialTimeout("unix", path, 500*time.Millisecond); err == nil {
		conn.Close()
		return fmt.Errorf("local admin socket %s is already served by another process", path)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	if err := os.Chmod(path, 0600); err != nil {
		ln.Close()
		return err
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
		_ = os.Remove(path)
	}()
	go func() { _ = srv.Serve(ln) }()
	return nil
}

// LocalAdminResolve é o lado cliente: fala com o server vivo pelo socket.
func LocalAdminResolve(path string, req LocalResolveRequest) (LocalResolveResponse, error) {
	var out LocalResolveResponse
	client := &http.Client{Timeout: 2 * time.Minute, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", path)
	}}}
	body, err := json.Marshal(req)
	if err != nil {
		return out, err
	}
	res, err := client.Post("http://local-admin/executions/resolve-uncertain", "application/json", strings.NewReader(string(body)))
	if err != nil {
		return out, fmt.Errorf("cannot reach the running server on %s (is it running, and are you root or the service user?): %w", path, err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		raw := make([]byte, 2048)
		n, _ := res.Body.Read(raw)
		return out, fmt.Errorf("server refused: %s", strings.TrimSpace(string(raw[:n])))
	}
	return out, json.NewDecoder(res.Body).Decode(&out)
}
