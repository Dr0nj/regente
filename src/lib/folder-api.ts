/**
 * folder-api.ts — Wrappers REST para folder lifecycle (F11.6).
 *
 * Server mode apenas. Em localStorage mode, folders são implícitos (derivados
 * do campo `team` das definitions) e as operações devolvem stubs/no-ops.
 */
import { api, isServerMode } from "./server-client";

export interface FolderInfo {
  name: string;
  jobCount: number;
  archived?: boolean;
}

export async function listFolders(): Promise<FolderInfo[]> {
  if (!isServerMode()) return [];
  return api<FolderInfo[]>("/api/folders");
}

export async function createFolder(name: string): Promise<void> {
  if (!isServerMode()) {
    // localStorage mode: folder é só um label, nada a persistir até existir um job
    return;
  }
  await api<{ name: string }>("/api/folders", {
    method: "POST",
    body: JSON.stringify({ name }),
  });
}

export async function renameFolder(oldName: string, newName: string): Promise<void> {
  if (!isServerMode()) return;
  await api<{ name: string }>(`/api/folders/${encodeURIComponent(oldName)}`, {
    method: "PATCH",
    body: JSON.stringify({ newName }),
  });
}

export async function deleteFolder(name: string, force = false): Promise<void> {
  if (!isServerMode()) return;
  const q = force ? "?force=true" : "";
  await api<void>(`/api/folders/${encodeURIComponent(name)}${q}`, {
    method: "DELETE",
  });
}

export async function archiveFolder(name: string): Promise<void> {
  if (!isServerMode()) return;
  await api<{ name: string }>(`/api/folders/${encodeURIComponent(name)}/archive`, {
    method: "POST",
  });
}
