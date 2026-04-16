import { useMemo, useState } from "react";
import { ReactFlow, Background, BackgroundVariant, type Node, type Edge } from "@xyflow/react";
import JobNodeV2 from "./JobNodeV2";
import MonitoringSidebarV2, { type MonitoringJob } from "./MonitoringSidebarV2";
import DesignSidebarV2 from "./DesignSidebarV2";
import type { JobNodeData } from "@/lib/job-config";

import "@xyflow/react/dist/style.css";
import "@/index.css";
import "./tokens.css";

/* ──────────────────────────────────────────────────────────────
   V2 Preview — piloto visual com sidebars
   ──────────────────────────────────────────────────────────────
   Switcher: Design | Monitoring no topo.
   Sidebar flutuante destacada (não colada nas bordas/topo).
   Canvas ocupa o resto, preto dominante.
   PILOTO — será destruído após aprovação.
   ────────────────────────────────────────────────────────────── */

type Mode = "design" | "monitoring";

const monitoringJobs: MonitoringJob[] = [
  { id: "01", label: "extract-picpay-tx",       team: "DATA", jobType: "LAMBDA",        status: "SUCCESS", durationMs: 4200,   startedAt: "02:14" },
  { id: "02", label: "extract-pix-events",      team: "DATA", jobType: "LAMBDA",        status: "SUCCESS", durationMs: 3100,   startedAt: "02:14" },
  { id: "03", label: "transform-daily",         team: "DATA", jobType: "GLUE",          status: "RUNNING", durationMs: 45000,  startedAt: "02:15" },
  { id: "04", label: "load-warehouse-fact",     team: "DATA", jobType: "BATCH",         status: "WAITING" },
  { id: "05", label: "load-warehouse-dim",      team: "DATA", jobType: "BATCH",         status: "WAITING" },
  { id: "06", label: "reconcile-ledger",        team: "FIN",  jobType: "STEP_FUNCTION", status: "FAILED",  durationMs: 12400,  startedAt: "01:58" },
  { id: "07", label: "fraud-check-daily",       team: "RISK", jobType: "LAMBDA",        status: "RUNNING", durationMs: 8900,   startedAt: "02:16" },
  { id: "08", label: "risk-score-refresh",      team: "RISK", jobType: "GLUE",          status: "WAITING" },
  { id: "09", label: "notify-ops",              team: "PLAT", jobType: "HTTP",          status: "INACTIVE" },
  { id: "10", label: "backup-postgres",         team: "PLAT", jobType: "BATCH",         status: "SUCCESS", durationMs: 186000, startedAt: "00:30" },
  { id: "11", label: "rotate-secrets",          team: "PLAT", jobType: "LAMBDA",        status: "SUCCESS", durationMs: 920,    startedAt: "03:00" },
  { id: "12", label: "reconcile-cartoes",       team: "FIN",  jobType: "STEP_FUNCTION", status: "SUCCESS", durationMs: 23400,  startedAt: "02:00" },
  { id: "13", label: "pix-settlement-batch",    team: "FIN",  jobType: "BATCH",         status: "RUNNING", durationMs: 67000,  startedAt: "02:10" },
  { id: "14", label: "send-cashback-rewards",   team: "FIN",  jobType: "HTTP",          status: "WAITING" },
  { id: "15", label: "audit-export-daily",      team: "RISK", jobType: "GLUE",          status: "WAITING" },
];

function buildMonitoringCanvas(): { nodes: Node[]; edges: Edge[] } {
  const rows = [
    ["01", "02"],
    ["03"],
    ["04", "05"],
    ["07"],
    ["13"],
  ];
  const nodes: Node[] = [];
  rows.forEach((row, rowIdx) => {
    row.forEach((id, colIdx) => {
      const job = monitoringJobs.find((j) => j.id === id)!;
      nodes.push({
        id: `m-${id}`,
        type: "jobV2",
        position: { x: 60 + colIdx * 240, y: 40 + rowIdx * 80 },
        data: {
          label: job.label,
          jobType: job.jobType,
          status: job.status,
          team: job.team,
          lastRun: job.startedAt,
        } as JobNodeData,
      });
    });
  });
  const edges: Edge[] = [
    { id: "m-e1-3", source: "m-01", target: "m-03", style: { stroke: "#262626", strokeWidth: 1 } },
    { id: "m-e2-3", source: "m-02", target: "m-03", style: { stroke: "#262626", strokeWidth: 1 } },
    { id: "m-e3-4", source: "m-03", target: "m-04", style: { stroke: "#262626", strokeWidth: 1 } },
    { id: "m-e3-5", source: "m-03", target: "m-05", style: { stroke: "#262626", strokeWidth: 1 } },
    { id: "m-e4-7", source: "m-04", target: "m-07", style: { stroke: "#262626", strokeWidth: 1 } },
    { id: "m-e7-13", source: "m-07", target: "m-13", style: { stroke: "#262626", strokeWidth: 1 } },
  ];
  return { nodes, edges };
}

function buildDesignCanvas(): { nodes: Node[]; edges: Edge[] } {
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

export default function V2Preview() {
  const [mode, setMode] = useState<Mode>("monitoring");
  const nodeTypes = useMemo(() => ({ jobV2: JobNodeV2 }), []);
  const monitoring = useMemo(buildMonitoringCanvas, []);
  const design = useMemo(buildDesignCanvas, []);
  const current = mode === "monitoring" ? monitoring : design;

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

        {/* Mode switcher */}
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
              onClick={() => setMode(m)}
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
          <span><span style={{ color: "var(--v2-status-ok)" }}>●</span> 5</span>
          <span><span style={{ color: "var(--v2-status-running)" }}>●</span> 3</span>
          <span><span style={{ color: "var(--v2-status-failed)" }}>●</span> 1</span>
          <span><span style={{ color: "var(--v2-status-waiting)" }}>●</span> 6</span>
        </div>
      </header>

      {/* Stage: canvas + sidebar flutuante */}
      <main style={{ flex: 1, position: "relative", minHeight: 0 }}>
        <ReactFlow
          nodes={current.nodes}
          edges={current.edges}
          nodeTypes={nodeTypes}
          fitView
          fitViewOptions={{ padding: 0.2 }}
          proOptions={{ hideAttribution: true }}
          nodesDraggable={false}
          nodesConnectable={false}
          elementsSelectable={mode === "design"}
          panOnDrag
          zoomOnScroll
        >
          <Background variant={BackgroundVariant.Dots} gap={18} size={1} color="#1a1a1a" />
        </ReactFlow>

        {mode === "monitoring" ? (
          <MonitoringSidebarV2 jobs={monitoringJobs} />
        ) : (
          <DesignSidebarV2 />
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
        <span>bg #000 · surface #0a0a0a · border #262626</span>
        <span>accent #11C76F / #064E2B</span>
        <span style={{ marginLeft: "auto" }}>{mode} · 2026-04-16</span>
      </footer>

      <style>{`
        @keyframes v2-dot-pulse {
          0%, 100% { opacity: 1; }
          50% { opacity: 0.35; }
        }
        .react-flow__edge-path { stroke: #262626; }
        .react-flow__edge-text { fill: #a3a3a3; }
        .react-flow__edge-textbg { fill: #0a0a0a; }
      `}</style>
    </div>
  );
}
