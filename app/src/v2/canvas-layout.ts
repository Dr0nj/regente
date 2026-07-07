/**
 * canvas-layout — módulo PURO de layout do canvas (sem React state).
 *
 * Constantes de geometria, builders de nodes/edges (Monitoring e Design) e o
 * layout por folder (colunas Control-M style: dagre TB pros conectados + grade
 * pros soltos). Extraído do V2Preview.tsx (2026-07-01) para conter o blast
 * radius de mudanças de UI: layout muda AQUI, ciclo de vida do componente lá.
 */

import Dagre from "@dagrejs/dagre";
import { Position } from "@xyflow/react";
import type { Node, Edge, NodeHandle } from "@xyflow/react";
import type { JobNodeData } from "@/lib/job-config";
import type { JobInstance, JobDefinition, EdgeCondition } from "@/lib/orchestrator-model";
import { EDGE_CONDITION_DEFAULT, TEAMS } from "@/lib/orchestrator-model";
import type { MonitoringJob } from "./MonitoringSidebarV2";

export type Mode = "design" | "monitoring";

/* ──────────────────────────────────────────────────────────────
   Constantes de layout (folders como colunas — Control-M style)
   ────────────────────────────────────────────────────────────── */

export const NODE_W = 220;
export const NODE_H = 72;

// Handles estáticos do card de job — fallback de geometria pro RF v12 renderizar
// a LINHA DE DEPENDÊNCIA antes do ResizeObserver medir o nó. Sem isto, toda vez
// que o array de nós é reconstruído (cada update de status) o RF zera
// internals.handleBounds pra `undefined` — porque parseHandles() só preserva a
// medição anterior quando userNode.measured existe, e nossos nós não carregam
// `measured`. Com handleBounds nulo, getEdgePosition() retorna null e a EDGE SOME
// (mesma raiz do "cards somem", camada 3, agora na aresta: sob rajada o re-measure
// não chega e a linha fica sumida até um F5). Declarando os handles, o fallback
// `toHandleBounds(node.handles)` mantém a aresta sempre posicionável; a medição
// real do DOM assume depois (getEdgePosition prefere internals.handleBounds).
// Geometria = padrão do RF pro card 200×55 com nub de 6px (centro horizontal,
// 3px além de cada ponta), pra o fallback coincidir com o measured — sem "pulo".
const HANDLE_PX = 6;
const CARD_VISUAL_W = 200; // largura do JobNodeV2 (≠ NODE_W=220, a célula de layout)
const CARD_VISUAL_H = 55; // altura renderizada do card (medida no lab)
const JOB_HANDLES: NodeHandle[] = [
  { type: "target", position: Position.Top, x: (CARD_VISUAL_W - HANDLE_PX) / 2, y: -HANDLE_PX / 2, width: HANDLE_PX, height: HANDLE_PX },
  { type: "source", position: Position.Bottom, x: (CARD_VISUAL_W - HANDLE_PX) / 2, y: CARD_VISUAL_H - HANDLE_PX / 2, width: HANDLE_PX, height: HANDLE_PX },
];
// Âncora vertical de entrada do canvas: o topo do conteúdo fica este tanto abaixo
// do topo da área (px de tela). Um pouco mais baixo que a trava antiga (24) — mesma
// sensação do "Organizar". Vale pro Monitoring e pro Design.
export const TOP_ANCHOR = 88;
// ── Folgas do limite de pan (câmera presa ao CONTEÚDO — regra do usuário:
// "não tem porque rolar telas e telas em preto") ──
// Acima do conteúdo, em px de TELA: um tico além da âncora do Organizar — puxar a
// tela pra baixo para logo acima de onde o Organizar deixaria.
export const PAN_SLACK_TOP_SCREEN = TOP_ANCHOR + 32;
// Abaixo do último job, em px de TELA: "puxar pra cima tem limite = o último job".
export const PAN_SLACK_BOTTOM_SCREEN = 200;
// Monitoring — px de MUNDO além da travessia completa: o último job da direita pode
// sumir na esquerda "por pouco" (e vice-versa); este é o "pouco".
export const PAN_CROSS_SLACK = 40;
// Design — margem (px de mundo) pros LADOS da caixa dos jobs: visão mais contida
// que o Monitoring — não atravessa, só respira ao redor do conteúdo.
export const DESIGN_PAN_MARGIN_X = 360;
const NODE_GAP_Y = 28; // ranksep dagre (dep vertical)
const NODE_GAP_X = 36; // nodesep dagre (jobs paralelos na mesma linha)
const COL_PADDING_X = 24;
// Grade dos jobs SOLTOS (sem dependência interna). Configurável em Settings (Fase 2).
const LAYOUT_COLUMNS = 10; // colunas-base da grade (cap soft)
const LAYOUT_MAX_ROWS = 30; // ao passar disso, alarga colunas em vez de crescer pra baixo
export type LayoutConfig = { columns: number; maxRows: number };
export const DEFAULT_LAYOUT: LayoutConfig = { columns: LAYOUT_COLUMNS, maxRows: LAYOUT_MAX_ROWS };

