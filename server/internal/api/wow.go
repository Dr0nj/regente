// Package api — Diferenciais §5 "wow" (2026-07-07):
//
//	D-4  GET  /api/analytics/forecast?defId=   previsão de duração por histórico
//	     GET  /api/analytics/durations?date=   p50 por definition (barras da Timeline)
//	D-13 CRUD /api/templates                   templates reutilizáveis de job
//	D-14 GET  /api/selfservice/jobs            jobs expostos ao portal
//	     POST /api/selfservice/run/{defId}     negócio roda job aprovado (gate próprio)
//	D-15 GET/POST /qa/{token}                  quick-action de alerta (página mobile)
package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/quickaction"
	"github.com/go-chi/chi/v5"
)

// ─── D-4 performance forecasting ────────────────────────────────────────────

func (s *server) perfForecast(w http.ResponseWriter, r *http.Request) {
	defID := r.URL.Query().Get("defId")
	if defID == "" {
		http.Error(w, "defId required", http.StatusBadRequest)
		return
	}
	writeJSON(w, 200, s.cfg.Scheduler.PerfForecast(defID))
}

func (s *server) dayDurations(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if date == "" {
		date = s.cfg.Scheduler.TodayDate()
	}
	lookback := 14
	if v := r.URL.Query().Get("lookback"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 90 {
			lookback = n
		}
	}
	writeJSON(w, 200, map[string]any{
		"date": date, "p50ByDef": s.cfg.Scheduler.DayDurations(date, lookback),
	})
}

// ─── D-13 job templates ─────────────────────────────────────────────────────

type jobTemplate struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Definition  domain.JobDefinition `json:"definition"`
	CreatedBy   string               `json:"createdBy,omitempty"`
	CreatedAt   time.Time            `json:"createdAt"`
}

