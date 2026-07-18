package api

import (
	"database/sql"
	"encoding/json"
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
	// HeldFromStatus — status congelado pelo HOLD (schemaV16, hold geral): o hold
	// vale pra qualquer status não-RUNNING e o Release restaura ESTE status (não
	// WAITING cego). '' quando não-HELD ou hold legado (era WAITING). O front usa
	// pra rotular o Release ("volta a NOTOK") — o Delete só olha status=HELD.
	HeldFromStatus string `json:"heldFromStatus,omitempty"`
	// M1 (schemaV18) — Monitoring IMUTÁVEL: tudo que o card/lista/grafo exibem
	// vem CONGELADO da ordem (label/tipo/gates/condições), nunca da def viva.
	// Instances legadas sem backfill ficam com ''/vazio e o front cai na def
	// viva SÓ nesse caso.
	Label       string   `json:"label,omitempty"`
	JobType     string   `json:"jobType,omitempty"`
	ConfirmReq  bool     `json:"confirmReq,omitempty"`
	Environment string   `json:"environment,omitempty"`
	PinnedAgent string   `json:"pinnedAgent,omitempty"`
	CondsIn     []string `json:"condsIn,omitempty"`
	CondsOutAdd []string `json:"condsOutAdd,omitempty"`
	// F15 (schemaV19) — recursos/quotas congelados na ordem (nome→qtd). Entrada
	// do card WAIT RESOURCE; mesmo dado que o gate F15 lê do snapshot.
	Resources map[string]int `json:"resources,omitempty"`
}

const instanceCols = `id, definition_id, COALESCE(team,''), order_date, status, scheduled_at,
	started_at, finished_at,
	COALESCE(agent_id,''), COALESCE(exit_code,0), COALESCE(output,''), COALESCE(forced,0),
	COALESCE(carried_from,''), COALESCE(confirmed,0), COALESCE(cycle_runs,0), COALESCE(dry_run,0),
	COALESCE(hold_scope,''), COALESCE(held_from_status,''),
	COALESCE(label,''), COALESCE(job_type,''), COALESCE(confirm_req,0),
	COALESCE(environment,''), COALESCE(pinned_agent,''), COALESCE(conds_in,''), COALESCE(conds_out_add,''),
	COALESCE(resources,'')`

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
		var forcedInt, confirmedInt, dryRunInt, confirmReqInt int
		var condsIn, condsOutAdd, resources string
		if err := rows.Scan(
			&ir.ID, &ir.DefinitionID, &ir.Team, &ir.OrderDate, &ir.Status, &ir.ScheduledAt,
			&startedAt, &finishedAt,
			&ir.AgentID, &ir.ExitCode, &ir.Output, &forcedInt,
			&ir.CarriedFrom, &confirmedInt, &ir.CycleRuns, &dryRunInt, &ir.HoldScope, &ir.HeldFromStatus,
			&ir.Label, &ir.JobType, &confirmReqInt,
			&ir.Environment, &ir.PinnedAgent, &condsIn, &condsOutAdd, &resources,
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
		ir.ConfirmReq = confirmReqInt == 1
		ir.CondsIn = decodeConds(condsIn)
		ir.CondsOutAdd = decodeConds(condsOutAdd)
		ir.Resources = decodeResources(resources)
		out = append(out, ir)
	}
	return out, rows.Err()
}

// decodeConds — coluna conds_* ('' ou JSON array de strings) → slice ([] = nil,
// o omitempty do JSON esconde). Conteúdo inválido é tratado como vazio.
func decodeConds(raw string) []string {
	if raw == "" {
		return nil
	}
	var list []string
	if json.Unmarshal([]byte(raw), &list) != nil || len(list) == 0 {
		return nil
	}
	return list
}

