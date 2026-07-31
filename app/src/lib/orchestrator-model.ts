/**
 * Orchestrator Model — Core types
 *
 * Fundamental separation: JobDefinition (what/when) vs JobInstance (a concrete run).
 *
 * Design mode works with JobDefinitions.
 * Monitoring mode works with JobInstances.
 * The Scheduler bridges the two by creating instances from definitions.
 */

/* ── Instance Status ── */

/**
 * Lifecycle of a job instance in monitoring:
 * - WAITING:    scheduled but not yet time to run
 * - RUNNING:    currently executing
 * - OK:         completed successfully
 * - NOTOK:      completed with failure
 * - HOLD:       manually held (won't run until released)
 * - CANCELLED:  manually cancelled before execution
 */
export type InstanceStatus =
  | "WAITING"
  | "RUNNING"
  | "OK"
  | "NOTOK"
  | "HOLD"
  | "CANCELLED";

export const INSTANCE_STATUS_CONFIG: Record<
  InstanceStatus,
  { label: string; color: string; dotColor: string; glowClass: string }
> = {
  WAITING:   { label: "Waiting",   color: "text-amber-400",   dotColor: "bg-amber-400",   glowClass: "node-glow-waiting" },
  RUNNING:   { label: "Running",   color: "text-cyan-400",    dotColor: "bg-cyan-400",    glowClass: "node-glow-running" },
  OK:        { label: "OK",        color: "text-emerald-400", dotColor: "bg-emerald-400", glowClass: "node-glow-success" },
  NOTOK:     { label: "Not OK",    color: "text-red-400",     dotColor: "bg-red-400",     glowClass: "node-glow-failed" },
  HOLD:      { label: "Hold",      color: "text-violet-400",  dotColor: "bg-violet-400",  glowClass: "node-glow-inactive" },
  CANCELLED: { label: "Cancelled", color: "text-slate-400",   dotColor: "bg-slate-500",   glowClass: "node-glow-inactive" },
};

/* ── Schedule Definition ── */

/** Frequência de recorrência (Control-M-like). */
export type ScheduleFrequency = "daily" | "weekly" | "monthly" | "businessday" | "advanced";

/** Regras avançadas nomeadas (v1). */
export type AdvancedRule =
  | "first-businessday"
  | "last-businessday"
  | "first-businessday-not-monday"
  | "penultimate-businessday";

export interface JobSchedule {
  /** Whether this schedule is active */
  enabled: boolean;
  /** Cron expression (5-field) — LEGADO; vazio quando usa Frequency. */
  cronExpression?: string;
  /** Human-readable description */
  description?: string;
  /** Timezone (MVP: browser local; future: IANA tz) */
  timezone?: string;

  /* ── Quando no dia (runtime) ── */
  /** "HH:MM" — hora em que fica elegível após a daily */
  runAt?: string;
  windowFrom?: string;
  windowTo?: string;
  cyclic?: boolean;
  intervalMin?: number;
  /** Cyclic: teto de execuções (0/ausente = sem teto; limitado por windowTo/daily). */
  cyclicMaxRuns?: number;

  /** Ciclo de vida da daily (Control-M Keep Active): quantas diárias EXTRA o job
   *  sobrevive se NÃO terminou OK (carry-over). 0/ausente = DEFAULT (um NOTOK
   *  não-tratado ainda persiste +1 diária). RUNNING/HELD persistem sempre. */
  keepActive?: number;

  /* ── Em quais dias (avaliado pela daily) ── */
  frequency?: ScheduleFrequency;
  /** weekly: ["mon","tue",...] */
  daysOfWeek?: string[];
  /** monthly: 1..31; -1 = último dia */
  daysOfMonth?: number[];
  /** businessday: 5 = 5º dia útil; -1 = último dia útil */
  nthBusinessDays?: number[];
  /** filtro de meses 1..12 (vazio = todos) */
  monthsOfYear?: number[];
  /** advanced: regra nomeada */
  advancedRule?: AdvancedRule;
  /** Shift (Control-M "roll"): dia nominal inelegível (feriado/fim de semana) →
   *  "" | "none" = não roda; "next-businessday" = rola pro próximo dia útil;
   *  "prev-businessday" = antecipa pro dia útil anterior. */
  shift?: "" | "none" | "next-businessday" | "prev-businessday";
}

