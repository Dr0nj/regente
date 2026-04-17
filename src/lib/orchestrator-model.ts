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

export interface JobSchedule {
  /** Cron expression (5-field: min hour dom month dow) */
  cronExpression: string;
  /** Human-readable description (auto-generated from cron) */
  description?: string;
  /** Whether this schedule is active */
  enabled: boolean;
  /** Timezone (MVP: browser local; future: IANA tz) */
  timezone?: string;
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
  /**
   * Fase 8 — dependências upstream.
   * Define quais definitions precisam ter terminado (com dada
   * condição) antes que esta instance possa sair de WAITING.
   * Undefined/empty = sem dependências.
   */
  upstream?: Array<{ from: string; condition: EdgeCondition }>;
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
  /** Whether this was a manual (Run Now/Force) trigger */
  manual: boolean;
  /** Copied from definition for execution */
  actionConfig?: Record<string, unknown>;
  retries: number;
  timeout: number;
  variables?: Array<{ key: string; value: string }>;
  dryRun?: boolean;
}

/* ── Order Date Helper ── */

export function todayOrderDate(): string {
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
 * Times (folders) canônicos do Regente PicPay.
 * Mantido como const para permitir extensão futura sem breaking change.
 * Fase 5: migrar `JobDefinition.team` para `Team` (required).
 */
export const TEAMS = ["DATA", "FIN", "PLAT", "RISK"] as const;
export type Team = (typeof TEAMS)[number];
