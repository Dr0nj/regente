import { test, expect, type Page, type WebSocketRoute } from "@playwright/test";
import { spawn, type ChildProcess } from "node:child_process";
import { resolve } from "node:path";
import { writeFileSync } from "node:fs";

const base = "http://127.0.0.1:18929";
let vite: ChildProcess;
test.beforeAll(async ({ request }) => {
  vite = spawn(process.execPath, [resolve("node_modules/vite/bin/vite.js"), "--host", "127.0.0.1", "--port", "18929", "--strictPort"], {
    env: { ...process.env, VITE_REGENTE_SERVER_URL: "@origin" }, windowsHide: true, stdio: ["ignore", "pipe", "pipe"],
  });
  let output = "";
  vite.stdout!.on("data", data => { output += data.toString(); });
  vite.stderr!.on("data", data => { output += data.toString(); });
  let requestError = "";
  try { await expect.poll(async () => {
    if (vite.exitCode !== null) throw new Error(output);
    try { return (await request.get(base)).status(); } catch (e) { requestError = String(e); return 0; }
  }, { timeout: 30000 }).toBe(200); } catch (e) { throw new Error(`${e}\n${output}\n${requestError}`, { cause: e }); }
});
test.afterAll(async () => {
  if (!vite || vite.exitCode !== null) return;
  vite.kill(); await new Promise<void>(done => vite.once("exit", () => done()));
});

function fixture(count: number) {
  return Array.from({ length: count }, (_, i) => ({
    id: String(i).padStart(6, "0"), definitionId: `job-${i}`, label: `Job ${i}`, jobType: "COMMAND", team: "Load",
    orderDate: "2026-11-06", scheduledAt: "2026-11-06T00:00:00Z", status: "WAITING",
    condsIn: i % 10 ? [`ready-${i - 1}`] : [], condsOutAdd: [`ready-${i}`],
  }));
}
async function mock(page: Page, count: number) {
  const data = fixture(count);
  let socket: WebSocketRoute | undefined, lists = 0, resources = 0;
  await page.routeWebSocket(/\/ws\/web/, ws => { socket = ws; });
  await page.route("**/api/**", async route => {
    const url = new URL(route.request().url()), path = url.pathname;
    let body: unknown = [];
    if (path === "/api/auth/me") body = { id: 1, username: "Canvas tester", role: "admin" };
    else if (path === "/api/auth/event-ticket") body = { ticket: "synthetic" };
    else if (path === "/api/daily/status") body = { orderDate: "2026-11-06", dailyAt: "00:00", timezone: "UTC" };
    else if (path === "/api/instances") { lists++; expect(url.searchParams.has("limit")).toBe(false); body = data; }
    else if (path === "/api/instances/summary") {
      const q = url.searchParams.get("q"), total = q ? data.filter(i => i.label.includes(q)).length : data.length;
      body = { date: "2026-11-06", total, byStatus: { WAITING: total }, byFolder: { Load: total } };
    }
    else if (path === "/api/instances/page") {
      const q = url.searchParams.get("q"), filtered = q ? data.filter(i => i.label.includes(q)) : data;
      const offset = Number(url.searchParams.get("offset") ?? 0), limit = Number(url.searchParams.get("limit") ?? 200);
      body = { items: filtered.slice(offset, offset + limit), nextCursor: "" };
    }
    else if (path.startsWith("/api/instances/")) body = data.find(i => i.id === path.split("/")[3]) ?? {};
    else if (path === "/api/definitions") body = data.map(i => ({ id: i.definitionId, label: i.label, team: i.team, jobType: i.jobType, schedule: { enabled: true } }));
    else if (path === "/api/folders") body = [{ name: "Load", count: data.length }];
    else if (path === "/api/resources") { resources++; body = []; }
    else if (path === "/api/agents") body = [{ id: "agent", online: true, capabilities: ["COMMAND"] }];
    else if (path === "/api/alerts/unacknowledged-count") body = { count: 0 };
    else if (path === "/api/git/status") body = { configured: false };
    await route.fulfill({ json: body, headers: { "X-CSRF-Token": "synthetic" } });
  });
  await page.addInitScript(() => {
    localStorage.setItem("regente:legacyCap", "500");
    localStorage.setItem("regente:minimap", "1");
    sessionStorage.setItem("regente:viewports", JSON.stringify([["monitoring", { x: 200, y: 40, zoom: 1 }]]));
  });
  return { data, send: (event: string, payload: unknown) => socket!.send(JSON.stringify({ event, payload })),
    connected: () => !!socket, lists: () => lists, resources: () => resources };
}

