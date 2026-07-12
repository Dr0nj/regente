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
	Confirmed    bool       `json:"confirmed,omitempty"`   // Control-M Confirm: operador liberou (job confirm:true).
	CycleRuns    int        `json:"cycleRuns,omitempty"`   // cyclic runtime: voltas OK completadas no dia.
	// DryRun CONGELADO na ordem (coluna dry_run, ver schemaV9). É a fonte da flag
	// "job roda sem fazer nada" (selo 👻GHOST) no Monitoring — deliberadamente NÃO
	// derivada da definition viva, senão ligar dryRun no Design reescreveria cards
	// de jobs já ordenados. Monitoring é imutável; só a próxima ordem reflete a def.
	DryRun bool `json:"dryRun,omitempty"`
	// HoldScope — origem do HOLD (schemaV14). "" = hold individual (liberável 1-a-1);
	// "folder" = segurado por uma pausa de folder (D-2), só o resume da folder libera.
	// O front usa isto para o cadeado (folder vs individual) e para bloquear o
	// Release individual de um job segurado pela folder.
	HoldScope string `json:"holdScope,omitempty"`
}

const instanceCols = `id, definition_id, COALESCE(team,''), order_date, status, scheduled_at,
	started_at, finished_at,
	COALESCE(agent_id,''), COALESCE(exit_code,0), COALESCE(output,''), COALESCE(forced,0),
	COALESCE(carried_from,''), COALESCE(confirmed,0), COALESCE(cycle_runs,0), COALESCE(dry_run,0),
	COALESCE(hold_scope,'')`

// scanInstances materializa as linhas. CRÍTICO: propaga rows.Err() e qualquer
// erro de Scan em vez de engolir e devolver lista PARCIAL. Um erro de leitura no
// meio da iteração (ex.: SQLITE_BUSY sob rajada de writes) fazia Next() parar e
// o handler responder 200 com uma lista truncada — o cliente tratava isso como
// snapshot completo e apagava do board tudo que "faltou" (cards sumindo, só
// voltando com F5). Melhor falhar (o cliente preserva o estado atual em erro).
func scanInstances(rows *sql.Rows) ([]instanceRow, error) {
	out := []instanceRow{}
	for rows.Next() {
		var ir instanceRow
		var startedAt, finishedAt sql.NullTime
		var forcedInt, confirmedInt, dryRunInt int
		if err := rows.Scan(
			&ir.ID, &ir.DefinitionID, &ir.Team, &ir.OrderDate, &ir.Status, &ir.ScheduledAt,
			&startedAt, &finishedAt,
			&ir.AgentID, &ir.ExitCode, &ir.Output, &forcedInt,
			&ir.CarriedFrom, &confirmedInt, &ir.CycleRuns, &dryRunInt, &ir.HoldScope,
		); err != nil {
			return nil, err
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
		ir.Confirmed = confirmedInt == 1
		ir.DryRun = dryRunInt == 1
		out = append(out, ir)
	}
	return out, rows.Err()
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
	list, err := scanInstances(rows)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, 200, list)
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
	// offset opcional (UI-1): random-access pra sidebar virtualizada — um salto
	// de scrollbar pede a página N sem andar cursor a cursor. Ignorado quando há
	// cursor (keyset continua sendo o caminho barato do scroll sequencial).
	if off := r.URL.Query().Get("offset"); off != "" && r.URL.Query().Get("cursor") == "" {
		if n, err := strconv.Atoi(off); err == nil && n > 0 {
			sqlStr += ` OFFSET ?`
			args = append(args, n)
		}
	}
	rows, err := s.cfg.DB.Query(sqlStr, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	items, err := scanInstances(rows)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
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
	// pausedFolders — folders com pelo menos um job segurado por PAUSA DE FOLDER
	// (status HELD + hold_scope='folder', schemaV14). Alimenta o cadeado da folder
	// e o estado do botão pausar/retomar na sidebar do modo windowed (no modo local
	// o front deriva isso do holdScope das próprias instances). [] se nenhuma.
	pausedFolders := []string{}
	if rows, err := s.cfg.DB.Query(
		`SELECT DISTINCT team FROM instances WHERE `+where+` AND status=? AND hold_scope='folder'`,
		append(args, string(domain.StatusHeld))...,
	); err == nil {
		for rows.Next() {
			var t string
			if rows.Scan(&t) == nil {
				pausedFolders = append(pausedFolders, t)
			}
		}
		rows.Close()
	}
	writeJSON(w, 200, map[string]any{"date": q.date, "total": total, "byStatus": byStatus, "byFolder": byFolder, "pausedFolders": pausedFolders})
}

func (s *server) holdInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.requireInstanceWrite(w, r, id) {
		return
	}
	// Hold individual: hold_scope='' (liberável 1-a-1). Distinto da pausa de folder,
	// que marca 'folder' e só sai no resume da folder (schemaV14).
	_, err := s.cfg.DB.Exec(`UPDATE instances SET status=?, hold_scope='' WHERE id=? AND status=?`,
		string(domain.StatusHeld), id, string(domain.StatusWaiting))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.cfg.Scheduler.EmitEvent(id, "held", "operator", "")
	s.cfg.Hub.BroadcastWeb("instance.changed", map[string]any{"id": id, "status": string(domain.StatusHeld), "holdScope": ""})
	writeJSON(w, 200, map[string]string{"id": id, "status": string(domain.StatusHeld)})
}