// decodeResources — coluna resources ('' ou JSON {nome:qtd}) → mapa (nil quando
// vazio; o omitempty do JSON esconde). Conteúdo inválido é tratado como vazio.
func decodeResources(raw string) map[string]int {
	if raw == "" {
		return nil
	}
	var m map[string]int
	if json.Unmarshal([]byte(raw), &m) != nil || len(m) == 0 {
		return nil
	}
	return m
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

// instanceDetail — resposta de GET /api/instances/{id}: a linha do runtime +
// a AÇÃO CONGELADA NA ORDEM (definition_snapshot): label, jobType e actionConfig
// do momento em que a instance foi materializada. É o que o drawer do Monitoring
// mostra nas seções Action/Output — a mesma foto que o dispatch executa
// (defForInstance), não a def viva, que pode ter sido editada depois da ordem.
// Snapshot ausente (seed antigo) → campos vazios; o front cai na def de desenho.
// M1: Label/JobType agora vivem no próprio instanceRow (colunas congeladas
// schemaV18); o detalhe acrescenta a action E a DEF CONGELADA INTEIRA
// (definition_snapshot) — o drawer do Monitoring lê Schedule/Condições dela,
// não da def viva do Design (imutabilidade total). Payload só do detalhe (a
// lista de escala não carrega nada disso).
type instanceDetail struct {
	instanceRow
	ActionConfig map[string]interface{} `json:"actionConfig,omitempty"`
	// SnapshotDef — a JobDefinition congelada na ordem (JSON cru do domain, que
	// o front consome como JobDefinition: mesmos json tags). Vazio em instance
	// legada sem snapshot (o drawer cai na def viva só nesse caso).
	SnapshotDef json.RawMessage `json:"snapshotDef,omitempty"`
}

// getInstance — GET /api/instances/{id}. Detalhe de UMA instance, incluindo o
// que a lista deliberadamente NÃO carrega (payload de escala): a action do
// snapshot. RBAC igual ao list: folder ilegível responde 404 (não vaza existência).
func (s *server) getInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rows, err := s.cfg.DB.Query(`SELECT `+instanceCols+` FROM instances WHERE id=?`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	list, err := scanInstances(rows)
	rows.Close()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(list) == 0 {
		http.Error(w, "instance not found", http.StatusNotFound)
		return
	}
	det := instanceDetail{instanceRow: list[0]}

	if allowed, restrict := s.allowedTeams(r, det.OrderDate); restrict {
		ok := false
		for _, t := range allowed {
			if t == det.Team {
				ok = true
				break
			}
		}
		if !ok {
			http.Error(w, "instance not found", http.StatusNotFound)
			return
		}
	}

	var snap string
	_ = s.cfg.DB.QueryRow(`SELECT COALESCE(definition_snapshot,'') FROM instances WHERE id=?`, id).Scan(&snap)
	if snap != "" {
		var d domain.JobDefinition
		if json.Unmarshal([]byte(snap), &d) == nil {
			// Label/JobType já vêm das colunas congeladas (M1); reforço só p/
			// instance legada com snapshot mas sem backfill de coluna.
			if det.Label == "" {
				det.Label = d.Label
			}
			if det.JobType == "" {
				det.JobType = d.JobType
			}
			det.ActionConfig = d.Params
			det.SnapshotDef = json.RawMessage(snap) // def congelada inteira p/ Schedule/Deps
		}
	}
	writeJSON(w, 200, det)
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
	// Hold geral (schemaV16): segura QUALQUER status exceto RUNNING (a execução
	// já está no agente — não há o que segurar; cancele ou espere terminar) e o
	// próprio HELD (idempotente). held_from_status congela o status original —
	// o Release restaura ELE, não WAITING cego (que re-executaria um OK).
	// Hold individual: hold_scope='' (liberável 1-a-1). Distinto da pausa de
	// folder, que marca 'folder' e só sai no resume da folder (schemaV14).
	res, err := s.cfg.DB.Exec(
		`UPDATE instances SET held_from_status=status, status=?, hold_scope='' WHERE id=? AND status NOT IN (?,?)`,
		string(domain.StatusHeld), id, string(domain.StatusRunning), string(domain.StatusHeld))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var status string
		if err := s.cfg.DB.QueryRow(`SELECT status FROM instances WHERE id=?`, id).Scan(&status); err != nil {
			http.Error(w, "instance not found", http.StatusNotFound)
			return
		}
		if status == string(domain.StatusRunning) {
			http.Error(w, "job RUNNING não pode ser segurado — cancele ou aguarde terminar", http.StatusConflict)
			return
		}
		// Já estava HELD: no-op idempotente (ecoa o estado atual).
		writeJSON(w, 200, map[string]string{"id": id, "status": status})
		return
	}
	// heldFromStatus no broadcast: o payload parcial do WS é o que o espelho do
	// front aplica NA HORA (o refresh de cauda reconcilia depois) — sem ele o
	// rótulo "Release (volta a X)" só apareceria no próximo refresh cheio.
	var heldFrom string
	_ = s.cfg.DB.QueryRow(`SELECT COALESCE(held_from_status,'') FROM instances WHERE id=?`, id).Scan(&heldFrom)
	s.cfg.Scheduler.EmitEvent(id, "held", "operator", "")
	s.cfg.Hub.BroadcastWeb("instance.changed", map[string]any{"id": id, "status": string(domain.StatusHeld), "holdScope": "", "heldFromStatus": heldFrom})
	writeJSON(w, 200, map[string]string{"id": id, "status": string(domain.StatusHeld)})
}

