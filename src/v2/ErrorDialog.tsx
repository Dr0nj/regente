/**
 * ErrorDialog — modal de erro humano. Mostra título amigável,
 * dica acionável e detalhe técnico colapsável.
 */
import { useState } from "react";
import { classifyError } from "@/lib/error-format";

interface Props {
  error: unknown;
  onClose: () => void;
}

export function ErrorDialog({ error, onClose }: Props) {
  const c = classifyError(error);
  const [showRaw, setShowRaw] = useState(false);

  return (
    <div
      onClick={onClose}
      style={{
        position: "fixed", inset: 0, background: "rgba(0,0,0,0.6)",
        display: "flex", alignItems: "center", justifyContent: "center", zIndex: 1000,
      }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          background: "var(--v2-bg-surface, #1a1a1a)",
          border: "1px solid var(--v2-border-medium, #333)",
          borderLeft: "4px solid var(--v2-status-failed, #ef4444)",
          borderRadius: 6, width: 480, maxWidth: "90vw", maxHeight: "80vh",
          display: "flex", flexDirection: "column", overflow: "hidden",
          fontFamily: "var(--v2-font-sans, system-ui)",
          boxShadow: "0 12px 40px rgba(0,0,0,0.5)",
        }}
      >
        <div style={{ padding: "14px 18px", borderBottom: "1px solid var(--v2-border-subtle, #2a2a2a)", display: "flex", alignItems: "center", gap: 10 }}>
          <div style={{ width: 24, height: 24, borderRadius: "50%", background: "rgba(239,68,68,0.18)", color: "var(--v2-status-failed, #ef4444)", display: "flex", alignItems: "center", justifyContent: "center", fontWeight: 700, fontSize: 14 }}>!</div>
          <div style={{ fontSize: 14, fontWeight: 600, color: "var(--v2-text-primary, #eee)", flex: 1 }}>{c.title}</div>
          <button onClick={onClose} title="Fechar" style={{ background: "transparent", border: "none", color: "var(--v2-text-secondary, #888)", cursor: "pointer", fontSize: 18, padding: 0, width: 22, height: 22 }}>×</button>
        </div>

        <div style={{ padding: "14px 18px", overflowY: "auto", display: "flex", flexDirection: "column", gap: 12 }}>
          {c.hint && (
            <div style={{ fontSize: 12.5, lineHeight: 1.5, color: "var(--v2-text-primary, #ddd)", whiteSpace: "pre-wrap" }}>
              {c.hint}
            </div>
          )}

          <div>
            <button
              onClick={() => setShowRaw((v) => !v)}
              style={{ background: "transparent", border: "1px solid var(--v2-border-subtle, #333)", color: "var(--v2-text-secondary, #888)", padding: "3px 8px", borderRadius: 3, fontSize: 10, cursor: "pointer", letterSpacing: "0.06em", textTransform: "uppercase" }}
            >
              {showRaw ? "▾ Ocultar detalhe técnico" : "▸ Detalhe técnico"}
            </button>
            {showRaw && (
              <pre style={{ marginTop: 8, padding: 10, background: "var(--v2-bg-canvas, #0d0d0d)", border: "1px solid var(--v2-border-subtle, #2a2a2a)", borderRadius: 3, fontSize: 11, color: "var(--v2-text-secondary, #aaa)", fontFamily: "var(--v2-font-mono, monospace)", whiteSpace: "pre-wrap", wordBreak: "break-word", margin: "8px 0 0" }}>
                {c.status ? `HTTP ${c.status}\n` : ""}{c.detail}
              </pre>
            )}
          </div>
        </div>

        <div style={{ padding: "10px 18px", borderTop: "1px solid var(--v2-border-subtle, #2a2a2a)", display: "flex", justifyContent: "flex-end" }}>
          <button onClick={onClose} style={{ background: "var(--v2-accent-brand, #2a6)", color: "#fff", border: "none", padding: "6px 16px", borderRadius: 4, cursor: "pointer", fontSize: 12, fontWeight: 600 }}>OK</button>
        </div>
      </div>
    </div>
  );
}
