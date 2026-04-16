/**
 * SsmExecutorAdapter — stub Fase 8.
 *
 * Fará AWS SSM Run Command em targets EC2/ECS (self-hosted runners).
 * Ainda não implementado: lança para deixar claro que é roadmap.
 */

import type { ExecutorPort, ExecutionResult } from "@/lib/ports/ExecutorPort";
import type { JobInstance } from "@/lib/orchestrator-model";

export class SsmExecutorAdapter implements ExecutorPort {
  readonly name = "ssm";

  supports(_jobType: string): boolean {
    return false; // desabilitado até Fase 8
  }

  async execute(_instance: JobInstance): Promise<ExecutionResult> {
    throw new Error("SsmExecutorAdapter: not implemented (Fase 8)");
  }
}
