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

	// DemoMode — SEM agente online: true = mock-finaliza OK (demo/playground);
	// false (default, honesto) = a instance volta pra WAITING e o tick re-tenta
	// quando um agente com a capability conectar. Flag -demo-mode no main.
	DemoMode bool

	// noAgentAt — throttle do evento "no agent online" por instance (sem isso o
	// tick de 2s inundaria instance_events enquanto não houver agente).
	noAgentAt map[string]time.Time

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

	// Ciclo de vida das goroutines de fundo (dispatch, mock-finish do DemoMode,
	// backoff de retry). Stop() fecha quit e espera as em voo — sem isso um write
	// tardio (ex.: o mock-finish de 1s do DemoMode) escapa do teardown do teste e
	// corre com o RemoveAll do t.TempDir() ("directory not empty"), o flake
	// recorrente da CI. Em produção o Run(ctx) não precisa chamar Stop.
	quit     chan struct{}
	wg       sync.WaitGroup
	stopOnce sync.Once

	// === E1 (2026-07-07) — timezone da daily ===
	// nowFn é o relógio-fonte, injetável nos testes (default time.Now); o "agora"
	// de negócio é sempre nowFn().In(loc do settings.daily_timezone). tzName/tzLoc
	// cacheiam o time.LoadLocation por NOME — mudar o setting recarrega no próximo
	// uso, sem restart; nome inválido loga UMA vez e cai no relógio local.
	nowFn  func() time.Time
	tzMu   sync.Mutex
	tzName string
	tzLoc  *time.Location

	// E4 — fila assíncrona de eventos (nil = desligada, write síncrono).
	// Ligada por StartEventQueue() no modo internal; ver eventqueue.go.
	eventCh chan eventRec

	// E5 — throttle do check "daily fechou? manda o relatório" (1×/min no tick).
	lastReportCheck time.Time

	// ARCH-3 — guarda de ticks sobrepostos (camada em-processo sempre; advisory
	// cross-processo opt-in via EnableTickLock). Ver ticklock.go.
	tickGuard *tickGuard

	// Slow Execution pela MÉDIA histórica (2026-07-17, ver slowalert.go):
	// slowFired deduplica o alerta por RUN (cada instance alerta no máximo uma
	// vez, seja durante a execução ou no término); slowAvg cacheia a média por
	// definition com TTL curto pra varredura das RUNNING ficar barata no tick.
	slowMu    sync.Mutex
	slowFired map[string]bool
	slowAvg   map[string]slowAvgEntry
}

// Bus — Fase 2 (futuro serverless): transporte do control-plane. Abstrai a
// difusão de eventos para a web e o roteamento de dispatch para agentes, de
// modo que o WebSocket hub (default) possa ser trocado por NATS/SSE/long-poll
// sem tocar no core do scheduler (ver docs/arquitetura-futuro.md). *hub.Hub
// satisfaz esta interface; o tipo hub.Client segue compartilhado por ser o
// canal de envio concreto para o agente.
type Bus interface {
	BroadcastWeb(event string, payload interface{})
	PickAgent(capability, env string) *hub.Client
	GetAgent(id string) *hub.Client
	// Dispatch entrega o payload a um agent — local (hub) ou, no bus distribuído
	// (R5/NATS), roteado ao nó dono do agent. `env` = def.Environment (ADV-2:
	// roteamento por ambiente — hub.EnvMatch). Retorna o resultado e o id escolhido.
	Dispatch(agentID, capability, env string, raw []byte) (hub.DispatchOutcome, string)
	// HasAgent — há agente (local ou remoto) capaz de receber este dispatch agora?
	// O tick usa ANTES do claim: sem agente, a instance fica WAITING (WAIT AGENT)
	// sem churn de estado — nada de RUNNING↔WAITING piscando a cada tick.
	HasAgent(agentID, capability, env string) bool
}

// Leader — G1 HA. Só o nó líder roda a daily automática e o tick de dispatch;
// os demais nós servem API normalmente. Implementações em internal/leader:
// SingleNode (sempre líder — SQLite/nó único) e PgAdvisory (advisory lock no
// Postgres para vários nós). O claim atômico de startInstance (fix do P15) já
// garante no máx. 1 execução por instance mesmo se dois nós despacharem.
type Leader interface{ IsLeader() bool }

func New(store *storage.FileStore, db *db.DB, bus Bus, tick time.Duration) *Scheduler {
	s := &Scheduler{
		store:     store,
		db:        db,
		hub:       bus,
		tick:      tick,
		running:   map[string]bool{},
		noAgentAt: map[string]time.Time{},
		settings:  Settings{DailyAt: "00:00", Timezone: "America/Sao_Paulo"},
		quit:      make(chan struct{}),
		nowFn:     time.Now,
		slowFired: map[string]bool{},
		slowAvg:   map[string]slowAvgEntry{},
	}
	// ARCH-3 — guard sempre presente (camada em-processo). A camada cross-processo
	// no Postgres é opt-in via EnableTickLock (modo serverless).
	s.tickGuard = &tickGuard{db: db}
	// O pool de condições é o modelo ÚNICO de dependência (2026-07-17) — não é
	// mais uma engine opcional "F16": nasce junto com o scheduler. AttachConditions
	// segue existindo para o main plugar o broadcast (e para testes trocarem).
	s.AttachConditions(NewConditionEngine(db))
	return s
}

// EnableTickLock — ARCH-3: liga a camada CROSS-PROCESSO do lock-por-tick
// (advisory lock no Postgres). Chamado do main no modo -scheduler=external, onde
// não há líder de longa duração segurando o dispatch e dois containers podem
// receber `POST /scheduler/tick` ao mesmo tempo. No-op fora do Postgres. Ver
// ticklock.go / docs/arquitetura-futuro.md §4.
func (s *Scheduler) EnableTickLock() {
	if s.tickGuard == nil {
		s.tickGuard = &tickGuard{db: s.db}
	}
	s.tickGuard.dbLock = true
}

// sleepOrStop dorme d, ou retorna false na hora se Stop() foi chamado — deixa os
// sleeps de fundo (mock-finish do DemoMode, backoff de retry) abortáveis no
// teardown em vez de escreverem no DB depois que o teste/processo já encerrou.
func (s *Scheduler) sleepOrStop(d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-s.quit:
		return false
	}
}