// readLayoutConfig — lê a config de layout do localStorage (default 10/30), com
// clamp sensato. Mesma estratégia do toggle do minimap (pref de visão por browser).
export function readLayoutConfig(): LayoutConfig {
  if (typeof window === "undefined") return DEFAULT_LAYOUT;
  const num = (k: string, def: number, min: number, max: number) => {
    const v = parseInt(window.localStorage.getItem(k) ?? "", 10);
    return Number.isFinite(v) ? Math.min(max, Math.max(min, v)) : def;
  };
  return {
    columns: num("regente:layoutCols", LAYOUT_COLUMNS, 1, 40),
    maxRows: num("regente:layoutMaxRows", LAYOUT_MAX_ROWS, 1, 200),
  };
}
const COL_PADDING_TOP = 40; // espaço pro header da folder
const COL_PADDING_BOTTOM = 24;
const COL_GAP = 28; // gap horizontal entre folders
const CANVAS_PADDING = 24;

const INSTANCE_TO_UI_STATUS: Record<JobInstance["status"], JobNodeData["status"]> = {
  OK: "SUCCESS",
  NOTOK: "FAILED",
  RUNNING: "RUNNING",
  WAITING: "WAITING",
  HOLD: "INACTIVE",
  CANCELLED: "INACTIVE",
};

export function instanceToMonitoring(inst: JobInstance): MonitoringJob {
  return {
    id: inst.id,
    label: inst.label,
    team: inst.team ?? "—",
    jobType: inst.jobType as JobNodeData["jobType"],
    status: INSTANCE_TO_UI_STATUS[inst.status],
    durationMs: inst.durationMs ?? (inst.startedAt ? Date.now() - inst.startedAt : undefined),
    startedAt: inst.startedAt ? fmtHm(inst.startedAt) : undefined,
  };
}

function fmtHm(ms: number): string {
  return new Date(ms).toLocaleTimeString("en-GB", { hour12: false }).slice(0, 5);
}

/** Resumo curto do schedule estruturado para exibir no node do canvas. */
function scheduleSummary(s: JobDefinition["schedule"]): string {
  const wd: Record<string, string> = { mon: "seg", tue: "ter", wed: "qua", thu: "qui", fri: "sex", sat: "sáb", sun: "dom" };
  let base = "";
  switch (s.frequency ?? "daily") {
    case "weekly": base = (s.daysOfWeek ?? []).map((d) => wd[d] ?? d).join(",") || "semanal"; break;
    case "monthly": base = "dia " + ((s.daysOfMonth ?? []).map((d) => d === -1 ? "últ" : d).join(",") || "?"); break;
    case "businessday": base = (s.nthBusinessDays ?? []).map((d) => d === -1 ? "últ útil" : `${d}º út`).join(",") || "dia útil"; break;
    case "advanced": base = s.advancedRule ?? "regra"; break;
    default: base = "diário";
  }
  return s.runAt ? `${base} ${s.runAt}` : base;
}

/* ──────────────────────────────────────────────────────────────
   Canvas builders (folders como colunas, dagre TB dentro)
   ────────────────────────────────────────────────────────────── */

