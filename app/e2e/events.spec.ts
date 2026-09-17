import { test, expect } from "@playwright/test";
import { spawn } from "node:child_process";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";

test("I04: scoped frames and automatic reconnect after ACL change", async ({ page, request }, testInfo) => {
  const dir = await mkdtemp(join(tmpdir(), "regente-events-e2e-"));
  const base = "http://127.0.0.1:18904";
  const bin = resolve("../.integration/server-i03" + (process.platform === "win32" ? ".exe" : ""));
  const args = ["-addr", "127.0.0.1:18904", "-db", join(dir, "state.db"), "-workspace", join(dir, "workspace"),
    "-spa-dir", resolve("dist"), "-auth-mode", "local", "-api-token", "synthetic-events-admin",
    "-git-source", "", "-selfmon=false", "-server-agent=false", "-scheduler=external"];
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith("REGENTE_") && !key.startsWith("GITHUB_") && !key.startsWith("OTEL_")));
  const child = spawn(bin, args, { windowsHide: true, stdio: ["ignore", "pipe", "pipe"], env });
  let output = "";
  child.stdout.on("data", data => { output += data.toString(); });
  child.stderr.on("data", data => { output += data.toString(); });
  const headers = { Authorization: "Bearer synthetic-events-admin" };
  const frames: string[] = [];
  const sockets: string[] = [];
  page.on("websocket", socket => {
    sockets.push(socket.url());
    socket.on("framereceived", frame => frames.push(frame.payload.toString()));
  });
  try {
    await expect.poll(async () => { try { return (await request.get(base + "/health")).status(); } catch { return 0; } }, { timeout: 20000 }).toBe(200);
    const created = await request.post(base + "/api/users", { headers, data: { username: "reader", password: "synthetic-password", role: "viewer" } });
    expect(created.status()).toBe(200);
    const user = await created.json();
    expect((await request.patch(base + "/api/users/" + user.id + "/password", { headers, data: { next: "synthetic-password" } })).status()).toBe(204);
    expect((await request.put(base + "/api/users/" + user.id + "/acls", { headers, data: [{ folder: "A", perms: "r" }] })).status()).toBe(200);
    const save = async (team: string, label: string) => {
      const response = await request.post(base + "/api/definitions", { headers, data: {
        id: "job-" + team, team, label, jobType: "COMMAND", actionConfig: { command: "echo synthetic" },
        schedule: { enabled: false },
      } });
      expect(response.status()).toBe(200);
    };
    await save("A", "Visible A");
    await save("B", "Private B");
    await page.goto(base);
    await page.getByPlaceholder("Username", { exact: true }).fill("reader");
    await page.getByPlaceholder("Encrypted password").fill("synthetic-password");
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(page.getByRole("button", { name: "Account", exact: true })).toBeVisible();
    await expect.poll(() => sockets.length).toBeGreaterThan(0);
    // A sentinela é uma resposta do servidor no MESMO canal, não um sleep.
    const sentinel = async () => {
      const before = frames.length;
      expect((await request.put(base + "/api/settings", { headers, data: { env_label: "event-lab", github_token: "synthetic-secret" } })).status()).toBe(200);
      await expect.poll(() => frames.slice(before).some(raw => JSON.parse(raw).event === "settings.changed")).toBe(true);
      return frames.slice(before);
    };
    await sentinel();
    let start = frames.length;
    await save("B", "Private B changed");
    await sentinel();
    expect(frames.slice(start).some(raw => JSON.parse(raw).event === "_resync")).toBe(false);
    expect(frames.join("")).not.toContain("synthetic-secret");
    expect(frames.join("")).not.toContain("Private B");
    start = frames.length;
    await save("A", "Visible A changed");
    await expect.poll(() => frames.slice(start).some(raw => JSON.parse(raw).event === "_resync")).toBe(true);

    const beforeReconnect = sockets.length;
    expect((await request.put(base + "/api/users/" + user.id + "/acls", { headers, data: [{ folder: "B", perms: "r" }] })).status()).toBe(200);
    await expect.poll(() => sockets.length, { timeout: 10000 }).toBeGreaterThan(beforeReconnect);
    await sentinel();
    const visible = await page.evaluate(async () => (await fetch("/api/definitions")).json());
    expect(visible.map((def: { team: string }) => def.team)).toEqual(["B"]);
    start = frames.length;
    await save("A", "Now private A");
    await sentinel();
    expect(frames.slice(start).some(raw => JSON.parse(raw).event === "_resync")).toBe(false);
    start = frames.length;
    await save("B", "Now visible B");
    await expect.poll(() => frames.slice(start).some(raw => JSON.parse(raw).event === "_resync")).toBe(true);
    for (const url of sockets) {
      expect(new URL(url).searchParams.has("ticket")).toBe(true);
      expect(new URL(url).searchParams.has("token")).toBe(false);
    }
  } finally {
    await testInfo.attach("server.log", { body: output, contentType: "text/plain" });
    await testInfo.attach("received-events.json", { body: JSON.stringify(frames, null, 2), contentType: "application/json" });
    child.kill();
    await new Promise<void>(done => { if (child.exitCode !== null) done(); else child.once("exit", () => done()); });
  }
});
