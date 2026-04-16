import { useEffect, useMemo, useState } from "react";
import { ReactFlow, Background, BackgroundVariant, type Node, type Edge } from "@xyflow/react";
import JobNodeV2 from "./JobNodeV2";
import MonitoringSidebarV2, { type MonitoringJob } from "./MonitoringSidebarV2";
import DesignSidebarV2 from "./DesignSidebarV2";
import InstanceDetailsDrawer from "./InstanceDetailsDrawer";
import type { JobNodeData } from "@/lib/job-config";
import type { JobInstance, JobDefinition } from "@/lib/orchestrator-model";
import { createInstance, todayOrderDate } from "@/lib/orchestrator-model";
import {
  getTodayInstances,
  onInstanceChange,
  holdInstance,
  releaseInstance,
  cancelInstance,
  rerunInstance,
  skipInstance,
  bypassInstance,
  orderJob,
} from "@/lib/instance-store";

import "@xyflow/react/dist/style.css";
import "@/index.css";
import "./tokens.css";

type Mode = "design" | "monitoring";

/* ──────────────────────────────────────────────────────────────
   Mapeamento Domain ↔ UI
   ────────────────────────────────────────────────────────────── */

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
    startedAt: inst.startedAt ? new Date(inst.startedAt).toLocaleTimeString("en-GB", { hour12: false }).slice(0, 5) : undefined,
  };
}

/* ──────────────────────────────────────────────────────────────
   Seed — cria instances de exemplo no primeiro load
   ────────────────────────────────────────────────────────────── */

const SEED_FLAG = "regente:v2-seeded:v1";

function sampleDef(id: string, label: string, jobType: string, team: string): JobDefinition {
  return {
    id,
    label,
    jobType,
    team,
    schedule: { cronExpression: "0 3 * * *", enabled: true, description: "daily 03:00" },
    retries: 2,
    timeout: 300,
  };
}

function seedIfEmpty(): void {
  if (typeof window === "undefined") return;
  if (window.localStorage.getItem(SEED_FLAG) === "1") return;
  const existing = getTodayInstances();
  if (existing.length > 0) {
    window.localStorage.setItem(SEED_FLAG, "1");
    return;
  }

  const now = Date.now();
  const samples: Array<{ def: JobDefinition; status: JobInstance["status"]; offsetStart?: number; durationMs?: number; error?: string }> = [
    { def: sampleDef("extract-picpay-tx",    "extract-picpay-tx",    "LAMBDA",        "DATA"), status: "OK",      offsetStart: -3600_000, durationMs: 4200 },
    { def: sampleDef("extract-pix-events",   "extract-pix-events",   "LAMBDA",        "DATA"), status: "OK",      offsetStart: -3600_000, durationMs: 3100 },
    { def: sampleDef("transform-daily",      "transform-daily",      "GLUE",          "DATA"), status: "RUNNING", offsetStart: -45_000 },
    { def: sampleDef("load-warehouse-fact",  "load-warehouse-fact",  "BATCH",         "DATA"), status: "WAITING" },
    { def: sampleDef("load-warehouse-dim",   "load-warehouse-dim",   "BATCH",         "DATA"), status: "WAITING" },
    { def: sampleDef("reconcile-ledger",     "reconcile-ledger",     "STEP_FUNCTION", "FIN"),  status: "NOTOK",   offsetStart: -7200_000, durationMs: 12400, error: "timeout after 12s on endpoint /reconcile" },
    { def: sampleDef("fraud-check-daily",    "fraud-check-daily",    "LAMBDA",        "RISK"), status: "RUNNING", offsetStart: -8_900 },
    { def: sampleDef("risk-score-refresh",   "risk-score-refresh",   "GLUE",          "RISK"), status: "WAITING" },
    { def: sampleDef("notify-ops",           "notify-ops",           "HTTP",          "PLAT"), status: "WAITING" },
    { def: sampleDef("backup-postgres",      "backup-postgres",      "BATCH",         "PLAT"), status: "OK",      offsetStart: -9000_000, durationMs: 186000 },
    { def: sampleDef("rotate-secrets",       "rotate-secrets",       "LAMBDA",        "PLAT"), status: "OK",      offsetStart: -1800_000, durationMs: 920 },
    { def: sampleDef("reconcile-cartoes",    "reconcile-cartoes",    "STEP_FUNCTION", "FIN"),  status: "OK",      offsetStart: -5400_000, durationMs: 23400 },
    { def: sampleDef("pix-settlement-batch", "pix-settlement-batch", "BATCH",         "FIN"),  status: "RUNNING", offsetStart: -67_000 },
    { def: sampleDef("send-cashback-rewards","send-cashback-rewards","HTTP",          "FIN"),  status: "WAITING" },
    { def: sampleDef("audit-export-daily",   "audit-export-daily",   "GLUE",          "RISK"), status: "HOLD" },
  ];

  for (const s of samples) {
    const inst = createInstance(s.def, new Date(), false);
    inst.status = s.status;
    if (s.offsetStart) {
      inst.startedAt = now + s.offsetStart;
      if (s.durationMs) {
        inst.completedAt = inst.startedAt + s.durationMs;
        inst.durationMs = s.durationMs;
      }
      inst.attempts = 1;
    }
    if (s.error) inst.error = s.error;
    const all = JSON.parse(window.localStorage.getItem("regente:instances") || "[]") as JobInstance[];
    all.push(inst);
    window.localStorage.setItem("regente:instances", JSON.stringify(all));
  }
  window.localStorage.setItem(SEED_FLAG, "1");
}

