/**
 * useCanvasCamera — dono ÚNICO da câmera do canvas.
 *
 * Princípio (reforma 2026-07-06, pedido do usuário): A CÂMERA É DO USUÁRIO.
 * Ela SÓ se move por comando explícito — arrastar/roda, clicar num job na
 * sidebar, botão "Organizar", Force Order (foca o job forçado). Criar job,
 * abrir/fechar drawer, trocar de aba, mudar folders, churn de dado: NADA disso
 * re-enquadra. A única exceção é o 1º paint de um modo sem câmera salva
 * (enquadra ANTES de pintar — não é percebido como movimento).
 *
 * Limites de pan derivam do CONTEÚDO (nada de rolar telas de preto):
 *  - Monitoring: pros lados dá pra atravessar o conteúdo até o último job sumir
 *    "por pouco" (1 tela + PAN_CROSS_SLACK); em cima folga pequena; embaixo o
 *    limite é o último job (+PAN_SLACK_BOTTOM_SCREEN).
 *  - Design: visão presa à caixa dos jobs ± DESIGN_PAN_MARGIN_X; vazio = câmera
 *    travada na origem.
 * O extent é DINÂMICO (função de zoom + tamanho do pane) e TODO movimento —
 * user (pan/wheel custom) e programático (organizar/focar/minimap) — passa pelo
 * MESMO clamp, então câmera e trava nunca divergem (o ReactFlow só clamparia
 * pan/zoom nativo, que está desligado; o translateExtent que exportamos é só
 * cinto de segurança).
 */

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useReactFlow, useStore, useStoreApi } from "@xyflow/react";
import {
  NODE_W,
  NODE_H,
  TOP_ANCHOR,
  PAN_SLACK_TOP_SCREEN,
  PAN_SLACK_BOTTOM_SCREEN,
  PAN_CROSS_SLACK,
  DESIGN_PAN_MARGIN_X,
  contentBounds,
  type Canvas,
  type Mode,
} from "../canvas-layout";

type SavedViewport = { x: number; y: number; zoom: number };
type Extent = [[number, number], [number, number]];

