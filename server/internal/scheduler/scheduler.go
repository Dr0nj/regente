// Package scheduler — core de orquestração do servidor.
//
// Responsabilidades:
//   - Auto-rodar a daily quando o relógio cruza `Settings.DailyAt`
//   - Materializar instances WAITING para cada definition habilitada
//   - Tick periódico: avaliar deps e promover WAITING -> RUNNING
//   - Despachar para agent via hub (ou mock-finalizar se não houver agent)
//   - Receber resultados via FinishInstance (chamado pelo ws handler)
//   - Suportar ForceOrder (Control-M: "Order Force")
package scheduler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/storage"
	"github.com/Dr0nj/regente-server/internal/telemetry"

	"go.opentelemetry.io/otel/attribute"
)

// Settings — configuração global (depois persistida em workspace/settings.yaml).
type Settings struct {
	DailyAt  string `json:"dailyAt" yaml:"dailyAt"`   // "HH:MM" (default 00:00 — meia-noite, ver memory/core/regente-product-model.md)
	Timezone string `json:"timezone" yaml:"timezone"` // default America/Sao_Paulo
}

type Scheduler struct {
	store *storage.FileStore
	db    *db.DB
	hub   Bus
	tick  time.Duration

	mu         sync.Mutex
	running    map[string]bool
	defs       []domain.JobDefinition
	settings   Settings
	lastTickAt time.Time // R2 — watchdog: instante do último ciclo de scheduling

	// === Bloco 2 — engines opcionais ===
	calStore   *storage.CalendarStore // F14
	resources  *ResourceTracker       // F15
	conditions *ConditionEngine       // F16
	sla        *SLAEngine             // F19
	alerts     *AlertEngine           // Phase 8 alerting
	variables  *storage.VariableStore // F18 globals

	// === Opção B (2026-04-26) — daily lê de Git ===
	git *storage.GitOps // fonte autoritativa das definitions na daily; sha lido sob demanda

	// === G1 (2026-06-14) — HA: só o líder materializa a daily + dispatch ===
	leader Leader
}

// Bus — Fase 2 (futuro serverless): transporte do control-plane. Abstrai a
// difusão de eventos para a web e o roteamento de dispatch para agentes, de
// modo que o WebSocket hub (default) possa ser trocado por NATS/SSE/long-poll
// sem tocar no core do scheduler (ver docs/arquitetura-futuro.md). *hub.Hub
// satisfaz esta interface; o tipo hub.Client segue compartilhado por ser o
// canal de envio concreto para o agente.
type Bus interface {
	BroadcastWeb(event string, payload interface{})
	PickAgent(capability string) *hub.Client
	GetAgent(id string) *hub.Client
	// Dispatch entrega o payload a um agent — local (hub) ou, no bus distribuído
	// (R5/NATS), roteado ao nó dono do agent. Retorna o resultado e o id escolhido.
	Dispatch(agentID, capability string, raw []byte) (hub.DispatchOutcome, string)
}

// Leader — G1 HA. Só o nó líder roda a daily automática e o tick de dispatch;
// os demais nós servem API normalmente. Implementações em internal/leader:
// SingleNode (sempre líder — SQLite/nó único) e PgAdvisory (advisory lock no
// Postgres para vários nós). O claim atômico de startInstance (fix do P15) já
// garante no máx. 1 execução por instance mesmo se dois nós despacharem.
type Leader interface{ IsLeader() bool }

func New(store *storage.FileStore, db *db.DB, bus Bus, tick time.Duration) *Scheduler {
	return &Scheduler{
		store:    store,
		db:       db,
		hub:      bus,
		tick:     tick,
		running:  map[string]bool{},
		settings: Settings{DailyAt: "00:00", Timezone: "America/Sao_Paulo"},
	}
}

// === Bloco 2 setters (chamados em main após construir as engines) ===

func (s *Scheduler) AttachCalendars(c *storage.CalendarStore) { s.calStore = c }
func (s *Scheduler) AttachResources(r *ResourceTracker)       { s.resources = r }
func (s *Scheduler) AttachConditions(c *ConditionEngine)      { s.conditions = c }
func (s *Scheduler) AttachSLA(sl *SLAEngine)                  { s.sla = sl }
func (s *Scheduler) AttachAlerts(a *AlertEngine)              { s.alerts = a }
func (s *Scheduler) AttachVariables(v *storage.VariableStore) { s.variables = v }
func (s *Scheduler) AttachGit(g *storage.GitOps)              { s.git = g }
func (s *Scheduler) AttachLeader(l Leader)                    { s.leader = l }

// isLeader — nil leader = nó único (sempre líder). G1.
func (s *Scheduler) isLeader() bool { return s.leader == nil || s.leader.IsLeader() }

// IsLeader — R3: status de liderança exposto para o /readyz (público). nó único = true.
func (s *Scheduler) IsLeader() bool { return s.isLeader() }

// Resources / Conditions / SLA exposed for API handlers.
func (s *Scheduler) Resources() *ResourceTracker       { return s.resources }
func (s *Scheduler) Conditions() *ConditionEngine      { return s.conditions }
func (s *Scheduler) SLA() *SLAEngine                   { return s.sla }
func (s *Scheduler) Alerts() *AlertEngine              { return s.alerts }
func (s *Scheduler) Calendars() *storage.CalendarStore { return s.calStore }
func (s *Scheduler) Variables() *storage.VariableStore { return s.variables }

// buildVarContext \u2014 BuildContext + globals injetados do VariableStore (F18).
func (s *Scheduler) buildVarContext(def domain.JobDefinition, instanceID string) VarContext {
	ctx := BuildContext(def, instanceID, time.Now().Format("2006-01-02"), nil, "")
	if s.variables != nil {
		ctx.Global = s.variables.Snapshot()
	}
	return ctx
}

// Defs returns a snapshot of loaded definitions (for forecast).
func (s *Scheduler) Defs() []domain.JobDefinition {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.JobDefinition, len(s.defs))
	copy(out, s.defs)
	return out
}