/* ──────────────────────────────────────────────────────────────
   Canvas helpers
   ────────────────────────────────────────────────────────────── */

function buildMonitoringCanvas(instances: JobInstance[]): { nodes: Node[]; edges: Edge[] } {
  // Layout: 3 colunas, stacking por posição de chegada. DAG real virá da definition.
  const nodes: Node[] = instances.slice(0, 15).map((inst, idx) => {
    const col = idx % 3;
    const row = Math.floor(idx / 3);
    return {
      id: `m-${inst.id}`,
      type: "jobV2",
      position: { x: 60 + col * 240, y: 30 + row * 80 },
      data: {
        label: inst.label,
        jobType: inst.jobType,
        status: INSTANCE_TO_UI_STATUS[inst.status],
        team: inst.team,
        lastRun: inst.startedAt ? new Date(inst.startedAt).toLocaleTimeString("en-GB", { hour12: false }).slice(0, 5) : undefined,
        mode: "monitoring",
      } as JobNodeData,
    };
  });
  return { nodes, edges: [] };
}

function buildDesignCanvas(): { nodes: Node[]; edges: Edge[] } {
  // Placeholder — Fase 6 conectará ao definition-store
  const nodes: Node[] = [
    { id: "d-1", type: "jobV2", position: { x: 60, y: 40 }, data: { label: "extract-picpay-tx", jobType: "LAMBDA", status: "INACTIVE", team: "DATA" } as JobNodeData },
    { id: "d-2", type: "jobV2", position: { x: 60, y: 120 }, data: { label: "transform-daily", jobType: "GLUE", status: "INACTIVE", team: "DATA" } as JobNodeData },
    { id: "d-3", type: "jobV2", position: { x: 60, y: 200 }, data: { label: "load-warehouse", jobType: "BATCH", status: "INACTIVE", team: "DATA" } as JobNodeData },
    { id: "d-4", type: "jobV2", position: { x: 320, y: 200 }, data: { label: "reconcile-ledger", jobType: "STEP_FUNCTION", status: "INACTIVE", team: "FIN" } as JobNodeData },
  ];
  const edges: Edge[] = [
    { id: "d-e1-2", source: "d-1", target: "d-2", label: "on-success", style: { stroke: "#064E2B", strokeWidth: 1.5 }, labelStyle: { fill: "#11C76F", fontSize: 10, fontFamily: "JetBrains Mono, monospace" }, labelBgStyle: { fill: "#0a0a0a" } },
    { id: "d-e2-3", source: "d-2", target: "d-3", label: "on-success", style: { stroke: "#064E2B", strokeWidth: 1.5 }, labelStyle: { fill: "#11C76F", fontSize: 10, fontFamily: "JetBrains Mono, monospace" }, labelBgStyle: { fill: "#0a0a0a" } },
    { id: "d-e2-4", source: "d-2", target: "d-4", label: "on-failure", style: { stroke: "#7f1d1d", strokeWidth: 1.5, strokeDasharray: "4 4" }, labelStyle: { fill: "#ef4444", fontSize: 10, fontFamily: "JetBrains Mono, monospace" }, labelBgStyle: { fill: "#0a0a0a" } },
  ];
  return { nodes, edges };
}

/* ──────────────────────────────────────────────────────────────
   Component
   ────────────────────────────────────────────────────────────── */