async function moduleSnapshot(page: Page) {
  return page.evaluate(async () => {
    const path = "/src/lib/server-instance-store.ts", store = await import(path);
    return { count: store.getTodayInstances().length, first: store.getInstance("000000")?.status, last: store.getInstance("005099")?.status };
  });
}

test("WS burst publishes once; complete daily ignores legacyCap; stale REST cannot erase or regress jobs", async ({ page }) => {
  test.setTimeout(60000);
  const api = await mock(page, 5100);
  await page.route(base + "/store-test", route => route.fulfill({ contentType: "text/html", body: "<html><body>Store test</body></html>" }));
  await page.goto(base + "/store-test");
  await page.evaluate(async () => {
    const path = "/src/lib/server-instance-store.ts", store = await import(path);
    Object.assign(window, { publications: [] });
    store.onInstanceChange((instances: unknown[], change: { ids: Set<string> }) => {
      Reflect.get(window, "publications").push({ count: instances.length, changed: [...change.ids] });
    });
    store.getTodayInstances();
  });
  await expect.poll(() => moduleSnapshot(page)).toMatchObject({ count: 5100, last: "WAITING" });
  await expect.poll(api.connected).toBe(true);
  // Await the initial and reconnect refresh publications before measuring a burst.
  await expect.poll(api.lists).toBeGreaterThanOrEqual(1);
  await page.waitForTimeout(100);
  await page.evaluate(() => Reflect.set(window, "publications", []));
  for (let i = 0; i < 40; i++) api.send("instance.changed", { ...api.data[i], status: "RUNNING" });
  await expect.poll(() => page.evaluate(() => Reflect.get(window, "publications").length)).toBe(1);
  const pubs = await page.evaluate(() => Reflect.get(window, "publications"));
  expect(pubs[0].changed).toHaveLength(40); expect(pubs[0].count).toBe(5100);
  const listCount = api.lists();
  await page.route("**/api/instances/000040", route => route.fulfill({ json: { ...api.data[40], status: "HELD", holdScope: "folder", heldFromStatus: "WAITING" } }));
  api.send("instance.changed", { id: "000040", status: "HELD", holdScope: "folder", heldFromStatus: "WAITING" });
  await expect.poll(() => page.evaluate(async () => {
    const path = "/src/lib/server-instance-store.ts", inst = (await import(path)).getInstance("000040");
    return { status: inst?.status, scope: inst?.holdScope };
  })).toEqual({ status: "HOLD", scope: "folder" });
  expect(api.lists()).toBe(listCount);

  let release!: () => void;
  const held = new Promise<void>(done => { release = done; });
  let intercepted = false;
  await page.route("**/api/instances?**", async route => { intercepted = true; await held; await route.fulfill({ json: api.data.slice(0, 20) }); });
  await page.evaluate(() => { const path = "/src/lib/server-instance-store.ts"; void import(path).then(s => s.refreshFromServer()); });
  await expect.poll(() => intercepted).toBe(true);
  api.send("instance.changed", { ...api.data[0], status: "OK" });
  api.send("instance.changed", { ...api.data[0], id: "new-job", status: "WAITING" });
  api.send("instance.deleted", { id: "000001" });
  await expect.poll(async () => (await moduleSnapshot(page)).first).toBe("OK");
  release();
  await page.waitForTimeout(100);
  const checked = await page.evaluate(async () => {
    const path = "/src/lib/server-instance-store.ts", s = await import(path);
    return { count: s.getTodayInstances().length, status: s.getInstance("000000")?.status, added: !!s.getInstance("new-job"), deleted: !!s.getInstance("000001") };
  });
  expect(checked).toEqual({ count: 5100, status: "OK", added: true, deleted: false });
  // A richer but older detail reply must obey the same no-regression rule.
  let releaseDetail!: () => void;
  const detailHeld = new Promise<void>(done => { releaseDetail = done; });
  let detailRequested = false;
  await page.route("**/api/instances/000002", async route => { detailRequested = true; await detailHeld; await route.fulfill({ json: api.data[2] }); });
  await page.evaluate(() => { const path = "/src/lib/server-instance-store.ts"; void import(path).then(s => s.fetchInstanceDetail("000002")); });
  await expect.poll(() => detailRequested).toBe(true);
  api.send("instance.deleted", { id: "000002" });
  await page.waitForTimeout(30);
  releaseDetail();
  await page.waitForTimeout(50);
  expect(await page.evaluate(async () => { const path = "/src/lib/server-instance-store.ts"; return !!(await import(path)).getInstance("000002"); })).toBe(false);
});

