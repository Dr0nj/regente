/**
 * Definition Store — persistence for JobDefinitions.
 *
 * Bridges the React Flow canvas (Node<JobNodeData>) with the orchestrator model (JobDefinition).
 * Design mode creates/edits definitions on the canvas.
 * The Scheduler reads definitions to create daily instances.
 *
 * Conversions:
 *   Node<JobNodeData> ←→ JobDefinition
 */

import type { Node } from "@xyflow/react";
import type { JobNodeData } from "@/lib/job-config";
import type { JobDefinition, JobSchedule } from "@/lib/orchestrator-model";
import { describeCron } from "@/lib/cron";

/* ── Node → Definition ── */

/**
 * Convert a canvas node to a JobDefinition for the scheduler.
 */
export function nodeToDefinition(node: Node<JobNodeData>): JobDefinition {
  const d = node.data as JobNodeData;
  const cronExpr = d.schedule?.trim() || "";

  const schedule: JobSchedule = {
    cronExpression: cronExpr,
    description: cronExpr ? describeCron(cronExpr) : "No schedule",
    enabled: !!cronExpr,
  };

  return {
    id: node.id,
    label: d.label,
    jobType: d.jobType,
    team: d.team,
    schedule,
    retries: d.retries ?? 0,
    timeout: d.timeout ?? 300,
    actionConfig: d.httpConfig as unknown as Record<string, unknown>,
    variables: d.variables,
    dryRun: d.dryRun,
  };
}

/**
 * Convert all canvas job nodes to JobDefinitions.
 */
export function nodesToDefinitions(nodes: Node<JobNodeData>[]): JobDefinition[] {
  return nodes
    .filter((n) => n.type === "job" || !n.type)
    .map(nodeToDefinition);
}

/* ──────────────────────────────────────────────────────────────
   Fase 7 — Runtime store (async, com subscribers)
   ──────────────────────────────────────────────────────────────
   Envelope fino em cima de `container.storage` (Port). Mantém
   cache em memória para a UI subscrever e emite eventos em cada
   mutação. A persistência real (Git ou localStorage) é escolhida
   pelo container.
   ────────────────────────────────────────────────────────────── */

import { container } from "@/lib/container";
import { normalizeDefsConditions } from "@/lib/conditions-model";

type DefinitionsListener = (defs: JobDefinition[]) => void;

let _cache: JobDefinition[] = [];
let _loaded = false;
let _loading: Promise<JobDefinition[]> | null = null;
const _listeners = new Set<DefinitionsListener>();

function emitChange(): void {
  for (const fn of _listeners) {
    try { fn(_cache); } catch { /* ignore */ }
  }
}

/** Carrega do storage configurado. Idempotente em chamadas paralelas.
 *  Normalização do modelo único de condições (conditions-model): em server
 *  mode as defs já vêm normalizadas (no-op idempotente); em local mode isto
 *  expande `upstream` legado do localStorage e deriva a visão de topologia. */
export async function loadDefinitions(): Promise<JobDefinition[]> {
  if (_loaded) return _cache;
  if (_loading) return _loading;
  _loading = (async () => {
    try {
      _cache = normalizeDefsConditions(await container.storage.list());
      _loaded = true;
      emitChange();
      return _cache;
    } finally {
      _loading = null;
    }
  })();
  return _loading;
}

/** Cache atual (sincrono). Pode estar vazio antes do primeiro load. */
export function getDefinitions(): JobDefinition[] {
  return _cache;
}

/** Inscrição em mudanças (upsert/remove). Retorna unsubscribe. */
export function onDefinitionsChange(fn: DefinitionsListener): () => void {
  _listeners.add(fn);
  return () => { _listeners.delete(fn); };
}

/** Upsert de uma definition — grava no storage e atualiza cache. A visão
 *  `upstream` é re-derivada no ESCOPO inteiro (uma condição nova pode ligar
 *  este job a produtores/consumidores existentes). */
export async function saveDefinition(def: JobDefinition): Promise<void> {
  await container.storage.save(def);
  const idx = _cache.findIndex((d) => d.id === def.id);
  if (idx >= 0) _cache = [..._cache.slice(0, idx), def, ..._cache.slice(idx + 1)];
  else _cache = [..._cache, def];
  _cache = normalizeDefsConditions(_cache);
  emitChange();
}

/** Remove por id. */
export async function deleteDefinition(id: string): Promise<void> {
  await container.storage.remove(id);
  _cache = normalizeDefsConditions(_cache.filter((d) => d.id !== id));
  emitChange();
}

/** Força refetch do storage (útil quando mudanças externas chegam via WS). */
export async function reloadDefinitions(): Promise<JobDefinition[]> {
  _cache = normalizeDefsConditions(await container.storage.list());
  _loaded = true;
  emitChange();
  return _cache;
}