// Run roda o loop interno de scheduling (modo daemon clássico): a cada `tick`
// dispara Tick(). Usado quando -scheduler=internal (default).
func (s *Scheduler) Run(ctx context.Context) {
	s.reloadDefs()
	t := time.NewTicker(s.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Tick()
		}
	}
}

// Tick executa um ciclo de scheduling: materializa a daily se devida e avalia
// deps/dispatch uma vez. É idempotente (claim atômico em startInstance + checks
// de existência na daily), então pode ser disparado por um cron externo
// (-scheduler=external + POST /api/scheduler/tick) num deploy serverless
// scale-to-zero, sem o ticker em goroutine. Fase 1 — ver docs/arquitetura-futuro.md.
//
// G1 — só o líder materializa a daily e despacha; followers retornam cedo.
func (s *Scheduler) Tick() {
	// R2 — watchdog: registra que o loop de scheduling rodou. A idade deste
	// instante é exposta em /metrics e /livez; se parar de avançar (ticker morto
	// no modo internal, ou cron parado no external), o monitor externo alerta.
	s.mu.Lock()
	s.lastTickAt = time.Now()
	s.mu.Unlock()
	// R2 — panic-recovery: um panic na materialização da daily ou na avaliação de
	// deps/dispatch NÃO pode derrubar o processo nem matar o loop de scheduling.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[scheduler] PANIC no Tick recuperado: %v", r)
		}
	}()
	if !s.isLeader() {
		return
	}
	s.autoDailyIfDue()
	s.tickOnce()
}

// LastTick — R2: instante do último ciclo de scheduling (watchdog/health).
// Zero se o scheduler ainda não rodou nenhum tick.
func (s *Scheduler) LastTick() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastTickAt
}

func (s *Scheduler) reloadDefs() {
	defs, err := s.store.List()
	if err != nil {
		log.Printf("[scheduler] reload defs: %v", err)
		return
	}
	s.mu.Lock()
	s.defs = defs
	s.mu.Unlock()
}

// ReloadDefs é chamado pelo handler de save/delete de definition.
func (s *Scheduler) ReloadDefs() { s.reloadDefs() }

// RebuildResourcesFromRunning — Enterprise/HA: o ResourceTracker (F15/quotas) é
// in-memory e vive no líder. Após restart ou failover, um novo líder começa com o
// tracker ZERADO enquanto há instances RUNNING que ainda detêm slots no mundo
// real → sem isto, as quotas furariam (o novo líder deixaria começar jobs acima
// da capacidade). Este método reconstrói o uso a partir do estado durável: cada
// instance RUNNING carrega o snapshot da def (com Resources). Chamar no boot e ao
// assumir liderança. Retorna quantas instances foram re-registradas.
func (s *Scheduler) RebuildResourcesFromRunning() (int, error) {
	if s.resources == nil {
		return 0, nil
	}
	rows, err := s.db.Query(`SELECT id, COALESCE(definition_snapshot,'') FROM instances WHERE status=?`, string(domain.StatusRunning))
	if err != nil {
		return 0, err
	}
	type rec struct{ id, snap string }
	var recs []rec
	for rows.Next() {
		var id, snap string
		if err := rows.Scan(&id, &snap); err == nil {
			recs = append(recs, rec{id, snap})
		}
	}
	rows.Close()

	n := 0
	for _, rc := range recs {
		if rc.snap == "" {
			continue
		}
		var def domain.JobDefinition
		if json.Unmarshal([]byte(rc.snap), &def) != nil || len(def.Resources) == 0 {
			continue
		}
		s.resources.Reacquire(rc.id, def.Resources)
		n++
	}
	if n > 0 {
		log.Printf("[scheduler] quotas: %d instances RUNNING re-registradas no tracker (rebuild)", n)
	}
	return n, nil
}

// currentCommitSHA — sha do HEAD do workspace de leitura (lido sob demanda).
// Vazio se GitOps não atachado.
func (s *Scheduler) currentCommitSHA() string {
	if s.git == nil {
		return ""
	}
	return s.git.Status().SHA
}

// dailySync — Opção B (2026-04-26): antes de cada daily, sincroniza o workspace
// com origin/<branch> e recarrega definitions. Retorna o SHA do HEAD após o fetch.
// Em falha, daily não roda — melhor adiar (retry no próximo tick) do que rodar contra
// snapshot velho.
//
// Quando GitOps não está atachado (modo offline/dev), apenas reload local + sha vazio.
func (s *Scheduler) dailySync() (sha string, err error) {
	if s.git == nil {
		s.reloadDefs()
		return "", nil
	}
	if err := s.git.SyncFromRemote(); err != nil {
		return "", fmt.Errorf("git sync: %w", err)
	}
	s.reloadDefs()
	return s.git.Status().SHA, nil
}

// emitEvent — registra um evento estruturado na trilha por instance.
// Não falha o fluxo se a inserção falhar (best-effort).
//
//	kind:   ordered | submitted | started | finished | timeout |
//	        cancelled | held | released | force-ordered | set-ok
//	actor:  "scheduler" | "operator" | "agent:<id>" | ""
func (s *Scheduler) emitEvent(instanceID, kind, actor, message string) {
	if instanceID == "" || kind == "" {
		return
	}
	_, err := s.db.Exec(
		`INSERT INTO instance_events(instance_id, kind, actor, message) VALUES(?,?,?,?)`,
		instanceID, kind, actor, message,
	)
	if err != nil {
		log.Printf("[scheduler] emitEvent %s/%s: %v", instanceID, kind, err)
	}
}

// EmitEvent — wrapper público para a API/handlers registrarem ações de operador.
func (s *Scheduler) EmitEvent(instanceID, kind, actor, message string) {
	s.emitEvent(instanceID, kind, actor, message)
}

