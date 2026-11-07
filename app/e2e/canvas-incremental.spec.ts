import { test, expect } from "@playwright/test";
import { writeFileSync } from "node:fs";
import { createCanvasModel, DEFAULT_LAYOUT, type Canvas } from "../src/v2/canvas-layout";
import { createCanvasSelection } from "../src/v2/canvas-selection";
import { latestFrame } from "../src/lib/latest-frame";
import { enableCanvasMetrics, readCanvasMetrics } from "../src/v2/canvas-metrics";
import type { JobInstance } from "../src/lib/orchestrator-model";

function jobs(count: number): JobInstance[] {
  return Array.from({ length: count }, (_, i) => ({
    id: String(i).padStart(6, "0"), definitionId: `job-${i}`, label: `Job ${i}`, jobType: "COMMAND", team: "Load",
    orderDate: "2026-11-06", createdAt: 1, scheduledAt: 1, status: "WAITING", attempts: 0, manual: false, retries: 0, timeout: 0,
    condsIn: i % 10 ? [`ready-${i - 1}`] : [], condsOutAdd: [`ready-${i}`],
  }));
}
function finite(canvas: Canvas) {
  for (const n of canvas.nodes) {
    expect(Number.isFinite(n.position.x) && Number.isFinite(n.position.y)).toBe(true);
    expect(n.initialWidth).toBeGreaterThan(0); expect(n.initialHeight).toBeGreaterThan(0);
  }
}

test("status patches preserve geometry, unrelated nodes, edges and no-op snapshots", () => {
  const model = createCanvasModel(), input = jobs(20);
  const a = model.monitoring(input, []);
  input[1] = { ...input[1], status: "RUNNING", startedAt: 1 };
  const b = model.monitoring(input, []);
  expect(model.layoutBuilds).toBe(1); expect(b.lanes).toBe(a.lanes);
  for (let i = 0; i < b.nodes.length; i++) {
    expect(b.nodes[i].position).toBe(a.nodes[i].position);
    if (b.nodes[i].id !== "m-000001") expect(b.nodes[i]).toBe(a.nodes[i]);
  }
  expect(b.edges.filter((e, i) => e !== a.edges[i]).map(e => e.target)).toEqual(["m-000001"]);
  expect(model.monitoring(input.map(i => ({ ...i })), [])).toBe(b);
  expect(model.layoutBuilds).toBe(1);
  finite(b);
});

test("structural signatures invalidate addition, deletion, deps, ODAT, folders, dimensions and config", () => {
  const model = createCanvasModel(); let input = jobs(2);
  const first = model.monitoring(input, []); expect(model.layoutBuilds).toBe(1);
  input = jobs(3); model.monitoring(input, []); expect(model.layoutBuilds).toBe(2);
  input = input.slice(0, 2); model.monitoring(input, []); expect(model.layoutBuilds).toBe(3);
  input[1] = { ...input[1], condsIn: [] }; const detached = model.monitoring(input, []);
  expect(model.layoutBuilds).toBe(4); expect(detached.edges).toHaveLength(0);
  expect(detached.nodes.find(n => n.id === "m-000001")!.position).not.toEqual(first.nodes.find(n => n.id === "m-000001")!.position);
  input[1] = { ...input[1], team: "Other" }; const moved = model.monitoring(input, []);
  expect(model.layoutBuilds).toBe(5); expect(moved.lanes).toHaveLength(2);
  input[1] = { ...input[1], manualOrder: true }; const tagged = model.monitoring(input, []);
  expect(model.layoutBuilds).toBe(6); expect(tagged.nodes.find(n => n.id === "m-000001")!.initialHeight).toBe(99);
  expect(tagged.nodes.find(n => n.id === "m-000000")!.initialHeight).toBe(80);
  input[1] = { ...input[1], carriedFrom: "2026-11-05" }; model.monitoring(input, []); expect(model.layoutBuilds).toBe(7);
  model.monitoring(input, [], { ...DEFAULT_LAYOUT, columns: 2 }); expect(model.layoutBuilds).toBe(8);
  const overridden = model.monitoring(input, [], DEFAULT_LAYOUT, null, new Map([["Load", { columns: 3 }]])); expect(model.layoutBuilds).toBe(9);
  finite(overridden);
});

test("resource and condition changes preserve unrelated objects and layout", () => {
  const model = createCanvasModel(), input = jobs(20);
  input[10] = { ...input[10], resources: { quota: 1 } };
  const a = model.monitoring(input, [], DEFAULT_LAYOUT, null, null, new Set(), new Map());
  const b = model.monitoring(input, [], DEFAULT_LAYOUT, null, null, new Set(), new Map([["quota", { capacity: 1, used: 1 }]]));
  expect(b.nodes.filter((n, i) => n !== a.nodes[i]).map(n => n.id)).toEqual(["m-000010"]);
  expect(b.edges).toBe(a.edges); expect(b.lanes).toBe(a.lanes); expect(model.layoutBuilds).toBe(1);
  const c = model.monitoring(input, [], DEFAULT_LAYOUT, null, null, new Set(["ready-0@2026-11-06"]));
  expect(c.nodes.find(n => n.id === "m-000019")).toBe(b.nodes.find(n => n.id === "m-000019"));
  expect(model.layoutBuilds).toBe(1);
});