/** Vínculo de calendar a um job (chamar/negar). */
export interface CalendarRef {
  name: string;
  mode: "include" | "exclude";
}

/* ── Job Definition (Design mode entity) ── */

/**
 * A job definition is what you create in Design mode.
 * It describes WHAT to do, WHEN to do it, and dependencies.
 * It has NO execution state — that lives in JobInstance.
 */
export interface JobDefinition {
  /** Unique ID of this definition */
  id: string;
  /** Display label */
  label: string;
  /** Job type (determines executor) */
  jobType: string;
  /** Team/folder grouping */
  team?: string;
  /** When this job should run */
  schedule: JobSchedule;
  /** Max retry attempts */
  retries: number;
  /** Timeout in seconds */
  timeout: number;
  /** Execution configuration (type-specific) */
  actionConfig?: Record<string, unknown>;
  /** Custom variables */
  variables?: Array<{ key: string; value: string }>;
  /** If true, log intent without executing */
  dryRun?: boolean;
  /** Control-M "Wait for confirmation": a instance não roda até o operador
   *  confirmar (botão Confirm no Monitoring). Rerun re-exige confirmação. */
  confirm?: boolean;
  /** F14 — calendar legado (include). Preferir `calendars`. */
  calendar?: string;
  /** F14+ — calendars chamados no job, cada um include ou exclude (negar). */
  calendars?: CalendarRef[];
  /**
   * Fase 8 — dependências upstream.
   * Define quais definitions precisam ter terminado (com dada
   * condição) antes que esta instance possa sair de WAITING.
   * `dateRef` (2026-07-16, Control-M date de condition) diz QUAL diária do pai
   * satisfaz, sempre relativa ao ODAT do consumidor: "odat"/ausente = mesma
   * diária de origem; "prev" = diária anterior; "stat" = qualquer (estática).
   * Undefined/empty = sem dependências.
   */
  upstream?: Array<{ from: string; condition: EdgeCondition; dateRef?: DepDateRef }>;
  /**
   * Actions / On-Do (Control-M "On/Do") — regras reativas por job.
   * Cada regra dispara no máximo uma vez por instance. Ver ActionRule.
   * Undefined/empty = sem regras.
   */
  actions?: ActionRule[];

  /* ── Campos ricos do backend (domain.JobDefinition) exibidos em Monitoring.
     Carregados pelo adapter (toWeb); round-trip preservado no toServer.
     Aditivos e opcionais — Design ainda não edita todos. ── */

  /** Control-M "Application" — agrupamento acima de Folder/SubApplication.
   *  Ainda não configurável na UI; reservado (placeholder no painel General). */
  application?: string;
  /** F20 — ambiente de execução (dev/staging/prod). Routing por env. */
  environment?: string;
  /** F15 — recursos consumidos (nome → quantidade). O scheduler bloqueia o
   *  start enquanto faltar capacidade. */
  resources?: Record<string, number>;
  /** F16 — conditions IN exigidas para o job sair de WAITING (Control-M). */
  conditionsIn?: string[];
  /** F16 — conditions OUT adicionadas quando o job termina OK. */
  conditionsOutAdd?: string[];
  /** F16 — conditions OUT removidas quando o job termina OK. */
  conditionsOutRemove?: string[];
  /** CL — lógica booleana de ENTRADA (AND/OR, forma DNF). Opcional: ausente =
   *  AND implícito de `conditionsIn` (retrocompat). Presente = a expressão que o
   *  gate do server avalia; seus membros também constam em `conditionsIn` (a
   *  união — mantém topologia/linhas/snapshot). Congelada na ordem (M1). */
  conditionLogic?: ConditionLogic;
  /** F18 — variáveis locais do job (escopo definition, mapa nome→valor),
   *  interpoláveis nos params. Distinto de `variables` (array snapshotado na
   *  instance): esta é a config viva do desenho. */
  localVars?: Record<string, string>;
  /** F19 — SLA: duração esperada / deadline; alerta em breach. */
  sla?: { expectedDurationMin?: number; deadlineHM?: string; severity?: string; webhookUrl?: string };
  /** F17 — sub-workflow: folder que precisa terminar OK antes deste job. */
  subWorkflow?: { folder: string; variables?: Record<string, string> };
}