func (s *Scheduler) autoDailyIfDue() {
	now := time.Now()
	today := now.Format("2006-01-02")

	var started sql.NullString
	err := s.db.QueryRow("SELECT started_at FROM daily_runs WHERE order_date=?", today).Scan(&started)
	if err == nil {
		return // já rodou hoje
	}

	parts := strings.Split(s.settings.DailyAt, ":")
	if len(parts) != 2 {
		return
	}
	hh, _ := strconv.Atoi(parts[0])
	mm, _ := strconv.Atoi(parts[1])
	dailyTime := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, now.Location())
	if now.Before(dailyTime) {
		return
	}
	// Opção B (2026-04-26): sincroniza com Git ANTES de materializar.
	// Se falhar, não marca daily_runs — próximo tick tenta de novo.
	sha, err := s.dailySync()
	if err != nil {
		log.Printf("[scheduler] daily %s skipped: %v", today, err)
		return
	}
	if sha != "" {
		log.Printf("[scheduler] daily %s: synced workspace to %s", today, short(sha))
	}
	s.RunDaily(today)
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// pendingInstance — uma instance decidida (gating já passou) aguardando o INSERT
// em lote. Materializar em duas fases (decidir tudo em memória, depois gravar numa
// transação) é o que torna a daily escalável a 100k–1M (ver insertDailyBatch).
type pendingInstance struct {
	id, defID, team string
	scheduledAt     time.Time
	snapshot        string
}

// dailyBatchChunk — tamanho do lote por transação na materialização. Bound o WAL
// do SQLite e dá progresso parcial (re-rodar é idempotente), sem perder o ganho
// do commit único por lote.
const dailyBatchChunk = 5000

// carryPlan — decisão de carry-over de UMA instance na virada da daily.
type carryPlan struct {
	carry     bool
	newBudget int    // orçamento a gravar na instance carregada (-1 = inalterado)
	reason    string // p/ o evento "carried" e testes
}

// keepActiveDays — quantas diárias EXTRA um job sobrevive sem terminar OK.
// notokDefault=true (caso NOTOK) → baseline 1 mesmo sem keepActive (DEFAULT +1).
// notokDefault=false (caso WAITING nunca-rodou) → só sobrevive se keepActive>0.
func keepActiveDays(def domain.JobDefinition, notokDefault bool) int {
	if def.Schedule.KeepActive > 0 {
		return def.Schedule.KeepActive
	}
	if notokDefault {
		return 1
	}
	return 0
}

// carryDecision — REGRA pura do ciclo de vida da daily (Control-M-like). Decide se
// uma instance no estado `status` (com `budget` atual e a def `def`) sobrevive à
// virada e qual orçamento ela leva pro novo dia. Isolada p/ ser testável sem DB.
//
//	RUNNING → carrega sempre (rastrear até terminar); orçamento intacto (-1 fica -1).
//	HELD    → carrega sempre enquanto em hold; orçamento intacto.
//	NOTOK   → carrega se ainda há orçamento; lazy-init = keepActive ou 1 (DEFAULT).
//	WAITING → (nunca rodou) só carrega se keepActive>0; lazy-init = keepActive.
//	OK/CANCELLED/outros → não carrega (encerrado).
func carryDecision(status string, budget int, def domain.JobDefinition) carryPlan {
	switch status {
	case string(domain.StatusRunning):
		return carryPlan{carry: true, newBudget: budget, reason: "running"}
	case string(domain.StatusHeld):
		return carryPlan{carry: true, newBudget: budget, reason: "held"}
	case string(domain.StatusNotOK):
		b := budget
		if b < 0 { // 1ª virada como NOTOK: inicializa o orçamento
			b = keepActiveDays(def, true)
		}
		if b <= 0 {
			return carryPlan{carry: false, newBudget: 0, reason: "notok-exhausted"}
		}
		return carryPlan{carry: true, newBudget: b - 1, reason: "notok"}
	case string(domain.StatusWaiting):
		b := budget
		if b < 0 {
			b = keepActiveDays(def, false)
		}
		if b <= 0 {
			return carryPlan{carry: false, newBudget: 0, reason: "waiting-no-keepactive"}
		}
		return carryPlan{carry: true, newBudget: b - 1, reason: "waiting-keepactive"}
	}
	return carryPlan{carry: false, newBudget: budget, reason: "terminal"}
}

// carriedInstance — uma instance decidida a carregar pro novo dia.
type carriedInstance struct {
	id     string
	from   string // order_date de origem (preservado entre múltiplas viradas)
	budget int
	reason string
}

// carryOver — ciclo de vida da daily (Control-M New Day). ANTES de materializar a
// daily de `date`, traz da diária ANTERIOR (o order_date mais recente < date) as
// instances que sobrevivem à virada: RUNNING/HELD persistem sempre; NOTOK não-tratado
// e WAITING/keepActive persistem enquanto têm orçamento (carry_budget). A instance
// carregada AVANÇA seu order_date para `date` mantendo ID/status/started_at/snapshot/
// eventos — assim o tick, a API paginada e o RBAC (todos filtram order_date) a enxergam
// no novo dia sem nenhuma mudança, e o check de existência do RunDaily NÃO cria uma
// instance fresca duplicada para a mesma definition.
//
// Idempotente: instances já em `date` não estão na diária anterior, então re-rodar
// (botão manual + auto) não move nada duas vezes nem reconsome orçamento.
func (s *Scheduler) carryOver(date string) int {
	var prev string
	if err := s.db.QueryRow(
		`SELECT COALESCE(MAX(order_date),'') FROM instances WHERE order_date < ?`, date,
	).Scan(&prev); err != nil || prev == "" {
		return 0
	}

	rows, err := s.db.Query(
		`SELECT id, definition_id, status, COALESCE(carry_budget,-1), COALESCE(carried_from,''),
		        COALESCE(definition_snapshot,'')
		 FROM instances
		 WHERE order_date=? AND status NOT IN (?,?)`,
		prev, string(domain.StatusOK), string(domain.StatusCancelled),
	)
	if err != nil {
		log.Printf("[scheduler] carry-over %s: query: %v", date, err)
		return 0
	}

	s.mu.Lock()
	live := make(map[string]domain.JobDefinition, len(s.defs))
	for _, d := range s.defs {
		live[d.ID] = d
	}
	s.mu.Unlock()

	var plan []carriedInstance
	for rows.Next() {
		var id, defID, status, from, snap string
		var budget int
		if rows.Scan(&id, &defID, &status, &budget, &from, &snap) != nil {
			continue
		}
		def, _ := defForInstance(instRow{DefID: defID, Snapshot: snap}, live)
		d := carryDecision(status, budget, def)
		if !d.carry {
			continue
		}
		origin := from
		if origin == "" { // 1ª virada: a origem da ordem é a diária de onde ela vem
			origin = prev
		}
		plan = append(plan, carriedInstance{id: id, from: origin, budget: d.newBudget, reason: d.reason})
	}
	rows.Close()

	if len(plan) == 0 {
		return 0
	}
	if err := s.applyCarry(date, plan); err != nil {
		log.Printf("[scheduler] carry-over %s: apply: %v", date, err)
		return 0
	}
	log.Printf("[scheduler] carry-over %s: %d instances trazidas da diária %s", date, len(plan), prev)
	return len(plan)
}

