/**
 * GitAdapter — stub Fase 3.
 *
 * Persistirá cada JobDefinition como YAML em `definitions/<id>.yaml`
 * no repositório GitHub configurado, via GitHub Contents API.
 *
 * Por enquanto apenas lança `Not implemented` para forçar uso do
 * LocalStorageAdapter até a Fase 3.
 */

import type { StoragePort } from "@/lib/ports/StoragePort";
import type { JobDefinition } from "@/lib/orchestrator-model";

export class GitAdapter implements StoragePort {
  async list(): Promise<JobDefinition[]> {
    throw new Error("GitAdapter: not implemented (Fase 3)");
  }
  async get(_id: string): Promise<JobDefinition | null> {
    throw new Error("GitAdapter: not implemented (Fase 3)");
  }
  async save(_def: JobDefinition): Promise<void> {
    throw new Error("GitAdapter: not implemented (Fase 3)");
  }
  async remove(_id: string): Promise<void> {
    throw new Error("GitAdapter: not implemented (Fase 3)");
  }
  async saveBatch(_defs: JobDefinition[]): Promise<void> {
    throw new Error("GitAdapter: not implemented (Fase 3)");
  }
}