/**
 * CL — lógica booleana de ENTRADA (AND/OR em forma DNF, espelha
 * domain.ConditionLogic). `op` combina os GRUPOS (topo); cada grupo combina
 * seus membros pelo `CondGroup.op`. Avaliação: op( group.op(member ∈ pool?) ).
 *   (C1 AND C2) OR C3  →  { op:"OR",  groups:[{op:"AND",members:[C1,C2]},{op:"AND",members:[C3]}] }
 *   (C1 OR C2) AND C3  →  { op:"AND", groups:[{op:"OR",members:[C1,C2]},{op:"AND",members:[C3]}] }
 * Membros carregam o sufixo de data (@odat/@prev/@stat) como em conditionsIn.
 */
export interface ConditionLogic {
  op: CondBoolOp;
  groups: CondGroup[];
}
export interface CondGroup {
  op: CondBoolOp;
  members: string[];
}
export type CondBoolOp = "AND" | "OR";
/** Membro RESERVADO da lógica: satisfeito quando o "a partir de" (windowFrom) é
 *  atingido. Base do fallback temporal "condição OU horário" (CL-2). NÃO entra em
 *  conditionsIn (não é condição do pool). Espelha domain.CondTokenTime. */
export const COND_TIME_TOKEN = "$TIME";

/**
 * ActionRule — uma regra "On <gatilho> Do <ação>" (Control-M On/Do).
 *
 * Estrutura PLANA: só os campos relevantes ao On/Do escolhido são usados.
 *  - on="result"  → status (OK|NOTOK) terminal do job, após esgotar retries.
 *  - on="exit"    → exitCodes: o código de saída terminal casa com a espec (COMPSTAT).
 *  - on="attempt" → attempt (1-based): a N-ésima tentativa FALHOU.
 *  - on="runtime" → afterMin: o job está RUNNING há mais que N minutos.
 *
 * Ações (do):
 *  - "notify"        → alerta nos canais (message/severity/channels).
 *  - "set-condition" → seta a condition global no escopo do order_date.
 *  - "run-job"       → Force Order de targetJob (ignora deps).
 *  - "set-ok"        → flipa o PRÓPRIO job NOTOK→OK (só faz sentido com on result NOTOK).
 */
export interface ActionRule {
  /** Gatilho. */
  on: "result" | "exit" | "attempt" | "runtime";
  status?: "OK" | "NOTOK"; // on==="result"
  /** on==="exit": lista/faixa/comparação de exit codes — "1,2,3" · "1-4" · ">0" · "!=0". */
  exitCodes?: string;
  attempt?: number; // on==="attempt" (1-based)
  afterMin?: number; // on==="runtime"
  /** Ação. */
  do: "notify" | "set-condition" | "run-job" | "set-ok";
  message?: string; // notify
  severity?: "info" | "warning" | "critical"; // notify
  channels?: string[]; // notify (vazio = todos configurados)
  condition?: string; // set-condition
  targetJob?: string; // run-job
}

/* ── Job Instance (Monitoring mode entity) ── */

/**
 * A job instance is a concrete scheduled/triggered execution of a JobDefinition.
 * Created by the Scheduler when a job's schedule matches the current date/time,
 * or manually via "Run Now" (force/order).
 */
