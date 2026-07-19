import type { CSSProperties } from "react";

// addToggleStyle — o botão ＋ (vira ✕ quando aberto) que fica AO LADO DO TÍTULO e
// revela o campo de digitação. Padrão único das listas "adicionar uma linha" do
// produto (condições, recursos, pools do ambiente): o campo fica ESCONDIDO até
// clicar no ＋ — inclusive para a PRIMEIRA linha —, deixando o workspace limpo.
// `accent` deixa o botão herdar a cor do eixo (âmbar nos recursos, brand nas
// condições). Regra para novas configs desse tipo.
export function addToggleStyle(open: boolean, accent = "var(--v2-accent-brand)"): CSSProperties {
  return {
    width: 20, height: 20, borderRadius: "50%", display: "inline-flex", alignItems: "center", justifyContent: "center",
    background: open ? "var(--v2-accent-deep)" : "transparent",
    border: `1px solid ${open ? accent : "var(--v2-border-medium)"}`,
    color: open ? accent : "var(--v2-text-muted)", cursor: "pointer", padding: 0, flexShrink: 0,
  };
}
