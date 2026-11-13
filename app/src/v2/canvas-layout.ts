/**
 * canvas-layout — módulo PURO de layout do canvas (sem React state).
 *
 * Constantes de geometria, builders de nodes/edges (Monitoring e Design) e o
 * layout hierárquico Dagre por folder (raízes e soltos no topo). Extraído do V2Preview.tsx (2026-07-01) para conter o blast
 * radius de mudanças de UI: layout muda AQUI, ciclo de vida do componente lá.
 */

import { recordCanvasMetric } from "./canvas-metrics";
import { dependencyLayout, JOB_WIDTH, JOB_HEIGHT, JOB_TAGGED_HEIGHT } from "./dependency-layout";
import { Position } from "@xyflow/react";
import type { Node, Edge, NodeHandle } from "@xyflow/react";
import type { JobNodeData } from "@/lib/job-config";
import type { JobInstance, JobDefinition, EdgeCondition, DepDateRef } from "@/lib/orchestrator-model";
import { EDGE_CONDITION_DEFAULT, TEAMS, odateOf } from "@/lib/orchestrator-model";
import { missingConds, poolHas, splitCondSuffix, instEdgeCondNames, instMissingConds, producesBase, condIsAlternative } from "@/lib/conditions-model";
import type { MonitoringJob } from "./MonitoringSidebarV2";

export type Mode = "design" | "monitoring";

/* ──────────────────────────────────────────────────────────────
   Constantes de layout (folders como colunas — Control-M style)
   ────────────────────────────────────────────────────────────── */

export const NODE_W = JOB_WIDTH;
export const NODE_H = JOB_TAGGED_HEIGHT;

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
const CARD_VISUAL_W = JOB_WIDTH; // shared with the rendered card
const CARD_VISUAL_H = JOB_HEIGHT; // browser-verified base card
const CARD_VISUAL_H_TAGGED = JOB_TAGGED_HEIGHT; // browser-verified tagged card
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
  UNCERTAIN: "UNCERTAIN",
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

const timeFormatter = new Intl.DateTimeFormat("en-GB", { hour12: false, hour: "2-digit", minute: "2-digit", second: "2-digit" });
function fmtHm(ms: number): string { return timeFormatter.format(ms).slice(0, 5); }

// fmtRunRange — o horário no card do Monitoring: início e, quando a execução já
// TERMINOU, "início–fim" (pedido do usuário; é o que o operador quer ler sem
// abrir o drawer). Enquanto roda (ou enquanto espera com uma tentativa anterior
// no currículo) fica só o início. Nunca imprime nada para quem nunca rodou —
// começar = ter entrado em RUNNING.
function fmtRunRange(inst: JobInstance): string | undefined {
  if (!inst.startedAt) return undefined;
  const start = fmtHm(inst.startedAt);
  const terminal = inst.status === "OK" || inst.status === "NOTOK";
  if (!terminal || !inst.completedAt) return start;
  return `${start}–${fmtHm(inst.completedAt)}`;
}

/** Resumo curto do schedule estruturado para exibir no node do canvas. */
function scheduleSummary(s: JobDefinition["schedule"]): string {
  const wd: Record<string, string> = { mon: "mon", tue: "tue", wed: "wed", thu: "thu", fri: "fri", sat: "sat", sun: "sun" };
  let base: string;
  switch (s.frequency ?? "daily") {
    case "weekly": base = (s.daysOfWeek ?? []).map((d) => wd[d] ?? d).join(",") || "weekly"; break;
    case "monthly": base = "day " + ((s.daysOfMonth ?? []).map((d) => d === -1 ? "last" : d).join(",") || "?"); break;
    case "businessday": base = (s.nthBusinessDays ?? []).map((d) => d === -1 ? "last bd" : `${d} bd`).join(",") || "business day"; break;
    case "advanced": base = s.advancedRule ?? "rule"; break;
    default: base = "daily";
  }
  return s.runAt ? `${base} ${s.runAt}` : base;
}

/* ──────────────────────────────────────────────────────────────
   Canvas builders (folders como colunas, dagre TB dentro)
   ────────────────────────────────────────────────────────────── */

export interface Canvas { nodes: Node[]; edges: Edge[]; lanes: LaneInfo[]; topology?: Array<{ source: string; target: string }> }
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