func (s *server) listTemplates(w http.ResponseWriter, r *http.Request) {
	rows, err := s.cfg.DB.Query(
		`SELECT name, description, definition, created_by, created_at FROM job_templates ORDER BY name`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []jobTemplate{}
	for rows.Next() {
		var t jobTemplate
		var defJSON string
		if rows.Scan(&t.Name, &t.Description, &defJSON, &t.CreatedBy, &t.CreatedAt) != nil {
			continue
		}
		if json.Unmarshal([]byte(defJSON), &t.Definition) != nil {
			continue
		}
		out = append(out, t)
	}
	if !rowsOK(w, rows) {
		return
	}
	writeJSON(w, 200, out)
}

// saveTemplate — POST /api/templates {name, description, definition}.
// A definition é validada SEM id/team (o template é a forma, não a instância:
// id/team novos são dados na hora de instanciar no Design).
func (s *server) saveTemplate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string               `json:"name"`
		Description string               `json:"description"`
		Definition  domain.JobDefinition `json:"definition"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if req.Definition.JobType == "" {
		http.Error(w, "definition.jobType required", http.StatusBadRequest)
		return
	}
	// o molde não carrega identidade nem vínculos do job de origem
	req.Definition.ID = ""
	req.Definition.Team = ""
	req.Definition.Upstream = nil
	defJSON, _ := json.Marshal(req.Definition)
	_, err := s.cfg.DB.Exec(
		`INSERT INTO job_templates(name, description, definition, created_by) VALUES(?,?,?,?)
		 ON CONFLICT(name) DO UPDATE SET description=excluded.description, definition=excluded.definition, created_by=excluded.created_by`,
		req.Name, req.Description, string(defJSON), actorFromCtx(r),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, 200, map[string]any{"name": req.Name, "saved": true})
}

func (s *server) deleteTemplate(w http.ResponseWriter, r *http.Request) {
	name := urlName(r, "name")
	res, err := s.cfg.DB.Exec(`DELETE FROM job_templates WHERE name=?`, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── D-14 self-service portal ───────────────────────────────────────────────

// selfServiceJobs — GET /api/selfservice/jobs: as definitions expostas
// (selfService: true) + o estado da execução mais recente de hoje. Qualquer
// usuário LOGADO vê — o portal é para o negócio, o gate é o flag na def.
func (s *server) selfServiceJobs(w http.ResponseWriter, r *http.Request) {
	defs, err := s.cfg.Store.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	today := s.cfg.Scheduler.TodayDate()
	type ssJob struct {
		ID          string     `json:"id"`
		Label       string     `json:"label"`
		Team        string     `json:"team"`
		Description string     `json:"description,omitempty"`
		LastStatus  string     `json:"lastStatus,omitempty"`
		LastRunAt   *time.Time `json:"lastRunAt,omitempty"`
		Running     bool       `json:"running"`
	}
	out := []ssJob{}
	for _, d := range defs {
		if !d.SelfService {
			continue
		}
		j := ssJob{ID: d.ID, Label: d.Label, Team: d.Team, Description: d.Schedule.Description}
		var status string
		var st sql.NullTime
		row := s.cfg.DB.QueryRow(
			`SELECT status, started_at FROM instances
			 WHERE definition_id=? AND order_date=? ORDER BY scheduled_at DESC LIMIT 1`, d.ID, today)
		if row.Scan(&status, &st) == nil {
			j.LastStatus = status
			j.Running = status == string(domain.StatusRunning)
			if st.Valid {
				t := st.Time
				j.LastRunAt = &t
			}
		}
		out = append(out, j)
	}
	writeJSON(w, 200, out)
}

// selfServiceRun — POST /api/selfservice/run/{defId}. Gate PRÓPRIO e deliberado:
// qualquer usuário logado (viewer incluso) pode disparar, DESDE QUE o dono do
// job tenha feito o opt-in explícito (selfService: true no YAML — versionado,
// revisável). É o modelo Control-M Self Service: o negócio roda o que a
// engenharia expôs, sem acesso a Design/Monitoring.
func (s *server) selfServiceRun(w http.ResponseWriter, r *http.Request) {
	defID := chi.URLParam(r, "defId")
	defs, err := s.cfg.Store.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var target *domain.JobDefinition
	for i := range defs {
		if defs[i].ID == defID {
			target = &defs[i]
			break
		}
	}
	if target == nil || !target.SelfService {
		// 404 também para jobs existentes NÃO expostos: o portal não vaza o catálogo.
		http.Error(w, "job not available for self-service", http.StatusNotFound)
		return
	}
	instID, err := s.cfg.Scheduler.ForceOrder(defID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.cfg.Scheduler.EmitEvent(instID, "self-service", actorFromCtx(r), "disparado pelo portal self-service")
	writeJSON(w, 200, map[string]any{"instanceId": instID})
}

// ─── D-15 quick actions (rotas públicas /qa/{token}) ────────────────────────

// quickActionPage — GET /qa/{token}: página de CONFIRMAÇÃO mobile-friendly.
// GET nunca executa (preview de link em chat não pode dar rerun); o botão faz
// o POST. Token inválido/expirado mostra o erro na própria página.
func (s *server) quickActionPage(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	instID, action, err := s.verifyQuickAction(token)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil {
		w.WriteHeader(qaErrStatus(err))
		fmt.Fprint(w, qaHTML("Link inválido", "<p class=err>"+html.EscapeString(qaErrMsg(err))+"</p>", ""))
		return
	}
	var status string
	_ = s.cfg.DB.QueryRow(`SELECT status FROM instances WHERE id=?`, instID).Scan(&status)
	body := fmt.Sprintf(
		"<p>Confirmar <b>%s</b> em</p><p class=job>%s</p><p class=meta>status atual: %s</p>",
		html.EscapeString(action), html.EscapeString(instID), html.EscapeString(status))
	form := `<form method="POST"><button type="submit">Confirmar ` + html.EscapeString(action) + `</button></form>`
	fmt.Fprint(w, qaHTML("Regente — ação rápida", body, form))
}

// quickActionExec — POST /qa/{token}: verifica e EXECUTA (mesma semântica dos
// handlers operacionais, com feedback de rows-affected). actor = "quick-action".
func (s *server) quickActionExec(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	instID, action, err := s.verifyQuickAction(token)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil {
		w.WriteHeader(qaErrStatus(err))
		fmt.Fprint(w, qaHTML("Link inválido", "<p class=err>"+html.EscapeString(qaErrMsg(err))+"</p>", ""))
		return
	}
	status, aerr := s.applyInstanceAction("quick-action", instID, action)
	if aerr != nil {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, qaHTML("Não aplicado", fmt.Sprintf(
			"<p class=err>%s</p><p class=meta>%s</p>",
			html.EscapeString(aerr.Error()), html.EscapeString(instID)), ""))
		return
	}
	if action == "confirm" || action == "release" || action == "rerun" {
		go s.cfg.Scheduler.Tick()
	}
	fmt.Fprint(w, qaHTML("Feito ✓", fmt.Sprintf(
		"<p class=ok>%s aplicado</p><p class=job>%s</p><p class=meta>novo status: %s</p>",
		html.EscapeString(action), html.EscapeString(instID), html.EscapeString(status)), ""))
}

func (s *server) verifyQuickAction(token string) (string, string, error) {
	var secret string
	_ = s.cfg.DB.QueryRow(`SELECT value FROM settings WHERE key='quickaction_secret'`).Scan(&secret)
	if secret == "" {
		return "", "", quickaction.ErrBadToken // nunca emitido → nada a verificar
	}
	return quickaction.Verify(secret, token)
}

func qaErrStatus(err error) int {
	if errors.Is(err, quickaction.ErrExpired) {
		return http.StatusGone
	}
	return http.StatusForbidden
}

func qaErrMsg(err error) string {
	if errors.Is(err, quickaction.ErrExpired) {
		return "Este link expirou. Use a UI do Regente ou aguarde o próximo alerta."
	}
	return "Link inválido."
}

// qaHTML — página mínima mobile-first, self-contained (sem asset externo).
func qaHTML(title, body, form string) string {
	return `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>` + html.EscapeString(title) + `</title><style>
body{font-family:system-ui,sans-serif;background:#0b0f1a;color:#e2e8f0;display:flex;min-height:100vh;align-items:center;justify-content:center;margin:0}
main{max-width:420px;padding:32px;text-align:center}
h1{font-size:18px;color:#94a3b8;margin-bottom:24px}
.job{font-family:ui-monospace,monospace;background:#1e293b;padding:8px 12px;border-radius:8px;word-break:break-all}
.meta{color:#64748b;font-size:13px}.ok{color:#4ade80;font-size:20px}.err{color:#f87171}
button{background:#2563eb;color:#fff;border:0;border-radius:10px;padding:14px 28px;font-size:16px;width:100%;margin-top:16px}
button:active{background:#1d4ed8}</style><main><h1>` + html.EscapeString(title) + `</h1>` + body + form + `</main>`
}
