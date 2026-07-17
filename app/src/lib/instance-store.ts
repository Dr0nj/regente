/**
 * Instance Store — MVP
 *
 * Manages the lifecycle of JobInstances:
 * - Create instances from definitions when scheduled
 * - Update status in real-time (WAITING → RUNNING → OK/NOTOK)
 * - Query today's active instances for Monitoring view
 * - Persist to localStorage (MVP), Supabase-ready interface
 *
 * Future: swap to DynamoDB + Streams or Supabase Realtime
 */

import {
  type JobInstance,
  type JobDefinition,
  type InstanceStatus,
  createInstance,
  todayOrderDate,
  odateOf,
} from "@/lib/orchestrator-model";
import { localLoad, localSave } from "@/lib/persistence";
import { evaluateAlerts, type EvaluationContext } from "@/lib/alerting";
import { applyOutcomesLocal } from "@/lib/conditions-store";
import { getDefinitions } from "@/lib/definition-store";

/* ── Storage ── */

const INSTANCES_KEY = "regente:instances";
const MAX_INSTANCES = 500;

/* ── Real-time subscribers ── */

type InstanceListener = (instances: JobInstance[]) => void;
const listeners = new Set<InstanceListener>();

function notify() {
  const all = getInstances();
  for (const fn of listeners) {
    try { fn(all); } catch { /* */ }
  }
}

