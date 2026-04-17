import { useCallback, useEffect, useMemo, useRef, useState } from "react";
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
import { onServerEvent, isServerMode } from "@/lib/server-client";

import "@xyflow/react/dist/style.css";
import "@/index.css";
import "./tokens.css";

type Mode = "design" | "monitoring";

/* ──────────────────────────────────────────────────────────────
   Constantes de layout (swimlanes por team)
   ────────────────────────────────────────────────────────────── */

const LANE_HEIGHT = 140;
const LANE_HEADER_W = 90;
const NODE_W = 180;
const NODE_H = 56;
const NODE_GAP = 24;
const CANVAS_LEFT_OFFSET = LANE_HEADER_W + 32;

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
   Canvas builders (swimlanes + edges)
   ────────────────────────────────────────────────────────────── */

interface Canvas { nodes: Node[]; edges: Edge[]; lanes: LaneInfo[] }
interface LaneInfo { team: string; y: number; count: number }

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

function edgeStyleForCondition(c: EdgeCondition) {
  if (c === "on-failure") {
    return { stroke: "#7f1d1d", strokeDasharray: "4 4", labelColor: "#ef4444" };
  }
  if (c === "on-complete" || c === "always") {
    return { stroke: "#525252", strokeDasharray: "6 3", labelColor: "#a3a3a3" };
  }
  return { stroke: "#064E2B", strokeDasharray: undefined, labelColor: "#11C76F" };
}

function makeEdge(source: string, target: string, condition: EdgeCondition): Edge {
  const s = edgeStyleForCondition(condition);
  return {
    id: `e-${source}-${target}`,
    source,
    target,
    label: condition,
    data: { condition },
    style: { stroke: s.stroke, strokeWidth: 1.5, strokeDasharray: s.strokeDasharray },
    labelStyle: { fill: s.labelColor, fontSize: 10, fontFamily: "JetBrains Mono, monospace" },
    labelBgStyle: { fill: "#0a0a0a" },
  };
}

function buildMonitoringCanvas(instances: JobInstance[], defs: JobDefinition[]): Canvas {
  const grouped = groupByTeam(instances);
  const nodes: Node[] = [];
  const lanes: LaneInfo[] = [];
  let laneIdx = 0;

  const posById = new Map<string, { defId: string; x: number; y: number }>();

  for (const [team, list] of grouped) {
    const y = 40 + laneIdx * LANE_HEIGHT;
    lanes.push({ team, y, count: list.length });
    const laneWidth = Math.max(600, CANVAS_LEFT_OFFSET + list.length * (NODE_W + NODE_GAP));
    nodes.push({
      id: `lane-m-${team}`,
      type: "laneLabel",
      position: { x: 8, y },
      data: { team, count: list.length, width: laneWidth },
      draggable: false,
      selectable: false,
      connectable: false,
    });
    list.forEach((inst, col) => {
      const x = CANVAS_LEFT_OFFSET + col * (NODE_W + NODE_GAP);
      nodes.push({
        id: `m-${inst.id}`,
        type: "jobV2",
        position: { x, y: y + 20 },
        data: {
          label: inst.label,
          jobType: inst.jobType,
          status: INSTANCE_TO_UI_STATUS[inst.status],
          team: inst.team,
          lastRun: inst.startedAt ? fmtHm(inst.startedAt) : undefined,
          mode: "monitoring",
        } as JobNodeData,
        draggable: false,
      });
      posById.set(inst.id, { defId: inst.definitionId, x, y: y + 20 });
    });
    laneIdx++;
  }

  // Edges a partir do upstream da definition, resolvidas para instances do mesmo dia.
  const defsById = new Map(defs.map((d) => [d.id, d] as const));
  const instByDefId = new Map<string, JobInstance>();
  for (const i of instances) instByDefId.set(i.definitionId, i);

  const edges: Edge[] = [];
  for (const inst of instances) {
    const def = defsById.get(inst.definitionId);
    if (!def?.upstream?.length) continue;
    for (const u of def.upstream) {
      const parent = instByDefId.get(u.from);
      if (!parent) continue;
      edges.push(makeEdge(`m-${parent.id}`, `m-${inst.id}`, u.condition ?? EDGE_CONDITION_DEFAULT));
    }
  }

  return { nodes, edges, lanes };
}

function buildDesignCanvas(defs: JobDefinition[]): Canvas {
  const grouped = groupByTeam(defs);
  const nodes: Node[] = [];
  const lanes: LaneInfo[] = [];
  let laneIdx = 0;

  for (const [team, list] of grouped) {
    const y = 40 + laneIdx * LANE_HEIGHT;
    lanes.push({ team, y, count: list.length });
    const laneWidth = Math.max(600, CANVAS_LEFT_OFFSET + list.length * (NODE_W + NODE_GAP));
    nodes.push({
      id: `lane-d-${team}`,
      type: "laneLabel",
      position: { x: 8, y },
      data: { team, count: list.length, width: laneWidth },
      draggable: false,
      selectable: false,
      connectable: false,
    });
    list.forEach((def, col) => {
      nodes.push({
        id: `d-${def.id}`,
        type: "jobV2",
        position: { x: CANVAS_LEFT_OFFSET + col * (NODE_W + NODE_GAP), y: y + 20 },
        data: {
          label: def.label,
          jobType: def.jobType as JobNodeData["jobType"],
          status: def.schedule.enabled ? "WAITING" : "INACTIVE",
          team: def.team,
          schedule: def.schedule.cronExpression,
          mode: "design",
        } as JobNodeData,
      });
    });
    laneIdx++;
  }

  const edges: Edge[] = [];
  for (const def of defs) {
    if (!def.upstream?.length) continue;
    for (const u of def.upstream) {
      edges.push(makeEdge(`d-${u.from}`, `d-${def.id}`, u.condition ?? EDGE_CONDITION_DEFAULT));
    }
  }
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

  const canvas = useMemo<Canvas>(
    () => (mode === "monitoring" ? buildMonitoringCanvas(instances, defs) : buildDesignCanvas(defs)),
    [mode, instances, defs],
  );

  const monitoringJobs = useMemo(() => instances.map(instanceToMonitoring), [instances]);
  const selectedInstance = selectedInstanceId ? instances.find((i) => i.id === selectedInstanceId) : null;

  const statusCounts = useMemo(() => {
    const c = { ok: 0, running: 0, failed: 0, waiting: 0, hold: 0 };
    for (const i of instances) {
      if (i.status === "OK") c.ok++;
      else if (i.status === "RUNNING") c.running++;
      else if (i.status === "NOTOK") c.failed++;
      else if (i.status === "WAITING") c.waiting++;
      else if (i.status === "HOLD") c.hold++;
    }
    return c;
  }, [instances]);

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

  const hasDefs = defs.length > 0;
  const hasInstances = instances.length > 0;

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
          height: 44,
          padding: "0 16px",
          borderBottom: "1px solid var(--v2-border-subtle)",
          background: "var(--v2-bg-surface)",
          display: "flex",
          alignItems: "center",
          gap: 16,
          flexShrink: 0,
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

        {mode === "monitoring" && (
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
        )}

        <div style={{ flex: 1 }} />

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
          panOnDrag
          zoomOnScroll
          onNodeClick={onNodeClick}
          onConnect={onConnect}
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
      </main>

      {/* Footer */}
      <footer
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
        <span>{defs.length} definitions · {instances.length} instances · {todayOrderDate()}</span>
        {lastDaily && <span>daily: {new Date(lastDaily).toLocaleTimeString("en-GB", { hour12: false })}</span>}
        <span style={{ marginLeft: "auto" }}>{mode}</span>
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
