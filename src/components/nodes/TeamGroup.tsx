import { memo } from "react";
import type { NodeProps, Node } from "@xyflow/react";

export interface TeamGroupData {
  [key: string]: unknown;
  label: string;
  color: string;
  jobCount: number;
  groupWidth: number;
  groupHeight: number;
}

type TeamGroupNode = Node<TeamGroupData, "teamGroup">;

const TEAM_COLORS: Record<string, { border: string; bg: string; text: string; badge: string }> = {
  "#22d3ee": { border: "rgba(34,211,238,0.35)", bg: "rgba(34,211,238,0.06)", text: "#67e8f9", badge: "rgba(34,211,238,0.18)" },
  "#a855f7": { border: "rgba(168,85,247,0.35)", bg: "rgba(168,85,247,0.06)", text: "#c084fc", badge: "rgba(168,85,247,0.18)" },
  "#f59e0b": { border: "rgba(245,158,11,0.35)", bg: "rgba(245,158,11,0.06)", text: "#fbbf24", badge: "rgba(245,158,11,0.18)" },
  "#10b981": { border: "rgba(16,185,129,0.35)", bg: "rgba(16,185,129,0.06)", text: "#34d399", badge: "rgba(16,185,129,0.18)" },
  "#f43f5e": { border: "rgba(244,63,94,0.35)", bg: "rgba(244,63,94,0.06)", text: "#fb7185", badge: "rgba(244,63,94,0.18)" },
};

function TeamGroupComponent({ data }: NodeProps<TeamGroupNode>) {
  const palette = TEAM_COLORS[data.color] ?? TEAM_COLORS["#22d3ee"];

  return (
    <div
      style={{
        width: data.groupWidth,
        height: data.groupHeight,
        borderRadius: 16,
        border: `2px dashed ${palette.border}`,
        background: palette.bg,
        position: "relative",
        boxShadow: `inset 0 0 40px ${palette.bg}, 0 0 20px ${palette.bg}`,
        pointerEvents: "none",
      }}
    >
      {/* Team label badge */}
      <div
        style={{
          position: "absolute",
          top: -14,
          left: 16,
          background: `linear-gradient(135deg, ${palette.badge}, rgba(10,15,28,0.9))`,
          border: `1.5px solid ${palette.border}`,
          borderRadius: 8,
          padding: "3px 12px",
          display: "flex",
          alignItems: "center",
          gap: 8,
          backdropFilter: "blur(8px)",
        }}
      >
        <span style={{ fontSize: 11, fontWeight: 700, color: palette.text, letterSpacing: "0.08em", textTransform: "uppercase" }}>
          {data.label}
        </span>
        <span style={{ fontSize: 10, color: "rgba(148,163,184,0.8)", fontWeight: 500 }}>
          {data.jobCount} jobs
        </span>
      </div>
    </div>
  );
}

export default memo(TeamGroupComponent);
