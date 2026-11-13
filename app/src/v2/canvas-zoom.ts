import { createContext } from "react";
export const CANVAS_MIN_ZOOM = 0.05;
export const CANVAS_MAX_ZOOM = 4;
export type CanvasDetail = "solid" | "compact" | "detail";
export function canvasDetail(zoom: number): CanvasDetail {
  return zoom <= .25 ? "solid" : zoom <= .55 ? "compact" : "detail";
}
export function clampCanvasZoom(zoom: number): number {
  return Number.isFinite(zoom) ? Math.min(CANVAS_MAX_ZOOM, Math.max(CANVAS_MIN_ZOOM, zoom)) : 1;
}
// Detalhe "solid" (zoom muito afastado): o card vira um bloco de cor. O contexto
// troca só no limiar, então os nós re-renderizam uma vez — não a cada frame.
export const CanvasSolidContext = createContext(false);
