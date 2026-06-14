import Dagre from "@dagrejs/dagre";
import type { Node, Edge } from "@xyflow/react";

const NODE_WIDTH = 240;
const NODE_HEIGHT = 110;

export function applyDagreLayout(
  nodes: Node[],
  edges: Edge[],
  direction: "TB" | "LR" = "TB"
): { nodes: Node[]; edges: Edge[] } {
  const g = new Dagre.graphlib.Graph().setDefaultEdgeLabel(() => ({}));

  g.setGraph({
    rankdir: direction,
    nodesep: 60,
    ranksep: 80,
    marginx: 40,
    marginy: 40,
  });

  for (const node of nodes) {
    g.setNode(node.id, { width: NODE_WIDTH, height: NODE_HEIGHT });
  }

  for (const edge of edges) {
    g.setEdge(edge.source, edge.target);
  }

  Dagre.layout(g);

  const layoutedNodes = nodes.map((node) => {
    const dagNode = g.node(node.id);
    return {
      ...node,
      position: {
        x: dagNode.x - NODE_WIDTH / 2,
        y: dagNode.y - NODE_HEIGHT / 2,
      },
    };
  });

  return { nodes: layoutedNodes, edges };
}

/**
 * applyDagreLayoutByFolder — Control-M style.
 *
 * Cada folder (node.data.team) vira uma COLUNA independente.
 * Dentro da coluna, dagre TB com os edges INTERNOS da folder.
 * Edges entre folders ainda são desenhadas pelo ReactFlow, mas não
 * afetam o layout (evita que o dagre colapse folders lado-a-lado
 * em um único grafo global).
 *
 * Retorna nodes com posições absolutas prontas para ReactFlow.
 */
export function applyDagreLayoutByFolder(
  jobNodes: Node[],
  edges: Edge[],
  options?: { folderGap?: number; nodesep?: number; ranksep?: number }
): { nodes: Node[]; edges: Edge[] } {
  const folderGap = options?.folderGap ?? 80;
  const nodesep = options?.nodesep ?? 40;
  const ranksep = options?.ranksep ?? 70;

  const FALLBACK = "—";
  const byFolder = new Map<string, Node[]>();
  for (const n of jobNodes) {
    const team = ((n.data as { team?: string })?.team ?? FALLBACK).trim() || FALLBACK;
    if (!byFolder.has(team)) byFolder.set(team, []);
    byFolder.get(team)!.push(n);
  }

  const folderNames = [...byFolder.keys()].sort((a, b) => {
    if (a === FALLBACK) return 1;
    if (b === FALLBACK) return -1;
    return a.localeCompare(b);
  });

  const nodeIdToFolder = new Map<string, string>();
  for (const [folder, members] of byFolder) {
    for (const m of members) nodeIdToFolder.set(m.id, folder);
  }

  const layoutedAll: Node[] = [];
  let cursorX = 0;

  for (const folder of folderNames) {
    const members = byFolder.get(folder)!;
    const innerEdges = edges.filter(
      (e) => nodeIdToFolder.get(e.source) === folder && nodeIdToFolder.get(e.target) === folder,
    );

    const g = new Dagre.graphlib.Graph().setDefaultEdgeLabel(() => ({}));
    g.setGraph({
      rankdir: "TB",
      nodesep,
      ranksep,
      marginx: 20,
      marginy: 20,
    });
    for (const n of members) g.setNode(n.id, { width: NODE_WIDTH, height: NODE_HEIGHT });
    for (const e of innerEdges) g.setEdge(e.source, e.target);
    Dagre.layout(g);

    // Bounding box do folder (dagre coordena por centro do node)
    let minX = Infinity;
    let maxX = -Infinity;
    let minY = Infinity;
    let maxY = -Infinity;
    for (const n of members) {
      const dn = g.node(n.id);
      const x0 = dn.x - NODE_WIDTH / 2;
      const y0 = dn.y - NODE_HEIGHT / 2;
      const x1 = x0 + NODE_WIDTH;
      const y1 = y0 + NODE_HEIGHT;
      if (x0 < minX) minX = x0;
      if (y0 < minY) minY = y0;
      if (x1 > maxX) maxX = x1;
      if (y1 > maxY) maxY = y1;
    }

    // Offset: começa em cursorX; alinhado ao top (y=0)
    const offsetX = cursorX - minX;
    const offsetY = -minY;

    for (const n of members) {
      const dn = g.node(n.id);
      layoutedAll.push({
        ...n,
        position: {
          x: dn.x - NODE_WIDTH / 2 + offsetX,
          y: dn.y - NODE_HEIGHT / 2 + offsetY,
        },
      });
    }

    const folderWidth = maxX - minX;
    cursorX += folderWidth + folderGap;
  }

  return { nodes: layoutedAll, edges };
}
