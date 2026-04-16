/**
 * GitAdapter — StoragePort via GitHub Contents API.
 *
 * Persiste cada JobDefinition como YAML em `definitions/<id>.yaml`.
 * Cada save vira um commit — auditoria nativa via git log.
 *
 * Config via env (Vite):
 *   VITE_REGENTE_GIT_REPO      = "owner/repo"
 *   VITE_REGENTE_GIT_BRANCH    = "main" (default)
 *   VITE_REGENTE_GIT_TOKEN     = Personal Access Token com repo scope
 *   VITE_REGENTE_GIT_PATH      = "definitions" (default)
 *
 * Se VITE_REGENTE_GIT_REPO ou TOKEN faltam, `isEnabled()` retorna false
 * e o container deve usar LocalStorageAdapter.
 */

import type { StoragePort } from "@/lib/ports/StoragePort";
import type { JobDefinition } from "@/lib/orchestrator-model";
import { definitionToYaml, yamlToDefinition } from "@/lib/yaml";

interface GitConfig {
  repo: string;
  branch: string;
  token: string;
  path: string;
}

function readConfig(): GitConfig | null {
  const env = (typeof import.meta !== "undefined" ? import.meta.env : {}) as Record<string, string | undefined>;
  const repo = env.VITE_REGENTE_GIT_REPO;
  const token = env.VITE_REGENTE_GIT_TOKEN;
  if (!repo || !token) return null;
  return {
    repo,
    token,
    branch: env.VITE_REGENTE_GIT_BRANCH || "main",
    path: (env.VITE_REGENTE_GIT_PATH || "definitions").replace(/^\/|\/$/g, ""),
  };
}

function b64encode(s: string): string {
  return btoa(unescape(encodeURIComponent(s)));
}
function b64decode(s: string): string {
  return decodeURIComponent(escape(atob(s.replace(/\s/g, ""))));
}

interface ContentResponse {
  sha: string;
  content: string;
  name: string;
  path: string;
}

export class GitAdapter implements StoragePort {
  private cfg: GitConfig | null;

  constructor(cfg?: GitConfig | null) {
    this.cfg = cfg ?? readConfig();
  }

  static isEnabled(): boolean {
    return readConfig() !== null;
  }

  private require(): GitConfig {
    if (!this.cfg) throw new Error("GitAdapter: not configured (VITE_REGENTE_GIT_REPO/TOKEN missing)");
    return this.cfg;
  }

  private headers(): HeadersInit {
    const cfg = this.require();
    return {
      Authorization: `Bearer ${cfg.token}`,
      Accept: "application/vnd.github+json",
      "X-GitHub-Api-Version": "2022-11-28",
    };
  }

  private apiUrl(filePath: string): string {
    const cfg = this.require();
    return `https://api.github.com/repos/${cfg.repo}/contents/${filePath}?ref=${cfg.branch}`;
  }

  async list(): Promise<JobDefinition[]> {
    const cfg = this.require();
    const res = await fetch(`https://api.github.com/repos/${cfg.repo}/contents/${cfg.path}?ref=${cfg.branch}`, {
      headers: this.headers(),
    });
    if (res.status === 404) return [];
    if (!res.ok) throw new Error(`GitAdapter.list: ${res.status} ${res.statusText}`);
    const items = (await res.json()) as Array<{ name: string; path: string; type: string }>;
    const yamls = items.filter((i) => i.type === "file" && i.name.endsWith(".yaml"));
    const defs = await Promise.all(yamls.map(async (i) => {
      const fileRes = await fetch(this.apiUrl(i.path), { headers: this.headers() });
      if (!fileRes.ok) throw new Error(`GitAdapter.list.fetch(${i.path}): ${fileRes.status}`);
      const content = (await fileRes.json()) as ContentResponse;
      return yamlToDefinition(b64decode(content.content));
    }));
    return defs;
  }

  async get(id: string): Promise<JobDefinition | null> {
    const cfg = this.require();
    const path = `${cfg.path}/${id}.yaml`;
    const res = await fetch(this.apiUrl(path), { headers: this.headers() });
    if (res.status === 404) return null;
    if (!res.ok) throw new Error(`GitAdapter.get: ${res.status}`);
    const body = (await res.json()) as ContentResponse;
    return yamlToDefinition(b64decode(body.content));
  }

  async save(def: JobDefinition): Promise<void> {
    const cfg = this.require();
    const path = `${cfg.path}/${def.id}.yaml`;
    const yaml = definitionToYaml(def);

    let existingSha: string | undefined;
    const head = await fetch(this.apiUrl(path), { headers: this.headers() });
    if (head.ok) {
      const body = (await head.json()) as ContentResponse;
      existingSha = body.sha;
    }

    const res = await fetch(`https://api.github.com/repos/${cfg.repo}/contents/${path}`, {
      method: "PUT",
      headers: { ...this.headers(), "Content-Type": "application/json" },
      body: JSON.stringify({
        message: existingSha ? `update ${def.id}` : `create ${def.id}`,
        content: b64encode(yaml),
        branch: cfg.branch,
        sha: existingSha,
      }),
    });
    if (!res.ok) {
      const txt = await res.text();
      throw new Error(`GitAdapter.save: ${res.status} ${txt}`);
    }
  }

  async remove(id: string): Promise<void> {
    const cfg = this.require();
    const path = `${cfg.path}/${id}.yaml`;
    const head = await fetch(this.apiUrl(path), { headers: this.headers() });
    if (head.status === 404) return;
    if (!head.ok) throw new Error(`GitAdapter.remove.head: ${head.status}`);
    const body = (await head.json()) as ContentResponse;

    const res = await fetch(`https://api.github.com/repos/${cfg.repo}/contents/${path}`, {
      method: "DELETE",
      headers: { ...this.headers(), "Content-Type": "application/json" },
      body: JSON.stringify({
        message: `delete ${id}`,
        sha: body.sha,
        branch: cfg.branch,
      }),
    });
    if (!res.ok) throw new Error(`GitAdapter.remove: ${res.status}`);
  }

  async saveBatch(defs: JobDefinition[]): Promise<void> {
    for (const d of defs) {
      await this.save(d);
    }
  }
}
/**
 * GitAdapter — stub Fase 3.
 *
 * Persistirá cada JobDefinition como YAML em `definitions/<id>.yaml`
 * no repositório GitHub configurado, via GitHub Contents API.
 *
 * Por enquanto apenas lança `Not implemented` para forçar uso do
 * LocalStorageAdapter até a Fase 3.
 */
