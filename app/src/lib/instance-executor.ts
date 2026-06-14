/**
 * Instance Executor — bridges JobInstance to the existing execution logic.
 *
 * Takes a JobInstance, builds a synthetic Node<JobNodeData> from its config,
 * runs it through the existing retry + simulateJob / executeHttpJob pipeline,
 * and returns a structured result.
 *
 * This avoids duplicating execution logic and lets the orchestrator model
 * reuse everything from Phase 7/10 (retry, HTTP, simulation, abort).
 */

import type { JobInstance } from "@/lib/orchestrator-model";
import type { HttpConfig } from "@/lib/job-config";
import { withRetry, type RetryResult } from "@/lib/retry";
import { executeHttpJob, type HttpJobResult } from "@/lib/http-executor";
import type { ExecutorFn } from "@/lib/orchestrator-scheduler";

/* ── Job Simulation (same as execution-engine.ts) ── */

const JOB_DURATION_MS: Record<string, [number, number]> = {
  LAMBDA: [400, 1200],
  BATCH: [1500, 3500],
  GLUE: [2000, 4000],
  STEP_FUNCTION: [800, 2000],
  CHOICE: [100, 300],
  PARALLEL: [200, 500],
  WAIT: [1000, 2000],
  HTTP: [300, 1000],
};

const FAILURE_RATE = 0.12;

async function runJobAction(
  instance: JobInstance,
  _attempt: number,
  signal?: AbortSignal,
): Promise<Record<string, unknown>> {
  // HTTP jobs with real config → actual HTTP call
  if (instance.jobType === "HTTP" && (instance.actionConfig as unknown as HttpConfig)?.url) {
    const httpConfig = instance.actionConfig as unknown as HttpConfig;
    const result: HttpJobResult = await executeHttpJob(
      httpConfig,
      instance.dryRun ?? false,
      signal,
    );

    if (!result.ok && !result.dryRun) {
      throw new Error(
        `HTTP ${result.statusCode} ${result.statusText}: ${result.responseBody.slice(0, 200)}`,
      );
    }

    return {
      exitCode: 0,
      durationMs: result.durationMs,
      output: `${result.dryRun ? "[DRY-RUN] " : ""}HTTP ${result.request.method} ${result.request.url} → ${result.statusCode || "OK"}`,
      httpResult: result,
    };
  }

  // All other jobs: simulation (MVP)
  const [minMs, maxMs] = JOB_DURATION_MS[instance.jobType] ?? [500, 1500];
  const duration = minMs + Math.random() * (maxMs - minMs);

  await new Promise<void>((resolve, reject) => {
    if (signal?.aborted) {
      reject(new DOMException("Aborted", "AbortError"));
      return;
    }
    const timer = setTimeout(resolve, duration);
    signal?.addEventListener(
      "abort",
      () => {
        clearTimeout(timer);
        reject(new DOMException("Aborted", "AbortError"));
      },
      { once: true },
    );
  });

  if (Math.random() < FAILURE_RATE) {
    throw new Error(`Job ${instance.label} failed: exit code 1`);
  }

  return {
    exitCode: 0,
    durationMs: Math.round(duration),
    output: `Completed ${instance.label}`,
  };
}

/* ── Executor Function (implements ExecutorFn interface) ── */

/**
 * The default executor for the OrchestratorScheduler.
 * Runs a single job instance with retry/backoff, respecting abort signals.
 */
export const instanceExecutor: ExecutorFn = async (instance, signal) => {
  const maxAttempts = (instance.retries ?? 2) + 1;
  const jobStart = performance.now();

  const result: RetryResult<Record<string, unknown>> = await withRetry(
    (attempt) => runJobAction(instance, attempt, signal),
    {
      maxAttempts,
      baseDelayMs: 500,
      maxDelayMs: 10_000,
      jitter: true,
      signal,
    },
  );

  const durationMs = Math.round(performance.now() - jobStart);

  return {
    success: result.success,
    durationMs,
    attempts: result.attempts,
    error: result.success ? undefined : String(result.error),
    output: result.success ? (result.value as Record<string, unknown>) : undefined,
  };
};
