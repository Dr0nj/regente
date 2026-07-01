import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Dagre from "@dagrejs/dagre";
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  BackgroundVariant,
  useReactFlow,
  useStore,
  useStoreApi,
  type Node,
  type Edge,
  type Connection,
  type NodeMouseHandler,
  type OnConnect,
  type ReactFlowInstance,
} from "@xyflow/react";
import JobNodeV2 from "./JobNodeV2";
import LaneLabelNode from "./LaneLabelNode";
import MonitoringSidebarV2, { type MonitoringJob } from "./MonitoringSidebarV2";
import ScaleMonitor from "./ScaleMonitor";
import DailyDiffModal from "./DailyDiffModal";
import DryRunModal from "./DryRunModal";
import DesignSidebarV2 from "./DesignSidebarV2";
import InstanceDetailsDrawer from "./InstanceDetailsDrawer";
import JobConfigDrawer from "./JobConfigDrawer";
import CanvasContextMenu, { type ContextMenuItem } from "./CanvasContextMenu";
import FolderManagerDialog from "./FolderManagerDialog";
import BulkActionBar from "./BulkActionBar";
import type { JobNodeData, JobType } from "@/lib/job-config";
import type {
  JobInstance,
  JobDefinition,
  EdgeCondition,
} from "@/lib/orchestrator-model";
import { todayOrderDate, EDGE_CONDITION_DEFAULT, TEAMS } from "@/lib/orchestrator-model";
import {
  getTodayInstances,
  onInstanceChange,
  holdInstance,
  releaseInstance,
  cancelInstance,
  rerunInstance,
  skipInstance,
  bypassInstance,
  forceInstance,
} from "@/lib/runtime-bridge";
import {
  loadDefinitions,
  onDefinitionsChange,
  getDefinitions,
  saveDefinition,
  deleteDefinition,
  reloadDefinitions,
} from "@/lib/definition-store";
import {
  runDaily,
  startScheduler,
  stopScheduler,
  updateSchedulerDefs,
  getLastDailyRun,
} from "@/lib/runtime-bridge";
import { container } from "@/lib/container";
import { onServerEvent, isServerMode, onAuthEvent, setAuthToken, SERVER_URL } from "@/lib/server-client";
import { fetchMe, loadCachedUser, type AuthUser } from "@/lib/auth-api";
import { LoginForm } from "./LoginForm";
import { UserMenu } from "./UserMenu";
import { UsersDialog } from "./UsersDialog";
import { ControlMPanel } from "./ControlMPanel";
import { AlertsPanel } from "./AlertsPanel";
import { setAlertNotifier } from "@/lib/alerting";
import { fetchUnacknowledgedCount } from "@/lib/alerts-api";
import { SettingsDialog } from "./SettingsDialog";
import { GitStatusBadge } from "./GitStatusBadge";
import { PRBannerHost } from "./PRBannerHost";
import { PublishButton } from "./PublishButton";
import { getDesignSessionId, setDesignSessionId, onDesignSessionChange, onDesignSessionConflict } from "@/lib/server-client";
import { getDesignSession, getDesignSessionStatus, bulkSessionDefinitions, createDesignSession, openSessionFolder, createSessionFolder, type SessionStatus, type PublishResult } from "@/lib/design-session-api";
import { toast, ToastHost } from "./Toast";
import EdgeConditionModal from "./EdgeConditionModal";
import { getGitInfo, commitUrl } from "@/lib/git-info";
import { FolderOpen, Play, Zap, GitCommitHorizontal, GitCompare, FlaskConical, LayoutGrid, ChevronLeft, ChevronRight } from "lucide-react";

import "@xyflow/react/dist/style.css";
import "@/index.css";
import "./tokens.css";

type Mode = "design" | "monitoring";

/* ──────────────────────────────────────────────────────────────
   Constantes de layout (folders como colunas — Control-M style)
   ────────────────────────────────────────────────────────────── */

const NODE_W = 220;
const NODE_H = 72;
// Âncora vertical de entrada do canvas: o topo do conteúdo fica este tanto abaixo
// do topo da área (px de tela). Um pouco mais baixo que a trava antiga (24) — mesma
// sensação do "Organizar". Vale pro Monitoring e pro Design.
const TOP_ANCHOR = 88;
// Folga do translateExtent ACIMA do conteúdo, em px de MUNDO. O extent é estático
// (não conhece o zoom), então precisa cobrir a âncora de TOP_ANCHOR px de TELA no
// pior caso: TOP_ANCHOR / minZoom (0.5 do ReactFlow) = 176. Com menos que isso, a
// âncora/centralização deixava a câmera FORA do extent e o primeiro pan "pulava"
// pra posição travada. Todo movimento programático clampa por este mesmo limite
// (clampTy), então câmera e trava nunca divergem.
const PAN_SLACK_TOP = 176;
// Design — margem (px de mundo) ao redor da caixa dos jobs para o limite de pan:
// dá folga pros lados/baixo sem deixar "se perder" no vazio.
const DESIGN_PAN_MARGIN_X = 360;
const DESIGN_PAN_MARGIN_BOTTOM = 480;
const NODE_GAP_Y = 28; // ranksep dagre (dep vertical)
const NODE_GAP_X = 36; // nodesep dagre (jobs paralelos na mesma linha)
const COL_PADDING_X = 24;
// Grade dos jobs SOLTOS (sem dependência interna). Configurável em Settings (Fase 2).
const LAYOUT_COLUMNS = 10; // colunas-base da grade (cap soft)
const LAYOUT_MAX_ROWS = 30; // ao passar disso, alarga colunas em vez de crescer pra baixo
type LayoutConfig = { columns: number; maxRows: number };
const DEFAULT_LAYOUT: LayoutConfig = { columns: LAYOUT_COLUMNS, maxRows: LAYOUT_MAX_ROWS };