func (s *server) releaseInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.requireInstanceWrite(w, r, id) {
		return
	}
	// Um job segurado por uma PAUSA DE FOLDER (hold_scope='folder') NÃO pode ser
	// liberado individualmente — só o resume da folder inteira destrava (paridade
	// Control-M "Hold folder" ⇒ "Release folder"). O Release individual só age em
	// holds individuais (hold_scope=''). O front já esconde o botão nesse caso;
	// este 409 é a rede de segurança do servidor (idempotente/atômico).
	res, err := s.cfg.DB.Exec(`UPDATE instances SET status=? WHERE id=? AND status=? AND hold_scope=''`,
		string(domain.StatusWaiting), id, string(domain.StatusHeld))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Nada liberado: ou já não estava em hold individual, ou está segurado pela
		// folder. Distingue os dois pra dar um erro claro (o não-hold é idempotente).
		var status, scope string
		_ = s.cfg.DB.QueryRow(`SELECT status, COALESCE(hold_scope,'') FROM instances WHERE id=?`, id).Scan(&status, &scope)
		if status == string(domain.StatusHeld) && scope == "folder" {
			http.Error(w, "job segurado por uma pausa de folder — libere pela folder (Release folder), não individualmente", http.StatusConflict)
			return
		}
	}
	s.cfg.Scheduler.EmitEvent(id, "released", "operator", "")
	s.cfg.Hub.BroadcastWeb("instance.changed", map[string]any{"id": id, "status": string(domain.StatusWaiting), "holdScope": ""})
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

// confirmInstance — Control-M "Confirm": libera um job definido com
// confirm:true (gate WAIT_CONFIRM). Vale para WAITING e HELD (confirmar um job
// em hold conta; ele roda quando for liberado). Cutuca o tick — confirmou,
// disparou.
func (s *server) confirmInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.requireInstanceWrite(w, r, id) {
		return
	}
	res, err := s.cfg.DB.Exec(`UPDATE instances SET confirmed=1 WHERE id=? AND status IN (?,?)`,
		id, string(domain.StatusWaiting), string(domain.StatusHeld))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, "instance not found or not in WAITING/HELD", http.StatusBadRequest)
		return
	}
	s.cfg.Scheduler.EmitEvent(id, "confirmed", "operator", "")
	var status string
	_ = s.cfg.DB.QueryRow(`SELECT status FROM instances WHERE id=?`, id).Scan(&status)
	s.cfg.Hub.BroadcastWeb("instance.changed", map[string]interface{}{"id": id, "status": status, "confirmed": true})
	go s.cfg.Scheduler.Tick()
	writeJSON(w, 200, map[string]interface{}{"id": id, "status": status, "confirmed": true})
}

func (s *server) rerunInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.requireInstanceWrite(w, r, id) {
		return
	}
	// confirmed=0: rerun re-exige Confirm em jobs confirm:true (Control-M).
	_, err := s.cfg.DB.Exec(
		`UPDATE instances SET status=?, started_at=NULL, finished_at=NULL, exit_code=NULL, output=NULL, confirmed=0 WHERE id=?`,
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
	// E1 — "hoje" é o dia na timezone de NEGÓCIO (settings.daily_timezone), o
	// mesmo relógio da daily automática: rodar manualmente às 22h de SP com o
	// server em UTC (01h do dia seguinte) NÃO pode materializar a diária de amanhã.
	today := s.cfg.Scheduler.TodayDate()
	created := s.cfg.Scheduler.RunDaily(today)
	writeJSON(w, 200, map[string]interface{}{"orderDate": today, "created": created})
}

// dailyStatus — estado da daily pelo relógio de NEGÓCIO do servidor: última
// execução (daily_runs) + horário (settings.daily_at) + timezone (E1,
// settings.daily_timezone; "" = relógio local do server). É a fonte da verdade
// do rodapé do Monitoring — o front NÃO persiste mais isso em localStorage em
// server mode.
func (s *server) dailyStatus(w http.ResponseWriter, r *http.Request) {
	var lastDate, lastAt string
	_ = s.cfg.DB.QueryRow(
		`SELECT order_date, started_at FROM daily_runs ORDER BY order_date DESC LIMIT 1`,
	).Scan(&lastDate, &lastAt)
	tzName, _ := s.cfg.Scheduler.DailyTimezone()
	now := s.cfg.Scheduler.NowLocal()
	writeJSON(w, 200, map[string]interface{}{
		"orderDate":   now.Format("2006-01-02"),
		"dailyAt":     s.cfg.Scheduler.DailyAt(),
		"timezone":    tzName,
		"lastRunDate": lastDate,
		"lastRunAt":   lastAt,
		"serverNow":   now.Format(time.RFC3339),
	})
}

