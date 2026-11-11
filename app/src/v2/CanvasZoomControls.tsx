import { useStore } from "@xyflow/react";
import { CANVAS_MIN_ZOOM, CANVAS_MAX_ZOOM } from "./canvas-zoom";

export default function CanvasZoomControls({ onZoom, onFit }: { onZoom: (factor: number) => void; onFit: () => void }) {
  const zoom = useStore(s => s.transform[2]);
  return <div className="v2-canvas-zoom" role="group" aria-label="Canvas zoom">
    <button title="Zoom out" aria-label="Zoom out canvas" disabled={zoom <= CANVAS_MIN_ZOOM} onClick={() => onZoom(1 / 1.25)}>−</button>
    <output aria-label="Canvas zoom percentage">{Math.round(zoom * 100)}%</output>
    <button title="Zoom in" aria-label="Zoom in canvas" disabled={zoom >= CANVAS_MAX_ZOOM} onClick={() => onZoom(1.25)}>+</button>
    <button title="Fit canvas" aria-label="Fit canvas" onClick={onFit}>Fit</button>
  </div>;
}
