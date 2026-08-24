/**
 * NavMinimap — minimap de navegação próprio (protótipo opt-in, Settings → Geral).
 *
 * Desenha um retângulo por job a partir de canvas.nodes (posição garantida), sem
 * depender do MiniMap do ReactFlow (que filtra nós custom sem dimensão medida —
 * motivo de os jobs nunca aparecerem lá). Clique navega o canvas até o ponto.
 * Extraído do V2Preview.tsx (2026-07-01).
 */

import { memo, useMemo } from "react";
import { useStore, type Node } from "@xyflow/react";
import { NODE_W, NODE_H } from "./canvas-layout";

export default function NavMinimap({ nodes, width, height, onNavigate }: {
  nodes: Node[];
  width: number;
  height: number;
  // Navegação clamp-aware do pai (focusOnPoint) — setCenter cru deixaria a câmera
  // fora do translateExtent ao clicar perto do topo (pulo no próximo pan).
  onNavigate: (fx: number, fy: number, zoom: number) => void;
}) {
  // PERF: o corpo do minimap NÃO assina o transform. Assinava, e como a câmera
  // escreve o viewport a cada mousemove, o componente inteiro re-renderizava por
  // frame de pan — com 200 jobs eram 200 <rect> reconciliados a cada frame só pra
  // mover um retângulo. Quem assina agora é o <ViewportRect> lá embaixo (1 rect),
  // e a camada de jobs virou memo por [nodes,width,height].
  const tzoom = useStore((s) => s.transform[2]);

  // Mostra APENAS os jobs (ignora lanes/containers e outros nós). useMemo pra o
  // memo do <JobsLayer> segurar: array novo a cada render invalidaria tudo.
  const jobs = useMemo(() => nodes.filter((n) => n.type === "jobV2"), [nodes]);
  if (jobs.length === 0) {
    return <div style={{ width, height, display: "grid", placeItems: "center", fontSize: 11, color: "var(--v2-text-muted)" }}>no jobs</div>;
  }
  const MARGIN = 8; // respiro dentro da caixa do minimap
  const xs = jobs.map((n) => n.position.x);
  const ys = jobs.map((n) => n.position.y);
  // Bounds SÓ dos jobs (âncora no canto de cima/esquerda). NÃO entra o viewport aqui
  // de propósito: a escala do minimap fica ESTÁVEL, sempre mostrando toda a grade de
  // jobs "no zoom out", independente de quanto o monitoring está com zoom.
  const minX = Math.min(...xs);
  const minY = Math.min(...ys);
  const maxX = Math.max(...xs) + NODE_W;
  const maxY = Math.max(...ys) + NODE_H;
  const bw = Math.max(1, maxX - minX), bh = Math.max(1, maxY - minY);
  const scale = Math.min((width - 2 * MARGIN) / bw, (height - 2 * MARGIN) / bh);
  // Ancorado no topo-esquerdo (não centraliza) — reflete a organização real: 1ª
  // coluna/linha no canto, seguindo pra direita conforme a grade.
  const offX = MARGIN, offY = MARGIN;
  // Quadradinho na proporção real do card (mín. legível).
  const sw = Math.max(3, NODE_W * scale);
  const sh = Math.max(2, NODE_H * scale);

  const handleClick = (e: React.MouseEvent<SVGSVGElement>) => {
    const r = e.currentTarget.getBoundingClientRect();
    const fx = minX + (e.clientX - r.left - offX) / scale;
    const fy = minY + (e.clientY - r.top - offY) / scale;
    onNavigate(fx, fy, tzoom);
  };
  return (
    <svg width={width} height={height} onClick={handleClick} style={{ display: "block", cursor: "pointer" }}>
      <JobsLayer jobs={jobs} minX={minX} minY={minY} scale={scale} offX={offX} offY={offY} sw={sw} sh={sh} />
      {/* Retângulo do viewport — mostra onde a tela está sobre o conteúdo. */}
      <ViewportRect minX={minX} minY={minY} scale={scale} offX={offX} offY={offY} />
    </svg>
  );
}

// Camada dos jobs — memo: só re-renderiza quando a LISTA muda (publish, folder,
// daily), nunca por movimento de câmera.
const JobsLayer = memo(function JobsLayer({ jobs, minX, minY, scale, offX, offY, sw, sh }: {
  jobs: Node[]; minX: number; minY: number; scale: number; offX: number; offY: number; sw: number; sh: number;
}) {
  const rects = useMemo(() => jobs.map((n) => (
    <rect
      key={n.id}
      x={offX + (n.position.x - minX) * scale}
      y={offY + (n.position.y - minY) * scale}
      width={sw}
      height={sh}
      rx={1}
      fill={miniNodeColor(n)}
      stroke="#06080c"
      strokeWidth={0.5}
    />
  )), [jobs, minX, minY, scale, offX, offY, sw, sh]);
  return <>{rects}</>;
});

// Retângulo da área visível — ÚNICO assinante do transform. Re-renderiza a cada
// frame de pan, mas custa 1 <rect>.
function ViewportRect({ minX, minY, scale, offX, offY }: {
  minX: number; minY: number; scale: number; offX: number; offY: number;
}) {
  const tx = useStore((s) => s.transform[0]);
  const ty = useStore((s) => s.transform[1]);
  const tzoom = useStore((s) => s.transform[2]);
  const vpW = useStore((s) => s.width);
  const vpH = useStore((s) => s.height);
  const viewMinX = -tx / tzoom, viewMinY = -ty / tzoom;
  const viewMaxX = (vpW - tx) / tzoom, viewMaxY = (vpH - ty) / tzoom;
  return (
    <rect
      x={offX + (viewMinX - minX) * scale}
      y={offY + (viewMinY - minY) * scale}
      width={(viewMaxX - viewMinX) * scale}
      height={(viewMaxY - viewMinY) * scale}
      fill="rgba(255,255,255,0.05)"
      stroke="var(--v2-accent-brand)"
      strokeWidth={1}
      rx={2}
      pointerEvents="none"
    />
  );
}

// Cor do nó no minimap por status (hex p/ o fill SVG do minimap).
function miniNodeColor(n: Node): string {
  const s = String((n.data as { status?: string } | undefined)?.status ?? "");
  if (s === "FAILED" || s === "NOTOK") return "#ef4444";
  if (s === "RUNNING") return "#22d3ee";
  if (s === "SUCCESS" || s === "OK") return "#11C76F";
  if (s === "WAITING") return "#737373";
  return "#3a3a3a";
}
