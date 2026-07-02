/**
 * useCanvasCamera — dono ÚNICO da câmera do canvas (trava de pan + âncora de
 * entrada + centralizações). Extraído do V2Preview.tsx (2026-07-01).
 *
 * Regra de ouro: TODO movimento programático passa pelo clamp do MESMO limite
 * do translateExtent. O ReactFlow só clampa pan/zoom do USUÁRIO — setViewport/
 * setCenter passam direto — e uma câmera fora do extent "pula" pra posição
 * travada no primeiro pan (era o bug da centralização).
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useReactFlow, useStore, useStoreApi } from "@xyflow/react";
import {
  NODE_W,
  NODE_H,
  TOP_ANCHOR,
  PAN_SLACK_TOP,
  DESIGN_PAN_MARGIN_X,
  DESIGN_PAN_MARGIN_BOTTOM,
  type Canvas,
  type Mode,
} from "../canvas-layout";

export function useCanvasCamera(canvas: Canvas, mode: Mode, viewContextKey: string) {
  const { getViewport, setViewport } = useReactFlow();
  const storeApi = useStoreApi();

  // Trava de pan. Monitoring: topo do conteúdo (folders) alinhado com o ACTIVE JOBS;
  // livre pros lados e pra CIMA (revelar mais jobs abaixo), nunca abaixo do topo.
  // Design: BOUNDED na caixa dos jobs da folder + margem — só puxa pros lados quando
  // os jobs passam da tela, e com LIMITE pra não "se perder" no vazio.
  const panExtent = useMemo<[[number, number], [number, number]] | undefined>(() => {
    if (canvas.nodes.length === 0) return undefined;
    const top = Math.min(...canvas.nodes.map((n) => n.position.y));
    if (mode === "monitoring") {
      return [[-100000, top - PAN_SLACK_TOP], [100000, 100000]];
    }
    // Design — caixa [minX,minY]..[maxX,maxY] dos jobs + margem.
    const xs = canvas.nodes.map((n) => n.position.x);
    const ys = canvas.nodes.map((n) => n.position.y);
    const minX = Math.min(...xs);
    const maxX = Math.max(...xs) + NODE_W;
    const minY = Math.min(...ys);
    const maxY = Math.max(...ys) + NODE_H;
    return [
      [minX - DESIGN_PAN_MARGIN_X, minY - PAN_SLACK_TOP],
      [maxX + DESIGN_PAN_MARGIN_X, maxY + DESIGN_PAN_MARGIN_BOTTOM],
    ];
  }, [mode, canvas.nodes]);

  // clampTy — aplica ao movimento PROGRAMÁTICO (organizar/centralizar/minimap) o
  // MESMO limite superior do translateExtent.
  const clampTy = useCallback((ty: number, zoom: number): number => {
    if (canvas.nodes.length === 0) return ty;
    const top = Math.min(...canvas.nodes.map((n) => n.position.y));
    return Math.min(ty, (PAN_SLACK_TOP - top) * zoom);
  }, [canvas.nodes]);

  // focusOnPoint — centraliza um ponto do mundo na tela SEM sair do extent.
  // Substitui o setCenter cru: para jobs perto do topo, "centraliza até onde a
  // trava deixa" (job fica visível no alto) e a câmera permanece numa posição
  // legal — mexer depois não salta.
  const focusOnPoint = useCallback((px: number, py: number, zoom: number, duration = 350) => {
    const { width, height } = storeApi.getState();
    const tx = width / 2 - px * zoom;
    const ty = clampTy(height / 2 - py * zoom, zoom);
    setViewport({ x: tx, y: ty, zoom }, { duration });
  }, [storeApi, clampTy, setViewport]);

  // organizeView — re-enquadra ancorando o TOPO do conteúdo em TOP_ANCHOR (em vez
  // de centralizar verticalmente, que jogaria o conteúdo mais pra baixo). Fonte
  // ÚNICA usada na ENTRADA, no botão "Organizar" e no pós-save de job novo — assim
  // todos caem exatamente no mesmo limite.
  const organizeViewImpl = useCallback((duration: number) => {
    if (canvas.nodes.length === 0) return;
    // Fit calculado NA MÃO (sem fitView): o fitView do RF v12 é assíncrono e o
    // promise nem resolve quando chamado logo após o mount (nós ainda medindo) —
    // era a corrida que deixava a entrada ora centralizada (fora do extent → pulo
    // no 1º pan), ora em 0,0. Bounds vêm das lanes (contêm todos os jobs), o pane
    // vem do store — tudo síncrono e determinístico.
    const { width, height } = storeApi.getState();
    if (!width || !height) return;
    const L = canvas.lanes;
    const b = L.length > 0
      ? {
          minX: Math.min(...L.map((l) => l.x)),
          maxX: Math.max(...L.map((l) => l.x + l.width)),
          minY: Math.min(...L.map((l) => l.y)),
          maxY: Math.max(...L.map((l) => l.y + l.height)),
        }
      : {
          minX: Math.min(...canvas.nodes.map((n) => n.position.x)),
          maxX: Math.max(...canvas.nodes.map((n) => n.position.x + NODE_W)),
          minY: Math.min(...canvas.nodes.map((n) => n.position.y)),
          maxY: Math.max(...canvas.nodes.map((n) => n.position.y + NODE_H)),
        };
    const bw = Math.max(1, b.maxX - b.minX);
    const bh = Math.max(1, b.maxY - b.minY);
    const PADDING = 0.12; // fração do pane, como o fitView fazia
    const zoomFit = Math.min((width * (1 - PADDING * 2)) / bw, (height * (1 - PADDING * 2)) / bh);
    const zoom = Math.max(0.5, Math.min(1, zoomFit)); // não afasta além do minZoom nem amplia >1
    const tx = width / 2 - ((b.minX + b.maxX) / 2) * zoom;
    const ty = clampTy(TOP_ANCHOR - b.minY * zoom, zoom);
    setViewport({ x: tx, y: ty, zoom }, { duration });
  }, [canvas.nodes, canvas.lanes, storeApi, setViewport, clampTy]);

  // Identidade ESTÁVEL + implementação sempre fresca: quem captura organizeView
  // em closures antigas (setTimeout do pós-save, gate de entrada) chama a versão
  // com os canvas.nodes atuais, sem re-disparar effects a cada re-layout.
  const organizeViewRef = useRef(organizeViewImpl);
  useEffect(() => { organizeViewRef.current = organizeViewImpl; }, [organizeViewImpl]);
  const organizeView = useCallback((duration: number) => organizeViewRef.current(duration), []);

  // Auto-organiza (ancora o topo) SÓ ao entrar num contexto ou quando os jobs
  // aparecem pela 1ª vez. Churn de dado (Force/Run Daily/WS) NÃO reancora, então
  // a câmera não "pula" nem volta ao centro sozinha.
  const hasNodes = canvas.nodes.length > 0;
  // paneReady: o <ReactFlow> pode montar DEPOIS dos nodes existirem (ex.: login —
  // os dados chegam via _connected enquanto o LoginForm ainda cobre a tela) e o
  // onInit dispara ANTES do pane ser medido (width/height=0 → organize no-op).
  // Assinar as dimensões do store reativa o gate assim que o pane ganha tamanho.
  const paneReady = useStore((s) => s.width > 0 && s.height > 0);
  useEffect(() => {
    if (!hasNodes || !paneReady) return;
    const t = setTimeout(() => organizeViewRef.current(220), 140);
    return () => clearTimeout(t);
  }, [viewContextKey, hasNodes, paneReady]);

  /* ── Centralizar um nó (clique na sidebar / aba Folders) ── */
  const focusNode = useCallback((nodeId: string) => {
    const node = canvas.nodes.find((n) => n.id === nodeId);
    if (!node) return;
    const px = node.position.x + NODE_W / 2;
    const py = node.position.y + NODE_H / 2;
    focusOnPoint(px, py, 1.1);
  }, [canvas.nodes, focusOnPoint]);

  // Foco pendente: centraliza num job assim que o nó aparece no canvas (ex.: após
  // Force Order — o instance é criado async, então esperamos ele materializar).
  // Mantém o zoom atual (não dá aquele salto), e some depois de focar 1×.
  const [pendingFocusId, setPendingFocusId] = useState<string | null>(null);
  useEffect(() => {
    if (!pendingFocusId) return;
    const node = canvas.nodes.find((n) => n.id === `m-${pendingFocusId}`);
    if (!node) return; // ainda não materializou — espera o próximo re-layout
    focusOnPoint(node.position.x + NODE_W / 2, node.position.y + NODE_H / 2, getViewport().zoom);
    setPendingFocusId(null);
  }, [pendingFocusId, canvas.nodes, focusOnPoint, getViewport]);

  return { panExtent, organizeView, focusOnPoint, focusNode, setPendingFocusId };
}
