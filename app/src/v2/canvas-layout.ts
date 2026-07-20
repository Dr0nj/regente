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
import type { JobInstance, JobDefinition, EdgeCondition, DepDateRef } from "@/lib/orchestrator-model";
import { EDGE_CONDITION_DEFAULT, TEAMS, odateOf } from "@/lib/orchestrator-model";
import { missingConds, poolHas, splitCondSuffix, instEdgeCondNames, instMissingConds } from "@/lib/conditions-model";
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
// BUG-9 — a geometria estática é AUTORITATIVA na prática: com `node.handles`
// presente, o parseHandles do @xyflow/system usa SEMPRE o fallback nos rebuilds
// (a medição do DOM não reassume — rebuild a cada tick reseta pro estático).
// Então o y do handle de saída tem que bater com a ALTURA REAL de cada card:
// a linha de TAGS (⚡FORCED/👻GHOST, própria abaixo do nome) deixa o card mais
// alto — com o y calibrado só pro card base, a seta nascia DENTRO do card
// ("atrás" dele) e ficava visivelmente mais curta. Duas variantes de handle,
// escolhidas por card no builder do Monitoring (Design não tem linha de tags).
const HANDLE_PX = 6;
const CARD_VISUAL_W = 200; // largura do JobNodeV2 (≠ NODE_W=220, a célula de layout)
const CARD_VISUAL_H = 55; // altura renderizada do card base (medida no lab)
const CARD_VISUAL_H_TAGGED = 72; // card com a linha de tags ⚡FORCED/👻GHOST
function jobHandles(cardH: number): NodeHandle[] {
  return [
    { type: "target", position: Position.Top, x: (CARD_VISUAL_W - HANDLE_PX) / 2, y: -HANDLE_PX / 2, width: HANDLE_PX, height: HANDLE_PX },
    { type: "source", position: Position.Bottom, x: (CARD_VISUAL_W - HANDLE_PX) / 2, y: cardH - HANDLE_PX / 2, width: HANDLE_PX, height: HANDLE_PX },
  ];
}
const JOB_HANDLES: NodeHandle[] = jobHandles(CARD_VISUAL_H);
const JOB_HANDLES_TAGGED: NodeHandle[] = jobHandles(CARD_VISUAL_H_TAGGED);
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
// ranksep dagre (dep vertical): folga suficiente pra LINHA de dependência
// aparecer entre pai e filho — com 28px os cards quase encostavam e só o ✓
// ficava visível (report do usuário, 2026-07-17).
const NODE_GAP_Y = 72;
const NODE_GAP_X = 36; // nodesep dagre (jobs paralelos na mesma linha)
const COL_PADDING_X = 24;
// Grade dos jobs SOLTOS (sem dependência interna). Configurável em Settings (Fase 2).
const LAYOUT_COLUMNS = 10; // colunas-base da grade (cap soft)
const LAYOUT_MAX_ROWS = 30; // ao passar disso, alarga colunas em vez de crescer pra baixo
export type LayoutConfig = { columns: number; maxRows: number };
export const DEFAULT_LAYOUT: LayoutConfig = { columns: LAYOUT_COLUMNS, maxRows: LAYOUT_MAX_ROWS };

// UI-3 — overrides POR FOLDER (team → parcial). Vêm do .regente-folder.yaml via
// GET /api/folders; campo ausente herda o global. Resolução única aqui para o
// Monitoring e o Design usarem a MESMA regra.
export type LayoutOverrides = ReadonlyMap<string, Partial<LayoutConfig>>;

