package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/go-chi/chi/v5"
)

type instanceRow struct {
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
	CarriedFrom  string     `json:"carriedFrom,omitempty"` // ciclo de vida da daily: dia de origem se foi carregada da diária anterior.
}

const instanceCols = `id, definition_id, COALESCE(team,''), order_date, status, scheduled_at,
	started_at, finished_at,
	COALESCE(agent_id,''), COALESCE(exit_code,0), COALESCE(output,''), COALESCE(forced,0),
	COALESCE(carried_from,'')`

func scanInstances(rows *sql.Rows) []instanceRow {
	out := []instanceRow{}
	for rows.Next() {
		var ir instanceRow
		var startedAt, finishedAt sql.NullTime
		var forcedInt int
		if err := rows.Scan(
			&ir.ID, &ir.DefinitionID, &ir.Team, &ir.OrderDate, &ir.Status, &ir.ScheduledAt,
			&startedAt, &finishedAt,
			&ir.AgentID, &ir.ExitCode, &ir.Output, &forcedInt,
			&ir.CarriedFrom,
		); err != nil {
			continue
		}
		if startedAt.Valid {
			t := startedAt.Time
			ir.StartedAt = &t
		}
		if finishedAt.Valid {
			t := finishedAt.Time
			ir.FinishedAt = &t
		}
		ir.Forced = forcedInt == 1
		out = append(out, ir)
	}
	return out
}

// instanceQuery — P2/escala: filtros server-side de /api/instances*. Tudo opcional
// menos a data. Permite filtrar/paginar/contar NO BANCO em vez de baixar o dia
// inteiro e filtrar no cliente (que não escala a 100k+).
type instanceQuery struct {
	date     string
	folder   string   // team exato (a "folder" do Control-M)
	statuses []string // OR entre status
	search   string   // LIKE em id / definition_id
}

func parseInstanceQuery(r *http.Request) instanceQuery {
	q := instanceQuery{
		date:   r.URL.Query().Get("date"),
		folder: r.URL.Query().Get("folder"),
		search: strings.TrimSpace(r.URL.Query().Get("q")),
	}
	if q.date == "" {
		q.date = time.Now().Format("2006-01-02")
	}
	if st := r.URL.Query().Get("status"); st != "" {
		for _, s := range strings.Split(st, ",") {
			if s = strings.TrimSpace(s); s != "" {
				q.statuses = append(q.statuses, s)
			}
		}
	}
	return q
}