// Stop sinaliza as goroutines de fundo (dispatch/mock-finish/retry) a abortar e
// espera as em voo terminarem. Idempotente. Usado no teardown dos testes (ANTES
// do Close do DB) para garantir que nenhum write sobreviva ao RemoveAll do
// t.TempDir(); também disponível para um shutdown gracioso do processo.
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() { close(s.quit) })
	s.wg.Wait()
}

// === Bloco 2 setters (chamados em main após construir as engines) ===

func (s *Scheduler) AttachCalendars(c *storage.CalendarStore) { s.calStore = c }
func (s *Scheduler) AttachResources(r *ResourceTracker)       { s.resources = r }
// AttachConditions pluga o pool de condições e liga o broadcast de mudanças:
// o painel Condições do Monitoring e as linhas do grafo são um REFLEXO do
// pool, então toda mudança (job OK, Set OK, ação On/Do, operador, migração)
// vira um `condition.changed` no WS.
func (s *Scheduler) AttachConditions(c *ConditionEngine) {
	s.conditions = c
	if c != nil {
		c.OnChange(func(name, scope, kind string) {
			s.hub.BroadcastWeb("condition.changed", map[string]string{
				"name": name, "scope": scope, "kind": kind,
			})
		})
	}
}
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
// orderDate vem da PR\u00d3PRIA instance (n\u00e3o de time.Now()): %%ODATE precisa ser a
// data da ordem mesmo em rerun tardio ou instance carregada da di\u00e1ria anterior.
func (s *Scheduler) buildVarContext(def domain.JobDefinition, instanceID string) VarContext {
	orderDate := time.Now().Format("2006-01-02")
	var od, localJSON string
	if err := s.db.QueryRow(`SELECT order_date, COALESCE(local_vars,'') FROM instances WHERE id=?`, instanceID).Scan(&od, &localJSON); err == nil && od != "" {
		orderDate = od
	}
	ctx := BuildContext(def, instanceID, orderDate, nil, "")
	// CTM-1 — vars locais da instance (%%SETLOCAL): escopo acima da definition,
	// abaixo do runtime; visíveis SÓ pela própria instance.
	if localJSON != "" {
		local := map[string]string{}
		if err := json.Unmarshal([]byte(localJSON), &local); err == nil && len(local) > 0 {
			ctx.Local = local
		}
	}
	if s.variables != nil {
		ctx.Global = s.variables.Snapshot()
	}
	// Offsets de dia ÚTIL (%%ODATE+3B) contam pelo calendar do job (feriados),
	// caindo em Mon–Fri puro sem calendar — mesma régua do schedule/forecast.
	if s.calStore != nil {
		cal := businessCalendar(def, s.calStore)
		ctx.BusinessDay = func(t time.Time) bool { return isBusinessDay(t, cal) }
	}
	return ctx
}

// applySetVarDirectives — SET de variável em runtime (Control-M ctmvar): varre
// o output terminal por linhas "%%SET NOME=VALOR" e grava as globais no
// VariableStore (auditado por evento "set-var" + broadcast variables.changed).
func (s *Scheduler) applySetVarDirectives(id, output string) {
	if s.variables == nil || output == "" || !strings.Contains(output, "%%SET") {
		return
	}
	matches := setVarDirective.FindAllStringSubmatch(output, maxSetVarsPerJob)
	if len(matches) == 0 {
		return
	}
	for _, m := range matches {
		name, value := m[1], m[2]
		if _, err := s.variables.Set(name, value, "job:"+id); err != nil {
			log.Printf("[scheduler] %s: set-var %s: %v", id, name, err)
			continue
		}
		s.emitEvent(id, "set-var", "scheduler", name+"="+value)
	}
	s.hub.BroadcastWeb("variables.changed", map[string]interface{}{"by": id})
}

// applyLocalSetVarDirectives — CTM-1: SET de variável com escopo LOCAL
// ("%%SETLOCAL NOME=VALOR" no output). Persiste em instances.local_vars (JSON):
// o valor vive SÓ na própria instance — lido pela interpolação dos params dela
// (retries/reruns/voltas cyclic) e invisível para qualquer outro job. Aplicado
// a CADA término de tentativa (antes do retry), então uma tentativa falha
// passa estado para a próxima.
func (s *Scheduler) applyLocalSetVarDirectives(id, output string) {
	if output == "" || !strings.Contains(output, "%%SETLOCAL") {
		return
	}
	matches := setLocalVarDirective.FindAllStringSubmatch(output, maxSetVarsPerJob)
	if len(matches) == 0 {
		return
	}
	cur := map[string]string{}
	var raw string
	if err := s.db.QueryRow(`SELECT COALESCE(local_vars,'') FROM instances WHERE id=?`, id).Scan(&raw); err != nil {
		return // instance sumiu — nada a persistir
	}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &cur)
	}
	for _, m := range matches {
		cur[m[1]] = m[2]
		s.emitEvent(id, "set-var-local", "scheduler", m[1]+"="+m[2])
	}
	buf, err := json.Marshal(cur)
	if err != nil {
		return
	}
	if _, err := s.db.Exec(`UPDATE instances SET local_vars=? WHERE id=?`, string(buf), id); err != nil {
		log.Printf("[scheduler] %s: set-var-local: %v", id, err)
	}
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
	// ARCH-3 — lock-por-tick: pula ciclos SOBREPOSTOS (em-processo sempre;
	// cross-nó no Postgres serverless quando ligado). Não é correção (o claim
	// atômico já garante), é higiene. O watchdog acima já marcou lastTickAt: um
	// tick pulado ainda prova que o loop está vivo.
	ok, release := s.tickGuard.tryEnter()
	if !ok {
		return
	}
	defer release()
	if !s.isLeader() {
		return
	}
	s.autoDailyIfDue()
	// E5 — se a daily de hoje fechou (ou bateu daily_report_at), envia o
	// relatório 1× (claim em report_sent_at); throttle interno de 1 min.
	s.maybeSendDailyReport()
	s.tickOnce()
}

