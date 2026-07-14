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
  team?: string;
  orderDate: string;
  status: string;
  scheduledAt?: string;
  startedAt?: string;
  finishedAt?: string;
  agentId?: string;
  exitCode?: number;
  output?: string;
  forced?: boolean;
  carriedFrom?: string;
  confirmed?: boolean;
  cycleRuns?: number;
  dryRun?: boolean;
  holdScope?: string;
  depsSatisfied?: string[];
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
    // Snapshot da diária: a folder (team) é congelada NA instância pelo server
    // (coluna `team` no INSERT). O monitoring reflete o dia como foi schedulado —
    // apagar/mover o job no Design NÃO pode reescrever a daília corrente. Por isso
    // usamos o team da instância, não o da definition viva (que pode nem existir).
    team: s.team || undefined,
    orderDate: s.orderDate,
    createdAt: parseTime(s.scheduledAt) ?? Date.now(),
    scheduledAt: parseTime(s.scheduledAt) ?? Date.now(),
    startedAt: started,
    completedAt: completed,
    status: STATUS_MAP[s.status.toUpperCase()] ?? "WAITING",
    durationMs: started && completed ? completed - started : undefined,
    attempts: 0,
    manual: !!s.forced,
    carriedFrom: s.carriedFrom || undefined,
    confirmed: s.confirmed,
    cycleRuns: s.cycleRuns,
    // Snapshot da ordem (coluna dry_run no server, ver schemaV9): a flag que
    // acende o selo 👻GHOST no Monitoring vem CONGELADA da instância, não da def
    // viva. Assim, ligar dryRun no Design + publicar NÃO reescreve cards de jobs
    // já ordenados — o Monitoring só muda na próxima ordem (daily/force/manual).
    dryRun: s.dryRun,
    // Origem do HOLD (schemaV14): "folder" = segurado por uma pausa de folder
    // (não liberável 1-a-1); "" / ausente = hold individual. Snapshot da instância.
    holdScope: s.holdScope,
    // Latches de dependência (schemaV15): a linha verde do canvas é POR INSTÂNCIA
    // (claim de evento), não o status vivo do pai. Presente (mesmo []) = server novo.
    depsSatisfied: s.depsSatisfied,
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

// Anti-race do refresh (bug: forçar N jobs fazia cards sumirem até F5).
// Cada força/evento WS disparava um GET /api/instances concorrente e cada um
// dava cache.clear()+repopula; uma resposta ANTIGA chegando por último apagava
// instances recém-criadas — e no monitoring nó do canvas = instance.
// - touchedAt: última mutação via WS por id; snapshot iniciado ANTES do evento
//   não pode deletar nem regredir esse id (o refresh de cauda reconcilia).
// - tombstones: ids deletados via WS; snapshot antigo não os ressuscita.
const touchedAt = new Map<string, number>();
const tombstones = new Map<string, number>();

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
  touchedAt.set(s.id, Date.now());
  cache.set(s.id, toWeb(s));
  notify();
}

function upsertFromEvent(payload: unknown): void {
  if (!payload || typeof payload !== "object") return;
  const p = payload as ServerInstance & { enrichFrom?: ServerInstance };
  // Server broadcasts partial payloads em ações (hold/release/cancel). Se vier
  // com id+status mas sem orderDate, faz refresh completo para manter cache íntegro.
  if (!p.orderDate || !p.scheduledAt) {
    if (p.id) touchedAt.set(p.id, Date.now());
    const existing = p.id ? cache.get(p.id) : undefined;
    if (existing && p.status) {
      const next: JobInstance = {
        ...existing,
        status: STATUS_MAP[String(p.status).toUpperCase()] ?? existing.status,
        // hold/release parciais carregam holdScope (schemaV14) — aplica na hora
        // pro cadeado (folder vs individual) refletir antes do refresh de cauda.
        ...(p.holdScope !== undefined ? { holdScope: p.holdScope } : {}),
      };
      cache.set(p.id, next);
      notify();
      // também agenda um refresh full para reconciliar
      refresh().catch((err) => console.warn("[server-instances] refresh failed", err));
      return;
    }
    refresh().catch((err) => console.warn("[server-instances] refresh failed", err));
    return;
  }
  applyInstance(p);
}

