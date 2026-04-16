/**
 * HttpExecutorAdapter — executa jobs do tipo HTTP via fetch nativo.
 *
 * Envolve a função legada `executeHttpJob` expondo o contrato ExecutorPort.
 * Só declara suporte a jobType === "HTTP".
 */

import type { ExecutorPort, ExecutionResult } from "@/lib/ports/ExecutorPort";
import type { JobInstance } from "@/lib/orchestrator-model";
import type { HttpConfig } from "@/lib/job-config";
import { executeHttpJob } from "@/lib/http-executor";

export class HttpExecutorAdapter implements ExecutorPort {
  readonly name = "http";

  supports(jobType: string): boolean {
    return jobType === "HTTP";
  }

  async execute(instance: JobInstance): Promise<ExecutionResult> {
    const cfg = instance.actionConfig as HttpConfig | undefined;
    if (!cfg?.url || !cfg?.method) {
      return {
        ok: false,
        durationMs: 0,
        error: "HTTP job missing url/method in actionConfig",
      };
    }

    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), instance.timeout * 1000);

    try {
      const res = await executeHttpJob(cfg, !!instance.dryRun, controller.signal);
      return {
        ok: res.ok,
        durationMs: res.durationMs,
        output: {
          statusCode: res.statusCode,
          statusText: res.statusText,
          headers: res.responseHeaders,
          bodyPreview: res.responseBody,
        },
        error: res.ok ? undefined : `HTTP ${res.statusCode} ${res.statusText}`,
      };
    } catch (err) {
      return {
        ok: false,
        durationMs: 0,
        error: err instanceof Error ? err.message : String(err),
      };
    } finally {
      clearTimeout(timer);
    }
  }
}
