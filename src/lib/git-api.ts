// F13 GitOps client API.
import { api, isServerMode } from "./server-client";

export interface GitStatus {
  configured: boolean;
  source?: string;
  /** URL navegável do repo (sem credenciais) — base dos deep-links GitHub. */
  webUrl?: string;
  branch?: string;
  sha?: string;
  shortSha?: string;
  lastSync?: string;
  // F13.4 drift
  drift?: boolean;
  remoteSha?: string;
  driftErr?: string;
  // Token via UI — estado de auth para push/PR
  hasToken?: boolean;
  authMode?: "token" | "none" | string;
}

export interface PRResult {
  prNumber?: number;
  prUrl?: string;
  branch?: string;
  commitSha?: string;
  mode: "direct" | "pr-required" | "pr-optional" | string;
}

export interface DefinitionAuditEntry {
  id: number;
  ts: string;
  actor: string;
  action: string;
  team: string;
  definitionId: string;
  gitCommitSha?: string;
  prNumber?: number;
  prUrl?: string;
  branch?: string;
  mode?: string;
}

export async function fetchGitStatus(): Promise<GitStatus> {
  if (!isServerMode()) return { configured: false };
  return api<GitStatus>("/api/git/status");
}

export async function syncGit(): Promise<GitStatus> {
  return api<GitStatus>("/api/git/sync", { method: "POST" });
}

export async function checkDrift(): Promise<GitStatus> {
  if (!isServerMode()) return { configured: false };
  return api<GitStatus>("/api/git/drift");
}

/** Token via UI: grava o PAT no server (persiste em settings, propaga em runtime). */
export async function setGitToken(token: string): Promise<GitStatus> {
  return api<GitStatus>("/api/git/token", { method: "POST", body: JSON.stringify({ token }) });
}

/** Remove o PAT do server (volta a read-only). */
export async function clearGitToken(): Promise<GitStatus> {
  return api<GitStatus>("/api/git/token", { method: "DELETE" });
}

/** Remove a SQLite DB poluída no repo (push). Resolve o bug do WAL/DB. */
export async function cleanupGitDB(): Promise<{ changed: boolean; status?: string; error?: string }> {
  return api("/api/git/cleanup-db", { method: "POST" });
}

export async function fetchDefinitionAudit(team: string, id: string): Promise<DefinitionAuditEntry[]> {
  if (!isServerMode()) return [];
  return api<DefinitionAuditEntry[]>(`/api/definitions/${encodeURIComponent(team)}/${encodeURIComponent(id)}/audit`);
}
