/**
 * EdgeConditionModal — escolha da condição de dependência ao conectar dois
 * jobs no canvas (substitui o window.prompt antigo).
 *
 * Semântica Control-M: a edge representa a condição que o job sucessor
 * espera do predecessor.
 */
import { useEffect, useState } from "react";
import { CheckCircle2, XCircle, CircleDot, Infinity as InfinityIcon } from "lucide-react";
import type { EdgeCondition } from "@/lib/orchestrator-model";

const OPTIONS: Array<{
  value: EdgeCondition;
  label: string;
  hint: string;
  Icon: typeof CheckCircle2;
}> = [
  { value: "on-success",  label: "On Success",  hint: "Roda quando o predecessor terminar OK (padrão Control-M)", Icon: CheckCircle2 },
  { value: "on-failure",  label: "On Failure",  hint: "Roda quando o predecessor terminar NOTOK",                 Icon: XCircle },
  { value: "on-complete", label: "On Complete", hint: "Roda quando terminar, OK ou NOTOK",                        Icon: CircleDot },
  { value: "always",      label: "Always",      hint: "Sem gate — apenas ordenação visual",                       Icon: InfinityIcon },
];

interface Props {
  fromLabel: string;
  toLabel: string;
  onConfirm: (condition: EdgeCondition) => void;
  onCancel: () => void;
}

export default function EdgeConditionModal({ fromLabel, toLabel, onConfirm, onCancel }: Props) {
  const [selected, setSelected] = useState<EdgeCondition>("on-success");

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onCancel();
      if (e.key === "Enter") onConfirm(selected);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [selected, onConfirm, onCancel]);

  return (
    <div
      onClick={onCancel}
      style={{
        position: "fixed", inset: 0, zIndex: 90,
        background: "rgba(0,0,0,0.6)",
        display: "flex", alignItems: "center", justifyContent: "center",
      }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          width: 420,
          background: "var(--v2-bg-elevated)",
          border: "1px solid var(--v2-border-strong)",
          borderRadius: 6,
          fontFamily: "var(--v2-font-sans)",
          overflow: "hidden",
        }}
      >
        <div style={{ padding: "12px 16px", borderBottom: "1px solid var(--v2-border-subtle)" }}>
          <div style={{ fontSize: 12, fontWeight: 600, color: "var(--v2-text-primary)" }}>
            Condição da dependência
          </div>
          <div style={{ fontSize: 11, color: "var(--v2-text-secondary)", marginTop: 4, fontFamily: "var(--v2-font-mono)" }}>
            {fromLabel} <span style={{ color: "var(--v2-text-muted)" }}>→</span> {toLabel}
          </div>
        </div>

        <div style={{ padding: 8, display: "flex", flexDirection: "column", gap: 4 }}>
          {OPTIONS.map((opt) => {
            const active = selected === opt.value;
            const Icon = opt.Icon;
            return (
              <button
                key={opt.value}
                onClick={() => setSelected(opt.value)}
                onDoubleClick={() => onConfirm(opt.value)}
                style={{
                  display: "flex", alignItems: "center", gap: 10,
                  padding: "9px 10px",
                  background: active ? "var(--v2-accent-deep)" : "transparent",
                  border: `1px solid ${active ? "var(--v2-accent-dark)" : "transparent"}`,
                  borderRadius: 4,
                  cursor: "pointer",
                  textAlign: "left",
                  width: "100%",
                }}
              >
                <Icon size={15} style={{ color: active ? "var(--v2-accent-brand)" : "var(--v2-text-muted)", flexShrink: 0 }} />
                <div style={{ minWidth: 0 }}>
                  <div style={{ fontSize: 12, fontWeight: 600, color: active ? "var(--v2-accent-brand)" : "var(--v2-text-primary)" }}>
                    {opt.label}
                  </div>
                  <div style={{ fontSize: 10.5, color: "var(--v2-text-secondary)", marginTop: 1, lineHeight: 1.35 }}>
                    {opt.hint}
                  </div>
                </div>
              </button>
            );
          })}
        </div>

        <div style={{ padding: "10px 16px", borderTop: "1px solid var(--v2-border-subtle)", display: "flex", gap: 8, justifyContent: "flex-end" }}>
          <button
            onClick={onCancel}
            style={{
              padding: "5px 12px", background: "transparent",
              border: "1px solid var(--v2-border-medium)", borderRadius: 3,
              color: "var(--v2-text-secondary)", fontSize: 11, cursor: "pointer",
            }}
          >
            Cancelar (Esc)
          </button>
          <button
            onClick={() => onConfirm(selected)}
            style={{
              padding: "5px 12px", background: "var(--v2-accent-deep)",
              border: "1px solid var(--v2-accent-brand)", borderRadius: 3,
              color: "var(--v2-accent-brand)", fontSize: 11, fontWeight: 600, cursor: "pointer",
            }}
          >
            Conectar (Enter)
          </button>
        </div>
      </div>
    </div>
  );
}