function edgeStyleForState(state: DepState) {
  // Padrão visual idêntico para condições do mesmo estado.
  // Todas as edges são tracejadas (uniformidade visual).
  //
  // Cores via token `--v2-edge-*` (tokens.css), NUNCA hex chumbado: o selo ✓/✗
  // é desenhado em SVG (<rect>/<text> do EdgeText), e `fill` inline resolve
  // var() igual a qualquer propriedade CSS. Com hex fixo o selo ficava um
  // quadrado quase preto sobre o canvas dos temas claros.
  const dash = "5 4";
  if (state === "satisfied") {
    return {
      stroke: "var(--v2-edge-ok)",
      labelFill: "var(--v2-edge-ok-label)",
      labelBg: "var(--v2-edge-ok-bg)",
      dash,
    };
  }
  if (state === "blocked") {
    return {
      stroke: "var(--v2-edge-blocked)",
      labelFill: "var(--v2-edge-blocked-label)",
      labelBg: "var(--v2-edge-blocked-bg)",
      dash,
    };
  }
  // pending — neutro/cinza, sem label
  return {
    stroke: "var(--v2-edge-idle)",
    labelFill: "var(--v2-edge-idle-label)",
    labelBg: "var(--v2-edge-idle-bg)",
    dash,
  };
}

