// Package domain — modelo canônico do orquestrador Regente.
//
// Estes tipos são a fonte de verdade do servidor. O YAML em disco e o JSON
// sobre a rede seguem exatamente esta estrutura.
package domain

import "time"

// EdgeCondition — condição de uma dependência upstream.
type EdgeCondition string

const (
	CondOnSuccess  EdgeCondition = "on-success"
	CondOnFailure  EdgeCondition = "on-failure"
	CondOnComplete EdgeCondition = "on-complete"
	CondAlways     EdgeCondition = "always"
)

// Upstream — dependência de um job em outro (OR implícito na ausência de `group`).
type Upstream struct {
	From      string        `yaml:"from" json:"from"`
	Condition EdgeCondition `yaml:"condition" json:"condition"`
}

// Schedule — quando o job deve rodar (estilo Control-M).
//
// QUANDO no dia (runtime):
//   - RunAt      "HH:MM" hora exata em que a instance fica elegível após a daily
//   - WindowFrom/WindowTo janela de execução permitida
//   - Cyclic + IntervalMin para jobs repetitivos dentro da janela
//
// EM QUAIS DIAS (avaliado pela daily, via Frequency):
//   - "" ou "daily"     todo dia
//   - "weekly"          weekday ∈ DaysOfWeek (["mon","tue",...])
//   - "monthly"         dia-do-mês ∈ DaysOfMonth (1..31; -1 = último dia)
//   - "businessday"     é o N-ésimo dia útil do mês ∈ NthBusinessDays
//     (5 = 5º dia útil; -1 = último dia útil)
//   - "advanced"        AdvancedRule (enum de regras nomeadas)
//
// MonthsOfYear (1..12) filtra meses em qualquer frequência (vazio = todos).
// A elegibilidade final ainda passa pelos Calendars do job (include/exclude).
type Schedule struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Description — texto livre exibido na UI (o front sempre enviou; o server
	// dropava por falta do campo). Persistido no YAML para não perder no publish.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	RunAt       string `yaml:"runAt,omitempty" json:"runAt,omitempty"`
	WindowFrom  string `yaml:"windowFrom,omitempty" json:"windowFrom,omitempty"`
	WindowTo    string `yaml:"windowTo,omitempty" json:"windowTo,omitempty"`
	Cyclic      bool   `yaml:"cyclic,omitempty" json:"cyclic,omitempty"`
	IntervalMin int    `yaml:"intervalMin,omitempty" json:"intervalMin,omitempty"`

	// CyclicMaxRuns — teto de execuções do ciclo (0 = sem teto; o ciclo é
	// limitado pela janela WindowTo e, sem janela, pela virada da daily).
	CyclicMaxRuns int `yaml:"cyclicMaxRuns,omitempty" json:"cyclicMaxRuns,omitempty"`

	// KeepActive (Control-M-like) — quantas diárias EXTRA o job sobrevive se NÃO
	// terminou OK (carry-over entre diárias). 0 = comportamento DEFAULT (um NOTOK
	// não-tratado ainda persiste +1 diária; um WAITING que nunca rodou NÃO persiste).
	// >0 = sobrevive N diárias mesmo sem rodar/terminar OK. RUNNING e HELD persistem
	// sempre, independente deste valor (ver scheduler.carryOver).
	KeepActive int `yaml:"keepActive,omitempty" json:"keepActive,omitempty"`

	// Recorrência estruturada (Control-M-like).
	Frequency       string   `yaml:"frequency,omitempty" json:"frequency,omitempty"`
	DaysOfWeek      []string `yaml:"daysOfWeek,omitempty" json:"daysOfWeek,omitempty"`
	DaysOfMonth     []int    `yaml:"daysOfMonth,omitempty" json:"daysOfMonth,omitempty"`
	NthBusinessDays []int    `yaml:"nthBusinessDays,omitempty" json:"nthBusinessDays,omitempty"`
	MonthsOfYear    []int    `yaml:"monthsOfYear,omitempty" json:"monthsOfYear,omitempty"`
	AdvancedRule    string   `yaml:"advancedRule,omitempty" json:"advancedRule,omitempty"`

	// Shift (Control-M "roll") — o que fazer quando o dia NOMINAL da recorrência
	// cai num dia não-elegível (feriado/exclude do calendar; sem calendar, fim de
	// semana): "" | "none" = não roda (comportamento clássico);
	// "next-businessday" = rola pro PRÓXIMO dia elegível;
	// "prev-businessday" = ANTECIPA pro dia elegível anterior.
	Shift string `yaml:"shift,omitempty" json:"shift,omitempty"`

	// Legado: cron de 5 campos. Se presente e Frequency vazio, é a fonte.
	CronExpression string `yaml:"cronExpression,omitempty" json:"cronExpression,omitempty"`
}