export interface Canvas { nodes: Node[]; edges: Edge[]; lanes: LaneInfo[] }
export interface LaneInfo { team: string; x: number; y: number; width: number; height: number; count: number }

export interface ContentBounds { minX: number; minY: number; maxX: number; maxY: number }

// contentBounds — caixa do CONTEÚDO do canvas (lanes contêm todos os jobs +
// padding; fallback nos nós quando não há lane). Fonte única pro limite de pan e
// pro Organizar — os dois derivam da MESMA caixa, então nunca divergem.
export function contentBounds(canvas: Canvas): ContentBounds | null {
  const L = canvas.lanes;
  if (L.length > 0) {
    return {
      minX: Math.min(...L.map((l) => l.x)),
      maxX: Math.max(...L.map((l) => l.x + l.width)),
      minY: Math.min(...L.map((l) => l.y)),
      maxY: Math.max(...L.map((l) => l.y + l.height)),
    };
  }
  if (canvas.nodes.length === 0) return null;
  return {
    minX: Math.min(...canvas.nodes.map((n) => n.position.x)),
    maxX: Math.max(...canvas.nodes.map((n) => n.position.x + NODE_W)),
    minY: Math.min(...canvas.nodes.map((n) => n.position.y)),
    maxY: Math.max(...canvas.nodes.map((n) => n.position.y + NODE_H)),
  };
}

function groupByTeam<T extends { team?: string }>(items: T[]): Map<string, T[]> {
  const map = new Map<string, T[]>();
  // garante ordem: TEAMS canônicos primeiro, depois custom
  for (const t of TEAMS) map.set(t, []);
  for (const it of items) {
    const team = (it.team ?? "—").trim() || "—";
    if (!map.has(team)) map.set(team, []);
    map.get(team)!.push(it);
  }
  // remove lanes vazias
  for (const [k, v] of [...map.entries()]) if (v.length === 0) map.delete(k);
  return map;
}

/**
 * Estado de satisfação de uma dependência (Control-M semantics).
 *
 * Regra do operador (definida pelo usuário):
 *   - Pai NOTOK/CANCELLED → vermelho SEMPRE, independente do tipo
 *     de condição. Só "Set OK" (converte NOTOK→OK manualmente) libera.
 *   - Pai OK → verde (condição satisfeita visualmente).
 *   - Pai WAITING/RUNNING/HOLD → âmbar (pendente).
 *
 * Mesmo que o filho tenha sido FORCED/Run Now e esteja rodando, a
 * edge para um pai vermelho permanece vermelha — ela representa o
 * estado da DEPENDÊNCIA, não do filho.
 */
type DepState = "satisfied" | "blocked" | "pending";

function evaluateDepState(parentStatus: JobInstance["status"]): DepState {
  if (parentStatus === "OK") return "satisfied";
  if (parentStatus === "NOTOK" || parentStatus === "CANCELLED") return "blocked";
  return "pending"; // WAITING/RUNNING/HOLD
}

function edgeStyleForState(state: DepState, _condition: EdgeCondition) {
  // Padrão visual idêntico para condições do mesmo estado.
  // Todas as edges são tracejadas (uniformidade visual).
  const dash = "5 4";
  if (state === "satisfied") {
    return { stroke: "#11C76F", labelFill: "#11C76F", labelBg: "#052e19", dash };
  }
  if (state === "blocked") {
    return { stroke: "#dc2626", labelFill: "#fca5a5", labelBg: "#450a0a", dash };
  }
  // pending — neutro/cinza, sem label
  return { stroke: "#525252", labelFill: "#a3a3a3", labelBg: "#1c1917", dash };
}

/**
 * Detecta violação de invariante: o par (status pai, status filho, condição)
 * representa um estado que jamais deveria existir num scheduler correto.
 * Ex.: filho RUNNING/OK antes do pai terminar com on-success.
 *
 * Isto é APENAS detecção/warning; o scheduler-runtime é quem previne
 * promoção. Este guard captura stale data do localStorage.
 */
