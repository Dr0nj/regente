/**
 * git-info.ts — cache do /api/git/status + builders de deep-link GitHub.
 *
 * O server expõe `webUrl` (URL navegável do repo, sem credenciais) no git
 * status. Daqui saem os links "ver no GitHub" da UI:
 *   - definition → blob do YAML em definitions/<folder>/<id>.yaml
 *   - commit     → /commit/<sha>
 */
import { fetchGitStatus, type GitStatus } from "./git-api";

let cached: GitStatus | null = null;
let inflight: Promise<GitStatus | null> | null = null;

export async function getGitInfo(): Promise<GitStatus | null> {
  if (cached?.configured) return cached;
  if (inflight) return inflight;
  inflight = fetchGitStatus()
    .then((st) => {
      cached = st;
      return st;
    })
    .catch(() => null)
    .finally(() => { inflight = null; });
  return inflight;
}

/** Invalida o cache (ex.: após sync que muda branch). */
export function invalidateGitInfo(): void {
  cached = null;
}

export function definitionFileUrl(st: GitStatus | null, folder: string, id: string): string | null {
  if (!st?.webUrl || !folder || !id) return null;
  const branch = st.branch || "main";
  return `${st.webUrl}/blob/${encodeURIComponent(branch)}/definitions/${encodeURIComponent(folder)}/${encodeURIComponent(id)}.yaml`;
}

export function commitUrl(st: GitStatus | null, sha?: string): string | null {
  if (!st?.webUrl || !sha) return null;
  return `${st.webUrl}/commit/${sha}`;
}
