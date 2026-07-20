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
// WAIT CONFIRM — violeta: WAITING preso no gate Control-M "Wait for confirmation",
// aguardando o operador confirmar. Card inteiro tinge de violeta + tag CONFIRM.
const WAIT_CONFIRM_COLOR = "#a78bfa";
// WAIT RESOURCE — âmbar: WAITING sem unidade livre de um recurso/quota (F15).
// Mesma cor do bloco Recursos do drawer e do painel Recursos do Monitoring.
const WAIT_RESOURCE_COLOR = "#f59e0b";

function JobNodeV2Component({ data, selected }: NodeProps<JobNodeV2>) {
  const waitConfirm = data.status === "WAITING" && !!data.waitConfirm;
  const waitAgent = data.status === "WAITING" && !!data.waitAgent;
  const waitResource = data.status === "WAITING" && !!data.waitResource;
  // Prioridade visual segue a ordem dos gates: confirm (humano) > agente >
  // recurso. WAIT COND não recolore (usa a cor de WAITING), só troca o rótulo.
  const statusColor = waitConfirm
    ? WAIT_CONFIRM_COLOR
    : waitAgent
    ? WAIT_AGENT_COLOR
    : waitResource
    ? WAIT_RESOURCE_COLOR
    : STATUS_COLOR[data.status];
  const isRunning = data.status === "RUNNING";

  return (
    <div
      className="v2-grain-card v2-edge-highlight"
      style={{
        position: "relative",
        width: 200,
        // WAIT CONFIRM tinge o card inteiro de violeta (composto sobre a surface
        // pra manter a opacidade dos demais cards). Só quando aguardando confirmar.
        background: waitConfirm
          ? "linear-gradient(0deg, rgba(167,139,250,0.14), rgba(167,139,250,0.14)), var(--v2-bg-surface)"
          : "var(--v2-bg-surface)",
        // Selecionado = borda + halo NEON na cor do tema (--v2-accent-brand/glow).
        // Vale para 1 ou N jobs: cada card selecionado acende sozinho, em vez de
        // uma caixa azul arrastada sobre a área inteira (essa foi neutralizada no
        // .react-flow__nodesselection-rect em index.css). Funciona em Design e
        // Monitoring — ambos usam o `selected` nativo do ReactFlow.
        // Seleção vence o violeta do confirm (neon > tinta de estado).
        border: `1px solid ${
          selected ? "var(--v2-accent-brand)" : waitConfirm ? WAIT_CONFIRM_COLOR : "var(--v2-border-medium)"
        }`,
        boxShadow: selected
          ? "0 0 0 1px var(--v2-accent-brand), 0 0 16px 2px var(--v2-accent-glow), inset 0 1px 0 rgba(255,255,255,0.04)"
          : waitConfirm
          ? "0 0 0 1px rgba(167,139,250,0.45), 0 0 14px 1px rgba(167,139,250,0.25), inset 0 1px 0 rgba(255,255,255,0.04)"
          : undefined,
        borderRadius: "var(--v2-radius)",
        fontFamily: "var(--v2-font-sans)",
        overflow: "hidden",
        transition: "box-shadow 120ms ease, border-color 120ms ease",
      }}
    >
      {/* HOLD — cadeado sobreposto no canto superior esquerdo, fundo transparente.
          HOLD colapsa para INACTIVE na cor de status (igual a CANCELLED); o cadeado
          é o que diferencia "segurado" de "ocioso/cancelado". Tinta ÂMBAR quando o
          hold veio de uma PAUSA DE FOLDER (não liberável 1-a-1, só pelo resume da
          folder); VIOLETA quando é um hold individual do operador. */}
      {data.held && (
        <div
          title={data.folderHeld
            ? "Em HOLD pela pausa da folder — só libera pelo Retomar da folder inteira, não individualmente"
            : "Em HOLD — segurado manualmente por um operador; não roda até um Release"}
          style={{
            position: "absolute",
            top: 3,
            left: 5,
            zIndex: 2,
            display: "flex",
            pointerEvents: "none",
          }}
        >
          <Lock size={11} strokeWidth={2.5} color={data.folderHeld ? "#f59e0b" : "#c4b5fd"} />
        </div>
      )}
      {/* Status bar — faixa colorida à esquerda, não gradiente */}
      <div style={{ display: "flex" }}>
        <div
          style={{
            width: 6,
            background: statusColor,
            flexShrink: 0,
          }}
        />
        <div style={{ flex: 1, padding: "8px 10px", minWidth: 0 }}>
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
                  title="Colocado na mão — Order Force (Design): ordem nova fora do agendamento, respeitando os gates de runtime (janela/condições/agente/recursos). Run Now não marca nada."
                  style={{
                    fontSize: "var(--v2-text-xs)",
                    fontFamily: "var(--v2-font-mono)",
                    fontWeight: 700,
                    letterSpacing: "0.08em",
                    color: "#fbbf24",
                    padding: "0px 4px",
                    border: "1px solid #78350f",
                    background: "rgba(251,191,36,0.08)",
                    borderRadius: "var(--v2-radius-sm)",
                    flexShrink: 0,
                  }}
                >
                  🖐 MANUAL
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
                    padding: "0px 4px",
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
          )}

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
              {/* Control-M parity: aguardando confirmação lê "CONFIRM" (violeta);
                  preso por condição de entrada ausente do pool lê "WAIT COND"
                  (modelo único de dependências); sem agente online lê
                  "WAIT AGENT" (azul claro); "WAIT" = horário. */}
              {waitConfirm ? "CONFIRM"
                : waitAgent ? "WAIT AGENT"
                : data.status === "WAITING" && data.waitEvent ? "WAIT COND"
                : waitResource ? "WAIT RESOURCE"
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
