/**
 * server-instance-store — espelho local do estado de instances do server.
 *
 * Expoem a mesma API de `instance-store.ts` (getTodayInstances, onInstanceChange,
 * hold/release/cancel/rerun/skip/bypass/forceInstance) mas tudo roteia via REST
 * ao regente-server; atualizações em tempo real vêm por WebSocket `/ws/web`.
 *
 * Ativado via `runtime-bridge` quando `VITE_REGENTE_SERVER_URL` está setado.
 */

import type { JobDefinition, JobInstance, InstanceStatus, ConditionLogic } from "@/lib/orchestrator-model";
import { todayOrderDate } from "@/lib/orchestrator-model";
import { syncBusinessDate, onBusinessDateChange } from "@/lib/business-date";
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
  // Como foi forçada (schemaV15): "" = Run Now (sem tag) · "order" = Order Force
  // (colocada na mão → selo 🖐 MANUAL). `forced` sozinho não distingue os dois.
  forceMode?: string;
  carriedFrom?: string;
  confirmed?: boolean;
  cycleRuns?: number;
  dryRun?: boolean;
  holdScope?: string;
  heldFromStatus?: string;
  // M1 (schemaV18) — CONGELADOS na ordem, agora vêm NA LISTA também (o
  // Monitoring inteiro é imutável): label/tipo exibidos, gates (confirm/agente)
  // e as condições da ordem que desenham as linhas do grafo.
  label?: string;
  jobType?: string;
  confirmReq?: boolean;
  environment?: string;
  pinnedAgent?: string;
  condsIn?: string[];
  condsOutAdd?: string[];
  resources?: Record<string, number>; // F15 (schemaV19) — entrada do WAIT RESOURCE.
  condLogic?: ConditionLogic; // CL (schemaV21) — lógica AND/OR congelada; linhas OR (CL-4).
  // Só no DETALHE (GET /api/instances/{id}) — a lista não a carrega (payload
  // de escala): a action CONGELADA NA ORDEM, vinda da definition_snapshot.
  actionConfig?: Record<string, unknown>;
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
    // label/jobType: CONGELADOS na ordem (schemaV18), agora na lista — nunca
    // mais da def viva. Instance legada sem backfill vem VAZIA (não coagimos pra
    // definitionId): o canvas cai na def viva só quando label==="". Coagir
    // apagaria a distinção "frozen label que POR ACASO é igual ao id" × "legado
    // sem label" — era a raiz do card mostrar o nome novo da def viva.
    label: s.label ?? "",
    jobType: s.jobType ?? "",
    // actionConfig só no payload de DETALHE (a lista não a carrega por escala).
    actionConfig: s.actionConfig,
    // M1: gates e condições congelados da ordem — origem das linhas do grafo e
    // dos selos WAIT CONFIRM/WAIT AGENT no Monitoring.
    confirmReq: s.confirmReq,
    environment: s.environment,
    pinnedAgent: s.pinnedAgent,
    condsIn: s.condsIn,
    condsOutAdd: s.condsOutAdd,
    resources: s.resources,
    condLogic: s.condLogic,
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
    // Selo 🖐 MANUAL = SÓ Order Force (colocada na mão). Run Now (force_mode='')
    // força uma instance existente e NÃO ganha tag — pedido do usuário: "um job
    // que recebe Run Now não precisa de tag; um job forçado fica com MANUAL".
    manualOrder: s.forceMode === "order",
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
    // Status congelado pelo hold (schemaV16, hold geral): o Release restaura
    // este status. Só vem quando HELD; guia o rótulo do Release na UI.
    heldFrom: s.heldFromStatus ? (STATUS_MAP[s.heldFromStatus.toUpperCase()] ?? undefined) : undefined,
    // (Os antigos depsSatisfied/depsClaims do schemaV15 foram aposentados: a
    // linha do canvas agora é reflexo do POOL de condições — conditions-store.)
    // Output só existe se a run ACONTECEU (dispatch/agent/saída/fim). Antes o
    // objeto {text:"",exitCode:0} nascia sempre — e uma WAITING nunca rodada
    // mostrava "resultado" com exit 0 na aba Output do drawer.
    output: (s.output || s.agentId || s.startedAt || s.finishedAt)
      ? { text: s.output ?? "", exitCode: s.exitCode ?? 0, agentId: s.agentId }
      : undefined,
    retries: 0,
    timeout: 0,
  };
}