test("real canvas virtualizes 5100 jobs, focuses an offscreen job and survives updates and pan", async ({ page }, testInfo) => {
  test.setTimeout(90000);
  const api = await mock(page, 5100), errors: string[] = [];
  page.on("pageerror", e => errors.push(e.message));
  await page.setViewportSize({ width: 1600, height: 1000 });
  // Emulate delayed/lost node measurements; pane sizing still works normally.
  await page.addInitScript(() => {
    const Native = ResizeObserver;
    window.ResizeObserver = class extends Native {
      observe(target: Element, options?: ResizeObserverOptions) {
        if (!target.classList.contains("react-flow__node")) super.observe(target, options);
      }
    };
  });
  await page.goto(base);
  await expect(page.getByRole("button", { name: "Account", exact: true })).toBeVisible();
  await expect.poll(() => moduleSnapshot(page)).toMatchObject({ count: 5100 });
  await expect(page.locator(".react-flow__node-jobV2").first()).toBeVisible();
  expect(await page.locator(".react-flow__node-jobV2").count()).toBeLessThan(5100);
  expect(api.resources()).toBe(0);
  await page.evaluate(async () => { const path = "/src/v2/canvas-metrics.ts"; (await import(path)).enableCanvasMetrics(true); });
  // Search the real sidebar, then focus the last job outside the initial viewport.
  const search = page.getByPlaceholder(/Filter by/i).first();
  await search.fill("Job 5099");
  await page.getByText("Job 5099", { exact: true }).first().click();
  const last = page.locator('.react-flow__node-jobV2[data-id="m-005099"]');
  await expect(last).toBeVisible();
  // Exclude the explicit 350ms focus animation from the status measurement.
  await page.waitForTimeout(400);
  const position = await last.getAttribute("style");
  await page.evaluate(async () => {
    const path = "/src/lib/server-client.ts", client = await import(path);
    let received = 0;
    const off = client.onServerEvent((ev: { event: string; payload?: { id?: string } }) => {
      if (ev.event === "instance.changed" && ev.payload?.id === "005099") received = performance.now();
    });
    const node = document.querySelector('[data-id="m-005099"]')!;
    const observer = new MutationObserver(() => {
      if (received && node.textContent?.includes("RUNNING")) {
        observer.disconnect(); off();
        requestAnimationFrame(() => Reflect.set(window, "eventToFrameMs", performance.now() - received));
      }
    });
    observer.observe(node, { subtree: true, childList: true, characterData: true });
  });
  await page.evaluate(async () => { const path = "/src/v2/canvas-metrics.ts"; (await import(path)).enableCanvasMetrics(true); });
  const started = Date.now();
  api.data[5099].status = "RUNNING";
  api.send("instance.changed", { ...api.data[5099], status: "RUNNING", startedAt: "2026-11-06T10:00:00Z" });
  await expect(last).toContainText("RUNNING");
  const visualLatencyMs = Date.now() - started;
  const statusMetrics = await page.evaluate(async () => { const path = "/src/v2/canvas-metrics.ts"; return (await import(path)).readCanvasMetrics(); });
  expect(statusMetrics.totals.jobRender).toBeLessThanOrEqual(4); // Includes React's development StrictMode double render.
  expect(await last.getAttribute("style")).toBe(position);
  // Many mouse samples in one frame must collapse to a single camera write.
  await page.evaluate(() => {
    const pane = document.querySelector(".react-flow__pane")!;
    pane.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, button: 0, clientX: 900, clientY: 600 }));
    for (let i = 0; i < 100; i++) window.dispatchEvent(new MouseEvent("mousemove", { clientX: 900 - i, clientY: 600 }));
  });
  await page.evaluate(() => new Promise<void>(done => requestAnimationFrame(() => done())));
  await page.evaluate(() => window.dispatchEvent(new MouseEvent("mouseup")));
  const metrics = await page.evaluate(async () => { const path = "/src/v2/canvas-metrics.ts"; return (await import(path)).readCanvasMetrics(); });
  expect(metrics.totals.panViewport).toBe(1); expect(metrics.totals.dagre ?? 0).toBe(0);
  const continuous = await page.evaluate(async () => {
    const path = "/src/v2/canvas-metrics.ts", diagnostics = await import(path);
    diagnostics.enableCanvasMetrics(true);
    const pane = document.querySelector(".react-flow__pane")!;
    pane.dispatchEvent(new MouseEvent("mousedown", { bubbles: true, button: 0, clientX: 900, clientY: 600 }));
    const times: number[] = [];
    for (let i = 0; i < 60; i++) {
      window.dispatchEvent(new MouseEvent("mousemove", { clientX: 900 - i * 2, clientY: 600 }));
      await new Promise<void>(done => requestAnimationFrame(t => { times.push(t); done(); }));
    }
    window.dispatchEvent(new MouseEvent("mouseup"));
    const intervals = times.slice(1).map((t, i) => t - times[i]);
    return { intervals, missedFrames: intervals.reduce((n, ms) => n + Math.max(0, Math.round(ms / (1000 / 60)) - 1), 0), metrics: diagnostics.readCanvasMetrics(), eventToFrameMs: Reflect.get(window, "eventToFrameMs") };
  });
  expect(continuous.metrics.totals.panViewport).toBeLessThanOrEqual(60);
  expect(continuous.metrics.totals.dagre ?? 0).toBe(0); expect(continuous.metrics.totals.build ?? 0).toBe(0);
  const predecessor = page.locator('.react-flow__node-jobV2[data-id="m-005098"]');
  await predecessor.click({ modifiers: ["Shift"] });
  await expect(predecessor).toHaveClass(/selected/); await expect(last).toHaveClass(/selected/);
  // Native Shift+drag still owns selection; the custom pan handler yields it.
  const a = await predecessor.boundingBox(), b = await last.boundingBox();
  await page.keyboard.down("Shift");
  await page.mouse.move(a!.x - 8, a!.y - 8); await page.mouse.down();
  await page.mouse.move(b!.x + b!.width + 8, b!.y + b!.height + 8, { steps: 12 });
  await page.mouse.up(); await page.keyboard.up("Shift");
  await expect(predecessor).toHaveClass(/selected/); await expect(last).toHaveClass(/selected/);
  await expect(last).toBeVisible(); expect(errors).toEqual([]);
  await page.screenshot({ path: testInfo.outputPath("canvas-5100.png") });
  const metricsFile = testInfo.outputPath("canvas-metrics.json");
  writeFileSync(metricsFile, JSON.stringify({ visualLatencyMs, statusMetrics, ...metrics, continuous }, null, 2));
  await testInfo.attach("canvas-metrics.json", { path: metricsFile, contentType: "application/json" });
});


