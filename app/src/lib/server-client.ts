/**
 * server-client.ts — Config + fetch helper + WS subscriber para o regente-server.
 *
 * Ativado quando `VITE_REGENTE_SERVER_URL` está definido.
 * Em server mode, o scheduler, storage e state runtime são do server Go.
 */

type Env = Record<string, string | undefined>;
const env = (import.meta as unknown as { env: Env }).env ?? {};

// `@origin` = same-origin (o servidor Go serve o SPA na mesma porta, ex.: atrás de um
// Cloudflare Tunnel). Resolve pra window.location.origin em runtime, então a URL do
// túnel pode mudar sem precisar rebuildar o front. Vazio = local mode; URL absoluta =
// server remoto explícito (Vite dev → localhost:9090).
const RAW_URL = env.VITE_REGENTE_SERVER_URL?.trim();
export const SERVER_URL: string | null =
  RAW_URL === "@origin"
    ? (typeof window !== "undefined" ? window.location.origin : null)
    : RAW_URL
      ? RAW_URL.replace(/\/$/, "")
      : null;
const ENV_TOKEN: string = env.VITE_REGENTE_TOKEN?.trim() || "dev-token";
const LS_TOKEN_KEY = "regente:authToken";

export function getAuthToken(): string {
  if (typeof window !== "undefined") {
    const t = window.localStorage.getItem(LS_TOKEN_KEY);
    if (t) return t;
  }
  return ENV_TOKEN;
}

export function setAuthToken(token: string | null): void {
  if (typeof window === "undefined") return;
  if (token) window.localStorage.setItem(LS_TOKEN_KEY, token);
  else window.localStorage.removeItem(LS_TOKEN_KEY);
  // Token mudou (login/logout): o WS aberto está autenticado com o token ANTIGO
  // (ou nem conectou, se o mount rodou antes do login). Reconecta já com o novo —
  // o "_connected" emitido no onopen faz os stores ressincronizarem (defs+instances),
  // sem exigir F5 depois de logar.
  reconnectNow();
}

// Listeners para eventos de auth (401 / logout)
type AuthListener = (event: "unauthorized" | "logout") => void;
const authListeners = new Set<AuthListener>();
export function onAuthEvent(fn: AuthListener): () => void {
  authListeners.add(fn);
  return () => { authListeners.delete(fn); };
}
function emitAuth(ev: "unauthorized" | "logout") {
  for (const fn of authListeners) try { fn(ev); } catch (e) { console.error(e); }
}

/** Backwards-compat: legado SERVER_TOKEN ainda exportado. */
export const SERVER_TOKEN: string = ENV_TOKEN;

export function isServerMode(): boolean {
  return !!SERVER_URL;
}

// === Design session (Etapa 3+4 do realinhamento, 2026-04-26) ===
// Quando setado, ServerApiAdapter e folder-api roteiam para
// /api/design/sessions/{sid}/* em vez de /api/definitions e /api/folders.
// null = trabalhando no workspace principal (uso normal: Monitoring).
let _designSessionId: string | null = null;
type SessionListener = (sid: string | null) => void;
const sessionListeners = new Set<SessionListener>();

// P7 (2026-04-26) — concurrent-tab guard via BroadcastChannel.
// Toda vez que esta aba ativa uma session, anuncia. Outras abas que estavam
// na mesma session (ou em qualquer session) recebem e podem reagir.
type TabBroadcastMsg =
  | { type: "session-claim"; sid: string; tabId: string; ts: number }
  | { type: "session-release"; sid: string; tabId: string };

const TAB_ID = (() => {
  try {
    return (crypto as Crypto).randomUUID();
  } catch {
    return `tab-${Math.random().toString(36).slice(2)}-${Date.now()}`;
  }
})();
let _bc: BroadcastChannel | null = null;
type ConflictListener = (sid: string, otherTabId: string) => void;
const conflictListeners = new Set<ConflictListener>();

function ensureBC(): BroadcastChannel | null {
  if (_bc) return _bc;
  if (typeof BroadcastChannel === "undefined") return null;
  _bc = new BroadcastChannel("regente:design-sessions");
  _bc.onmessage = (ev: MessageEvent<TabBroadcastMsg>) => {
    const msg = ev.data;
    if (!msg || msg.type !== "session-claim") return;
    if (msg.tabId === TAB_ID) return;
    if (_designSessionId && msg.sid === _designSessionId) {
      // Outra aba assumiu a MESMA session que estamos editando.
      for (const fn of conflictListeners) try { fn(msg.sid, msg.tabId); } catch (e) { console.error(e); }
    }
  };
  return _bc;
}

// Retomada pós-reload: a session vive no server (clone + DB), então o sid é
// persistido por browser e restaurado no boot — F5/troca de tela não perde mais
// o working set do Design. Validação (404 = expirou, actor ≠ me) é do V2Preview.
// O claim P7 é anunciado no restore pra manter a política "última aba ativa vence".
const LS_SESSION_KEY = "regente:designSessionId";
if (typeof window !== "undefined" && SERVER_URL) {
  try {
    const saved = window.localStorage.getItem(LS_SESSION_KEY);
    if (saved) {
      _designSessionId = saved;
      ensureBC()?.postMessage({ type: "session-claim", sid: saved, tabId: TAB_ID, ts: Date.now() } satisfies TabBroadcastMsg);
    }
  } catch { /* localStorage indisponível */ }
}