// keepDetail — preserva o enriquecimento do DETALHE quando um payload mais
// pobre (lista/WS, que não carregam actionConfig/label/jobType por escala)
// sobrescreve a linha no espelho. O snapshot da ordem é IMUTÁVEL, então
// preservar nunca mente — sem isto, cada refresh apagava o que o drawer
// tinha acabado de buscar.
function keepDetail(next: JobInstance, prev?: JobInstance): JobInstance {
  if (!prev) return next;
  if (next.actionConfig === undefined && prev.actionConfig !== undefined) next.actionConfig = prev.actionConfig;
  if (!next.jobType && prev.jobType) next.jobType = prev.jobType;
  if (!next.label && prev.label) next.label = prev.label;
  // M1: colunas congeladas — um payload WS mais pobre nunca deve apagar o que
  // já veio da lista (o snapshot é imutável, preservar não mente).
  if (next.condsIn === undefined && prev.condsIn !== undefined) next.condsIn = prev.condsIn;
  if (next.condsOutAdd === undefined && prev.condsOutAdd !== undefined) next.condsOutAdd = prev.condsOutAdd;
  if (next.confirmReq === undefined && prev.confirmReq !== undefined) next.confirmReq = prev.confirmReq;
  if (next.environment === undefined && prev.environment !== undefined) next.environment = prev.environment;
  if (next.pinnedAgent === undefined && prev.pinnedAgent !== undefined) next.pinnedAgent = prev.pinnedAgent;
  if (next.resources === undefined && prev.resources !== undefined) next.resources = prev.resources;
  return next;
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
  cache.set(s.id, keepDetail(toWeb(s), cache.get(s.id)));
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
        // idem heldFromStatus (schemaV16): o rótulo "Release (volta a X)" e o
        // Delete não podem esperar o refresh cheio ("" limpa no release).
        ...(p.heldFromStatus !== undefined
          ? { heldFrom: p.heldFromStatus ? (STATUS_MAP[p.heldFromStatus.toUpperCase()] ?? undefined) : undefined }
          : {}),
      };
      // ST-1 — o start/fim vêm NO evento (o server os inclui em started/
      // finished): o card mostra "início–fim" no instante em que o job termina,
      // sem esperar o refresh de cauda. Sem isto o horário de fim só aparecia no
      // próximo GET cheio (ou num clique) — a caixinha ficava com meia verdade.
      //
      // Sair de terminal APAGA o fim antigo: rerun/retry zeram finished_at no
      // server, e uma execução nova com o fim da anterior desenharia um intervalo
      // invertido no card.
      const started = parseTime(p.startedAt), finished = parseTime(p.finishedAt);
      if (next.status === "RUNNING" || next.status === "WAITING") {
        next.completedAt = undefined;
        next.durationMs = undefined;
      }
      if (started !== undefined) next.startedAt = started;
      if (finished !== undefined) next.completedAt = finished;
      if (next.startedAt != null && next.completedAt != null && next.completedAt >= next.startedAt) {
        next.durationMs = next.completedAt - next.startedAt;
      }
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
// que o resto sumiu. Dentro de um mesmo order_date a única remoção real é o
// DELETE explícito do operador (Control-M "Delete job", 2026-07-16) — e essa
// chega SEMPRE pelo canal dedicado: broadcast `instance.deleted` + remoção
// otimista no deleteInstance(), ambos via tombstone. Logo, uma resposta
// same-date "menor" que o cache é SEMPRE um snapshot parcial (SQLITE_BUSY/erro
// transitório truncando a lista num 200 sob rajada de rerun/cancel/set-OK),
// jamais uma remoção real. Por isso NÃO se deleta por ausência no mesmo dia:
// fazer isso era o que apagava o board. Remoção por ausência só faz sentido em
// TROCA DE DATA (limpar o que é de outra data). (Custo aceito: um delete feito
// por OUTRO cliente com o WS desconectado só some daqui no F5/troca de data.)
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
    cache.set(s.id, keepDetail(toWeb(s), cache.get(s.id)));
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
    // DAY-1 — pergunta ao server QUE DIA É antes do primeiro GET. Sem isso a
    // carga inicial usa o relógio do browser e pede um dia que pode não existir
    // (server em outro fuso, ou janela 00:00→daily_at): 200 com lista vazia,
    // board em branco. Falha aqui não bloqueia — cai no fallback e o
    // _connected/watchdog reconcilia.
    initialLoad = syncBusinessDate()
      .then(() => refresh())
      .catch((err) => {
        console.error("[server-instances] initial load failed", err);
        scheduleInitialRetry();
      }).finally(() => { initialLoad = null; });
  }
  return initialLoad;
}

// DAY-1 — a virada da daily é uma TROCA DE DATA no board: ressincroniza a data
// de negócio ANTES do refresh, senão o GET sai com o dia velho e o doFetch nem
// entra no ramo de limpeza (que só roda quando a data muda).
function refreshAcrossDayFlip(why: string): void {
  void syncBusinessDate()
    .then(() => refresh())
    .catch((err) => console.warn(`[server-instances] ${why} refresh failed`, err));
}