// applyCarry grava as viradas numa transação: avança order_date, atualiza o
// orçamento/origem, re-arma o watchdog (carried_at) e registra o evento.
func (s *Scheduler) applyCarry(date string, plan []carriedInstance) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	upd, err := tx.Prepare(
		`UPDATE instances SET order_date=?, carry_budget=?, carried_from=?, carried_at=CURRENT_TIMESTAMP WHERE id=?`,
	)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer upd.Close()
	evt, err := tx.Prepare(`INSERT INTO instance_events(instance_id, kind, actor, message) VALUES(?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	defer evt.Close()

	for _, c := range plan {
		if _, err := upd.Exec(date, c.budget, c.from, c.id); err != nil {
			log.Printf("[scheduler] carry update %s: %v", c.id, err)
			continue
		}
		_, _ = evt.Exec(c.id, "carried", "scheduler",
			fmt.Sprintf("carry-over para %s (%s, desde %s, budget=%d)", date, c.reason, c.from, c.budget))
	}
	return tx.Commit()
}

// RunDaily materializa instances WAITING para todas as defs habilitadas.
// Idempotente: se já existe instance para (def, date), é pulada.
// Cada instance carrega o SHA do commit que originou as defs (Opção B).
//
// Escala (P1): em vez de O(N) round-trips (COUNT por def + INSERT autocommit por
// linha + evento por instance), faz UMA query set-based de existência, decide o
// gating em memória e grava em LOTE por transação com prepared statements — 1
// commit por chunk em vez de 1 fsync por linha. Leva a daily de ~1k inst/s para
// dezenas/centenas de milhares por segundo (ver TestScale_RunDaily).
func (s *Scheduler) RunDaily(date string) int {
	_, span := telemetry.Span(context.Background(), "scheduler.daily", attribute.String("order_date", date))
	defer span.End()
	s.mu.Lock()
	defs := make([]domain.JobDefinition, len(s.defs))
	copy(defs, s.defs)
	s.mu.Unlock()

	commitSHA := s.currentCommitSHA()

	// 0) Ciclo de vida da daily (Control-M New Day): traz da diária anterior as
	// instances que sobrevivem à virada (RUNNING/HELD/NOTOK-não-tratado/keepActive)
	// AVANÇANDO seu order_date para hoje. Roda ANTES da existência: assim uma ordem
	// carregada conta como "já existe" para sua def e o passo 2 NÃO cria uma fresca
	// duplicada. Idempotente (re-rodar não re-move).
	carried := s.carryOver(date)

	// 1) Existência em UMA query (set-based), não um COUNT(*) por def.
	existing := make(map[string]struct{})
	if rows, err := s.db.Query("SELECT definition_id FROM instances WHERE order_date=?", date); err == nil {
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				existing[id] = struct{}{}
			}
		}
		rows.Close()
	} else {
		log.Printf("[scheduler] daily %s: existência: %v", date, err)
	}

	// 2) Gating (schedule + calendars) decidido em MEMÓRIA — sem tocar o banco.
	var t time.Time
	if s.calStore != nil {
		t, _ = time.Parse("2006-01-02", date)
	}
	batch := make([]pendingInstance, 0, len(defs))
	for _, d := range defs {
		if !d.Schedule.Enabled {
			continue
		}
		if _, dup := existing[d.ID]; dup {
			continue
		}
		// Recorrência estruturada + calendars include/exclude (Control-M-like).
		// Sem calStore atachado, cai no gating só por frequência.
		if s.calStore != nil && !IsScheduledOn(d, t, s.calStore) {
			continue
		}
		snap, _ := json.Marshal(d) // Fase A: congela a def no momento da ordem.
		batch = append(batch, pendingInstance{
			id: d.ID + "-" + date, defID: d.ID, team: d.Team,
			scheduledAt: computeScheduledAt(d, date), snapshot: string(snap),
		})
	}

	// 3) Grava em lote (transação + prepared statements, chunked).
	created := s.insertDailyBatch(date, commitSHA, batch)

	_, _ = s.db.Exec("INSERT OR REPLACE INTO daily_runs(order_date, started_at) VALUES(?, CURRENT_TIMESTAMP)", date)
	s.hub.BroadcastWeb("daily.started", map[string]interface{}{"orderDate": date, "created": created, "carried": carried, "commitSha": commitSHA})
	log.Printf("[scheduler] daily %s: %d instances created, %d carried (commit=%s)", date, created, carried, short(commitSHA))
	return created
}

// insertDailyBatch grava as instances decididas em LOTE, uma transação por chunk
// (dailyBatchChunk). Cada chunk é atômico; entre chunks, re-rodar é idempotente
// (a existência set-based pula o que já entrou). Retorna o total inserido.
func (s *Scheduler) insertDailyBatch(date, commitSHA string, batch []pendingInstance) int {
	created := 0
	for start := 0; start < len(batch); start += dailyBatchChunk {
		end := min(start+dailyBatchChunk, len(batch))
		created += s.insertDailyChunk(date, commitSHA, batch[start:end])
	}
	return created
}

// insertDailyChunk insere um chunk numa única transação com prepared statements
// (instance + evento "ordered"). 1 commit por chunk — sem fsync por linha.
func (s *Scheduler) insertDailyChunk(date, commitSHA string, chunk []pendingInstance) int {
	if len(chunk) == 0 {
		return 0
	}
	tx, err := s.db.Begin()
	if err != nil {
		log.Printf("[scheduler] daily %s: begin tx: %v", date, err)
		return 0
	}
	insStmt, err := tx.Prepare(`INSERT INTO instances(id, definition_id, team, order_date, status, scheduled_at, definition_commit_sha, definition_snapshot) VALUES(?,?,?,?,?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		log.Printf("[scheduler] daily %s: prepare insert: %v", date, err)
		return 0
	}
	defer insStmt.Close()
	evtStmt, err := tx.Prepare(`INSERT INTO instance_events(instance_id, kind, actor, message) VALUES(?,?,?,?)`)
	if err != nil {
		_ = tx.Rollback()
		log.Printf("[scheduler] daily %s: prepare event: %v", date, err)
		return 0
	}
	defer evtStmt.Close()

	created := 0
	for _, p := range chunk {
		if _, err := insStmt.Exec(p.id, p.defID, p.team, date, string(domain.StatusWaiting), p.scheduledAt, commitSHA, p.snapshot); err != nil {
			log.Printf("[scheduler] insert %s: %v", p.id, err)
			continue
		}
		msg := fmt.Sprintf("daily order_date=%s scheduled=%s", date, p.scheduledAt.Format(time.RFC3339))
		if commitSHA != "" {
			msg += " commit=" + short(commitSHA)
		}
		_, _ = evtStmt.Exec(p.id, "ordered", "scheduler", msg)
		created++
	}
	if err := tx.Commit(); err != nil {
		log.Printf("[scheduler] daily %s: commit: %v", date, err)
		return 0
	}
	return created
}