// releaseSQL — Release/Resume restauram o status congelado pelo hold
// (held_from_status, schemaV16); '' = hold legado (pré-migração), cai em
// WAITING como sempre foi. Compartilhado pelo release individual, bulk e
// resume de folder — os três só divergem no WHERE (escopo).
const releaseSQL = `UPDATE instances
	SET status = CASE WHEN COALESCE(held_from_status,'')='' THEN 'WAITING' ELSE held_from_status END,
	    held_from_status='', hold_scope=''`

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
	res, err := s.cfg.DB.Exec(releaseSQL+` WHERE id=? AND status=? AND hold_scope=''`,
		id, string(domain.StatusHeld))
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
	// O status restaurado depende do que o hold congelou — lê o resultado real.
	status := string(domain.StatusWaiting)
	_ = s.cfg.DB.QueryRow(`SELECT status FROM instances WHERE id=?`, id).Scan(&status)
	s.cfg.Scheduler.EmitEvent(id, "released", "operator", "")
	s.cfg.Hub.BroadcastWeb("instance.changed", map[string]any{"id": id, "status": status, "holdScope": "", "heldFromStatus": ""})
	writeJSON(w, 200, map[string]string{"id": id, "status": status})
}

// deleteInstance — Control-M "Delete job": remove a ordem da tela (e do state
// store). SÓ vale para job em HOLD — como RUNNING não é segurável, um job em
// execução nunca é deletável; qualquer outro status vira deletável passando
// pelo hold primeiro (o fluxo do usuário: Hold → Delete). O POOL de condições
// fica intacto: condições que a instance adicionou/removeu em términos OK já
// aconteceram (deletar a ordem não desfaz história); um WAITING deletado nunca
// aplicou saídas. Os instance_events morrem junto (mesmo padrão do archiveGC).
func (s *server) deleteInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.requireInstanceWrite(w, r, id) {
		return
	}
	var status string
	if err := s.cfg.DB.QueryRow(`SELECT status FROM instances WHERE id=?`, id).Scan(&status); err != nil {
		http.Error(w, "instance not found", http.StatusNotFound)
		return
	}
	if status != string(domain.StatusHeld) {
		http.Error(w, "delete exige o job em HOLD (segure primeiro; RUNNING não é deletável)", http.StatusConflict)
		return
	}
	if _, err := s.cfg.DB.Exec(`DELETE FROM instance_events WHERE instance_id=?`, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res, err := s.cfg.DB.Exec(`DELETE FROM instances WHERE id=? AND status=?`, id, string(domain.StatusHeld))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		http.Error(w, "instance mudou de estado durante o delete — recarregue", http.StatusConflict)
		return
	}
	go s.cfg.Scheduler.Tick()
	s.cfg.Hub.BroadcastWeb("instance.deleted", map[string]string{"id": id})
	writeJSON(w, 200, map[string]string{"id": id, "status": "deleted"})
}

