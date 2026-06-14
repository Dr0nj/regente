/**
 * SchedulerPort — interface de agendamento.
 *
 * Fase 2: contrato abstrato. MVP usa tick browser (30s);
 * Fase 8: EventBridge + Lambda.
 */

import type { JobDefinition, JobInstance } from "@/lib/orchestrator-model";

export interface SchedulerPort {
  /** Inicia o tick de agendamento. Idempotente. */
  start(): void;

  /** Para o tick. */
  stop(): void;

  /** Status do scheduler (running/stopped). */
  isRunning(): boolean;

  /**
   * Avalia definitions e cria instances elegíveis para `orderDate`.
   * Chamado internamente pelo tick ou manualmente (Order Today).
   */
  evaluate(defs: JobDefinition[], orderDate: string): JobInstance[];

  /**
   * Cria uma instance imediatamente (Force / Run Now), ignorando schedule.
   */
  force(def: JobDefinition): JobInstance;
}
