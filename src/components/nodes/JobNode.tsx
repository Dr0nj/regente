import { memo } from "react";
import { Handle, Position, type NodeProps, type Node } from "@xyflow/react";
import { STATUS_MAP, type JobNodeData } from "@/lib/job-config";
import type { AppMode } from "@/lib/types";

/* ──────────────────────────────────────────────────────────────
   JobNode ── identidade PicPay, densidade Control-M/Airflow
   ──────────────────────────────────────────────────────────────
   - Zero gradiente por tipo (tipo é texto mono, não cor)
   - Zero noise overlay, zero glassmorphism
   - Única animação: dot pulse em RUNNING (semântica)
   - Paleta: preto + verde escuro PicPay, status ≤ 5 cores
   - Densidade: 200×52px
   ────────────────────────────────────────────────────────────── */

type JobNodeT = Node<JobNodeData, "job">;

const STATUS_COLOR_VAR: Record<string, string> = {
  SUCCESS:  "var(--color-status-success)",
  RUNNING:  "var(--color-status-running)",
  FAILED:   "var(--color-status-failed)",
  WAITING:  "var(--color-status-waiting)",
  INACTIVE: "var(--color-status-inactive)",
};

const STATUS_SHORT: Record<string, string> = {
  SUCCESS:  "OK",
  RUNNING:  "RUN",
  FAILED:   "FAIL",
  WAITING:  "WAIT",
  INACTIVE: "IDLE",
};

function JobNodeComponent({ data, selected, id }: NodeProps<JobNodeT>) {
  const status = data.status;
  const statusColor = STATUS_COLOR_VAR[status] ?? "var(--color-text-muted)";
  const statusShort = STATUS_SHORT[status] ?? status;
  const statusLabel = STATUS_MAP[status]?.label ?? status;
  const mode = (data.mode ?? "design") as AppMode;
  const isRunning = mode === "monitoring" && status === "RUNNING";

  return (
    <div
      data-label={data.label}
      data-id={id}
      className="regente-node"
      style={{
        width: 200,
        background: "var(--color-bg-surface)",
        border: `1px solid ${selected ? "var(--color-accent-dark)" : "var(--color-border-medium)"}`,
        borderRadius: 4,
        fontFamily: "Inter, -apple-system, system-ui, sans-serif",
        overflow: "hidden",
        display: "flex",
      }}
    >
      <div
        aria-label={`status: ${statusLabel}`}
        style={{ width: 3, background: statusColor, flexShrink: 0 }}
      />

      <div style={{ flex: 1, padding: "8px 10px", minWidth: 0 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
          <span
            style={{
              fontSize: 13,
              fontWeight: 600,
              color: "var(--color-text-primary)",
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
                fontSize: 10,
                color: "var(--color-text-muted)",
                fontFamily: "JetBrains Mono, SF Mono, Consolas, monospace",
                padding: "1px 4px",
                border: "1px solid var(--color-border-subtle)",
                borderRadius: 2,
                flexShrink: 0,
                letterSpacing: "0.04em",
              }}
            >
              {data.team}
            </span>
          )}
        </div>

        <div
          style={{
            display: "flex",
            alignItems: "center",
            gap: 6,
            marginTop: 3,
            fontSize: 10,
            fontFamily: "JetBrains Mono, SF Mono, Consolas, monospace",
            color: "var(--color-text-muted)",
            letterSpacing: "0.04em",
          }}
        >
          <span style={{ textTransform: "uppercase" }}>{data.jobType}</span>
          <span style={{ color: "var(--color-border-strong)" }}>│</span>
          <span
            style={{
              color: statusColor,
              display: "inline-flex",
              alignItems: "center",
              gap: 4,
              fontWeight: 600,
            }}
          >
            <span
              className={isRunning ? "animate-pulse-dot" : ""}
              style={{
                width: 6,
                height: 6,
                borderRadius: "50%",
                background: statusColor,
              }}
            />
            {statusShort}
          </span>
          {data.lastRun && (
            <>
              <span style={{ color: "var(--color-border-strong)" }}>│</span>
              <span>{data.lastRun}</span>
            </>
          )}
        </div>
      </div>

      <Handle type="target" position={Position.Top} className="!-top-[4px]" />
      <Handle type="source" position={Position.Bottom} className="!-bottom-[4px]" />

      {data.jobType === "CHOICE" && (
        <>
          <Handle type="source" position={Position.Right} id="choice-right" className="!-right-[4px] !top-1/2" />
          <Handle type="source" position={Position.Left} id="choice-left" className="!-left-[4px] !top-1/2" />
        </>
      )}
    </div>
  );
}

export default memo(JobNodeComponent);