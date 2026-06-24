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

export type Explanation = serverInstance.Explanation;
export type ExplainBlocker = serverInstance.ExplainBlocker;

// Explain só existe no server mode (depende do gating do scheduler). No modo
// local devolve null → a UI esconde o painel.
export const fetchInstanceExplain = isServerMode()
  ? serverInstance.fetchInstanceExplain
  : async (_id: string): Promise<serverInstance.Explanation | null> => null;

export type DailyDiff = serverInstance.DailyDiff;
export type DiffDefChange = serverInstance.DiffDefChange;

// Diff de Daily só existe no server mode (lê snapshots do estado durável).
export const fetchDailyDiff = isServerMode()
  ? serverInstance.fetchDailyDiff
  : async (_opts?: { from?: string; to?: string; folder?: string }): Promise<serverInstance.DailyDiff | null> => null;

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