function isConditionInvariantViolated(
  parentStatus: JobInstance["status"],
  childStatus: JobInstance["status"],
  condition: EdgeCondition,
): boolean {
  const childStarted = childStatus === "RUNNING" || childStatus === "OK" || childStatus === "NOTOK";
  if (!childStarted) return false;
  if (condition === "on-success") return parentStatus !== "OK";
  if (condition === "on-failure") return parentStatus !== "NOTOK";
  if (condition === "on-complete" || condition === "always") {
    return parentStatus !== "OK" && parentStatus !== "NOTOK";
  }
  return false;
}

function makeEdge(
  source: string,
  target: string,
  condition: EdgeCondition,
  parentStatus: JobInstance["status"],
): Edge {
  const state = evaluateDepState(parentStatus);
  const s = edgeStyleForState(state, condition);
  const label = state === "satisfied" ? "✓" : state === "blocked" ? "✗" : "";
  return {
    id: `e-${source}-${target}`,
    source,
    target,
    label,
    data: { condition, state },
    style: {
      stroke: s.stroke,
      strokeWidth: 1.5,
      strokeDasharray: s.dash,
    },
    labelStyle: { fill: s.labelFill, fontSize: 12, fontFamily: "JetBrains Mono, monospace", fontWeight: 700 },
    labelBgStyle: { fill: s.labelBg },
  };
}

/**
 * Roda dagre TB isolado em cada team usando só edges internas; retorna
 * posições LOCAIS (origem 0,0) e bounding box. Offset horizontal é
 * aplicado pelo chamador para empilhar colunas.
 */
interface InnerLayout {
  team: string;
  positions: Map<string, { x: number; y: number }>;
  width: number;
  height: number;
  count: number;
}

function layoutFolderInner<T extends { id: string; team?: string }>(
  team: string,
  members: T[],
  nodeIdOf: (t: T) => string,
  allEdges: Array<{ source: string; target: string }>,
  cfg: LayoutConfig = DEFAULT_LAYOUT,
): InnerLayout {
  const memberIds = new Set(members.map((m) => nodeIdOf(m)));
  const innerEdges = allEdges.filter((e) => memberIds.has(e.source) && memberIds.has(e.target));

  // Particiona a folder: CONECTADOS (≥1 aresta interna) vs SOLTOS (sem aresta interna).
  // Dependentes mantêm o fluxo top-down do dagre; soltos vão pra uma GRADE (evita a
  // fila horizontal infinita que o dagre faz quando todo nó cai no rank 0).
  const connectedIds = new Set<string>();
  for (const e of innerEdges) { connectedIds.add(e.source); connectedIds.add(e.target); }
  const connected = members.filter((m) => connectedIds.has(nodeIdOf(m)));
  const standalone = members.filter((m) => !connectedIds.has(nodeIdOf(m)));

  const positions = new Map<string, { x: number; y: number }>();
  let flowW = 0, flowH = 0;

  // 1) CONECTADOS → dagre TB (camadas: A em cima, B/C lado a lado embaixo, etc.).
  if (connected.length > 0) {
    const g = new Dagre.graphlib.Graph().setDefaultEdgeLabel(() => ({}));
    g.setGraph({ rankdir: "TB", nodesep: NODE_GAP_X, ranksep: NODE_GAP_Y, marginx: 0, marginy: 0 });
    for (const m of connected) g.setNode(nodeIdOf(m), { width: NODE_W, height: NODE_H });
    for (const e of innerEdges) g.setEdge(e.source, e.target);
    Dagre.layout(g);

    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    const raw = new Map<string, { x: number; y: number }>();
    for (const m of connected) {
      const dn = g.node(nodeIdOf(m));
      const x0 = dn.x - NODE_W / 2;
      const y0 = dn.y - NODE_H / 2;
      raw.set(nodeIdOf(m), { x: x0, y: y0 });
      if (x0 < minX) minX = x0;
      if (y0 < minY) minY = y0;
      if (x0 + NODE_W > maxX) maxX = x0 + NODE_W;
      if (y0 + NODE_H > maxY) maxY = y0 + NODE_H;
    }
    for (const [id, p] of raw) positions.set(id, { x: p.x - minX, y: p.y - minY });
    flowW = maxX - minX;
    flowH = maxY - minY;
  }

  // 2) SOLTOS → GRADE com wrap + alargamento, ABAIXO da zona de fluxos.
  //    cols-base = LAYOUT_COLUMNS; passou de LAYOUT_MAX_ROWS linhas, alarga em vez
  //    de crescer pra baixo (cols = max(base, ceil(N/maxRows))). Ordem estável p/ id.
  let gridW = 0, gridH = 0;
  const gap = flowH > 0 && standalone.length > 0 ? NODE_GAP_Y * 3 : 0;
  if (standalone.length > 0) {
    const sorted = [...standalone].sort((a, b) => nodeIdOf(a).localeCompare(nodeIdOf(b)));
    const cols = Math.max(cfg.columns, Math.ceil(sorted.length / cfg.maxRows));
    const cellW = NODE_W + NODE_GAP_X;
    const cellH = NODE_H + NODE_GAP_Y;
    const baseY = flowH + gap;
    let usedCols = 0, usedRows = 0;
    sorted.forEach((m, k) => {
      const c = k % cols;
      const r = Math.floor(k / cols);
      positions.set(nodeIdOf(m), { x: c * cellW, y: baseY + r * cellH });
      if (c + 1 > usedCols) usedCols = c + 1;
      if (r + 1 > usedRows) usedRows = r + 1;
    });
    gridW = usedCols * cellW - NODE_GAP_X;
    gridH = usedRows * cellH - NODE_GAP_Y;
  }

  return {
    team,
    positions,
    width: Math.max(flowW, gridW),
    height: flowH + gap + gridH,
    count: members.length,
  };
}

