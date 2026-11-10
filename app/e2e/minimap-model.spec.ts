import { test, expect } from "@playwright/test";
import type { Node } from "@xyflow/react";
import { createMiniMapModel, fitMap, mapTransform, mapPoint, zoomMap, mapLOD, viewportOnMap, dragMapViewport, selectionBounds, folderHue } from "../src/v2/minimap-model";
import { jobVisualStatus, dominantStatus, priorityStatus } from "../src/v2/job-status-visual";
import { canvasDetail, clampCanvasZoom } from "../src/v2/canvas-zoom";
import type { LaneInfo } from "../src/v2/canvas-layout";

const node = (id: string, x = 0, y = 0, status = "SUCCESS"): Node => ({ id, type: "jobV2", position: { x, y }, initialWidth: 220, initialHeight: 80, data: { label: id, team: "Batch", status } });
const lane: LaneInfo = { team: "Batch", x: -300, y: -200, width: 2000, height: 1000, count: 2 };

test("status priority preserves failure, uncertainty, execution and every waiting reason", () => {
  expect(jobVisualStatus({ status: "FAILED", held: true })).toBe("failed");
  expect(jobVisualStatus({ status: "UNCERTAIN", held: true })).toBe("uncertain");
  expect(jobVisualStatus({ status: "RUNNING", held: true })).toBe("running");
  expect(jobVisualStatus({ status: "WAITING", held: true, waitConfirm: true })).toBe("hold");
  expect(jobVisualStatus({ status: "WAITING", waitConfirm: true, waitAgent: true })).toBe("confirm");
  expect(jobVisualStatus({ status: "WAITING", waitAgent: true, waitResource: true })).toBe("agent");
  expect(jobVisualStatus({ status: "WAITING", waitResource: true, waitEvent: true })).toBe("resource");
  expect(jobVisualStatus({ status: "WAITING", waitEvent: true })).toBe("condition");
  for (const [status, expected] of [["WAITING", "waiting"], ["OK", "success"], ["NOTOK", "failed"], ["HELD", "hold"], ["CANCELLED", "inactive"]]) expect(jobVisualStatus({ status })).toBe(expected);
  expect(priorityStatus({ success: 100, failed: 1, running: 2 })).toBe("failed");
  expect(dominantStatus({ success: 100, failed: 1, running: 2 })).toBe("success");
  expect(dominantStatus({ success: 2, failed: 2 })).toBe("failed");
});

test("empty map is finite; bounds include negative coordinates, tagged jobs and folders", () => {
  const build = createMiniMapModel();
  const empty = build([], [], []);
  expect(empty.jobs).toHaveLength(0);
  expect(mapTransform(empty.bounds, 0, 0, fitMap(empty.bounds)).scale).toBeGreaterThan(0);
  const a = node("a", -400, -300); a.initialHeight = 99;
  const model = build([a, node("b", 2000, 1200)], [lane], []);
  expect(model.bounds).toEqual({ minX: -400, minY: -300, maxX: 2220, maxY: 1280 });
  expect(model.byID.get("a")!.height).toBe(99);
  expect(model.folders[0].counts.success).toBe(2);
  expect(folderHue("Batch")).toBe(model.folders[0].hue);
});

test("density retains all jobs and minority failures and running jobs", () => {
  const nodes = Array.from({ length: 5000 }, (_, i) => node(String(i), 0, 0, i === 0 ? "FAILED" : i === 1 ? "RUNNING" : "SUCCESS"));
  const model = createMiniMapModel()(nodes, [{ ...lane, count: nodes.length }], []);
  expect(model.jobs).toHaveLength(5000);
  for (const cells of model.cells) {
    expect(cells.reduce((sum, c) => sum + c.count, 0)).toBe(5000);
    expect(cells[0]).toMatchObject({ dominant: "success", priority: "failed", counts: { failed: 1, running: 1, success: 4998 } });
  }
});

