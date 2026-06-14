/**
 * BrowserTickAdapter — SchedulerPort envolvendo o OrchestratorScheduler atual.
 *
 * Fase 2: bridge entre Port e implementação legada.
 * Fase 8: substituído por EventBridgeAdapter.
 */

import type { SchedulerPort } from "@/lib/ports/SchedulerPort";
import type { JobDefinition, JobInstance } from "@/lib/orchestrator-model";
import { createInstance, todayOrderDate } from "@/lib/orchestrator-model";
import { parseCron, nextRun } from "@/lib/cron";

export class BrowserTickAdapter implements SchedulerPort {
  private intervalId: ReturnType<typeof setInterval> | null = null;
  private tickMs: number;
  private onTick?: () => void;

  constructor(opts: { tickMs?: number; onTick?: () => void } = {}) {
    this.tickMs = opts.tickMs ?? 30_000;
    this.onTick = opts.onTick;
  }

  start(): void {
    if (this.intervalId !== null) return;
    this.intervalId = setInterval(() => this.onTick?.(), this.tickMs);
  }

  stop(): void {
    if (this.intervalId !== null) {
      clearInterval(this.intervalId);
      this.intervalId = null;
    }
  }

  isRunning(): boolean {
    return this.intervalId !== null;
  }

  evaluate(defs: JobDefinition[], orderDate: string): JobInstance[] {
    const now = new Date();
    const created: JobInstance[] = [];
    for (const def of defs) {
      if (!def.schedule?.enabled) continue;
      const expr = def.schedule.cronExpression?.trim();
      if (!expr) continue;
      try {
        const parsed = parseCron(expr);
        if (!parsed) continue;
        const next = nextRun(parsed, now);
        if (!next) continue;
        const d = next;
        const nextOrderDate = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
        if (nextOrderDate === orderDate) {
          created.push(createInstance(def, next, false));
        }
      } catch {
        // cron inválido → ignora silenciosamente (validação é da UI)
      }
    }
    return created;
  }

  force(def: JobDefinition): JobInstance {
    return createInstance(def, new Date(), true);
  }

  /** @internal usado apenas por testes para garantir `orderDate` de hoje. */
  static todayOrderDate = todayOrderDate;
}
