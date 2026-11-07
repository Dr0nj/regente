import Dagre from "@dagrejs/dagre";
import { recordCanvasMetric } from "./canvas-metrics";

export const GRAPH_SPACING = { horizontal: 100, vertical: 80, nodePadding: 20, arrow: 16 } as const;
export const JOB_WIDTH = 220;
export const JOB_HEIGHT = 80;
export const JOB_TAGGED_HEIGHT = 99;

/** Sugiyama-style layered layout, with stable input order and SCCs for cycles. */
export function dependencyLayout(ids: string[], edges: Array<{ source: string; target: string }>, width: number, height: number) {
  // Independent dependency components need no crossing minimization with each
  // other. Solving them separately avoids Dagre's superlinear work across a
  // whole daily, while preserving root alignment, spacing and determinism.
  const sorted = [...new Set(ids)].sort(), adjacency = new Map(sorted.map(id => [id, [] as string[]]));
  for (const e of edges) if (adjacency.has(e.source) && adjacency.has(e.target)) {
    adjacency.get(e.source)!.push(e.target); adjacency.get(e.target)!.push(e.source);
  }
  const owner = new Map<string, number>(), groups: string[][] = [];
  for (const id of sorted) {
    if (owner.has(id)) continue;
    const group = [id], index = groups.length;
    owner.set(id, index);
    for (let i = 0; i < group.length; i++) for (const next of adjacency.get(group[i])!) {
      if (!owner.has(next)) { owner.set(next, index); group.push(next); }
    }
    groups.push(group);
  }
  const links = groups.map(() => [] as Array<{ source: string; target: string }>);
  for (const e of edges) if (owner.has(e.source) && owner.get(e.source) === owner.get(e.target)) links[owner.get(e.source)!].push(e);
  const positions = new Map<string, { x: number; y: number }>();
  // Canonical IDs preserve ordering while allowing identical independent
  // topologies to share a layout within this build. No assumptions about job
  // names, status, count or a particular workflow template enter the key.
  const shapes = new Map<string, Array<{ x: number; y: number }>>();
  let cursor = 0;
  groups.forEach((group, i) => {
    const ordered = [...group].sort((a, b) => a.localeCompare(b));
    const canonical = ordered.map((_, j) => String(j).padStart(10, "0"));
    const ids = new Map(ordered.map((id, j) => [id, canonical[j]]));
    const edges = links[i].map(e => ({ source: ids.get(e.source)!, target: ids.get(e.target)! }))
      .sort((a, b) => a.source.localeCompare(b.source) || a.target.localeCompare(b.target));
    const shapeKey = JSON.stringify([ordered.length, edges]);
    let shape = shapes.get(shapeKey);
    if (!shape) {
      const local = ordered.length === 1 ? new Map([[canonical[0], { x: 0, y: 0 }]]) : layoutConnected(canonical, edges, width, height);
      shape = canonical.map(id => local.get(id)!);
      shapes.set(shapeKey, shape);
    }
    let right = 0;
    ordered.forEach((id, j) => {
      const p = shape[j]; positions.set(id, { x: cursor + p.x, y: p.y }); right = Math.max(right, p.x + width);
    });
    cursor += right + GRAPH_SPACING.horizontal;
  });
  return positions;
}

function layoutConnected(ids: string[], edges: Array<{ source: string; target: string }>, width: number, height: number) {
  const sorted = [...new Set(ids)].sort();
  const positions = new Map<string, { x: number; y: number }>();
  if (!sorted.length) return positions;
  const graph = new Dagre.graphlib.Graph();
  for (const id of sorted) graph.setNode(id);
  for (const e of [...edges].sort((a, b) => a.source.localeCompare(b.source) || a.target.localeCompare(b.target))) {
    if (graph.hasNode(e.source) && graph.hasNode(e.target)) graph.setEdge(e.source, e.target);
  }
  const components = Dagre.graphlib.alg.tarjan(graph).map(m => m.sort()).sort((a, b) => a[0].localeCompare(b[0]));
  const owner = new Map<string, string>();
  // Reverse edges + BT + longest-path ranks by distance FROM roots. Normal
  // longest-path ranks towards sinks and can lower short/disconnected roots.
  const layered = new Dagre.graphlib.Graph().setGraph({
    rankdir: "BT", ranker: "longest-path", nodesep: GRAPH_SPACING.horizontal,
    ranksep: GRAPH_SPACING.vertical, edgesep: GRAPH_SPACING.arrow,
  }).setDefaultEdgeLabel(() => ({}));
  components.forEach((members, i) => {
    const key = String(i);
    members.forEach(id => owner.set(id, key));
    layered.setNode(key, { width: members.length * width + (members.length - 1) * GRAPH_SPACING.horizontal, height });
  });
  for (const e of graph.edges()) {
    const a = owner.get(e.v)!, b = owner.get(e.w)!;
    if (a !== b) layered.setEdge(b, a);
  }
  const start = performance.now();
  Dagre.layout(layered);
  recordCanvasMetric("dagre", start, { nodes: ids.length, edges: edges.length });
  components.forEach((members, i) => {
    const node = layered.node(String(i));
    members.forEach((id, j) => positions.set(id, {
      x: node.x - node.width / 2 + j * (width + GRAPH_SPACING.horizontal),
      y: node.y - height / 2,
    }));
  });
  // Dagre coordinates are centers; canvas uses top-left, with no empty margin.
  const minX = Math.min(...[...positions.values()].map(p => p.x));
  const minY = Math.min(...[...positions.values()].map(p => p.y));
  for (const p of positions.values()) { p.x -= minX; p.y -= minY; }
  return positions;
}

/** Traverse each direction separately: siblings are not dependencies of a selection. */
export function dependencyRelations(selected: Set<string>, edges: Array<{ source: string; target: string }>) {
  if (!selected.size) return { ancestors: new Set<string>(), descendants: new Set<string>() };
  const walk = (upstream: boolean) => {
    const adjacency = new Map<string, string[]>();
    for (const e of edges) {
      const from = upstream ? e.target : e.source, to = upstream ? e.source : e.target;
      const list = adjacency.get(from);
      if (list) list.push(to); else adjacency.set(from, [to]);
    }
    const seen = new Set(selected), queue = [...selected];
    for (let i = 0; i < queue.length; i++) for (const next of adjacency.get(queue[i]) ?? []) {
      if (!seen.has(next)) { seen.add(next); queue.push(next); }
    }
    for (const id of selected) seen.delete(id);
    return seen;
  };
  return { ancestors: walk(true), descendants: walk(false) };
}
