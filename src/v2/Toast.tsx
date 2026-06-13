/**
 * Toast — feedback de ação não-bloqueante (substitui window.alert).
 *
 * Uso:
 *   import { toast, ToastHost } from "./Toast";
 *   toast.success("Publicado", { detail: "commit abc1234", linkUrl, linkLabel });
 *   <ToastHost />  // uma vez, no root
 *
 * Sem dependências: emitter de módulo + host que renderiza a pilha.
 */
import { useEffect, useState } from "react";
import { CheckCircle2, AlertCircle, Info, X, ExternalLink } from "lucide-react";

export type ToastKind = "success" | "error" | "info";

export interface ToastItem {
  id: number;
  kind: ToastKind;
  title: string;
  detail?: string;
  linkUrl?: string;
  linkLabel?: string;
}

type ToastInput = Omit<ToastItem, "id" | "kind">;

let nextId = 1;
const listeners = new Set<(t: ToastItem) => void>();

function emit(kind: ToastKind, title: string, opts?: Partial<ToastInput>) {
  const item: ToastItem = { id: nextId++, kind, title, ...opts };
  for (const fn of listeners) {
    try { fn(item); } catch { /* ignore */ }
  }
}

export const toast = {
  success: (title: string, opts?: Partial<ToastInput>) => emit("success", title, opts),
  error:   (title: string, opts?: Partial<ToastInput>) => emit("error", title, opts),
  info:    (title: string, opts?: Partial<ToastInput>) => emit("info", title, opts),
};

const KIND_STYLE: Record<ToastKind, { fg: string; border: string; Icon: typeof Info }> = {
  success: { fg: "var(--v2-status-ok)",     border: "rgba(17,199,111,.35)", Icon: CheckCircle2 },
  error:   { fg: "var(--v2-status-failed)", border: "rgba(239,68,68,.4)",   Icon: AlertCircle },
  info:    { fg: "var(--v2-text-secondary)", border: "var(--v2-border-strong)", Icon: Info },
};

const TTL_MS: Record<ToastKind, number> = { success: 5000, info: 5000, error: 9000 };

export function ToastHost() {
  const [items, setItems] = useState<ToastItem[]>([]);

  useEffect(() => {
    const onToast = (t: ToastItem) => {
      setItems((prev) => [...prev.slice(-4), t]); // máx 5 simultâneos
      window.setTimeout(() => {
        setItems((prev) => prev.filter((i) => i.id !== t.id));
      }, TTL_MS[t.kind]);
    };
    listeners.add(onToast);
    return () => { listeners.delete(onToast); };
  }, []);

  if (items.length === 0) return null;

  return (
    <div
      style={{
        position: "fixed",
        right: 16,
        bottom: 36,
        display: "flex",
        flexDirection: "column",
        gap: 8,
        zIndex: 100,
        maxWidth: 380,
      }}
    >
      {items.map((t) => {
        const s = KIND_STYLE[t.kind];
        const Icon = s.Icon;
        return (
          <div
            key={t.id}
            style={{
              display: "flex",
              alignItems: "flex-start",
              gap: 10,
              padding: "10px 12px",
              background: "var(--v2-bg-elevated)",
              border: `1px solid ${s.border}`,
              borderRadius: 4,
              boxShadow: "0 8px 24px rgba(0,0,0,0.5)",
              fontFamily: "var(--v2-font-sans)",
            }}
          >
            <Icon size={15} style={{ color: s.fg, flexShrink: 0, marginTop: 1 }} />
            <div style={{ minWidth: 0, flex: 1 }}>
              <div style={{ fontSize: 12, fontWeight: 600, color: "var(--v2-text-primary)", lineHeight: 1.35 }}>
                {t.title}
              </div>
              {t.detail && (
                <div style={{ fontSize: 11, color: "var(--v2-text-secondary)", marginTop: 2, lineHeight: 1.4, wordBreak: "break-word" }}>
                  {t.detail}
                </div>
              )}
              {t.linkUrl && (
                <a
                  href={t.linkUrl}
                  target="_blank"
                  rel="noreferrer"
                  style={{
                    display: "inline-flex", alignItems: "center", gap: 4,
                    fontSize: 11, color: "var(--v2-accent-brand)", marginTop: 4,
                    textDecoration: "none", fontFamily: "var(--v2-font-mono)",
                  }}
                >
                  <ExternalLink size={11} />
                  {t.linkLabel ?? t.linkUrl}
                </a>
              )}
            </div>
            <button
              onClick={() => setItems((prev) => prev.filter((i) => i.id !== t.id))}
              style={{
                background: "transparent", border: "none", cursor: "pointer",
                color: "var(--v2-text-muted)", padding: 0, flexShrink: 0,
              }}
              title="Fechar"
            >
              <X size={13} />
            </button>
          </div>
        );
      })}
    </div>
  );
}
