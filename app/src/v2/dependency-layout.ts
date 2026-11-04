import Dagre from "@dagrejs/dagre";

/** Longest dependency depth, including disconnected roots. Cycles share a rank. */
export function dependencyLayout(ids: string[], edges: Array<{ source: string; target: string }>, width: number, height: number) {
  const graph = new Dagre.graphlib.Graph();
  const sorted = [...ids].sort();
  for (const id of sorted) graph.setNode(id);
  for (const e of edges) if (graph.hasNode(e.source) && graph.hasNode(e.target)) graph.setEdge(e.source, e.target);
  const components = Dagre.graphlib.alg.tarjan(graph);
  const owner = new Map<string, number>();
  components.forEach((members, i) => members.forEach(id => owner.set(id, i)));
  const children = components.map(() => new Set<number>());
  const indegree = components.map(() => 0);
  const depth = components.map(() => 0);
  for (const e of graph.edges()) {
    const a = owner.get(e.v)!, b = owner.get(e.w)!;
    if (a !== b && !children[a].has(b)) { children[a].add(b); indegree[b]++; }
  }
  const queue = indegree.flatMap((n, i) => n === 0 ? [i] : []);
  for (let i = 0; i < queue.length; i++) {
    const a = queue[i];
    for (const b of children[a]) {
      depth[b] = Math.max(depth[b], depth[a] + 1);
      if (--indegree[b] === 0) queue.push(b);
    }
  }
  const rows = new Map<number, string[]>();
  for (const id of sorted) {
    const rank = depth[owner.get(id)!];
    if (!rows.has(rank)) rows.set(rank, []);
    rows.get(rank)!.push(id);
  }
  const positions = new Map<string, { x: number; y: number }>();
  // Compact each row around its parents, keeping a full card of separation.
  for (const [rank, members] of [...rows].sort(([a], [b]) => a - b)) {
    const rowOrder = new Map(members.map((id, index) => [id, index]));
    const desired = (id: string) => {
      const parents = (graph.predecessors(id) ?? []).flatMap(p => positions.has(p) ? [positions.get(p)!.x] : []);
      return parents.length ? parents.reduce((a, b) => a + b, 0) / parents.length : rowOrder.get(id)! * (width + 24);
    };
    members.sort((a, b) => desired(a) - desired(b) || a.localeCompare(b));
    const target = members.reduce((sum, id) => sum + desired(id), 0) / members.length;
    const start = Math.max(0, target - (members.length - 1) * (width + 24) / 2);
    members.forEach((id, i) => positions.set(id, { x: start + i * (width + 24), y: rank * (height + 44) }));
  }
  return positions;
}

/** Traverse each direction separately: siblings are not dependencies of a selection. */
export function dependencyRelations(selected: Set<string>, edges: Array<{ source: string; target: string }>) {
  const walk = (upstream: boolean) => {
    const adjacency = new Map<string, string[]>();
    for (const e of edges) {
      const from = upstream ? e.target : e.source, to = upstream ? e.source : e.target;
      adjacency.set(from, [...(adjacency.get(from) ?? []), to]);
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
