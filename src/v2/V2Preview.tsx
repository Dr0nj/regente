import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Dagre from "@dagrejs/dagre";
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  BackgroundVariant,
  useReactFlow,
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
import { onServerEvent, isServerMode, onAuthEvent, setAuthToken } from "@/lib/server-client";
import { fetchMe, loadCachedUser, type AuthUser } from "@/lib/auth-api";
import { LoginForm } from "./LoginForm";
import { UserMenu } from "./UserMenu";
import { UsersDialog } from "./UsersDialog";

import "@xyflow/react/dist/style.css";
import "@/index.css";
import "./tokens.css";

type Mode = "design" | "monitoring";

/* ──────────────────────────────────────────────────────────────
   Constantes de layout (folders como colunas — Control-M style)
   ────────────────────────────────────────────────────────────── */

const NODE_W = 220;
const NODE_H = 72;
const NODE_GAP_Y = 28; // ranksep dagre (dep vertical)
const NODE_GAP_X = 36; // nodesep dagre (jobs paralelos na mesma linha)
const COL_PADDING_X = 24;
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
): InnerLayout {
  const memberIds = new Set(members.map((m) => nodeIdOf(m)));
  const innerEdges = allEdges.filter((e) => memberIds.has(e.source) && memberIds.has(e.target));

  const g = new Dagre.graphlib.Graph().setDefaultEdgeLabel(() => ({}));
  g.setGraph({ rankdir: "TB", nodesep: NODE_GAP_X, ranksep: NODE_GAP_Y, marginx: 0, marginy: 0 });
  for (const m of members) g.setNode(nodeIdOf(m), { width: NODE_W, height: NODE_H });
  for (const e of innerEdges) g.setEdge(e.source, e.target);
  Dagre.layout(g);

  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
  const raw = new Map<string, { x: number; y: number }>();
  for (const m of members) {
    const dn = g.node(nodeIdOf(m));
    const x0 = dn.x - NODE_W / 2;
    const y0 = dn.y - NODE_H / 2;
    raw.set(nodeIdOf(m), { x: x0, y: y0 });
    if (x0 < minX) minX = x0;
    if (y0 < minY) minY = y0;
    if (x0 + NODE_W > maxX) maxX = x0 + NODE_W;
    if (y0 + NODE_H > maxY) maxY = y0 + NODE_H;
  }

  // Normaliza para origem (0,0)
  const positions = new Map<string, { x: number; y: number }>();
  for (const [id, p] of raw) {
    positions.set(id, { x: p.x - minX, y: p.y - minY });
  }
  return {
    team,
    positions,
    width: maxX - minX,
    height: maxY - minY,
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
): { nodes: Node[]; lanes: LaneInfo[] } {
  const grouped = groupByTeam(items);
  const layouts: InnerLayout[] = [];
  for (const [team, members] of grouped) {
    layouts.push(layoutFolderInner(team, members, nodeIdOf, allEdges));
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

function buildMonitoringCanvas(rawInstances: JobInstance[], defs: JobDefinition[]): Canvas {
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
  );

  return { nodes, edges, lanes };
}

function buildDesignCanvas(defs: JobDefinition[]): Canvas {
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
        schedule: def.schedule.cronExpression,
        mode: "design",
      } as JobNodeData,
      zIndex: 10,
    }),
    rawEdges,
    (def) => `d-${def.id}`,
  );

  return { nodes, edges, lanes };
}

/* ──────────────────────────────────────────────────────────────
   Inner component (tem acesso a useReactFlow)
   ────────────────────────────────────────────────────────────── */

