/**
 * folder-api.ts — REST client para folders (server mode).
 * Em local mode os wrappers retornam no-ops e a UI fica em
 * "single bucket" virtual (tudo em um só folder).
 */
import { api } from "@/lib/server-client";
import { isServerMode } from "@/lib/server-client";

export interface FolderInfo {
  name: string;
  jobCount: number;
  archived?: boolean;
}

export async function listFolders(): Promise<FolderInfo[]> {
  if (!isServerMode()) return [];
  return await api<FolderInfo[]>("/api/folders");
}

export async function createFolder(name: string): Promise<void> {
  if (!isServerMode()) return;
  await api("/api/folders", {
    method: "POST",
    body: JSON.stringify({ name }),
  });
}

export async function renameFolder(oldName: string, newName: string): Promise<void> {
  if (!isServerMode()) return;
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
