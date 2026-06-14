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
} from "@/lib/orchestrator-model";
import { localLoad, localSave } from "@/lib/persistence";

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
}

/** Hold an instance (prevent execution) */
export function holdInstance(instanceId: string): void {
  updateInstanceStatus(instanceId, "HOLD");
}

/** Release a held instance back to WAITING */
export function releaseInstance(instanceId: string): void {
  const inst = getInstance(instanceId);
  if (inst?.status === "HOLD") {
    updateInstanceStatus(instanceId, "WAITING");
  }
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

