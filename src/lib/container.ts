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
import { GitAdapter } from "@/lib/adapters/storage/GitAdapter";
import { ServerApiAdapter } from "@/lib/adapters/storage/ServerApiAdapter";
import { BrowserTickAdapter } from "@/lib/adapters/scheduler/BrowserTickAdapter";
import { MockExecutorAdapter } from "@/lib/adapters/executor/MockExecutorAdapter";
import { HttpExecutorAdapter } from "@/lib/adapters/executor/HttpExecutorAdapter";
import { isServerMode, SERVER_URL } from "@/lib/server-client";

export interface RegenteContainer {
  storage: StoragePort;
  scheduler: SchedulerPort;
  executors: ExecutorPort[];
  /** Seleciona o primeiro executor que suporta o jobType. Fallback: mock. */
  executorFor(jobType: string): ExecutorPort;
  /** Nome do backend de storage ativo (para UI/debug). */
  storageBackend: "server" | "git" | "localStorage";
  /** URL do regente-server quando em server mode. */
  serverUrl: string | null;
}

function buildContainer(): RegenteContainer {
  const serverOn = isServerMode();
  const gitEnabled = !serverOn && GitAdapter.isEnabled();

  let storage: StoragePort;
  let backend: RegenteContainer["storageBackend"];
  if (serverOn) {
    storage = new ServerApiAdapter();
    backend = "server";
  } else if (gitEnabled) {
    storage = new GitAdapter();
    backend = "git";
  } else {
    storage = new LocalStorageAdapter();
    backend = "localStorage";
  }

  const scheduler = new BrowserTickAdapter();

  const http = new HttpExecutorAdapter();
  const mock = new MockExecutorAdapter();
  const executors: ExecutorPort[] = [http, mock];

  return {
    storage,
    scheduler,
    executors,
    storageBackend: backend,
    serverUrl: SERVER_URL,
    executorFor(jobType: string): ExecutorPort {
      return executors.find((e) => e.supports(jobType)) ?? mock;
    },
  };
}

export const container: RegenteContainer = buildContainer();
