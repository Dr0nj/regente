/**
 * server-instance-store — espelho local do estado de instances do server.
 *
 * Expoem a mesma API de `instance-store.ts` (getTodayInstances, onInstanceChange,
 * hold/release/cancel/rerun/skip/bypass/forceInstance) mas tudo roteia via REST
 * ao regente-server; atualizações em tempo real vêm por WebSocket `/ws/web`.
 *
 * Ativado via `runtime-bridge` quando `VITE_REGENTE_SERVER_URL` está setado.
 */

import type { JobDefinition, JobInstance, InstanceStatus } from "@/lib/orchestrator-model";
import { todayOrderDate } from "@/lib/orchestrator-model";
import { api, onServerEvent } from "@/lib/server-client";

/* ── Server shape ── */

interface ServerInstance {
  id: string;
  definitionId: string;
  orderDate: string;
  status: string;
  scheduledAt?: string;
  startedAt?: string;
  finishedAt?: string;
  agentId?: string;
  exitCode?: number;
  output?: string;
  forced?: boolean;
}

function parseTime(s?: string): number | undefined {
  if (!s) return undefined;
  const t = Date.parse(s);
  return Number.isFinite(t) ? t : undefined;
}

const STATUS_MAP: Record<string, InstanceStatus> = {
  WAITING: "WAITING",
  RUNNING: "RUNNING",
  OK: "OK",
  NOTOK: "NOTOK",
  HOLD: "HOLD",
  HELD: "HOLD",
  CANCELLED: "CANCELLED",
};

function toWeb(s: ServerInstance): JobInstance {
  const started = parseTime(s.startedAt);
  const completed = parseTime(s.finishedAt);
  return {
    id: s.id,
    definitionId: s.definitionId,
    label: s.definitionId,
    jobType: "",
    team: undefined,
    orderDate: s.orderDate,
    createdAt: parseTime(s.scheduledAt) ?? Date.now(),
    scheduledAt: parseTime(s.scheduledAt) ?? Date.now(),
    startedAt: started,
    completedAt: completed,
    status: STATUS_MAP[s.status.toUpperCase()] ?? "WAITING",
    durationMs: started && completed ? completed - started : undefined,
    attempts: 0,
    manual: !!s.forced,
    output: {
      text: s.output ?? "",
      exitCode: s.exitCode ?? 0,
      agentId: s.agentId,
    },
    retries: 0,
    timeout: 0,
  };
}

/* ── Cache local ── */

const cache = new Map<string, JobInstance>();
let lastFetchDate: string | null = null;
let initialLoad: Promise<void> | null = null;

type Listener = (instances: JobInstance[]) => void;
const listeners = new Set<Listener>();

function snapshot(): JobInstance[] {
  return [...cache.values()];
}

function notify(): void {
  const snap = snapshot();
  for (const fn of listeners) {
    try { fn(snap); } catch (err) { console.error("[server-instances] listener error", err); }
  }
}

function applyInstance(s: ServerInstance): void {
  cache.set(s.id, toWeb(s));
  notify();
}

function upsertFromEvent(payload: unknown): void {
  if (!payload || typeof payload !== "object") return;
  const p = payload as ServerInstance & { enrichFrom?: ServerInstance };
  // Server broadcasts partial payloads em ações (hold/release/cancel). Se vier
  // com id+status mas sem orderDate, faz refresh completo para manter cache íntegro.
  if (!p.orderDate || !p.scheduledAt) {
    const existing = cache.get(p.id);
    if (existing && p.status) {
      const next: JobInstance = {
        ...existing,
        status: STATUS_MAP[String(p.status).toUpperCase()] ?? existing.status,
      };
      cache.set(p.id, next);
      notify();
      // também agenda um refresh full para reconciliar
      void refresh();
      return;
    }
    void refresh();
    return;
  }
  applyInstance(p);
}

async function refresh(date = todayOrderDate()): Promise<void> {
  const arr = await api<ServerInstance[]>(`/api/instances?date=${encodeURIComponent(date)}`);
  cache.clear();
  for (const s of arr ?? []) cache.set(s.id, toWeb(s));
  lastFetchDate = date;
  notify();
}

function ensureLoaded(): Promise<void> {
  if (lastFetchDate === todayOrderDate()) return Promise.resolve();
  if (!initialLoad) {
    initialLoad = refresh().catch((err) => {
      console.error("[server-instances] initial load failed", err);
    }).finally(() => { initialLoad = null; });
  }
  return initialLoad;
}

/* ── WS subscription (lazy) ── */

let wsSubscribed = false;
function ensureWs(): void {
  if (wsSubscribed) return;
  wsSubscribed = true;
  onServerEvent((ev) => {
    switch (ev.event) {
      case "instance.changed":
        upsertFromEvent(ev.payload);
        break;
      case "instance.deleted": {
        const p = ev.payload as { id?: string } | undefined;
        if (p?.id && cache.delete(p.id)) notify();
        break;
      }
      case "daily.started":
        void refresh();
        break;
    }
  });
}

/* ── Public API (mirror de instance-store.ts) ── */

export function getTodayInstances(): JobInstance[] {
  ensureWs();
  void ensureLoaded();
  return snapshot();
}

export function getInstances(orderDate?: string): JobInstance[] {
  ensureWs();
  void ensureLoaded();
  const all = snapshot();
  return orderDate ? all.filter((i) => i.orderDate === orderDate) : all;
}

export function getInstance(id: string): JobInstance | undefined {
  return cache.get(id);
}

export function onInstanceChange(listener: Listener): () => void {
  ensureWs();
  void ensureLoaded();
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}

export async function holdInstance(id: string): Promise<void> {
  await api<void>(`/api/instances/${encodeURIComponent(id)}/hold`, { method: "POST" });
}

export async function releaseInstance(id: string): Promise<void> {
  await api<void>(`/api/instances/${encodeURIComponent(id)}/release`, { method: "POST" });
}

export async function cancelInstance(id: string): Promise<void> {
  await api<void>(`/api/instances/${encodeURIComponent(id)}/cancel`, { method: "POST" });
}

export async function rerunInstance(id: string): Promise<JobInstance | null> {
  await api<void>(`/api/instances/${encodeURIComponent(id)}/rerun`, { method: "POST" });
  // server reaproveita o mesmo id; devolve a própria instance após refresh
  await refresh();
  return cache.get(id) ?? null;
}

export async function skipInstance(id: string): Promise<void> {
  // v1: server não tem skip dedicado → cancela como aproximação
  await cancelInstance(id);
}

export async function bypassInstance(id: string): Promise<void> {
  // v1: server não tem bypass dedicado; UI vai ganhar endpoint em F14
  void id;
  console.warn("[server-instances] bypass not yet implemented on server");
}

export async function forceInstance(def: JobDefinition): Promise<JobInstance> {
  const r = await api<{ instanceId: string }>(
    `/api/definitions/${encodeURIComponent(def.id)}/force`,
    { method: "POST" },
  );
  await refresh();
  const hit = cache.get(r.instanceId);
  if (hit) return hit;
  // fallback stub
  return {
    id: r.instanceId,
    definitionId: def.id,
    label: def.label,
    jobType: def.jobType,
    team: def.team,
    orderDate: todayOrderDate(),
    createdAt: Date.now(),
    scheduledAt: Date.now(),
    status: "WAITING",
    attempts: 0,
    manual: true,
    retries: def.retries,
    timeout: def.timeout,
  };
}

export async function refreshFromServer(): Promise<void> {
  await refresh();
}
