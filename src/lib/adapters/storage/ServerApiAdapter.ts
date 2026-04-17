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
import { api } from "@/lib/server-client";

interface ServerSchedule {
  enabled?: boolean;
  runAt?: string;
  windowFrom?: string;
  windowTo?: string;
  cyclic?: boolean;
  intervalMin?: number;
}

interface ServerDefinition {
  id: string;
  label: string;
  team?: string;
  jobType: string;
  schedule?: ServerSchedule;
  retries?: number;
  timeout?: number;
  dryRun?: boolean;
  upstream?: Array<{ from: string; condition: EdgeCondition }>;
  params?: Record<string, unknown>;
  agentId?: string;
}

function toWeb(d: ServerDefinition): JobDefinition {
  const sched = d.schedule ?? {};
  return {
    id: d.id,
    label: d.label,
    jobType: d.jobType,
    team: d.team,
    schedule: {
      cronExpression: "",
      description: sched.runAt ? `daily ${sched.runAt}` : "server-scheduled",
      enabled: sched.enabled ?? true,
    },
    retries: d.retries ?? 0,
    timeout: d.timeout ?? 300,
    actionConfig: {
      ...(d.params ?? {}),
      _serverSchedule: sched,
      _agentId: d.agentId,
    },
    dryRun: d.dryRun,
    upstream: d.upstream,
  };
}

function toServer(d: JobDefinition): ServerDefinition {
  const ac = (d.actionConfig ?? {}) as Record<string, unknown>;
  const serverSched = (ac._serverSchedule as ServerSchedule | undefined) ?? {
    enabled: d.schedule.enabled,
  };
  const { _serverSchedule, _agentId, ...params } = ac;
  void _serverSchedule;
  return {
    id: d.id,
    label: d.label,
    team: d.team,
    jobType: d.jobType,
    schedule: { ...serverSched, enabled: d.schedule.enabled ?? true },
    retries: d.retries,
    timeout: d.timeout,
    dryRun: d.dryRun,
    upstream: d.upstream,
    params,
    agentId: typeof _agentId === "string" ? _agentId : undefined,
  };
}

export class ServerApiAdapter implements StoragePort {
  private cache: Map<string, ServerDefinition> = new Map();

  async list(): Promise<JobDefinition[]> {
    const arr = await api<ServerDefinition[]>("/api/definitions");
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
    const saved = await api<ServerDefinition>("/api/definitions", {
      method: "POST",
      body: JSON.stringify(payload),
    });
    if (saved) this.cache.set(saved.id, saved);
  }

  async remove(id: string): Promise<void> {
    const cached = this.cache.get(id);
    const team = cached?.team ?? "default";
    await api<void>(
      `/api/definitions/${encodeURIComponent(team)}/${encodeURIComponent(id)}`,
      { method: "DELETE" },
    );
    this.cache.delete(id);
  }

  async saveBatch(defs: JobDefinition[]): Promise<void> {
    for (const d of defs) await this.save(d);
  }
}
