/**
 * Formata e classifica erros do server-client `api()` para apresentação humana.
 *
 * `api()` joga `Error` com `.body` (string ou objeto JSON) e `.status` (HTTP code).
 * `extractMessage` retorna o melhor texto disponível (corpo do server > message).
 * `classifyError` devolve título + dica acionável para um modal.
 */

export interface ApiLike {
  message?: string;
  body?: unknown;
  status?: number;
}

export function extractMessage(e: unknown): string {
  if (!e || typeof e !== "object") return String(e);
  const a = e as ApiLike;
  const body = a.body;
  if (typeof body === "string" && body.trim()) return body.trim();
  if (body && typeof body === "object") {
    const o = body as { error?: string; message?: string; jobs?: string[]; violations?: unknown[] };
    // Prefere a mensagem HUMANA sobre o código de máquina ("schema", "policy"):
    // o server manda ambos, e só o code sozinho ("schema") não diz nada ao usuário.
    let msg = (o.message && o.message.trim()) || o.error || "";
    // Anexa a lista detalhada que o publish 422 traz (qual job, qual campo falta).
    const items = Array.isArray(o.jobs) && o.jobs.length
      ? o.jobs
      : Array.isArray(o.violations) && o.violations.length
        ? o.violations.map((v) => (typeof v === "string" ? v : JSON.stringify(v)))
        : [];
    if (items.length) msg += (msg ? "\n\n" : "") + items.map((j) => "• " + j).join("\n");
    if (msg) return msg;
  }
  return a.message ?? String(e);
}

export interface ClassifiedError {
  title: string;
  detail: string;
  hint?: string;
  status?: number;
}

export function classifyError(e: unknown): ClassifiedError {
  const a = (e ?? {}) as ApiLike;
  const status = a.status;
  const detail = extractMessage(e);
  const lower = detail.toLowerCase();

  // Erros ESTRUTURADOS do publish: o server manda {error, message, jobs|violations}.
  // O `detail` (via extractMessage) já carrega a mensagem + a lista de jobs a corrigir.
  const body = a.body && typeof a.body === "object" ? (a.body as { error?: string }) : null;
  if (body?.error === "schema") {
    return {
      title: "Job(s) with incomplete configuration",
      detail,
      status,
      hint:
        "One or more jobs are missing the required fields of their type (e.g. COMMAND needs 'command', HTTP needs 'url', DATABASE needs driver/dsn/sql).\n" +
        "Open each job listed below, fill in the 'Job action config' section and publish again.",
    };
  }
  if (body?.error === "policy") {
    return {
      title: "Publish blocked by policy",
      detail,
      status,
      hint: "The working set violates rules in policies.yaml (enforcement=error). Fix the listed jobs or the policy before publishing.",
    };
  }

  if (lower.includes("github token") || lower.includes("github_token")) {
    return {
      title: "GitHub token not configured",
      detail,
      status,
      hint:
        "The server needs a Personal Access Token to open a PR or push.\n" +
        "1. Generate one at https://github.com/settings/tokens/new (scope: repo).\n" +
        "2. Set GITHUB_TOKEN in the environment where regente-server runs.\n" +
        "3. Restart the server.",
    };
  }
  if (lower.startsWith("actionconfig:") || lower.includes("required") && lower.includes("actionconfig")) {
    return {
      title: "Invalid job configuration",
      detail,
      status,
      hint: "Fill in the required fields of the 'Job action config' section before saving.",
    };
  }
  if (lower.includes("label required")) {
    return { title: "Label is required", detail, status, hint: "Set a readable name for the job." };
  }
  if (lower.includes("id required")) {
    return { title: "ID is required", detail, status, hint: "Set a unique identifier." };
  }
  if (lower.includes("team") && lower.includes("required")) {
    return { title: "Folder is required", detail, status, hint: "Select an active folder before saving." };
  }
  if (lower.includes("commit") || lower.includes("push")) {
    return {
      title: "Failed to publish to Git",
      detail,
      status,
      hint: "Check connectivity, the repository credentials and whether there are conflicts on the remote.",
    };
  }
  if (status === 401) return { title: "Not authenticated", detail, status, hint: "Sign in again." };
  if (status === 403) return { title: "No permission", detail, status, hint: "You do not have access to this operation." };
  if (status === 404) return { title: "Not found", detail, status };
  if (status === 502 || status === 503 || status === 504) {
    return { title: `Server error (${status})`, detail, status, hint: "The server failed to process the request. See the technical message below." };
  }
  if (status && status >= 500) {
    return { title: `Internal error (${status})`, detail, status };
  }
  return { title: status ? `Error ${status}` : "Error", detail, status };
}
