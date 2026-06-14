/**
 * ServerApiAdapter — StoragePort em cima do regente-server Go.
 *
 * Map entre o shape server (runAt, params, upstream[{from,condition}]) e o
 * shape do modelo web (schedule.cronExpression, actionConfig, upstream[...]).
 *
 * Estratégia de compatibilidade:
 *  - O server é fonte de verdade. Campos server-only (runAt, jobType-native
 *    params) são preservados em `actionConfig` + `schedule` do modelo web.
 *  - `cronExpression` permanece vazio quando o server só traz runAt. A UI
 *    Design não depende de cron para swimlane/lista; apenas para "enabled".
 */

import type { StoragePort } from "@/lib/ports/StoragePort";
import type { JobDefinition, EdgeCondition } from "@/lib/orchestrator-model";
import { api, getDesignSessionId } from "@/lib/server-client";

// Path helpers — retornam URLs do session-scoped endpoint quando uma
// design session estiver ativa, senão do endpoint global do workspace.
function defsListPath(): string {
  const sid = getDesignSessionId();
  return sid ? `/api/design/sessions/${encodeURIComponent(sid)}/definitions` : "/api/definitions";
}
function defsSavePath(): string {
  const sid = getDesignSessionId();
  return sid ? `/api/design/sessions/${encodeURIComponent(sid)}/definitions` : "/api/definitions";
}
function defsDeletePath(team: string, id: string): string {
  const sid = getDesignSessionId();
  const t = encodeURIComponent(team);
  const i = encodeURIComponent(id);
  return sid
    ? `/api/design/sessions/${encodeURIComponent(sid)}/definitions/${t}/${i}`
    : `/api/definitions/${t}/${i}`;
}

interface ServerSchedule {
  enabled?: boolean;
  runAt?: string;
  windowFrom?: string;
  windowTo?: string;
  cyclic?: boolean;
  intervalMin?: number;
  frequency?: string;
  daysOfWeek?: string[];
  daysOfMonth?: number[];
  nthBusinessDays?: number[];
  monthsOfYear?: number[];
  advancedRule?: string;
  cronExpression?: string;
}

interface ServerCalendarRef { name: string; mode: string }

interface ServerDefinition {
  id: string;
  label: string;
  team?: string;
  jobType: string;
  schedule?: ServerSchedule;
  retries?: number;
  timeout?: number;
  dryRun?: boolean;
  calendar?: string;
  calendars?: ServerCalendarRef[];
  upstream?: Array<{ from: string; condition: EdgeCondition }>;
  // JSON wire field: server expõe `actionConfig` (json tag), embora o
  // YAML em disco mantenha `params` por compat. NUNCA renomear pra params
  // aqui — o server descarta o map silenciosamente.
  actionConfig?: Record<string, unknown>;
  agentId?: string;
}

// Schedule estruturado mapeado 1:1 (sem esconder em actionConfig — 2026-06-12).
function scheduleToWeb(s: ServerSchedule): JobDefinition["schedule"] {
  return {
    enabled: s.enabled ?? true,
    cronExpression: s.cronExpression ?? "",
    runAt: s.runAt,
    windowFrom: s.windowFrom,
    windowTo: s.windowTo,
    cyclic: s.cyclic,
    intervalMin: s.intervalMin,
    frequency: (s.frequency as JobDefinition["schedule"]["frequency"]) || "daily",
    daysOfWeek: s.daysOfWeek,
    daysOfMonth: s.daysOfMonth,
    nthBusinessDays: s.nthBusinessDays,
    monthsOfYear: s.monthsOfYear,
    advancedRule: s.advancedRule as JobDefinition["schedule"]["advancedRule"],
  };
}

