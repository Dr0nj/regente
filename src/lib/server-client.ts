/**
 * server-client.ts — Config + fetch helper + WS subscriber para o regente-server.
 *
 * Ativado quando `VITE_REGENTE_SERVER_URL` está definido.
 * Em server mode, o scheduler, storage e state runtime são do server Go.
 */

type Env = Record<string, string | undefined>;
const env = (import.meta as unknown as { env: Env }).env ?? {};

const RAW_URL = env.VITE_REGENTE_SERVER_URL?.trim();
export const SERVER_URL: string | null = RAW_URL ? RAW_URL.replace(/\/$/, "") : null;
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
    let body: unknown;
    try { body = await res.json(); } catch { body = await res.text().catch(() => undefined); }
    const err = new Error(`${res.status} ${res.statusText} @ ${path}`) as ApiError;
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

function connect(): void {
  if (!SERVER_URL) return;
  try {
    const url = `${wsUrl("/ws/web")}?token=${encodeURIComponent(getAuthToken())}`;
    ws = new WebSocket(url);
  } catch (err) {
    console.error("[regente-ws] construct failed", err);
    scheduleReconnect();
    return;
  }

  ws.onopen = () => { backoffMs = 1000; };
  ws.onmessage = (msg) => {
    try {
      const data = JSON.parse(String(msg.data)) as ServerEvent;
      for (const fn of listeners) {
        try { fn(data); } catch (err) { console.error("[regente-ws] listener error", err); }
      }
    } catch (err) {
      console.error("[regente-ws] bad message", err, msg.data);
    }
  };
  ws.onclose = () => { ws = null; scheduleReconnect(); };
  ws.onerror = () => { try { ws?.close(); } catch { /* */ } };
}

function scheduleReconnect(): void {
  if (reconnectTimer) return;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    connect();
  }, backoffMs);
  backoffMs = Math.min(backoffMs * 2, MAX_BACKOFF);
}

/** Subscribe a eventos do server. Lazy-connect na primeira subscription. */
export function onServerEvent(fn: EventListener): () => void {
  listeners.add(fn);
  if (!ws && !reconnectTimer && isServerMode()) connect();
  return () => { listeners.delete(fn); };
}