// Cap de segurança do canvas legado (ReactFlow não virtualiza): nunca puxa mais
// que legacyCap() instances do dia. UI-1: o cap limita SÓ o desenho do grafo —
// a sidebar ACTIVE JOBS virtualizada mostra o dia inteiro via summary/page
// (windowed) e o ViewPoint (ScaleMonitor) cobre o drill-down a 100k–1M.
// Configurável em Settings (regente:legacyCap); teto 5000 = parseLimit do server.
const LEGACY_CAP_DEFAULT = 2000;
export function legacyCap(): number {
  if (typeof window === "undefined") return LEGACY_CAP_DEFAULT;
  const v = parseInt(window.localStorage.getItem("regente:legacyCap") ?? "", 10);
  return Number.isFinite(v) ? Math.min(5000, Math.max(500, v)) : LEGACY_CAP_DEFAULT;
}

// doFetch — UMA rodada de GET + reconcile por MERGE (nunca cache.clear()).
// gen guard: se um fetch mais novo começou enquanto este aguardava a resposta,
// a resposta velha é descartada inteira — resposta fora de ordem não pode
// reescrever o board com um snapshot do passado.
//
// RAIZ DO "cards somem em rajada e só voltam com F5" (fechada de vez):
// O GET /api/instances é um snapshot POSITIVO — prova o que EXISTE, nunca prova
// que o resto sumiu. Dentro de um mesmo order_date o runtime NUNCA apaga instance
// (não há DELETE FROM instances em produção — só o seed dev; e o server nem emite
// instance.deleted). Logo, uma resposta same-date "menor" que o cache é SEMPRE um
// snapshot parcial (SQLITE_BUSY/erro transitório de leitura truncando a lista num
// 200 sob rajada de rerun/cancel/set-OK), jamais uma remoção real. Por isso NÃO se
// deleta por ausência no mesmo dia: fazer isso era o que apagava o board. Remoção
// por ausência só faz sentido em TROCA DE DATA (limpar o que é de outra data).
let fetchGen = 0;

async function doFetch(date: string): Promise<void> {
  const gen = ++fetchGen;
  const startedAt = Date.now();
  const arr = await api<ServerInstance[]>(
    `/api/instances?date=${encodeURIComponent(date)}&limit=${legacyCap()}`,
  );
  if (gen !== fetchGen) return; // obsoleto: fetch mais novo já em voo/aplicado

  // Upsert positivo do snapshot.
  for (const s of arr ?? []) {
    // Deletado via WS durante o fetch → snapshot antigo não ressuscita.
    if ((tombstones.get(s.id) ?? 0) >= startedAt) continue;
    // Mutado via WS durante o fetch → snapshot não regride status; o refresh
    // de cauda (agendado pelo próprio evento) reconcilia com dados completos.
    if (cache.has(s.id) && (touchedAt.get(s.id) ?? 0) >= startedAt) continue;
    cache.set(s.id, toWeb(s));
  }
  // Remoção por AUSÊNCIA só em troca de data (ver bloco acima). No mesmo dia,
  // um id que "faltou" no snapshot é truncamento transitório — preserva.
  if (date !== lastFetchDate) {
    for (const [id, inst] of [...cache]) {
      if (inst.orderDate === date) continue; // é do dia consultado → mantém
      if ((touchedAt.get(id) ?? 0) >= startedAt) continue; // mutado via WS agora → mantém
      cache.delete(id);
    }
  }
  // Marcas antigas já cumpriram o papel (só protegem contra snapshots que
  // começaram antes delas; futuros snapshots começam depois de agora).
  for (const [id, ts] of touchedAt) if (ts < startedAt) touchedAt.delete(id);
  for (const [id, ts] of tombstones) if (ts < startedAt) tombstones.delete(id);

  lastFetchDate = date;
  notify();
}

// refresh — single-flight com rodada de cauda coalescida. Nunca há dois GETs
// em voo; qualquer gatilho DURANTE um fetch agenda exatamente UMA rodada extra
// disparada depois dele (portanto depois do commit que originou o gatilho).
// Rajada de N forças/eventos = no máximo 2 fetches, e o último sempre vê tudo.
let inFlight: Promise<void> | null = null;
let trailing: Promise<void> | null = null;
let trailingDate: string | null = null;

function refresh(date = todayOrderDate()): Promise<void> {
  if (inFlight) {
    trailingDate = date;
    if (!trailing) {
      trailing = inFlight.catch(() => {}).then(() => {
        trailing = null;
        const d = trailingDate ?? todayOrderDate();
        trailingDate = null;
        return refresh(d);
      });
    }
    return trailing;
  }
  inFlight = doFetch(date).finally(() => { inFlight = null; });
  return inFlight;
}

