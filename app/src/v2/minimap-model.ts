import type { Node, Edge } from "@xyflow/react";
import type { LaneInfo } from "./canvas-layout";
import { jobVisualStatus, dominantStatus, priorityStatus, type StatusCounts, type VisualStatus } from "./job-status-visual";
import { recordCanvasMetric } from "./canvas-metrics";

export interface MapBounds { minX: number; minY: number; maxX: number; maxY: number }
export interface MapJob {
  id: string; x: number; y: number; width: number; height: number;
  folder: string; label: string; status: VisualStatus; selected: boolean; relation?: string;
}
export interface MapFolder extends LaneInfo { id: string; hue: number; counts: StatusCounts }
export interface MapCell { id: string; x: number; y: number; size: number; count: number; counts: StatusCounts; dominant: VisualStatus; priority: VisualStatus }
export interface MapLink { source: string; target: string; count: number; x1: number; y1: number; x2: number; y2: number }
export interface MiniMapModel {
  bounds: MapBounds; jobs: MapJob[]; byID: Map<string, MapJob>; folders: MapFolder[];
  cells: [MapCell[], MapCell[]]; links: MapLink[]; edges: Array<{ source: string; target: string }>;
  geometryBuilds: number;
}
export interface MapView { x: number; y: number; zoom: number }
export interface MapTransform { scale: number; offX: number; offY: number }
export const MAP_MARGIN = 8;
const emptyBounds = (): MapBounds => ({ minX: 0, minY: 0, maxX: 1, maxY: 1 });
export function folderHue(name: string): number {
  let hash = 2166136261;
  for (let i = 0; i < name.length; i++) hash = Math.imul(hash ^ name.charCodeAt(i), 16777619);
  return (hash >>> 0) % 360;
}
export function mapTransform(bounds: MapBounds, width: number, height: number, view: MapView): MapTransform {
  const bw = Math.max(1, bounds.maxX - bounds.minX), bh = Math.max(1, bounds.maxY - bounds.minY);
  const scale = Math.max(.000001, Math.min(Math.max(1, width - MAP_MARGIN * 2) / bw, Math.max(1, height - MAP_MARGIN * 2) / bh)) * view.zoom;
  return { scale, offX: width / 2 - view.x * scale, offY: height / 2 - view.y * scale };
}
export function mapPoint(x: number, y: number, transform: MapTransform) {
  return { x: (x - transform.offX) / transform.scale, y: (y - transform.offY) / transform.scale };
}
export function fitMap(bounds: MapBounds): MapView {
  return { x: (bounds.minX + bounds.maxX) / 2, y: (bounds.minY + bounds.maxY) / 2, zoom: 1 };
}
export function clampMap(view: MapView, bounds: MapBounds): MapView {
  return { x: Math.max(bounds.minX, Math.min(bounds.maxX, view.x)), y: Math.max(bounds.minY, Math.min(bounds.maxY, view.y)), zoom: Math.max(1, Math.min(16, view.zoom)) };
}
export function zoomMap(view: MapView, bounds: MapBounds, width: number, height: number, factor: number, anchor = { x: width / 2, y: height / 2 }): MapView {
  const current = mapTransform(bounds, width, height, view), point = mapPoint(anchor.x, anchor.y, current);
  const zoom = Math.max(1, Math.min(16, view.zoom * factor)), scale = current.scale * zoom / view.zoom;
  return clampMap({ zoom, x: point.x - (anchor.x - width / 2) / scale, y: point.y - (anchor.y - height / 2) / scale }, bounds);
}
export function mapLOD(view: MapView): "far" | "medium" | "near" { return view.zoom < 2 ? "far" : view.zoom < 5 ? "medium" : "near"; }
export function viewportOnMap(camera: { x: number; y: number; zoom: number }, pane: { width: number; height: number }, t: MapTransform) {
  return { x: t.offX - camera.x / camera.zoom * t.scale, y: t.offY - camera.y / camera.zoom * t.scale, width: pane.width / camera.zoom * t.scale, height: pane.height / camera.zoom * t.scale };
}
export function dragMapViewport(camera: { x: number; y: number; zoom: number }, dx: number, dy: number, scale: number) {
  return { x: camera.x - dx / scale * camera.zoom, y: camera.y - dy / scale * camera.zoom, zoom: camera.zoom };
}
export function selectionBounds(jobs: MapJob[]): MapBounds | null {
  let bounds: MapBounds | null = null;
  for (const job of jobs) if (job.selected) {
    bounds ??= { minX: job.x, minY: job.y, maxX: job.x + job.width, maxY: job.y + job.height };
    bounds.minX = Math.min(bounds.minX, job.x); bounds.minY = Math.min(bounds.minY, job.y);
    bounds.maxX = Math.max(bounds.maxX, job.x + job.width); bounds.maxY = Math.max(bounds.maxY, job.y + job.height);
  }
  return bounds;
}
function countStatus(counts: StatusCounts, status: VisualStatus, delta: number): StatusCounts {
  return { ...counts, [status]: (counts[status] ?? 0) + delta };
}
function cellKey(job: MapJob, size: number) { return `${Math.floor((job.x + job.width / 2) / size)}:${Math.floor((job.y + job.height / 2) / size)}`; }

