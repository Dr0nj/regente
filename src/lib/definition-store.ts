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
