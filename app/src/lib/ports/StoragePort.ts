/**
 * StoragePort — interface de persistência de JobDefinitions.
 *
 * Contrato abstrato. Implementações: ServerApiAdapter (produto, regente-server/
 * GitOps) e LocalStorageAdapter (demo/browser).
 *
 * Regra: o domínio nunca importa adapter direto — sempre Port.
 */

import type { JobDefinition } from "@/lib/orchestrator-model";

export interface StoragePort {
  /** Lê todas as definitions. */
  list(): Promise<JobDefinition[]>;

  /** Lê uma definition por id. Retorna null se não existir. */
  get(id: string): Promise<JobDefinition | null>;

  /** Cria ou atualiza uma definition (upsert por id). */
  save(def: JobDefinition): Promise<void>;

  /** Remove uma definition. No-op se não existir. */
  remove(id: string): Promise<void>;

  /** Salva um lote atômico (útil para import/versionamento). */
  saveBatch(defs: JobDefinition[]): Promise<void>;
}