function scheduleToServer(s: JobDefinition["schedule"]): ServerSchedule {
  return {
    enabled: s.enabled ?? true,
    runAt: s.runAt || undefined,
    windowFrom: s.windowFrom || undefined,
    windowTo: s.windowTo || undefined,
    cyclic: s.cyclic || undefined,
    intervalMin: s.intervalMin || undefined,
    frequency: s.frequency || "daily",
    daysOfWeek: s.daysOfWeek?.length ? s.daysOfWeek : undefined,
    daysOfMonth: s.daysOfMonth?.length ? s.daysOfMonth : undefined,
    nthBusinessDays: s.nthBusinessDays?.length ? s.nthBusinessDays : undefined,
    monthsOfYear: s.monthsOfYear?.length ? s.monthsOfYear : undefined,
    advancedRule: s.advancedRule || undefined,
    cronExpression: s.cronExpression || undefined,
  };
}

function toWeb(d: ServerDefinition): JobDefinition {
  return {
    id: d.id,
    label: d.label,
    jobType: d.jobType,
    team: d.team,
    schedule: scheduleToWeb(d.schedule ?? {}),
    retries: d.retries ?? 0,
    timeout: d.timeout ?? 300,
    calendar: d.calendar,
    calendars: d.calendars?.map((c) => ({ name: c.name, mode: c.mode === "exclude" ? "exclude" : "include" })),
    actionConfig: {
      ...(d.actionConfig ?? {}),
      _agentId: d.agentId,
    },
    dryRun: d.dryRun,
    upstream: d.upstream,
  };
}

function toServer(d: JobDefinition): ServerDefinition {
  const ac = (d.actionConfig ?? {}) as Record<string, unknown>;
  const { _agentId, _serverSchedule, ...actionConfig } = ac;
  void _serverSchedule; // descarta resíduo de versões antigas
  return {
    id: d.id,
    label: d.label,
    team: d.team,
    jobType: d.jobType,
    schedule: scheduleToServer(d.schedule),
    retries: d.retries,
    timeout: d.timeout,
    dryRun: d.dryRun,
    calendar: d.calendar || undefined,
    calendars: d.calendars?.length ? d.calendars : undefined,
    upstream: d.upstream,
    actionConfig,
    agentId: typeof _agentId === "string" ? _agentId : undefined,
  };
}

export class ServerApiAdapter implements StoragePort {
  private cache: Map<string, ServerDefinition> = new Map();

  async list(): Promise<JobDefinition[]> {
    const arr = await api<ServerDefinition[]>(defsListPath());
    this.cache.clear();
    for (const d of arr ?? []) this.cache.set(d.id, d);
    return (arr ?? []).map(toWeb);
  }

  async get(id: string): Promise<JobDefinition | null> {
    const cached = this.cache.get(id);
    if (cached) return toWeb(cached);
    await this.list();
    const hit = this.cache.get(id);
    return hit ? toWeb(hit) : null;
  }

  async save(def: JobDefinition): Promise<void> {
    const payload = toServer(def);
    const resp = await api<ServerDefinition | { definition: ServerDefinition; git?: unknown }>(
      defsSavePath(),
      { method: "POST", body: JSON.stringify(payload) },
    );
    // F13.2 — server may wrap response as {definition, git: PRResult}
    let saved: ServerDefinition | null = null;
    let git: unknown = null;
    if (resp && typeof resp === "object" && "definition" in resp) {
      saved = (resp as { definition: ServerDefinition }).definition;
      git = (resp as { git?: unknown }).git ?? null;
    } else {
      saved = resp as ServerDefinition;
    }
    if (saved) this.cache.set(saved.id, saved);
    if (git && typeof window !== "undefined") {
      window.dispatchEvent(new CustomEvent("regente:write-result", {
        detail: { action: "save", team: saved?.team, id: saved?.id, git },
      }));
    }
  }

  async remove(id: string): Promise<void> {
    const cached = this.cache.get(id);
    const team = cached?.team ?? "default";
    const resp = await api<unknown>(
      defsDeletePath(team, id),
      { method: "DELETE" },
    );
    this.cache.delete(id);
    const git = resp && typeof resp === "object" && "git" in resp
      ? (resp as { git?: unknown }).git
      : null;
    if (git && typeof window !== "undefined") {
      window.dispatchEvent(new CustomEvent("regente:write-result", {
        detail: { action: "delete", team, id, git },
      }));
    }
  }

  async saveBatch(defs: JobDefinition[]): Promise<void> {
    for (const d of defs) await this.save(d);
  }
}
