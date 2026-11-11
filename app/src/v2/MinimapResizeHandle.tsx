import { useEffect, useId, useRef, type PointerEvent } from "react";
import { latestFrame } from "../lib/latest-frame";
import { clampMapSize, resizeMapKey, type MapSize } from "./minimap-controls";

export default function MinimapResizeHandle({ size, right, onResize }: {
  size: MapSize; right: number; onResize: (size: MapSize) => void;
}) {
  const helpID = useId();
  const gesture = useRef<{ id: number; x: number; y: number; size: MapSize } | null>(null);
  const frame = useRef<ReturnType<typeof latestFrame<MapSize>> | null>(null);
  useEffect(() => {
    const next = latestFrame(onResize); frame.current = next;
    return () => { next.cancel(); frame.current = null; gesture.current = null; };
  }, [onResize]);
  const nextSize = (event: PointerEvent<HTMLButtonElement>) => {
    const current = gesture.current;
    return current && current.id === event.pointerId ? clampMapSize({
      w: current.size.w + current.x - event.clientX, h: current.size.h + current.y - event.clientY,
    }) : null;
  };
  const cancel = (event: PointerEvent<HTMLButtonElement>) => {
    if (gesture.current?.id !== event.pointerId) return;
    frame.current?.cancel(); gesture.current = null;
  };
  return <button type="button" className="v2-minimap-resize" aria-label="Resize minimap" aria-describedby={helpID}
    title="Resize minimap: drag or use arrow keys; Shift for larger steps; Home to reset"
    style={{ right: right + size.w - 12, bottom: 16 + size.h - 12 }}
    onPointerDown={event => {
      if (event.button !== 0 || gesture.current) return;
      event.preventDefault(); event.stopPropagation(); event.currentTarget.focus();
      gesture.current = { id: event.pointerId, x: event.clientX, y: event.clientY, size };
      event.currentTarget.setPointerCapture(event.pointerId);
    }}
    onPointerMove={event => { const next = nextSize(event); if (next) frame.current?.schedule(next); }}
    onPointerUp={event => {
      const next = nextSize(event); if (!next) return;
      cancel(event); onResize(next);
      if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
    }}
    onPointerCancel={cancel} onLostPointerCapture={cancel}
    onKeyDown={event => {
      if (gesture.current) return;
      const next = resizeMapKey(size, event); if (!next) return;
      event.preventDefault(); event.stopPropagation(); onResize(next);
    }}>
    <span aria-hidden="true">⤢</span>
    <span className="v2-map-sr-only" id={helpID}>Width {size.w}, height {size.h} pixels. Arrow keys resize; Shift changes by 40 pixels; Home resets the size.</span>
  </button>;
}