export function getDesignSessionId(): string | null { return _designSessionId; }
export function setDesignSessionId(sid: string | null): void {
  const prev = _designSessionId;
  _designSessionId = sid;
  if (typeof window !== "undefined") {
    try {
      if (sid) window.localStorage.setItem(LS_SESSION_KEY, sid);
      else window.localStorage.removeItem(LS_SESSION_KEY);
    } catch { /* localStorage indisponível */ }
  }
  if (sid && sid !== prev) {
    const bc = ensureBC();
    bc?.postMessage({ type: "session-claim", sid, tabId: TAB_ID, ts: Date.now() } satisfies TabBroadcastMsg);
  } else if (!sid && prev) {
    const bc = ensureBC();
    bc?.postMessage({ type: "session-release", sid: prev, tabId: TAB_ID } satisfies TabBroadcastMsg);
  }
  for (const fn of sessionListeners) try { fn(sid); } catch (e) { console.error(e); }
}
export function onDesignSessionChange(fn: SessionListener): () => void {
  sessionListeners.add(fn);
  return () => { sessionListeners.delete(fn); };
}

/** P7 — registra callback para conflito de aba (outra aba assumiu a mesma session). */
export function onDesignSessionConflict(fn: ConflictListener): () => void {
  ensureBC();
  conflictListeners.add(fn);
  return () => { conflictListeners.delete(fn); };
}

export function wsUrl(path: string): string {
  if (!SERVER_URL) throw new Error("server mode disabled");
  const u = SERVER_URL.replace(/^http/i, (m) => (m.toLowerCase() === "https" ? "wss" : "ws"));
  return `${u}${path}`;
}

/* ── REST ── */

export interface ApiError extends Error {
  status: number;
  body?: unknown;
}

export async function api<T = unknown>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  if (!SERVER_URL) throw new Error("server mode disabled");
  const headers = new Headers(init.headers ?? {});
  headers.set("Authorization", `Bearer ${getAuthToken()}`);
  if (init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const res = await fetch(`${SERVER_URL}${path}`, { ...init, headers });
  if (res.status === 401) {
    emitAuth("unauthorized");
  }
  if (!res.ok) {
    // O body só pode ser lido uma vez. Se tentarmos res.json() e falhar,
    // res.text() depois retorna string vazia (stream já consumido). Lemos
    // como texto e tentamos parsear como JSON em memória.
    const raw = await res.text().catch(() => "");
    let body: unknown = raw;
    if (raw) {
      try { body = JSON.parse(raw); } catch { /* keep raw text */ }
    }
    const detail = typeof body === "string" && body.trim()
      ? body.trim()
      : (body && typeof body === "object"
          // Mensagem humana antes do código de máquina ("schema"/"policy").
          ? ((body as { error?: string; message?: string }).message
              ?? (body as { error?: string; message?: string }).error
              ?? "")
          : "");
    const msg = detail
      ? `${res.status} ${res.statusText} @ ${path} — ${detail}`
      : `${res.status} ${res.statusText} @ ${path}`;
    const err = new Error(msg) as ApiError;
    err.status = res.status;
    err.body = body;
    throw err;
  }
  if (res.status === 204) return undefined as T;
  const ctype = res.headers.get("content-type") ?? "";
  if (ctype.includes("application/json")) return (await res.json()) as T;
  return (await res.text()) as unknown as T;
}

/* ── WS /ws/web ── */

export interface ServerEvent {
  event: string;
  payload?: unknown;
}

type EventListener = (ev: ServerEvent) => void;

const listeners = new Set<EventListener>();
let ws: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let backoffMs = 1000;
const MAX_BACKOFF = 15000;

function emit(ev: ServerEvent): void {
  for (const fn of listeners) {
    try { fn(ev); } catch (err) { console.error("[regente-ws] listener error", err); }
  }
}

function connect(): void {
  if (!SERVER_URL) return;
  let sock: WebSocket;
  try {
    const url = `${wsUrl("/ws/web")}?token=${encodeURIComponent(getAuthToken())}`;
    sock = new WebSocket(url);
  } catch (err) {
    console.error("[regente-ws] construct failed", err);
    scheduleReconnect();
    return;
  }
  ws = sock;

  sock.onopen = () => {
    backoffMs = 1000;
    // Evento sintético local (não vem do server): sinaliza "canal ao vivo de novo".
    // Stores usam isso pra ressincronizar estado perdido enquanto o WS esteve fora
    // (reconexão de rede, login com token novo, server reiniciado).
    emit({ event: "_connected" });
  };
  sock.onmessage = (msg) => {
    try {
      emit(JSON.parse(String(msg.data)) as ServerEvent);
    } catch (err) {
      console.error("[regente-ws] bad message", err, msg.data);
    }
  };
  // Guard `ws === sock`: um reconnectNow() pode já ter aberto um socket novo;
  // o onclose do antigo não pode derrubar/agendar por cima do atual.
  sock.onclose = () => {
    if (ws === sock) { ws = null; scheduleReconnect(); }
  };
  sock.onerror = () => { try { sock.close(); } catch { /* */ } };
}

function scheduleReconnect(): void {
  if (reconnectTimer) return;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    connect();
  }, backoffMs);
  backoffMs = Math.min(backoffMs * 2, MAX_BACKOFF);
}

/** Derruba o socket atual (se houver) e reconecta imediatamente com o token vigente. */
function reconnectNow(): void {
  if (!isServerMode() || listeners.size === 0) return;
  if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null; }
  backoffMs = 1000;
  const old = ws;
  ws = null;
  if (old) {
    old.onclose = null;
    try { old.close(); } catch { /* */ }
  }
  connect();
}

/** Subscribe a eventos do server. Lazy-connect na primeira subscription. */
export function onServerEvent(fn: EventListener): () => void {
  listeners.add(fn);
  if (!ws && !reconnectTimer && isServerMode()) connect();
  return () => { listeners.delete(fn); };
}
