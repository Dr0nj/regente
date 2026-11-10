import { memo, useEffect, useId, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { useStore, useStoreApi, type Node } from "@xyflow/react";
import type { LaneInfo } from "./canvas-layout";
import { mapKeyAction } from "./minimap-controls";
import { latestFrame } from "../lib/latest-frame";
import { recordCanvasMetric } from "./canvas-metrics";
import { STATUS_VISUALS, priorityStatus, type VisualStatus } from "./job-status-visual";
import { createMiniMapModel, clampMap, fitMap, mapLOD, mapPoint, mapTransform, selectionBounds, zoomMap, viewportOnMap, dragMapViewport,
  type MiniMapModel, type MapTransform, type MapView, type MapJob } from "./minimap-model";

type Camera = { x: number; y: number; zoom: number };
type Props = { nodes: Node[]; lanes: LaneInfo[]; edges: Array<{ source: string; target: string }>; width: number; height: number;
  onNavigate: (x: number, y: number, zoom: number, duration?: number) => void;
  onMoveCamera: (camera: Camera) => unknown };

export default function NavMinimap({ nodes, lanes, edges, width, height, onNavigate, onMoveCamera }: Props) {
  const mapID = useId(), legendID = useId(), helpID = useId();
  const legendButton = useRef<HTMLButtonElement>(null);
  const [builder] = useState(createMiniMapModel);
  const model = useMemo(() => builder(nodes, lanes, edges), [builder, nodes, lanes, edges]);
  const [manualView, setView] = useState<MapView | null>(null);
  const view = useMemo(() => manualView ? clampMap(manualView, model.bounds) : fitMap(model.bounds), [manualView, model.bounds]);
  const mapHeight = Math.max(30, height - 58);
  const transform = useMemo(() => mapTransform(model.bounds, width, mapHeight, view), [model.bounds, width, mapHeight, view]);
  const lod = mapLOD(view);
  const store = useStoreApi();
  const [collapsed, setCollapsed] = useState(() => { try { return localStorage.getItem("regente:minimap-collapsed") === "1"; } catch { return false; } });
  const [showEdges, setShowEdges] = useState(true);
  const [showLegend, setShowLegend] = useState(false);
  const [tooltip, setTooltip] = useState("");
  const [theme, setTheme] = useState(() => document.documentElement.getAttribute("data-theme"));
  useEffect(() => {
    const observer = new MutationObserver(() => setTheme(document.documentElement.getAttribute("data-theme")));
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    return () => observer.disconnect();
  }, []);
  const svgRef = useRef<SVGSVGElement>(null);
  const gesture = useRef<{ id: number; x: number; y: number; moved: boolean; viewport: boolean; camera: Camera; transform: MapTransform; folderID: string | null } | null>(null);
  const pointerFrame = useRef<ReturnType<typeof latestFrame<{ x: number; y: number; at: number }>> | null>(null);
  useEffect(() => {
    const frame = latestFrame<{ x: number; y: number; at: number }>(point => {
      const current = gesture.current;
      if (!current) return;
      const started = performance.now(), dx = point.x - current.x, dy = point.y - current.y;
      if (Math.abs(dx) + Math.abs(dy) > 3) current.moved = true;
      if (current.viewport && current.moved) onMoveCamera(dragMapViewport(current.camera, dx, dy, current.transform.scale));
      recordCanvasMetric("minimapPointerFrame", started, { droppedFrames: Math.max(0, Math.floor((started - point.at) / 16.67) - 1) });
    });
    pointerFrame.current = frame;
    return () => { frame.cancel(); pointerFrame.current = null; };
  }, [onMoveCamera]);
  const hoverFrame = useRef<ReturnType<typeof latestFrame<{ x: number; y: number }>> | null>(null);
  useEffect(() => {
    const frame = latestFrame<{ x: number; y: number }>(point => {
      const svg = svgRef.current;
      if (!svg || gesture.current) return;
      const rect = svg.getBoundingClientRect(), world = mapPoint(point.x - rect.left, point.y - rect.top, transform);
      const job = lod === "near" ? model.jobs.find(job => world.x >= job.x && world.x <= job.x + job.width && world.y >= job.y && world.y <= job.y + job.height) : undefined;
      setTooltip(job ? `${job.label} · ${STATUS_VISUALS[job.status].label}` : "");
    });
    hoverFrame.current = frame;
    return () => { frame.cancel(); hoverFrame.current = null; };
  }, [model, transform, lod]);
  useEffect(() => {
    const svg = svgRef.current;
    if (!svg || collapsed) return;
    const wheel = (event: WheelEvent) => {
      event.preventDefault(); event.stopPropagation();
      const rect = svg.getBoundingClientRect(), unit = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? mapHeight : 1;
      setView(zoomMap(view, model.bounds, width, mapHeight, Math.exp(-event.deltaY * unit * .002), { x: event.clientX - rect.left, y: event.clientY - rect.top }));
    };
    svg.addEventListener("wheel", wheel, { passive: false });
    return () => svg.removeEventListener("wheel", wheel);
  }, [view, model.bounds, width, mapHeight, collapsed]);
  const selected = useMemo(() => selectionBounds(model.jobs), [model]);
  const navigate = (x: number, y: number) => onNavigate(x, y, store.getState().transform[2]);
  const focus = () => {
    if (!selected) return;
    const x = (selected.minX + selected.maxX) / 2, y = (selected.minY + selected.maxY) / 2;
    setView(clampMap({ ...view, x, y }, model.bounds)); navigate(x, y);
  };
  const toggle = () => {
    const next = !collapsed; setCollapsed(next);
    try { localStorage.setItem("regente:minimap-collapsed", next ? "1" : "0"); } catch { /* Preferência não impede navegação. */ }
  };
  const down = (event: ReactPointerEvent<SVGSVGElement>) => {
    if (event.button !== 0 || gesture.current) return;
    event.preventDefault(); event.stopPropagation(); event.currentTarget.focus();
    const [x, y, zoom] = store.getState().transform;
    gesture.current = { id: event.pointerId, x: event.clientX, y: event.clientY, moved: false,
      viewport: (event.target as Element).getAttribute("data-map-viewport") === "true", camera: { x, y, zoom }, transform, folderID: (event.target as Element).closest("[data-folder]")?.getAttribute("data-folder") ?? null };
    hoverFrame.current?.cancel();
    event.currentTarget.setPointerCapture(event.pointerId);
  };
  const up = (event: ReactPointerEvent<SVGSVGElement>) => {
    const current = gesture.current;
    if (!current || current.id !== event.pointerId) return;
    pointerFrame.current?.cancel();
    const dx = event.clientX - current.x, dy = event.clientY - current.y;
    const moved = current.moved || Math.abs(dx) + Math.abs(dy) > 3;
    if (current.viewport && moved) onMoveCamera(dragMapViewport(current.camera, dx, dy, current.transform.scale));
    if (!moved) {
      const rect = event.currentTarget.getBoundingClientRect();
      const point = mapPoint(event.clientX - rect.left, event.clientY - rect.top, transform);
      const job = lod === "near" ? model.jobs.find(job => point.x >= job.x && point.x <= job.x + job.width && point.y >= job.y && point.y <= job.y + job.height) : undefined;
      const folderID = current.folderID;
      const folder = model.folders.find(folder => folder.id === folderID);
      if (folder) navigate(folder.x + folder.width / 2, folder.y + folder.height / 2);
      else if (job) navigate(job.x + job.width / 2, job.y + job.height / 2);
      else navigate(point.x, point.y);
    }
    gesture.current = null;
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId);
  };
  const panMap = (dx: number, dy: number) => setView(clampMap({ ...view, x: view.x + dx / transform.scale, y: view.y + dy / transform.scale }, model.bounds));
  return <section className="v2-minimap" data-collapsed={collapsed} style={{ width, height: collapsed ? 30 : height }} aria-label="Workflow navigation map"
    onKeyDown={event => {
      if (event.key === "Escape" && showLegend) {
        event.preventDefault(); event.stopPropagation(); setShowLegend(false); legendButton.current?.focus();
      }
    }}>
    <div className="v2-minimap-toolbar" role="group" aria-label="Minimap controls">
      <button title={collapsed ? "Expand minimap" : "Collapse minimap"} aria-label={collapsed ? "Expand minimap" : "Collapse minimap"} aria-expanded={!collapsed} aria-controls={!collapsed ? mapID : undefined} onClick={toggle}>{collapsed ? "▣" : "▾"}</button>
      <span className="v2-minimap-heading">Map</span>
      {!collapsed && <>
        <button aria-label="Zoom out minimap" title="Zoom out minimap" disabled={view.zoom <= 1} onClick={() => setView(zoomMap(view, model.bounds, width, mapHeight, 1 / 1.5))}>−</button>
        <button aria-label="Zoom in minimap" title="Zoom in minimap" disabled={view.zoom >= 16} onClick={() => setView(zoomMap(view, model.bounds, width, mapHeight, 1.5))}>+</button>
        <button title="Fit minimap" aria-label="Fit minimap" onClick={() => setView(null)}>Fit</button>
        <button title="Center selected jobs" aria-label="Center selection" disabled={!selected} onClick={focus}>Center</button>
        <button title="Show connections" aria-label="Show minimap connections" aria-pressed={showEdges} onClick={() => setShowEdges(!showEdges)}>↗</button>
        <button ref={legendButton} title="Status legend" aria-label="Minimap status legend" aria-expanded={showLegend} aria-controls={showLegend ? legendID : undefined} onClick={() => setShowLegend(!showLegend)}>?</button>
      </>}
    </div>
    {!collapsed && <>
      <div className="v2-minimap-map" style={{ width, height: mapHeight }}>
        <MapRaster model={model} transform={transform} lod={lod} width={width} height={mapHeight} showEdges={showEdges} theme={theme} />
        <svg id={mapID} ref={svgRef} aria-describedby={helpID} width={width} height={mapHeight} tabIndex={0} role="group" aria-label="Navigation map: click to focus, drag viewport, wheel to zoom, arrow keys to explore"
          onPointerDown={down} onPointerUp={up} onPointerLeave={() => { hoverFrame.current?.cancel(); setTooltip(""); }} onPointerMove={event => {
            if (gesture.current?.id === event.pointerId) pointerFrame.current?.schedule({ x: event.clientX, y: event.clientY, at: performance.now() });
            else if (!gesture.current && !(event.target as Element).closest("[data-folder]")) hoverFrame.current?.schedule({ x: event.clientX, y: event.clientY });
          }} onPointerCancel={event => { if (gesture.current?.id === event.pointerId) { pointerFrame.current?.cancel(); gesture.current = null; } }} onLostPointerCapture={event => { if (gesture.current?.id === event.pointerId) { pointerFrame.current?.cancel(); gesture.current = null; } }}
          onKeyDown={event => {
            const action = mapKeyAction(event); if (!action) return;
            if (action.kind === "dismiss") { hoverFrame.current?.cancel(); setTooltip(""); return; }
            event.preventDefault(); event.stopPropagation();
            if (action.kind === "panMap") panMap(action.dx, action.dy);
            if (action.kind === "panCanvas") {
              const [x, y, zoom] = store.getState().transform;
              onMoveCamera({ x: x - action.dx, y: y - action.dy, zoom });
            }
            if (action.kind === "zoom") setView(zoomMap(view, model.bounds, width, mapHeight, action.factor));
            if (action.kind === "fit") setView(null);
            if (action.kind === "focus") navigate(view.x, view.y);
          }}>
          <ViewportRect transform={transform} />
          {model.folders.map(folder => {
            const x = transform.offX + folder.x * transform.scale, y = transform.offY + folder.y * transform.scale, w = folder.width * transform.scale;
            if (x > width || x + w < 0 || y > mapHeight || y + folder.height * transform.scale < 0 || w < 34) return null;
            const counts = Object.entries(folder.counts).filter(([, count]) => count).map(([status, count]) => `${STATUS_VISUALS[status as VisualStatus].label}: ${count}`).join(" · ");
            const label = `${folder.team} (${Object.values(folder.counts).reduce((sum, count) => sum + count, 0)})`, available = Math.max(3, Math.floor((Math.min(w, width) - 6) / 6));
            return <g key={folder.id} data-folder={folder.id} className="v2-minimap-folder" tabIndex={0} role="button" aria-label={`Focus folder ${folder.team}, ${counts}`} onFocus={() => setTooltip(`${folder.team} · ${counts}`)}
              onBlur={() => setTooltip("")} onMouseEnter={() => { hoverFrame.current?.cancel(); setTooltip(`${folder.team} · ${counts}`); }} onMouseLeave={() => setTooltip("")}
              onKeyDown={event => { if (!event.ctrlKey && !event.metaKey && !event.altKey && (event.key === "Enter" || event.key === " ")) { event.preventDefault(); event.stopPropagation(); navigate(folder.x + folder.width / 2, folder.y + folder.height / 2); } }}>
              <title>{folder.team} · {counts}</title>
              <rect x={Math.max(0, x)} y={Math.max(0, y)} width={Math.min(w, width)} height={15} rx={2} />
              <text x={Math.max(0, x) + 3} y={Math.max(0, y) + 11}>{label.length > available ? label.slice(0, available - 1) + "…" : label}</text>
            </g>;
          })}
          {!model.jobs.length && <text x={width / 2} y={mapHeight / 2} textAnchor="middle" fill="var(--v2-text-secondary)" fontSize={11}>No jobs</text>}
        </svg>
      </div>
      <p id={helpID} className="v2-map-sr-only">Arrow keys explore the map. Shift plus arrow keys move the main canvas. Plus and minus zoom the map, Home fits, Enter centers the canvas, Escape closes the legend.</p>
      <div className="v2-minimap-footer" title={tooltip || `${model.jobs.length} jobs · ${model.folders.length} folders`}>
        {tooltip || `${model.jobs.length.toLocaleString()} jobs · ${Math.round(view.zoom * 100)}% map · ${lod}`}
      </div>
      {showLegend && <div id={legendID} role="region" aria-label="Job status legend" className="v2-minimap-legend">{(Object.keys(STATUS_VISUALS) as VisualStatus[]).map(status => <span key={status}><i style={{ background: `var(${STATUS_VISUALS[status].token})` }} />{STATUS_VISUALS[status].label}</span>)}</div>}
    </>}
  </section>;
}

