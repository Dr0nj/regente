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

// ── Job-as-code (modo código do Design, 2026-07-06) ─────────────────────────

export interface SessionCode {
  session: string;
  folders: string[];
  count: number;
  code: string;
}

export interface CodePlan {
  creates: string[];
  updates: string[];
  deletes: string[];
  unchanged: number;
}

export interface CodeApplyResult {
  session: string;
  parsed: number;
  plan: CodePlan;
  errors: string[] | null;
  applied: boolean;
  results: BulkItemResult[] | null;
}

/** Working set (folders abertas) como YAML multi-doc — mesmo dialeto do Git. */
export async function getSessionCode(sid: string, folders?: string[]): Promise<SessionCode> {
  const q = folders && folders.length > 0 ? `?folders=${encodeURIComponent(folders.join(","))}` : "";
  return await api<SessionCode>(`/api/design/sessions/${encodeURIComponent(sid)}/code${q}`);
}

/** Valida (apply=false) ou aplica (apply=true) o YAML de volta no working set. */
export async function applySessionCode(
  sid: string,
  code: string,
  opts: { folders?: string[]; apply: boolean; allowDelete?: boolean },
): Promise<CodeApplyResult> {
  return await api<CodeApplyResult>(`/api/design/sessions/${encodeURIComponent(sid)}/code`, {
    method: "POST",
    body: JSON.stringify({ code, folders: opts.folders, apply: opts.apply, allowDelete: opts.allowDelete ?? false }),
  });
}

// ── CTM-3: Mass Update / Find & Update rico (2026-07-06) ────────────────────

export interface MassCriteria {
  ids?: string[];
  folders?: string[];
  jobType?: string;
  field?: string;
  regex?: string;
  fieldEmpty?: string;
}

export interface MassOperation {
  op:
    | "set-field" | "find-replace"
    | "add-action" | "remove-action"
    | "add-upstream" | "remove-upstream"
    | "set-variable" | "remove-variable"
    | "add-condition-in" | "remove-condition-in";
  field?: string;
  value?: unknown;
  onlyIfEmpty?: boolean;
  find?: string;
  replace?: string;
  action?: Record<string, unknown>;
  actionMatch?: Record<string, unknown>;
  upstream?: { from: string; condition?: string };
  key?: string;
  val?: string;
}

export interface MassChange { field: string; before: string; after: string }

export interface MassItem {
  id: string;
  team: string;
  label: string;
  changes: MassChange[] | null;
  error?: string;
  ok: boolean;
}

export interface MassUpdateResult {
  session: string;
  matched: number;
  changed: number;
  applied: boolean;
  items: MassItem[];
  undoDepth: number;
}

/** Preview (apply=false) ou aplicação (apply=true) do mass update. */
export async function massUpdateSession(
  sid: string,
  criteria: MassCriteria,
  operation: MassOperation,
  apply: boolean,
): Promise<MassUpdateResult> {
  return await api<MassUpdateResult>(`/api/design/sessions/${encodeURIComponent(sid)}/massupdate`, {
    method: "POST",
    body: JSON.stringify({ criteria, operation, apply }),
  });
}

export interface MassUndoResult {
  session: string;
  label: string;
  total: number;
  ok: number;
  failed: number;
  results: BulkItemResult[];
  undoDepth: number;
}

/** Desfaz a última aplicação de mass update da session. */
export async function massUpdateUndo(sid: string): Promise<MassUndoResult> {
  return await api<MassUndoResult>(`/api/design/sessions/${encodeURIComponent(sid)}/massupdate/undo`, {
    method: "POST",
    body: JSON.stringify({}),
  });
}
