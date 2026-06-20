/**
 * theme.ts — temas da UI.
 *
 * Um tema sobrescreve os tokens `--v2-*` via `:root[data-theme="<id>"]`
 * (definidos em src/v2/tokens.css). "escuro" é o default (sem atributo).
 * A escolha persiste em localStorage e é aplicada no boot (initTheme).
 */

export type ThemeId = "escuro" | "brasil" | "brasil-ouro" | "brasil-mata" | "rosa";

export interface ThemeDef {
  id: ThemeId;
  name: string;
  desc: string;
  /** Cores do swatch (3 faixas) mostrado no card de seleção do tema. */
  flag: { field: string; rhombus: string; disc: string };
}

/** Temas disponíveis. O 1º é o default (Escuro); seguem variações Brasil e o Rosa. */
export const THEMES: ThemeDef[] = [
  {
    id: "escuro",
    name: "Escuro",
    desc: "Padrão — preto e verde",
    flag: { field: "#0a0a0a", rhombus: "#11C76F", disc: "#064E2B" },
  },
  {
    id: "brasil",
    name: "Brasil",
    desc: "Verde e amarelo — bandeira atual",
    flag: { field: "#009C3B", rhombus: "#FFDF00", disc: "#002776" },
  },
  {
    id: "brasil-ouro",
    name: "Brasil Ouro",
    desc: "Amarelo-ouro em destaque",
    flag: { field: "#1c7a3f", rhombus: "#FFD200", disc: "#143a8a" },
  },
  {
    id: "brasil-mata",
    name: "Brasil Mata",
    desc: "Verde-mata profundo",
    flag: { field: "#00591f", rhombus: "#E6C200", disc: "#002766" },
  },
  {
    id: "rosa",
    name: "Rosa",
    desc: "Rosa neon sobre fundo escuro",
    flag: { field: "#160810", rhombus: "#FF4FA3", disc: "#5e0d34" },
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
