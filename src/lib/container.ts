/**
 * Container — composition root (DI).
 *
 * Fase 2: escolha estática dos adapters MVP.
 * Fase 8: flag de ambiente troca para adapters AWS (DynamoDB/EventBridge/SSM)
 *         sem tocar em domínio ou UI.
 *
 * Uso:
 *   import { container } from "@/lib/container";
 *   const defs = await container.storage.list();
 *   const res  = await container.executorFor("HTTP").execute(instance);
 *
 * Regra de ouro: componentes React NÃO importam adapters diretamente.
 * Importam apenas `container` (ou os Ports via hook/context futuro).
 */

import type { StoragePort } from "@/lib/ports/StoragePort";
import type { SchedulerPort } from "@/lib/ports/SchedulerPort";
import type { ExecutorPort } from "@/lib/ports/ExecutorPort";

import { LocalStorageAdapter } from "@/lib/adapters/storage/LocalStorageAdapter";
import { BrowserTickAdapter } from "@/lib/adapters/scheduler/BrowserTickAdapter";
import { MockExecutorAdapter } from "@/lib/adapters/executor/MockExecutorAdapter";
import { HttpExecutorAdapter } from "@/lib/adapters/executor/HttpExecutorAdapter";

export interface RegenteContainer {
  storage: StoragePort;
  scheduler: SchedulerPort;
  executors: ExecutorPort[];
  /** Seleciona o primeiro executor que suporta o jobType. Fallback: mock. */
  executorFor(jobType: string): ExecutorPort;
}

function buildContainer(): RegenteContainer {
  const storage = new LocalStorageAdapter();
  const scheduler = new BrowserTickAdapter();

  const http = new HttpExecutorAdapter();
  const mock = new MockExecutorAdapter();

  // Ordem importa: mais específico primeiro, genérico por último.
  const executors: ExecutorPort[] = [http, mock];

  return {
    storage,
    scheduler,
    executors,
    executorFor(jobType: string): ExecutorPort {
      return executors.find((e) => e.supports(jobType)) ?? mock;
    },
  };
}

export const container: RegenteContainer = buildContainer();