test("status and selection patches retain geometry, topology and untouched marker identities", () => {
  const build = createMiniMapModel(), lanes = [lane], edges = [{ source: "a", target: "b" }];
  const nodes = [node("a"), node("b", 800)];
  const a = build(nodes, lanes, edges);
  nodes[0] = { ...nodes[0], data: { ...nodes[0].data, status: "FAILED" } };
  const b = build(nodes, lanes, edges);
  expect(b.geometryBuilds).toBe(1); expect(b.bounds).toBe(a.bounds); expect(b.links).toBe(a.links);
  expect(b.edges).toBe(a.edges); expect(b.byID.get("b")).toBe(a.byID.get("b"));
  expect(a.folders[0].counts.success).toBe(2); expect(b.folders[0].counts).toMatchObject({ success: 1, failed: 1 });
  expect(a.cells[0].find(c => c.counts.success)?.counts.failed).toBeUndefined();
  nodes[0] = { ...nodes[0], selected: true };
  const c = build(nodes, lanes, edges.map(e => ({ ...e, style: { stroke: "red" } })));
  expect(c.geometryBuilds).toBe(1); expect(c.links).toBe(b.links);
  expect(selectionBounds(c.jobs)).toEqual({ minX: 0, minY: 0, maxX: 220, maxY: 80 });
  expect(build(nodes, lanes, edges)).toBe(c);
  nodes[0] = { ...nodes[0], position: { x: -500, y: 10 } };
  expect(build(nodes, lanes, edges).geometryBuilds).toBe(2);
  expect(build(nodes.slice(1), lanes, []).jobs).toHaveLength(1);
});

test("links aggregate by cell without interpreting dependencies again", () => {
  const model = createMiniMapModel()([node("a"), node("b", 2), node("c", 1000)], [], [{ source: "a", target: "c" }, { source: "b", target: "c" }, { source: "missing", target: "c" }]);
  expect(model.links).toHaveLength(1); expect(model.links[0].count).toBe(2);
  expect(model.edges).toHaveLength(3);
});

for (const [name, bounds] of Object.entries({ wide: { minX: -10000, minY: -50, maxX: 10000, maxY: 50 }, tall: { minX: -50, minY: -10000, maxX: 50, maxY: 10000 } })) {
  test(`fit, resize and cursor zoom preserve coordinates for ${name} maps`, () => {
    for (const [width, height] of [[260, 110], [560, 322]]) {
      const fit = fitMap(bounds), transform = mapTransform(bounds, width, height, fit);
      const p = { x: 12, y: -7 }, screen = { x: transform.offX + p.x * transform.scale, y: transform.offY + p.y * transform.scale };
      const result = mapPoint(screen.x, screen.y, transform);
      expect(result.x).toBeCloseTo(p.x); expect(result.y).toBeCloseTo(p.y);
      const zoomed = zoomMap(fit, bounds, width, height, 3, screen);
      const again = mapPoint(screen.x, screen.y, mapTransform(bounds, width, height, zoomed));
      expect(again.x).toBeCloseTo(p.x); expect(again.y).toBeCloseTo(p.y);
      expect(mapLOD(zoomed)).toBe("medium"); expect(mapLOD({ ...zoomed, zoom: 8 })).toBe("near");
      expect(zoomMap(fit, bounds, width, height, 100).zoom).toBe(16);
      expect(zoomMap(fit, bounds, width, height, .01)).toEqual(fit);
    }
  });
}

test("viewport drag translates world distances without changing canvas zoom", () => {
  const camera = { x: -40, y: 20, zoom: .17 }, t = { scale: .02, offX: 10, offY: 30 }, pane = { width: 900, height: 600 };
  const before = viewportOnMap(camera, pane, t), afterCamera = dragMapViewport(camera, 12, -5, t.scale);
  const after = viewportOnMap(afterCamera, pane, t);
  expect(afterCamera.zoom).toBe(.17); expect(after.width).toBe(before.width);
  expect(after.x - before.x).toBeCloseTo(12); expect(after.y - before.y).toBeCloseTo(-5);
});

test("main canvas supports 5–400 percent and solid jobs at 17 percent", () => {
  expect(clampCanvasZoom(.01)).toBe(.05); expect(clampCanvasZoom(10)).toBe(4);
  expect(clampCanvasZoom(NaN)).toBe(1);
  expect(canvasDetail(.17)).toBe("solid"); expect(canvasDetail(.25)).toBe("solid");
  expect(canvasDetail(.26)).toBe("compact"); expect(canvasDetail(.55)).toBe("compact");
  expect(canvasDetail(.9)).toBe("detail"); expect(canvasDetail(4)).toBe("detail");
});