export interface JobInstance {
  /** Unique instance ID */
  id: string;
  /** Reference to the parent definition */
  definitionId: string;
  /** Copied from definition at creation time */
  label: string;
  /** Copied from definition */
  jobType: string;
  /** Copied from definition */
  team?: string;
  /** The date this instance was ordered for (YYYY-MM-DD) */
  orderDate: string;
  /** When this instance was created */
  createdAt: number;
  /** Scheduled execution time (epoch ms) */
  scheduledAt: number;
  /** When execution actually started (epoch ms, null if not started) */
  startedAt?: number;
  /** When execution completed (epoch ms, null if not done) */
  completedAt?: number;
  /** Current status */
  status: InstanceStatus;
  /** Duration in ms (calculated after completion) */
  durationMs?: number;
  /** Number of attempts (including retries) */
  attempts: number;
  /** Error message if NOTOK */
  error?: string;
  /** Execution output/result */
  output?: Record<string, unknown>;
  /** Whether this was a manual (Run Now/Force) trigger. NOTA: `manual` cobre os
   *  DOIS caminhos de forçar (bypass de deps no tick local). NÃO usar pra tag
   *  visual — Run Now não ganha selo; use `manualOrder`. */
  manual: boolean;
  /** Ordem colocada NA MÃO pelo operador ("Order Force" do Design, force_mode=
   *  'order'): a ÚNICA que ganha o selo 🖐 MANUAL no card. Run Now (força uma
   *  instance já existente, force_mode='') NÃO seta isto — não leva tag nenhuma. */
  manualOrder?: boolean;
  /** Ciclo de vida da daily: order_date de origem se a instância foi carregada
   *  da diária anterior (carry-over Control-M); ausente se nasceu hoje. */
  carriedFrom?: string;
  /** Copied from definition for execution */
  actionConfig?: Record<string, unknown>;
  retries: number;
  timeout: number;
  variables?: Array<{ key: string; value: string }>;
  dryRun?: boolean;
  /** Control-M Confirm: o operador já liberou esta instance (confirm:true). */
  confirmed?: boolean;
  /** Cyclic runtime: voltas OK completadas neste dia (job cyclic). */
  cycleRuns?: number;
  /** Origem do HOLD (server schemaV14). "" / ausente = hold individual (liberável
   *  1-a-1); "folder" = segurado por uma pausa de folder (só o resume da folder
   *  libera). Vale só quando status==="HOLD"; guia o cadeado e bloqueia o Release
   *  individual de um job segurado pela folder. */
  holdScope?: string;
  /** Status congelado pelo HOLD (server schemaV16, hold geral): o hold vale pra
   *  qualquer status exceto RUNNING, e o Release restaura ESTE status — não
   *  WAITING cego. Vale só quando status==="HOLD"; ausente/"" = hold legado
   *  (era WAITING). O front usa pra rotular o Release ("volta a NOTOK"). */
  heldFrom?: InstanceStatus;
  /* ── M1 (server schemaV18) — Monitoring IMUTÁVEL ──
     Tudo que o card/lista/grafo exibem vem CONGELADO na ordem, NUNCA da def
     viva: renomear/trocar tipo/criar jobs+condições novos no Design não
     reescreve instances já ordenadas (só a próxima ordem via Force/daily).
     Instance legada sem backfill vem com estes campos vazios — o canvas cai
     na def viva SÓ nesse caso. */
  /** Control-M Confirm exigido pela ordem (def.confirm congelado) — gate WAIT CONFIRM. */
  confirmReq?: boolean;
  /** Ambiente congelado (roteamento) — entrada do WAIT AGENT. */
  environment?: string;
  /** Agente pinado congelado (def.agentId) — entrada do WAIT AGENT. */
  pinnedAgent?: string;
  /** Condições de ENTRADA congeladas (com sufixo @odat/@prev/@stat) — gate WAIT
   *  COND e origem das LINHAS do grafo do Monitoring. */
  condsIn?: string[];
  /** Condições de SAÍDA＋ congeladas — produtoras das linhas do grafo. */
  condsOutAdd?: string[];
  /** F15 (server schemaV19) — recursos/quotas CONGELADOS na ordem (nome→qtd).
   *  Entrada do card WAIT RESOURCE; mudar os recursos do job no Design não
   *  reescreve ordens já materializadas. */
  resources?: Record<string, number>;
  /** CL (server schemaV21) — lógica AND/OR CONGELADA na ordem. Entrada das LINHAS
   *  OR do grafo do Monitoring (CL-4); nil = linhas AND. Imutável como o resto. */
  condLogic?: ConditionLogic;
}

/* ── Order Date Helper ──
 *
 * DAY-1 — "hoje" é a DATA DE NEGÓCIO, e o dia de negócio vira no `daily_at`, não
 * à meia-noite. Quem decide é o SERVER (é ele quem materializa a daily): em
 * server mode `setBusinessDate` publica aqui o `orderDate` do /api/daily/status
 * e toda a SPA passa a pedir o MESMO dia que o server carimbou.
 *
 * O relógio do browser era a segunda metade do bug de produção: server em UTC +
 * operador em -03, e a tela pedia `GET /api/instances?date=` de um dia que não
 * existia — a ordem forçada respondia 200 e sumia do board. Aqui ele fica só
 * como fallback do LOCAL mode (sem server, daily à meia-noite) e da janela
 * entre o boot da página e a primeira resposta do server.
 */

