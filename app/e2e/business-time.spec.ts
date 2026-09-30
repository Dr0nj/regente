import { test, expect } from "@playwright/test";
import { spawn } from "node:child_process";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";

test.use({ timezoneId: "Asia/Tokyo" });
test("I06: frozen business timezone survives settings changes in Monitoring", async ({ page, request }, testInfo) => {
  const dir = await mkdtemp(join(tmpdir(), "regente-time-e2e-"));
  const base = "http://127.0.0.1:18906";
  const bin = resolve("../.integration/server-i03" + (process.platform === "win32" ? ".exe" : ""));
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith("REGENTE_") && !key.startsWith("GITHUB_") && !key.startsWith("OTEL_")));
  const child = spawn(bin, ["-addr", "127.0.0.1:18906", "-db", join(dir, "state.db"), "-workspace", join(dir, "workspace"), "-spa-dir", resolve("dist"), "-auth-mode", "local", "-api-token", "synthetic-time-admin", "-git-source", "", "-selfmon=false", "-server-agent=false", "-scheduler=external"], { windowsHide: true, stdio: ["ignore", "pipe", "pipe"], env });
  let output = "";
  child.stdout.on("data", data => { output += data.toString(); });
  child.stderr.on("data", data => { output += data.toString(); });
  const headers = { Authorization: "Bearer synthetic-time-admin" };
  try {
    await expect.poll(async () => { try { return (await request.get(base + "/health")).status(); } catch { return 0; } }, { timeout: 20000 }).toBe(200);
    const created = await request.post(base + "/api/users", { headers, data: { username: "time-reader", password: "synthetic-password", role: "admin" } });
    expect(created.status()).toBe(200);
    const user = await created.json();
    expect((await request.patch(base + "/api/users/" + user.id + "/password", { headers, data: { next: "synthetic-password" } })).status()).toBe(204);
    expect((await request.put(base + "/api/settings", { headers, data: { daily_timezone: "America/Sao_Paulo", daily_at: "06:00" } })).status()).toBe(200);
    expect((await request.post(base + "/api/definitions", { headers, data: { id: "clock-ui", team: "Time", label: "Clock UI", jobType: "COMMAND", confirm: true, actionConfig: { command: "echo synthetic" }, schedule: { enabled: false, windowFrom: "23:00", windowTo: "02:00" } } })).status()).toBe(200);
    expect((await request.post(base + "/api/definitions/clock-ui/force", { headers })).status()).toBe(200);
    // Mantém a mesma data de negócio ao mudar somente a zona; a ordem retém a foto.
    expect((await request.put(base + "/api/settings", { headers, data: { daily_timezone: "America/Argentina/Buenos_Aires", daily_at: "06:00" } })).status()).toBe(200);
    await page.goto(base);
    await page.getByPlaceholder("Username", { exact: true }).fill("time-reader");
    await page.getByPlaceholder("Encrypted password").fill("synthetic-password");
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(page.getByRole("button", { name: "Account", exact: true })).toBeVisible();
    await page.getByText("Clock UI", { exact: true }).first().click();
    await expect(page.getByText("America/Sao_Paulo · rollover 06:00", { exact: true })).toBeVisible();
    await expect(page.getByText(/GMT-3/).first()).toBeVisible();
    await page.getByRole("button", { name: "Schedule", exact: true }).click();
    await expect(page.getByText("America/Sao_Paulo · rollover 06:00", { exact: true })).toBeVisible();
    await expect(page.getByText("23:00 → 02:00", { exact: true })).toBeVisible();
  } finally {
    await testInfo.attach("server.log", { body: output, contentType: "text/plain" });
    child.kill();
    await new Promise<void>(done => { if (child.exitCode !== null) done(); else child.once("exit", () => done()); });
  }
});
