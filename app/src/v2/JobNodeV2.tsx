import { memo } from "react";
import { Handle, Position, type NodeProps, type Node } from "@xyflow/react";
import { Lock } from "lucide-react";
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

// WAIT AGENT — azul claro (sky): WAITING sem agente online pra executar.
const WAIT_AGENT_COLOR = "#38bdf8";

function JobNodeV2Component({ data, selected }: NodeProps<JobNodeV2>) {
  const waitAgent = data.status === "WAITING" && !!data.waitAgent;
  const statusColor = waitAgent ? WAIT_AGENT_COLOR : STATUS_COLOR[data.status];
  const isRunning = data.status === "RUNNING";

  return (
    <div
      className="v2-grain-card v2-edge-highlight"
      style={{
        position: "relative",
        width: 200,
        background: "var(--v2-bg-surface)",
        border: `1px solid ${selected ? "var(--v2-accent-dark)" : "var(--v2-border-medium)"}`,
        borderRadius: "var(--v2-radius)",
        fontFamily: "var(--v2-font-sans)",
        overflow: "hidden",
      }}
    >
      {/* HOLD manual — cadeado sobreposto no canto superior esquerdo, fundo
          transparente. HOLD colapsa para INACTIVE na cor de status (igual a
          CANCELLED); o cadeado é o que diferencia "segurado por operador" de
          "ocioso/cancelado". */}
      {data.held && (
        <div
          title="Em HOLD — segurado manualmente por um operador; não roda até um Release"
          style={{
            position: "absolute",
            top: 3,
            left: 5,
            zIndex: 2,
            display: "flex",
            pointerEvents: "none",
          }}
        >
          <Lock size={11} strokeWidth={2.5} color="#c4b5fd" />
        </div>
      )}
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
          {/* Linha 1: label (sem badge de folder — a folder já é contexto da lane) */}
          {/* paddingLeft reserva o espaço do cadeado sobreposto quando em HOLD,
              pra ele não cobrir as primeiras letras do label. */}
          <div style={{ display: "flex", alignItems: "center", gap: 6, paddingLeft: data.held ? 13 : 0 }}>
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
            {data.forced && (
              <span
                title="Force Order — bypass de deps/cron (Control-M semantics)"
                style={{
                  fontSize: "var(--v2-text-xs)",
                  fontFamily: "var(--v2-font-mono)",
                  fontWeight: 700,
                  letterSpacing: "0.08em",
                  color: "#fbbf24",
                  padding: "1px 4px",
                  border: "1px solid #78350f",
                  background: "rgba(251,191,36,0.08)",
                  borderRadius: "var(--v2-radius-sm)",
                  flexShrink: 0,
                }}
              >
                ⚡FORCED
              </span>
            )}
            {data.dryRun && (
              <span
                title="Dry run — o job entra na daily e 'roda', mas NÃO executa nada (log only)"
                style={{
                  fontSize: "var(--v2-text-xs)",
                  fontFamily: "var(--v2-font-mono)",
                  fontWeight: 700,
                  letterSpacing: "0.08em",
                  color: "#c4b5fd",
                  padding: "1px 4px",
                  border: "1px solid #4c1d95",
                  background: "rgba(196,181,253,0.08)",
                  borderRadius: "var(--v2-radius-sm)",
                  flexShrink: 0,
                }}
              >
                👻GHOST
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
              {/* Control-M parity: WAITING preso por dependência lê "WAIT EVENT";
                  sem agente online lê "WAIT AGENT" (azul claro); "WAIT" = horário. */}
              {waitAgent ? "WAIT AGENT"
                : data.status === "WAITING" && data.waitEvent ? "WAIT EVENT"
                : STATUS_LABEL[data.status]}
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