let businessDate: string | null = null;

/** Publica a data de negócio do server. `null` volta pro relógio do browser. */
export function setBusinessDate(date: string | null | undefined): void {
  businessDate = date && /^\d{4}-\d{2}-\d{2}$/.test(date) ? date : null;
}

export function todayOrderDate(): string {
  if (businessDate) return businessDate;
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

/* ── Instance Factory ── */

let instanceCounter = 0;

/**
 * Create a JobInstance from a JobDefinition.
 * Called by the Scheduler when a job's schedule matches, or by "Run Now".
 */
export function createInstance(
  def: JobDefinition,
  scheduledAt: Date,
  manual = false,
): JobInstance {
  return {
    id: `inst-${def.id}-${Date.now()}-${++instanceCounter}`,
    definitionId: def.id,
    label: def.label,
    jobType: def.jobType,
    team: def.team,
    orderDate: todayOrderDate(),
    createdAt: Date.now(),
    scheduledAt: scheduledAt.getTime(),
    status: "WAITING",
    attempts: 0,
    manual,
    actionConfig: def.actionConfig,
    retries: def.retries,
    timeout: def.timeout,
    variables: def.variables,
    dryRun: def.dryRun,
    // M1: congela o que o Monitoring exibe (mesmo racional do dryRun acima).
    confirmReq: def.confirm,
    environment: def.environment,
    pinnedAgent: typeof def.actionConfig?._agentId === "string" ? def.actionConfig._agentId : undefined,
    condsIn: def.conditionsIn,
    condsOutAdd: def.conditionsOutAdd,
    resources: def.resources,
    condLogic: def.conditionLogic,
  };
}

/* ──────────────────────────────────────────────────────────────
   Fase 2 — Edge condition + Teams
   ──────────────────────────────────────────────────────────────
   Preserva compatibilidade: campos novos são opcionais até a
   Fase 5 (wire-up completo). Consumers existentes continuam
   funcionando.
   ────────────────────────────────────────────────────────────── */

/**
 * Condição de disparo do sucessor. Inspiração Control-M:
 *   - on-success: dispara só se pai terminou OK
 *   - on-failure: dispara só se pai terminou NOTOK (branch de fallback/alerta)
 *   - on-complete: dispara independente do resultado (OK ou NOTOK)
 *   - always: alias de on-complete (mantido por clareza semântica)
 */
export type EdgeCondition = "on-success" | "on-failure" | "on-complete" | "always";

/**
 * Metadata opcional de uma aresta (React Flow Edge.data).
 * Quando ausente, assume-se "on-success" para preservar o
 * comportamento padrão de DAGs de workflow.
 */
export interface JobEdgeData {
  condition?: EdgeCondition;
  /** Rótulo opcional exibido na aresta (ex: "retry", "alerta"). */
  label?: string;
  [key: string]: unknown;
}

export const EDGE_CONDITION_DEFAULT: EdgeCondition = "on-success";

/**
 * Referência de DATA de uma dependência (Control-M date de condition),
 * relativa ao ODAT (data de origem) do consumidor:
 *   - "odat" (default): término do pai da MESMA diária de origem
 *   - "prev": término do pai da diária ANTERIOR
 *   - "stat": estática — qualquer término livre, sem olhar data
 */
export type DepDateRef = "odat" | "prev" | "stat";

/** ODAT — a data de ORIGEM da ordem (dia em que entrou em schedule pela 1ª
 *  vez). O carry-over avança orderDate (dia ativo) preservando a origem em
 *  carriedFrom; todo escopo de data (eventos, linhas, agrupamento) usa isto. */
export function odateOf(inst: Pick<JobInstance, "orderDate" | "carriedFrom">): string {
  return inst.carriedFrom || inst.orderDate;
}

/**
 * Times (folders) canônicos do Regente PicPay.
 * Mantido como const para permitir extensão futura sem breaking change.
 * Fase 5: migrar `JobDefinition.team` para `Team` (required).
 */
export const TEAMS = ["DATA", "FIN", "PLAT", "RISK"] as const;
export type Team = (typeof TEAMS)[number];