function makeEdge(
  source: string,
  target: string,
  condition: EdgeCondition,
  state: DepState,
  // CL-4: `alt` = a condição que liga este par é um membro ALTERNATIVO (OR) na
  // lógica do consumidor — não um requisito duro (AND). A linha ganha um
  // tracejado mais fino (pontilhado) e o rótulo "OR" (mesmo vocabulário do
  // toggle AND/OR do drawer), pra o grafo distinguir ramos alternativos de
  // dependências obrigatórias. A setinha sempre cria AND.
  alt = false,
): Edge {
  const s = edgeStyleForState(state);
  const dash = alt ? "1.5 4" : s.dash;
  const sym = state === "satisfied" ? "✓" : state === "blocked" ? "✗" : "";
  const label = alt ? (sym ? `${sym} OR` : "OR") : sym;
  return {
    id: `e-${source}-${target}`,
    type: "dependencyV2",
    source,
    target,
    label,
    // Entre a lane (zIndex 0) e os cards (zIndex 10): o fundo da folder é
    // SÓLIDO (LaneLabelNode compõe sobre --v2-bg-canvas pros dots não vazarem)
    // e a camada default de edges fica ABAIXO dos nós — sem elevar, o retângulo
    // opaco da folder engole todas as linhas de dependência.
    zIndex: 5,
    data: { condition, state, alt },
    style: {
      stroke: s.stroke,
      strokeWidth: 1.5,
      strokeDasharray: dash,
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

  // All roots, including isolated jobs, occupy the first dependency row.
  void cfg; // Legacy grid settings no longer split roots away from dependencies.
  const positions = dependencyLayout(members.map(nodeIdOf), innerEdges, NODE_W, NODE_H);
  const values = [...positions.values()];
  return {
    team,
    positions,
    width: Math.max(0, ...values.map(p => p.x + NODE_W)),
    height: Math.max(0, ...values.map(p => p.y + NODE_H)),
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
      selected: false,
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
  /** ids ONLINE sem slot livre (inUse >= slots). Ausente = ocupação desconhecida. */
  full?: Set<string>;
  /** capabilities com ao menos um agente online COM slot livre. */
  capsFree?: Set<string>;
}

// Há slot livre para o job? Só responde "não" com ocupação conhecida.
function hasFreeSlotFor(jobType: string, pinnedAgent: string | undefined, agents: AgentAvailability): boolean {
  if (!agents.full || !agents.capsFree) return true;
  const jt = (jobType || "").toUpperCase();
  if (jt === "SSH" || jt === "") return true;
  if (pinnedAgent) return !agents.full.has(pinnedAgent);
  return agents.capsFree.has(jt);
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

// Geometry contains only topology, fixed bounds and edge metadata. It never owns
// status, selection or clocks. One model belongs to one canvas, so independent
// views cannot evict each other's geometry or retain an unbounded day history.
interface StructuralLink { source: string; target: string; condition: EdgeCondition; names: string[]; alt: boolean }
export interface CanvasGeometry extends Canvas { links: StructuralLink[] }

export function buildCanvasGeometry(
  mode: Mode, items: Array<{ id: string; team?: string; manualOrder?: boolean; dryRun?: boolean }>,
  links: StructuralLink[], cfg: LayoutConfig, overrides?: LayoutOverrides | null,
): CanvasGeometry {
  const prefix = mode === "monitoring" ? "m" : "d";
  const { nodes, lanes } = composeColumns(prefix, items, (item, x, y) => {
    const height = mode === "monitoring" && (item.manualOrder || item.dryRun) ? JOB_TAGGED_HEIGHT : JOB_HEIGHT;
    return { id: `${prefix}-${item.id}`, type: "jobV2", position: { x, y },
      initialWidth: NODE_W, initialHeight: height,
      handles: height === JOB_TAGGED_HEIGHT ? JOB_HANDLES_TAGGED : JOB_HANDLES,
      data: {}, selected: false, draggable: false, zIndex: 10 };
  }, links, item => `${prefix}-${item.id}`, cfg, overrides);
  return { nodes, lanes, links, edges: links.map(e => makeEdge(e.source, e.target, e.condition, "pending", e.alt)) };
}

function monitoringData(inst: JobInstance, def: JobDefinition | undefined, agents: AgentAvailability | null | undefined,
  pool: ReadonlySet<string>, resourcePool: ResourcePool | null | undefined, nowMs: number): JobNodeData {
  const waitEvent = isWaitingOnConds(inst, def, pool);
  return {
        label: inst.label,
        jobType: inst.jobType,
        status: INSTANCE_TO_UI_STATUS[inst.status],
        team: inst.team,
        lastRun: fmtRunRange(inst),
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
        waitEvent: waitEvent,
        // WAIT AGENT (azul claro): WAITING sem bloqueio de dependência e sem
        // agente online capaz de executar. Só deriva quando a lista de agentes
        // já carregou (agents != null) — sem info, não acusa nada.
        // M1: jobType/pinnedAgent CONGELADOS na ordem (schemaV18), não a def viva.
        waitAgent: !!agents && inst.status === "WAITING" && !waitEvent &&
          !hasAgentFor(inst.jobType, inst.pinnedAgent, agents),
        // WAIT CONFIRM (violeta): a ordem exige confirmação (Control-M "Wait for
        // confirmation") e esta instância ainda não foi confirmada. M1: lê o
        // confirmReq CONGELADO na ordem (schemaV18) — ligar Confirm no Design não
        // reescreve cards já materializados. O gate do server é o mesmo snapshot.
        waitConfirm: inst.status === "WAITING" && !inst.confirmed && !!inst.confirmReq,
        // WAIT SLOT (turquesa): no horário, sem condição/confirm pendente, agente
        // online — mas todos os slots ocupados (inclusive por UNCERTAIN).
        waitSlot: !!agents && inst.status === "WAITING" && inst.scheduledAt <= nowMs && !waitEvent &&
          !(!!inst.confirmReq && !inst.confirmed) &&
          hasAgentFor(inst.jobType, inst.pinnedAgent, agents) &&
          !hasFreeSlotFor(inst.jobType, inst.pinnedAgent, agents),
        // WAIT RESOURCE (âmbar): a ordem exige um recurso/quota (F15 congelado,
        // schemaV19) sem unidade livre no pool. Deriva na MESMA ordem dos gates
        // do server (janela → confirm → cond → agente → recurso): só quando o
        // job já está no horário e não está preso por condição/confirm/agente —
        // aí o único motivo restante é o recurso. Sem pool carregado, não acusa.
        waitResource: !!resourcePool && inst.status === "WAITING" &&
          inst.scheduledAt <= nowMs &&
          !waitEvent &&
          !(!!inst.confirmReq && !inst.confirmed) &&
          !(!!agents && !hasAgentFor(inst.jobType, inst.pinnedAgent, agents)) &&
          !(!!agents && !hasFreeSlotFor(inst.jobType, inst.pinnedAgent, agents)) &&
          hasResourceShortfall(inst.resources, resourcePool),
      } as JobNodeData;
}

// All card fields are primitive visual values. Comparing just this projection
// keeps output/log payloads and REST object allocation out of React Flow.
function sameData(a: Record<string, unknown>, b: Record<string, unknown>): boolean {
  if (a === b) return true;
  const keys = Object.keys(a);
  return keys.length === Object.keys(b).length && keys.every(k => Object.is(a[k], b[k]));
}

export function applyCanvasRuntimeState(geometry: CanvasGeometry, previous: Canvas | undefined,
  dataFor: (id: string) => JobNodeData, stateFor: (link: StructuralLink) => DepState): Canvas {
  const oldNodes = new Map(previous?.nodes.map(n => [n.id, n]));
  const oldEdges = new Map(previous?.edges.map(e => [e.id, e]));
  const nodes = geometry.nodes.map(base => {
    const old = oldNodes.get(base.id);
    if (base.type === "laneLabel") return old && sameData(old.data, base.data) &&
      old.position.x === base.position.x && old.position.y === base.position.y ? old : base;
    const data = dataFor(base.id.slice(2));
    if (old && old.position.x === base.position.x && old.position.y === base.position.y &&
      old.initialWidth === base.initialWidth && old.initialHeight === base.initialHeight && old.handles === base.handles && sameData(old.data, data)) return old;
    return { ...base, data };
  });
  const edges = geometry.links.map((link, i) => {
    const base = geometry.edges[i], old = oldEdges.get(base.id);
    const state = stateFor(link);
    if (old && old.data?.state === state && old.data?.alt === link.alt && old.data?.condition === link.condition) return old;
    return state === "pending" ? base : makeEdge(link.source, link.target, link.condition, state, link.alt);
  });
  const stableNodes = previous && nodes.length === previous.nodes.length && nodes.every((n, i) => n === previous.nodes[i]) ? previous.nodes : nodes;
  const stableEdges = previous && edges.length === previous.edges.length && edges.every((e, i) => e === previous.edges[i]) ? previous.edges : edges;
  return previous && stableNodes === previous.nodes && stableEdges === previous.edges && geometry.lanes === previous.lanes
    ? previous : { nodes: stableNodes, edges: stableEdges, lanes: geometry.lanes, topology: geometry.links };
}

function monitoringLinks(instances: JobInstance[]): StructuralLink[] {
  const producers = new Map<string, JobInstance[]>();
  for (const inst of instances) for (const name of inst.condsOutAdd ?? []) {
    const base = splitCondSuffix(name).base, list = producers.get(base);
    if (list) list.push(inst); else producers.set(base, [inst]);
  }
  const links: StructuralLink[] = [];
  for (const inst of instances) {
    const cin = inst.condsIn ?? [], linked = new Set<string>();
    for (const name of cin) {
      const { base, ref } = splitCondSuffix(name);
      for (const parent of parentsForEdge(inst, producers.get(base) ?? [], ref)) {
        if (parent.id === inst.id || linked.has(parent.id)) continue;
        linked.add(parent.id);
        const names = instEdgeCondNames(parent.condsOutAdd, cin);
        links.push({ source: `m-${parent.id}`, target: `m-${inst.id}`, condition: EDGE_CONDITION_DEFAULT,
          names, alt: names.some(n => condIsAlternative(inst.condLogic, n)) });
      }
    }
  }
  return links;
}

export function createCanvasModel() {
  let key = "", geometry: CanvasGeometry | undefined, canvas: Canvas | undefined;
  let layoutBuilds = 0;
  const signatures = new WeakMap<JobInstance, string>();
  const emptyPool = new Set<string>();
  const dataCache = new Map<string, { inst: JobInstance; def: JobDefinition | undefined; data: JobNodeData; due: boolean }>();
  const edgeStateCache = new Map<StructuralLink, { inst: JobInstance; state: DepState }>();
  let previousAgents: AgentAvailability | null | undefined, previousPool: ReadonlySet<string> | undefined;
  let previousResources: ResourcePool | null | undefined;
  const configKey = (cfg: LayoutConfig, overrides?: LayoutOverrides | null) =>
    JSON.stringify([cfg, [...(overrides ?? [])].sort(([a], [b]) => a.localeCompare(b)), NODE_W, NODE_H, JOB_HEIGHT]);
  return {
    get layoutBuilds() { return layoutBuilds; },
    monitoring(raw: JobInstance[], defs: JobDefinition[], cfg: LayoutConfig = DEFAULT_LAYOUT,
      agents?: AgentAvailability | null, overrides?: LayoutOverrides | null, pool: ReadonlySet<string> = emptyPool,
      resources?: ResourcePool | null): Canvas {
      const start = performance.now();
      const defsById = new Map(defs.map(d => [d.id, d]));
      const instances = raw.map(inst => {
        const def = defsById.get(inst.definitionId);
        if (inst.team && inst.label && inst.jobType) return inst;
        return { ...inst, team: inst.team || def?.team, label: inst.label || def?.label || inst.definitionId, jobType: inst.jobType || def?.jobType || "" };
      });
      // Signature includes ODAT scope, frozen dependencies, folder order and
      // actual card dimensions. It deliberately excludes every runtime field.
      const nextKey = "m" + configKey(cfg, overrides) + JSON.stringify(instances.map(inst => {
        let signature = signatures.get(inst);
        if (!signature) {
          signature = JSON.stringify([inst.id, inst.team, odateOf(inst), inst.condsIn, inst.condsOutAdd, inst.condLogic, !!inst.manualOrder, !!inst.dryRun]);
          signatures.set(inst, signature);
        }
        return signature;
      }));
      if (!geometry || key !== nextKey) {
        geometry = buildCanvasGeometry("monitoring", instances, monitoringLinks(instances), cfg, overrides);
        key = nextKey; layoutBuilds++;
        dataCache.clear();
        edgeStateCache.clear();
      }
      const byId = new Map(instances.map(inst => [inst.id, inst]));
      const now = Date.now();
      const poolChanged = previousPool !== pool, agentsChanged = previousAgents !== agents, resourcesChanged = previousResources !== resources;
      const before = canvas;
      canvas = applyCanvasRuntimeState(geometry, canvas, id => {
        const inst = byId.get(id)!, def = defsById.get(inst.definitionId), old = dataCache.get(id);
        const due = inst.scheduledAt <= now;
        if (old && old.inst === inst && old.def === def && old.due === due &&
          !(inst.status === "WAITING" && (poolChanged || agentsChanged || (resourcesChanged && Object.keys(inst.resources ?? {}).length > 0)))) return old.data;
        const data = monitoringData(inst, def, agents, pool, resources, now);
        dataCache.set(id, { inst, def, data, due });
        return data;
      }, link => {
        const inst = byId.get(link.target.slice(2))!, old = edgeStateCache.get(link);
        if (old?.inst === inst && !poolChanged) return old.state;
        const state = evaluateEdgeState(inst, link.names, pool);
        edgeStateCache.set(link, { inst, state });
        return state;
      });
      previousPool = pool; previousAgents = agents; previousResources = resources;
      const result = canvas;
      recordCanvasMetric("build", start, () => ({ instances: raw.length, nodes: result.nodes.length, edges: result.edges.length,
        nodesRecreated: result.nodes.filter((n, i) => n !== before?.nodes[i]).length,
        edgesRecreated: result.edges.filter((e, i) => e !== before?.edges[i]).length, layoutBuilds }));
      return canvas;
    },
    design(defs: JobDefinition[], cfg: LayoutConfig = DEFAULT_LAYOUT, overrides?: LayoutOverrides | null): Canvas {
      const nextKey = "d" + configKey(cfg, overrides) + JSON.stringify(defs.map(d => [d.id, d.team, d.upstream, d.conditionsIn, d.conditionsOutAdd, d.conditionLogic]));
      const byId = new Map(defs.map(d => [d.id, d]));
      if (!geometry || key !== nextKey) {
        const links: StructuralLink[] = [];
        for (const def of defs) for (const u of def.upstream ?? []) {
          const parent = byId.get(u.from);
          if (!parent) continue;
          const alt = (def.conditionsIn ?? []).some(n => producesBase(parent, splitCondSuffix(n).base) && condIsAlternative(def.conditionLogic, n));
          links.push({ source: `d-${u.from}`, target: `d-${def.id}`, condition: u.condition ?? EDGE_CONDITION_DEFAULT, alt, names: [] });
        }
        geometry = buildCanvasGeometry("design", defs, links, cfg, overrides); key = nextKey; layoutBuilds++;
        dataCache.clear();
      }
      canvas = applyCanvasRuntimeState(geometry, canvas, id => {
        const def = byId.get(id)!;
        return { label: def.label, jobType: def.jobType as JobNodeData["jobType"], team: def.team,
          status: def.schedule.enabled ? "WAITING" : "INACTIVE", schedule: scheduleSummary(def.schedule), mode: "design" };
      }, () => "pending");
      return canvas;
    },
  };
}

// Pure entry points for one-shot callers. The UI owns a retained model per mode.
export function buildMonitoringCanvas(...args: Parameters<ReturnType<typeof createCanvasModel>["monitoring"]>): Canvas {
  return createCanvasModel().monitoring(...args);
}
export function buildDesignCanvas(...args: Parameters<ReturnType<typeof createCanvasModel>["design"]>): Canvas {
  return createCanvasModel().design(...args);
}