// Cache por geometria e identidade do nó. Pan/zoom não entram no modelo. Status
// ajusta somente contagens afetadas, sem refazer folders, células ou topologia.
export function createMiniMapModel() {
  let previous: MiniMapModel | undefined, lastLanes: LaneInfo[] | undefined;
  let sources = new Map<string, Node>();
  let sizes: [number, number] = [1, 1];
  let cellMaps: [Map<string, MapCell>, Map<string, MapCell>] = [new Map(), new Map()];
  return (nodes: Node[], lanes: LaneInfo[], edges: Array<Pick<Edge, "source" | "target">>): MiniMapModel => {
    const started = performance.now();
    let geometry = !previous || lastLanes !== lanes;
    const nextSources = new Map<string, Node>(), byID = new Map<string, MapJob>();
    const changes: Array<{ old?: MapJob; next: MapJob }> = [];
    for (const node of nodes) {
      if (node.type !== "jobV2") continue;
      nextSources.set(node.id, node);
      const old = previous?.byID.get(node.id);
      if (old && sources.get(node.id) === node) { byID.set(node.id, old); continue; }
      // Dimensões declaradas do canvas; nunca medir DOM nem executar layout.
      const next: MapJob = { id: node.id, x: node.position.x, y: node.position.y,
        width: node.initialWidth ?? node.width ?? 220, height: node.initialHeight ?? node.height ?? 80,
        label: String(node.data.label ?? node.id), folder: String(node.data.team ?? ""),
        status: jobVisualStatus(node.data), selected: !!node.selected,
        relation: typeof node.data.dependencyRelation === "string" ? node.data.dependencyRelation : undefined };
      if (!old || old.x !== next.x || old.y !== next.y || old.width !== next.width || old.height !== next.height || old.folder !== next.folder) geometry = true;
      byID.set(node.id, next); changes.push({ old, next });
    }
    if (previous && previous.jobs.length !== byID.size) geometry = true;
    const linksChanged = !previous || previous.edges.length !== edges.length || edges.some((edge, i) => edge.source !== previous!.edges[i].source || edge.target !== previous!.edges[i].target);
    if (!geometry && !linksChanged && !changes.length) return previous!;
    let folders = previous?.folders ?? [], bounds = previous?.bounds ?? emptyBounds();
    if (geometry) {
      bounds = { minX: Infinity, minY: Infinity, maxX: -Infinity, maxY: -Infinity };
      const extend = (x: number, y: number, width: number, height: number) => {
        bounds.minX = Math.min(bounds.minX, x); bounds.minY = Math.min(bounds.minY, y);
        bounds.maxX = Math.max(bounds.maxX, x + width); bounds.maxY = Math.max(bounds.maxY, y + height);
      };
      folders = lanes.map((lane, i) => { extend(lane.x, lane.y, lane.width, lane.height); return { ...lane, id: String(i), hue: folderHue(lane.team), counts: {} }; });
      for (const job of byID.values()) extend(job.x, job.y, job.width, job.height);
      if (!Number.isFinite(bounds.minX)) bounds = emptyBounds();
      const coarse = Math.max(240, (bounds.maxX - bounds.minX) / 48, (bounds.maxY - bounds.minY) / 24);
      sizes = [coarse, coarse / 4]; cellMaps = [new Map(), new Map()];
    } else folders = folders.slice();
    const folderIndexes = new Map<string, number[]>();
    folders.forEach((f, i) => folderIndexes.set(f.team, [...(folderIndexes.get(f.team) ?? []), i]));
    const update = (job: MapJob, delta: number) => {
      const candidates = folderIndexes.get(job.folder) ?? [];
      const index = candidates.find(i => job.x >= folders[i].x && job.x < folders[i].x + folders[i].width && job.y >= folders[i].y && job.y < folders[i].y + folders[i].height) ?? candidates[0];
      if (index !== undefined) folders[index] = { ...folders[index], counts: countStatus(folders[index].counts, job.status, delta) };
      sizes.forEach((size, level) => {
        const key = cellKey(job, size), old = cellMaps[level].get(key);
        const [x, y] = key.split(":").map(Number);
        const counts = countStatus(old?.counts ?? {}, job.status, delta);
        cellMaps[level].set(key, { id: key, x: x * size, y: y * size, size, counts,
          count: (old?.count ?? 0) + delta, dominant: dominantStatus(counts), priority: priorityStatus(counts) });
      });
    };
    if (geometry) for (const job of byID.values()) update(job, 1);
    else for (const change of changes) if (change.old?.status !== change.next.status) {
      if (change.old) update(change.old, -1);
      update(change.next, 1);
    }
    let links = previous?.links ?? [];
    if (geometry || linksChanged) {
      const aggregated = new Map<string, MapLink>();
      for (const edge of edges) {
        const source = byID.get(edge.source), target = byID.get(edge.target);
        if (!source || !target) continue;
        const s = cellKey(source, sizes[1]), t = cellKey(target, sizes[1]);
        if (s === t) continue;
        const key = `${s}>${t}`, old = aggregated.get(key);
        if (old) old.count++;
        else {
          const a = cellMaps[1].get(s)!, b = cellMaps[1].get(t)!;
          aggregated.set(key, { source: s, target: t, count: 1, x1: a.x + a.size / 2, y1: a.y + a.size / 2, x2: b.x + b.size / 2, y2: b.y + b.size / 2 });
        }
      }
      links = [...aggregated.values()];
    }
    const model: MiniMapModel = { bounds, folders, byID, jobs: [...byID.values()],
      cells: [[...cellMaps[0].values()], [...cellMaps[1].values()]], links,
      edges: linksChanged ? edges.map(({ source, target }) => ({ source, target })) : previous!.edges,
      geometryBuilds: (previous?.geometryBuilds ?? 0) + (geometry ? 1 : 0) };
    sources = nextSources; lastLanes = lanes; previous = model;
    recordCanvasMetric(geometry ? "minimapModelBuild" : "minimapModelPatch", started, { jobs: model.jobs.length, folders: folders.length, cells: model.cells[0].length, links: links.length, changed: changes.length });
    return model;
  };
}
