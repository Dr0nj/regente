/**
 * useCanvasCamera — dono ÚNICO da câmera do canvas (trava de pan + âncora de
 * entrada + centralizações). Extraído do V2Preview.tsx (2026-07-01).
 *
 * Regra de ouro: TODO movimento programático passa pelo clamp do MESMO limite
 * do translateExtent. O ReactFlow só clampa pan/zoom do USUÁRIO — setViewport/
 * setCenter passam direto — e uma câmera fora do extent "pula" pra posição
 * travada no primeiro pan (era o bug da centralização).
 */

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
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

type SavedViewport = { x: number; y: number; zoom: number };

// ── Sensibilidade do input do canvas (mouse) ─────────────────────────────────
// O ReactFlow v12 NÃO expõe prop de velocidade de zoom/pan (o fator do wheel é
// fixo no @xyflow/system e o arrasto é 1:1). Então desativamos o zoomOnScroll e o
// panOnDrag nativos e reimplementamos os dois AQUI, com ganho reduzido — pedido do
// usuário: "diminuir a velocidade de reação tanto pra puxar pros lados quanto pra
// dar zoom". Ajuste fino é só mexer nestas duas constantes.
const MIN_ZOOM = 0.5; // = default do ReactFlow (usado no clamp do fit)
const MAX_ZOOM = 2;   // = default do ReactFlow
// Ganho do zoom por unidade de deltaY. O nativo equivale a ~ln2 (0.693, pois faz
// 2^x); 0.35 ≈ metade da velocidade → passo mais suave por "clique" da roda.
const WHEEL_ZOOM_GAIN = 0.35;
// Fração do movimento do mouse aplicada ao pan no arrasto. 1 = 1:1 (nativo);
// 0.6 = a câmera anda 60% do que a mão anda → puxada mais devagar/calma.
const DRAG_PAN_SENS = 0.6;

const clamp = (v: number, min: number, max: number) => Math.min(Math.max(v, min), max);

// clampToExtent — replica a restrição do translateExtent do d3-zoom para um
// viewport {x,y,zoom} qualquer (o setViewport programático NÃO passa por ela).
// Mantém o pan/zoom custom dentro do MESMO limite que o ReactFlow usaria.
function clampToExtent(
  x: number,
  y: number,
  zoom: number,
  w: number,
  h: number,
  extent: [[number, number], [number, number]] | undefined,
): SavedViewport {
  if (!extent) return { x, y, zoom };
  const [[x0, y0], [x1, y1]] = extent;
  const minX = w - x1 * zoom;
  const maxX = -x0 * zoom;
  const minY = h - y1 * zoom;
  const maxY = -y0 * zoom;
  // Se o extent é MENOR que o pane (min > max), o d3 centraliza — replicamos.
  const cx = minX <= maxX ? clamp(x, minX, maxX) : (minX + maxX) / 2;
  const cy = minY <= maxY ? clamp(y, minY, maxY) : (minY + maxY) / 2;
  return { x: cx, y: cy, zoom };
}

// Câmeras guardadas por contexto de visão, POR ABA (sessionStorage), pra sobreviver
// ao F5 sem re-enquadrar. Antes o mapa era só em memória: no reload ele nascia vazio
// → o gate de entrada re-organizava (140ms + anim) e o board "pulava" da posição
// default (0,0) pra enquadrada. Com o snapshot restaurado, o setViewport do
// useLayoutEffect recoloca a câmera ANTES do paint dos nós → reload sem variação.
const VIEWPORTS_STORAGE_KEY = "regente:viewports";

function loadPersistedViewports(): Map<string, SavedViewport> {
  const m = new Map<string, SavedViewport>();
  if (typeof window === "undefined") return m;
  try {
    const raw = window.sessionStorage.getItem(VIEWPORTS_STORAGE_KEY);
    if (raw) for (const [k, v] of JSON.parse(raw) as [string, SavedViewport][]) m.set(k, v);
  } catch { /* ignore */ }
  return m;
}

