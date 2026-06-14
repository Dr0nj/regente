/**
 * OutputModal.tsx — F11.5 view de output de uma instance.
 *
 * Renderiza stdout+stderr (campo `output` da instance) em fonte mono,
 * com copy-to-clipboard e download .txt. Trunca em 1MB com aviso.
 */
import { useCallback, useEffect, useMemo } from "react";
import type { JobInstance } from "@/lib/orchestrator-model";

const MAX_SIZE = 1024 * 1024;

interface Props {
  instance: JobInstance;
  onClose: () => void;
}

export default function OutputModal({ instance, onClose }: Props) {
  useEffect(() => {
    const onEsc = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onEsc);
    return () => window.removeEventListener("keydown", onEsc);
  }, [onClose]);

  const raw = useMemo(() => {
    const o = instance.output as unknown;
    if (typeof o === "string") return o;
    if (o === undefined || o === null) return "";
    try {
      return JSON.stringify(o, null, 2);
    } catch {
      return String(o);
    }
  }, [instance.output]);

  const { text, truncated } = useMemo(() => {
    if (raw.length <= MAX_SIZE) return { text: raw, truncated: false };
    return { text: raw.slice(0, MAX_SIZE), truncated: true };
  }, [raw]);

  const copy = useCallback(() => {
    void navigator.clipboard.writeText(raw).catch(() => {});
  }, [raw]);

  const download = useCallback(() => {
    const blob = new Blob([raw], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${instance.id}.txt`;
    a.click();
    URL.revokeObjectURL(url);
  }, [raw, instance.id]);

  return (
    <div
      onClick={onClose}
      style={{
        position: "fixed",
        inset: 0,
        background: "rgba(0,0,0,0.6)",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        zIndex: 900,
      }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          width: "min(920px, 92vw)",
          height: "min(640px, 80vh)",
          background: "var(--v2-bg-surface)",
          border: "1px solid var(--v2-border-medium)",
          borderRadius: 6,
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
        }}
      >
        <header
          style={{
            padding: "10px 14px",
            borderBottom: "1px solid var(--v2-border-subtle)",
            display: "flex",
            alignItems: "center",
            gap: 12,
            flexShrink: 0,
            background: "var(--v2-bg-elevated)",
          }}
        >
          <div style={{ flex: 1, display: "flex", flexDirection: "column", gap: 2, minWidth: 0 }}>
            <span style={{ fontSize: 12, fontWeight: 600, color: "var(--v2-text-primary)" }}>
              {instance.label}
            </span>
            <span
              style={{
                fontSize: 10,
                fontFamily: "var(--v2-font-mono)",
                color: "var(--v2-text-muted)",
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
              }}
            >
              {instance.id} · {instance.status}
            </span>
          </div>
          <button
            onClick={copy}
            style={btnStyle()}
          >
            Copy
          </button>
          <button onClick={download} style={btnStyle()}>
            Download
          </button>
          <button onClick={onClose} style={btnStyle()}>
            Close
          </button>
        </header>
        <pre
          style={{
            flex: 1,
            margin: 0,
            padding: 14,
            overflow: "auto",
            fontSize: 11,
            fontFamily: "var(--v2-font-mono)",
            color: "var(--v2-text-primary)",
            background: "var(--v2-bg-canvas)",
            lineHeight: 1.5,
            whiteSpace: "pre-wrap",
            wordBreak: "break-word",
          }}
        >
          {text || "(no output)"}
        </pre>
        {truncated && (
          <div
            style={{
              padding: "6px 14px",
              fontSize: 10,
              fontFamily: "var(--v2-font-mono)",
              color: "var(--v2-status-hold)",
              background: "var(--v2-bg-elevated)",
              borderTop: "1px solid var(--v2-border-subtle)",
              flexShrink: 0,
            }}
          >
            Output truncated at 1MB — use Download for full content.
          </div>
        )}
      </div>
    </div>
  );
}

function btnStyle(): React.CSSProperties {
  return {
    padding: "4px 10px",
    background: "transparent",
    border: "1px solid var(--v2-border-medium)",
    color: "var(--v2-text-primary)",
    borderRadius: 3,
    fontSize: 10,
    fontFamily: "var(--v2-font-mono)",
    letterSpacing: "0.06em",
    textTransform: "uppercase",
    cursor: "pointer",
    fontWeight: 600,
  };
}
