/**
 * ExecutorPort — interface de execução de um JobInstance.
 *
 * Fase 2: contrato abstrato.
 * MVP: MockExecutorAdapter (simula) + HttpExecutorAdapter (REST real).
 * Fase 6: AgentExecutorAdapter (WebSocket reverso para agente Go).
 * Fase 8: SsmExecutorAdapter (AWS SSM Run Command).
 */

import type { JobInstance } from "@/lib/orchestrator-model";

export interface ExecutionResult {
  ok: boolean;
  durationMs: number;
  output?: Record<string, unknown>;
  error?: string;
}

export interface ExecutorPort {
  /** Nome do adapter (para logs/debug). */
  readonly name: string;

  /**
   * Executa o job. Deve respeitar `instance.timeout` e `instance.dryRun`.
   * Não lança exceção para falha de negócio — retorna `ok: false`.
   * Lança apenas em falha infraestrutural (ex: adapter indisponível).
   */
  execute(instance: JobInstance): Promise<ExecutionResult>;

  /** Indica se este adapter suporta um dado jobType. */
  supports(jobType: string): boolean;
}