// CalendarRef — vínculo de um calendar a um job, com modo.
//
//	include → o job só roda em dias elegíveis do calendar
//	exclude → o job NÃO roda em dias elegíveis do calendar (negar)
type CalendarRef struct {
	Name string `yaml:"name" json:"name"`
	Mode string `yaml:"mode" json:"mode"` // "include" | "exclude"
}

// JobDefinition — definição versionada em disco (YAML). Fonte da verdade.
//
// `Params` carrega parâmetros específicos do jobType (COMMAND/REST/...).
// Schema por tipo é responsabilidade do agent/executor.
type JobDefinition struct {
	ID       string     `yaml:"id" json:"id"`
	Label    string     `yaml:"label" json:"label"`
	Team     string     `yaml:"team" json:"team"`
	JobType  string     `yaml:"jobType" json:"jobType"`
	Schedule Schedule   `yaml:"schedule" json:"schedule"`
	Retries  int        `yaml:"retries,omitempty" json:"retries,omitempty"`
	Timeout  int        `yaml:"timeout,omitempty" json:"timeout,omitempty"` // seconds
	DryRun   bool       `yaml:"dryRun,omitempty" json:"dryRun,omitempty"`
	Upstream []Upstream `yaml:"upstream,omitempty" json:"upstream,omitempty"`
	// JSON wire usa "actionConfig" (frontend); YAML em disco mantem "params" (legado).
	Params  map[string]interface{} `yaml:"params,omitempty" json:"actionConfig,omitempty"`
	AgentID string                 `yaml:"agentId,omitempty" json:"agentId,omitempty"`

	// Confirm (Control-M "Wait for confirmation") — a instance NÃO roda até um
	// operador confirmar (POST /instances/{id}/confirm). Vale também para Force
	// Order (no Control-M a confirmação é um wait de runtime, não de schedule);
	// rerun re-exige confirmação. Gate WAIT_CONFIRM no gateInstance (fonte única).
	Confirm bool `yaml:"confirm,omitempty" json:"confirm,omitempty"`

	// === Bloco 2 — Control-M parity ===

	// F14: Calendar nomeado (LEGADO — tratado como include). Use Calendars[].
	Calendar string `yaml:"calendar,omitempty" json:"calendar,omitempty"`

	// F14+: vários calendars chamados no job, cada um include ou exclude (negar).
	Calendars []CalendarRef `yaml:"calendars,omitempty" json:"calendars,omitempty"`

	// F15: Recursos consumidos (nome → quantidade). Scheduler bloqueia start se faltar.
	Resources map[string]int `yaml:"resources,omitempty" json:"resources,omitempty"`

	// F16: Conditions IN/OUT (Control-M global conditions).
	ConditionsIn        []string `yaml:"conditionsIn,omitempty" json:"conditionsIn,omitempty"`
	ConditionsOutAdd    []string `yaml:"conditionsOutAdd,omitempty" json:"conditionsOutAdd,omitempty"`
	ConditionsOutRemove []string `yaml:"conditionsOutRemove,omitempty" json:"conditionsOutRemove,omitempty"`

	// F17: Sub-workflow — referencia outro folder/team que precisa terminar OK.
	SubWorkflow *SubWorkflowRef `yaml:"subWorkflow,omitempty" json:"subWorkflow,omitempty"`

	// F18: Variables locais do job (escopo definition). Interpoláveis em params.
	Variables map[string]string `yaml:"variables,omitempty" json:"variables,omitempty"`

	// F19: SLA — duração esperada e deadline; emite alerta se breach.
	SLA *SLASpec `yaml:"sla,omitempty" json:"sla,omitempty"`

	// F20: Environment — dev/staging/prod. Routing por env.
	Environment string `yaml:"environment,omitempty" json:"environment,omitempty"`

	// Actions / On-Do — regras configuráveis disparadas por evento do job
	// (Control-M "On/Do"). Avaliadas pelo ActionsEngine; ver actions.go.
	Actions []ActionRule `yaml:"actions,omitempty" json:"actions,omitempty"`
}