export default function V2Preview() {
  const [mode, setMode] = useState<Mode>("monitoring");
  const [instances, setInstances] = useState<JobInstance[]>([]);
  const [selectedInstanceId, setSelectedInstanceId] = useState<string | null>(null);

  const nodeTypes = useMemo(() => ({ jobV2: JobNodeV2 }), []);

  // Mount: seed + subscribe
  useEffect(() => {
    seedIfEmpty();
    setInstances(getTodayInstances());
    const unsub = onInstanceChange(() => {
      setInstances(getTodayInstances().filter((i) => i.orderDate === todayOrderDate()));
    });
    return unsub;
  }, []);

  const monitoringJobs = useMemo(() => instances.map(instanceToMonitoring), [instances]);
  const canvas = useMemo(
    () => (mode === "monitoring" ? buildMonitoringCanvas(instances) : buildDesignCanvas()),
    [mode, instances],
  );
  const selectedInstance = selectedInstanceId
    ? instances.find((i) => i.id === selectedInstanceId)
    : null;

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

  const handleRerun = (id: string) => {
    const fresh = rerunInstance(id);
    if (fresh) setSelectedInstanceId(fresh.id);
  };

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
              width: 18,
              height: 18,
              background: "var(--v2-accent-brand)",
              borderRadius: 3,
              display: "inline-flex",
              alignItems: "center",
              justifyContent: "center",
              color: "#000",
              fontWeight: 700,
              fontSize: 11,
            }}
          >
            R
          </div>
          <span style={{ fontSize: 13, fontWeight: 600, letterSpacing: "0.02em" }}>
            Regente
          </span>
        </div>

        <div
          style={{
            display: "flex",
            background: "var(--v2-bg-elevated)",
            border: "1px solid var(--v2-border-medium)",
            borderRadius: 4,
            overflow: "hidden",
          }}
        >
          {(["design", "monitoring"] as const).map((m) => (
            <button
              key={m}
              onClick={() => { setMode(m); setSelectedInstanceId(null); }}
              style={{
                padding: "5px 14px",
                background: mode === m ? "var(--v2-accent-deep)" : "transparent",
                border: "none",
                borderRight: m === "design" ? "1px solid var(--v2-border-medium)" : "none",
                color: mode === m ? "var(--v2-accent-brand)" : "var(--v2-text-secondary)",
                fontSize: 11,
                fontFamily: "var(--v2-font-mono)",
                letterSpacing: "0.06em",
                cursor: "pointer",
                fontWeight: mode === m ? 600 : 500,
                textTransform: "uppercase",
              }}
            >
              {m}
            </button>
          ))}
        </div>

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
      <main style={{ flex: 1, position: "relative", minHeight: 0 }}>
        <ReactFlow
          nodes={canvas.nodes}
          edges={canvas.edges}
          nodeTypes={nodeTypes}
          fitView
          fitViewOptions={{ padding: 0.2 }}
          proOptions={{ hideAttribution: true }}
          nodesDraggable={false}
          nodesConnectable={false}
          elementsSelectable
          panOnDrag
          zoomOnScroll
          onNodeClick={(_, node) => {
            if (mode === "monitoring") {
              const id = node.id.replace(/^m-/, "");
              setSelectedInstanceId(id);
            }
          }}
        >
          <Background variant={BackgroundVariant.Dots} gap={18} size={1} color="#1a1a1a" />
        </ReactFlow>

        {mode === "monitoring" ? (
          <MonitoringSidebarV2
            jobs={monitoringJobs}
            selectedId={selectedInstanceId}
            onSelect={setSelectedInstanceId}
          />
        ) : (
          <DesignSidebarV2 />
        )}

        {mode === "monitoring" && selectedInstance && (
          <InstanceDetailsDrawer
            instance={selectedInstance}
            handlers={{
              onHold:    holdInstance,
              onRelease: releaseInstance,
              onCancel:  cancelInstance,
              onSkip:    skipInstance,
              onBypass:  bypassInstance,
              onRerun:   handleRerun,
              onClose:   () => setSelectedInstanceId(null),
            }}
          />
        )}
      </main>

      {/* Footer */}
      <footer
        style={{
          height: 24,
          padding: "0 16px",
          borderTop: "1px solid var(--v2-border-subtle)",
          background: "var(--v2-bg-surface)",
          display: "flex",
          alignItems: "center",
          gap: 20,
          fontSize: 10,
          fontFamily: "var(--v2-font-mono)",
          color: "var(--v2-text-muted)",
          letterSpacing: "0.04em",
          flexShrink: 0,
        }}
      >
        <span>{instances.length} instances · {todayOrderDate()}</span>
        <span style={{ marginLeft: "auto" }}>{mode}</span>
      </footer>

      {/* Mock reference to orderJob/suppress unused (scheduler wire-up Fase 6) */}
      <span style={{ display: "none" }}>{orderJob.name}</span>
    </div>
  );
}
