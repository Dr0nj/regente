import { useEffect, useRef } from "react";

/* ──────────────────────────────────────────────────────────────
   CanvasContextMenu — menu flutuante para right-click em nodes
   do canvas (monitoring + design).
   ──────────────────────────────────────────────────────────────
   - Fecha em ESC, click fora, ou click em item.
   - Items com tone: neutral / primary / danger.
   - Posicionado em coords absolutas (clientX/clientY do MouseEvent).
   - Width fixa, sem viewport overflow handling: usuário pode rolar.
   ────────────────────────────────────────────────────────────── */

export type ContextMenuItem = {
  label: string;
  onClick: () => void;
  tone?: "neutral" | "primary" | "danger";
  shortcut?: string;
  disabled?: boolean;
};

export interface CanvasContextMenuProps {
  x: number;
  y: number;
  items: ContextMenuItem[];
  onClose: () => void;
}

const TONE_COLOR: Record<NonNullable<ContextMenuItem["tone"]>, string> = {
  neutral: "var(--v2-text-primary)",
  primary: "var(--v2-accent-brand)",
  danger:  "#fca5a5",
};

export function CanvasContextMenu({ x, y, items, onClose }: CanvasContextMenuProps) {
  const ref = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    function onDocClick(e: MouseEvent) {
      if (!ref.current) return;
      if (!ref.current.contains(e.target as Node)) onClose();
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    // capture phase for mousedown so the menu closes BEFORE another
    // right-click handler tries to re-open it on a different node.
    document.addEventListener("mousedown", onDocClick, true);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDocClick, true);
      document.removeEventListener("keydown", onKey);
    };
  }, [onClose]);

  return (
    <div
      ref={ref}
      onContextMenu={(e) => e.preventDefault()}
      style={{
        position: "fixed",
        left: x,
        top: y,
        minWidth: 200,
        background: "var(--v2-bg-elevated)",
        border: "1px solid var(--v2-border-strong)",
        borderRadius: "var(--v2-radius)",
        padding: 4,
        boxShadow: "0 12px 36px rgba(0,0,0,0.45)",
        zIndex: 1000,
        fontFamily: "var(--v2-font-sans)",
        fontSize: "var(--v2-text-sm)",
        userSelect: "none",
      }}
    >
      {items.length === 0 && (
        <div
          style={{
            padding: "8px 10px",
            color: "var(--v2-text-muted)",
            fontStyle: "italic",
          }}
        >
          Nenhuma ação disponível
        </div>
      )}
      {items.map((it, idx) => (
        <button
          key={idx}
          type="button"
          disabled={it.disabled}
          onClick={() => {
            if (it.disabled) return;
            it.onClick();
            onClose();
          }}
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            gap: 16,
            width: "100%",
            padding: "6px 10px",
            background: "transparent",
            border: "none",
            color: it.disabled ? "var(--v2-text-muted)" : TONE_COLOR[it.tone ?? "neutral"],
            cursor: it.disabled ? "not-allowed" : "pointer",
            textAlign: "left",
            borderRadius: "var(--v2-radius-sm)",
            opacity: it.disabled ? 0.55 : 1,
          }}
          onMouseEnter={(e) => {
            if (it.disabled) return;
            (e.currentTarget as HTMLButtonElement).style.background = "var(--v2-bg-surface)";
          }}
          onMouseLeave={(e) => {
            (e.currentTarget as HTMLButtonElement).style.background = "transparent";
          }}
        >
          <span>{it.label}</span>
          {it.shortcut && (
            <span
              style={{
                fontFamily: "var(--v2-font-mono)",
                fontSize: "var(--v2-text-xs)",
                color: "var(--v2-text-muted)",
              }}
            >
              {it.shortcut}
            </span>
          )}
        </button>
      ))}
    </div>
  );
}

export default CanvasContextMenu;
