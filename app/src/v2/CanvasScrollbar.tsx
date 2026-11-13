import { useCallback, useEffect, useRef, useState } from "react";
import { useStore, useStoreApi } from "@xyflow/react";

// Barra de rolagem horizontal do canvas (pedido do usuário, 2026-11-13): com a
// diária inteira na tela o conteúdo é muito mais largo que o pane e arrastar o
// fundo é lento para atravessá-lo. A barra aparece só quando o conteúdo não cabe
// na largura visível; o polegar é arrastável e o trilho aceita clique.
//
// Não é dona da câmera: lê o transform do store e escreve por `onMove` (o
// `apply()` do useCanvasCamera — mesmo clamp de todo movimento).

const MIN_THUMB_PX = 36;

interface Props {
  /** Limites horizontais do conteúdo, em px de mundo. */
  minX: number;
  maxX: number;
  onMove: (viewport: { x: number; y: number; zoom: number }) => unknown;
}

interface Box { left: number; right: number; width: number }

export default function CanvasScrollbar({ minX, maxX, onMove }: Props) {
  // Sidebar e drawer são overlays POR CIMA do pane (data-canvas-inset): a barra
  // ocupa só a faixa visível. Abrir/fechar um deles não gera evento que a barra
  // enxergue, então a medição é periódica e barata (3 retângulos) — e só grava
  // estado quando algo muda.
  const [box, setBox] = useState<Box>({ left: 0, right: 0, width: 0 });
  useEffect(() => {
    const measure = () => {
      const pane = document.querySelector(".react-flow")?.getBoundingClientRect();
      if (!pane) return;
      const l = document.querySelector('[data-canvas-inset="left"]')?.getBoundingClientRect();
      const r = document.querySelector('[data-canvas-inset="right"]')?.getBoundingClientRect();
      const left = l ? Math.max(0, Math.round(l.right - pane.left)) : 0;
      const right = r ? Math.max(0, Math.round(pane.right - r.left)) : 0;
      const width = Math.max(0, Math.round(pane.width) - left - right - 24);
      setBox((old) => old.left === left && old.right === right && old.width === width ? old : { left, right, width });
    };
    measure();
    const timer = window.setInterval(measure, 400);
    window.addEventListener("resize", measure);
    return () => { window.clearInterval(timer); window.removeEventListener("resize", measure); };
  }, []);
  const storeApi = useStoreApi();
  const trackRef = useRef<HTMLDivElement | null>(null);
  // O seletor devolve só o que muda o desenho (posição/tamanho arredondados),
  // então pan vertical e micro-movimentos não re-renderizam a barra.
  const geometry = useStore((s) => {
    const zoom = s.transform[2];
    const content = Math.max(1, maxX - minX);
    const view = s.width / zoom;
    if (!s.width || view >= content) return "";
    const size = Math.round((view / content) * 1000) / 1000;
    const viewLeft = -s.transform[0] / zoom;
    const pos = Math.round(Math.min(1, Math.max(0, (viewLeft - minX) / (content - view))) * 1000) / 1000;
    return `${pos}|${size}`;
  });

  // Fração 0..1 do percurso → câmera.
  const moveTo = useCallback((fraction: number) => {
    const { transform, width } = storeApi.getState();
    const zoom = transform[2];
    const content = Math.max(1, maxX - minX);
    const view = width / zoom;
    const viewLeft = minX + Math.min(1, Math.max(0, fraction)) * Math.max(0, content - view);
    onMove({ x: -viewLeft * zoom, y: transform[1], zoom });
  }, [storeApi, minX, maxX, onMove]);

  const startDrag = useCallback((event: React.PointerEvent<HTMLDivElement>, grabOffsetPx: number, thumbPx: number) => {
    const track = trackRef.current;
    if (!track) return;
    event.preventDefault();
    event.stopPropagation();
    const rect = track.getBoundingClientRect();
    const travel = Math.max(1, rect.width - thumbPx);
    const at = (clientX: number) => moveTo((clientX - rect.left - grabOffsetPx) / travel);
    let frame = 0, lastX = event.clientX;
    const onMoveEvent = (e: PointerEvent) => {
      lastX = e.clientX;
      if (frame) return;
      frame = requestAnimationFrame(() => { frame = 0; at(lastX); });
    };
    const stop = () => {
      if (frame) cancelAnimationFrame(frame);
      window.removeEventListener("pointermove", onMoveEvent);
      window.removeEventListener("pointerup", stop);
      window.removeEventListener("pointercancel", stop);
      document.body.style.userSelect = "";
    };
    document.body.style.userSelect = "none";
    window.addEventListener("pointermove", onMoveEvent);
    window.addEventListener("pointerup", stop);
    window.addEventListener("pointercancel", stop);
    at(event.clientX);
  }, [moveTo]);

  if (!geometry) return null;
  const [pos, size] = geometry.split("|").map(Number);
  if (box.width < MIN_THUMB_PX * 2) return null;
  const thumbPx = Math.min(box.width, Math.max(MIN_THUMB_PX, size * box.width));
  const thumbStyle = { width: thumbPx, left: pos * Math.max(0, box.width - thumbPx) };

  return (
    <div
      ref={trackRef}
      className="v2-canvas-scrollbar nowheel"
      role="scrollbar"
      aria-orientation="horizontal"
      aria-label="Canvas horizontal scroll"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(pos * 100)}
      style={{ left: box.left + 12, width: box.width }}
      onPointerDown={(e) => {
        // Clique no trilho: centraliza o polegar no ponto e segue arrastando.
        startDrag(e, thumbPx / 2, thumbPx);
      }}
    >
      <div
        className="v2-canvas-scrollbar-thumb"
        style={thumbStyle}
        onPointerDown={(e) => {
          const rect = e.currentTarget.getBoundingClientRect();
          startDrag(e, e.clientX - rect.left, rect.width);
        }}
      />
    </div>
  );
}