// RunDailyIfDue — ARCH-5: materializa a daily de hoje SE já passou do horário e
// ainda não rodou (idempotente). É o MESMO gatilho que o Tick chama a cada
// ciclo, exposto para um cron DIÁRIO dedicado (POST /api/scheduler/daily) no
// modo serverless — separando a cadência da daily (1×/dia) do tick de dispatch
// (segundos). Leader-gated e panic-recuperado como o Tick. Ver
// docs/arquitetura-futuro.md §4.
func (s *Scheduler) RunDailyIfDue() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[scheduler] PANIC no RunDailyIfDue recuperado: %v", r)
		}
	}()
	if !s.isLeader() {
		return
	}
	s.autoDailyIfDue()
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
	// E4 — com a fila ligada, o hot path só enfileira (send não-bloqueante);
	// fila cheia degrada pro INSERT síncrono de sempre — nunca perde evento.
	if ch := s.eventQueue(); ch != nil {
		select {
		case ch <- eventRec{instanceID, kind, actor, message}:
			return
		default: // cheia (eventQueueCap pendentes) — degrada
		}
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

// DailyAt — horário efetivo da daily ("HH:MM"): settings.daily_at (configurável
// em runtime pela UI/API, admin-only) com fallback no default do processo. O
// relógio de referência é SEMPRE o do servidor (time.Now local) — o cliente não
// agenda nada em server mode.
func (s *Scheduler) DailyAt() string {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key='daily_at'`).Scan(&v); err == nil {
		v = strings.TrimSpace(v)
		if _, _, ok := parseHHMM(v); ok {
			return v
		}
		if v != "" {
			log.Printf("[scheduler] settings daily_at %q inválido (esperado HH:MM) — usando %s", v, s.settings.DailyAt)
		}
	}
	return s.settings.DailyAt
}

// DailyTimezone — E1: timezone de NEGÓCIO da daily (settings.daily_timezone,
// nome IANA como "America/Sao_Paulo"). Vazio = relógio local do server
// (comportamento clássico). Devolve o nome CONFIGURADO e a *time.Location
// EFETIVA — nome inválido loga uma vez e cai no local, sem parar a daily.
// O LoadLocation é cacheado por nome: editar o setting na UI recarrega no
// próximo tick, sem restart.
func (s *Scheduler) DailyTimezone() (string, *time.Location) {
	var name string
	_ = s.db.QueryRow(`SELECT value FROM settings WHERE key='daily_timezone'`).Scan(&name)
	name = strings.TrimSpace(name)
	if name == "" {
		return "", time.Local
	}
	s.tzMu.Lock()
	defer s.tzMu.Unlock()
	if name == s.tzName && s.tzLoc != nil {
		return name, s.tzLoc
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		// Cacheia o fallback pelo MESMO nome: sem isso o tick de 2s logaria isto
		// centenas de vezes por minuto. Corrigir o setting muda o nome → recarrega.
		log.Printf("[scheduler] settings daily_timezone %q inválido (%v) — usando o relógio local do server", name, err)
		loc = time.Local
	}
	s.tzName, s.tzLoc = name, loc
	return name, loc
}

// NowLocal — E1: "agora" no relógio de negócio da daily. Toda decisão de
// "hoje"/horário de daily parte daqui (nowFn é injetável nos testes).
func (s *Scheduler) NowLocal() time.Time {
	_, loc := s.DailyTimezone()
	return s.nowFn().In(loc)
}

// TodayDate — E1: a data "de hoje" (YYYY-MM-DD) na timezone da daily. É o
// order_date que uma ordem criada AGORA (auto ou manual) recebe.
func (s *Scheduler) TodayDate() string { return s.NowLocal().Format("2006-01-02") }

// parseHHMM valida "HH:MM" (00:00–23:59).
func parseHHMM(v string) (hh, mm int, ok bool) {
	parts := strings.Split(v, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	hh, errH := strconv.Atoi(parts[0])
	mm, errM := strconv.Atoi(parts[1])
	if errH != nil || errM != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, 0, false
	}
	return hh, mm, true
}

func (s *Scheduler) autoDailyIfDue() {
	// E1 — o relógio de referência é o de NEGÓCIO (settings.daily_timezone; vazio
	// = local do server): `today`, o horário-alvo e o order_date gravado derivam
	// de `now` NESSA location. Server em UTC com negócio em America/Sao_Paulo
	// cruza a meia-noite às 03:00Z e materializa com o order_date de SP.
	now := s.NowLocal()
	today := now.Format("2006-01-02")

	var started sql.NullString
	err := s.db.QueryRow("SELECT started_at FROM daily_runs WHERE order_date=?", today).Scan(&started)
	if err == nil {
		return // já rodou hoje
	}

	hh, mm, ok := parseHHMM(s.DailyAt())
	if !ok {
		return
	}
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
	// E2 — retenção de auditoria: 1×/dia, logo após a daily, só no líder (o
	// caller já é leader-gated). Síncrono de propósito: roda em lotes curtos e
	// uma goroutine solta escreveria no DB depois do teardown (o flake do TempDir).
	s.auditGC()
	// ADV-5 — archives/retention de instances: mesmo racional e mesma janela.
	s.archiveGC()
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
	// dryRun CONGELADO da def no momento da ordem (não é lido da def viva depois).
	// Torna o selo 👻GHOST do Monitoring imutável: mudar dryRun no Design só reflete
	// na próxima ordem. Ver schemaV9 e canvas-layout.buildMonitoringCanvas.
	dryRun bool
	// M1 (schemaV18): label/job_type/confirm_req/environment/pinned_agent/conds_*
	// congelados da def no momento da ordem — o Monitoring inteiro é imutável.
	mcols monitorCols
}

// dailyBatchChunk — tamanho do lote por transação na materialização. Bound o WAL
// do SQLite e dá progresso parcial (re-rodar é idempotente), sem perder o ganho
// do commit único por lote.
const dailyBatchChunk = 5000

// carryPlan — decisão de carry-over de UMA instance na virada da daily.
type carryPlan struct {
	carry  bool
	reason string // p/ o evento "carried" e testes
}

// keepActiveDays — quantas diárias um job sobrevive além do seu ODAT sem
// terminar OK.
// notokDefault=true (caso NOTOK/retry em tratamento) → baseline 1 mesmo sem
// keepActive (um NOTOK não-tratado persiste +1 diária, DEFAULT do Control-M).
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

// carryDecision — REGRA pura do ciclo de vida da daily (Control-M-like).
// Decide se uma instance sobrevive à virada. Testável sem DB.
//
// As idades são em DIAS-CALENDÁRIO (timezone da daily), não em "número de
// viradas": o server desligado num dia NÃO estica a vida de ninguém — um job
// do dia 14 com keepActive=0 não aparece no dia 16 só porque a diária do 15
// não rodou (report do usuário, 2026-07-16).
//
//	ageDays         = hoje − ODAT (origem da ordem; carry não a move)
//	activityAgeDays = hoje − última atividade (falha p/ NOTOK; última execução
//	                  p/ retry pendente; sem timestamp, cai no ODAT)
//
// Regras (na ordem):
//
//	RUNNING → carrega SEMPRE (rastrear até terminar), qualquer idade.
//	HELD    → carrega SEMPRE enquanto em hold, qualquer idade.
//	NOTOK   → atravessa enquanto activityAgeDays ≤ keepActive|1: um NOTOK
//	          não-tratado persiste +1 diária após a FALHA (não após o ODAT:
//	          um RUNNING que atravessou dias e falhou hoje ainda ganha a
//	          diária de amanhã pro operador ver/tratar).
//	WAITING com RETRY AGENDADO em andamento (D-1 retryDelayMin: attempts>1 E
//	          started_at preenchido — rerun de operador zera started_at e NÃO
//	          cai aqui) → é um NOTOK em tratamento; mesma regra do NOTOK.
//	WAITING (incl. aguardando Confirm) → obedece keepActive ESTRITO:
//	          ageDays ≤ keepActive. keepActive=0 morre na primeira virada.
//	OK/CANCELLED/outros → não carrega (encerrado).
func carryDecision(status string, retryPending bool, ageDays, activityAgeDays int, def domain.JobDefinition) carryPlan {
	switch status {
	case string(domain.StatusRunning):
		return carryPlan{carry: true, reason: "running"}
	case string(domain.StatusHeld):
		return carryPlan{carry: true, reason: "held"}
	case string(domain.StatusNotOK):
		if activityAgeDays <= keepActiveDays(def, true) {
			return carryPlan{carry: true, reason: "notok"}
		}
		return carryPlan{carry: false, reason: "notok-expired"}
	case string(domain.StatusWaiting):
		if retryPending {
			if activityAgeDays <= keepActiveDays(def, true) {
				return carryPlan{carry: true, reason: "retry-pending"}
			}
			return carryPlan{carry: false, reason: "retry-expired"}
		}
		if ageDays <= keepActiveDays(def, false) {
			return carryPlan{carry: true, reason: "waiting-keepactive"}
		}
		return carryPlan{carry: false, reason: "waiting-no-keepactive"}
	}
	return carryPlan{carry: false, reason: "terminal"}
}

// carriedInstance — uma instance decidida a carregar pro novo dia.
type carriedInstance struct {
	id     string
	from   string // ODAT de origem (preservado entre múltiplas viradas)
	reason string
}

// carryOver — ciclo de vida da daily (Control-M New Day). ANTES de materializar a
// daily de `date`, traz da diária ANTERIOR (o order_date mais recente < date) as
// instances que sobrevivem à virada (carryDecision, idades em DIAS-CALENDÁRIO a
// partir do ODAT/última atividade — lacunas de New Day contam). A instance
// carregada AVANÇA seu order_date para `date` mantendo ID/status/started_at/
// snapshot/eventos — assim o tick, a API paginada e o RBAC (todos filtram
// order_date) a enxergam no novo dia sem nenhuma mudança. O ODAT (origem) fica
// preservado em carried_from — TODO escopo de data (eventos, conditions, UI)
// usa ele, ver odate.go. BUG-12: a carregada NÃO bloqueia a ordem fresca de
// `date` — o check de existência do RunDaily ignora carried_from≠'' e a daily
// materializa a instance nova da mesma definition ao lado da carregada.
//
// Idempotente: instances já em `date` não estão na diária anterior, então re-rodar
// (botão manual + auto) não move nada duas vezes.
func (s *Scheduler) carryOver(date string) int {
	var prev string
	if err := s.db.QueryRow(
		`SELECT COALESCE(MAX(order_date),'') FROM instances WHERE order_date < ?`, date,
	).Scan(&prev); err != nil || prev == "" {
		return 0
	}

	rows, err := s.db.Query(
		`SELECT id, definition_id, status, COALESCE(carried_from,''),
		        COALESCE(definition_snapshot,''), COALESCE(attempts,1), started_at, finished_at
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

	_, loc := s.DailyTimezone()
	dayOf := func(t sql.NullTime) string {
		if !t.Valid {
			return ""
		}
		return t.Time.In(loc).Format("2006-01-02")
	}

	var plan []carriedInstance
	for rows.Next() {
		var id, defID, status, from, snap string
		var attempts int
		var startedAt, finishedAt sql.NullTime
		if rows.Scan(&id, &defID, &status, &from, &snap, &attempts, &startedAt, &finishedAt) != nil {
			continue
		}
		def, _ := defForInstance(instRow{DefID: defID, Snapshot: snap}, live)
		odate := from
		if odate == "" { // nunca carregada: a origem é a diária de onde ela vem
			odate = prev
		}
		// Última atividade: falha (NOTOK) ou última execução (retry pendente).
		// Sem timestamp, a idade cai no ODAT (conservador: não estica vida).
		activity := dayOf(finishedAt)
		if activity == "" {
			activity = dayOf(startedAt)
		}
		if activity == "" {
			activity = odate
		}
		// Retry AGENDADO em andamento (D-1): já executou (started_at) e o retry
		// re-armou pra frente. Rerun de operador zera started_at e não conta.
		retryPending := attempts > 1 && startedAt.Valid
		d := carryDecision(status, retryPending, daysBetween(odate, date), daysBetween(activity, date), def)
		if !d.carry {
			continue
		}
		plan = append(plan, carriedInstance{id: id, from: odate, reason: d.reason})
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

// applyCarry grava as viradas numa transação: avança order_date, preserva o
// ODAT (carried_from), re-arma o watchdog (carried_at) e registra o evento.
func (s *Scheduler) applyCarry(date string, plan []carriedInstance) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	upd, err := tx.Prepare(
		`UPDATE instances SET order_date=?, carried_from=?, carried_at=CURRENT_TIMESTAMP WHERE id=?`,
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
		if _, err := upd.Exec(date, c.from, c.id); err != nil {
			log.Printf("[scheduler] carry update %s: %v", c.id, err)
			continue
		}
		_, _ = evt.Exec(c.id, "carried", "scheduler",
			fmt.Sprintf("carry-over para %s (%s, ODAT %s)", date, c.reason, c.from))
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
	// AVANÇANDO seu order_date para hoje. BUG-12: a carregada NÃO conta como "já
	// existe" — a fresca do dia entra junto (o passo 1 filtra carried_from).
	// Idempotente (re-rodar não re-move).
	carried := s.carryOver(date)

	// 1) Existência em UMA query (set-based), não um COUNT(*) por def.
	// BUG-12: instances CARREGADAS (carry-over, carried_from≠'') NÃO contam como
	// "já existe" — um job trazido de outro dia (NOTOK em tratamento, RUNNING
	// atravessando a virada, keepActive) não impede a ordem FRESCA de hoje de
	// entrar, como no New Day do Control-M. Só a fresca do dia (carried_from='')
	// bloqueia duplicata — re-rodar a daily segue idempotente.
	existing := make(map[string]struct{})
	if rows, err := s.db.Query("SELECT definition_id FROM instances WHERE order_date=? AND COALESCE(carried_from,'')=''", date); err == nil {
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
			dryRun: d.DryRun,          // congela dryRun (ver pendingInstance).
			mcols:  frozenMonitorCols(d), // M1: congela o resto do que o Monitoring exibe.
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
	insStmt, err := tx.Prepare(`INSERT INTO instances(id, definition_id, team, order_date, status, scheduled_at, definition_commit_sha, definition_snapshot, dry_run,
		label, job_type, confirm_req, environment, pinned_agent, conds_in, conds_out_add, resources) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
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
		if _, err := insStmt.Exec(p.id, p.defID, p.team, date, string(domain.StatusWaiting), p.scheduledAt, commitSHA, p.snapshot, boolToInt(p.dryRun),
			p.mcols.label, p.mcols.jobType, p.mcols.confirmReq, p.mcols.environment, p.mcols.pinned, p.mcols.condsIn, p.mcols.condsOutAdd, p.mcols.resources); err != nil {
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
	// RunAt manda; sem RunAt, a janela (WindowFrom) segura o início — um job
	// cyclic "a cada 10min das 08:00 às 18:00" começa às 08:00, não à meia-noite.
	at := d.Schedule.RunAt
	if at == "" {
		at = d.Schedule.WindowFrom
	}
	if hh, mm, ok := parseHHMM(at); ok {
		return time.Date(t.Year(), t.Month(), t.Day(), hh, mm, 0, 0, time.Local)
	}
	return t // imediatamente
}

type instRow struct {
	ID, DefID, OrderDate, Status string
	ScheduledAt                  time.Time
	StartedAt                    sql.NullTime
	CarriedAt                    sql.NullTime // virada da daily: re-arma o watchdog de stuck-running.
	Forced                       bool
	// ForceMode — como a instance foi forçada (schemaV15): "" = "Run Now"
	// clássico (bypass total de gates); ForceModeOrder = "Order Force" do Design
	// (ordem nova fora do agendamento que RESPEITA os gates de runtime).
	ForceMode string
	Confirmed bool // Control-M Confirm: operador liberou um job confirm:true.
	// CarriedFrom — ODAT de origem quando a instance foi carregada pela virada
	// da daily ("" = fresca do dia; order_date É a origem). Ver odate.go.
	CarriedFrom string
	Snapshot    string // Fase A: def congelada na ordem (JSON); "" em instances legadas.
	// HeldFrom — status congelado por um HOLD (schemaV16, hold geral): o hold
	// vale pra qualquer status não-RUNNING; o release restaura este valor.
	// Só tem significado quando Status=HELD ('' = hold legado, era WAITING).
	HeldFrom string
}

// defForInstance devolve a definition CONGELADA no momento da ordem (snapshot),
// caindo na def viva só para instances legadas sem snapshot. Garante que uma
// mudança publicada no Design durante o dia NÃO altere o que a instance de hoje
// executa (imutabilidade Control-M-like).
//
// Snapshots ordenados ANTES da unificação de condições podem carregar
// `upstream` legado sem as condições explícitas: ExpandSnapshotConditions
// converte o lado consumidor (In + OutRemove) em memória, para o gate destas
// instances continuar valendo no modelo único (o OutAdd do pai vem da def
// viva normalizada — ver applyConditionsOut).
func defForInstance(r instRow, live map[string]domain.JobDefinition) (domain.JobDefinition, bool) {
	if r.Snapshot != "" {
		var d domain.JobDefinition
		if err := json.Unmarshal([]byte(r.Snapshot), &d); err == nil {
			return domain.ExpandSnapshotConditions(d, live), true
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
		        started_at, carried_at, COALESCE(forced,0), COALESCE(force_mode,''), COALESCE(confirmed,0), COALESCE(carried_from,''), COALESCE(definition_snapshot,''), COALESCE(held_from_status,'')
		 FROM instances WHERE order_date=?`,
		today,
	)
	if err != nil {
		return
	}
	insts := []instRow{}
	for rows.Next() {
		var r instRow
		var forcedInt, confirmedInt int
		_ = rows.Scan(&r.ID, &r.DefID, &r.OrderDate, &r.Status, &r.ScheduledAt, &r.StartedAt, &r.CarriedAt, &forcedInt, &r.ForceMode, &confirmedInt, &r.CarriedFrom, &r.Snapshot, &r.HeldFrom)
		r.Forced = forcedInt == 1
		r.Confirmed = confirmedInt == 1
		insts = append(insts, r)
	}
	rows.Close()

	s.mu.Lock()
	defs := map[string]domain.JobDefinition{}
	for _, d := range s.defs {
		defs[d.ID] = d
	}
	s.mu.Unlock()

	// Foto do POOL de condições (modelo único de dependência): uma query por
	// tick, consultada in-memory pelo gate de cada instance WAITING. O pool é
	// pequeno (condições vivas do ambiente); o volume está nas instances.
	var condIdx CondIndex
	if s.conditions != nil {
		condIdx = s.conditions.LoadIndex()
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
		// "Run Now" (forced sem force_mode) bypassa janela/condições/recursos
		// — mas NÃO o agente: sem agente disponível, nem o forced é reivindicado
		// (senão pisca RUNNING↔WAITING). Também NÃO bypassa o Confirm: no Control-M
		// a confirmação é um wait de runtime (o job forçado continua "Wait User"
		// até o operador confirmar). O "Order Force" do Design (force_mode='order')
		// NÃO cai aqui: é uma ordem nova fora do AGENDAMENTO, mas os gates de
		// runtime (condições, agente, recursos, confirm) valem — a cópia forçada
		// de um job cuja condição de entrada já foi CONSUMIDA (deletada por um OK)
		// entra em WAIT COND até alguém recriá-la (rerun do pai, operador, ação).
		if r.Forced && r.ForceMode != ForceModeOrder {
			if def.Confirm && !r.Confirmed {
				continue
			}
			if !s.agentAvailable(def) {
				s.maybeEmitNoAgent(r.ID, def.JobType)
				continue
			}
			s.startInstance(r.ID, def)
			continue
		}
		// Gating pela FONTE ÚNICA (gateInstance) — o MESMO avaliador que o Explain
		// usa pra dizer o porquê. short-circuit no 1º bloqueio (hot path). Nenhum
		// gate bloqueia o dispatch sem aparecer aqui, então o Explain nunca diverge.
		//
		// Paridade Control-M (2026-07-02): condição de entrada que "nunca virá"
		// NÃO cancela o sucessor — ele fica WAITING (WAIT COND) até o operador
		// agir. O flip é reversível na prática: rerun/Set OK de quem cria a
		// condição (ou um set manual no painel) e o tick despacha o sucessor
		// sozinho. Quem nunca ficar elegível morre na virada da daily
		// (WAITING-nunca-rodou não carrega), como no Control-M.
		if blockers := s.gateInstance(r, def, condIdx, now, true); len(blockers) > 0 {
			// Sem agente: registra 1 evento com throttle (5min) pra visibilidade
			// no histórico — o estado NÃO muda (segue WAITING, sem broadcast).
			if blockers[0].Kind == GateAgent {
				s.maybeEmitNoAgent(r.ID, def.JobType)
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

	// Slow Execution — alerta DURANTE a execução quando o decorrido estoura a
	// média histórica do job + folga (rule-slow; ver slowalert.go).
	s.evaluateSlowRunning(now)
}

// agentAvailable — há agente AGORA pra este job? SSH é agentless (roda no
// próprio server) e DemoMode dispensa (mock-finish). Checado ANTES do claim.
// ADV-2: def.Environment roteia — só conta agente do mesmo env (ou coringa).
func (s *Scheduler) agentAvailable(def domain.JobDefinition) bool {
	if s.DemoMode || strings.EqualFold(def.JobType, "SSH") {
		return true
	}
	return s.hub.HasAgent(def.AgentID, def.JobType, def.Environment)
}

// maybeEmitNoAgent — registra o evento "no agent online" com throttle de 5 min
// por instance (o tick roda a cada 2s; sem throttle o event log inundaria).
func (s *Scheduler) maybeEmitNoAgent(id, jobType string) {
	s.mu.Lock()
	last, seen := s.noAgentAt[id]
	now := time.Now()
	if seen && now.Sub(last) < 5*time.Minute {
		s.mu.Unlock()
		return
	}
	s.noAgentAt[id] = now
	s.mu.Unlock()
	s.emitEvent(id, "submitted", "scheduler",
		"no agent online for capability "+jobType+" — waiting (re-tenta a cada tick)")
	log.Printf("[scheduler] %s: sem agente online p/ %s — instance segue WAITING", id, jobType)
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

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
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
		switch out, agentID := s.hub.Dispatch(def.AgentID, def.JobType, def.Environment, raw); out {
		case hub.DispatchNoAgent:
			if s.DemoMode {
				// Demo/playground: mock finaliza em 1s para não bloquear a demo.
				s.emitEvent(id, "submitted", "scheduler", "no agent online — mock (demo-mode)")
				if !s.sleepOrStop(1 * time.Second) {
					return // Stop() pediu parada — não escreve no DB pós-teardown
				}
				s.FinishInstance(id, domain.StatusOK, 0, "(no agent online, mocked — demo-mode)")
				return
			}
			// Produção (default): SEM agente NÃO é sucesso. Reverte o claim —
			// a instance volta pra WAITING e o tick re-tenta a cada ciclo; quando
			// um agente com a capability conectar, despacha de verdade. A frota
			// de agentes já é monitorada pelo selfmon (R7) → alerta operacional.
			_, _ = s.db.Exec(`UPDATE instances SET status=?, started_at=NULL WHERE id=?`,
				string(domain.StatusWaiting), id)
			if len(def.Resources) > 0 && s.resources != nil {
				s.resources.Release(id)
			}
			s.maybeEmitNoAgent(id, def.JobType)
			s.hub.BroadcastWeb("instance.changed", map[string]string{"id": id, "status": string(domain.StatusWaiting)})
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
	// CTM-1 — %%SETLOCAL: aplicado ANTES do retry (diferente do %%SET global,
	// que é terminal-only): a próxima tentativa da MESMA instance já lê o estado.
	s.applyLocalSetVarDirectives(id, output)
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
	// Modelo único de condições: o término OK aplica as saídas — ADICIONA as
	// ConditionsOutAdd ao pool e REMOVE as ConditionsOutRemove. A remoção das
	// condições de ENTRADA (armada automaticamente pela setinha no OutRemove) é
	// o CONSUMO: um rerun depois deste OK volta a esperar, porque a condição que
	// o liberou sumiu do pool. NOTOK não aplica nada — a condição de entrada
	// continua lá e o rerun roda direto (falha não consome).
	if status == domain.StatusOK {
		s.applyConditionsOut(id, "scheduler")
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
	// SET de variável em runtime (Control-M ctmvar): o job ATRIBUI variáveis
	// globais imprimindo "%%SET NOME=VALOR" no output — outro job lê via
	// %%NOME / ${var.NOME}. Varre em OK e NOTOK (a diretiva impressa rodou).
	if status == domain.StatusOK || status == domain.StatusNotOK {
		s.applySetVarDirectives(id, output)
	}
	// Cyclic runtime (Control-M cyclic): terminou OK → re-arma a MESMA instance
	// pra próxima volta (IntervalMin), enquanto a janela/teto permitirem. NOTOK
	// NÃO cicla (espera operador — rerun/Set OK), como no Control-M.
	if status == domain.StatusOK {
		s.maybeCycle(id)
	}
}

// maybeCycle — cyclic runtime: re-arma um job cyclic que terminou OK para rodar
// de novo em IntervalMin minutos. A MESMA instance volta pra WAITING com
// scheduled_at futuro (o gate de janela mostra "próxima volta às HH:MM") e
// attempts resetado (orçamento de retry renova POR VOLTA). O ciclo encerra
// quando: (a) a próxima volta cairia depois de WindowTo; (b) CyclicMaxRuns
// atingido; (c) a daily vira (WAITING nunca-rodou não carrega — a nova daily
// materializa uma instance fresca, como o New Day do Control-M).
func (s *Scheduler) maybeCycle(id string) {
	def, orderDate, _, ok := s.instanceContext(id)
	if !ok || !def.Schedule.Cyclic || def.Schedule.IntervalMin <= 0 {
		return
	}
	var runs int
	if err := s.db.QueryRow(`SELECT COALESCE(cycle_runs,0) FROM instances WHERE id=?`, id).Scan(&runs); err != nil {
		return
	}
	done := runs + 1 // voltas OK completadas, incluindo a que acabou de terminar
	if def.Schedule.CyclicMaxRuns > 0 && done >= def.Schedule.CyclicMaxRuns {
		s.emitEvent(id, "cyclic-done", "scheduler",
			fmt.Sprintf("ciclo encerrado: %d voltas (máx %d)", done, def.Schedule.CyclicMaxRuns))
		return
	}
	next := time.Now().Add(time.Duration(def.Schedule.IntervalMin) * time.Minute)
	if hh, mm, okW := parseHHMM(def.Schedule.WindowTo); okW {
		if t, err := time.Parse("2006-01-02", orderDate); err == nil {
			windowEnd := time.Date(t.Year(), t.Month(), t.Day(), hh, mm, 0, 0, time.Local)
			if next.After(windowEnd) {
				s.emitEvent(id, "cyclic-done", "scheduler",
					fmt.Sprintf("ciclo encerrado: janela até %s fechou (%d voltas)", def.Schedule.WindowTo, done))
				return
			}
		}
	}
	// Guard AND status='OK': se um operador agiu no meio (cancel/hold), não re-arma.
	res, err := s.db.Exec(
		`UPDATE instances SET status=?, scheduled_at=?, cycle_runs=?, attempts=1, exit_code=NULL, finished_at=NULL
		 WHERE id=? AND status=?`,
		string(domain.StatusWaiting), next, done, id, string(domain.StatusOK),
	)
	if err != nil {
		log.Printf("[scheduler] cyclic re-arm %s: %v", id, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return
	}
	s.emitEvent(id, "cyclic", "scheduler",
		fmt.Sprintf("volta %d OK — próxima às %s (intervalo %dmin)", done, next.Format("15:04:05"), def.Schedule.IntervalMin))
	s.hub.BroadcastWeb("instance.changed", map[string]interface{}{
		"id": id, "status": string(domain.StatusWaiting), "cyclic": true,
	})
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
		InstanceID:        id, // D-15: alvo dos quick-action links do alerta
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
	// Slow Execution — média histórica das execuções OK ANTERIORES (a própria
	// instance fica de fora: a run lenta não pode puxar a régua pra cima).
	// SlowFired: já alertou DURANTE a run (tick) → o terminal não repete.
	ctx.AvgDurationMs, ctx.HistoryRuns = s.avgOKDurationMs(defID, id)
	s.slowMu.Lock()
	if s.slowFired[id] {
		ctx.SlowFired = true
		delete(s.slowFired, id)
	}
	s.slowMu.Unlock()
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
	// D-1 — retryDelayMin>0: a próxima tentativa é AGENDADA (scheduled_at futuro)
	// em vez de re-dispatchada por goroutine. O tick despacha quando chegar a hora;
	// como o agendamento vive no DB, sobrevive a restart do server e à virada da
	// daily (o carry-over trata WAITING-que-já-tentou como NOTOK em tratamento).
	// É o que torna "retry após 3 dias" confiável — uma goroutine dormindo 3 dias
	// morreria no primeiro deploy.
	if def.RetryDelayMin > 0 {
		nextAt := time.Now().Add(time.Duration(def.RetryDelayMin) * time.Minute)
		_, _ = s.db.Exec(
			`UPDATE instances SET attempts=?, status=?, scheduled_at=?, exit_code=NULL, finished_at=NULL WHERE id=?`,
			next, string(domain.StatusWaiting), nextAt, id,
		)
		s.emitEvent(id, "retry", "scheduler",
			fmt.Sprintf("falhou — retry %d/%d agendado para %s (retryDelayMin=%d)",
				next, maxAttempts, nextAt.Format("2006-01-02 15:04"), def.RetryDelayMin))
		s.hub.BroadcastWeb("instance.changed", map[string]interface{}{"id": id, "status": string(domain.StatusWaiting)})
		return true
	}
	_, _ = s.db.Exec(
		`UPDATE instances SET attempts=?, status=?, exit_code=NULL, finished_at=NULL WHERE id=?`,
		next, string(domain.StatusWaiting), id,
	)
	s.emitEvent(id, "retry", "scheduler", fmt.Sprintf("falhou — retry %d/%d", next, maxAttempts))
	s.hub.BroadcastWeb("instance.changed", map[string]interface{}{"id": id, "status": string(domain.StatusWaiting)})
	// backoff curto antes de re-dispatchar (running[id] já foi liberado).
	// Goroutine rastreada + sleep abortável (ver Stop/sleepOrStop): sem isso o
	// AfterFunc tardio re-disparava startInstance DEPOIS do teardown do teste,
	// escrevendo no DB durante o RemoveAll do t.TempDir() (flake da CI).
	// R2 — recover: um panic na parte síncrona de startInstance não pode derrubar
	// o processo.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[scheduler] PANIC no retry de %s recuperado: %v", id, r)
			}
		}()
		if !s.sleepOrStop(5 * time.Second) {
			return // Stop() — aborta o backoff sem re-disparar pós-teardown
		}
		s.startInstance(id, def)
	}()
	return true
}

// SetOK — flip operacional -> OK (Control-M "Set OK"). Vale para NOTOK/
// CANCELLED (destravar sucessores a partir de um pai falho) e, desde o BUG-3,
// também para WAITING — um job preso em WAIT EVENT pode ser concluído OK na
// hora, sem esperar o evento chegar. Preserva output original com prefixo de
// auditoria.
func (s *Scheduler) SetOK(id string) error {
	var status, output string
	err := s.db.QueryRow(`SELECT status, COALESCE(output,'') FROM instances WHERE id=?`, id).Scan(&status, &output)
	if err != nil {
		return fmt.Errorf("instance %s not found", id)
	}
	switch status {
	case string(domain.StatusNotOK), string(domain.StatusCancelled), string(domain.StatusWaiting):
	default:
		return fmt.Errorf("instance %s is %s; Set OK only valid for WAITING/NOTOK/CANCELLED", id, status)
	}
	newOutput := "[set-ok by operator at " + time.Now().Format(time.RFC3339) + "]\n" + output
	// Guarda pelo status LIDO: se o tick reivindicar a instance (WAITING→RUNNING)
	// entre o SELECT e o UPDATE, não sobrescreve — o operador re-tenta vendo o
	// estado novo.
	res, err := s.db.Exec(
		`UPDATE instances SET status=?, exit_code=0, output=?, finished_at=COALESCE(finished_at, CURRENT_TIMESTAMP) WHERE id=? AND status=?`,
		string(domain.StatusOK), newOutput, id, status,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("instance %s changed state during Set OK — retry", id)
	}
	// Set OK é um término OK de verdade aos olhos das condições (regra do
	// usuário): APLICA as saídas — adiciona as ConditionsOutAdd (destrava
	// sucessores a partir de um pai falho) E remove as ConditionsOutRemove
	// (consome as condições de entrada: Set OK + rerun ⇒ o job volta a esperar,
	// porque o próprio Set OK apagou a condição do pool).
	s.applyConditionsOut(id, "set-ok")
	s.emitEvent(id, "set-ok", "operator", fmt.Sprintf("flipped from %s to OK", status))
	s.hub.BroadcastWeb("instance.changed", map[string]interface{}{
		"id": id, "status": string(domain.StatusOK), "setOk": true,
	})
	return nil
}

// applyConditionsOut — aplica as SAÍDAS de condição de uma instance que
// terminou OK (término real ou Set OK): adiciona OutAdd, remove OutRemove.
// O escopo de data é o ODAT (origem) do produtor — um job carregado do dia 14
// que termina hoje cria a condição DO DIA 14 (quem espera é o consumidor da
// mesma origem), com @stat/@prev resolvidos por nome.
//
// Fonte das saídas = SÓ o snapshot congelado na ordem (M1, 2026-07-17): criar
// um consumidor novo no Design DEPOIS da ordem não faz o OK de hoje produzir a
// condição nova — só a próxima ordem (Force/daily) carrega o OutAdd novo, como
// no Active Jobs File do Control-M. (Antes era snapshot ∪ def viva, por causa
// de instances pré-unificação; MigrateMonitoringSnapshot congelou essa união
// nos snapshots em voo uma única vez no upgrade.) Def viva só como fallback de
// instance LEGADA sem snapshot nenhum.
func (s *Scheduler) applyConditionsOut(id, actor string) {
	if s.conditions == nil {
		return
	}
	var defID, odate, snapshot string
	_ = s.db.QueryRow(
		`SELECT definition_id, `+odateExpr+`, COALESCE(definition_snapshot,'') FROM instances WHERE id=?`, id,
	).Scan(&defID, &odate, &snapshot)
	var def domain.JobDefinition
	if snapshot != "" {
		var snap domain.JobDefinition
		if json.Unmarshal([]byte(snapshot), &snap) == nil && snap.ID != "" {
			def = snap
		}
	}
	if def.ID == "" { // legado sem snapshot: def viva, melhor esforço
		s.mu.Lock()
		for _, d := range s.defs {
			if d.ID == defID {
				def = d
				break
			}
		}
		s.mu.Unlock()
	}
	if def.ID != "" {
		s.conditions.ApplyOutcomes(def, odate, actor, s.prevDaily)
	}
}

func unionStr(a, b []string) []string {
	out := append([]string{}, a...)
	for _, s := range b {
		found := false
		for _, x := range out {
			if x == s {
				found = true
				break
			}
		}
		if !found {
			out = append(out, s)
		}
	}
	return out
}

// ForceOrder cria uma instance NOVA fora do agendamento (Control-M "Order
// Force"). A ordem forçada NÃO bypassa os gates de RUNTIME: condições de
// entrada valem contra o pool (a cópia de um job cuja condição já foi
// CONSUMIDA — deletada pelo OK do consumidor — entra em WAIT COND até alguém
// recriá-la), e agente/recursos/Confirm continuam valendo. O que ela bypassa
// é o AGENDAMENTO (calendário/janela/horário — a ordem nasce elegível agora).
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
	now := time.Now()
	today := now.Format("2006-01-02")
	id := defID + "-FORCE-" + now.Format("150405")
	snap, _ := json.Marshal(*def) // Fase A: congela a def no momento da ordem manual.
	mc := frozenMonitorCols(*def) // M1: Force congela a def publicada ATUAL — é a ordem nova.
	_, err := s.db.Exec(
		// dry_run + colunas M1 congelados da def NO MOMENTO do force (imutável depois).
		`INSERT INTO instances(id, definition_id, team, order_date, status, scheduled_at, forced, force_mode, definition_commit_sha, definition_snapshot, dry_run,
			label, job_type, confirm_req, environment, pinned_agent, conds_in, conds_out_add, resources) VALUES(?,?,?,?,?,?,1,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, defID, def.Team, today, string(domain.StatusWaiting), now, ForceModeOrder, commitSHA, string(snap), boolToInt(def.DryRun),
		mc.label, mc.jobType, mc.confirmReq, mc.environment, mc.pinned, mc.condsIn, mc.condsOutAdd, mc.resources,
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
	// Tenta despachar JÁ, pelo MESMO gating do tick (condições, agente,
	// recursos, Confirm — nada de caminho paralelo): sem bloqueio, roda na
	// hora, como o Force sempre fez; bloqueado, fica WAITING com o motivo
	// visível no Explain e o tick assume dali (ex.: WAIT COND até a condição
	// existir; WAIT_CONFIRM até o operador confirmar).
	r := instRow{
		ID: id, DefID: defID, OrderDate: today, Status: string(domain.StatusWaiting),
		ScheduledAt: now, Forced: true, ForceMode: ForceModeOrder, Snapshot: string(snap),
	}
	if blockers := s.gateInstance(r, *def, nil, now, true); len(blockers) > 0 {
		s.emitEvent(id, "submitted", "operator", "force aguardando gate: "+blockers[0].Detail)
		return id, nil
	}
	if len(def.Resources) > 0 && s.resources != nil {
		if !s.resources.TryAcquire(id, def.Resources) {
			return id, nil // recurso indisponível — o tick re-tenta
		}
	}
	go s.startInstance(id, *def)
	return id, nil
}
