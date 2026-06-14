/**
 * StoragePort — interface de persistência de JobDefinitions.
 *
 * Fase 2: contrato abstrato. Adapters MVP envolvem localStorage;
 * Fase 3: GitAdapter (YAML em repo GitHub);
 * Fase 8: DynamoDbAdapter (AWS).
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
