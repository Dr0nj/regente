import type { NodeProps } from "@xyflow/react";

export interface LaneLabelData {
  team: string;
  count: number;
  width: number;
  height: number;
  [key: string]: unknown;
}

/**
 * Container de folder (Control-M smart-folder style).
 * Retângulo visual com header no topo, posicionado atrás dos jobs
 * (zIndex 0). Não-interativo: pan do canvas passa direto.
 *
 * Fundo SÓLIDO de propósito (tinta composta sobre --v2-bg-canvas): os dots do
 * Background do ReactFlow ficam SÓ na área externa do canvas — dentro da
 * folder a cor é chapada (pedido do usuário; antes o rgba ~transparente
 * deixava os dots vazarem pra dentro do retângulo).
 */
export default function LaneLabelNode({ data }: NodeProps) {
  const d = data as LaneLabelData;
  return (
    <div
      style={{
        width: d.width,
        height: d.height,
        background:
          "linear-gradient(0deg, rgba(17,199,111,0.03), rgba(17,199,111,0.03)), var(--v2-bg-canvas)",
        border: "1px solid var(--v2-border-medium)",
        borderRadius: 6,
        position: "relative",
        pointerEvents: "none",
      }}
    >
      <div
        style={{
          position: "absolute",
          top: 0,
          left: 0,
          right: 0,
          height: 28,
          padding: "0 12px",
          borderBottom: "1px solid var(--v2-border-subtle)",
          background: "var(--v2-bg-elevated)",
          borderRadius: "6px 6px 0 0",
          display: "flex",
          alignItems: "center",
          gap: 8,
          userSelect: "none",
        }}
      >
        <span
          style={{
            fontSize: 10,
            fontFamily: "var(--v2-font-mono)",
            letterSpacing: "0.1em",
            color: "var(--v2-text-primary)",
            textTransform: "uppercase",
            fontWeight: 600,
          }}
        >
          {d.team}
        </span>
        <span
          style={{
            fontSize: 9,
            fontFamily: "var(--v2-font-mono)",
            color: "var(--v2-text-muted)",
            padding: "1px 5px",
            border: "1px solid var(--v2-border-subtle)",
            borderRadius: 2,
            letterSpacing: "0.06em",
          }}
        >
          {d.count}
        </span>
      </div>
    </div>
  );
}