func computeScheduledAt(d domain.JobDefinition, date string) time.Time {
	t, _ := time.Parse("2006-01-02", date)
	if d.Schedule.RunAt != "" {
		parts := strings.Split(d.Schedule.RunAt, ":")
		if len(parts) == 2 {
			hh, _ := strconv.Atoi(parts[0])
			mm, _ := strconv.Atoi(parts[1])
			return time.Date(t.Year(), t.Month(), t.Day(), hh, mm, 0, 0, time.Local)
		}
	}
	return t // imediatamente
}

type instRow struct {
	ID, DefID, OrderDate, Status string
	ScheduledAt                  time.Time
	StartedAt                    sql.NullTime
	CarriedAt                    sql.NullTime // virada da daily: re-arma o watchdog de stuck-running.
	Forced                       bool
	Snapshot                     string // Fase A: def congelada na ordem (JSON); "" em instances legadas.
}

// defForInstance devolve a definition CONGELADA no momento da ordem (snapshot),
// caindo na def viva só para instances legadas sem snapshot. Garante que uma
// mudança publicada no Design durante o dia NÃO altere o que a instance de hoje
// executa (imutabilidade Control-M-like).
func defForInstance(r instRow, live map[string]domain.JobDefinition) (domain.JobDefinition, bool) {
	if r.Snapshot != "" {
		var d domain.JobDefinition
		if err := json.Unmarshal([]byte(r.Snapshot), &d); err == nil {
			return d, true
		}
	}
	d, ok := live[r.DefID]
	return d, ok
}

// statusRank — quanto maior, mais "definitivo" / determinante para evalDeps.
// Permite agregar múltiplas instances da mesma definition no mesmo dia
// (daily + forced) escolhendo a que melhor representa o estado da deps.
//
//	OK > NOTOK > CANCELLED > RUNNING > HOLD > WAITING > unknown
func statusRank(s string) int {
	switch s {
	case string(domain.StatusOK):
		return 6
	case string(domain.StatusNotOK):
		return 5
	case string(domain.StatusCancelled):
		return 4
	case string(domain.StatusRunning):
		return 3
	case string(domain.StatusHeld):
		return 2
	case string(domain.StatusWaiting):
		return 1
	}
	return 0
}

// stuckRunningTimeout — RUNNING sem FinishInstance por mais que isto vira NOTOK
// com output de timeout. Cobre desconexão de agent ou mock que falhou.
const stuckRunningTimeout = 15 * time.Minute

