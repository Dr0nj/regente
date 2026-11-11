import { GRAPH_SPACING, JOB_WIDTH, JOB_HEIGHT, JOB_TAGGED_HEIGHT } from "./dependency-layout";
import { memo, type CSSProperties } from "react";
import { jobStatusVisual } from "./job-status-visual";
import { recordCanvasMetric } from "./canvas-metrics";
import { Handle, Position, type NodeProps, type Node } from "@xyflow/react";
import { Lock } from "lucide-react";
import type { JobNodeData } from "@/lib/job-config";

// Cards preservam dimensões e geometria em todos os níveis de detalhe.
// A cor semântica é compartilhada com o minimapa; seleção tem contorno próprio.

type JobNodeV2 = Node<JobNodeData, "jobV2">;

function JobNodeV2Component({ data, selected }: NodeProps<JobNodeV2>) {
  recordCanvasMetric("jobRender");
  const visual = jobStatusVisual(data);
  const statusColor = visual.color;
  const relation = data.dependencyRelation;
  const relationColor = relation === "ancestor" ? "var(--job-ancestor)" : relation === "descendant" ? "var(--job-descendant)" : undefined;
  const isRunning = data.status === "RUNNING";

  return (
    <div
      className="v2-job-card"
      data-job-status={visual.status}
      data-selected={selected || undefined}
      aria-label={`${data.label}: ${visual.label}`}
      data-dependency-relation={relation as string | undefined}
      title={`${data.label} · ${visual.label}${relation === "ancestor" ? " · Predecessor of selected job" : relation === "descendant" ? " · Successor of selected job" : ""}`}
      style={{
        position: "relative",
        width: JOB_WIDTH,
        height: data.manualOrder || data.dryRun ? JOB_TAGGED_HEIGHT : JOB_HEIGHT,
        "--job-status-color": statusColor,
        border: `1px solid ${selected ? "var(--v2-accent-brand)" : "var(--v2-border-medium)"}`,
        boxShadow: selected ? "0 0 0 2px var(--v2-accent-brand), 0 0 10px var(--v2-accent-glow)" : undefined,
        outline: !selected && relationColor ? `2px solid ${relationColor}` : undefined,
        outlineOffset: 2,
        borderRadius: "var(--v2-radius)",
        fontFamily: "var(--v2-font-sans)",
        overflow: "hidden",
        transition: "box-shadow 120ms ease, border-color 120ms ease",
      } as CSSProperties}
    >
      {/* Cadeado preserva a origem do HOLD no tooltip. */}
      {data.held && (
        <div
          title={data.folderHeld
            ? "On HOLD by the folder pause — only released by the whole folder's Resume, not individually"
            : "On HOLD — held manually by an operator; does not run until a Release"}
          style={{
            position: "absolute",
            top: 3,
            left: 5,
            zIndex: 2,
            display: "flex",
            pointerEvents: "none",
          }}
        >
          <Lock size={11} strokeWidth={2.5} color="var(--job-hold)" />
        </div>
      )}
      {/* Status bar — faixa colorida à esquerda, não gradiente */}
      <div className="v2-job-content" style={{ display: "flex", height: "100%" }}>
        <div
          style={{
            width: 6,
            background: statusColor,
            flexShrink: 0,
          }}
        />
        <div style={{ flex: 1, padding: GRAPH_SPACING.nodePadding, minWidth: 0 }}>
          {/* Linha 1: label SOZINHO na linha — as tags (FORCED/GHOST) moram numa
              linha própria ABAIXO do nome (pedido do usuário: a quantidade de
              tags espremia o job name até sumir). A tag ✋CONFIRM foi
              DESCONTINUADA: o gate de confirmação já tinge o card inteiro de
              violeta e o status lê "CONFIRM" (bolinha roxa) — a tag era redundante. */}
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
          </div>

          {/* Linha de TAGS — só renderiza quando há tag (senão o card mantém a
              altura compacta de sempre; a célula de layout de 72px absorve a
              linha extra sem colidir com o vizinho). Selos possíveis: 🖐 MANUAL
              (ordem colocada na mão = Order Force) e 👻 GHOST (dry-run). Run Now
              NÃO tem selo — só nudge de uma instance que já existe. */}
          {(data.manualOrder || data.dryRun) && (
            <div style={{ display: "flex", alignItems: "center", gap: 4, marginTop: 2 }}>
              {data.manualOrder && (
                <span
                  title="Placed by hand — Order Force (Design): a new order outside the schedule, honoring the runtime gates (window/conditions/agent/resources). Run Now marks nothing."
                  style={{
                    fontSize: "var(--v2-text-xs)",
                    fontFamily: "var(--v2-font-mono)",
                    fontWeight: 700,
                    letterSpacing: "0.08em",
                    color: "var(--job-hold)",
                    padding: "0px 4px",
                    border: "1px solid var(--job-hold)",
                    background: "color-mix(in srgb, var(--job-hold) 8%, transparent)",
                    borderRadius: "var(--v2-radius-sm)",
                    flexShrink: 0,
                  }}
                >
                  🖐 MANUAL
                </span>
              )}
              {data.dryRun && (
                <span
                  title="Dry run — the job enters the daily and 'runs', but does NOT execute anything (log only)"
                  style={{
                    fontSize: "var(--v2-text-xs)",
                    fontFamily: "var(--v2-font-mono)",
                    fontWeight: 700,
                    letterSpacing: "0.08em",
                    color: "var(--job-confirm)",
                    padding: "0px 4px",
                    border: "1px solid var(--job-confirm)",
                    background: "color-mix(in srgb, var(--job-confirm) 8%, transparent)",
                    borderRadius: "var(--v2-radius-sm)",
                    flexShrink: 0,
                  }}
                >
                  👻GHOST
                </span>
              )}
            </div>
          )}

          {/* Linha 2: type + status */}
          <div className="v2-job-secondary"
            style={{
              display: "flex",
              alignItems: "center",
              gap: 6,
              marginTop: 3,
              fontSize: "var(--v2-text-xs)",
              fontFamily: "var(--v2-font-mono)",
              color: "var(--v2-text-secondary)",
            }}
          >
            <span style={{ textTransform: "uppercase", letterSpacing: "0.04em" }}>
              {data.jobType}
            </span>
            <span style={{ color: "var(--v2-border-strong)" }}>│</span>
            <span
              style={{
                color: "var(--v2-text-primary)",
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
              {/* Control-M parity: aguardando confirmação lê "CONFIRM" (violeta);
                  preso por condição de entrada ausente do pool lê "WAIT COND"
                  (modelo único de dependências); sem agente online lê
                  "WAIT AGENT" (azul claro); "WAIT" = horário. */}
              {visual.label}
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

      {/* Bolinhas de ligação — o nub VISÍVEL segue 6px, mas a caixa real tem
          18px (borda transparente + background-clip): com 6px clicáveis era
          quase impossível INICIAR o arrasto da setinha. O CSS do RF centra o
          handle na borda do card via translate(-50%, ±50%), então crescer a
          caixa não desloca o centro — as âncoras estáticas (JOB_HANDLES em
          canvas-layout) continuam batendo. */}
      <Handle
        type="target"
        position={Position.Top}
        style={{
          background: "var(--v2-border-strong)",
          backgroundClip: "padding-box",
          border: "6px solid transparent",
          width: 18,
          height: 18,
        }}
      />
      <Handle
        type="source"
        position={Position.Bottom}
        style={{
          background: "var(--v2-border-strong)",
          backgroundClip: "padding-box",
          border: "6px solid transparent",
          width: 18,
          height: 18,
        }}
      />
    </div>
  );
}

export default memo(JobNodeV2Component);
