/**
 * jobtypes-api.ts — catálogo de jobTypes do server (GET /api/jobtypes, ADV-1).
 *
 * É a MESMA fonte declarativa que o validador usa (domain/typeschema.go):
 * campos aceitos, aliases de tipo e de campo. O Design usa o índice para
 * PODAR do actionConfig as chaves que não pertencem ao tipo do job — sem a
 * poda, trocar o Job Type deixava params órfãos do tipo antigo (scriptPath
 * num COMMAND) e o save 400ava com "campo desconhecido".
 *
 * Cache por sessão do app (o registry só muda com binário novo do server).
 */
import { api, isServerMode } from "./server-client";

interface CatalogField {
  name: string;
  aliases?: string[];
}

interface CatalogEntry {
  type: string;
  aliases?: string[];
  fields?: CatalogField[];
}

let cache: Promise<Map<string, Set<string>>> | null = null;

/**
 * Índice UPPER(tipo ou alias) → conjunto de params aceitos (nomes + aliases).
 * Tipo fora do índice = desconhecido pelo server (mundo aberto, params livres).
 * Local mode ou falha de rede → mapa vazio (ninguém poda nada).
 */
export function jobTypeFieldIndex(): Promise<Map<string, Set<string>>> {
  if (!isServerMode()) return Promise.resolve(new Map());
  if (!cache) {
    cache = api<{ jobTypes: CatalogEntry[] }>("/api/jobtypes")
      .then((r) => {
        const idx = new Map<string, Set<string>>();
        for (const t of r.jobTypes ?? []) {
          const fields = new Set<string>();
          for (const f of t.fields ?? []) {
            fields.add(f.name);
            for (const a of f.aliases ?? []) fields.add(a);
          }
          idx.set(t.type.toUpperCase(), fields);
          for (const a of t.aliases ?? []) idx.set(a.toUpperCase(), fields);
        }
        return idx;
      })
      .catch(() => {
        cache = null; // permite re-tentar num próximo drawer
        return new Map<string, Set<string>>();
      });
  }
  return cache;
}

/**
 * Poda um actionConfig contra o schema do jobType: mantém só os params que o
 * tipo aceita + as chaves meta `_*` (ex.: `_agentId`). Tipo desconhecido pelo
 * catálogo (ou índice indisponível) devolve o config intacto.
 */
export function pruneConfigForType(
  index: Map<string, Set<string>> | null,
  jobType: string,
  config: Record<string, unknown>,
): Record<string, unknown> {
  const allowed = index?.get(jobType.trim().toUpperCase());
  if (!allowed) return config;
  return Object.fromEntries(
    Object.entries(config).filter(([k]) => k.startsWith("_") || allowed.has(k)),
  );
}