export function useCanvasCamera(canvas: Canvas, mode: Mode, viewContextKey: string) {
  const { getViewport, setViewport } = useReactFlow();
  const storeApi = useStoreApi();

  // Espelhos sempre-frescos p/ os listeners nativos (anexados 1× — não podem
  // fechar sobre `mode`/`panExtent` de um render antigo).
  const modeRef = useRef(mode);
  modeRef.current = mode;

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
  const extentRef = useRef(panExtent);
  extentRef.current = panExtent;

  // attachStage — ref-callback pro <main> do palco. Anexa listeners NATIVOS de
  // wheel (zoom custom, non-passive p/ poder preventDefault — o onWheel do React é
  // passivo) e de mousedown (pan por arrasto com sensibilidade < 1). Ambos filtram
  // por `.react-flow` no target, então minimap/drawers (irmãos no <main>) e a
  // ScaleMonitor (quando o canvas nem monta) passam batido. Reanexa sozinho em
  // remount do canvas (React chama com null→elemento).
  const cleanupStageRef = useRef<(() => void) | null>(null);
  const attachStage = useCallback((el: HTMLElement | null) => {
    cleanupStageRef.current?.();
    cleanupStageRef.current = null;
    if (!el) return;

    let pan: { cx: number; cy: number; vx: number; vy: number; zoom: number } | null = null;
    const insideFlow = (t: EventTarget | null): t is Element =>
      t instanceof Element && !!t.closest(".react-flow");

    const onWheel = (e: WheelEvent) => {
      const t = e.target;
      if (!insideFlow(t)) return;
      if (t.closest(".nowheel")) return; // deixa o scroll interno rolar
      e.preventDefault();
      const { x, y, zoom } = getViewport();
      const rect = el.getBoundingClientRect();
      const sx = e.clientX - rect.left;
      const sy = e.clientY - rect.top;
      // Mesma normalização de deltaMode do @xyflow/system (px/linha/página).
      const perUnit = e.deltaMode === 1 ? 0.05 : e.deltaMode === 2 ? 1 : 0.002;
      const nz = clamp(zoom * Math.exp(-e.deltaY * perUnit * WHEEL_ZOOM_GAIN), MIN_ZOOM, MAX_ZOOM);
      if (nz === zoom) return;
      // Zoom ancorado no cursor: mantém o ponto do MUNDO sob o ponteiro fixo.
      const wx = (sx - x) / zoom;
      const wy = (sy - y) / zoom;
      const { width, height } = storeApi.getState();
      setViewport(clampToExtent(sx - wx * nz, sy - wy * nz, nz, width, height, extentRef.current));
    };

    const onMouseMove = (e: MouseEvent) => {
      if (!pan) return;
      const dx = (e.clientX - pan.cx) * DRAG_PAN_SENS;
      const dy = (e.clientY - pan.cy) * DRAG_PAN_SENS;
      const { width, height } = storeApi.getState();
      setViewport(clampToExtent(pan.vx + dx, pan.vy + dy, pan.zoom, width, height, extentRef.current));
    };
    const endPan = () => {
      if (!pan) return;
      pan = null;
      document.body.style.userSelect = "";
      window.removeEventListener("mousemove", onMouseMove);
      window.removeEventListener("mouseup", endPan);
    };
    const onMouseDown = (e: MouseEvent) => {
      if (e.button !== 0 && e.button !== 1) return; // só esquerdo/meio (direito = ctx menu)
      if (e.shiftKey) return; // Shift+drag = seleção retangular do ReactFlow
      const t = e.target;
      if (!insideFlow(t)) return;
      // No Design os nós/handles são arrastáveis — não pana se o arrasto começa neles.
      if (modeRef.current === "design" && (t.closest(".react-flow__node") || t.closest(".react-flow__handle"))) return;
      if (e.button === 1) e.preventDefault(); // corta o autoscroll do botão do meio
      const vp = getViewport();
      pan = { cx: e.clientX, cy: e.clientY, vx: vp.x, vy: vp.y, zoom: vp.zoom };
      document.body.style.userSelect = "none";
      window.addEventListener("mousemove", onMouseMove);
      window.addEventListener("mouseup", endPan);
    };

    el.addEventListener("wheel", onWheel, { passive: false, capture: true });
    el.addEventListener("mousedown", onMouseDown);
    cleanupStageRef.current = () => {
      el.removeEventListener("wheel", onWheel, true);
      el.removeEventListener("mousedown", onMouseDown);
      endPan();
    };
  }, [getViewport, setViewport, storeApi]);

  // clampTy — aplica ao movimento PROGRAMÁTICO (organizar/centralizar/minimap) o
  // MESMO limite superior do translateExtent.
  const clampTy = useCallback((ty: number, zoom: number): number => {
    if (canvas.nodes.length === 0) return ty;
    const top = Math.min(...canvas.nodes.map((n) => n.position.y));
    return Math.min(ty, (PAN_SLACK_TOP - top) * zoom);
  }, [canvas.nodes]);

  // visibleInsets — o <ReactFlow> ocupa a largura INTEIRA do palco; a sidebar
  // (esquerda) e o drawer de detalhes (direita) são overlays absolutos POR CIMA
  // do pane. Centralizar em `width/2` (meio do pane) joga o conteúdo pro meio da
  // JANELA, não da FAIXA VISÍVEL — no Design (sem drawer) isso puxava tudo pra
  // esquerda (metade da sidebar). Medimos os overlays (marcados com
  // data-canvas-inset) pra centralizar no espaço que o usuário realmente vê.
  const visibleInsets = useCallback((): { left: number; right: number } => {
    if (typeof document === "undefined") return { left: 0, right: 0 };
    const pane = document.querySelector(".react-flow")?.getBoundingClientRect();
    if (!pane) return { left: 0, right: 0 };
    const l = document.querySelector('[data-canvas-inset="left"]')?.getBoundingClientRect();
    const r = document.querySelector('[data-canvas-inset="right"]')?.getBoundingClientRect();
    return {
      left: l ? Math.max(0, l.right - pane.left) : 0,
      right: r ? Math.max(0, pane.right - r.left) : 0,
    };
  }, []);

  // focusOnPoint — centraliza um ponto do mundo na FAIXA VISÍVEL (descontando
  // sidebar/drawer) SEM sair do extent. Substitui o setCenter cru: para jobs
  // perto do topo, "centraliza até onde a trava deixa" (job fica visível no alto)
  // e a câmera permanece numa posição legal — mexer depois não salta.
  const focusOnPoint = useCallback((px: number, py: number, zoom: number, duration = 350) => {
    const { width, height } = storeApi.getState();
    const { left, right } = visibleInsets();
    const visW = Math.max(1, width - left - right);
    const tx = left + visW / 2 - px * zoom;
    const ty = clampTy(height / 2 - py * zoom, zoom);
    setViewport({ x: tx, y: ty, zoom }, { duration });
  }, [storeApi, clampTy, setViewport, visibleInsets]);

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
    // Faixa visível (desconta sidebar/drawer): o fit e a centralização usam ela,
    // não a largura total do pane — senão o conteúdo cai atrás dos overlays.
    const { left, right } = visibleInsets();
    const visW = Math.max(1, width - left - right);
    const PADDING = 0.12; // fração do pane, como o fitView fazia
    const zoomFit = Math.min((visW * (1 - PADDING * 2)) / bw, (height * (1 - PADDING * 2)) / bh);
    const zoom = Math.max(0.5, Math.min(1, zoomFit)); // não afasta além do minZoom nem amplia >1
    const tx = left + visW / 2 - ((b.minX + b.maxX) / 2) * zoom;
    const ty = clampTy(TOP_ANCHOR - b.minY * zoom, zoom);
    setViewport({ x: tx, y: ty, zoom }, { duration });
  }, [canvas.nodes, canvas.lanes, storeApi, setViewport, clampTy, visibleInsets]);

  // Identidade ESTÁVEL + implementação sempre fresca: quem captura organizeView
  // em closures antigas (setTimeout do pós-save, gate de entrada) chama a versão
  // com os canvas.nodes atuais, sem re-disparar effects a cada re-layout.
  const organizeViewRef = useRef(organizeViewImpl);
  useEffect(() => { organizeViewRef.current = organizeViewImpl; }, [organizeViewImpl]);
  const organizeView = useCallback((duration: number) => organizeViewRef.current(duration), []);

  const hasNodes = canvas.nodes.length > 0;
  // paneReady: o <ReactFlow> pode montar DEPOIS dos nodes existirem (ex.: login —
  // os dados chegam via _connected enquanto o LoginForm ainda cobre a tela) e o
  // onInit dispara ANTES do pane ser medido (width/height=0 → organize no-op).
  // Assinar as dimensões do store reativa o gate assim que o pane ganha tamanho.
  const paneReady = useStore((s) => s.width > 0 && s.height > 0);

  // Câmera GUARDADA por contexto de visão (modo+folders). Trocar de aba
  // (Monitoring↔Design) NÃO re-enquadra: cada view volta EXATAMENTE pra onde o
  // usuário a deixou (era o "desalinha e realinha" + perder o centro ao voltar).
  // Só enquadra a 1ª vez que a view aparece (sem posição salva); depois disso a
  // posição só muda se o usuário pedir (Organizar / focar num job / pan/zoom).
  // useLayoutEffect: restaura ANTES do paint → sem flash da posição antiga.
  // Lazy: hidrata do sessionStorage 1× (não a cada render). null! + guarda = idiom
  // de ref preguiçosa tipada como Map não-nulo nos usos abaixo.
  const savedViewports = useRef<Map<string, SavedViewport>>(null!);
  if (!savedViewports.current) savedViewports.current = loadPersistedViewports();

  // Salva a posição da view que está SAINDO — só quando o viewContextKey MUDA de
  // fato (troca de aba/folders), nunca no churn de load. (Salvar a cada flip de
  // hasNodes/paneReady gravaria o viewport inicial 0,0 e o "restore" nunca
  // organizaria.) getViewport aqui ainda devolve a posição da view antiga, pois o
  // efeito de restaurar (declarado ABAIXO) só roda depois deste.
  const prevKeyRef = useRef(viewContextKey);
  useLayoutEffect(() => {
    if (prevKeyRef.current !== viewContextKey) {
      savedViewports.current.set(prevKeyRef.current, getViewport());
      prevKeyRef.current = viewContextKey;
    }
  });

  // Persiste as câmeras (views visitadas + a ATIVA) ao sair/recarregar, pra que o
  // F5 restaure exatamente onde o usuário estava. pagehide cobre reload e fechar
  // aba; prevKeyRef.current é a view ativa neste instante.
  useEffect(() => {
    const flush = () => {
      try {
        savedViewports.current.set(prevKeyRef.current, getViewport());
        window.sessionStorage.setItem(
          VIEWPORTS_STORAGE_KEY,
          JSON.stringify([...savedViewports.current]),
        );
      } catch { /* ignore */ }
    };
    window.addEventListener("pagehide", flush);
    return () => window.removeEventListener("pagehide", flush);
  }, [getViewport]);

  // Ao ENTRAR numa view: restaura a posição salva (sem animar → sem "desalinha e
  // realinha") ou, se é a 1ª vez, enquadra. hasNodes é boolean (0↔N), então churn
  // de dado (Force/Run Daily/WS) NÃO re-dispara e a câmera fica onde o usuário
  // deixou; trocar de aba muda viewContextKey e cai aqui.
  useLayoutEffect(() => {
    if (!hasNodes || !paneReady) return;
    const saved = savedViewports.current.get(viewContextKey);
    if (saved) {
      setViewport(saved);
      return;
    }
    const t = setTimeout(() => organizeViewRef.current(220), 140); // 1ª vez: enquadra
    return () => clearTimeout(t);
  }, [viewContextKey, hasNodes, paneReady, setViewport]);

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

  return { panExtent, organizeView, focusOnPoint, focusNode, setPendingFocusId, attachStage };
}
