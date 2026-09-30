import { test, expect } from "@playwright/test";
import { spawn, spawnSync } from "node:child_process";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";

test.setTimeout(60000);
test("I07: incomplete daily resumes its frozen plan in Monitoring", async ({ page, request }, testInfo) => {
  const dir = await mkdtemp(join(tmpdir(), "regente-daily-e2e-"));
  const base = "http://127.0.0.1:18907";
  const bin = resolve("../.integration/server-i03" + (process.platform === "win32" ? ".exe" : ""));
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith("REGENTE_") && !key.startsWith("GITHUB_") && !key.startsWith("OTEL_")));
  const child = spawn(bin, ["-addr", "127.0.0.1:18907", "-db", join(dir, "state.db"), "-workspace", join(dir, "workspace"), "-spa-dir", resolve("dist"), "-auth-mode", "local", "-api-token", "synthetic-time-admin", "-git-source", "", "-selfmon=false", "-server-agent=false", "-scheduler=external"], { windowsHide: true, stdio: ["ignore", "pipe", "pipe"], env });
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
    const status = await (await request.get(base + "/api/daily/status", { headers })).json();
    const date = status.orderDate as string;
    const plan = JSON.stringify([{ id: "daily-ui", label: "Frozen daily UI", team: "Daily", jobType: "COMMAND", confirm: true, schedule: { enabled: true }, actionConfig: { command: "echo frozen" }, _businessTime: { timezone: "UTC", dailyAt: "00:00" } }]);
    // Fixture de falha num DB descartável real; o Resume é HTTP real, sem mock.
    const seed = spawnSync(process.platform === "win32" ? "python" : "python3", ["-c", "import sqlite3,sys,hashlib; c=sqlite3.connect(sys.argv[1]); raw=sys.argv[3]; date=sys.argv[2]; checksum=hashlib.sha256((date+'\\n\\n'+raw).encode()).hexdigest(); c.execute(\"INSERT INTO daily_runs(order_date,started_at,state,expected_count,plan_json,plan_checksum,last_error) VALUES(?,datetime('now'),'failed',1,?,?,?)\",(date,raw,checksum,'Synthetic interrupted daily')); c.commit(); c.close()", join(dir,"state.db"), date, plan], { windowsHide: true, encoding: "utf8" });
    expect(seed.status, seed.stderr).toBe(0);
    await page.goto(base);
    await page.getByPlaceholder("Username", { exact: true }).fill("time-reader");
    await page.getByPlaceholder("Encrypted password").fill("synthetic-password");
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await expect(page.getByRole("button", { name: "Account", exact: true })).toBeVisible();
    await expect(page.getByRole("status").filter({ hasText: "Daily " + date + " incomplete" })).toContainText("failed · 0/1");
    await page.getByRole("button", { name: "Resume daily", exact: true }).click();
    await expect(page.getByRole("button", { name: "Resume daily", exact: true })).toHaveCount(0);
    const final = await (await request.get(base + "/api/daily/status", { headers })).json();
    expect(final.run.state).toBe("completed"); expect(final.run.inserted).toBe(1); expect(final.pending).toBeNull();
    const orders = await (await request.get(base + "/api/instances?date=" + date, { headers })).json();
    expect(orders).toHaveLength(1); expect(orders[0].label).toBe("Frozen daily UI");
  } finally {
    await testInfo.attach("server.log", { body: output, contentType: "text/plain" });
    child.kill();
    await new Promise<void>(done => { if (child.exitCode !== null) done(); else child.once("exit", () => done()); });
  }
});
