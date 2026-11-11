export interface MapSize { w: number; h: number }
export interface MapKey { key: string; ctrlKey?: boolean; metaKey?: boolean; altKey?: boolean; shiftKey?: boolean }
export type MapKeyAction = { kind: "panMap" | "panCanvas"; dx: number; dy: number }
  | { kind: "zoom"; factor: number } | { kind: "fit" | "focus" | "dismiss" };
const arrows: Record<string, [number, number]> = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] };
const browserShortcut = (event: MapKey) => event.ctrlKey || event.metaKey || event.altKey;

// Modificadores do browser permanecem livres; Shift+seta move a câmera principal.
export function mapKeyAction(event: MapKey): MapKeyAction | null {
  if (browserShortcut(event)) return null;
  const arrow = arrows[event.key];
  if (arrow) return { kind: event.shiftKey ? "panCanvas" : "panMap", dx: arrow[0] * 30, dy: arrow[1] * 30 };
  if (event.key === "+" || event.key === "=") return { kind: "zoom", factor: 1.5 };
  if (event.key === "-") return { kind: "zoom", factor: 1 / 1.5 };
  if (event.key === "Home") return { kind: "fit" };
  if (event.key === "Enter") return { kind: "focus" };
  if (event.key === "Escape") return { kind: "dismiss" };
  return null;
}
export function clampMapSize(size: MapSize): MapSize {
  return { w: Math.min(560, Math.max(260, size.w)), h: Math.min(380, Math.max(110, size.h)) };
}
export function resizeMapKey(size: MapSize, event: MapKey): MapSize | null {
  if (browserShortcut(event)) return null;
  if (event.key === "Home") return { w: 260, h: 168 };
  const arrow = arrows[event.key];
  if (!arrow) return null;
  const step = event.shiftKey ? 40 : 10;
  return clampMapSize({ w: size.w + arrow[0] * step, h: size.h + arrow[1] * step });
}