// where monta o WHERE + args do filtro, já incorporando a restrição de RBAC por
// CONJUNTO de folders legíveis (não por linha). restrict=true + allowed vazio =
// "não vê nada" (1=0).
func (q instanceQuery) where(allowed []string, restrict bool) (string, []any) {
	clauses := []string{"order_date=?"}
	args := []any{q.date}
	if q.folder != "" {
		clauses = append(clauses, "team=?")
		args = append(args, q.folder)
	}
	if len(q.statuses) > 0 {
		clauses = append(clauses, "status IN ("+placeholders(len(q.statuses))+")")
		for _, st := range q.statuses {
			args = append(args, st)
		}
	}
	if q.search != "" {
		clauses = append(clauses, "(id LIKE ? OR definition_id LIKE ?)")
		like := "%" + q.search + "%"
		args = append(args, like, like)
	}
	if restrict {
		if len(allowed) == 0 {
			clauses = append(clauses, "1=0")
		} else {
			clauses = append(clauses, "team IN ("+placeholders(len(allowed))+")")
			for _, t := range allowed {
				args = append(args, t)
			}
		}
	}
	return strings.Join(clauses, " AND "), args
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func parseLimit(r *http.Request, def int) int {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	if n > 5000 { // teto de segurança por request
		n = 5000
	}
	return n
}

// allowedTeams resolve, SEM O(N) por linha, quais folders o user pode ler nesta
// data. restrict=false → admin/sem-auth (sem filtro). restrict=true → o caller
// adiciona team IN(teams). Custo = O(folders distintas), não O(instances).
func (s *server) allowedTeams(r *http.Request, date string) (teams []string, restrict bool) {
	u, ok := auth.FromContext(r.Context())
	if !ok || u == nil || u.Role == auth.RoleAdmin {
		return nil, false
	}
	var distinct []string
	if rows, err := s.cfg.DB.Query("SELECT DISTINCT team FROM instances WHERE order_date=?", date); err == nil {
		for rows.Next() {
			var t string
			if rows.Scan(&t) == nil && t != "" {
				distinct = append(distinct, t)
			}
		}
		rows.Close()
	}
	readable, _ := auth.FilterReadableFolders(s.cfg.DB, u, distinct)
	return readable, true
}

// listInstances — GET /api/instances. Mantém o contrato (array) por compat, mas
// agora filtra/limita NO BANCO (folder, status, q, limit) e aplica RBAC por
// conjunto. Sem filtro/limit = comportamento de antes (dia inteiro).
func (s *server) listInstances(w http.ResponseWriter, r *http.Request) {
	q := parseInstanceQuery(r)
	allowed, restrict := s.allowedTeams(r, q.date)
	where, args := q.where(allowed, restrict)

	sqlStr := `SELECT ` + instanceCols + ` FROM instances WHERE ` + where + ` ORDER BY scheduled_at`
	if lim := parseLimit(r, 0); lim > 0 {
		sqlStr += " LIMIT ?"
		args = append(args, lim)
	}
	rows, err := s.cfg.DB.Query(sqlStr, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	writeJSON(w, 200, scanInstances(rows))
}

// pageInstances — GET /api/instances/page. Paginação por CURSOR (keyset estável em
// scheduled_at,id). Resposta {items, nextCursor}; nextCursor vazio = fim. O cursor
// é opaco (o id da última linha); a comparação keyset usa subqueries por PK (sem
// re-bindar timestamp, dialect-safe). É o caminho de escala: o front carrega o
// working set em páginas em vez de baixar o dia inteiro.
func (s *server) pageInstances(w http.ResponseWriter, r *http.Request) {
	q := parseInstanceQuery(r)
	allowed, restrict := s.allowedTeams(r, q.date)
	where, args := q.where(allowed, restrict)
	limit := parseLimit(r, 500)
	if limit == 0 {
		limit = 500
	}
	if cur := r.URL.Query().Get("cursor"); cur != "" {
		where += ` AND (scheduled_at > (SELECT scheduled_at FROM instances WHERE id=?)
		           OR (scheduled_at = (SELECT scheduled_at FROM instances WHERE id=?) AND id > ?))`
		args = append(args, cur, cur, cur)
	}
	sqlStr := `SELECT ` + instanceCols + ` FROM instances WHERE ` + where + ` ORDER BY scheduled_at, id LIMIT ?`
	args = append(args, limit+1) // +1 para saber se há próxima página
	rows, err := s.cfg.DB.Query(sqlStr, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	items := scanInstances(rows)
	next := ""
	if len(items) > limit {
		next = items[limit-1].ID
		items = items[:limit]
	}
	writeJSON(w, 200, map[string]any{"items": items, "nextCursor": next})
}

// summaryInstances — GET /api/instances/summary. Contadores agregados (GROUP BY),
// baratos mesmo com 1M instances (índice em order_date). {total, byStatus, byFolder}
// alimenta o dashboard sem baixar uma linha sequer. Respeita os mesmos filtros/RBAC.
func (s *server) summaryInstances(w http.ResponseWriter, r *http.Request) {
	q := parseInstanceQuery(r)
	allowed, restrict := s.allowedTeams(r, q.date)
	where, args := q.where(allowed, restrict)

	byStatus := map[string]int{}
	total := 0
	if rows, err := s.cfg.DB.Query(`SELECT status, COUNT(*) FROM instances WHERE `+where+` GROUP BY status`, args...); err == nil {
		for rows.Next() {
			var st string
			var n int
			if rows.Scan(&st, &n) == nil {
				byStatus[st] = n
				total += n
			}
		}
		rows.Close()
	}
	byFolder := map[string]int{}
	if rows, err := s.cfg.DB.Query(`SELECT team, COUNT(*) FROM instances WHERE `+where+` GROUP BY team`, args...); err == nil {
		for rows.Next() {
			var t string
			var n int
			if rows.Scan(&t, &n) == nil {
				byFolder[t] = n
			}
		}
		rows.Close()
	}
	writeJSON(w, 200, map[string]any{"date": q.date, "total": total, "byStatus": byStatus, "byFolder": byFolder})
}

func (s *server) holdInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.requireInstanceWrite(w, r, id) {
		return
	}
	_, err := s.cfg.DB.Exec(`UPDATE instances SET status=? WHERE id=? AND status=?`,
		string(domain.StatusHeld), id, string(domain.StatusWaiting))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cfg.Scheduler.EmitEvent(id, "held", "operator", "")
	s.cfg.Hub.BroadcastWeb("instance.changed", map[string]string{"id": id, "status": string(domain.StatusHeld)})
	writeJSON(w, 200, map[string]string{"id": id, "status": string(domain.StatusHeld)})
}

func (s *server) releaseInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.requireInstanceWrite(w, r, id) {
		return
	}
	_, err := s.cfg.DB.Exec(`UPDATE instances SET status=? WHERE id=? AND status=?`,
		string(domain.StatusWaiting), id, string(domain.StatusHeld))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cfg.Scheduler.EmitEvent(id, "released", "operator", "")
	s.cfg.Hub.BroadcastWeb("instance.changed", map[string]string{"id": id, "status": string(domain.StatusWaiting)})
	writeJSON(w, 200, map[string]string{"id": id, "status": string(domain.StatusWaiting)})
}

func (s *server) cancelInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.requireInstanceWrite(w, r, id) {
		return
	}
	_, err := s.cfg.DB.Exec(`UPDATE instances SET status=?, finished_at=CURRENT_TIMESTAMP WHERE id=?`,
		string(domain.StatusCancelled), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cfg.Scheduler.EmitEvent(id, "cancelled", "operator", "manual cancel")
	s.cfg.Hub.BroadcastWeb("instance.changed", map[string]string{"id": id, "status": string(domain.StatusCancelled)})
	writeJSON(w, 200, map[string]string{"id": id, "status": string(domain.StatusCancelled)})
}

func (s *server) rerunInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.requireInstanceWrite(w, r, id) {
		return
	}
	_, err := s.cfg.DB.Exec(
		`UPDATE instances SET status=?, started_at=NULL, finished_at=NULL, exit_code=NULL, output=NULL WHERE id=?`,
		string(domain.StatusWaiting), id,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cfg.Scheduler.EmitEvent(id, "rerun", "operator", "reset to WAITING")
	// Ciclo de vida do alerta: o operador agiu no job → marca alertas como tratados.
	s.markAlertsHandled(id, "rerun")
	s.cfg.Hub.BroadcastWeb("instance.changed", map[string]string{"id": id, "status": string(domain.StatusWaiting)})
	writeJSON(w, 200, map[string]string{"id": id, "status": string(domain.StatusWaiting)})
}

// setOKInstance — Control-M "Set OK": flip NOTOK/CANCELLED -> OK,
// destrava sucessores on-success.
func (s *server) setOKInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.requireInstanceWrite(w, r, id) {
		return
	}
	if err := s.cfg.Scheduler.SetOK(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Ciclo de vida do alerta: set OK trata os alertas pendentes do job.
	s.markAlertsHandled(id, "set_ok")
	writeJSON(w, 200, map[string]string{"id": id, "status": string(domain.StatusOK)})
}

func (s *server) runDaily(w http.ResponseWriter, r *http.Request) {
	today := time.Now().Format("2006-01-02")
	created := s.cfg.Scheduler.RunDaily(today)
	writeJSON(w, 200, map[string]interface{}{"orderDate": today, "created": created})
}

// diffDaily — Diferencial "o que mudou entre duas diárias?". Default: to=hoje,
// from=a diária anterior. ?folder= escopa a uma folder/team. Lê só os snapshots
// congelados (DNA Git-native) — diff exato, sem reprocessar Git.
func (s *server) diffDaily(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	to := q.Get("to")
	if to == "" {
		to = time.Now().Format("2006-01-02")
	}
	from := q.Get("from")
	if from == "" {
		from = s.cfg.Scheduler.PrevDailyDate(to)
	}
	if from == "" {
		http.Error(w, "sem diária anterior para comparar (passe ?from=YYYY-MM-DD)", http.StatusBadRequest)
		return
	}
	diff, err := s.cfg.Scheduler.DiffDaily(from, to, q.Get("folder"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, 200, diff)
}

// schedulerTick — Fase 1 (serverless): dispara um ciclo de scheduling sob
// demanda. Pensado para -scheduler=external, onde um cron externo (Cloud
// Scheduler, k8s CronJob, GitHub Actions) bate aqui em vez do ticker interno.
// Idempotente e leader-guarded; seguro chamar com frequência.
func (s *server) schedulerTick(w http.ResponseWriter, r *http.Request) {
	s.cfg.Scheduler.Tick()
	writeJSON(w, 200, map[string]interface{}{"ok": true, "at": time.Now().UTC().Format(time.RFC3339)})
}

func (s *server) forceOrder(w http.ResponseWriter, r *http.Request) {
	defID := chi.URLParam(r, "id")
	// F11.10b — precisa de write na folder da definition.
	defs, err := s.cfg.Store.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var team string
	for _, d := range defs {
		if d.ID == defID {
			team = d.Team
			break
		}
	}
	if team == "" {
		http.Error(w, "definition not found", http.StatusNotFound)
		return
	}
	if !s.requireFolderWrite(w, r, team) {
		return
	}
	id, err := s.cfg.Scheduler.ForceOrder(defID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, 200, map[string]string{"instanceId": id})
}

func (s *server) listAgents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.cfg.Hub.OnlineAgents())
}

