/**
 * folder-api.ts — REST client para folders (server mode).
 * Em local mode os wrappers retornam no-ops e a UI fica em
 * "single bucket" virtual (tudo em um só folder).
 *
 * Etapa 3+4 (2026-04-26): quando uma design session está ativa
 * (getDesignSessionId !== null), as chamadas roteiam para
 * /api/design/sessions/{sid}/folders — escreve local, sem push.
 */
import { api, getDesignSessionId } from "@/lib/server-client";
import { isServerMode } from "@/lib/server-client";

export interface FolderInfo {
  name: string;
  jobCount: number;
  archived?: boolean;
}

function listPath(): string {
  const sid = getDesignSessionId();
  return sid ? `/api/design/sessions/${encodeURIComponent(sid)}/folders` : "/api/folders";
}
function createPath(): string {
  const sid = getDesignSessionId();
  return sid ? `/api/design/sessions/${encodeURIComponent(sid)}/folders` : "/api/folders";
}

export async function listFolders(): Promise<FolderInfo[]> {
  if (!isServerMode()) return [];
  return await api<FolderInfo[]>(listPath());
}

export async function createFolder(name: string): Promise<void> {
  if (!isServerMode()) return;
  await api(createPath(), {
    method: "POST",
    body: JSON.stringify({ name }),
  });
}

export async function renameFolder(oldName: string, newName: string): Promise<void> {
  if (!isServerMode()) return;
  // Rename/delete/archive não roteiam para session no MVP — operações
  // destrutivas continuam usando workspace global (admin-only).
  await api(`/api/folders/${encodeURIComponent(oldName)}`, {
    method: "PATCH",
    body: JSON.stringify({ newName }),
  });
}

export async function deleteFolder(name: string, force = false): Promise<void> {
  if (!isServerMode()) return;
  await api(`/api/folders/${encodeURIComponent(name)}?force=${force}`, {
    method: "DELETE",
  });
}

export async function archiveFolder(name: string): Promise<void> {
  if (!isServerMode()) return;
  await api(`/api/folders/${encodeURIComponent(name)}/archive`, {
    method: "POST",
  });
}