function resolveLayout(team: string, cfg: LayoutConfig, overrides?: LayoutOverrides | null): LayoutConfig {
  const ov = overrides?.get(team);
  if (!ov) return cfg;
  return { columns: ov.columns ?? cfg.columns, maxRows: ov.maxRows ?? cfg.maxRows };
}

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
    // Cadeado da sidebar: HOLD por pausa de folder ("folder") vs hold individual
    // ("self"). Só quando em HOLD — INACTIVE também cobre CANCELLED (sem cadeado).
    holdScope: inst.status === "HOLD"
      ? (inst.holdScope === "folder" ? "folder" : "self")
      : undefined,
    // ODAT de origem quando carregada pela virada (carry-over): a sidebar
    // sub-agrupa os carried por data dentro da folder.
    carriedFrom: inst.carriedFrom,
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
 * Estado de uma linha de dependência — REFLEXO DO POOL de condições (modelo
 * único, 2026-07-17; docs/conditions-events.md). A linha P→C existe porque
 * alguma condição liga os dois (edgeCondNames: entradas de C que P produz), e
 * a cor diz o estado dessa condição PARA ESTE consumidor:
 *
 *   - VERDE ✓  — a condição existe no pool (o consumidor pode rodar), ou o
 *     consumidor JÁ RODOU sobre ela (RUNNING/OK — no OK ele a consome via
 *     saída−, mas a linha continua verde: deu certo).
 *   - VERMELHO ✗ — o job SUBSEQUENTE está com erro (NOTOK).
 *   - CINZA — a condição não existe (ainda não criada, consumida por um OK
 *     anterior — ex.: Set OK + rerun — ou deletada no painel Condições) e o
 *     consumidor está esperando.
 */
type DepState = "satisfied" | "blocked" | "pending";

/** Estado da linha POR PAR (cópia do pai × cópia do filho), pool-aware. */
function evaluateEdgeState(
  inst: JobInstance,
  linkNames: string[],
  pool: ReadonlySet<string>,
): DepState {
  if (inst.status === "NOTOK") return "blocked"; // vermelho = subsequente com erro
  if (inst.status === "RUNNING" || inst.status === "OK") return "satisfied";
  const odat = odateOf(inst);
  if (linkNames.length > 0 && linkNames.every((n) => poolHas(pool, n, odat))) {
    return "satisfied"; // a condição está lá — reflexo direto do pool
  }
  return "pending";
}

/**
 * Filtra as cópias do pai cuja DATA de origem casa o dateRef da aresta
 * (2026-07-16, escopo ODAT): o dia ativo mistura origens (a fresca de hoje ao
 * lado da carregada do dia 14) e a dependência só existe entre pares da diária
 * certa — sem isto, o job carregado ganhava linha (e satisfação visual) com o
 * pai FRESCO de hoje.
 *   - odat (default): pai com o MESMO ODAT do filho
 *   - prev: pai de diária ANTERIOR à do filho (aproximação visual do "New Day
 *     anterior" do server — candidatos; o claim verde é sempre exato)
 *   - stat: qualquer cópia
 */
export function parentsForEdge(
  inst: JobInstance,
  parents: JobInstance[],
  dateRef?: DepDateRef,
): JobInstance[] {
  const childOdate = odateOf(inst);
  switch (dateRef) {
    case "stat":
      return parents;
    case "prev":
      return parents.filter((p) => odateOf(p) < childOdate);
    default: // "odat" | undefined
      return parents.filter((p) => odateOf(p) === childOdate);
  }
}

/**
 * WAIT COND de UMA instance — a MESMA régua do gate do server: WAITING com
 * alguma condição de ENTRADA ausente do pool (todas as ConditionsIn, não só as
 * de setinha). Run Now (`manual`) bypassa. Exportada pro drawer/menus
 * decidirem as ações contextuais sem duplicar a semântica.
 *
 * M1: usa as condições CONGELADAS na ordem (`inst.condsIn`), não a def viva —
 * criar/editar deps no Design não muda o gate visual de instances já ordenadas.
 * A def viva é só fallback de instance LEGADA sem snapshot de conds
 * (`condsIn === undefined`).
 */
