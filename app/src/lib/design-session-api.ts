/**
 * design-session-api.ts — REST client para design sessions (Etapa 3+4+5).
 * Modelo conceitual em memory/core/regente-product-model.md.
 */
import { api } from "@/lib/server-client";

export interface DesignSession {
  id: string;
  actor: string;
  folders: string[];
  newFolders?: string[];
  baseSha: string;
  createdAt: string;
  lastTouch: string;
  /** true = working tree do clone tem trabalho não publicado (list/get). */
  dirty?: boolean;
}

export interface PublishResult {
  mode: "direct" | "pr-required" | "noop";
  commitSha?: string;
  branch?: string;
  prNumber?: number;
  prUrl?: string;
  forcedPR?: boolean;
}

export async function createDesignSession(
  folders: string[],
  newFolders: string[] = [],
): Promise<DesignSession> {
  return await api<DesignSession>("/api/design/sessions", {
    method: "POST",
    body: JSON.stringify({ folders, newFolders }),
  });
}

export async function listDesignSessions(): Promise<DesignSession[]> {
  return await api<DesignSession[]>("/api/design/sessions");
}

export async function getDesignSession(sid: string): Promise<DesignSession> {
  return await api<DesignSession>(`/api/design/sessions/${encodeURIComponent(sid)}`);
}

export async function deleteDesignSession(sid: string): Promise<void> {
  await api(`/api/design/sessions/${encodeURIComponent(sid)}`, { method: "DELETE" });
}

export async function publishDesignSession(
  sid: string,
  message?: string,
): Promise<PublishResult> {
  return await api<PublishResult>(
    `/api/design/sessions/${encodeURIComponent(sid)}/publish`,
    { method: "POST", body: JSON.stringify({ message: message ?? "" }) },
  );
}

// P8 (2026-04-26) — drift badge.
export interface SessionStatus {
  ahead: number;
  behind: number;
  error?: string;
}
export async function getDesignSessionStatus(sid: string): Promise<SessionStatus> {
  return await api<SessionStatus>(`/api/design/sessions/${encodeURIComponent(sid)}/status`);
}

// Re-alinhamento Fase 2 (2026-04-27) — gerência mid-session de folders ativas.

export interface AddFolderResult {
  name: string;
  session: string;
  willForcePR: boolean;
}

/** Cria uma folder NOVA na session (vai virar newFolder → força PR no Publish). */
export async function createSessionFolder(sid: string, name: string): Promise<AddFolderResult> {
  return await api<AddFolderResult>(
    `/api/design/sessions/${encodeURIComponent(sid)}/folders`,
    { method: "POST", body: JSON.stringify({ name }) },
  );
}

/** Abre uma folder EXISTENTE no escopo da session (não força PR). */
export async function openSessionFolder(sid: string, name: string): Promise<AddFolderResult> {
  return await api<AddFolderResult>(
    `/api/design/sessions/${encodeURIComponent(sid)}/folders/open`,
    { method: "POST", body: JSON.stringify({ name }) },
  );
}

// F11.8 (2026-06-12) — Find & Update / bulk em definitions DA session.

export interface BulkItemResult {
  id: string;
  ok: boolean;
  status?: string;
  error?: string;
}

export interface BulkResponse {
  action: string;
  total: number;
  ok: number;
  failed: number;
  results: BulkItemResult[];
}

export interface BulkDefinitionPatch {
  retries?: number;
  timeout?: number;
  agentId?: string;
  calendar?: string;
}

/** Bulk em definitions da session: move-folder | patch | delete (transacional por item). */
export async function bulkSessionDefinitions(
  sid: string,
  action: "move-folder" | "patch" | "delete",
  ids: string[],
  opts?: { targetFolder?: string; patch?: BulkDefinitionPatch },
): Promise<BulkResponse> {
  return await api<BulkResponse>(
    `/api/design/sessions/${encodeURIComponent(sid)}/bulk`,
    {
      method: "POST",
      body: JSON.stringify({ action, ids, targetFolder: opts?.targetFolder, patch: opts?.patch }),
    },
  );
}

/** Bulk em instances (Monitoring): hold | release | cancel | rerun | set-ok. */
export async function bulkInstancesApi(
  action: "hold" | "release" | "cancel" | "rerun" | "set-ok",
  ids: string[],
): Promise<BulkResponse> {
  return await api<BulkResponse>("/api/bulk/instances", {
    method: "POST",
    body: JSON.stringify({ action, ids }),
  });
}
