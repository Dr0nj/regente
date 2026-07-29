// Versão do build do server — o que aparece no rodapé da UI.
import { api, isServerMode } from "./server-client";

/**
 * Versão do binário EM EXECUÇÃO — não a do arquivo em disco. Trocar o binário
 * não troca o processo, então "a release nova saiu" e "a release nova está no ar"
 * são coisas diferentes; é essa segunda que o rodapé responde.
 *
 * Devolve "" quando não há server (modo local) ou quando a chamada falha: a
 * versão é cosmética e NUNCA pode quebrar o carregamento da UI.
 */
export async function getServerVersion(): Promise<string> {
  if (!isServerMode()) return "";
  try {
    const r = await api<{ version?: string }>("/api/version");
    return r?.version ?? "";
  } catch {
    return "";
  }
}
