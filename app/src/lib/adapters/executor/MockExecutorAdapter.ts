/**
 * MockExecutorAdapter — simula execução local (default).
 *
 * Suporta todos os jobTypes. Usado em dev/design para validar fluxo
 * sem chamadas externas. Latência e taxa de falha configuráveis.
 */

import type { ExecutorPort, ExecutionResult } from "@/lib/ports/ExecutorPort";
import type { JobInstance } from "@/lib/orchestrator-model";

export interface MockExecutorOptions {
  /** Latência artificial (ms). Default: 300-1500 aleatório. */
  latencyMs?: number | [number, number];
  /** Probabilidade de falha (0..1). Default: 0 (sempre OK). */
  failureRate?: number;
}

export class MockExecutorAdapter implements ExecutorPort {
  readonly name = "mock";
  private readonly opts: MockExecutorOptions;

  constructor(opts: MockExecutorOptions = {}) {
    this.opts = opts;
  }

  supports(_jobType: string): boolean {
    return true;
  }

  async execute(instance: JobInstance): Promise<ExecutionResult> {
    const start = Date.now();

    if (instance.dryRun) {
      return { ok: true, durationMs: 0, output: { dryRun: true } };
    }

    const latency = this.pickLatency();
    await new Promise((r) => setTimeout(r, latency));

    const fail = Math.random() < (this.opts.failureRate ?? 0);
    const durationMs = Date.now() - start;

    if (fail) {
      return { ok: false, durationMs, error: "mock failure (random)" };
    }
    return {
      ok: true,
      durationMs,
      output: { mock: true, jobType: instance.jobType, label: instance.label },
    };
  }

  private pickLatency(): number {
    const l = this.opts.latencyMs;
    if (typeof l === "number") return l;
    if (Array.isArray(l)) {
      const [min, max] = l;
      return min + Math.random() * (max - min);
    }
    return 300 + Math.random() * 1200;
  }
}