// readLayoutConfig — lê a config de layout do localStorage (default 10/30), com
// clamp sensato. Mesma estratégia do toggle do minimap (pref de visão por browser).
function readLayoutConfig(): LayoutConfig {
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

function instanceToMonitoring(inst: JobInstance): MonitoringJob {
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

interface Canvas { nodes: Node[]; edges: Edge[]; lanes: LaneInfo[] }
interface LaneInfo { team: string; x: number; y: number; width: number; height: number; count: number }

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

// Minimap próprio: desenha um ponto por job a partir de canvas.nodes (que têm posição
// garantida), sem depender do MiniMap do ReactFlow (que filtra nós custom sem dimensão
// medida — motivo de os jobs nunca aparecerem). Clique navega o canvas até o ponto.
function NavMinimap({ nodes, width, height, onNavigate }: {
  nodes: Node[];
  width: number;
  height: number;
  // Navegação clamp-aware do pai (focusOnPoint) — setCenter cru deixaria a câmera
  // fora do translateExtent ao clicar perto do topo (pulo no próximo pan).
  onNavigate: (fx: number, fy: number, zoom: number) => void;
}) {
  // Viewport ao vivo (transform + tamanho do canvas) p/ desenhar o retângulo da
  // área visível — é o que "leva em consideração o alinhamento da tela".
  const tx = useStore((s) => s.transform[0]);
  const ty = useStore((s) => s.transform[1]);
  const tzoom = useStore((s) => s.transform[2]);
  const vpW = useStore((s) => s.width);
  const vpH = useStore((s) => s.height);

  // Mostra APENAS os jobs (ignora lanes/containers e outros nós).
  const jobs = nodes.filter((n) => n.type === "jobV2");
  if (jobs.length === 0) {
    return <div style={{ width, height, display: "grid", placeItems: "center", fontSize: 11, color: "var(--v2-text-muted)" }}>sem jobs</div>;
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
  const toX = (fx: number) => offX + (fx - minX) * scale;
  const toY = (fy: number) => offY + (fy - minY) * scale;
  // Área visível atual (em coords de fluxo) só p/ o retângulo do viewport — clipado
  // pela caixa do minimap quando o usuário navega além dos jobs.
  const viewMinX = -tx / tzoom, viewMinY = -ty / tzoom;
  const viewMaxX = (vpW - tx) / tzoom, viewMaxY = (vpH - ty) / tzoom;
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
      {jobs.map((n) => (
        <rect
          key={n.id}
          x={toX(n.position.x)}
          y={toY(n.position.y)}
          width={sw}
          height={sh}
          rx={1}
          fill={miniNodeColor(n)}
          stroke="#06080c"
          strokeWidth={0.5}
        />
      ))}
      {/* Retângulo do viewport — mostra onde a tela está sobre o conteúdo. */}
      <rect
        x={toX(viewMinX)}
        y={toY(viewMinY)}
        width={(viewMaxX - viewMinX) * scale}
        height={(viewMaxY - viewMinY) * scale}
        fill="rgba(255,255,255,0.05)"
        stroke="var(--v2-accent-brand)"
        strokeWidth={1}
        rx={2}
        pointerEvents="none"
      />
    </svg>
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

function buildMonitoringCanvas(rawInstances: JobInstance[], defs: JobDefinition[], cfg: LayoutConfig = DEFAULT_LAYOUT): Canvas {
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

  const edges: Edge[] = [];
  const rawEdges: Array<{ source: string; target: string }> = [];
  for (const inst of instances) {
    const def = defsById.get(inst.definitionId);
    if (!def?.upstream?.length) continue;
    for (const u of def.upstream) {
      const parent = instByDefId.get(u.from);
      if (!parent) continue;
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
      data: {
        label: inst.label,
        jobType: inst.jobType,
        status: INSTANCE_TO_UI_STATUS[inst.status],
        team: inst.team,
        lastRun: inst.startedAt ? fmtHm(inst.startedAt) : undefined,
        mode: "monitoring",
        forced: inst.manual,
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

function buildDesignCanvas(defs: JobDefinition[], cfg: LayoutConfig = DEFAULT_LAYOUT): Canvas {
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

/* ──────────────────────────────────────────────────────────────
   Inner component (tem acesso a useReactFlow)
   ────────────────────────────────────────────────────────────── */

function V2PreviewInner() {
  const [mode, setMode] = useState<Mode>("monitoring");
  // P3/escala — ViewPoint server-driven (paginado/virtualizado), p/ 100k–1M jobs.
  const [scaleView, setScaleView] = useState(false);
  const [showDiff, setShowDiff] = useState(false);
  const [showDryRun, setShowDryRun] = useState(false);
  const [instances, setInstances] = useState<JobInstance[]>([]);
  const [defs, setDefs] = useState<JobDefinition[]>([]);
  const [selectedInstanceId, setSelectedInstanceId] = useState<string | null>(null);
  const [editingDef, setEditingDef] = useState<{ def: JobDefinition; isNew: boolean } | null>(null);
  const [lastDaily, setLastDaily] = useState<string | null>(getLastDailyRun());
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; items: ContextMenuItem[] } | null>(null);
  // F11.8 — visibleFolders: null = all visible. Persisted in localStorage.
  const [visibleFolders, setVisibleFolders] = useState<Set<string> | null>(() => {
    if (typeof window === "undefined") return null;
    try {
      const raw = window.localStorage.getItem("regente:visibleFolders");
      if (!raw) return null;
      const arr = JSON.parse(raw) as string[] | null;
      return arr === null ? null : new Set(arr);
    } catch { return null; }
  });
  const [showFolderManager, setShowFolderManager] = useState(false);
  // F11.10 — auth state
  const [me, setMe] = useState<AuthUser | null>(() => loadCachedUser());
  const [authChecked, setAuthChecked] = useState<boolean>(!isServerMode());
  const [showUsers, setShowUsers] = useState(false);
  const [showControlM, setShowControlM] = useState(false);
  const [showAlerts, setShowAlerts] = useState(false);
  // Minimap de navegação — protótipo opt-in (Settings → Geral), default off.
  const [showMinimap, setShowMinimap] = useState<boolean>(() => typeof window !== "undefined" && window.localStorage.getItem("regente:minimap") === "1");
  const [miniSize, setMiniSize] = useState<{ w: number; h: number }>({ w: 260, h: 168 });
  useEffect(() => {
    const sync = () => setShowMinimap(window.localStorage.getItem("regente:minimap") === "1");
    window.addEventListener("regente:minimap-changed", sync);
    return () => window.removeEventListener("regente:minimap-changed", sync);
  }, []);
  // Config da grade de jobs soltos (colunas / max linhas). Pref de visão por browser,
  // editável em Settings; re-layouta o canvas quando muda (evento regente:layout-changed).
  const [layoutCfg, setLayoutCfg] = useState<LayoutConfig>(() => readLayoutConfig());
  useEffect(() => {
    const sync = () => setLayoutCfg(readLayoutConfig());
    window.addEventListener("regente:layout-changed", sync);
    return () => window.removeEventListener("regente:layout-changed", sync);
  }, []);
  const [unreadAlerts, setUnreadAlerts] = useState<number>(0);

  // === Design sessions (Etapa 3+4+5, 2026-04-26) ===
  // sessionId === null → picker de folders (FolderManagerDialog) quando entrar em Design.
  // sessionId !== null → habilita PublishButton, e a UI de Design opera no clone.
  const [designSessionId, setDesignSessionIdState] = useState<string | null>(getDesignSessionId());
  const [designSessionNewFolders, setDesignSessionNewFolders] = useState<string[]>([]);
  // P2 (2026-04-26): folders no escopo da session (folders ∪ newFolders).
  // null = sem session (nenhum filtro aplicado por session); Set vazio é estado
  // transitório enquanto carrega — também não filtra (segurança contra esconder tudo).
  const [activeFolders, setActiveFolders] = useState<Set<string> | null>(null);
  // P8 (2026-04-26): drift do clone da session vs origin/<branch>.
  const [sessionStatus, setSessionStatus] = useState<SessionStatus | null>(null);
  useEffect(() => onDesignSessionChange((sid) => setDesignSessionIdState(sid)), []);
  // P7 (2026-04-26): outra aba assumiu a mesma session → libera essa aba.
  useEffect(() =>
    onDesignSessionConflict((sid) => {
      toast.error("Outra aba assumiu esta session", {
        detail: `${sid.slice(0, 16)}… — esta aba foi desconectada para evitar perda de edições.`,
      });
      setDesignSessionId(null);
      setDesignSessionNewFolders([]);
    }),
  []);
  // P2: quando entra em session, busca detalhes e popula sessionFolders.
  useEffect(() => {
    if (!designSessionId) {
      setActiveFolders(null);
      setDesignSessionNewFolders([]);
      return;
    }
    let cancel = false;
    getDesignSession(designSessionId)
      .then((s) => {
        if (cancel) return;
        const all = [...(s.folders ?? []), ...(s.newFolders ?? [])];
        setActiveFolders(new Set(all));
        setDesignSessionNewFolders(s.newFolders ?? []);
      })
      .catch(() => {
        if (cancel) return;
        // Falhou (404 = session expirou). Limpa para não filtrar erradamente.
        setActiveFolders(null);
        setDesignSessionNewFolders([]);
      });
    return () => { cancel = true; };
  }, [designSessionId]);
  // P8: polling 30s do drift status enquanto session ativa.
  useEffect(() => {
    if (!designSessionId) { setSessionStatus(null); return; }
    let cancel = false;
    const tick = () => {
      getDesignSessionStatus(designSessionId)
        .then((s) => { if (!cancel) setSessionStatus(s); })
        .catch(() => { if (!cancel) setSessionStatus(null); });
    };
    tick();
    const id = window.setInterval(tick, 30_000);
    return () => { cancel = true; window.clearInterval(id); };
  }, [designSessionId]);
  // F20 — environment label (visual tag)
  const [envLabel, setEnvLabel] = useState<string>("");
  const [showSettings, setShowSettings] = useState(false);
  // F11.9 — multi-selection no canvas (ReactFlow nativo via Shift+click / drag rect)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const rfInstance = useRef<ReactFlowInstance | null>(null);
  const { getViewport, setViewport } = useReactFlow();
  const storeApi = useStoreApi();

  const nodeTypes = useMemo(() => ({ jobV2: JobNodeV2, laneLabel: LaneLabelNode }), []);

  /* ── Mount: load definitions + subscribe ── */
  useEffect(() => {
    const serverMode = container.storageBackend === "server";

    if (!serverMode) {
      // One-shot migration: limpa os 15 fakes do seed v1 que ficaram no
      // localStorage em sessões anteriores. Qualquer instance hoje que
      // venha de uma definition inexistente é órfã e deve sumir.
      if (typeof window !== "undefined") {
        const oldSeedFlag = window.localStorage.getItem("regente:v2-seeded:v1");
        if (oldSeedFlag) {
          window.localStorage.removeItem("regente:instances");
          window.localStorage.removeItem("regente:v2-seeded:v1");
          window.localStorage.removeItem("regente:daily-run-at");
        }
      }
    }

    void loadDefinitions().then((list) => {
      setDefs(list);
      // Purga instances órfãs (sem definition correspondente) — só local mode.
      if (!serverMode && typeof window !== "undefined") {
        const raw = window.localStorage.getItem("regente:instances");
        if (raw) {
          try {
            const arr = JSON.parse(raw) as JobInstance[];
            const ids = new Set(list.map((d) => d.id));
            const cleaned = arr.filter((i) => ids.has(i.definitionId));
            if (cleaned.length !== arr.length) {
              window.localStorage.setItem("regente:instances", JSON.stringify(cleaned));
            }
          } catch { /* ignore */ }
        }
      }
      setInstances(getTodayInstances());
    });
    const unsubDefs = onDefinitionsChange((list) => {
      setDefs([...list]);
      updateSchedulerDefs([...list]);
    });
    setInstances(getTodayInstances());
    // SEM refiltro por todayOrderDate aqui: getTodayInstances já resolve o "hoje"
    // em cada modo (local filtra por data local; server devolve o dia que o SERVER
    // materializou). Refiltrar pela data local do browser zerava o board no primeiro
    // evento WS quando o dia do cliente ≠ dia do server (acesso remoto/virada de dia).
    const unsubInst = onInstanceChange(() => {
      setInstances(getTodayInstances());
    });
    startScheduler(2000);

    // Server mode: subscribe a WS para recarregar defs quando mudarem no server
    let unsubWs: (() => void) | null = null;
    if (isServerMode()) {
      unsubWs = onServerEvent((ev) => {
        // WS (re)conectou — inclui o pós-login (setAuthToken reconecta com o token
        // novo). Recarrega defs que podem ter falhado no mount (401) ou mudado
        // enquanto o canal esteve fora. Instances ressincronizam no próprio store.
        if (ev.event === "_connected") {
          void reloadDefinitions().then((list) => setDefs([...list]));
          return;
        }
        if (ev.event === "definition.changed" || ev.event === "definition.deleted") {
          void reloadDefinitions().then((list) => setDefs([...list]));
          // Loop GitHub→UI fechado: mudança veio do webhook (push/PR merged no
          // GitHub) → avisa o usuário que as caixinhas mudaram sozinhas.
          const payload = (ev.payload ?? {}) as { reason?: string; sha?: string };
          if (payload.reason === "git-webhook") {
            toast.info("Workspace atualizado via GitHub", {
              detail: payload.sha ? `main agora em ${payload.sha}` : "novo commit no main",
            });
          }
        }
        // F11.8 — folder.changed: foldermanager já faz refresh interno; aqui só
        // garantimos que defs sigam coerentes (rename/delete podem ter movido jobs).
        if (ev.event === "folder.changed") {
          void reloadDefinitions().then((list) => setDefs([...list]));
        }
        // Phase 8 — alerta disparado no server: toast + atualiza o badge.
        if (ev.event === "alert.fired") {
          const p = (ev.payload ?? {}) as { ruleName?: string; message?: string; severity?: string };
          const title = p.ruleName ?? "Alerta";
          if (p.severity === "critical" || p.severity === "warning") {
            toast.error(title, { detail: p.message });
          } else {
            toast.info(title, { detail: p.message });
          }
          void fetchUnacknowledgedCount().then(setUnreadAlerts);
        }
        // Ciclo de vida: alertas tratados (rerun/set-ok) no server → atualiza o badge.
        if (ev.event === "alert.changed") {
          void fetchUnacknowledgedCount().then(setUnreadAlerts);
        }
      });
    }

    return () => {
      unsubDefs();
      unsubInst();
      if (unsubWs) unsubWs();
      stopScheduler();
    };
  }, []);

  // Mantém scheduler com defs atuais
  useEffect(() => { updateSchedulerDefs(defs); }, [defs]);

  // Alerting (Phase 8) — surface fired alerts as toasts and keep the topbar
  // badge in sync. In local mode the notifier is invoked from instance-store;
  // in server mode the "alert.fired" WS event (handled in the mount effect)
  // drives the toast. Both paths refresh the unread badge.
  useEffect(() => {
    setAlertNotifier((event) => {
      const opts = { detail: event.message };
      if (event.severity === "critical" || event.severity === "warning") {
        toast.error(event.ruleName, opts);
      } else {
        toast.info(event.ruleName, opts);
      }
      void fetchUnacknowledgedCount().then(setUnreadAlerts);
    });
    return () => setAlertNotifier(null);
  }, []);

  // Recompute unread badge on instance changes (alerts fire during updates)
  // and whenever the panel toggles (acknowledge happens inside it).
  useEffect(() => {
    void fetchUnacknowledgedCount().then(setUnreadAlerts);
  }, [instances, showAlerts]);

  // F11.8 — persist visibleFolders
  useEffect(() => {
    if (typeof window === "undefined") return;
    if (visibleFolders === null) {
      window.localStorage.removeItem("regente:visibleFolders");
    } else {
      window.localStorage.setItem("regente:visibleFolders", JSON.stringify([...visibleFolders]));
    }
  }, [visibleFolders]);

  // F11.10 — resolve /me on mount + handle 401 events
  useEffect(() => {
    if (!isServerMode()) { setAuthChecked(true); return; }
    let cancel = false;

    // F20 — fetch env label (public endpoint, no auth)
    if (SERVER_URL) {
      fetch(`${SERVER_URL}/api/env`)
        .then((r) => r.ok ? r.json() : null)
        .then((data) => { if (!cancel && data?.label) setEnvLabel(data.label); })
        .catch(() => {});
    }

    (async () => {
      const u = await fetchMe();
      if (cancel) return;
      setMe(u);
      setAuthChecked(true);
    })();
    const off = onAuthEvent((ev) => {
      if (ev === "unauthorized") {
        setAuthToken(null);
        setMe(null);
      }
    });
    return () => { cancel = true; off(); };
  }, []);

  // Re-alinhamento Design (Fase 1, 2026-04-27):
  // `activeFolders` é o conjunto de folders abertas para trabalho (multi-select).
  // - Em design: filtro = visibleFolders ∩ activeFolders. Sem activeFolders, NADA é mostrado.
  // - Em monitoring: filtro = visibleFolders apenas (activeFolders ignorado, ver tudo).
  const hasActiveFolders = activeFolders !== null && activeFolders.size > 0;
  const effectiveFolders = useMemo<Set<string> | null>(() => {
    if (mode === "design") {
      // Design SEMPRE filtra por activeFolders. Sem folder ativa = empty set (nada).
      if (!hasActiveFolders) return new Set<string>();
      if (visibleFolders === null) return activeFolders;
      const inter = new Set<string>();
      for (const f of activeFolders!) if (visibleFolders.has(f)) inter.add(f);
      return inter;
    }
    // Monitoring: comportamento F11.8 (visibleFolders apenas).
    return visibleFolders;
  }, [mode, hasActiveFolders, activeFolders, visibleFolders]);
  const filteredDefs = useMemo(() => {
    if (effectiveFolders === null) return defs;
    return defs.filter((d) => effectiveFolders.has(d.team ?? ""));
  }, [defs, effectiveFolders]);
  // Draft def: mostra o nó no canvas enquanto a definition nova está sendo
  // configurada no drawer (antes de salvar). Some quando salva (vira real)
  // ou quando o drawer é fechado.
  const designDefsWithDraft = useMemo(() => {
    if (mode !== "design") return filteredDefs;
    if (!editingDef || !editingDef.isNew) return filteredDefs;
    if (filteredDefs.some((d) => d.id === editingDef.def.id)) return filteredDefs;
    return [...filteredDefs, editingDef.def];
  }, [mode, filteredDefs, editingDef]);
  const filteredInstances = useMemo(() => {
    if (effectiveFolders === null) return instances;
    const defsById = new Map(defs.map((d) => [d.id, d] as const));
    return instances.filter((i) => {
      const team = i.team || defsById.get(i.definitionId)?.team || "";
      return effectiveFolders.has(team);
    });
  }, [instances, defs, effectiveFolders]);

  const canvas = useMemo<Canvas>(
    () => (mode === "monitoring" ? buildMonitoringCanvas(filteredInstances, filteredDefs, layoutCfg) : buildDesignCanvas(designDefsWithDraft, layoutCfg)),
    [mode, filteredInstances, filteredDefs, designDefsWithDraft, layoutCfg],
  );

  // Abrir/criar folder no Design — absorvido do antigo FolderOpener pro botão
  // FOLDERS (FolderManagerDialog). Mantém a semântica de design-session: cria a
  // session lazily, abre/cria via API da session, rastreia folders novas pro PR.
  const addFolder = useCallback(async (action: "open" | "create", name: string) => {
    const trimmed = name.trim();
    if (!trimmed) return;
    let sid = designSessionId;
    const creatingSession = !sid;
    if (!sid) {
      const sess = await createDesignSession([], []);
      sid = sess.id;
    }
    // Cria/abre a folder na session ANTES de publicar o sid no global. Assim, quando
    // o effect [designSessionId] refetcha getDesignSession, a folder já existe →
    // sem corrida que zere activeFolders/newFolders (bug do "Publish não apareceu").
    const res = action === "open"
      ? await openSessionFolder(sid, trimmed)
      : await createSessionFolder(sid, trimmed);
    if (creatingSession) setDesignSessionId(sid);
    setActiveFolders((prev) => {
      const next = new Set(prev ?? []);
      next.add(res.name);
      return next;
    });
    if (res.willForcePR) {
      setDesignSessionNewFolders((prev) => prev.includes(res.name) ? prev : [...prev, res.name]);
    }
    await reloadDefinitions();
  }, [designSessionId]);

  // Fechar folder do working set: só remove da visão (não toca no Git nem na session).
  const closeFolder = useCallback((name: string) => {
    setActiveFolders((prev) => {
      if (!prev) return prev;
      const next = new Set(prev);
      next.delete(name);
      return next;
    });
  }, []);

  // Publish/Discard da design-session — extraídos para reuso na topbar E dentro do
  // modal de FOLDERS (o commit precisa estar visível na própria tela de criação).
  const handlePublished = useCallback(async (res: PublishResult) => {
    if (res.mode === "noop") {
      toast.info("Nada a publicar", { detail: "Working tree limpa — faça alguma edição antes." });
      return;
    }
    setDesignSessionId(null);
    setDesignSessionNewFolders([]);
    if (res.prUrl) {
      toast.success(`Publicado como PR #${res.prNumber}`, { linkUrl: res.prUrl, linkLabel: `PR #${res.prNumber} no GitHub` });
    } else {
      const st = await getGitInfo();
      toast.success("Publicado no GitHub", {
        detail: `commit ${res.commitSha?.slice(0, 7)}`,
        linkUrl: commitUrl(st, res.commitSha) ?? undefined,
        linkLabel: res.commitSha ? `ver commit ${res.commitSha.slice(0, 7)}` : undefined,
      });
    }
    await reloadDefinitions();
  }, []);

  const handleDiscardSession = useCallback(async () => {
    if (!window.confirm("Descartar a sessão? Todas as edições não publicadas serão perdidas.")) return;
    try {
      const sid = designSessionId;
      setDesignSessionId(null);
      setDesignSessionNewFolders([]);
      if (sid) {
        const { deleteDesignSession } = await import("@/lib/design-session-api");
        await deleteDesignSession(sid).catch(() => {});
      }
      await reloadDefinitions();
    } catch { /* ignore */ }
  }, [designSessionId]);

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
  // MESMO limite superior do translateExtent. O ReactFlow só clampa pan/zoom do
  // usuário; setViewport/setCenter passam direto — e uma câmera fora do extent
  // "pula" pra posição travada no primeiro pan (o bug da centralização).
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
  // ÚNICA usada tanto na ENTRADA quanto no botão "Organizar" — assim os dois caem
  // exatamente no mesmo limite (era o bug: o botão fazia fitView puro = mais baixo).
  const organizeView = useCallback((duration: number) => {
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

  // Ref sempre com o organizeView mais recente (fecha sobre os canvas.nodes atuais)
  // sem forçar o effect de entrada a rodar a cada re-layout.
  const organizeViewRef = useRef(organizeView);
  useEffect(() => { organizeViewRef.current = organizeView; }, [organizeView]);

  // Chave do CONTEXTO de visão: muda quando o usuário troca de modo ou o conjunto
  // de folders muda. NÃO muda quando só chegam updates de dado (status via WS/tick,
  // Run Daily materializando, Force). É o que decide quando reancorar a câmera.
  const viewContextKey = useMemo(() => {
    if (mode === "design") return "design:" + [...(activeFolders ?? [])].sort().join(",");
    return "monitoring:" + (visibleFolders ? [...visibleFolders].sort().join(",") : "*");
  }, [mode, activeFolders, visibleFolders]);

  // Auto-organiza (ancora o topo) SÓ ao entrar num contexto ou quando os jobs
  // aparecem pela 1ª vez. Depende só de [viewContextKey, hasNodes] — churn de dado
  // (Force/Run Daily/WS) NÃO reancora, então a câmera não "pula" nem volta ao
  // centro sozinha, e os jobs não somem exigindo F5.
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

  const monitoringJobs = useMemo(() => {
    const defsById = new Map(defs.map((d) => [d.id, d] as const));
    return filteredInstances.map((inst) => {
      const def = defsById.get(inst.definitionId);
      const enriched: JobInstance = def
        ? {
            ...inst,
            team: inst.team || def.team,
            label: inst.label && inst.label !== inst.definitionId ? inst.label : def.label,
            jobType: inst.jobType || def.jobType,
          }
        : inst;
      return instanceToMonitoring(enriched);
    });
  }, [filteredInstances, defs]);
  const selectedInstance = selectedInstanceId ? instances.find((i) => i.id === selectedInstanceId) : null;

  const statusCounts = useMemo(() => {
    const c = { ok: 0, running: 0, failed: 0, waiting: 0, hold: 0 };
    for (const i of filteredInstances) {
      if (i.status === "OK") c.ok++;
      else if (i.status === "RUNNING") c.running++;
      else if (i.status === "NOTOK") c.failed++;
      else if (i.status === "WAITING") c.waiting++;
      else if (i.status === "HOLD") c.hold++;
    }
    return c;
  }, [filteredInstances]);

  /* ── Sidebar click → centralize node ── */
  const focusNode = useCallback((nodeId: string) => {
    const node = canvas.nodes.find((n) => n.id === nodeId);
    if (!node) return;
    const px = node.position.x + NODE_W / 2;
    const py = node.position.y + NODE_H / 2;
    focusOnPoint(px, py, 1.1);
  }, [canvas.nodes, focusOnPoint]);

  const handleSidebarSelect = useCallback((instId: string) => {
    setSelectedInstanceId(instId);
    focusNode(`m-${instId}`);
  }, [focusNode]);

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

  /* ── Canvas node click ── */
  const onNodeClick: NodeMouseHandler = useCallback((_, node) => {
    if (mode === "monitoring") {
      const id = node.id.replace(/^m-/, "");
      setSelectedInstanceId(id);
    } else {
      const id = node.id.replace(/^d-/, "");
      const def = defs.find((d) => d.id === id);
      if (def) setEditingDef({ def, isNew: false });
    }
  }, [mode, defs]);

  /* ── Drag & drop de palette ── */
  const onDragOver = useCallback((e: React.DragEvent) => {
    if (mode !== "design") return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "copy";
  }, [mode]);

  const onDrop = useCallback((e: React.DragEvent) => {
    if (mode !== "design") return;
    e.preventDefault();
    const type = e.dataTransfer.getData("application/regente-jobtype") as JobType;
    if (!type) return;
    const rf = rfInstance.current;
    if (!rf) return;
    // Posição do drop é ignorada — o canvas organiza por swimlane.
    // Criamos um ID sugerido único.
    const suggestedId = `${type.toLowerCase()}-${Date.now().toString(36).slice(-5)}`;
    // Pré-seleciona folder se houver apenas uma ativa na session.
    const folderList = activeFolders ? Array.from(activeFolders) : [];
    const draftTeam = folderList.length === 1 ? folderList[0] : "";
    const draft: JobDefinition = {
      id: suggestedId,
      label: suggestedId,
      jobType: type,
      team: draftTeam,
      schedule: { enabled: true, frequency: "daily", runAt: "06:00" },
      retries: 2,
      timeout: 300,
    };
    setEditingDef({ def: draft, isNew: true });
  }, [mode, activeFolders]);

  /* ── onConnect (Fase 8: edges com condição) ──
     window.prompt substituído por EdgeConditionModal (2026-06-12). */
  const [pendingConn, setPendingConn] = useState<{ fromId: string; toId: string } | null>(null);
  const onConnect: OnConnect = useCallback((conn: Connection) => {
    if (mode !== "design") return;
    if (!conn.source || !conn.target || conn.source === conn.target) return;
    setPendingConn({
      fromId: conn.source.replace(/^d-/, ""),
      toId: conn.target.replace(/^d-/, ""),
    });
  }, [mode]);

  const confirmConnection = useCallback((condition: EdgeCondition) => {
    if (!pendingConn) return;
    const { fromId, toId } = pendingConn;
    setPendingConn(null);
    const target = defs.find((d) => d.id === toId);
    if (!target) return;
    const up = target.upstream ?? [];
    // remove aresta prévia do mesmo `from` para evitar duplicatas
    const next = [...up.filter((u) => u.from !== fromId), { from: fromId, condition }];
    const updated: JobDefinition = { ...target, upstream: next };
    void saveDefinition(updated).catch((e) => {
      toast.error("Falha ao salvar dependência", { detail: e instanceof Error ? e.message : String(e) });
    });
  }, [pendingConn, defs]);

  /* ── Save/Delete definition ── */
  const handleSaveDef = useCallback(async (def: JobDefinition) => {
    const wasNew = editingDef?.isNew ?? false;
    await saveDefinition(def);
    setEditingDef(null);
    // Job NOVO: reenquadra pra garantir que ele (e os já existentes) fiquem visíveis.
    // Sem isso, com zoom/pan o nó recém-criado pode nascer fora da tela e "sumir".
    // organizeView (e não fitView cru): fitView centraliza vertical = câmera fora
    // do extent = pulo no próximo pan.
    if (wasNew) setTimeout(() => organizeViewRef.current(300), 200);
  }, [editingDef]);
  const handleDeleteDef = useCallback(async (id: string) => {
    await deleteDefinition(id);
    // também remove referências upstream em outras definitions
    for (const d of getDefinitions()) {
      if (d.upstream?.some((u) => u.from === id)) {
        await saveDefinition({ ...d, upstream: d.upstream.filter((u) => u.from !== id) });
      }
    }
    setEditingDef(null);
  }, []);

  /* ── Run Daily ── */
  const handleRunDaily = useCallback(() => {
    const created = runDaily(defs);
    setLastDaily(new Date().toISOString());
    // NÃO reenquadramos aqui: se o board estava vazio, o gate de entrada ancora os
    // jobs quando eles aparecem (0→N); se já havia jobs, a câmera fica onde está
    // (antes um fitView aqui fazia os jobs "sumirem" e exigia F5).
    if (container.storageBackend === "server") {
      // server mode: refresh é assíncrono via WS; UI re-renderiza sozinha
      return;
    }
    if (created.length > 0) {
      setInstances(getTodayInstances());
    } else {
      toast.info("Nenhuma definition elegível", {
        detail: "Sem schedule habilitado ou instances já materializadas hoje.",
      });
    }
  }, [defs]);

  const handleRerunInstance = useCallback((id: string) => {
    Promise.resolve(rerunInstance(id)).then((fresh) => {
      if (fresh) setSelectedInstanceId(fresh.id);
    });
  }, []);

  /* ── F11.9 Bulk handlers ── */
  const clearSelection = useCallback(() => setSelectedIds(new Set()), []);

  // Stable ref required: ReactFlow v12 has onSelectionChange in its effect deps →
  // an inline arrow would create a new reference every render and loop infinitely.
  const handleSelectionChange = useCallback(({ nodes: sel }: { nodes: Node[] }) => {
    setSelectedIds((prev) => {
      const ids = new Set<string>();
      for (const n of sel) {
        if (n.type === "laneLabel") continue;
        ids.add(n.id.replace(/^[md]-/, ""));
      }
      if (ids.size === prev.size && [...ids].every((id) => prev.has(id))) return prev;
      return ids;
    });
  }, []);

  const handleBulk = useCallback(
    async (ids: string[], op: (id: string) => Promise<unknown> | unknown, label = "ação") => {
      const results = await Promise.allSettled(ids.map((id) => Promise.resolve(op(id))));
      const failed = results.filter((r) => r.status === "rejected");
      if (failed.length > 0) {
        console.warn(`[bulk] ${failed.length}/${ids.length} failed`, failed);
        toast.error(`Bulk ${label}: ${failed.length} de ${ids.length} falharam`, {
          detail: "Detalhes no console do navegador.",
        });
      } else {
        toast.success(`Bulk ${label}: ${ids.length} instance${ids.length === 1 ? "" : "s"} ok`);
      }
      clearSelection();
    },
    [clearSelection],
  );

  // F11.8 — bulk move de definitions para outra folder (via session bulk endpoint).
  const handleBulkMoveDefs = useCallback(
    async (ids: string[], targetFolder: string) => {
      if (!designSessionId) {
        toast.error("Mover em lote requer uma session de design ativa");
        return;
      }
      try {
        const res = await bulkSessionDefinitions(designSessionId, "move-folder", ids, { targetFolder });
        if (res.failed > 0) {
          const firstErr = res.results.find((r) => !r.ok)?.error ?? "";
          toast.error(`Move: ${res.failed} de ${res.total} falharam`, { detail: firstErr });
        } else {
          toast.success(`${res.ok} job${res.ok === 1 ? "" : "s"} movido${res.ok === 1 ? "" : "s"} para ${targetFolder}`);
        }
        await reloadDefinitions().then((list) => setDefs([...list]));
      } catch (e) {
        toast.error("Bulk move falhou", { detail: e instanceof Error ? e.message : String(e) });
      }
      clearSelection();
    },
    [designSessionId, clearSelection],
  );

  const handleBulkDeleteDefs = useCallback(
    async (ids: string[]) => {
      // delete each def + remove upstream refs
      const allDefs = getDefinitions();
      for (const id of ids) {
        try { await deleteDefinition(id); } catch (e) { console.error("[bulk delete]", id, e); }
      }
      for (const d of allDefs) {
        if (d.upstream?.some((u) => ids.includes(u.from))) {
          try {
            await saveDefinition({ ...d, upstream: d.upstream.filter((u) => !ids.includes(u.from)) });
          } catch (e) { console.error("[bulk delete upstream cleanup]", d.id, e); }
        }
      }
      clearSelection();
    },
    [clearSelection],
  );

  // ESC clears selection (ReactFlow doesn't do this by default)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && selectedIds.size > 0) {
        clearSelection();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [selectedIds.size, clearSelection]);

  /* ── Force Order (Run Now) ── */
  const [forceMenuOpen, setForceMenuOpen] = useState(false);
  const handleForce = useCallback((def: JobDefinition) => {
    setForceMenuOpen(false);
    Promise.resolve(forceInstance(def)).then((fresh) => {
      if (fresh) {
        setSelectedInstanceId(fresh.id);
        // Direciona a câmera pro job recém-forçado quando ele materializar (não
        // reenquadra o board inteiro — só centraliza no novo, mantendo o zoom).
        setPendingFocusId(fresh.id);
      }
    }).catch((err) => {
      console.error("[force] failed", err);
      toast.error("Force Order falhou", { detail: err?.message ?? String(err) });
    });
  }, []);

  /* ── Context menu (right-click no canvas) ── */
  const onNodeContextMenu = useCallback(
    (e: React.MouseEvent, node: Node) => {
      e.preventDefault();

      // Design mode: menu por definition
      if (mode === "design") {
        const id = node.id.replace(/^d-/, "");
        const def = defs.find((d) => d.id === id);
        if (!def) return;
        const items: ContextMenuItem[] = [
          { label: "Run Now", tone: "primary", onClick: () => handleForce(def) },
          { label: "Edit",                onClick: () => setEditingDef({ def, isNew: false }) },
          {
            label: "Duplicate",
            onClick: () => {
              // Clona a definition com novo id/label; abre o drawer como NEW
              // (gesto Control-M: criar a partir de um existente).
              const suffix = Date.now().toString(36).slice(-4);
              const clone: JobDefinition = {
                ...def,
                id: `${def.id}-copy-${suffix}`,
                label: `${def.label} (copy)`,
                upstream: def.upstream ? [...def.upstream] : undefined,
              };
              setEditingDef({ def: clone, isNew: true });
            },
          },
          { label: "Delete", tone: "danger", onClick: () => { void handleDeleteDef(def.id); } },
        ];
        setCtxMenu({ x: e.clientX, y: e.clientY, items });
        return;
      }

      // Monitoring mode: menu por instance
      const id = node.id.replace(/^m-/, "");
      const inst = instances.find((i) => i.id === id);
      if (!inst) return;
      const def = defs.find((d) => d.id === inst.definitionId);
      const status = inst.status;

      const items: ContextMenuItem[] = [];

      // Run Now: para WAITING/HOLD (cria nova force order da mesma def)
      if ((status === "WAITING" || status === "HOLD") && def) {
        items.push({ label: "Run Now", tone: "primary", onClick: () => handleForce(def) });
      }

      // Hold / Release / Cancel
      if (status === "WAITING") {
        items.push({ label: "Hold",   onClick: () => { void holdInstance(inst.id); } });
      }
      if (status === "HOLD") {
        items.push({ label: "Release", tone: "primary", onClick: () => { void releaseInstance(inst.id); } });
      }
      if (status === "WAITING" || status === "HOLD") {
        items.push({ label: "Cancel", tone: "danger", onClick: () => { void cancelInstance(inst.id); } });
      }

      // Set OK: NOTOK ou CANCELLED
      if (status === "NOTOK" || status === "CANCELLED") {
        items.push({ label: "Set OK", tone: "primary", onClick: () => { void bypassInstance(inst.id); } });
      }

      // Rerun: OK ou NOTOK (ou CANCELLED)
      if (status === "OK" || status === "NOTOK" || status === "CANCELLED") {
        items.push({ label: "Rerun", onClick: () => handleRerunInstance(inst.id) });
      }

      // View Output: sempre que houver run (started_at existe)
      if (inst.startedAt) {
        items.push({
          label: "View Output",
          onClick: () => setSelectedInstanceId(inst.id),
        });
      }

      setCtxMenu({ x: e.clientX, y: e.clientY, items });
    },
    [mode, defs, instances, handleForce, handleDeleteDef, handleRerunInstance],
  );

  const hasDefs = defs.length > 0;
  const hasInstances = instances.length > 0;

  // F11.10 — gate render: only show login overlay in server mode after we know
  // there is no user. Local mode skips auth entirely.
  if (isServerMode() && authChecked && !me) {
    return <LoginForm onLogin={(u) => setMe(u)} />;
  }

  return (
    <div
      className="v2-root"
      style={{
        height: "100vh",
        display: "flex",
        flexDirection: "column",
        background: "var(--v2-bg-canvas)",
      }}
    >
      {/* Topbar */}
      <header
        style={{
          margin: "10px 12px 2px",
          height: 58,
          padding: "0 18px",
          borderRadius: 16,
          border: "1px solid var(--v2-border-medium)",
          background: "var(--v2-bg-surface)",
          boxShadow: "0 10px 30px rgba(0,0,0,0.35)",
          display: "flex",
          alignItems: "center",
          gap: 14,
          flexShrink: 0,
          // z-index acima do canvas para dropdowns (UserMenu) escaparem do stacking context.
          position: "relative",
          zIndex: 50,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <img
            src="/logo-r.png"
            alt="Regente"
            height={30}
            style={{ height: 30, width: "auto", objectFit: "contain", display: "block" }}
          />
          <span style={{ fontSize: 20, fontWeight: 500, letterSpacing: "-0.01em", color: "var(--v2-text-primary)" }}>Regente</span>
          {envLabel && (
            <span style={{
              fontSize: 10, fontWeight: 700, letterSpacing: "0.04em",
              padding: "2px 7px", borderRadius: 3,
              background: "var(--v2-accent-brand)", color: "#000",
              lineHeight: 1, textTransform: "uppercase",
            }}>{envLabel}</span>
          )}
        </div>

        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <ChevronLeft size={16} style={{ color: "var(--v2-text-muted)" }} />
          <div
            style={{
              display: "flex", gap: 4, padding: 4,
              background: "var(--v2-bg-elevated)",
              border: "1px solid var(--v2-border-subtle)", borderRadius: 12,
            }}
          >
            {(["design", "monitoring"] as const).map((m) => (
              <button
                key={m}
                onClick={() => { setMode(m); setSelectedInstanceId(null); setEditingDef(null); }}
                style={{
                  padding: "6px 16px",
                  background: mode === m ? "var(--v2-accent-brand)" : "transparent",
                  border: "none", borderRadius: 8,
                  color: mode === m ? "var(--v2-bg-canvas)" : "var(--v2-text-secondary)",
                  fontSize: 11, fontFamily: "var(--v2-font-mono)",
                  letterSpacing: "0.06em", cursor: "pointer",
                  fontWeight: mode === m ? 700 : 500, textTransform: "uppercase",
                  transition: "background 140ms, color 140ms",
                }}
              >{m}</button>
            ))}
          </div>
          <ChevronRight size={16} style={{ color: "var(--v2-text-muted)" }} />
        </div>

        {/* F11.8 — Folders picker button (apenas em design) */}
        {mode === "design" && (
        <button
          onClick={() => setShowFolderManager(true)}
          title="Manage folders / filter visible folders"
          style={{
            padding: "5px 10px",
            background: visibleFolders !== null ? "var(--v2-accent-deep)" : "transparent",
            border: `1px solid ${visibleFolders !== null ? "var(--v2-accent-brand)" : "var(--v2-border-medium)"}`,
            color: visibleFolders !== null ? "var(--v2-accent-brand)" : "var(--v2-text-secondary)",
            borderRadius: 3,
            fontSize: 10, fontFamily: "var(--v2-font-mono)",
            letterSpacing: "0.06em", textTransform: "uppercase",
            cursor: "pointer", fontWeight: 600,
            display: "flex", alignItems: "center", gap: 6,
          }}
        >
          <FolderOpen size={12} />
          <span>Folders</span>
          {visibleFolders !== null && (
            <span style={{
              padding: "0 5px", background: "var(--v2-accent-brand)", color: "#000",
              borderRadius: 2, fontSize: 9, fontWeight: 700,
            }}>{visibleFolders.size}</span>
          )}
        </button>
        )}

        {mode === "monitoring" && (
          <>
            <button
              onClick={handleRunDaily}
              disabled={!hasDefs}
              title={hasDefs ? "Materializa instances de hoje a partir das definitions" : "Crie definitions no Design primeiro"}
              style={{
                padding: "5px 10px",
                background: "transparent",
                border: "1px solid var(--v2-accent-brand)",
                color: hasDefs ? "var(--v2-accent-brand)" : "var(--v2-text-muted)",
                borderColor: hasDefs ? "var(--v2-accent-brand)" : "var(--v2-border-medium)",
                borderRadius: 3,
                fontSize: 10, fontFamily: "var(--v2-font-mono)",
                letterSpacing: "0.06em", textTransform: "uppercase",
                cursor: hasDefs ? "pointer" : "not-allowed", fontWeight: 600,
                display: "flex", alignItems: "center", gap: 6,
              }}
            >
              <Play size={11} /> Run Daily
            </button>

            <button
              onClick={() => setScaleView((v) => !v)}
              title="ViewPoint server-driven — paginado/virtualizado, aguenta 100k–1M jobs/dia"
              style={{
                padding: "5px 10px",
                background: scaleView ? "var(--v2-accent-deep)" : "transparent",
                border: `1px solid ${scaleView ? "var(--v2-accent-brand)" : "var(--v2-border-medium)"}`,
                color: scaleView ? "var(--v2-accent-brand)" : "var(--v2-text-primary)",
                borderRadius: 3,
                fontSize: 10, fontFamily: "var(--v2-font-mono)",
                letterSpacing: "0.06em", textTransform: "uppercase",
                cursor: "pointer", fontWeight: 600,
                display: "flex", alignItems: "center", gap: 6,
              }}
            >
              <Zap size={11} /> ViewPoint
            </button>

            <button
              onClick={() => setShowDiff(true)}
              title="Diff de Daily — o que mudou em relação à diária anterior (jobs +/−, schedule, deps, def)"
              style={{
                padding: "5px 10px",
                background: "transparent",
                border: "1px solid var(--v2-border-medium)",
                color: "var(--v2-text-primary)",
                borderRadius: 3,
                fontSize: 10, fontFamily: "var(--v2-font-mono)",
                letterSpacing: "0.06em", textTransform: "uppercase",
                cursor: "pointer", fontWeight: 600,
                display: "flex", alignItems: "center", gap: 6,
              }}
            >
              <GitCompare size={11} /> Diff
            </button>

            <button
              onClick={() => setShowDryRun(true)}
              title="Dry Run — simular a daily de uma data futura sem materializar (quem roda / espera / nunca dispara)"
              style={{
                padding: "5px 10px",
                background: "transparent",
                border: "1px solid var(--v2-border-medium)",
                color: "var(--v2-text-primary)",
                borderRadius: 3,
                fontSize: 10, fontFamily: "var(--v2-font-mono)",
                letterSpacing: "0.06em", textTransform: "uppercase",
                cursor: "pointer", fontWeight: 600,
                display: "flex", alignItems: "center", gap: 6,
              }}
            >
              <FlaskConical size={11} /> Dry Run
            </button>

            <button
              onClick={() => organizeView(300)}
              title="Organizar — re-enquadra o canvas no mesmo limite da entrada (os jobs já se alinham sozinhos: dependentes em fluxo, soltos em grade)"
              style={{
                padding: "5px 10px",
                background: "transparent",
                border: "1px solid var(--v2-border-medium)",
                color: "var(--v2-text-primary)",
                borderRadius: 3,
                fontSize: 10, fontFamily: "var(--v2-font-mono)",
                letterSpacing: "0.06em", textTransform: "uppercase",
                cursor: "pointer", fontWeight: 600,
                display: "flex", alignItems: "center", gap: 6,
              }}
            >
              <LayoutGrid size={11} /> Organizar
            </button>

            <div style={{ position: "relative" }}>
              <button
                onClick={() => setForceMenuOpen((v) => !v)}
                disabled={!hasDefs}
                title={hasDefs ? "Force Order — criar instance agora (Run Now)" : "Crie definitions no Design primeiro"}
                style={{
                  padding: "5px 10px",
                  background: forceMenuOpen ? "var(--v2-accent-deep)" : "transparent",
                  border: "1px solid var(--v2-border-medium)",
                  color: hasDefs ? "var(--v2-text-primary)" : "var(--v2-text-muted)",
                  borderRadius: 3,
                  fontSize: 10, fontFamily: "var(--v2-font-mono)",
                  letterSpacing: "0.06em", textTransform: "uppercase",
                  cursor: hasDefs ? "pointer" : "not-allowed", fontWeight: 600,
                  display: "flex", alignItems: "center", gap: 6,
                }}
              >
                <Zap size={11} /> Force ▾
              </button>
              {forceMenuOpen && hasDefs && (
                <div
                  style={{
                    position: "absolute",
                    top: "calc(100% + 4px)",
                    left: 0,
                    minWidth: 260,
                    maxHeight: 360,
                    overflowY: "auto",
                    background: "var(--v2-bg-surface)",
                    border: "1px solid var(--v2-border-medium)",
                    borderRadius: 4,
                    boxShadow: "0 6px 20px rgba(0,0,0,0.4)",
                    zIndex: 20,
                    padding: "4px 0",
                  }}
                  onMouseLeave={() => setForceMenuOpen(false)}
                >
                  <div style={{
                    padding: "6px 10px",
                    fontSize: 9,
                    fontFamily: "var(--v2-font-mono)",
                    color: "var(--v2-text-muted)",
                    letterSpacing: "0.08em",
                    textTransform: "uppercase",
                    borderBottom: "1px solid var(--v2-border-subtle)",
                  }}>
                    Order Job — Run Now
                  </div>
                  {defs.map((d) => (
                    <button
                      key={d.id}
                      onClick={() => handleForce(d)}
                      style={{
                        display: "flex",
                        width: "100%",
                        padding: "7px 10px",
                        background: "transparent",
                        border: "none",
                        textAlign: "left",
                        color: "var(--v2-text-primary)",
                        fontSize: 11,
                        fontFamily: "var(--v2-font-sans)",
                        cursor: "pointer",
                        alignItems: "center",
                        gap: 8,
                      }}
                      onMouseEnter={(e) => (e.currentTarget.style.background = "var(--v2-bg-hover)")}
                      onMouseLeave={(e) => (e.currentTarget.style.background = "transparent")}
                    >
                      <span style={{ color: "var(--v2-accent-brand)" }}>▶</span>
                      <span style={{ flex: 1, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
                        {d.label}
                      </span>
                      <span style={{
                        fontSize: 9,
                        fontFamily: "var(--v2-font-mono)",
                        color: "var(--v2-text-muted)",
                        padding: "1px 5px",
                        border: "1px solid var(--v2-border-subtle)",
                        borderRadius: 2,
                      }}>
                        {d.team ?? "—"}
                      </span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          </>
        )}

        <div style={{ flex: 1 }} />

        {/* GitStatusBadge é artefato do Design (ver memory/core/regente-product-model.md). */}
        {mode === "design" && !designSessionId && (
          <GitStatusBadge canSync={!!me && me.role === "admin"} />
        )}
        {/* Abrir/criar folder agora vive 100% no botão FOLDERS (FolderManagerDialog). */}
        {mode === "design" && designSessionId && (
          <>
            <span style={{
              fontSize: 11, fontFamily: "var(--v2-font-mono)",
              color: "var(--v2-text-secondary)", padding: "3px 8px",
              background: "var(--v2-bg-elevated)", borderRadius: 4,
              border: "1px solid var(--v2-border-subtle)",
              display: "inline-flex", alignItems: "center", gap: 6,
            }} title={`session=${designSessionId}`}>
              <GitCommitHorizontal size={12} />
              SESSION • {designSessionId.slice(0, 12)}…
              {designSessionNewFolders.length > 0 && (
                <span style={{ color: "#fa6", marginLeft: 6 }}>+{designSessionNewFolders.length} novos</span>
              )}
            </span>
            {sessionStatus && (sessionStatus.ahead > 0 || sessionStatus.behind > 0) && (
              <span
                style={{
                  fontSize: 10, fontFamily: "var(--v2-font-mono)",
                  color: sessionStatus.behind > 0 ? "#fbb" : "#bfb",
                  padding: "3px 7px",
                  background: sessionStatus.behind > 0 ? "#3b1d1d" : "#1d3b1d",
                  borderRadius: 4,
                  border: `1px solid ${sessionStatus.behind > 0 ? "#533" : "#353"}`,
                }}
                title={
                  sessionStatus.behind > 0
                    ? `main avançou ${sessionStatus.behind} commit(s) desde o clone — publish vai precisar resolver drift`
                    : `${sessionStatus.ahead} commit(s) à frente do main`
                }
              >
                {sessionStatus.ahead > 0 && `↑${sessionStatus.ahead}`}
                {sessionStatus.ahead > 0 && sessionStatus.behind > 0 && " "}
                {sessionStatus.behind > 0 && `↓${sessionStatus.behind}`}
              </span>
            )}
            <PublishButton
              sessionId={designSessionId}
              newFolderCount={designSessionNewFolders.length}
              onPublished={handlePublished}
            />
            <button
              onClick={() => void handleDiscardSession()}
              style={{ background: "transparent", color: "#a66", border: "1px solid #533", padding: "5px 10px", borderRadius: 4, cursor: "pointer", fontSize: 11 }}
            >
              Descartar
            </button>
          </>
        )}

        {me && (
          <UserMenu
            me={me}
            onLogout={() => setMe(null)}
            onOpenUsers={() => setShowUsers(true)}
            onOpenControlM={() => setShowControlM(true)}
            onOpenSettings={() => setShowSettings(true)}
            unreadAlerts={unreadAlerts}
            onOpenAlerts={() => setShowAlerts(true)}
            alertsActive={showAlerts}
          />
        )}

        <div style={{ display: "flex", gap: 14, fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-secondary)", letterSpacing: "0.04em" }}>
          <span><span style={{ color: "var(--v2-status-ok)" }}>●</span> {statusCounts.ok}</span>
          <span><span style={{ color: "var(--v2-status-running)" }}>●</span> {statusCounts.running}</span>
          <span><span style={{ color: "var(--v2-status-failed)" }}>●</span> {statusCounts.failed}</span>
          <span><span style={{ color: "var(--v2-status-waiting)" }}>●</span> {statusCounts.waiting}</span>
          {statusCounts.hold > 0 && <span><span style={{ color: "var(--v2-text-secondary)" }}>●</span> {statusCounts.hold}</span>}
        </div>
      </header>

      {/* Stage */}
      <main
        style={{ flex: 1, position: "relative", minHeight: 0 }}
        onDragOver={onDragOver}
        onDrop={onDrop}
      >
        {/* P3/escala — não monta o canvas legado quando o ViewPoint cobre a tela */}
        {!(mode === "monitoring" && scaleView) && (
        <ReactFlow
          nodes={canvas.nodes}
          edges={canvas.edges}
          nodeTypes={nodeTypes}
          // (sem o prop fitView: o fit inicial embutido do RF aplicava DEPOIS do
          // organizeView de entrada — corrida — e deixava a câmera descentrada/fora
          // do extent. A entrada é 100% do organizeView, no gate 0→N.)
          proOptions={{ hideAttribution: true }}
          nodesDraggable={mode === "design"}
          nodesConnectable={mode === "design"}
          elementsSelectable
          // Pan: left button (default UX). Selection rect: Shift+drag.
          // panOnDrag={[0,1]} cobre left+middle; right (2) fica livre p/ ctx menu.
          panOnDrag={[0, 1]}
          translateExtent={panExtent}
          zoomOnScroll
          selectionOnDrag={false}
          selectionKeyCode="Shift"
          multiSelectionKeyCode={["Shift", "Meta", "Control"]}
          onNodeClick={onNodeClick}
          onNodeContextMenu={onNodeContextMenu}
          onConnect={onConnect}
          onSelectionChange={handleSelectionChange}
          onInit={(inst) => { rfInstance.current = inst; }}
        >
          <Background variant={BackgroundVariant.Dots} gap={18} size={1} color="#1a1a1a" />
        </ReactFlow>
        )}
        {showMinimap && !(mode === "monitoring" && scaleView) && (
          <>
            <div
              style={{
                position: "absolute", zIndex: 10,
                right: mode === "monitoring" && selectedInstance ? 392 : 16, bottom: 16,
                width: miniSize.w, height: miniSize.h,
                background: "var(--v2-bg-surface)",
                border: "1px solid var(--v2-border-medium)",
                borderRadius: 8, overflow: "hidden",
                boxShadow: "0 6px 20px rgba(0,0,0,0.4)",
              }}
            >
              <NavMinimap nodes={canvas.nodes} width={miniSize.w} height={miniSize.h} onNavigate={focusOnPoint} />
            </div>
            <div
              title="Redimensionar minimap"
              onPointerDown={(e) => {
                e.preventDefault();
                const sx = e.clientX, sy = e.clientY, sw = miniSize.w, sh = miniSize.h;
                const move = (ev: PointerEvent) => setMiniSize({
                  w: Math.min(560, Math.max(170, sw + (sx - ev.clientX))),
                  h: Math.min(380, Math.max(110, sh + (sy - ev.clientY))),
                });
                const up = () => {
                  window.removeEventListener("pointermove", move);
                  window.removeEventListener("pointerup", up);
                };
                window.addEventListener("pointermove", move);
                window.addEventListener("pointerup", up);
              }}
              style={{
                position: "absolute", zIndex: 11, cursor: "nwse-resize",
                right: (mode === "monitoring" && selectedInstance ? 392 : 16) + miniSize.w - 7,
                bottom: 16 + miniSize.h - 7,
                width: 14, height: 14, borderRadius: 4,
                background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-strong)",
              }}
            />
          </>
        )}

        {/* Empty state overlay */}
        {mode === "monitoring" && !hasInstances && (
          <EmptyState
            title={hasDefs ? "Nenhuma instance hoje" : "Ambiente vazio"}
            hint={hasDefs
              ? "Clique em Run Daily na topbar para materializar os jobs do dia."
              : "Vá para Design mode e crie jobs arrastando tipos da palette."}
          />
        )}
        {mode === "design" && !hasActiveFolders && (
          <EmptyState
            title="Nenhuma folder aberta"
            hint="Abra ou crie uma folder para começar a trabalhar. Sem folder ativa, não há onde colocar jobs."
          />
        )}
        {mode === "design" && hasActiveFolders && designDefsWithDraft.length === 0 && (
          <EmptyState
            title="Nenhuma definition"
            hint="Arraste um tipo da palette para o canvas para criar o primeiro job."
          />
        )}

        {mode === "monitoring" ? (
          // P3/escala — esconde a sidebar legada (não-virtualizada) sob o ViewPoint.
          scaleView ? null : (
            <MonitoringSidebarV2
              jobs={monitoringJobs}
              selectedId={selectedInstanceId}
              onSelect={handleSidebarSelect}
            />
          )
        ) : (
          // Fase 1: palette de drag só aparece com folder ativa.
          // Sem folder, não há destino válido para drop → esconde para evitar UX quebrada.
          hasActiveFolders ? (
            <DesignSidebarV2
              definitions={defs}
              onJobClick={(id) => focusNode(`d-${id}`)}
            />
          ) : null
        )}

        {mode === "monitoring" && selectedInstance && (
          <InstanceDetailsDrawer
            instance={selectedInstance}
            handlers={{
              onHold: holdInstance,
              onRelease: releaseInstance,
              onCancel: cancelInstance,
              onSkip: skipInstance,
              onBypass: bypassInstance,
              onRerun: handleRerunInstance,
              onClose: () => setSelectedInstanceId(null),
            }}
          />
        )}

        {mode === "design" && editingDef && (
          <JobConfigDrawer
            definition={editingDef.def}
            isNew={editingDef.isNew}
            availableFolders={activeFolders ? Array.from(activeFolders).sort() : []}
            allDefs={defs}
            handlers={{
              onSave: handleSaveDef,
              onDelete: handleDeleteDef,
              onClose: () => setEditingDef(null),
            }}
          />
        )}

        {ctxMenu && (
          <CanvasContextMenu
            x={ctxMenu.x}
            y={ctxMenu.y}
            items={ctxMenu.items}
            onClose={() => setCtxMenu(null)}
          />
        )}

        {showFolderManager && (
          <FolderManagerDialog
            visibleFolders={visibleFolders}
            onChangeVisible={setVisibleFolders}
            activeFolders={activeFolders ?? new Set()}
            onOpenFolder={(name) => addFolder("open", name)}
            onCreateFolder={(name) => addFolder("create", name)}
            onCloseFolder={closeFolder}
            sessionId={designSessionId}
            newFolderCount={designSessionNewFolders.length}
            onPublished={handlePublished}
            onDiscardSession={handleDiscardSession}
            inDesignMode={mode === "design"}
            onOpened={() => { setMode("design"); setShowFolderManager(false); }}
            onClose={() => setShowFolderManager(false)}
          />
        )}

        {showUsers && me && me.role === "admin" && (
          <UsersDialog meId={me.id} onClose={() => setShowUsers(false)} />
        )}

        {showControlM && <ControlMPanel onClose={() => setShowControlM(false)} />}

        {showAlerts && (
          <AlertsPanel
            onClose={() => setShowAlerts(false)}
            onChange={() => { void fetchUnacknowledgedCount().then(setUnreadAlerts); }}
            isAdmin={me?.role === "admin"}
          />
        )}

        {showSettings && <SettingsDialog onClose={() => setShowSettings(false)} />}
        {showDiff && <DailyDiffModal onClose={() => setShowDiff(false)} />}
        {showDryRun && <DryRunModal onClose={() => setShowDryRun(false)} />}

        <PRBannerHost />

        {/* F11.9 — Bulk action bar */}
        {selectedIds.size > 0 && mode === "monitoring" && (
          <BulkActionBar
            mode="monitoring"
            selected={selectedIds}
            instances={filteredInstances}
            handlers={{
              onHoldAll:    (ids) => handleBulk(ids, holdInstance, "hold"),
              onReleaseAll: (ids) => handleBulk(ids, releaseInstance, "release"),
              onCancelAll:  (ids) => handleBulk(ids, cancelInstance, "cancel"),
              onSetOkAll:   (ids) => handleBulk(ids, bypassInstance, "set-ok"),
              onRerunAll:   (ids) => handleBulk(ids, rerunInstance, "rerun"),
              onClear:      clearSelection,
            }}
          />
        )}
        {selectedIds.size > 0 && mode === "design" && (
          <BulkActionBar
            mode="design"
            selected={selectedIds}
            defs={filteredDefs}
            folders={activeFolders ? Array.from(activeFolders).sort() : []}
            handlers={{
              onDeleteAll: handleBulkDeleteDefs,
              onMoveAll: handleBulkMoveDefs,
              onClear: clearSelection,
            }}
          />
        )}

        {/* Modal de condição da dependência (substitui window.prompt) */}
        {pendingConn && (
          <EdgeConditionModal
            fromLabel={defs.find((d) => d.id === pendingConn.fromId)?.label ?? pendingConn.fromId}
            toLabel={defs.find((d) => d.id === pendingConn.toId)?.label ?? pendingConn.toId}
            onConfirm={confirmConnection}
            onCancel={() => setPendingConn(null)}
          />
        )}

        {/* P3/escala — ViewPoint server-driven cobre o canvas quando ligado */}
        {mode === "monitoring" && scaleView && (
          <ScaleMonitor onClose={() => setScaleView(false)} />
        )}

        <ToastHost />
      </main>

      {/* Footer */}
      <footer
        className="v2-edge-highlight"
        style={{
          height: 24, padding: "0 16px",
          borderTop: "1px solid var(--v2-border-subtle)",
          background: "var(--v2-bg-surface)",
          display: "flex", alignItems: "center", gap: 20,
          fontSize: 10, fontFamily: "var(--v2-font-mono)",
          color: "var(--v2-text-muted)",
          letterSpacing: "0.04em", flexShrink: 0,
        }}
      >
        <span>
          <span style={{ color: "var(--v2-text-secondary)", fontWeight: 500 }}>{defs.length}</span>
          <span style={{ opacity: 0.7 }}> definitions · </span>
          <span style={{ color: "var(--v2-text-secondary)", fontWeight: 500 }}>{instances.length}</span>
          <span style={{ opacity: 0.7 }}> instances · </span>
          <span style={{ color: "var(--v2-text-secondary)", fontWeight: 500 }}>{todayOrderDate()}</span>
        </span>
        {lastDaily && (
          <span>
            <span style={{ opacity: 0.7 }}>daily </span>
            <span style={{ color: "var(--v2-text-secondary)", fontWeight: 500 }}>
              {new Date(lastDaily).toLocaleTimeString("en-GB", { hour12: false })}
            </span>
          </span>
        )}
        <span style={{ marginLeft: "auto", color: "var(--v2-text-secondary)", fontWeight: 600, textTransform: "uppercase" }}>{mode}</span>
      </footer>
    </div>
  );
}

/* ──────────────────────────────────────────────────────────────
   Subcomponents
   ────────────────────────────────────────────────────────────── */

function EmptyState({ title, hint }: { title: string; hint: string }) {
  return (
    <div
      style={{
        position: "absolute",
        inset: 0,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        pointerEvents: "none",
      }}
    >
      <div
        style={{
          padding: "16px 24px",
          background: "var(--v2-bg-surface)",
          border: "1px solid var(--v2-border-medium)",
          borderRadius: 6,
          textAlign: "center",
          maxWidth: 360,
        }}
      >
        <div style={{ fontSize: 13, fontWeight: 600, color: "var(--v2-text-primary)", marginBottom: 6 }}>{title}</div>
        <div style={{ fontSize: 11, color: "var(--v2-text-secondary)", lineHeight: 1.5 }}>{hint}</div>
      </div>
    </div>
  );
}

/* ──────────────────────────────────────────────────────────────
   Root (wraps ReactFlowProvider)
   ────────────────────────────────────────────────────────────── */

export default function V2Preview() {
  return (
    <ReactFlowProvider>
      <V2PreviewInner />
    </ReactFlowProvider>
  );
}