// Gate visual da issue #39: executar após o ensaio diário, sem disputar CPU.
test("canvas zoom LOD and minimap navigation preserve layout in dark and light themes", async ({ page }, testInfo) => {
  const api = await mock(page, 20), errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  api.data[0].status = "OK"; api.data[1].status = "NOTOK";
  api.data[2].status = "RUNNING"; api.data[3].status = "UNCERTAIN";
  api.data[4].status = "HELD";
  Object.assign(api.data[5], { confirmReq: true });
  await page.setViewportSize({ width: 1600, height: 1000 });
  await page.goto(base);
  await expect(page.locator(".v2-job-card").first()).toBeVisible();
  await expect.poll(api.connected).toBe(true);
  const map = page.getByRole("group", { name: /^Navigation map:/ });
  await expect(map).toBeVisible();
  const zoom = page.getByRole("status", { name: "Canvas zoom percentage" });
  const initialZoom = await zoom.textContent();
  await page.evaluate(async () => { const path = "/src/v2/canvas-metrics.ts"; (await import(path)).enableCanvasMetrics(true); });
  await page.getByRole("button", { name: "Zoom in minimap", exact: true }).click();
  await expect(zoom).toHaveText(initialZoom!);
  await expect(page.locator(".v2-minimap-footer")).toContainText("150% map");
  await map.focus(); await page.keyboard.press("+"); await page.keyboard.press("ArrowRight");
  await expect(zoom).toHaveText(initialZoom!);
  await page.getByRole("button", { name: "Fit minimap", exact: true }).click();
  await expect(page.locator(".v2-minimap-footer")).toContainText("100% map");
  await page.getByRole("button", { name: /^Focus folder Load,/ }).press("Enter");
  await expect(zoom).toHaveText(initialZoom!);
  await page.getByRole("button", { name: "Collapse minimap", exact: true }).click();
  await expect(map).not.toBeVisible();
  expect(await page.evaluate(() => localStorage.getItem("regente:minimap-collapsed"))).toBe("1");
  await page.getByRole("button", { name: "Expand minimap", exact: true }).click();
  // 100% / 1.25^8 = 16.78%, comparável à referência de 17%.
  for (let i = 0; i < 8; i++) await page.getByRole("button", { name: "Zoom out canvas", exact: true }).click();
  await expect(zoom).toHaveText("17%");
  await expect(page.locator("[data-canvas-detail=solid]")).toHaveCount(1);
  for (const theme of ["escuro", "claro"]) {
    await page.evaluate(theme => document.documentElement.setAttribute("data-theme", theme), theme);
    const styles = await page.locator(".v2-job-card").evaluateAll(cards => cards.map(card => {
      const style = getComputedStyle(card), expected = document.createElement("span");
      expected.style.backgroundColor = style.getPropertyValue("--job-status-color"); card.appendChild(expected);
      const color = getComputedStyle(expected).backgroundColor; expected.remove();
      return { color: style.backgroundColor, expected: color, hidden: getComputedStyle(card.querySelector(".v2-job-content")!).visibility };
    }));
    expect(styles.length).toBeGreaterThan(0);
    for (const style of styles) { expect(style.color).toBe(style.expected); expect(style.hidden).toBe("hidden"); }
    await page.screenshot({ path: testInfo.outputPath(`canvas-solid-17-${theme}.png`) });
  }
  for (let i = 0; i < 8; i++) await page.getByRole("button", { name: "Zoom in canvas", exact: true }).click();
  await expect(zoom).toHaveText("100%");
  await expect(page.locator("[data-canvas-detail=detail]")).toHaveCount(1);
  await page.screenshot({ path: testInfo.outputPath("canvas-detail-100-light.png") });
  for (const theme of ["escuro", "claro"]) {
    await page.evaluate(theme => document.documentElement.setAttribute("data-theme", theme), theme);
    const contrast = await page.locator(".v2-job-card").evaluateAll(cards => {
      const canvas = document.createElement("canvas"); canvas.width = 1; canvas.height = 1;
      const context = canvas.getContext("2d")!;
      const luminance = (color: string) => {
        context.clearRect(0, 0, 1, 1); context.fillStyle = color; context.fillRect(0, 0, 1, 1);
        const rgb = [...context.getImageData(0, 0, 1, 1).data].slice(0, 3).map(value => {
          const channel = value / 255; return channel <= .04045 ? channel / 12.92 : ((channel + .055) / 1.055) ** 2.4;
        });
        return rgb[0] * .2126 + rgb[1] * .7152 + rgb[2] * .0722;
      };
      return cards.map(card => {
        const name = card.querySelector(".v2-job-content span")!;
        const a = luminance(getComputedStyle(name).color), b = luminance(getComputedStyle(card).backgroundColor);
        return { label: name.textContent, ratio: (Math.max(a, b) + .05) / (Math.min(a, b) + .05) };
      });
    });
    for (const result of contrast) expect(result.ratio, `${theme}: ${result.label}`).toBeGreaterThanOrEqual(4.5);
  }

  const metrics = await page.evaluate(async () => { const path = "/src/v2/canvas-metrics.ts"; return (await import(path)).readCanvasMetrics(); });
  expect(metrics.totals.dagre ?? 0).toBe(0); expect(metrics.totals.minimapModelBuild ?? 0).toBe(0);
  await page.getByRole("button", { name: "Fit canvas", exact: true }).click();
  await expect(page.locator(".v2-job-card").first()).toBeVisible();
  expect(errors).toEqual([]);
});


