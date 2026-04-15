/**
 * Orchestrator Scheduler — MVP
 *
 * The brain of Regente. Periodically checks job definitions,
 * determines which should run based on their cron schedule,
 * orders instances, and triggers execution at the right time.
 *
 * MVP: runs in the browser via setInterval (30s tick).
 * Future: EventBridge Rules + Step Functions.
 *
 * Flow:
 * 1. tick() runs every 30s
 * 2. For each enabled job definition with a valid cron:
 *    a. Check if the cron matches the current time window
 *    b. Check if an instance already exists for this definition today
 *    c. If no instance yet and cron matches → order the instance (WAITING)
 * 3. For each WAITING instance whose scheduledAt has passed:
 *    a. Trigger execution → status becomes RUNNING
 *    b. On completion → status becomes OK or NOTOK
 */

import type { JobDefinition, JobInstance } from "@/lib/orchestrator-model";
import { parseCron, nextRun } from "@/lib/cron";
import {
  orderJob,
  getTodayInstances,
  updateInstanceStatus,
  getInstancesForDefinition,
} from "@/lib/instance-store";

/* ── Types ── */

export type ExecutorFn = (
  instance: JobInstance,
  signal: AbortSignal,
) => Promise<{ success: boolean; durationMs: number; attempts: number; error?: string; output?: Record<string, unknown> }>;

export type SchedulerListener = (event: SchedulerEvent) => void;

export interface SchedulerEvent {
  type: "instance-ordered" | "instance-started" | "instance-completed" | "tick";
  instance?: JobInstance;
  timestamp: Date;
  data?: Record<string, unknown>;
}

/* ── Scheduler ── */

export class OrchestratorScheduler {
  private definitions = new Map<string, JobDefinition>();
  private intervalId: ReturnType<typeof setInterval> | null = null;
  private executor: ExecutorFn | null = null;
  private listeners = new Set<SchedulerListener>();
  private runningInstances = new Map<string, AbortController>();
  private _started = false;

  /** Register an executor function (called to actually run jobs) */
  setExecutor(fn: ExecutorFn): void {
    this.executor = fn;
  }

  /** Subscribe to scheduler events */
  on(listener: SchedulerListener): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  private emit(event: SchedulerEvent): void {
    for (const fn of this.listeners) {
      try { fn(event); } catch { /* */ }
    }
  }

  /** Load/reload all job definitions into the scheduler */
  loadDefinitions(defs: JobDefinition[]): void {
    this.definitions.clear();
    for (const d of defs) {
      if (d.schedule.enabled) {
        this.definitions.set(d.id, d);
      }
    }
  }

  /** Add or update a single definition */
  upsertDefinition(def: JobDefinition): void {
    if (def.schedule.enabled) {
      this.definitions.set(def.id, def);
    } else {
      this.definitions.delete(def.id);
    }
  }

  /** Remove a definition from the scheduler */
  removeDefinition(defId: string): void {
    this.definitions.delete(defId);
  }

  /** Start the scheduler loop */
  start(): void {
    if (this._started) return;
    this._started = true;
    this.intervalId = setInterval(() => this.tick(), 30_000);
    // First tick immediately
    this.tick();
  }

  /** Stop the scheduler loop */
  stop(): void {
    if (this.intervalId) {
      clearInterval(this.intervalId);
      this.intervalId = null;
    }
    this._started = false;
  }

  get started(): boolean {
    return this._started;
  }

  /** Get all loaded definitions */
  getDefinitions(): JobDefinition[] {
    return [...this.definitions.values()];
  }

  /** Abort a running instance */
  abortInstance(instanceId: string): void {
    const ctrl = this.runningInstances.get(instanceId);
    if (ctrl) {
      ctrl.abort();
      this.runningInstances.delete(instanceId);
    }
  }

  /**
   * Force/Order — manually trigger a job (Run Now).
   * Creates an instance and immediately queues for execution.
   */
  forceOrder(def: JobDefinition): JobInstance {
    const inst = orderJob(def, new Date(), true);
    this.emit({ type: "instance-ordered", instance: inst, timestamp: new Date() });
    // Immediately execute
    this.executeInstance(inst);
    return inst;
  }

  /* ── Core tick ── */

  private tick(): void {
    const now = new Date();

    this.emit({ type: "tick", timestamp: now, data: { definitionCount: this.definitions.size } });

    // 1. Check definitions → order instances for today
    for (const def of this.definitions.values()) {
      this.checkAndOrder(def, now);
    }

    // 2. Check WAITING instances → trigger if scheduled time has passed
    const today = getTodayInstances();
    for (const inst of today) {
      if (inst.status === "WAITING" && inst.scheduledAt <= now.getTime()) {
        this.executeInstance(inst);
      }
    }
  }

  /**
   * Check if a definition should have an instance ordered for today.
   * Only creates one instance per definition per day (for daily schedules).
   */
  private checkAndOrder(def: JobDefinition, now: Date): void {
    // Parse the cron
    const cron = parseCron(def.schedule.cronExpression);
    if (!cron) return;

    // Check if this cron should fire today
    const next = nextRun(cron, new Date(now.getFullYear(), now.getMonth(), now.getDate(), 0, 0, 0));
    if (!next) return;

    // Only order if the next run is today
    const nextDate = next.toISOString().slice(0, 10);
    const todayStr = now.toISOString().slice(0, 10);
    if (nextDate !== todayStr) return;

    // Check if we already ordered this definition today
    const existing = getInstancesForDefinition(def.id);
    const alreadyOrdered = existing.some(
      (i) => i.status !== "CANCELLED" && !i.manual,
    );
    if (alreadyOrdered) return;

    // Order the instance (WAITING until scheduled time)
    const inst = orderJob(def, next, false);
    this.emit({ type: "instance-ordered", instance: inst, timestamp: now });
  }

  /** Execute a single instance */
  private async executeInstance(inst: JobInstance): Promise<void> {
    if (!this.executor) return;
    if (this.runningInstances.has(inst.id)) return; // already running

    const ctrl = new AbortController();
    this.runningInstances.set(inst.id, ctrl);

    // Mark RUNNING
    updateInstanceStatus(inst.id, "RUNNING");
    this.emit({ type: "instance-started", instance: { ...inst, status: "RUNNING" }, timestamp: new Date() });

    try {
      const result = await this.executor(inst, ctrl.signal);

      const status = result.success ? "OK" : "NOTOK";
      updateInstanceStatus(inst.id, status as "OK" | "NOTOK", {
        durationMs: result.durationMs,
        attempts: result.attempts,
        error: result.error,
        output: result.output,
      });

      this.emit({
        type: "instance-completed",
        instance: { ...inst, status: status as "OK" | "NOTOK" },
        timestamp: new Date(),
        data: result,
      });
    } catch (e) {
      updateInstanceStatus(inst.id, "NOTOK", {
        error: e instanceof Error ? e.message : String(e),
        attempts: 1,
      });
      this.emit({
        type: "instance-completed",
        instance: { ...inst, status: "NOTOK" },
        timestamp: new Date(),
        data: { error: e instanceof Error ? e.message : String(e) },
      });
    } finally {
      this.runningInstances.delete(inst.id);
    }
  }
}

/** Singleton scheduler instance */
export const orchestratorScheduler = new OrchestratorScheduler();
