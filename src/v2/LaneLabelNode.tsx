import type { NodeProps } from "@xyflow/react";

export interface LaneLabelData {
  team: string;
  count: number;
  width: number;
  [key: string]: unknown;
}

/**
 * Rótulo de swimlane (não-interativo). Posicionado como node do
 * React Flow para acompanhar pan/zoom do canvas.
 */
export default function LaneLabelNode({ data }: NodeProps) {
  const d = data as LaneLabelData;
  return (
    <div
      style={{
        width: d.width,
        height: 2,
        borderTop: "1px dashed var(--v2-border-subtle)",
        position: "relative",
      }}
    >
      <span
        style={{
          position: "absolute",
          left: 0,
          top: -20,
          padding: "2px 6px",
          background: "var(--v2-bg-elevated)",
          border: "1px solid var(--v2-border-subtle)",
          borderRadius: 3,
          fontSize: 10,
          fontFamily: "var(--v2-font-mono)",
          letterSpacing: "0.08em",
          color: "var(--v2-text-secondary)",
          textTransform: "uppercase",
          userSelect: "none",
        }}
      >
        {d.team}
        <span style={{ marginLeft: 6, color: "var(--v2-text-muted)" }}>{d.count}</span>
      </span>
    </div>
  );
}