test("selection preserves unrelated identity, clears halos, and retains runtime updates", () => {
  const model = createCanvasModel(), input = jobs(20), select = createCanvasSelection();
  const a = model.monitoring(input, []);
  const idle = select(a.nodes, a.edges, new Set(), "m");
  const chosen = select(a.nodes, a.edges, new Set(["000001"]), "m");
  for (let i = 0; i < chosen.length; i++) if (chosen[i].id.startsWith("m-00001")) expect(chosen[i]).toBe(idle[i]);
  expect(chosen.find(n => n.id === "m-000001")!.selected).toBe(true);
  input[1] = { ...input[1], status: "OK" }; const b = model.monitoring(input, []);
  const updated = select(b.nodes, b.edges, new Set(["000001"]), "m");
  expect(updated.find(n => n.id === "m-000001")!.selected).toBe(true);
  expect(updated.find(n => n.id === "m-000001")!.data.status).toBe("SUCCESS");
  const cleared = select(b.nodes, b.edges, new Set(), "m");
  expect(cleared.every(n => !n.selected && n.data.dependencyRelation === undefined)).toBe(true);
  expect(model.layoutBuilds).toBe(1);
});

test("pan applies the latest sample once per frame and cancels after gesture/unmount", () => {
  let tick: FrameRequestCallback | undefined, requests = 0, cancellations = 0;
  const applied: number[] = [];
  const scheduler = latestFrame<number>(n => applied.push(n), cb => { tick = cb; return ++requests; }, () => { tick = undefined; cancellations++; });
  for (let i = 0; i < 100; i++) scheduler.schedule(i);
  expect(requests).toBe(1); expect(applied).toEqual([]);
  tick!(16); expect(applied).toEqual([99]);
  scheduler.schedule(100); scheduler.cancel(); expect(cancellations).toBe(1); expect(tick).toBeUndefined();
  scheduler.schedule(101); tick!(32); expect(applied).toEqual([99, 101]);
});

test("28800 jobs in 120 independent fork/join blocks retain every dependency", () => {
  test.setTimeout(60000);
  const input = jobs(28800);
  const parents = [[], [0], [0], [1], [2], [3], [2], [5, 6], [5, 6], [5, 6]];
  for (let i = 0; i < input.length; i++) {
    const within = i % 10, group = i - within;
    const upstream = within === 0 && i % 240 !== 0 ? [i - 6, i - 3, i - 2, i - 1] : parents[within].map(p => group + p);
    input[i] = { ...input[i], condsIn: upstream.map(p => `ready-${p}`) };
  }
  const model = createCanvasModel(), start = performance.now(), a = model.monitoring(input, []);
  const initialMs = performance.now() - start;
  expect(a.nodes).toHaveLength(28801); expect(a.edges).toHaveLength(45600);
  input[28799] = { ...input[28799], status: "OK" };
  const updateStart = performance.now(), b = model.monitoring(input, []), updateMs = performance.now() - updateStart;
  expect(model.layoutBuilds).toBe(1);
  expect(b.nodes.filter((n, i) => n !== a.nodes[i]).map(n => n.id)).toEqual(["m-028799"]);
  expect(b.edges.filter((e, i) => e !== a.edges[i])).toHaveLength(2);
  console.log(JSON.stringify({ fixture: "120 blocks, 24 fork/join groups of ten each", jobs: 28800, edges: 45600, initialMs, updateMs }));
});

test("measure 100, 500, 1000, 5000 jobs with one status event and a burst", () => {
  test.setTimeout(120000);
  const results = [];
  for (const count of [100, 500, 1000, 5000, ...(process.env.REGENTE_CANVAS_LARGE_BENCHMARK ? [28800] : [])]) {
    enableCanvasMetrics(true);
    const model = createCanvasModel(), input = jobs(count);
    let start = performance.now(); const a = model.monitoring(input, []); const initialMs = performance.now() - start;
    const initialDagreMs = readCanvasMetrics().durations.dagre ?? 0;
    enableCanvasMetrics(true);
    input[0] = { ...input[0], status: "RUNNING", startedAt: 1 };
    start = performance.now(); const b = model.monitoring(input, []); const updateMs = performance.now() - start;
    expect(model.layoutBuilds).toBe(1); expect(readCanvasMetrics().totals.dagre ?? 0).toBe(0);
    expect(b.nodes).toHaveLength(count + 1); expect(b.nodes.at(-1)!.id).toBe(`m-${String(count - 1).padStart(6, "0")}`);
    const nodesRecreated = b.nodes.filter((n, i) => n !== a.nodes[i]).length;
    const edgesRecreated = b.edges.filter((e, i) => e !== a.edges[i]).length;
    expect(nodesRecreated).toBe(1); expect(edgesRecreated).toBe(0);
    for (let i = 0; i < Math.min(100, count); i++) input[i] = { ...input[i], status: "OK" };
    start = performance.now(); const c = model.monitoring(input, []); const burstMs = performance.now() - start;
    expect(model.layoutBuilds).toBe(1); expect(c.nodes).toHaveLength(count + 1);
    results.push({ count, initialMs, initialDagreMs, updateMs, nodesRecreated, edgesRecreated, burstMs, updateDagreCalls: 0 });
  }
  enableCanvasMetrics(false);
  console.log(JSON.stringify(results));
  if (process.env.REGENTE_CANVAS_METRICS_OUTPUT) writeFileSync(process.env.REGENTE_CANVAS_METRICS_OUTPUT, JSON.stringify(results, null, 2));
});
