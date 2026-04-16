/**
 * LocalStorageAdapter — StoragePort em cima de window.localStorage.
 *
 * MVP: chave única `regente:definitions` contendo JSON array.
 * Fase 3: substituído (ou espelhado) por GitAdapter.
 */

import type { StoragePort } from "@/lib/ports/StoragePort";
import type { JobDefinition } from "@/lib/orchestrator-model";

const KEY = "regente:definitions:v1";

function readAll(): JobDefinition[] {
  if (typeof window === "undefined") return [];
  try {
    const raw = window.localStorage.getItem(KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as JobDefinition[];
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

function writeAll(defs: JobDefinition[]): void {
  if (typeof window === "undefined") return;
  window.localStorage.setItem(KEY, JSON.stringify(defs));
}

export class LocalStorageAdapter implements StoragePort {
  async list(): Promise<JobDefinition[]> {
    return readAll();
  }

  async get(id: string): Promise<JobDefinition | null> {
    return readAll().find((d) => d.id === id) ?? null;
  }

  async save(def: JobDefinition): Promise<void> {
    const all = readAll();
    const idx = all.findIndex((d) => d.id === def.id);
    if (idx >= 0) all[idx] = def;
    else all.push(def);
    writeAll(all);
  }

  async remove(id: string): Promise<void> {
    writeAll(readAll().filter((d) => d.id !== id));
  }

  async saveBatch(defs: JobDefinition[]): Promise<void> {
    const byId = new Map<string, JobDefinition>();
    for (const d of readAll()) byId.set(d.id, d);
    for (const d of defs) byId.set(d.id, d);
    writeAll([...byId.values()]);
  }
}