/**
 * Posiciona colunas lado-a-lado e emite:
 *  - lane node (container/retângulo) por folder
 *  - job nodes posicionados absolutamente dentro da coluna
 */
function composeColumns<T extends { id: string; team?: string }>(
  prefix: "m" | "d",
  items: T[],
  buildJobNode: (t: T, absX: number, absY: number) => Node,
  allEdges: Array<{ source: string; target: string }>,
  nodeIdOf: (t: T) => string,
  cfg: LayoutConfig = DEFAULT_LAYOUT,
): { nodes: Node[]; lanes: LaneInfo[] } {
  const grouped = groupByTeam(items);
  const layouts: InnerLayout[] = [];
  for (const [team, members] of grouped) {
    layouts.push(layoutFolderInner(team, members, nodeIdOf, allEdges, cfg));
  }

  const nodes: Node[] = [];
  const lanes: LaneInfo[] = [];
  let cursorX = CANVAS_PADDING;
  const topY = CANVAS_PADDING;

  for (const L of layouts) {
    const colWidth = L.width + COL_PADDING_X * 2;
    const colHeight = L.height + COL_PADDING_TOP + COL_PADDING_BOTTOM;

    // Container/retângulo da folder (atrás dos jobs)
    nodes.push({
      id: `lane-${prefix}-${L.team}`,
      type: "laneLabel",
      position: { x: cursorX, y: topY },
      // initialWidth/Height: o RF v12 mantém o nó `visibility:hidden` até seu
      // ResizeObserver medir as dimensões. Sob rajada de comandos esse observer
      // estoura ("ResizeObserver loop completed with undelivered notifications")
      // e a medida nunca chega → o nó fica invisível e o board "some" até um F5.
      // Como o layout é determinístico, damos a dimensão de cara: hasDimensions
      // fica true na 1ª render e o nó nunca depende da medição pra aparecer.
      initialWidth: colWidth,
      initialHeight: colHeight,
      data: { team: L.team, count: L.count, width: colWidth, height: colHeight },
      draggable: false,
      selectable: false,
      connectable: false,
      zIndex: 0,
    });

    lanes.push({
      team: L.team,
      x: cursorX,
      y: topY,
      width: colWidth,
      height: colHeight,
      count: L.count,
    });

    // Jobs dentro da coluna
    const members = grouped.get(L.team)!;
    for (const m of members) {
      const local = L.positions.get(nodeIdOf(m))!;
      const absX = cursorX + COL_PADDING_X + local.x;
      const absY = topY + COL_PADDING_TOP + local.y;
      nodes.push(buildJobNode(m, absX, absY));
    }

    cursorX += colWidth + COL_GAP;
  }

  return { nodes, lanes };
}