// dailyReport — E5: relatório/SLO da daily (o que rodou, o que falhou, atraso).
// Default = a diária de HOJE na timezone de negócio. Fonte do card compacto do
// Monitoring e do push opcional (daily_report_channels).
func (s *server) dailyReport(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if date == "" {
		date = s.cfg.Scheduler.TodayDate()
	}
	rep, err := s.cfg.Scheduler.BuildDailyReport(date)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, 200, rep)
}

// dryRunDaily — Diferencial "simular a daily de uma data futura SEM materializar":
// quem roda, quem espera (depois de quem) e quem nunca dispara (e por quê). Default
// date = amanhã. Não toca o banco.
func (s *server) dryRunDaily(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	if date == "" {
		date = time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	}
	dr, err := s.cfg.Scheduler.DryRun(date)
	if err != nil {
		http.Error(w, "data inválida (use YYYY-MM-DD)", http.StatusBadRequest)
		return
	}
	writeJSON(w, 200, dr)
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

// schedulerDaily — ARCH-5: gatilho de daily DEDICADO. Materializa a diária de
// hoje se devida (idempotente, leader-guarded), SEM rodar o ciclo de dispatch.
// Pensado para um cron DIÁRIO separado do tick de alta frequência — no modo
// serverless você aponta um cron de minutos em /scheduler/tick (dispatch) e um
// cron diário aqui (materialização), em vez de cada tick checar a daily.
func (s *server) schedulerDaily(w http.ResponseWriter, r *http.Request) {
	s.cfg.Scheduler.RunDailyIfDue()
	writeJSON(w, 200, map[string]interface{}{
		"ok": true, "orderDate": s.cfg.Scheduler.TodayDate(), "at": time.Now().UTC().Format(time.RFC3339),
	})
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

// forceRunInstance — "Run Now" sobre a instance EXISTENTE (Control-M "Force"
// aplicado a uma ordem já materializada, não um novo Order Force). Marca a
// instância WAITING/HELD como forced=1 e reagenda para agora; o tick (ramo
// forced) então bypassa janela/deps/conditions/recursos. NÃO bypassa agente
// indisponível nem o gate Confirm — mesma semântica do force-order. O job que
// sofre a ação é o MESMO (preserva o snapshot imutável da ordem); nenhuma nova
// instance é criada.
func (s *server) forceRunInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.requireInstanceWrite(w, r, id) {
		return
	}
	res, err := s.cfg.DB.Exec(
		`UPDATE instances SET status=?, forced=1, scheduled_at=? WHERE id=? AND status IN (?,?)`,
		string(domain.StatusWaiting), time.Now(), id,
		string(domain.StatusWaiting), string(domain.StatusHeld),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, "instance not found or not in WAITING/HELD", http.StatusBadRequest)
		return
	}
	s.cfg.Scheduler.EmitEvent(id, "force-ordered", "operator", "run now — bypass de janela/deps/conditions/recursos")
	s.cfg.Hub.BroadcastWeb("instance.changed", map[string]interface{}{"id": id, "status": string(domain.StatusWaiting), "forced": true})
	// Cutuca o tick (leader-gated) pra despachar já, sem esperar o ciclo de 2s.
	go s.cfg.Scheduler.Tick()
	writeJSON(w, 200, map[string]interface{}{"id": id, "status": string(domain.StatusWaiting), "forced": true})
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

// blastRadius — Diferencial "se eu cancelar/segurar este job AGORA, qual o
// impacto?": jobs downstream que deixam de rodar (cascata), SLAs em risco e times
// afetados. Percorre o grafo de deps a partir do alvo, visitando só o raio.
func (s *server) blastRadius(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	br, err := s.cfg.Scheduler.BlastRadius(id)
	if err != nil {
		http.Error(w, "instance não encontrada", http.StatusNotFound)
		return
	}
	writeJSON(w, 200, br)
}

// neighborhood — Diferencial "grafo local do job": ancestrais (de quem depende) e
// descendentes (quem depende dele) até `radius` saltos, com status por instance.
// Contexto de vizinhança antes de agir. Read-only.
func (s *server) neighborhood(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	radius := 1
	if v := r.URL.Query().Get("radius"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			radius = n
		}
	}
	nb, err := s.cfg.Scheduler.Neighborhood(id, radius)
	if err != nil {
		http.Error(w, "instance não encontrada", http.StatusNotFound)
		return
	}
	writeJSON(w, 200, nb)
}

// rca — Diferencial "causa raiz": sobe a cadeia de upstreams falhos e aponta o job
// que falhou por conta própria e derrubou o resto. Read-only.
func (s *server) rca(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, err := s.cfg.Scheduler.RCA(id)
	if err != nil {
		http.Error(w, "instance não encontrada", http.StatusNotFound)
		return
	}
	writeJSON(w, 200, res)
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
