/**
 * DAG Validation — Phase 5
 *
 * Detects structural problems in the workflow graph:
 * - Cycles (invalid DAG)
 * - Orphan nodes (no incoming or outgoing edges)
 * - Invalid edges (source/target doesn't exist)
 * - Duplicate edges
 * - Self-loops
 */

import type { Node, Edge } from "@xyflow/react";

export type ValidationSeverity = "error" | "warning" | "info";

export interface ValidationIssue {
  id: string;
  severity: ValidationSeverity;
  type: "cycle" | "orphan" | "invalid-edge" | "duplicate-edge" | "self-loop";
  message: string;
  nodeIds?: string[];
  edgeIds?: string[];
}

export interface ValidationResult {
  valid: boolean;
  issues: ValidationIssue[];
  stats: {
    nodeCount: number;
    edgeCount: number;
    orphanCount: number;
    cycleCount: number;
  };
}

let issueCounter = 0;
function nextId() {
  return `val-${++issueCounter}`;
}

/** Detect self-loops (edge where source === target) */
function findSelfLoops(edges: Edge[]): ValidationIssue[] {
  return edges
    .filter((e) => e.source === e.target)
    .map((e) => ({
      id: nextId(),
      severity: "error" as const,
      type: "self-loop" as const,
      message: `Self-loop: edge "${e.id}" connects node "${e.source}" to itself`,
      nodeIds: [e.source],
      edgeIds: [e.id],
    }));
}

/** Detect edges pointing to non-existent nodes */
function findInvalidEdges(nodeIds: Set<string>, edges: Edge[]): ValidationIssue[] {
  const issues: ValidationIssue[] = [];
  for (const e of edges) {
    if (!nodeIds.has(e.source)) {
      issues.push({
        id: nextId(),
        severity: "error",
        type: "invalid-edge",
        message: `Edge "${e.id}" has invalid source "${e.source}" (node not found)`,
        edgeIds: [e.id],
      });
    }
    if (!nodeIds.has(e.target)) {
      issues.push({
        id: nextId(),
        severity: "error",
        type: "invalid-edge",
        message: `Edge "${e.id}" has invalid target "${e.target}" (node not found)`,
        edgeIds: [e.id],
      });
    }
  }
  return issues;
}

/** Detect duplicate edges (same source → target pair) */
function findDuplicateEdges(edges: Edge[]): ValidationIssue[] {
  const seen = new Map<string, Edge>();
  const issues: ValidationIssue[] = [];
  for (const e of edges) {
    const key = `${e.source}→${e.target}`;
    const prev = seen.get(key);
    if (prev) {
      issues.push({
        id: nextId(),
        severity: "warning",
        type: "duplicate-edge",
        message: `Duplicate edge from "${e.source}" to "${e.target}"`,
        nodeIds: [e.source, e.target],
        edgeIds: [prev.id, e.id],
      });
    } else {
      seen.set(key, e);
    }
  }
  return issues;
}

/** Detect orphan nodes (no edges at all) */
function findOrphans(nodeIds: Set<string>, edges: Edge[]): ValidationIssue[] {
  const connected = new Set<string>();
  for (const e of edges) {
    connected.add(e.source);
    connected.add(e.target);
  }
  const issues: ValidationIssue[] = [];
  for (const id of nodeIds) {
    if (!connected.has(id)) {
      issues.push({
        id: nextId(),
        severity: "warning",
        type: "orphan",
        message: `Node "${id}" is disconnected (no edges)`,
        nodeIds: [id],
      });
    }
  }
  return issues;
}

/**
 * Detect cycles using DFS with coloring.
 * Returns one issue per distinct cycle found (reports the back-edge that closes it).
 */
function findCycles(nodeIds: Set<string>, edges: Edge[]): ValidationIssue[] {
  const adj = new Map<string, string[]>();
  for (const id of nodeIds) adj.set(id, []);
  for (const e of edges) {
    adj.get(e.source)?.push(e.target);
  }

  // 0=white, 1=gray, 2=black
  const color = new Map<string, number>();
  for (const id of nodeIds) color.set(id, 0);

  const issues: ValidationIssue[] = [];
  const path: string[] = [];

  function dfs(u: string) {
    color.set(u, 1); // gray
    path.push(u);
    for (const v of adj.get(u) ?? []) {
      if (color.get(v) === 1) {
        // Back edge → cycle found
        const cycleStart = path.indexOf(v);
        const cycleNodes = path.slice(cycleStart);
        issues.push({
          id: nextId(),
          severity: "error",
          type: "cycle",
          message: `Cycle detected: ${cycleNodes.join(" → ")} → ${v}`,
          nodeIds: cycleNodes,
        });
      } else if (color.get(v) === 0) {
        dfs(v);
      }
    }
    path.pop();
    color.set(u, 2); // black
  }

  for (const id of nodeIds) {
    if (color.get(id) === 0) dfs(id);
  }

  return issues;
}

/**
 * Run full DAG validation on a set of nodes and edges.
 * Automatically filters out non-job nodes (like teamGroup).
 */
export function validateDAG(nodes: Node[], edges: Edge[]): ValidationResult {
  issueCounter = 0;

  // Only validate job nodes
  const jobNodes = nodes.filter((n) => n.type === "job" || !n.type);
  const jobEdges = edges.filter(
    (e) => !e.source.startsWith("group-") && !e.target.startsWith("group-")
  );
  const nodeIds = new Set(jobNodes.map((n) => n.id));

  const issues: ValidationIssue[] = [
    ...findSelfLoops(jobEdges),
    ...findInvalidEdges(nodeIds, jobEdges),
    ...findDuplicateEdges(jobEdges),
    ...findOrphans(nodeIds, jobEdges),
    ...findCycles(nodeIds, jobEdges),
  ];

  const orphanCount = issues.filter((i) => i.type === "orphan").length;
  const cycleCount = issues.filter((i) => i.type === "cycle").length;

  return {
    valid: issues.filter((i) => i.severity === "error").length === 0,
    issues,
    stats: {
      nodeCount: jobNodes.length,
      edgeCount: jobEdges.length,
      orphanCount,
      cycleCount,
    },
  };
}