type instanceEvent struct {
	ID         int64     `json:"id"`
	InstanceID string    `json:"instanceId"`
	TS         time.Time `json:"ts"`
	Kind       string    `json:"kind"`
	Actor      string    `json:"actor,omitempty"`
	Message    string    `json:"message,omitempty"`
}

// explainInstance — Diferencial "por que esse job não rodou?": expõe, por
// instance, o gating que o scheduler já computa (janela · deps · conditions ·
// recursos) via a FONTE ÚNICA gateInstance. Determinístico (sem IA); o futuro
// tool MCP explain_job() chama exatamente isto.
func (s *server) explainInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ex, err := s.cfg.Scheduler.Explain(id)
	if err != nil {
		http.Error(w, "instance não encontrada", http.StatusNotFound)
		return
	}
	writeJSON(w, 200, ex)
}

func (s *server) listInstanceEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rows, err := s.cfg.DB.Query(
		`SELECT id, instance_id, ts, kind, COALESCE(actor,''), COALESCE(message,'')
		 FROM instance_events WHERE instance_id=? ORDER BY id DESC`,
		id,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []instanceEvent{}
	for rows.Next() {
		var e instanceEvent
		if err := rows.Scan(&e.ID, &e.InstanceID, &e.TS, &e.Kind, &e.Actor, &e.Message); err != nil {
			continue
		}
		out = append(out, e)
	}
	writeJSON(w, 200, out)
}