/** Disponibilidade de agentes (derivada de GET /api/agents) p/ o card WAIT AGENT. */
export interface AgentAvailability {
  ids: Set<string>;   // ids de agentes ONLINE
  caps: Set<string>;  // capabilities anunciadas pelos agentes online (UPPERCASE)
}

// Job tem agente disponível? Pinado (actionConfig._agentId) exige o id online;
// senão basta alguém anunciar a capability do jobType. SSH é agentless.
function hasAgentFor(def: JobDefinition | undefined, jobType: string, agents: AgentAvailability): boolean {
  const jt = (jobType || "").toUpperCase();
  if (jt === "SSH" || jt === "") return true;
  const pinned = (def?.actionConfig as Record<string, unknown> | undefined)?._agentId;
  if (typeof pinned === "string" && pinned) return agents.ids.has(pinned);
  return agents.caps.has(jt);
}

export function buildMonitoringCanvas(rawInstances: JobInstance[], defs: JobDefinition[], cfg: LayoutConfig = DEFAULT_LAYOUT, agents?: AgentAvailability | null): Canvas {
  // Edges a partir do upstream da definition, resolvidas para instances do mesmo dia.
  const defsById = new Map(defs.map((d) => [d.id, d] as const));

  // Enriquecimento: server mode devolve instances sem team/label/jobType
  // (server-instance-store.toWeb hardcoda undefined). Fundimos a partir
  // da definition correspondente para que folder/label apareçam no monitoring.
  const instances: JobInstance[] = rawInstances.map((inst) => {
    const def = defsById.get(inst.definitionId);
    if (!def) return inst;
    return {
      ...inst,
      team: inst.team || def.team,
      label: inst.label && inst.label !== inst.definitionId ? inst.label : def.label,
      jobType: inst.jobType || def.jobType,
    };
  });

  const instByDefId = new Map<string, JobInstance>();
  for (const i of instances) instByDefId.set(i.definitionId, i);

  // WAIT EVENT (paridade Control-M): instance WAITING cuja dependência ainda não
  // liberou — pai não rodou / rodando / falhou. O card mostra "WAIT EVENT" em vez
  // de "WAIT" pra distinguir de "esperando o horário". Mesma fonte visual das
  // edges (evaluateDepState): aresta âmbar/vermelha ⇔ card em WAIT EVENT.
  const waitingOnDeps = new Set<string>();

  const edges: Edge[] = [];
  const rawEdges: Array<{ source: string; target: string }> = [];
  for (const inst of instances) {
    const def = defsById.get(inst.definitionId);
    if (!def?.upstream?.length) continue;
    for (const u of def.upstream) {
      const parent = instByDefId.get(u.from);
      if (!parent) {
        // Pai ainda não materializado neste dia = dependência não satisfeita.
        if (inst.status === "WAITING") waitingOnDeps.add(inst.id);
        continue;
      }
      if (inst.status === "WAITING" && evaluateDepState(parent.status) !== "satisfied") {
        waitingOnDeps.add(inst.id);
      }
      const condition = u.condition ?? EDGE_CONDITION_DEFAULT;
      const src = `m-${parent.id}`;
      const tgt = `m-${inst.id}`;
      rawEdges.push({ source: src, target: tgt });

      // Detecta violação de invariante apenas para console warning
      // (não afeta visual: a cor da edge segue o estado do pai).
      // EXCEÇÃO: instances forçadas (manual) bypassam deps por design
      // (Control-M "Order Force") — não é violação, é intencional.
      const violated = !inst.manual && isConditionInvariantViolated(parent.status, inst.status, condition);
      if (violated && typeof console !== "undefined") {
        // eslint-disable-next-line no-console
        console.warn(
          `[regente] dependency invariant suspicious: ${parent.label}(${parent.status}) -${condition}-> ${inst.label}(${inst.status})`,
        );
      }
      edges.push(makeEdge(src, tgt, condition, parent.status));
    }
  }

  const { nodes, lanes } = composeColumns(
    "m",
    instances,
    (inst, x, y) => ({
      id: `m-${inst.id}`,
      type: "jobV2",
      position: { x, y },
      // Dimensão de cara (ver nota na lane): evita o `visibility:hidden` do RF
      // enquanto mede — sob rajada a medição pode não chegar e o card some.
      initialWidth: NODE_W,
      initialHeight: NODE_H,
      // Handles estáticos (ver JOB_HANDLES): mantém a LINHA de dependência
      // posicionável mesmo antes/sem a medição — sob rajada a edge não some.
      handles: JOB_HANDLES,
      data: {
        label: inst.label,
        jobType: inst.jobType,
        status: INSTANCE_TO_UI_STATUS[inst.status],
        team: inst.team,
        lastRun: inst.startedAt ? fmtHm(inst.startedAt) : undefined,
        mode: "monitoring",
        forced: inst.manual,
        // Selo 👻GHOST ("job roda sem fazer nada — log only"): lê o dryRun
        // CONGELADO na própria instância (snapshot da ordem), NUNCA a def viva.
        //
        // Por quê (bug corrigido em 2026-07-04): o Monitoring é IMUTÁVEL — uma
        // instância já ordenada só muda numa NOVA ordem (daily/force/manual).
        // Antes, isto lia `defsById.get(inst.definitionId)?.dryRun` (a def VIVA),
        // então ligar dryRun no Design + publicar reescrevia o selo de jobs já
        // materializados em tempo real (o "ghost fantasma"). O snapshot vem do
        // server (coluna dry_run, schemaV9) e do createInstance no path local.
        dryRun: !!inst.dryRun,
        // HOLD manual (operador): colapsa para INACTIVE na cor, então o card
        // sinaliza com um cadeado sobreposto — snapshot da própria instância.
        held: inst.status === "HOLD",
        waitEvent: waitingOnDeps.has(inst.id),
        // WAIT AGENT (azul claro): WAITING sem bloqueio de dependência e sem
        // agente online capaz de executar. Só deriva quando a lista de agentes
        // já carregou (agents != null) — sem info, não acusa nada.
        waitAgent: !!agents && inst.status === "WAITING" && !waitingOnDeps.has(inst.id) &&
          !hasAgentFor(defsById.get(inst.definitionId), inst.jobType, agents),
      } as JobNodeData,
      draggable: false,
      zIndex: 10,
    }),
    rawEdges,
    (inst) => `m-${inst.id}`,
    cfg,
  );

  return { nodes, edges, lanes };
}

