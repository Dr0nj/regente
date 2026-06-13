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
    const o = body as { error?: string; message?: string };
    if (o.error) return o.error;
    if (o.message) return o.message;
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

  if (lower.includes("github token") || lower.includes("github_token")) {
    return {
      title: "Token do GitHub não configurado",
      detail,
      status,
      hint:
        "O servidor precisa de um Personal Access Token para abrir PR ou push.\n" +
        "1. Gere em https://github.com/settings/tokens/new (escopo: repo).\n" +
        "2. Defina GITHUB_TOKEN no ambiente onde o regente-server roda.\n" +
        "3. Reinicie o server.",
    };
  }
  if (lower.startsWith("actionconfig:") || lower.includes("required") && lower.includes("actionconfig")) {
    return {
      title: "Configuração do job inválida",
      detail,
      status,
      hint: "Preencha os campos obrigatórios da seção 'Job action config' antes de salvar.",
    };
  }
  if (lower.includes("label required")) {
    return { title: "Label obrigatório", detail, status, hint: "Defina um nome legível para o job." };
  }
  if (lower.includes("id required")) {
    return { title: "ID obrigatório", detail, status, hint: "Defina um identificador único." };
  }
  if (lower.includes("team") && lower.includes("required")) {
    return { title: "Folder obrigatória", detail, status, hint: "Selecione uma folder ativa antes de salvar." };
  }
  if (lower.includes("commit") || lower.includes("push")) {
    return {
      title: "Falha ao publicar no Git",
      detail,
      status,
      hint: "Verifique conectividade, credenciais do repositório e se há conflitos no remote.",
    };
  }
  if (status === 401) return { title: "Não autenticado", detail, status, hint: "Faça login novamente." };
  if (status === 403) return { title: "Sem permissão", detail, status, hint: "Você não tem acesso a esta operação." };
  if (status === 404) return { title: "Não encontrado", detail, status };
  if (status === 502 || status === 503 || status === 504) {
    return { title: `Erro no servidor (${status})`, detail, status, hint: "O servidor falhou ao processar. Veja a mensagem técnica abaixo." };
  }
  if (status && status >= 500) {
    return { title: `Erro interno (${status})`, detail, status };
  }
  return { title: status ? `Erro ${status}` : "Erro", detail, status };
}
