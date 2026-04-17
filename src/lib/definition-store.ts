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
    retries: d.retries ?? 2,
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

/* ── Definition → Partial Node update ── */

/**
 * Apply a schedule from a JobDefinition back to a node's data.
 * This is used when the schedule editor in the properties panel updates.
 */
export function definitionScheduleToNodeData(def: JobDefinition): Partial<JobNodeData> {
  return {
    schedule: def.schedule.cronExpression,
  };
}

/* ── Bulk export ── */

/**
 * Get all enabled definitions (with valid cron) from a set of nodes.
 * Used by the scheduler to load definitions.
 */
export function getSchedulableDefinitions(nodes: Node<JobNodeData>[]): JobDefinition[] {
  return nodesToDefinitions(nodes).filter(
    (d) => d.schedule.enabled && d.schedule.cronExpression,
  );
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

/** Carrega do storage configurado. Idempotente em chamadas paralelas. */
export async function loadDefinitions(): Promise<JobDefinition[]> {
  if (_loaded) return _cache;
  if (_loading) return _loading;
  _loading = (async () => {
    try {
      _cache = await container.storage.list();
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

/** Upsert de uma definition — grava no storage e atualiza cache. */
export async function saveDefinition(def: JobDefinition): Promise<void> {
  await container.storage.save(def);
  const idx = _cache.findIndex((d) => d.id === def.id);
  if (idx >= 0) _cache = [..._cache.slice(0, idx), def, ..._cache.slice(idx + 1)];
  else _cache = [..._cache, def];
  emitChange();
}

/** Remove por id. */
export async function deleteDefinition(id: string): Promise<void> {
  await container.storage.remove(id);
  _cache = _cache.filter((d) => d.id !== id);
  emitChange();
}

/** Reset (útil em testes / limpar tudo no dev). */
export async function clearAllDefinitions(): Promise<void> {
  const ids = _cache.map((d) => d.id);
  for (const id of ids) await container.storage.remove(id);
  _cache = [];
  emitChange();
}