export function isWaitingOnConds(
  inst: JobInstance,
  def: JobDefinition | undefined,
  pool: ReadonlySet<string>,
): boolean {
  if (inst.status !== "WAITING" || inst.manual) return false;
  const missing = inst.condsIn !== undefined
    ? instMissingConds(inst.condsIn, odateOf(inst), pool)
    : missingConds(def, odateOf(inst), pool);
  return missing.length > 0;
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

function makeEdge(
  source: string,
  target: string,
  condition: EdgeCondition,
  state: DepState,
): Edge {
  const s = edgeStyleForState(state, condition);
  const label = state === "satisfied" ? "✓" : state === "blocked" ? "✗" : "";
  return {
    id: `e-${source}-${target}`,
    source,
    target,
    label,
    // Entre a lane (zIndex 0) e os cards (zIndex 10): o fundo da folder é
    // SÓLIDO (LaneLabelNode compõe sobre --v2-bg-canvas pros dots não vazarem)
    // e a camada default de edges fica ABAIXO dos nós — sem elevar, o retângulo
    // opaco da folder engole todas as linhas de dependência.
    zIndex: 5,
    data: { condition, state },
    style: {
      stroke: s.stroke,
      strokeWidth: 1.5,
      strokeDasharray: s.dash,
    },
    // ✓ pequeno de propósito: o símbolo é um selo, a LINHA é a informação —
    // grande demais ele cobria a linha inteira entre cards próximos.
    labelStyle: { fill: s.labelFill, fontSize: 9, fontFamily: "JetBrains Mono, monospace", fontWeight: 700 },
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
  overrides?: LayoutOverrides | null,
): { nodes: Node[]; lanes: LaneInfo[] } {
  const grouped = groupByTeam(items);
  const layouts: InnerLayout[] = [];
  for (const [team, members] of grouped) {
    layouts.push(layoutFolderInner(team, members, nodeIdOf, allEdges, resolveLayout(team, cfg, overrides)));
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

// Job tem agente disponível? Pinado (congelado na ordem) exige o id online;
// senão basta alguém anunciar a capability do jobType. SSH é agentless.
// M1: lê jobType/pinnedAgent CONGELADOS na instância, não a def viva.
function hasAgentFor(jobType: string, pinnedAgent: string | undefined, agents: AgentAvailability): boolean {
  const jt = (jobType || "").toUpperCase();
  if (jt === "SSH" || jt === "") return true;
  if (pinnedAgent) return agents.ids.has(pinnedAgent);
  return agents.caps.has(jt);
}

/** Estado do pool de recursos (GET /api/resources) p/ o card WAIT RESOURCE. */
export type ResourcePool = ReadonlyMap<string, { capacity: number; used: number }>;

// Job em WAIT RESOURCE? Algum recurso CONGELADO na ordem (F15) sem unidade livre
// no pool. Recurso desconhecido no pool → capacidade default 1 (mesma semântica
// do TryAcquire do server). `used` já conta os holders RUNNING; um WAITING não
// segura, então a régua é used+qty > cap. Pedido multi-recurso é tudo-ou-nada,
// então falta de QUALQUER um já bloqueia.
function hasResourceShortfall(resources: Record<string, number> | undefined, pool: ResourcePool): boolean {
  if (!resources) return false;
  for (const [name, qty] of Object.entries(resources)) {
    const st = pool.get(name);
    const cap = st ? st.capacity : 1;
    const used = st ? st.used : 0;
    if (used + qty > cap) return true;
  }
  return false;
}

export function buildMonitoringCanvas(rawInstances: JobInstance[], defs: JobDefinition[], cfg: LayoutConfig = DEFAULT_LAYOUT, agents?: AgentAvailability | null, overrides?: LayoutOverrides | null, pool: ReadonlySet<string> = new Set(), resourcePool?: ResourcePool | null): Canvas {
  // M1 — Monitoring 100% INSTANCE-DRIVEN: tudo (label/tipo/linhas/gates) vem
  // CONGELADO na ordem (schemaV18), nunca da def viva. As `defs` entram só como
  // FALLBACK de instance LEGADA sem backfill (label/jobType vazios).
  const defsById = new Map(defs.map((d) => [d.id, d] as const));
  // Relógio do gate de janela: WAIT RESOURCE só vale pra job já no horário (antes
  // disso é "WAIT" de agendamento). Rebuild por tick mantém isto fresco.
  const nowMs = Date.now();

  // Enriquecimento (só legado): instance sem colunas congeladas cai na def viva
  // pra folder/label/tipo aparecerem. Instance com snapshot (o caso normal) usa
  // os próprios campos — editar a def no Design não a reescreve.
  const instances: JobInstance[] = rawInstances.map((inst) => {
    const def = defsById.get(inst.definitionId);
    if (!def) return inst.label ? inst : { ...inst, label: inst.definitionId };
    return {
      ...inst,
      team: inst.team || def.team,
      // Frozen label (mesmo quando IGUAL ao id) vence; SÓ o vazio (legado sem
      // backfill) cai na def viva. Nunca comparar com definitionId — um label
      // congelado legítimo pode ser igual ao id (foi a raiz do card com o nome novo).
      label: inst.label || def.label,
      jobType: inst.jobType || def.jobType,
    };
  });

  // WAIT COND: instance WAITING com alguma condição de ENTRADA ausente do
  // pool — a MESMA régua do gate do server (todas as condsIn congeladas). O
  // card mostra "WAIT COND" pra distinguir de "esperando o horário".
  const waitingOnDeps = new Set<string>();

  // Índice PRODUTOR (do snapshot): base da condição → instances cujo condsOutAdd
  // congelado a adiciona. É a fonte ÚNICA das linhas do grafo — nunca a
  // topologia viva do Design (`def.upstream`), que só vale no modo design.
  // Assim, criar/ligar jobs novos no Design não desenha linhas em instances já
  // ordenadas; a linha nova só aparece na cópia forçada (que tem a saída no
  // próprio snapshot).
  const producersByBase = new Map<string, JobInstance[]>();
  for (const p of instances) {
    for (const n of p.condsOutAdd ?? []) {
      const base = splitCondSuffix(n).base;
      const list = producersByBase.get(base);
      if (list) list.push(p);
      else producersByBase.set(base, [p]);
    }
  }

  const edges: Edge[] = [];
  const rawEdges: Array<{ source: string; target: string }> = [];
  for (const inst of instances) { // inst = CONSUMIDOR
    if (isWaitingOnConds(inst, defsById.get(inst.definitionId), pool)) waitingOnDeps.add(inst.id);
    const cin = inst.condsIn ?? [];
    if (!cin.length) continue;
    const tgt = `m-${inst.id}`;
    const linkedParents = new Set<string>(); // 1 linha por par (produtor, consumidor)
    for (const n of cin) {
      const { base, ref } = splitCondSuffix(n);
      const producers = producersByBase.get(base);
      if (!producers) continue;
      // Só as cópias do produtor da diária que o SUFIXO da entrada aceita
      // (@odat mesma origem · @prev anterior · @stat qualquer): o filho
      // carregado do dia 14 conversa com o pai do 14, não com o fresco de hoje.
      const parents = parentsForEdge(inst, producers, ref);
      for (const parent of parents) {
        if (parent.id === inst.id || linkedParents.has(parent.id)) continue;
        linkedParents.add(parent.id);
        // As condições que LIGAM este par (entradas do filho que ESTE pai
        // produz): a cor da linha é o estado delas no POOL para este consumidor.
        // verde ✓ = existe (ou o filho rodou sobre ela); cinza = ainda não
        // existe / consumida / deletada; vermelho ✗ = filho NOTOK.
        const linkNames = instEdgeCondNames(parent.condsOutAdd, cin);
        const state = evaluateEdgeState(inst, linkNames, pool);
        const src = `m-${parent.id}`;
        rawEdges.push({ source: src, target: tgt });
        edges.push(makeEdge(src, tgt, EDGE_CONDITION_DEFAULT, state));
      }
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
      // BUG-9: card com linha de tags é mais alto — o handle de saída desce
      // junto, senão a seta nasce atrás do card. Só MANUAL (Order Force) e GHOST
      // (dry-run) fazem a linha de tags; Run Now não leva selo → card base.
      handles: (inst.manualOrder || inst.dryRun) ? JOB_HANDLES_TAGGED : JOB_HANDLES,
      data: {
        label: inst.label,
        jobType: inst.jobType,
        status: INSTANCE_TO_UI_STATUS[inst.status],
        team: inst.team,
        lastRun: inst.startedAt ? fmtHm(inst.startedAt) : undefined,
        mode: "monitoring",
        // Selo 🖐 MANUAL = SÓ Order Force (ordem colocada na mão). Run Now força
        // uma instance existente e NÃO ganha tag (force_mode='' → manualOrder=false).
        manualOrder: inst.manualOrder,
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
        // HOLD (operador ou pausa de folder): colapsa para INACTIVE na cor, então
        // o card sinaliza com um cadeado sobreposto — snapshot da própria instância.
        held: inst.status === "HOLD",
        // Cadeado âmbar (vs violeta do hold individual) quando o HOLD veio de uma
        // pausa de folder — não liberável 1-a-1, só pelo resume da folder.
        folderHeld: inst.status === "HOLD" && inst.holdScope === "folder",
        waitEvent: waitingOnDeps.has(inst.id),
        // WAIT AGENT (azul claro): WAITING sem bloqueio de dependência e sem
        // agente online capaz de executar. Só deriva quando a lista de agentes
        // já carregou (agents != null) — sem info, não acusa nada.
        // M1: jobType/pinnedAgent CONGELADOS na ordem (schemaV18), não a def viva.
        waitAgent: !!agents && inst.status === "WAITING" && !waitingOnDeps.has(inst.id) &&
          !hasAgentFor(inst.jobType, inst.pinnedAgent, agents),
        // WAIT CONFIRM (violeta): a ordem exige confirmação (Control-M "Wait for
        // confirmation") e esta instância ainda não foi confirmada. M1: lê o
        // confirmReq CONGELADO na ordem (schemaV18) — ligar Confirm no Design não
        // reescreve cards já materializados. O gate do server é o mesmo snapshot.
        waitConfirm: inst.status === "WAITING" && !inst.confirmed && !!inst.confirmReq,
        // WAIT RESOURCE (âmbar): a ordem exige um recurso/quota (F15 congelado,
        // schemaV19) sem unidade livre no pool. Deriva na MESMA ordem dos gates
        // do server (janela → confirm → cond → agente → recurso): só quando o
        // job já está no horário e não está preso por condição/confirm/agente —
        // aí o único motivo restante é o recurso. Sem pool carregado, não acusa.
        waitResource: !!resourcePool && inst.status === "WAITING" &&
          inst.scheduledAt <= nowMs &&
          !waitingOnDeps.has(inst.id) &&
          !(!!inst.confirmReq && !inst.confirmed) &&
          !(!!agents && !hasAgentFor(inst.jobType, inst.pinnedAgent, agents)) &&
          hasResourceShortfall(inst.resources, resourcePool),
      } as JobNodeData,
      draggable: false,
      zIndex: 10,
    }),
    rawEdges,
    (inst) => `m-${inst.id}`,
    cfg,
    overrides,
  );

  return { nodes, edges, lanes };
}

export function buildDesignCanvas(defs: JobDefinition[], cfg: LayoutConfig = DEFAULT_LAYOUT, overrides?: LayoutOverrides | null): Canvas {
  const edges: Edge[] = [];
  const rawEdges: Array<{ source: string; target: string }> = [];
  for (const def of defs) {
    if (!def.upstream?.length) continue;
    for (const u of def.upstream) {
      const src = `d-${u.from}`;
      const tgt = `d-${def.id}`;
      rawEdges.push({ source: src, target: tgt });
      edges.push(makeEdge(src, tgt, u.condition ?? EDGE_CONDITION_DEFAULT, "pending"));
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
    overrides,
  );

  return { nodes, edges, lanes };
}