func (s *Scheduler) tickOnce() {
	now := time.Now()
	today := now.Format("2006-01-02")

	// Carrega TODAS as instances do dia (não só WAITING/RUNNING).
	// evalDeps precisa enxergar pais OK/NOTOK para decidir corretamente.
	rows, err := s.db.Query(
		`SELECT id, definition_id, order_date, status, scheduled_at,
		        started_at, carried_at, COALESCE(forced,0), COALESCE(definition_snapshot,'')
		 FROM instances WHERE order_date=?`,
		today,
	)
	if err != nil {
		return
	}
	insts := []instRow{}
	for rows.Next() {
		var r instRow
		var forcedInt int
		_ = rows.Scan(&r.ID, &r.DefID, &r.OrderDate, &r.Status, &r.ScheduledAt, &r.StartedAt, &r.CarriedAt, &forcedInt, &r.Snapshot)
		r.Forced = forcedInt == 1
		insts = append(insts, r)
	}
	rows.Close()

	s.mu.Lock()
	defs := map[string]domain.JobDefinition{}
	for _, d := range s.defs {
		defs[d.ID] = d
	}
	s.mu.Unlock()

	// Agrega múltiplas instances da mesma def: mantém a "mais determinante"
	// pelo statusRank. Daily + Forced no mesmo dia não se sobrescrevem
	// arbitrariamente; a decisão é estável.
	instByDef := map[string]instRow{}
	for _, r := range insts {
		prev, ok := instByDef[r.DefID]
		if !ok || statusRank(r.Status) > statusRank(prev.Status) {
			instByDef[r.DefID] = r
		}
	}

	for _, r := range insts {
		// Watchdog: RUNNING parado por muito tempo vira NOTOK (timeout). Um job
		// carregado na virada da daily (carried_at) re-arma o relógio: mede-se a
		// staleness de max(started_at, carried_at), pra um RUNNING legítimo que
		// atravessa a virada não ser reapado no instante em que aparece no novo dia.
		if r.Status == string(domain.StatusRunning) && r.StartedAt.Valid {
			anchor := r.StartedAt.Time
			if r.CarriedAt.Valid && r.CarriedAt.Time.After(anchor) {
				anchor = r.CarriedAt.Time
			}
			if now.Sub(anchor) > stuckRunningTimeout {
				s.emitEvent(r.ID, "timeout", "scheduler", fmt.Sprintf("RUNNING > %s without agent result", stuckRunningTimeout))
				s.FinishInstance(r.ID, domain.StatusNotOK, -1,
					fmt.Sprintf("(timeout: RUNNING > %s without agent result)", stuckRunningTimeout))
				continue
			}
		}
		if r.Status != string(domain.StatusWaiting) {
			continue
		}
		if now.Before(r.ScheduledAt) {
			continue
		}
		def, ok := defForInstance(r, defs)
		if !ok {
			continue
		}
		// Forced bypassa deps (Control-M parity).
		if r.Forced {
			s.startInstance(r.ID, def)
			continue
		}
		// Gating pela FONTE ÚNICA (gateInstance) — o MESMO avaliador que o Explain
		// usa pra dizer o porquê. short-circuit no 1º bloqueio (hot path). Nenhum
		// gate bloqueia o dispatch sem aparecer aqui, então o Explain nunca diverge.
		if blockers := s.gateInstance(r, def, instByDef, now, true); len(blockers) > 0 {
			// Dep permanentemente impossível → CANCELLED (o 1º bloqueio é o
			// determinante por causa do short-circuit, na mesma ordem do gate).
			if blockers[0].Kind == GateDepBlocked {
				_, _ = s.db.Exec(`UPDATE instances SET status=?, finished_at=CURRENT_TIMESTAMP WHERE id=?`,
					string(domain.StatusCancelled), r.ID)
				s.emitEvent(r.ID, "cancelled", "scheduler", "upstream condition permanently unsatisfiable")
				s.hub.BroadcastWeb("instance.changed", map[string]string{"id": r.ID, "status": string(domain.StatusCancelled)})
			}
			continue
		}
		// Gates read-only passaram → reserva ATÔMICA do recurso (a única etapa com
		// efeito colateral; o gate só fez Shortfalls read-only) e dispara.
		if len(def.Resources) > 0 && s.resources != nil {
			if !s.resources.TryAcquire(r.ID, def.Resources) {
				continue
			}
		}
		s.startInstance(r.ID, def)
	}

	// F19 — SLA evaluation per tick
	if s.sla != nil {
		defsByID := map[string]domain.JobDefinition{}
		for _, d := range s.defs {
			defsByID[d.ID] = d
		}
		s.sla.Evaluate(defsByID, now)
	}

	// Actions/On-Do — dimensão "runtime": shouts por duração nas instances RUNNING.
	s.evaluateRuntimeActions(now)
}

// evalDeps avalia todas as upstreams; retorna (ready, permanentlyBlocked).
// Thin loop sobre edgeState (a regra por-aresta, fonte única compartilhada com o
// Explain). Para na 1ª aresta não-satisfeita — mesma semântica de short-circuit
// de antes (uma espera adiante mascara um bloqueio permanente posterior).
func (s *Scheduler) evalDeps(def domain.JobDefinition, insts map[string]instRow) (bool, bool) {
	for _, u := range def.Upstream {
		up, exists := insts[u.From]
		sat, permanent := edgeState(u.Condition, up.Status, exists)
		if permanent {
			return false, true
		}
		if !sat {
			return false, false
		}
	}
	return true, false
}

