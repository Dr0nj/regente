/**
 * runtime-bridge — escolhe a origem dos stores (local vs server).
 *
 * Se `VITE_REGENTE_SERVER_URL` estiver setado, roteia instance-store +
 * scheduler-runtime para os adapters server-*. Caso contrário, usa os
 * locais (localStorage + BrowserTickAdapter).
 *
 * Consumers devem importar daqui (V2Preview.tsx), não dos módulos locais
 * diretamente, para que a troca seja transparente.
 */

import { isServerMode } from "@/lib/server-client";

import * as localInstance from "@/lib/instance-store";
import * as serverInstance from "@/lib/server-instance-store";

import * as localScheduler from "@/lib/scheduler-runtime";
import * as serverScheduler from "@/lib/server-scheduler-runtime";

/* ── Instance API ── */

export const getTodayInstances = isServerMode()
  ? serverInstance.getTodayInstances
  : localInstance.getTodayInstances;

export const onInstanceChange = isServerMode()
  ? serverInstance.onInstanceChange
  : localInstance.onInstanceChange;

export const holdInstance = isServerMode()
  ? serverInstance.holdInstance
  : localInstance.holdInstance;

export const releaseInstance = isServerMode()
  ? serverInstance.releaseInstance
  : localInstance.releaseInstance;

export const cancelInstance = isServerMode()
  ? serverInstance.cancelInstance
  : localInstance.cancelInstance;

export const rerunInstance = isServerMode()
  ? serverInstance.rerunInstance
  : localInstance.rerunInstance;

export const skipInstance = isServerMode()
  ? serverInstance.skipInstance
  : localInstance.skipInstance;

export const bypassInstance = isServerMode()
  ? serverInstance.bypassInstance
  : localInstance.bypassInstance;

export const forceInstance = isServerMode()
  ? serverInstance.forceInstance
  : localInstance.forceInstance;

export type InstanceEvent = serverInstance.InstanceEvent;

export const fetchInstanceEvents = isServerMode()
  ? serverInstance.fetchInstanceEvents
  : async (_id: string): Promise<serverInstance.InstanceEvent[]> => [];

/* ── Scheduler API ── */

export const runDaily = isServerMode()
  ? serverScheduler.runDaily
  : localScheduler.runDaily;

export const startScheduler = isServerMode()
  ? serverScheduler.startScheduler
  : localScheduler.startScheduler;

export const stopScheduler = isServerMode()
  ? serverScheduler.stopScheduler
  : localScheduler.stopScheduler;

export const updateSchedulerDefs = isServerMode()
  ? serverScheduler.updateSchedulerDefs
  : localScheduler.updateSchedulerDefs;

export const getLastDailyRun = isServerMode()
  ? serverScheduler.getLastDailyRun
  : localScheduler.getLastDailyRun;