// ── Sensibilidade do input do canvas (mouse) ─────────────────────────────────
// O ReactFlow v12 NÃO expõe prop de velocidade de zoom/pan (o fator do wheel é
// fixo no @xyflow/system e o arrasto é 1:1). Então desativamos o zoomOnScroll e o
// panOnDrag nativos e reimplementamos os dois AQUI, com ganho reduzido — pedido do
// usuário: "velocidade de puxar um pouco mais baixa que o normal". Ajuste fino é
// só mexer nestas duas constantes.
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
function clampToExtent(
  x: number,
  y: number,
  zoom: number,
  w: number,
  h: number,
  extent: Extent,
): SavedViewport {
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

// Câmeras guardadas POR MODO ("design"/"monitoring"), por aba (sessionStorage),
// pra sobreviver ao F5 e a remounts do <ReactFlow> (ViewPoint/CodeMode cobrem o
// palco) sem re-enquadrar. Chave por MODO — e não por conjunto de folders — é o
// que garante "mudar folders não move a câmera".
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
  // fechar sobre `mode`/bounds de um render antigo).
  const modeRef = useRef(mode);
  // eslint-disable-next-line react-hooks/refs -- espelho fresco p/ listener nativo (anexado 1×); a câmera só escreve via apply() e este ref é parte da trava de extent — refatorar já causou câmera pulando; ver roadmap §RH
  modeRef.current = mode;

  const bounds = useMemo(() => contentBounds(canvas), [canvas]);
  const boundsRef = useRef(bounds);
  // eslint-disable-next-line react-hooks/refs -- espelho fresco p/ listener nativo (anexado 1×); a câmera só escreve via apply() e este ref é parte da trava de extent — refatorar já causou câmera pulando; ver roadmap §RH
  boundsRef.current = bounds;

  const hasNodes = canvas.nodes.length > 0;
  const hasNodesRef = useRef(hasNodes);
  // eslint-disable-next-line react-hooks/refs -- espelho fresco p/ listener nativo (anexado 1×); a câmera só escreve via apply() e este ref é parte da trava de extent — refatorar já causou câmera pulando; ver roadmap §RH
  hasNodesRef.current = hasNodes;

  // extentFor — limite de pan DINÂMICO (px de mundo) pra um zoom/pane dados.
  // As folgas verticais são em px de TELA (÷ zoom): o "quanto dá pra puxar" é
  // constante aos olhos, em qualquer zoom.
  const extentFor = useCallback((zoom: number, w: number, h: number): Extent => {
    const b = boundsRef.current;
    if (!b) return [[0, 0], [w / zoom, h / zoom]]; // vazio: câmera travada na origem
    const y0 = b.minY - PAN_SLACK_TOP_SCREEN / zoom;
    let y1 = b.maxY + PAN_SLACK_BOTTOM_SCREEN / zoom;
    // Conteúdo mais BAIXO que a tela: estende o fundo até caber 1 viewport —
    // trava o topo na folga (sem isso o d3 centralizaria na vertical e o
    // Organizar/limite de cima perderiam o sentido).
    if (y1 - y0 < h / zoom) y1 = y0 + h / zoom;
    if (modeRef.current === "monitoring") {
      // Travessia completa + um "pouco": dá pra empurrar o conteúdo inteiro pra
      // fora da tela por PAN_CROSS_SLACK px de mundo, e nada além.
      const cross = w / zoom + PAN_CROSS_SLACK;
      return [[b.minX - cross, y0], [b.maxX + cross, y1]];
    }
    return [[b.minX - DESIGN_PAN_MARGIN_X, y0], [b.maxX + DESIGN_PAN_MARGIN_X, y1]];
  }, []);

  // clampCamera — TODO movimento (user ou programático) passa aqui.
  const clampCamera = useCallback((vp: SavedViewport): SavedViewport => {
    const { width, height } = storeApi.getState();
    if (!width || !height) return vp;
    return clampToExtent(vp.x, vp.y, vp.zoom, width, height, extentFor(vp.zoom, width, height));
  }, [storeApi, extentFor]);

  // Hidrata as câmeras salvas 1× (não a cada render). null! + guarda = ref
  // preguiçosa tipada como Map não-nulo nos usos abaixo.
  const savedViewports = useRef<Map<string, SavedViewport>>(null!);
  // lazy-init 1× no padrão que a regra aceita (ref.current == null); ver roadmap §RH
  if (savedViewports.current == null) savedViewports.current = loadPersistedViewports();
  const prevKeyRef = useRef(viewContextKey);

  // remember — grava a câmera da view ATIVA a cada movimento. É o que faz
  // remount do <ReactFlow> (abrir/fechar ViewPoint/CodeMode) e troca de aba
  // voltarem EXATAMENTE pra onde o usuário deixou. Canvas vazio não conta:
  // posição sem conteúdo não é posição do usuário (bloquearia o enquadre de
  // 1ª vez quando a view ganhar jobs).
  const remember = useCallback((vp: SavedViewport) => {
    if (!hasNodesRef.current) return;
    savedViewports.current.set(prevKeyRef.current, vp);
  }, []);

  // apply — clampa + aplica + lembra. Único caminho de escrita da câmera.
  const apply = useCallback((vp: SavedViewport, duration = 0): SavedViewport => {
    const c = clampCamera(vp);
    setViewport(c, duration > 0 ? { duration } : undefined);
    remember(c);
    return c;
  }, [clampCamera, setViewport, remember]);

  // translateExtent pro <ReactFlow> — SÓ cinto de segurança (pan/zoom nativos
  // estão desligados; todo movimento real passa no clampCamera). Superset do
  // extent dinâmico no pior caso (zoom mínimo), pra nunca brigar com ele.
  const panExtent = useMemo<Extent | undefined>(() => {
    if (!bounds) return undefined;
    const vw = (typeof window !== "undefined" ? window.innerWidth : 1920) / MIN_ZOOM;
    const vh = (typeof window !== "undefined" ? window.innerHeight : 1080) / MIN_ZOOM;
    const sideSlack = mode === "monitoring" ? vw + PAN_CROSS_SLACK : DESIGN_PAN_MARGIN_X;
    return [
      [bounds.minX - sideSlack, bounds.minY - PAN_SLACK_TOP_SCREEN / MIN_ZOOM],
      [bounds.maxX + sideSlack, bounds.maxY + Math.max(PAN_SLACK_BOTTOM_SCREEN / MIN_ZOOM, vh)],
    ];
  }, [mode, bounds]);

  // attachStage — ref-callback pro <main> do palco. Anexa listeners NATIVOS de
  // wheel (zoom custom, non-passive p/ poder preventDefault — o onWheel do React é
  // passivo) e de mousedown (pan por arrasto com sensibilidade < 1). Ambos filtram
  // por `.react-flow` no target, então minimap/drawers (irmãos no <main>) e a
  // ScaleMonitor (quando o canvas nem monta) passam batido. Reanexa sozinho em
  // remount do canvas (React chama com null→elemento).
  const panActiveRef = useRef(false);
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
      apply({ x: sx - wx * nz, y: sy - wy * nz, zoom: nz });
    };

    const onMouseMove = (e: MouseEvent) => {
      if (!pan) return;
      const dx = (e.clientX - pan.cx) * DRAG_PAN_SENS;
      const dy = (e.clientY - pan.cy) * DRAG_PAN_SENS;
      apply({ x: pan.vx + dx, y: pan.vy + dy, zoom: pan.zoom });
    };
    const endPan = () => {
      if (!pan) return;
      pan = null;
      panActiveRef.current = false;
      document.body.style.userSelect = "";
      document.body.classList.remove("canvas-panning");
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
      panActiveRef.current = true;
      document.body.style.userSelect = "none";
      // Cursor de arrasto via CLASSE (não style no body): a regra CSS com `*`
      // vence o pointer dos cards, então o cursor muda mesmo panando por cima
      // de job/folder. Usa `move` (nativo do SO — respeita o tema do usuário);
      // grab/grabbing são bitmaps do browser e ignoram o tema. Um toggle por pan.
      document.body.classList.add("canvas-panning");
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
  }, [getViewport, apply]);

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
  // sidebar/drawer) SEM sair do extent (apply clampa). Usado SÓ em navegação
  // explícita: clique na sidebar/Folders, minimap, Force Order.
  const focusOnPoint = useCallback((px: number, py: number, zoom: number, duration = 350) => {
    const { width, height } = storeApi.getState();
    const { left, right } = visibleInsets();
    const visW = Math.max(1, width - left - right);
    apply({ x: left + visW / 2 - px * zoom, y: height / 2 - py * zoom, zoom }, duration);
  }, [storeApi, apply, visibleInsets]);

  // organizeView — re-enquadra ancorando o TOPO do conteúdo em TOP_ANCHOR (em vez
  // de centralizar verticalmente). SÓ roda no botão "Organizar" e no 1º paint de
  // um modo sem câmera salva — nunca em reação a dado/aba/folder.
  const organizeView = useCallback((duration: number) => {
    const b = boundsRef.current;
    if (!b) return;
    // Fit calculado NA MÃO (sem fitView): o fitView do RF v12 é assíncrono e o
    // promise nem resolve quando chamado logo após o mount (nós ainda medindo).
    // Bounds vêm do contentBounds (lanes → jobs), o pane vem do store — tudo
    // síncrono e determinístico.
    const { width, height } = storeApi.getState();
    if (!width || !height) return;
    const bw = Math.max(1, b.maxX - b.minX);
    const bh = Math.max(1, b.maxY - b.minY);
    // Faixa visível (desconta sidebar/drawer): o fit e a centralização usam ela,
    // não a largura total do pane — senão o conteúdo cai atrás dos overlays.
    const { left, right } = visibleInsets();
    const visW = Math.max(1, width - left - right);
    const PADDING = 0.12; // fração do pane, como o fitView fazia
    const zoomFit = Math.min((visW * (1 - PADDING * 2)) / bw, (height * (1 - PADDING * 2)) / bh);
    const zoom = Math.max(MIN_ZOOM, Math.min(1, zoomFit)); // não afasta além do minZoom nem amplia >1
    const tx = left + visW / 2 - ((b.minX + b.maxX) / 2) * zoom;
    const ty = TOP_ANCHOR - b.minY * zoom;
    apply({ x: tx, y: ty, zoom }, duration);
  }, [storeApi, apply, visibleInsets]);

  // paneReady: o <ReactFlow> pode montar DEPOIS dos nodes existirem (ex.: login —
  // os dados chegam via _connected enquanto o LoginForm ainda cobre a tela) e o
  // onInit dispara ANTES do pane ser medido (width/height=0 → organize no-op).
  // Assinar as dimensões do store reativa o gate assim que o pane ganha tamanho.
  const paneReady = useStore((s) => s.width > 0 && s.height > 0);

  // Salva a posição da view que está SAINDO quando o viewContextKey muda (troca
  // de aba). Redundante com o remember-por-movimento, mas cobre drift (ex.:
  // setViewport animado interrompido). Declarado ANTES do efeito de restaurar:
  // getViewport aqui ainda devolve a posição da view antiga.
  //
  // SÓ salva se a view que sai TINHA conteúdo: câmera de canvas vazio não é
  // posição do usuário — salvá-la bloquearia o enquadre de 1ª vez quando o modo
  // finalmente ganhar jobs (ex.: visitar o Design vazio e voltar depois).
  // prevHasNodesRef = hasNodes do render ANTERIOR (a troca de key troca o canvas
  // no MESMO render, então o hasNodes atual já é o da view nova).
  const prevHasNodesRef = useRef(false);
  useLayoutEffect(() => {
    if (prevKeyRef.current !== viewContextKey) {
      if (prevHasNodesRef.current) savedViewports.current.set(prevKeyRef.current, getViewport());
      prevKeyRef.current = viewContextKey;
    }
    prevHasNodesRef.current = hasNodes;
  });

  // Persiste as câmeras ao sair/recarregar, pra que o F5 restaure exatamente onde
  // o usuário estava. pagehide cobre reload e fechar aba. A câmera ATIVA só entra
  // se o canvas tem conteúdo (recarregar na tela de login não pode gravar o
  // viewport default por cima da posição real).
  useEffect(() => {
    const flush = () => {
      try {
        if (hasNodesRef.current) savedViewports.current.set(prevKeyRef.current, getViewport());
        window.sessionStorage.setItem(
          VIEWPORTS_STORAGE_KEY,
          JSON.stringify([...savedViewports.current]),
        );
      } catch { /* ignore */ }
    };
    window.addEventListener("pagehide", flush);
    return () => window.removeEventListener("pagehide", flush);
  }, [getViewport]);

  // Ao ENTRAR numa view (troca de aba / remount do canvas): restaura a posição
  // salva SEM animar; se é a 1ª vez do modo, enquadra SINCRONAMENTE antes do
  // paint (duration 0, sem timeout) — aparece já enquadrado, sem pulo visível.
  // hasNodes é boolean (0↔N), então churn de dado (Force/Run Daily/WS) NÃO
  // re-dispara e a câmera fica onde o usuário deixou.
  useLayoutEffect(() => {
    if (!hasNodes || !paneReady) return;
    const saved = savedViewports.current.get(viewContextKey);
    if (saved) {
      setViewport(clampCamera(saved)); // clampa: o conteúdo pode ter mudado
      return;
    }
    organizeView(0); // 1ª vez do modo: enquadra antes do paint
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [viewContextKey, hasNodes, paneReady]);

  // Conteúdo mudou (folders filtrados, publish, daily) e a câmera ficou FORA do
  // novo limite? Puxa de volta com um ease curto — sem isso ela ficaria num mar
  // de preto sem conteúdo (e "pularia" no próximo pan). Dentro do limite = não
  // mexe (é o caso do churn normal). Deps nos VALORES da caixa: identidade do
  // objeto muda a cada re-layout, os números não.
  useEffect(() => {
    if (!bounds || !paneReady || panActiveRef.current) return;
    const vp = getViewport();
    const c = clampCamera(vp);
    if (Math.abs(c.x - vp.x) > 0.5 || Math.abs(c.y - vp.y) > 0.5) {
      setViewport(c, { duration: 200 });
      remember(c);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bounds?.minX, bounds?.minY, bounds?.maxX, bounds?.maxY, mode, paneReady]);

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
    // eslint-disable-next-line react-hooks/set-state-in-effect -- limpa o foco pendente após enquadrar 1× (Force Order materializa o nó async); a câmera só escreve via apply() e o foco é one-shot; ver roadmap §RH
    setPendingFocusId(null);
  }, [pendingFocusId, canvas.nodes, focusOnPoint, getViewport]);

  return { panExtent, organizeView, focusOnPoint, focusNode, setPendingFocusId, attachStage };
}