func (s *Scheduler) startInstance(id string, def domain.JobDefinition) {
	s.mu.Lock()
	if s.running[id] {
		s.mu.Unlock()
		return
	}
	s.running[id] = true
	s.mu.Unlock()

	// Claim atômico: só dispara se a instance AINDA está WAITING. Garante no
	// máximo UMA execução por janela WAITING mesmo com múltiplas vias de
	// dispatch chamando startInstance (tick + ForceOrder direto + retry).
	// Sem isso (P15): o AfterFunc tardio do maybeRetry re-rodava uma instance
	// já terminal e o tick + AfterFunc disparavam em dobro → retries=2 rodava
	// 5x em vez de 3.
	res, err := s.db.Exec(
		`UPDATE instances SET status=?, started_at=CURRENT_TIMESTAMP WHERE id=? AND status=?`,
		string(domain.StatusRunning), id, string(domain.StatusWaiting),
	)
	claimed := false
	if err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			claimed = true
		}
	}
	if !claimed {
		// Outra via já reivindicou esta instance (ou ela não está mais WAITING).
		s.mu.Lock()
		delete(s.running, id)
		s.mu.Unlock()
		return
	}
	s.emitEvent(id, "started", "scheduler", "")
	s.hub.BroadcastWeb("instance.changed", map[string]string{"id": id, "status": string(domain.StatusRunning)})

	go func() {
		// R2 — panic-recovery por dispatch: um panic aqui (executor, hub, codec)
		// NÃO derruba o processo. Libera o running-guard e finaliza a instance
		// como NOTOK para ela não ficar pendurada em RUNNING.
		defer func() {
			s.mu.Lock()
			delete(s.running, id)
			s.mu.Unlock()
			if r := recover(); r != nil {
				log.Printf("[scheduler] PANIC no dispatch de %s recuperado: %v", id, r)
				s.FinishInstance(id, domain.StatusNotOK, -1, fmt.Sprintf("(panic no dispatch: %v)", r))
			}
		}()

		_, span := telemetry.Span(context.Background(), "scheduler.dispatch",
			attribute.String("instance", id), attribute.String("job_type", def.JobType))
		defer span.End()

		// C1 — SSH é agentless: roda no próprio server (shell-out pro ssh),
		// sem precisar de agente instalado no alvo.
		if strings.EqualFold(def.JobType, "SSH") {
			s.runSSH(id, def)
			return
		}

		payload := map[string]interface{}{
			"event":      "dispatch",
			"instanceId": id,
			"jobType":    def.JobType,
			"params":     InterpolateParams(def.Params, s.buildVarContext(def, id)),
			"timeout":    def.Timeout,
		}
		raw, _ := json.Marshal(payload)
		// Dispatch via Bus: local (hub) ou roteado ao nó dono do agent (bus NATS).
		// Só o líder chega aqui (tick leader-gated) → a escolha do agent é feita por
		// um decisor único, sem dupla-execução entre nós.
		switch out, agentID := s.hub.Dispatch(def.AgentID, def.JobType, raw); out {
		case hub.DispatchNoAgent:
			// Mock finaliza em 1s para não bloquear o scheduler.
			s.emitEvent(id, "submitted", "scheduler", "no agent online — mock")
			time.Sleep(1 * time.Second)
			s.FinishInstance(id, domain.StatusOK, 0, "(no agent online, mocked)")
		case hub.DispatchQueueFull:
			s.emitEvent(id, "submitted", "scheduler", "agent queue full")
			s.FinishInstance(id, domain.StatusNotOK, -1, "agent queue full")
		default: // hub.DispatchSent
			_, _ = s.db.Exec(`UPDATE instances SET agent_id=? WHERE id=?`, agentID, id)
			s.emitEvent(id, "submitted", "scheduler", "dispatched to agent:"+agentID)
		}
		// O resultado chega de forma assíncrona via ws handler -> FinishInstance.
	}()
}

// FinishInstance — chamado pelo ws handler quando o agent publica "result".
func (s *Scheduler) FinishInstance(id string, status domain.InstanceStatus, exitCode int, output string) {
	// Actions/On-Do — dimensão "attempt": a tentativa que ACABOU de rodar falhou.
	// Avaliada ANTES do retry, para cobrir TODA tentativa falha (inclusive a final).
	if status == domain.StatusNotOK {
		if def, orderDate, attempt, ok := s.instanceContext(id); ok && len(def.Actions) > 0 {
			s.applyActions(id, orderDate, def, actionEvent{kind: "attempt", attempt: attempt})
		}
	}
	// P15 — retry de execution: se falhou e ainda há tentativas (def.Retries),
	// re-dispatcha em vez de finalizar como NOTOK.
	if status == domain.StatusNotOK && s.maybeRetry(id, output) {
		return
	}
	_, _ = s.db.Exec(
		`UPDATE instances SET status=?, exit_code=?, output=?, finished_at=CURRENT_TIMESTAMP WHERE id=?`,
		string(status), exitCode, output, id,
	)
	// F15 — release resources holdings
	if s.resources != nil {
		s.resources.Release(id)
	}
	// F16 — emit ConditionsOut on OK
	if status == domain.StatusOK && s.conditions != nil {
		var defID, orderDate string
		_ = s.db.QueryRow(`SELECT definition_id, order_date FROM instances WHERE id=?`, id).Scan(&defID, &orderDate)
		s.mu.Lock()
		var def domain.JobDefinition
		for _, d := range s.defs {
			if d.ID == defID {
				def = d
				break
			}
		}
		s.mu.Unlock()
		if def.ID != "" {
			s.conditions.ApplyOutcomes(def, orderDate, "scheduler")
		}
	}
	s.emitEvent(id, "finished", "agent", fmt.Sprintf("status=%s exit=%d", status, exitCode))
	s.hub.BroadcastWeb("instance.changed", map[string]interface{}{
		"id": id, "status": string(status), "exitCode": exitCode,
	})
	// Phase 8 — avalia regras de alerta na transição terminal (retries já
	// esgotados neste ponto). Best-effort; nunca quebra o fluxo de finish.
	if s.alerts != nil && (status == domain.StatusOK || status == domain.StatusNotOK) {
		s.alerts.Evaluate(s.buildAlertContext(id, status))
	}
	// Actions/On-Do — dimensão "result" (terminal): OK ou NOTOK com retries
	// esgotados. Lê a def CONGELADA da instance (snapshot do momento da ordem).
	if status == domain.StatusOK || status == domain.StatusNotOK {
		if def, orderDate, _, ok := s.instanceContext(id); ok && len(def.Actions) > 0 {
			s.applyActions(id, orderDate, def, actionEvent{kind: "result", status: status})
		}
	}
}

