import type { Node } from "@xyflow/react";
import { dependencyRelations } from "./dependency-layout";

/** Projection cache: selection never changes the geometry or unrelated nodes. */
export function createCanvasSelection() {
  let previous = new Map<string, { source: Node; projected: Node }>();
  type Link = { source: string; target: string };
  let lastEdges: Link[] | undefined, lastIds = "";
  let relations = { ancestors: new Set<string>(), descendants: new Set<string>() };
  return (nodes: Node[], edges: Link[], ids: Set<string>, prefix: "m" | "d"): Node[] => {
    const key = prefix + JSON.stringify([...ids].sort());
    if (key !== lastIds || edges !== lastEdges) {
      relations = dependencyRelations(new Set([...ids].map(id => `${prefix}-${id}`)), edges);
      lastIds = key; lastEdges = edges;
    }
    const next = new Map<string, { source: Node; projected: Node }>();
    const projected = nodes.map(source => {
      const selected = source.type !== "laneLabel" && ids.has(source.id.slice(2));
      const dependencyRelation = relations.ancestors.has(source.id) ? "ancestor" : relations.descendants.has(source.id) ? "descendant" : undefined;
      const old = previous.get(source.id);
      let node: Node;
      if (old?.source === source && old.projected.selected === selected && old.projected.data.dependencyRelation === dependencyRelation) node = old.projected;
      else if (source.selected === selected && source.data.dependencyRelation === dependencyRelation) node = source;
      else node = { ...source, selected, data: source.data.dependencyRelation === dependencyRelation ? source.data : { ...source.data, dependencyRelation } };
      next.set(source.id, { source, projected: node });
      return node;
    });
    previous = next;
    return projected;
  };
}