// ActionRule — uma regra "On <gatilho> Do <ação>" (Control-M On/Do).
//
// Estrutura PLANA (não aninhada) para legibilidade no YAML e edição na UI: os
// campos de gatilho (On + qualificador) e os de ação (Do + parâmetros) convivem;
// só os relevantes ao On/Do escolhido são lidos. Cada regra dispara NO MÁXIMO uma
// vez por instance (idempotência via tabela action_fires, chaveada pelo índice da
// regra no array). Três dimensões de gatilho:
//
//	On=="result"  → Status (OK|NOTOK) terminal do job, após esgotar retries.
//	On=="attempt" → Attempt (1-based): a N-ésima tentativa FALHOU (escada de rerun).
//	On=="runtime" → AfterMin: o job está RUNNING há mais que N minutos (shout).
//
// Ações (Do):
//
//	"notify"        → alerta nos canais (Message/Severity/Channels) — Slack/webhook/e-mail/PagerDuty.
//	"set-condition" → seta a Condition global no escopo do order_date (destrava sucessores).
//	"run-job"       → Force Order de TargetJob (roda outro job, ignorando deps).
//	"set-ok"        → flipa o PRÓPRIO job NOTOK→OK (só faz sentido com On result NOTOK).
type ActionRule struct {
	// Gatilho.
	On       string `yaml:"on" json:"on"`                             // "result" | "attempt" | "runtime"
	Status   string `yaml:"status,omitempty" json:"status,omitempty"` // On=="result": "OK" | "NOTOK"
	Attempt  int    `yaml:"attempt,omitempty" json:"attempt,omitempty"`
	AfterMin int    `yaml:"afterMin,omitempty" json:"afterMin,omitempty"`

	// Ação.
	Do        string   `yaml:"do" json:"do"`                                   // "notify" | "set-condition" | "run-job" | "set-ok"
	Message   string   `yaml:"message,omitempty" json:"message,omitempty"`     // notify
	Severity  string   `yaml:"severity,omitempty" json:"severity,omitempty"`   // notify (warning|critical|info)
	Channels  []string `yaml:"channels,omitempty" json:"channels,omitempty"`   // notify (vazio = todos configurados)
	Condition string   `yaml:"condition,omitempty" json:"condition,omitempty"` // set-condition
	TargetJob string   `yaml:"targetJob,omitempty" json:"targetJob,omitempty"` // run-job
}

// SubWorkflowRef — F17.
type SubWorkflowRef struct {
	Folder    string            `yaml:"folder" json:"folder"`                           // team a aguardar
	Variables map[string]string `yaml:"variables,omitempty" json:"variables,omitempty"` // vars injetadas
}

// SLASpec — F19.
type SLASpec struct {
	ExpectedDurationMin int    `yaml:"expectedDurationMin,omitempty" json:"expectedDurationMin,omitempty"`
	DeadlineHM          string `yaml:"deadlineHM,omitempty" json:"deadlineHM,omitempty"` // "HH:MM"
	Severity            string `yaml:"severity,omitempty" json:"severity,omitempty"`     // warning|critical
	WebhookURL          string `yaml:"webhookUrl,omitempty" json:"webhookUrl,omitempty"` // canal
}

// Calendar — F14. Define dias elegíveis para schedule.
type Calendar struct {
	Name         string   `yaml:"name" json:"name"`
	BusinessDays []string `yaml:"businessDays,omitempty" json:"businessDays,omitempty"` // ["mon","tue","wed","thu","fri"]
	Holidays     []string `yaml:"holidays,omitempty" json:"holidays,omitempty"`         // ISO ["2026-12-25"]
	IncludeDates []string `yaml:"includeDates,omitempty" json:"includeDates,omitempty"` // exceções: roda mesmo se não businessDay
	ExcludeDates []string `yaml:"excludeDates,omitempty" json:"excludeDates,omitempty"` // pula mesmo se businessDay
	Timezone     string   `yaml:"timezone,omitempty" json:"timezone,omitempty"`
}

// Condition — F16. Estado runtime de uma condição global.
type Condition struct {
	Name      string    `json:"name"`
	ScopeDate string    `json:"scopeDate,omitempty"` // YYYY-MM-DD ou "" (permanente)
	SetAt     time.Time `json:"setAt"`
	SetBy     string    `json:"setBy"`
}

// SLABreach — F19.
type SLABreach struct {
	ID         int       `json:"id"`
	InstanceID string    `json:"instanceId"`
	DefID      string    `json:"definitionId"`
	Kind       string    `json:"kind"` // duration|deadline
	Severity   string    `json:"severity"`
	Message    string    `json:"message"`
	DetectedAt time.Time `json:"detectedAt"`
	Notified   bool      `json:"notified"`
}

// InstanceStatus — ciclo de vida da instance.
type InstanceStatus string

const (
	StatusWaiting   InstanceStatus = "WAITING"
	StatusRunning   InstanceStatus = "RUNNING"
	StatusOK        InstanceStatus = "OK"
	StatusNotOK     InstanceStatus = "NOTOK"
	StatusHeld      InstanceStatus = "HELD"
	StatusCancelled InstanceStatus = "CANCELLED"
	StatusSLABreach InstanceStatus = "SLA_BREACH"
)

// JobInstance — execução concreta de uma definition num dia (order).
type JobInstance struct {
	ID           string         `json:"id"`
	DefinitionID string         `json:"definitionId"`
	OrderDate    string         `json:"orderDate"` // YYYY-MM-DD
	Status       InstanceStatus `json:"status"`
	ScheduledAt  time.Time      `json:"scheduledAt"`
	StartedAt    *time.Time     `json:"startedAt,omitempty"`
	FinishedAt   *time.Time     `json:"finishedAt,omitempty"`
	AgentID      string         `json:"agentId,omitempty"`
	ExitCode     int            `json:"exitCode,omitempty"`
	Output       string         `json:"output,omitempty"`
	Forced       bool           `json:"forced,omitempty"`
}