function V2PreviewInner() {
  const [mode, setMode] = useState<Mode>("monitoring");
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
  // F11.9 — multi-selection no canvas (ReactFlow nativo via Shift+click / drag rect)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const rfInstance = useRef<ReactFlowInstance | null>(null);
  const { setCenter, fitView } = useReactFlow();

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
    const unsubInst = onInstanceChange(() => {
      setInstances(getTodayInstances().filter((i) => i.orderDate === todayOrderDate()));
    });
    startScheduler(2000);

    // Server mode: subscribe a WS para recarregar defs quando mudarem no server
    let unsubWs: (() => void) | null = null;
    if (isServerMode()) {
      unsubWs = onServerEvent((ev) => {
        if (ev.event === "definition.changed" || ev.event === "definition.deleted") {
          void reloadDefinitions().then((list) => setDefs([...list]));
        }
        // F11.8 — folder.changed: foldermanager já faz refresh interno; aqui só
        // garantimos que defs sigam coerentes (rename/delete podem ter movido jobs).
        if (ev.event === "folder.changed") {
          void reloadDefinitions().then((list) => setDefs([...list]));
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

  // F11.8 — filtered defs/instances by visibleFolders (null = all)
  const filteredDefs = useMemo(() => {
    if (visibleFolders === null) return defs;
    return defs.filter((d) => visibleFolders.has(d.team ?? ""));
  }, [defs, visibleFolders]);
  const filteredInstances = useMemo(() => {
    if (visibleFolders === null) return instances;
    const defsById = new Map(defs.map((d) => [d.id, d] as const));
    return instances.filter((i) => {
      const team = i.team || defsById.get(i.definitionId)?.team || "";
      return visibleFolders.has(team);
    });
  }, [instances, defs, visibleFolders]);

  const canvas = useMemo<Canvas>(
    () => (mode === "monitoring" ? buildMonitoringCanvas(filteredInstances, filteredDefs) : buildDesignCanvas(filteredDefs)),
    [mode, filteredInstances, filteredDefs],
  );

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
    setCenter(px, py, { zoom: 1.1, duration: 350 });
  }, [canvas.nodes, setCenter]);

  const handleSidebarSelect = useCallback((instId: string) => {
    setSelectedInstanceId(instId);
    focusNode(`m-${instId}`);
  }, [focusNode]);

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
    const draft: JobDefinition = {
      id: suggestedId,
      label: suggestedId,
      jobType: type,
      team: "",
      schedule: { cronExpression: "0 3 * * *", enabled: true, description: "daily 03:00" },
      retries: 2,
      timeout: 300,
    };
    setEditingDef({ def: draft, isNew: true });
  }, [mode]);

  /* ── onConnect (Fase 8: edges com condição) ── */
  const onConnect: OnConnect = useCallback((conn: Connection) => {
    if (mode !== "design") return;
    if (!conn.source || !conn.target || conn.source === conn.target) return;
    const fromId = conn.source.replace(/^d-/, "");
    const toId = conn.target.replace(/^d-/, "");
    const choice = window.prompt(
      "Condição da dependência?\n  s = on-success (default)\n  f = on-failure\n  c = on-complete\n  a = always",
      "s",
    );
    if (choice === null) return;
    const map: Record<string, EdgeCondition> = { s: "on-success", f: "on-failure", c: "on-complete", a: "always" };
    const condition = map[choice.trim().toLowerCase()] ?? "on-success";
    const target = defs.find((d) => d.id === toId);
    if (!target) return;
    const up = target.upstream ?? [];
    // remove aresta prévia do mesmo `from` para evitar duplicatas
    const next = [...up.filter((u) => u.from !== fromId), { from: fromId, condition }];
    const updated: JobDefinition = { ...target, upstream: next };
    void saveDefinition(updated);
  }, [mode, defs]);

  /* ── Save/Delete definition ── */
  const handleSaveDef = useCallback(async (def: JobDefinition) => {
    await saveDefinition(def);
    setEditingDef(null);
  }, []);
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
    if (container.storageBackend === "server") {
      // server mode: refresh é assíncrono via WS; UI re-renderiza sozinha
      setTimeout(() => fitView({ padding: 0.2, duration: 300 }), 200);
      return;
    }
    if (created.length > 0) {
      setInstances(getTodayInstances());
      setTimeout(() => fitView({ padding: 0.2, duration: 300 }), 50);
    } else {
      alert("Nenhuma definition elegível (sem cron habilitado ou já materializadas hoje).");
    }
  }, [defs, fitView]);

  const handleRerunInstance = useCallback((id: string) => {
    Promise.resolve(rerunInstance(id)).then((fresh) => {
      if (fresh) setSelectedInstanceId(fresh.id);
    });
  }, []);

  /* ── F11.9 Bulk handlers ── */
  const clearSelection = useCallback(() => setSelectedIds(new Set()), []);

  const handleBulk = useCallback(
    async (ids: string[], op: (id: string) => Promise<unknown> | unknown) => {
      const results = await Promise.allSettled(ids.map((id) => Promise.resolve(op(id))));
      const failed = results.filter((r) => r.status === "rejected");
      if (failed.length > 0) {
        console.warn(`[bulk] ${failed.length}/${ids.length} failed`, failed);
        alert(`${failed.length} of ${ids.length} actions failed. See console.`);
      }
      clearSelection();
    },
    [clearSelection],
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
      if (fresh) setSelectedInstanceId(fresh.id);
    }).catch((err) => {
      console.error("[force] failed", err);
      alert(`Force falhou: ${err?.message ?? err}`);
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
        className="v2-grain v2-edge-highlight"
        style={{
          height: 44,
          padding: "0 16px",
          borderBottom: "1px solid var(--v2-border-subtle)",
          background: "var(--v2-bg-surface)",
          display: "flex",
          alignItems: "center",
          gap: 16,
          flexShrink: 0,
          // z-index acima do canvas para dropdowns (UserMenu) escaparem do stacking context.
          position: "relative",
          zIndex: 50,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <div
            style={{
              width: 18, height: 18, background: "var(--v2-accent-brand)",
              borderRadius: 3, display: "inline-flex", alignItems: "center", justifyContent: "center",
              color: "#000", fontWeight: 700, fontSize: 11,
            }}
          >R</div>
          <span style={{ fontSize: 13, fontWeight: 600, letterSpacing: "0.02em" }}>Regente</span>
          <span style={{ fontSize: 9, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)", letterSpacing: "0.06em", marginLeft: 4 }}>
            {container.storageBackend}
          </span>
        </div>

        <div
          style={{
            display: "flex", background: "var(--v2-bg-elevated)",
            border: "1px solid var(--v2-border-medium)", borderRadius: 4, overflow: "hidden",
          }}
        >
          {(["design", "monitoring"] as const).map((m) => (
            <button
              key={m}
              onClick={() => { setMode(m); setSelectedInstanceId(null); setEditingDef(null); }}
              style={{
                padding: "5px 14px",
                background: mode === m ? "var(--v2-accent-deep)" : "transparent",
                border: "none",
                borderRight: m === "design" ? "1px solid var(--v2-border-medium)" : "none",
                color: mode === m ? "var(--v2-accent-brand)" : "var(--v2-text-secondary)",
                fontSize: 11, fontFamily: "var(--v2-font-mono)",
                letterSpacing: "0.06em", cursor: "pointer",
                fontWeight: mode === m ? 600 : 500, textTransform: "uppercase",
              }}
            >{m}</button>
          ))}
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
          <span>▦ Folders</span>
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
              }}
            >
              ▶ Run Daily
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
                }}
              >
                ⚡ Force ▾
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

        {me && (
          <UserMenu
            me={me}
            onLogout={() => setMe(null)}
            onOpenUsers={() => setShowUsers(true)}
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
        <ReactFlow
          nodes={canvas.nodes}
          edges={canvas.edges}
          nodeTypes={nodeTypes}
          fitView
          fitViewOptions={{ padding: 0.25 }}
          proOptions={{ hideAttribution: true }}
          nodesDraggable={mode === "design"}
          nodesConnectable={mode === "design"}
          elementsSelectable
          // Pan: left button (default UX). Selection rect: Shift+drag.
          // panOnDrag={[0,1]} cobre left+middle; right (2) fica livre p/ ctx menu.
          panOnDrag={[0, 1]}
          zoomOnScroll
          selectionOnDrag={false}
          selectionKeyCode="Shift"
          multiSelectionKeyCode={["Shift", "Meta", "Control"]}
          onNodeClick={onNodeClick}
          onNodeContextMenu={onNodeContextMenu}
          onConnect={onConnect}
          onSelectionChange={({ nodes: sel }) => {
            const ids = new Set<string>();
            for (const n of sel) {
              if (n.type === "laneLabel") continue;
              const raw = n.id.replace(/^[md]-/, "");
              ids.add(raw);
            }
            setSelectedIds(ids);
          }}
          onInit={(inst) => { rfInstance.current = inst; }}
        >
          <Background variant={BackgroundVariant.Dots} gap={18} size={1} color="#1a1a1a" />
        </ReactFlow>

        {/* Empty state overlay */}
        {mode === "monitoring" && !hasInstances && (
          <EmptyState
            title={hasDefs ? "Nenhuma instance hoje" : "Ambiente vazio"}
            hint={hasDefs
              ? "Clique em Run Daily na topbar para materializar os jobs do dia."
              : "Vá para Design mode e crie jobs arrastando tipos da palette."}
          />
        )}
        {mode === "design" && !hasDefs && (
          <EmptyState
            title="Nenhuma definition"
            hint="Arraste um tipo da palette para o canvas para criar o primeiro job."
          />
        )}

        {mode === "monitoring" ? (
          <MonitoringSidebarV2
            jobs={monitoringJobs}
            selectedId={selectedInstanceId}
            onSelect={handleSidebarSelect}
          />
        ) : (
          <DesignSidebarV2 definitions={defs} />
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
            onClose={() => setShowFolderManager(false)}
          />
        )}

        {showUsers && me && me.role === "admin" && (
          <UsersDialog meId={me.id} onClose={() => setShowUsers(false)} />
        )}

        {/* F11.9 — Bulk action bar */}
        {selectedIds.size > 0 && mode === "monitoring" && (
          <BulkActionBar
            mode="monitoring"
            selected={selectedIds}
            instances={filteredInstances}
            handlers={{
              onHoldAll:    (ids) => handleBulk(ids, holdInstance),
              onReleaseAll: (ids) => handleBulk(ids, releaseInstance),
              onCancelAll:  (ids) => handleBulk(ids, cancelInstance),
              onSetOkAll:   (ids) => handleBulk(ids, bypassInstance),
              onRerunAll:   (ids) => handleBulk(ids, rerunInstance),
              onClear:      clearSelection,
            }}
          />
        )}
        {selectedIds.size > 0 && mode === "design" && (
          <BulkActionBar
            mode="design"
            selected={selectedIds}
            defs={filteredDefs}
            handlers={{
              onDeleteAll: handleBulkDeleteDefs,
              onClear: clearSelection,
            }}
          />
        )}
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
