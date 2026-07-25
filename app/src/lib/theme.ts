/**
 * theme.ts — temas da UI.
 *
 * Um tema sobrescreve os tokens `--v2-*` via `:root[data-theme="<id>"]`
 * (definidos em src/v2/tokens.css). "escuro" é o default (sem atributo).
 * A escolha persiste em localStorage e é aplicada no boot (initTheme).
 */

export type ThemeId =
  | "escuro"
  | "claro" | "branco-azul" | "sepia" | "cinza-claro"
  | "verde-amarelo" | "amarelo-ouro" | "verde-mata" | "rosa"
  | "azul-neon" | "azul-escuro" | "violeta" | "vermelho" | "laranja"
  | "cinza" | "bege-escuro" | "marrom";

export interface ThemeDef {
  id: ThemeId;
  name: string;
  desc: string;
  /** Cores do swatch (3 faixas) mostrado no card de seleção do tema. */
  flag: { field: string; rhombus: string; disc: string };
}

/** Temas disponíveis. O 1º é o default (Escuro); seguem as variações coloridas. */
export const THEMES: ThemeDef[] = [
  {
    id: "escuro",
    name: "Dark",
    desc: "Default — black and green",
    flag: { field: "#0a0a0a", rhombus: "#11C76F", disc: "#064E2B" },
  },
  {
    id: "claro",
    name: "Light",
    desc: "Light — off-white background and green",
    flag: { field: "#eef1f3", rhombus: "#047857", disc: "#b4bcc3" },
  },
  {
    id: "branco-azul",
    name: "White Blue",
    desc: "Light — white and blue",
    flag: { field: "#eef2f7", rhombus: "#2563eb", disc: "#aebccc" },
  },
  {
    id: "sepia",
    name: "Sepia",
    desc: "Light — warm paper and amber",
    flag: { field: "#f1eadd", rhombus: "#9a5410", disc: "#bea77f" },
  },
  {
    id: "cinza-claro",
    name: "Light Gray",
    desc: "Light — neutral gray, colored statuses",
    flag: { field: "#eceef0", rhombus: "#475569", disc: "#adb4bb" },
  },
  {
    id: "verde-amarelo",
    name: "Green Yellow",
    desc: "Vibrant green and yellow",
    flag: { field: "#009C3B", rhombus: "#FFDF00", disc: "#0a3d1f" },
  },
  {
    id: "amarelo-ouro",
    name: "Gold Yellow",
    desc: "Gold-yellow accent",
    flag: { field: "#161106", rhombus: "#FFD200", disc: "#C9A100" },
  },
  {
    id: "verde-mata",
    name: "Forest Green",
    desc: "Deep forest green",
    flag: { field: "#03140c", rhombus: "#00C853", disc: "#00813a" },
  },
  {
    id: "azul-neon",
    name: "Neon Blue",
    desc: "Neon blue over a dark background",
    flag: { field: "#061018", rhombus: "#29C5FF", disc: "#0a7fb8" },
  },
  {
    id: "azul-escuro",
    name: "Dark Blue",
    desc: "Dark blue — deep blue",
    flag: { field: "#070d1a", rhombus: "#3B82F6", disc: "#1e3a8a" },
  },
  {
    id: "rosa",
    name: "Pink",
    desc: "Neon pink over a dark background",
    flag: { field: "#180813", rhombus: "#FF4FA3", disc: "#C71E73" },
  },
  {
    id: "violeta",
    name: "Violet",
    desc: "Neon violet",
    flag: { field: "#120a1f", rhombus: "#A855F7", disc: "#6b21a8" },
  },
  {
    id: "vermelho",
    name: "Red",
    desc: "Intense red",
    flag: { field: "#1a0808", rhombus: "#FF4D4D", disc: "#991b1b" },
  },
  {
    id: "laranja",
    name: "Orange",
    desc: "Vibrant orange",
    flag: { field: "#180d04", rhombus: "#FF8C1A", disc: "#b45309" },
  },
  {
    id: "cinza",
    name: "Gray",
    desc: "Gray interface, colored statuses",
    flag: { field: "#0d0d0d", rhombus: "#d4d4d4", disc: "#525252" },
  },
  {
    id: "bege-escuro",
    name: "Dark Beige",
    desc: "Beige/sand on a dark background",
    flag: { field: "#15110a", rhombus: "#D9C89A", disc: "#8a7a4c" },
  },
  {
    id: "marrom",
    name: "Brown",
    desc: "Earthy brown",
    flag: { field: "#160c07", rhombus: "#C68A4E", disc: "#7a4f2a" },
  },
];

const STORAGE_KEY = "regente:theme";

export function getThemeId(): ThemeId {
  if (typeof window === "undefined") return "escuro";
  const v = window.localStorage.getItem(STORAGE_KEY) as ThemeId | null;
  return THEMES.some((t) => t.id === v) ? (v as ThemeId) : "escuro";
}

export function applyTheme(id: ThemeId): void {
  if (typeof document === "undefined") return;
  // "escuro" = sem atributo (usa o :root base); os demais setam data-theme.
  if (id === "escuro") {
    document.documentElement.removeAttribute("data-theme");
  } else {
    document.documentElement.setAttribute("data-theme", id);
  }
  try {
    window.localStorage.setItem(STORAGE_KEY, id);
  } catch {
    /* localStorage indisponível — aplica só em memória */
  }
}

/** Aplica o tema salvo. Chamar uma vez no boot (antes do primeiro paint). */
export function initTheme(): void {
  applyTheme(getThemeId());
}