// Retry da carga inicial: um 401 pré-login, um hiccup do tunnel ou o server ainda
// subindo NÃO podem deixar o board vazio até o usuário dar F5. Reagenda até a
// primeira carga bem-sucedida (para de tentar quando lastFetchDate é de hoje).
let retryTimer: ReturnType<typeof setTimeout> | null = null;
function scheduleInitialRetry(): void {
  if (retryTimer) return;
  retryTimer = setTimeout(() => {
    retryTimer = null;
    if (lastFetchDate === todayOrderDate()) return;
    refresh().catch((err) => {
      console.warn("[server-instances] retry load failed", err);
      scheduleInitialRetry();
    });
  }, 5000);
}

function ensureLoaded(): Promise<void> {
  if (lastFetchDate === todayOrderDate()) return Promise.resolve();
  if (!initialLoad) {
    initialLoad = refresh().catch((err) => {
      console.error("[server-instances] initial load failed", err);
      scheduleInitialRetry();
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
      // Ações em massa (D-2 pausa/resume de folder): o payload é um resumo
      // {action, folder, total}, não instances — refresh full pra o mirror pegar
      // os novos status/hold_scope (senão o cadeado da folder só aparecia no F5).
      case "instance.bulk":
        refresh().catch((err) => console.warn("[server-instances] bulk refresh failed", err));
        break;
      case "instance.deleted": {
        const p = ev.payload as { id?: string } | undefined;
        if (p?.id) {
          tombstones.set(p.id, Date.now());
          touchedAt.delete(p.id);
          if (cache.delete(p.id)) notify();
        }
        break;
      }
      case "daily.started":
        refresh().catch((err) => console.warn("[server-instances] refresh failed", err));
        break;
      // WS (re)conectou: ressincroniza tudo — cobre eventos perdidos offline e o
      // primeiro load que falhou com 401 antes do login (token novo já vale aqui).
      case "_connected":
        void refresh().catch(() => scheduleInitialRetry());
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
  // Set OK (Control-M parity): flip NOTOK/CANCELLED -> OK no server,
  // destrava sucessores on-success.
  await api<void>(`/api/instances/${encodeURIComponent(id)}/set-ok`, { method: "POST" });
  await refresh();
}

export async function confirmInstance(id: string): Promise<void> {
  // Control-M Confirm: libera um job confirm:true parado no gate WAIT_CONFIRM.
  await api<void>(`/api/instances/${encodeURIComponent(id)}/confirm`, { method: "POST" });
  await refresh();
}

// forceRunInstance — "Run Now" sobre a instance EXISTENTE: bypassa os gates de
// impedimento (janela/deps/conditions/recursos) e roda ESTE mesmo job. NÃO
// bypassa agente indisponível nem Confirm. Diferente de forceInstance(def), que
// cria uma nova ordem — aqui nenhuma instance nova é criada.
export async function forceRunInstance(id: string): Promise<void> {
  await api<void>(`/api/instances/${encodeURIComponent(id)}/force`, { method: "POST" });
  await refresh();
}

export interface InstanceEvent {
  id: number;
  instanceId: string;
  ts: string;       // RFC3339
  kind: string;     // ordered | started | submitted | finished | timeout | cancelled | held | released | rerun | force-ordered | set-ok
  actor?: string;   // scheduler | operator | agent
  message?: string;
}

export async function fetchInstanceEvents(id: string): Promise<InstanceEvent[]> {
  return api<InstanceEvent[]>(`/api/instances/${encodeURIComponent(id)}/events`);
}

/* ── Explain ("por que esse job não rodou?") ── */

export interface ExplainBlocker {
  kind: "WAIT_WINDOW" | "WINDOW_CLOSED" | "WAIT_CONFIRM" | "WAIT_DEP" | "BLOCKED_DEP" | "WAIT_CONDITION" | "WAIT_AGENT" | "WAIT_RESOURCE";
  detail: string;
  upstream?: string;
  upstreamStatus?: string;
  condition?: string;
  resource?: string;
  want?: number;
  used?: number;
  capacity?: number;
}

export interface Explanation {
  instanceId: string;
  definitionId: string;
  status: string;
  runnable: boolean;
  summary: string;
  blockers: ExplainBlocker[];
}

export async function fetchInstanceExplain(id: string): Promise<Explanation> {
  return api<Explanation>(`/api/instances/${encodeURIComponent(id)}/explain`);
}

/* ── Diff de Daily ("o que mudou entre duas diárias?") ── */

export interface DiffDefRef { defId: string; label?: string; team?: string }
export interface DiffFieldChange { field: string; from: string; to: string }
export interface DiffDefChange { defId: string; label?: string; team?: string; changes: DiffFieldChange[] }
export interface DailyDiff {
  dateA: string;
  dateB: string;
  folder?: string;
  commitA?: string;
  commitB?: string;
  sameCommit: boolean;
  counts: { totalA: number; totalB: number; added: number; removed: number; changed: number; unchanged: number };
  added: DiffDefRef[];
  removed: DiffDefRef[];
  changed: DiffDefChange[];
  truncated: boolean;
}

/* ── Blast Radius ("se eu cancelar/segurar este job agora?") ── */

export interface BlastNode { defId: string; label?: string; team?: string; status?: string; hasSla?: boolean; depth: number }
export interface BlastRadius {
  instanceId: string;
  definitionId: string;
  status: string;
  orderDate: string;
  counts: { downstream: number; slaAtRisk: number; teamsAffected: number; maxDepth: number };
  downstream: BlastNode[];
  slaAtRisk: BlastNode[];
  truncated: boolean;
}

export async function fetchBlastRadius(id: string): Promise<BlastRadius> {
  return api<BlastRadius>(`/api/instances/${encodeURIComponent(id)}/blast-radius`);
}

/* ── Job Neighborhood (grafo local) ── */

export interface NeighborNode { defId: string; label?: string; team?: string; status?: string; depth: number; condition?: string; hasSla?: boolean }
export interface Neighborhood {
  instanceId: string;
  definitionId: string;
  label?: string;
  team?: string;
  status: string;
  orderDate: string;
  radius: number;
  upstream: NeighborNode[];
  downstream: NeighborNode[];
  truncated: boolean;
}

export async function fetchNeighborhood(id: string, radius = 1): Promise<Neighborhood> {
  return api<Neighborhood>(`/api/instances/${encodeURIComponent(id)}/neighborhood?radius=${radius}`);
}

/* ── RCA (causa raiz) ── */

export interface RCACause { defId: string; label?: string; team?: string; status: string; depth: number; reason?: string }
export interface RCA {
  instanceId: string;
  definitionId: string;
  status: string;
  orderDate: string;
  summary: string;
  roots: RCACause[];
  chain?: string[];
}

export async function fetchRCA(id: string): Promise<RCA> {
  return api<RCA>(`/api/instances/${encodeURIComponent(id)}/rca`);
}

/* ── Dry Run ("simular uma daily futura sem materializar") ── */

export type DryRunOutcome = "RUN" | "WAIT" | "BLOCKED" | "NOT_SCHEDULED";
export interface DryRunJob { defId: string; label?: string; team?: string; outcome: DryRunOutcome; reason: string; dependsOn?: string[] }
export interface DryRun {
  date: string;
  hasCalendars: boolean;
  counts: { run: number; wait: number; blocked: number; notScheduled: number; total: number };
  jobs: DryRunJob[];
  truncated: boolean;
}

export async function fetchDryRun(date?: string): Promise<DryRun> {
  const q = date ? `?date=${encodeURIComponent(date)}` : "";
  return api<DryRun>(`/api/daily/dryrun${q}`);
}

export async function fetchDailyDiff(opts?: { from?: string; to?: string; folder?: string }): Promise<DailyDiff> {
  const qs = new URLSearchParams();
  if (opts?.from) qs.set("from", opts.from);
  if (opts?.to) qs.set("to", opts.to);
  if (opts?.folder) qs.set("folder", opts.folder);
  const q = qs.toString();
  return api<DailyDiff>(`/api/daily/diff${q ? "?" + q : ""}`);
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

// UI-1 — puxa UMA instance pro cache pelo id (sidebar windowed: o dia tem mais
// jobs que o cap, a row clicada pode não estar no espelho local — sem isto o
// drawer não abriria). Usa o LIKE do listInstances (q casa id OU definition_id,
// daí o limit>1 + match exato aqui).
export async function fetchInstanceById(id: string): Promise<JobInstance | null> {
  const hit = cache.get(id);
  if (hit) return hit;
  try {
    const date = lastFetchDate ?? todayOrderDate();
    const arr = await api<ServerInstance[]>(
      `/api/instances?date=${encodeURIComponent(date)}&q=${encodeURIComponent(id)}&limit=50`,
    );
    const exact = (arr ?? []).find((s) => s.id === id);
    if (!exact) return null;
    applyInstance(exact);
    return cache.get(id) ?? null;
  } catch (err) {
    console.warn("[server-instances] fetchInstanceById failed", err);
    return null;
  }
}