const MapRaster = memo(function MapRaster({ model, transform: t, lod, width, height, showEdges, theme }: {
  model: MiniMapModel; transform: MapTransform; lod: "far" | "medium" | "near"; width: number; height: number; showEdges: boolean; theme: string | null;
}) {
  const ref = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    const started = performance.now(), canvas = ref.current, ctx = canvas?.getContext("2d");
    if (!canvas || !ctx) return;
    const dpr = window.devicePixelRatio || 1;
    canvas.width = Math.round(width * dpr); canvas.height = Math.round(height * dpr); ctx.scale(dpr, dpr);
    const style = getComputedStyle(document.documentElement), color = Object.fromEntries(Object.entries(STATUS_VISUALS).map(([status, visual]) => [status, style.getPropertyValue(visual.token).trim()])) as Record<VisualStatus, string>;
    const relationColors = { ancestor: style.getPropertyValue("--job-ancestor").trim(), descendant: style.getPropertyValue("--job-descendant").trim() };
    const text = style.getPropertyValue("--v2-text-primary").trim(), border = style.getPropertyValue("--v2-border-strong").trim();
    const selectedColor = style.getPropertyValue("--job-selected").trim();
    ctx.fillStyle = style.getPropertyValue("--v2-bg-canvas").trim(); ctx.fillRect(0, 0, width, height);
    ctx.fillStyle = border;
    for (let x = 8; x < width; x += 16) for (let y = 8; y < height; y += 16) ctx.fillRect(x, y, 1, 1);
    const visible = (x: number, y: number, w: number, h: number) => x + w >= 0 && y + h >= 0 && x <= width && y <= height;
    for (const folder of model.folders) {
      const x = t.offX + folder.x * t.scale, y = t.offY + folder.y * t.scale, w = folder.width * t.scale, h = folder.height * t.scale;
      if (!visible(x, y, w, h)) continue;
      ctx.fillStyle = `hsla(${folder.hue}, 45%, 50%, .09)`; ctx.strokeStyle = `hsla(${folder.hue}, 50%, 50%, .5)`;
      ctx.fillRect(x, y, w, h); ctx.lineWidth = 1; ctx.strokeRect(x, y, w, h);
      if ((folder.counts.failed ?? 0) + (folder.counts.uncertain ?? 0) > 0) { ctx.fillStyle = color[priorityStatus(folder.counts)]; ctx.fillRect(x, y, Math.min(w, 3), h); }
    }
    const hasSelection = model.jobs.some(job => job.selected);
    const related = (job: MapJob) => job.selected || !!job.relation;
    if (showEdges && lod === "medium") {
      ctx.strokeStyle = border; ctx.globalAlpha = .6;
      for (const edge of model.links) {
        ctx.lineWidth = Math.min(2, .5 + Math.log2(edge.count + 1) / 4); ctx.beginPath();
        ctx.moveTo(t.offX + edge.x1 * t.scale, t.offY + edge.y1 * t.scale); ctx.lineTo(t.offX + edge.x2 * t.scale, t.offY + edge.y2 * t.scale); ctx.stroke();
      }
    }
    if (showEdges && (lod === "near" || (lod === "medium" && hasSelection))) for (const edge of model.edges) {
      const a = model.byID.get(edge.source), b = model.byID.get(edge.target);
      if (!a || !b || (lod === "medium" && !(related(a) && related(b)))) continue;
      ctx.globalAlpha = hasSelection && !(related(a) && related(b)) ? .08 : .5; ctx.strokeStyle = related(a) && related(b) ? (a.relation === "ancestor" || b.relation === "ancestor" ? relationColors.ancestor : relationColors.descendant) : border; ctx.lineWidth = 1;
      ctx.beginPath(); ctx.moveTo(t.offX + (a.x + a.width / 2) * t.scale, t.offY + (a.y + a.height) * t.scale);
      ctx.lineTo(t.offX + (b.x + b.width / 2) * t.scale, t.offY + b.y * t.scale); ctx.stroke();
    }
    ctx.globalAlpha = 1;
    if (lod !== "near") for (const cell of model.cells[lod === "far" ? 0 : 1]) {
      if (cell.count <= 0) continue;
      const x = t.offX + cell.x * t.scale, y = t.offY + cell.y * t.scale, size = Math.max(2, cell.size * t.scale - 1);
      if (!visible(x, y, size, size)) continue;
      ctx.globalAlpha = (hasSelection ? .22 : .6) + Math.min(.35, Math.log2(cell.count + 1) / 12);
      ctx.fillStyle = color[cell.dominant]; ctx.fillRect(x, y, size, size);
      // Uma falha minoritária nunca some no verde dominante.
      ctx.globalAlpha = 1; ctx.fillStyle = color[cell.priority]; ctx.fillRect(x, y, Math.max(2, size * .25), size);
      if (cell.counts.running && cell.priority !== "running") { ctx.fillStyle = color.running; ctx.fillRect(x + size - 2, y, 2, size); }
    }
    const drawJob = (job: MapJob) => {
      const x = t.offX + job.x * t.scale, y = t.offY + job.y * t.scale, w = Math.max(3, job.width * t.scale), h = Math.max(2, job.height * t.scale);
      if (!visible(x, y, w, h)) return;
      ctx.globalAlpha = hasSelection && !related(job) ? .25 : 1;
      ctx.fillStyle = color[job.status]; ctx.fillRect(x, y, w, h);
      if (related(job)) {
        ctx.globalAlpha = 1; ctx.lineWidth = job.selected ? 2 : 1.5;
        ctx.strokeStyle = job.selected ? selectedColor : relationColors[job.relation as keyof typeof relationColors] || text;
        ctx.strokeRect(x - 1, y - 1, w + 2, h + 2);
      }
      if (lod === "near" && w > 70 && h > 17) {
        ctx.fillStyle = style.getPropertyValue("--v2-bg-surface").trim(); ctx.globalAlpha = .95; ctx.fillRect(x, y + h, w, 13);
        ctx.globalAlpha = 1; ctx.fillStyle = text; ctx.font = "10px sans-serif"; ctx.fillText(job.label.slice(0, Math.floor(w / 6)), x + 2, y + h + 10, w - 4);
      }
    };
    for (const job of model.jobs) if (lod === "near" || related(job) || (lod === "medium" && ["failed", "uncertain", "running"].includes(job.status))) drawJob(job);
    ctx.globalAlpha = 1;
    recordCanvasMetric("minimapRaster", started, { jobs: model.jobs.length, folders: model.folders.length, cells: model.cells[lod === "far" ? 0 : 1].length, links: model.links.length });
  }, [model, t, lod, width, height, showEdges, theme]);
  return <canvas ref={ref} aria-hidden="true" style={{ width, height, display: "block" }} />;
});

function ViewportRect({ transform }: { transform: MapTransform }) {
  const camera = useStore(s => s.transform), width = useStore(s => s.width), height = useStore(s => s.height);
  const rect = viewportOnMap({ x: camera[0], y: camera[1], zoom: camera[2] }, { width, height }, transform);
  recordCanvasMetric("minimapViewportUpdate");
  return <rect {...rect} data-map-viewport="true" className="v2-minimap-viewport" rx={2}><title>Drag to move the canvas</title></rect>;
}