/** Subscribe to instance changes (for Monitoring real-time updates) */
export function onInstanceChange(listener: InstanceListener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/* ── CRUD ── */

/** Get all instances (optionally filtered by orderDate) */
export function getInstances(orderDate?: string): JobInstance[] {
  const all = localLoad<JobInstance>(INSTANCES_KEY);
  if (orderDate) return all.filter((i) => i.orderDate === orderDate);
  return all;
}

/** Get today's instances */
export function getTodayInstances(): JobInstance[] {
  return getInstances(todayOrderDate());
}

/** Get a single instance by ID */
export function getInstance(instanceId: string): JobInstance | undefined {
  return localLoad<JobInstance>(INSTANCES_KEY).find((i) => i.id === instanceId);
}

/** Get instances for a specific definition (today) */
export function getInstancesForDefinition(definitionId: string): JobInstance[] {
  return getTodayInstances().filter((i) => i.definitionId === definitionId);
}

/** Save all instances (internal) */
function saveAll(instances: JobInstance[]): void {
  localSave(INSTANCES_KEY, instances, MAX_INSTANCES);
  notify();
}

/**
 * Order a job — create instance from definition.
 * Called by Scheduler (automatic) or Run Now (manual).
 */
export function orderJob(
  def: JobDefinition,
  scheduledAt: Date,
  manual = false,
): JobInstance {
  const inst = createInstance(def, scheduledAt, manual);
  const all = localLoad<JobInstance>(INSTANCES_KEY);
  all.push(inst);
  saveAll(all);
  return inst;
}

/**
 * Update instance status. Core operation for real-time monitoring.
 */
export function updateInstanceStatus(
  instanceId: string,
  status: InstanceStatus,
  extra?: Partial<Pick<JobInstance, "startedAt" | "completedAt" | "durationMs" | "attempts" | "error" | "output">>,
): void {
  const all = localLoad<JobInstance>(INSTANCES_KEY);
  const inst = all.find((i) => i.id === instanceId);
  if (!inst) return;

  const prevStatus = inst.status;
  inst.status = status;
  if (extra) Object.assign(inst, extra);

  // Auto-set timestamps
  if (status === "RUNNING" && !inst.startedAt) {
    inst.startedAt = Date.now();
  }
  if ((status === "OK" || status === "NOTOK" || status === "CANCELLED") && !inst.completedAt) {
    inst.completedAt = Date.now();
    if (inst.startedAt) {
      inst.durationMs = inst.completedAt - inst.startedAt;
    }
  }

  saveAll(all);

  // Alerting (Phase 8) — evaluate rules when a real run reaches a terminal
  // state. Only on the first transition into OK/NOTOK (avoids re-fire when a
  // status is re-applied) and only when the job actually executed (startedAt).
  const becameTerminal = (status === "OK" || status === "NOTOK")
    && prevStatus !== "OK" && prevStatus !== "NOTOK";
  if (becameTerminal && inst.startedAt) {
    try {
      evaluateAlerts(buildAlertContext(inst, all));
    } catch { /* alerting must never break the runtime */ }
  }

  // Modelo único de condições: um término OK (execução do tick, Skip ou
  // Bypass/Set OK local) APLICA as saídas — adiciona ConditionsOutAdd e remove
  // ConditionsOutRemove do pool local (o consumo). NOTOK não toca o pool.
  if (status === "OK" && prevStatus !== "OK") {
    try {
      const def = getDefinitions().find((d) => d.id === inst.definitionId);
      applyOutcomesLocal(def, odateOf(inst), "local-ok");
    } catch { /* conditions must never break the runtime */ }
  }
}

/**
 * Build the alerting EvaluationContext from an instance and the full store.
 * Success rate / consecutive failures are derived from prior runs of the same
 * definition (today), since the metrics subsystem is not wired in browser mode.
 */
function buildAlertContext(inst: JobInstance, all: JobInstance[]): EvaluationContext {
  const history = all
    .filter((i) => i.definitionId === inst.definitionId
      && (i.status === "OK" || i.status === "NOTOK")
      && i.completedAt)
    .sort((a, b) => (a.completedAt ?? 0) - (b.completedAt ?? 0));

  const window = history.slice(-10);
  const okCount = window.filter((i) => i.status === "OK").length;
  const recentSuccessRate = window.length > 0 ? okCount / window.length : 1;

  let consecutiveFailures = 0;
  for (let i = history.length - 1; i >= 0; i--) {
    if (history[i].status === "NOTOK") consecutiveFailures++;
    else break;
  }

  // Slow Execution — média das execuções OK ANTERIORES (a própria run fica de
  // fora pra não puxar a régua). historyRuns=0 ⇒ primeira execução, sem alerta.
  const okPrev = history.filter((i) => i.status === "OK" && i.id !== inst.id && typeof i.durationMs === "number");
  const avgDurationMs = okPrev.length > 0
    ? okPrev.reduce((sum, i) => sum + (i.durationMs ?? 0), 0) / okPrev.length
    : 0;

  return {
    workflowId: inst.definitionId,
    workflowName: inst.label,
    status: inst.status === "OK" ? "SUCCESS" : "FAILED",
    durationMs: inst.durationMs ?? 0,
    maxJobRetries: Math.max(0, inst.attempts - 1),
    recentSuccessRate,
    consecutiveFailures,
    avgDurationMs,
    historyRuns: okPrev.length,
  };
}

/**
 * Hold an instance (prevent execution / freeze state).
 * Hold GERAL (paridade com o server, 2026-07-16): vale pra QUALQUER status
 * exceto RUNNING (a execução já está em andamento) e o próprio HOLD. O status
 * original fica em `heldFrom` — o release restaura ELE, não WAITING cego
 * (que re-executaria um OK segurado).
 */
export function holdInstance(instanceId: string): void {
  const all = localLoad<JobInstance>(INSTANCES_KEY);
  const inst = all.find((i) => i.id === instanceId);
  if (!inst || inst.status === "RUNNING" || inst.status === "HOLD") return;
  inst.heldFrom = inst.status;
  inst.status = "HOLD";
  saveAll(all);
}

/** Release a held instance back to its original (pre-hold) status */
export function releaseInstance(instanceId: string): void {
  const all = localLoad<JobInstance>(INSTANCES_KEY);
  const inst = all.find((i) => i.id === instanceId);
  if (!inst || inst.status !== "HOLD") return;
  inst.status = inst.heldFrom ?? "WAITING"; // hold legado (sem heldFrom) era WAITING
  inst.heldFrom = undefined;
  saveAll(all);
}

/**
 * Delete — Control-M "Delete job": remove a ordem da tela (e do store).
 * SÓ vale para job em HOLD — como RUNNING não é segurável, um job em execução
 * nunca é deletável; qualquer outro status vira deletável passando pelo hold.
 */
export function deleteInstance(instanceId: string): void {
  const all = localLoad<JobInstance>(INSTANCES_KEY);
  const inst = all.find((i) => i.id === instanceId);
  if (!inst || inst.status !== "HOLD") return;
  saveAll(all.filter((i) => i.id !== instanceId));
}

/** Cancel a WAITING/HOLD instance */
export function cancelInstance(instanceId: string): void {
  const inst = getInstance(instanceId);
  if (inst && (inst.status === "WAITING" || inst.status === "HOLD")) {
    updateInstanceStatus(instanceId, "CANCELLED");
  }
}

/**
 * Re-run a NOTOK instance — creates a new instance for the same definition.
 * Like Control-M's "Rerun" action.
 */
export function rerunInstance(instanceId: string): JobInstance | null {
  const inst = getInstance(instanceId);
  if (!inst || inst.status !== "NOTOK") return null;

  // Create new instance from the same definition data
  const def: JobDefinition = {
    id: inst.definitionId,
    label: inst.label,
    jobType: inst.jobType,
    team: inst.team,
    schedule: { cronExpression: "", enabled: true }, // schedule doesn't matter for rerun
    retries: inst.retries,
    timeout: inst.timeout,
    actionConfig: inst.actionConfig,
    variables: inst.variables,
    dryRun: inst.dryRun,
  };

  return orderJob(def, new Date(), true);
}

/** Clear instances for a specific date (cleanup) */
export function clearInstances(orderDate?: string): void {
  if (orderDate) {
    const all = localLoad<JobInstance>(INSTANCES_KEY);
    saveAll(all.filter((i) => i.orderDate !== orderDate));
  } else {
    localStorage.removeItem(INSTANCES_KEY);
    notify();
  }
}

/** Summary stats for today's instances */
export function getTodayStats(): {
  total: number;
  waiting: number;
  running: number;
  ok: number;
  notOk: number;
  hold: number;
} {
  const today = getTodayInstances();
  return {
    total: today.length,
    waiting: today.filter((i) => i.status === "WAITING").length,
    running: today.filter((i) => i.status === "RUNNING").length,
    ok: today.filter((i) => i.status === "OK").length,
    notOk: today.filter((i) => i.status === "NOTOK").length,
    hold: today.filter((i) => i.status === "HOLD").length,
  };
}

/* ──────────────────────────────────────────────────────────────
   Fase 4 — Controles Control-M adicionais
   ────────────────────────────────────────────────────────────── */

/**
 * Skip — marca uma instance WAITING/HOLD como OK sem executar.
 * Control-M "Confirm" / "Skip" equivalent.
 * Não dispara sucessores via condição on-success (ver scheduler).
 */
export function skipInstance(instanceId: string): void {
  const inst = getInstance(instanceId);
  if (!inst) return;
  if (inst.status === "WAITING" || inst.status === "HOLD") {
    updateInstanceStatus(instanceId, "OK", {
      completedAt: Date.now(),
      durationMs: 0,
      output: { skipped: true },
    });
  }
}

/**
 * Bypass — força uma instance NOTOK a ser tratada como OK para
 * desbloquear sucessores on-success. Mantém registro do bypass
 * no output para auditoria.
 */
export function bypassInstance(instanceId: string): void {
  const inst = getInstance(instanceId);
  if (!inst || inst.status !== "NOTOK") return;
  updateInstanceStatus(instanceId, "OK", {
    output: { ...(inst.output ?? {}), bypassed: true, originalError: inst.error },
    error: undefined,
  });
}

/** Force — cria uma nova instance imediata (Order Now / Run Now). */
export function forceInstance(def: JobDefinition): JobInstance {
  return orderJob(def, new Date(), true);
}

/**
 * forceRunInstance — "Run Now" sobre a instance EXISTENTE (não cria nova). Marca
 * como manual/forced e reagenda para agora; o tick trata `manual` como bypass de
 * deps (janela também some ao zerar scheduledAt). NÃO bypassa Confirm. Só vale
 * para WAITING/HOLD.
 */
export function forceRunInstance(instanceId: string): void {
  const all = localLoad<JobInstance>(INSTANCES_KEY);
  const inst = all.find((i) => i.id === instanceId);
  if (!inst || (inst.status !== "WAITING" && inst.status !== "HOLD")) return;
  inst.status = "WAITING";
  inst.manual = true;
  inst.scheduledAt = Date.now();
  saveAll(all);
}