func (s *server) cancelInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !s.requireInstanceWrite(w, r, id) {
		return
	}
	// Modelo único de condições: cancel NÃO toca o pool. Um WAITING cancelado
	// nunca aplicou saídas (a condição de entrada segue lá pra um clone/rerun);
	// um OK cancelado já consumiu (as OutRemove rodaram no término — história).
	_, err := s.cfg.DB.Exec(`UPDATE instances SET status=?, held_from_status='', finished_at=CURRENT_TIMESTAMP WHERE id=?`,
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
	// Modelo único de condições: rerun NÃO toca o pool — o gate re-avalia o que
	// EXISTE. Rerun após OK espera de novo (o próprio OK consumiu a condição de
	// entrada via OutRemove); rerun após NOTOK roda direto (falha não consome,
	// a condição segue no pool).
	//
	// BUG-2: `confirmed` SOBREVIVE ao rerun — um job que já passou pelo gate
	// Confirm não volta pro CONFIRM ao ser re-rodado; ele re-entra direto no
	// gating normal (deps/janela). Quem nunca confirmou segue exigindo Confirm.
	//
	// O bypass do Run Now NÃO é pegajoso: rerun de instance com forced=1 e
	// force_mode='' volta ao gating normal (senão o rerun re-dispara na hora,
	// ignorando WAIT COND — quer bypass de novo? Run Now de novo). A cópia de
	// Order Force (mode='order') mantém o forced: ela nunca teve janela real.
	_, err := s.cfg.DB.Exec(
		`UPDATE instances SET status=?, started_at=NULL, finished_at=NULL, exit_code=NULL, output=NULL,
		        held_from_status='',
		        forced = CASE WHEN COALESCE(force_mode,'')='' THEN 0 ELSE forced END
		 WHERE id=?`,
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
	// BUG-10/11: rerun com os gates já satisfeitos (ex.: horário que já passou)
	// entra NA HORA — cutuca o tick em vez de esperar o próximo ciclo.
	go s.cfg.Scheduler.Tick()
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
	// BUG-4/10: o Set OK materializa evento/conditions pros dependentes — cutuca
	// o tick pra eles entrarem na hora, sem esperar o próximo ciclo.
	go s.cfg.Scheduler.Tick()
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
	var lastDate string
	var lastAt sql.NullTime
	_ = s.cfg.DB.QueryRow(
		`SELECT order_date, started_at FROM daily_runs ORDER BY order_date DESC LIMIT 1`,
	).Scan(&lastDate, &lastAt)
	tzName, _ := s.cfg.Scheduler.DailyTimezone()
	now := s.cfg.Scheduler.NowLocal()
	resp := map[string]interface{}{
		"orderDate":   now.Format("2006-01-02"),
		"dailyAt":     s.cfg.Scheduler.DailyAt(),
		"timezone":    tzName,
		"lastRunDate": lastDate,
		"serverNow":   now.Format(time.RFC3339),
	}
	// lateStart no rodapé do Monitoring (só quando há daily): reusa a fonte única
	// de pontualidade (mesma flag do /api/daily/report), sem os counts.
	if lastAt.Valid {
		resp["lastRunAt"] = lastAt.Time.Format(time.RFC3339)
		resp["lateStart"] = s.cfg.Scheduler.IsDailyLate(lastDate, lastAt.Time)
	}
	writeJSON(w, 200, resp)
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
	// BUG-1: force_mode='' explícito — um Run Now sobre uma cópia criada por
	// "Order Force" (force_mode='order', que RESPEITA os gates de runtime e pode
	// estar presa em WAIT EVENT) converte a instance pro modo bypass TOTAL. Sem
	// isso o ramo forced do tick (`ForceMode != order`) nunca ativava e o job
	// continuava aguardando evento mesmo depois do Run Now.
	res, err := s.cfg.DB.Exec(
		`UPDATE instances SET status=?, forced=1, force_mode='', held_from_status='', scheduled_at=? WHERE id=? AND status IN (?,?)`,
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