// Watchdog da virada: o gatilho normal é o evento `daily.started` do WS, mas se
// ele se perder (WS caído exatamente no daily_at, aba suspensa pelo browser) o
// board fica preso no dia anterior até um F5 — que é o sintoma do DAY-1 visto do
// outro lado. Uma checagem por minuto de uma linha só é barata.
//
// A condição é "o board está num dia que não é mais hoje" (`lastFetchDate`), e
// NÃO "a data mudou durante esta sincronização": qualquer outro caminho pode ter
// publicado a data nova antes (o /api/daily/status do rodapé, por exemplo) e aí
// o watchdog não veria transição nenhuma para reagir — foi exatamente o que
// aconteceu ao vivo, com o rodapé no dia novo e o board no velho.
const DAY_FLIP_WATCHDOG_MS = 60_000;
function startDayFlipWatchdog(): void {
  if (typeof window === "undefined") return;
  window.setInterval(() => {
    void syncBusinessDate().then(() => {
      if (lastFetchDate !== todayOrderDate()) refresh().catch(() => {});
    }).catch(() => {});
  }, DAY_FLIP_WATCHDOG_MS);
}

/* ── WS subscription (lazy) ── */

let wsSubscribed = false;
function ensureWs(): void {
  if (wsSubscribed) return;
  wsSubscribed = true;
  // Qualquer caminho que descubra a data nova (rodapé, sidebar, ViewPoint) vira
  // troca de dia no board na hora — o watchdog abaixo é só a rede de segurança.
  onBusinessDateChange(() => {
    refresh().catch((err) => console.warn("[server-instances] day-flip refresh failed", err));
  });
  startDayFlipWatchdog();
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
      // A daily rodou = o dia de negócio VIROU (DAY-1). O board tem que trocar
      // de dia junto, não só recarregar o dia velho.
      case "daily.started":
        refreshAcrossDayFlip("daily");
        break;
      // WS (re)conectou: ressincroniza tudo — cobre eventos perdidos offline e o
      // primeiro load que falhou com 401 antes do login (token novo já vale aqui).
      // Inclui a data: offline pode ter atravessado o daily_at.
      case "_connected":
        void syncBusinessDate().then(() => refresh()).catch(() => scheduleInitialRetry());
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

// deleteInstance — Control-M "Delete job": remove a ordem da tela e do state
// store. O server SÓ aceita com o job em HOLD (RUNNING nunca — não é
// segurável). Remoção otimista do espelho: tombstone + delete local na hora;
// o broadcast `instance.deleted` cobre os outros clientes.
export async function deleteInstance(id: string): Promise<void> {
  await api<void>(`/api/instances/${encodeURIComponent(id)}`, { method: "DELETE" });
  tombstones.set(id, Date.now());
  touchedAt.delete(id);
  if (cache.delete(id)) notify();
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

/* ── Output (OL-2): sysout da EXECUÇÃO por tentativa, live-tail ── */

export interface InstanceOutput {
  attempts: number;   // total de tentativas da instance (selector 1..attempts)
  attempt: number;    // tentativa desta resposta
  text: string;       // concat dos chunks (live enquanto RUNNING) ou consolidado
  complete: boolean;  // a tentativa não recebe mais chunks (terminou ou é anterior)
  exitCode?: number;  // só na tentativa final terminada
}

export async function fetchInstanceOutput(id: string, attempt?: number): Promise<InstanceOutput> {
  const q = attempt != null ? `?attempt=${attempt}` : "";
  return api<InstanceOutput>(`/api/instances/${encodeURIComponent(id)}/output${q}`);
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
    manualOrder: true, // Order Force (colocada na mão) → selo 🖐 MANUAL.
    retries: def.retries,
    timeout: def.timeout,
  };
}

export async function refreshFromServer(): Promise<void> {
  await refresh();
}

/* ── Detalhe de UMA instance (GET /api/instances/{id}) ── */

// A action CONGELADA NA ORDEM — o que a lista deliberadamente não carrega
// (payload de escala): label/jobType/actionConfig da definition_snapshot, a
// MESMA foto que o dispatch executa. É o que o drawer mostra em Action/Output.
export interface InstanceOrderDetail {
  label?: string;
  jobType?: string;
  actionConfig?: Record<string, unknown>;
  // M1: a DEF CONGELADA INTEIRA (definition_snapshot) — o drawer lê Schedule e
  // Condições daqui, não da def viva do Design. Ausente em instance legada.
  snapshotDef?: JobDefinition;
}

export async function fetchInstanceDetail(id: string): Promise<InstanceOrderDetail | null> {
  try {
    const s = await api<ServerInstance & { snapshotDef?: JobDefinition }>(`/api/instances/${encodeURIComponent(id)}`);
    if (!s?.id) return null;
    applyInstance(s); // o espelho ganha a linha mais rica de carona
    return { label: s.label, jobType: s.jobType, actionConfig: s.actionConfig, snapshotDef: s.snapshotDef };
  } catch (err) {
    console.warn("[server-instances] fetchInstanceDetail failed", err);
    return null;
  }
}

// UI-1 — puxa UMA instance pro cache pelo id (sidebar windowed: o dia tem mais
// jobs que o cap, a row clicada pode não estar no espelho local — sem isto o
// drawer não abriria). Antes usava o LIKE do listInstances; o detalhe por id
// é exato e ainda traz a action congelada da ordem.
export async function fetchInstanceById(id: string): Promise<JobInstance | null> {
  const hit = cache.get(id);
  if (hit) return hit;
  await fetchInstanceDetail(id);
  return cache.get(id) ?? null;
}
