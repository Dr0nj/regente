/**
 * differentials-api.ts — Diferenciais leva 2 (2026-07-07).
 *
 * D-2 pause/resume de folder · D-4 performance forecast · D-11 chaos inject ·
 * D-13 job templates · D-14 self-service portal. Todas server-side por
 * natureza (histórico/estado no DB do server) — em modo local retornam
 * vazio/no-op para a UI degradar sem quebrar.
 */
import { api, isServerMode } from "./server-client";
import type { JobDefinition } from "./orchestrator-model";

/* ── D-4 performance forecasting ── */

export interface DurationSample {
  orderDate: string;
  durationMs: number;
}

export interface PerfForecast {
  defId: string;
  samples: DurationSample[];
  p50Ms: number;
  p90Ms: number;
  trendMsPerRun: number;
  slower: boolean;
  nextMs: number;
  etaMs?: number;
}

export async function fetchPerfForecast(defId: string): Promise<PerfForecast | null> {
  if (!isServerMode()) return null;
  try {
    return await api<PerfForecast>(`/api/analytics/forecast?defId=${encodeURIComponent(defId)}`);
  } catch {
    return null;
  }
}

export async function fetchDayDurations(date?: string): Promise<Record<string, number>> {
  if (!isServerMode()) return {};
  try {
    const out = await api<{ p50ByDef: Record<string, number> }>(
      `/api/analytics/durations${date ? `?date=${date}` : ""}`,
    );
    return out.p50ByDef ?? {};
  } catch {
    return {};
  }
}

/* ── ADV-3 — Statistics por definition ── */

/** ST-1 — uma EXECUÇÃO terminada: entrou em RUNNING, terminou, durou tanto. */
export interface RunSample {
  instanceId: string;
  orderDate: string;
  attempt: number;
  status: string; // OK | NOTOK
  startedAt: string;
  finishedAt: string;
  durationMs: number;
  exitCode?: number;
}

export interface JobStats {
  defId: string;
  runs: number;
  ok: number;
  notok: number;
  successRate: number;
  minMs: number;
  avgMs: number;
  p50Ms: number;
  p90Ms: number;
  maxMs: number;
  /** Últimas execuções (nova → velha), até 10. */
  recent?: RunSample[];
  lastStatus?: string;
  lastFinishedAt?: string;
  lastDurationMs?: number;
  window: number;
}

export async function fetchJobStats(defId: string): Promise<JobStats | null> {
  if (!isServerMode()) return null;
  try {
    return await api<JobStats>(`/api/analytics/jobstats?defId=${encodeURIComponent(defId)}`);
  } catch {
    return null;
  }
}

/* ── ADV-3 — What-If (simulação de cenário, read-only) ── */

export interface WhatIfChange {
  defId: string;
  delayMin?: number;
  durationMs?: number;
  fail?: boolean;
  skip?: boolean;
}

export interface WhatIfRow {
  defId: string;
  label: string;
  team: string;
  wave: number;
  baseRuns: boolean;
  baseStart?: string;
  baseEnd?: string;
  scenRuns: boolean;
  scenStart?: string;
  scenEnd?: string;
  scenStatus?: string;
  deltaMs: number;
  state: "unchanged" | "delayed" | "earlier" | "blocked" | "skipped" | "fails" | "starts-running" | "not-run";
  slaBreachBase: boolean;
  slaBreachScen: boolean;
  impacted: boolean;
  changeInjected: boolean;
}

export interface WhatIfReport {
  orderDate: string;
  rows: WhatIfRow[];
  summary: {
    total: number;
    impacted: number;
    blocked: number;
    newSlaBreaches: number;
    makespanBaseMs: number;
    makespanScenMs: number;
  };
}

export async function runWhatIf(changes: WhatIfChange[], date?: string): Promise<WhatIfReport | null> {
  if (!isServerMode()) return null;
  try {
    return await api<WhatIfReport>("/api/whatif", {
      method: "POST",
      body: JSON.stringify({ date, changes }),
    });
  } catch {
    return null;
  }
}

/* ── D-2 pause/resume de workflow (folder) ── */

export async function pauseFolder(name: string): Promise<number> {
  const out = await api<{ affected: number }>(
    `/api/folders/${encodeURIComponent(name)}/pause`, { method: "POST" });
  return out.affected;
}

export async function resumeFolder(name: string): Promise<number> {
  const out = await api<{ affected: number }>(
    `/api/folders/${encodeURIComponent(name)}/resume`, { method: "POST" });
  return out.affected;
}

/* ── D-11 chaos engineering ── */

export async function injectFailure(instanceId: string): Promise<void> {
  await api(`/api/instances/${encodeURIComponent(instanceId)}/inject-failure`, { method: "POST" });
}

/* ── D-13 job templates ── */

export interface JobTemplate {
  name: string;
  description?: string;
  definition: JobDefinition;
  createdBy?: string;
  createdAt: string;
}

export async function fetchTemplates(): Promise<JobTemplate[]> {
  if (!isServerMode()) return [];
  try {
    return await api<JobTemplate[]>("/api/templates");
  } catch {
    return [];
  }
}

export async function saveTemplate(name: string, description: string, definition: JobDefinition): Promise<void> {
  await api("/api/templates", {
    method: "POST",
    body: JSON.stringify({ name, description, definition }),
  });
}

export async function deleteTemplate(name: string): Promise<void> {
  await api(`/api/templates/${encodeURIComponent(name)}`, { method: "DELETE" });
}

/* ── D-14 self-service portal ── */

export interface SelfServiceJob {
  id: string;
  label: string;
  team: string;
  description?: string;
  lastStatus?: string;
  lastRunAt?: string;
  running: boolean;
}

export async function fetchSelfServiceJobs(): Promise<SelfServiceJob[]> {
  return api<SelfServiceJob[]>("/api/selfservice/jobs");
}

export async function runSelfServiceJob(defId: string): Promise<string> {
  const out = await api<{ instanceId: string }>(
    `/api/selfservice/run/${encodeURIComponent(defId)}`, { method: "POST" });
  return out.instanceId;
}