test("minimap supports keyboard resize, camera navigation and legend dismissal", async ({ page }) => {
  await mock(page, 20);
  await page.setViewportSize({ width: 1600, height: 1000 });
  await page.goto(base);
  const map = page.getByRole("group", { name: /^Navigation map:/ });
  await expect(map).toBeVisible();
  const canvasZoom = page.getByRole("status", { name: "Canvas zoom percentage" });
  const zoomBefore = await canvasZoom.textContent();
  // Arrasto real: amostra uma área da viewport não coberta por label de folder.
  const dragPoint = await page.locator(".v2-minimap-viewport").evaluate(rect => {
    const box = rect.getBoundingClientRect(), pane = rect.closest("svg")!.getBoundingClientRect();
    const left = Math.max(box.left, pane.left), right = Math.min(box.right, pane.right);
    const top = Math.max(box.top, pane.top), bottom = Math.min(box.bottom, pane.bottom);
    if (right <= left || bottom <= top) return null;
    for (const fraction of [.5, .7, .3, .9]) {
      const point = { x: left + (right - left) / 2, y: top + (bottom - top) * fraction };
      if (document.elementFromPoint(point.x, point.y) === rect) return point;
    }
    return null;
  });
  expect(dragPoint).not.toBeNull();
  await page.evaluate(async () => { const path = "/src/v2/canvas-metrics.ts"; (await import(path)).enableCanvasMetrics(true); });
  await page.mouse.move(dragPoint!.x, dragPoint!.y); await page.mouse.down();
  await page.mouse.move(dragPoint!.x + 12, dragPoint!.y + 6, { steps: 12 }); await page.mouse.up();
  await expect(canvasZoom).toHaveText(zoomBefore!);
  const dragMetrics = await page.evaluate(async () => { const path = "/src/v2/canvas-metrics.ts"; return (await import(path)).readCanvasMetrics(); });
  expect(dragMetrics.totals.minimapPointerFrame).toBeGreaterThan(0);
  expect(dragMetrics.totals.minimapRaster ?? 0).toBe(0);
  expect(dragMetrics.totals.minimapModelBuild ?? 0).toBe(0);
  expect(dragMetrics.totals.dagre ?? 0).toBe(0);
  await page.getByRole("button", { name: "Zoom in minimap", exact: true }).click();
  await map.focus();
  await page.keyboard.press("Control+Home");
  await expect(page.locator(".v2-minimap-footer")).toContainText("150% map");
  const before = await page.locator(".react-flow__viewport").getAttribute("style");
  await page.keyboard.press("Shift+ArrowRight");
  await expect(page.locator(".react-flow__viewport")).not.toHaveAttribute("style", before!);
  await expect(canvasZoom).toHaveText(zoomBefore!);
  await expect(page.locator(".v2-minimap-footer")).toContainText("150% map");

  const legendButton = page.getByRole("button", { name: "Minimap status legend", exact: true });
  await legendButton.press("Enter");
  const legend = page.getByRole("region", { name: "Job status legend", exact: true });
  await expect(legend).toBeVisible();
  await expect(legend).toContainText("WAIT RESOURCE");
  await expect(legend).toContainText("WAIT COND");
  await expect(legend).toContainText("IDLE");
  await map.focus(); await page.keyboard.press("Escape");
  await expect(legend).not.toBeVisible(); await expect(legendButton).toBeFocused();
  await expect(legendButton).toHaveAttribute("aria-expanded", "false");

  const resize = page.getByRole("button", { name: "Resize minimap", exact: true });
  const handle = await resize.boundingBox();
  await page.mouse.move(handle!.x + handle!.width / 2, handle!.y + handle!.height / 2); await page.mouse.down();
  await page.mouse.move(handle!.x + handle!.width / 2 - 20, handle!.y + handle!.height / 2 - 20, { steps: 8 }); await page.mouse.up();
  await expect(page.getByRole("region", { name: "Workflow navigation map", exact: true })).toHaveCSS("width", "280px");
  await resize.focus(); await page.keyboard.press("Home");
  await page.keyboard.press("ArrowRight"); await page.keyboard.press("Shift+ArrowDown");
  await expect(page.getByRole("region", { name: "Workflow navigation map", exact: true })).toHaveCSS("width", "270px");
  await expect(page.getByRole("region", { name: "Workflow navigation map", exact: true })).toHaveCSS("height", "208px");
  await expect(resize).toBeFocused();
  await page.keyboard.press("Home");
  await expect(page.getByRole("region", { name: "Workflow navigation map", exact: true })).toHaveCSS("height", "168px");
  await page.getByRole("button", { name: "Collapse minimap", exact: true }).press("Enter");
  await expect(resize).not.toBeVisible();
  await page.reload();
  await expect(page.getByRole("button", { name: "Expand minimap", exact: true })).toBeVisible();
  await expect(map).not.toBeVisible();
  await page.getByRole("button", { name: "Expand minimap", exact: true }).press("Enter");
  await expect(map).toBeVisible();
  await expect(resize).toBeVisible();
  const out = page.getByRole("button", { name: "Zoom out canvas", exact: true });
  for (let i = 0; i < 30 && await out.isEnabled(); i++) await out.click();
  await expect(canvasZoom).toHaveText("5%"); await expect(out).toBeDisabled();
  const into = page.getByRole("button", { name: "Zoom in canvas", exact: true });
  for (let i = 0; i < 30 && await into.isEnabled(); i++) await into.click();
  await expect(canvasZoom).toHaveText("400%"); await expect(into).toBeDisabled();
});