// buildAlertContext monta o AlertContext a partir da instance finalizada e do
// histórico da mesma definition (success rate / falhas consecutivas).
func (s *Scheduler) buildAlertContext(id string, status domain.InstanceStatus) AlertContext {
	var defID, snapshot string
	var attempts int
	var startedAt, finishedAt sql.NullTime
	_ = s.db.QueryRow(
		`SELECT definition_id, COALESCE(definition_snapshot,''), COALESCE(attempts,1), started_at, finished_at
		 FROM instances WHERE id=?`, id,
	).Scan(&defID, &snapshot, &attempts, &startedAt, &finishedAt)

	ctx := AlertContext{
		WorkflowID:        defID,
		WorkflowName:      defID,
		Status:            string(status),
		MaxJobRetries:     attempts - 1,
		RecentSuccessRate: 1,
	}
	if ctx.MaxJobRetries < 0 {
		ctx.MaxJobRetries = 0
	}
	if startedAt.Valid && finishedAt.Valid {
		ctx.DurationMs = finishedAt.Time.Sub(startedAt.Time).Milliseconds()
	}
	// label do snapshot congelado, se houver.
	if snapshot != "" {
		var def domain.JobDefinition
		if json.Unmarshal([]byte(snapshot), &def) == nil && def.Label != "" {
			ctx.WorkflowName = def.Label
		}
	}

	// Histórico terminal da definition (ordem cronológica).
	rows, err := s.db.Query(
		`SELECT status FROM instances
		 WHERE definition_id=? AND status IN (?,?) AND finished_at IS NOT NULL
		 ORDER BY finished_at ASC`,
		defID, string(domain.StatusOK), string(domain.StatusNotOK),
	)
	if err == nil {
		defer rows.Close()
		var hist []string
		for rows.Next() {
			var st string
			if rows.Scan(&st) == nil {
				hist = append(hist, st)
			}
		}
		// success rate sobre a janela das últimas 10.
		window := hist
		if len(window) > 10 {
			window = window[len(window)-10:]
		}
		if len(window) > 0 {
			ok := 0
			for _, st := range window {
				if st == string(domain.StatusOK) {
					ok++
				}
			}
			ctx.RecentSuccessRate = float64(ok) / float64(len(window))
		}
		// falhas consecutivas no fim do histórico.
		for i := len(hist) - 1; i >= 0; i-- {
			if hist[i] == string(domain.StatusNotOK) {
				ctx.ConsecutiveFailures++
			} else {
				break
			}
		}
	}
	return ctx
}

// maybeRetry re-dispatcha a instance se ainda há tentativas (def.Retries do
// snapshot imutável). Retorna true se agendou retry (FinishInstance não finaliza).
// O slot de resource NÃO é liberado entre tentativas (segue reservado).
func (s *Scheduler) maybeRetry(id, output string) bool {
	var attempts int
	var snapshot string
	if err := s.db.QueryRow(`SELECT COALESCE(attempts,1), COALESCE(definition_snapshot,'') FROM instances WHERE id=?`, id).Scan(&attempts, &snapshot); err != nil {
		return false
	}
	if snapshot == "" {
		return false
	}
	var def domain.JobDefinition
	if err := json.Unmarshal([]byte(snapshot), &def); err != nil {
		return false
	}
	maxAttempts := def.Retries + 1
	if def.Retries <= 0 || attempts >= maxAttempts {
		return false
	}
	next := attempts + 1
	_, _ = s.db.Exec(
		`UPDATE instances SET attempts=?, status=?, exit_code=NULL, finished_at=NULL WHERE id=?`,
		next, string(domain.StatusWaiting), id,
	)
	s.emitEvent(id, "retry", "scheduler", fmt.Sprintf("falhou — retry %d/%d", next, maxAttempts))
	s.hub.BroadcastWeb("instance.changed", map[string]interface{}{"id": id, "status": string(domain.StatusWaiting)})
	// backoff curto antes de re-dispatchar (running[id] já foi liberado).
	// R2 — recover: o timer roda em goroutine própria; um panic na parte síncrona
	// de startInstance (antes de spawnar a dela) não pode derrubar o processo.
	time.AfterFunc(5*time.Second, func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[scheduler] PANIC no retry de %s recuperado: %v", id, r)
			}
		}()
		s.startInstance(id, def)
	})
	return true
}

// SetOK — flip operacional NOTOK/CANCELLED -> OK (Control-M "Set OK").
// Único gatilho que destrava sucessores on-success a partir de um pai falho.
// Preserva output original com prefixo de auditoria.
func (s *Scheduler) SetOK(id string) error {
	var status, output string
	err := s.db.QueryRow(`SELECT status, COALESCE(output,'') FROM instances WHERE id=?`, id).Scan(&status, &output)
	if err != nil {
		return fmt.Errorf("instance %s not found", id)
	}
	if status != string(domain.StatusNotOK) && status != string(domain.StatusCancelled) {
		return fmt.Errorf("instance %s is %s; Set OK only valid for NOTOK/CANCELLED", id, status)
	}
	newOutput := "[set-ok by operator at " + time.Now().Format(time.RFC3339) + "]\n" + output
	_, err = s.db.Exec(
		`UPDATE instances SET status=?, exit_code=0, output=?, finished_at=COALESCE(finished_at, CURRENT_TIMESTAMP) WHERE id=?`,
		string(domain.StatusOK), newOutput, id,
	)
	if err != nil {
		return err
	}
	s.emitEvent(id, "set-ok", "operator", fmt.Sprintf("flipped from %s to OK", status))
	s.hub.BroadcastWeb("instance.changed", map[string]interface{}{
		"id": id, "status": string(domain.StatusOK), "setOk": true,
	})
	return nil
}

// ForceOrder cria uma instance já em RUNNING ignorando cron e deps.
// Equivale ao "Order Force" do Control-M.
func (s *Scheduler) ForceOrder(defID string) (string, error) {
	s.mu.Lock()
	var def *domain.JobDefinition
	for i := range s.defs {
		if s.defs[i].ID == defID {
			d := s.defs[i]
			def = &d
			break
		}
	}
	s.mu.Unlock()
	if def == nil {
		return "", fmt.Errorf("definition %s not found", defID)
	}
	commitSHA := s.currentCommitSHA()
	today := time.Now().Format("2006-01-02")
	id := defID + "-FORCE-" + time.Now().Format("150405")
	snap, _ := json.Marshal(*def) // Fase A: congela a def no momento da ordem manual.
	_, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, team, order_date, status, scheduled_at, forced, definition_commit_sha, definition_snapshot) VALUES(?,?,?,?,?,?,1,?,?)`,
		id, defID, def.Team, today, string(domain.StatusWaiting), time.Now(), commitSHA, string(snap),
	)
	if err != nil {
		return "", err
	}
	msg := "order_date=" + today
	if commitSHA != "" {
		msg += " commit=" + short(commitSHA)
	}
	s.emitEvent(id, "force-ordered", "operator", msg)
	s.hub.BroadcastWeb("instance.changed", map[string]interface{}{"id": id, "status": "WAITING", "forced": true})
	go s.startInstance(id, *def)
	return id, nil
}
