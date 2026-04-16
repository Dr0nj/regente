import { memo } from "react";
import { Handle, Position, type NodeProps, type Node } from "@xyflow/react";
import type { JobNodeData, JobStatus } from "@/lib/job-config";

/* ──────────────────────────────────────────────────────────────
   JobNodeV2 — identidade PicPay, densidade Control-M/Airflow
   ──────────────────────────────────────────────────────────────
   Princípios aplicados:
   - Zero gradiente por tipo de job (tipo é texto, não cor)
   - Zero noise overlay, zero glassmorphism decorativo
   - Zero animação não-semântica (só dot pulse em RUNNING)
   - Paleta ≤ 5 cores de status; resto é neutro
   - Densidade: 180×52px (vs 240×110px do v1)
   - Tipografia: mono para ID, sans para label
   ────────────────────────────────────────────────────────────── */

type JobNodeV2 = Node<JobNodeData, "jobV2">;

const STATUS_COLOR: Record<JobStatus, string> = {
  SUCCESS: "var(--v2-status-ok)",
  RUNNING: "var(--v2-status-running)",
  FAILED: "var(--v2-status-failed)",
  WAITING: "var(--v2-status-waiting)",
  INACTIVE: "var(--v2-text-muted)",
};

const STATUS_LABEL: Record<JobStatus, string> = {
  SUCCESS: "OK",
  RUNNING: "RUN",
  FAILED: "FAIL",
  WAITING: "WAIT",
  INACTIVE: "IDLE",
};

function JobNodeV2Component({ data, selected }: NodeProps<JobNodeV2>) {
  const statusColor = STATUS_COLOR[data.status];
  const isRunning = data.status === "RUNNING";

  return (
    <div
      style={{
        width: 200,
        background: "var(--v2-bg-surface)",
        border: `1px solid ${selected ? "var(--v2-accent-dark)" : "var(--v2-border-medium)"}`,
        borderRadius: "var(--v2-radius)",
        fontFamily: "var(--v2-font-sans)",
        overflow: "hidden",
      }}
    >
      {/* Status bar — 2px colorida à esquerda, não gradiente */}
      <div style={{ display: "flex" }}>
        <div
          style={{
            width: 3,
            background: statusColor,
            flexShrink: 0,
          }}
        />
        <div style={{ flex: 1, padding: "8px 10px", minWidth: 0 }}>
          {/* Linha 1: label + team */}
          <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
            <span
              style={{
                fontSize: "var(--v2-text-md)",
                fontWeight: 600,
                color: "var(--v2-text-primary)",
                whiteSpace: "nowrap",
                overflow: "hidden",
                textOverflow: "ellipsis",
                flex: 1,
              }}
            >
              {data.label}
            </span>
            {data.team && (
              <span
                style={{
                  fontSize: "var(--v2-text-xs)",
                  color: "var(--v2-text-muted)",
                  fontFamily: "var(--v2-font-mono)",
                  padding: "1px 4px",
                  border: "1px solid var(--v2-border-subtle)",
                  borderRadius: "var(--v2-radius-sm)",
                  flexShrink: 0,
                }}
              >
                {data.team}
              </span>
            )}
          </div>

          {/* Linha 2: type + status */}
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 6,
              marginTop: 3,
              fontSize: "var(--v2-text-xs)",
              fontFamily: "var(--v2-font-mono)",
              color: "var(--v2-text-muted)",
            }}
          >
            <span style={{ textTransform: "uppercase", letterSpacing: "0.04em" }}>
              {data.jobType}
            </span>
            <span style={{ color: "var(--v2-border-strong)" }}>│</span>
            <span
              style={{
                color: statusColor,
                display: "inline-flex",
                alignItems: "center",
                gap: 4,
                fontWeight: 600,
                letterSpacing: "0.04em",
              }}
            >
              <span
                style={{
                  width: 6,
                  height: 6,
                  borderRadius: "50%",
                  background: statusColor,
                  animation: isRunning ? "v2-dot-pulse 1.2s ease-in-out infinite" : "none",
                }}
              />
              {STATUS_LABEL[data.status]}
            </span>
            {data.lastRun && (
              <>
                <span style={{ color: "var(--v2-border-strong)" }}>│</span>
                <span>{data.lastRun}</span>
              </>
            )}
          </div>
        </div>
      </div>

      <Handle
        type="target"
        position={Position.Top}
        style={{
          background: "var(--v2-border-strong)",
          border: "none",
          width: 6,
          height: 6,
        }}
      />
      <Handle
        type="source"
        position={Position.Bottom}
        style={{
          background: "var(--v2-border-strong)",
          border: "none",
          width: 6,
          height: 6,
        }}
      />
    </div>
  );
}

export default memo(JobNodeV2Component);