export function buildDesignCanvas(defs: JobDefinition[], cfg: LayoutConfig = DEFAULT_LAYOUT): Canvas {
  const edges: Edge[] = [];
  const rawEdges: Array<{ source: string; target: string }> = [];
  for (const def of defs) {
    if (!def.upstream?.length) continue;
    for (const u of def.upstream) {
      const src = `d-${u.from}`;
      const tgt = `d-${def.id}`;
      rawEdges.push({ source: src, target: tgt });
      edges.push(makeEdge(src, tgt, u.condition ?? EDGE_CONDITION_DEFAULT, "WAITING"));
    }
  }

  const { nodes, lanes } = composeColumns(
    "d",
    defs,
    (def, x, y) => ({
      id: `d-${def.id}`,
      type: "jobV2",
      position: { x, y },
      // Dimensão de cara (ver nota na lane): evita o `visibility:hidden` do RF
      // enquanto mede — sob rajada a medição pode não chegar e o card some.
      initialWidth: NODE_W,
      initialHeight: NODE_H,
      // Handles estáticos (ver JOB_HANDLES): mantém a LINHA de dependência
      // posicionável mesmo antes/sem a medição — sob rajada a edge não some.
      handles: JOB_HANDLES,
      data: {
        label: def.label,
        jobType: def.jobType as JobNodeData["jobType"],
        status: def.schedule.enabled ? "WAITING" : "INACTIVE",
        team: def.team,
        schedule: scheduleSummary(def.schedule),
        mode: "design",
      } as JobNodeData,
      zIndex: 10,
    }),
    rawEdges,
    (def) => `d-${def.id}`,
    cfg,
  );

  return { nodes, edges, lanes };
}
