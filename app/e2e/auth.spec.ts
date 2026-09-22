import { test, expect } from "@playwright/test";
import { spawn } from "node:child_process";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";

for (const mode of ["local", "hybrid", "oidc"]) {
  test(`${mode}: login policy and browser session`, async ({ page, request }, testInfo) => {
    const dir = await mkdtemp(join(tmpdir(), "regente-auth-e2e-"));
    const base = "http://127.0.0.1:18903";
    const bin = resolve("../.integration/server-i03" + (process.platform === "win32" ? ".exe" : ""));
    const args = ["-addr", "127.0.0.1:18903", "-db", join(dir, "state.db"), "-workspace", join(dir, "workspace"),
      "-spa-dir", resolve("dist"), "-auth-mode", mode, "-api-token", "", "-git-source", "", "-selfmon=false"];
    if (mode !== "local") args.push("-oidc-issuer", "http://127.0.0.1:1", "-oidc-client-id", "test", "-oidc-redirect-url", base + "/api/auth/oidc/callback");
    const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith("REGENTE_") && !key.startsWith("GITHUB_") && !key.startsWith("OTEL_")));
    const child = spawn(bin, args, { windowsHide: true, stdio: ["ignore", "pipe", "pipe"], env });
    let output = "";
    child.stdout.on("data", data => { output += data.toString(); });
    child.stderr.on("data", data => { output += data.toString(); });
    try {
      await expect.poll(async () => { try { return (await request.get(base + "/health")).status(); } catch { return 0; } }, { timeout: 20000 }).toBe(200);
      const authConfig = page.waitForResponse(response => response.url() === base + "/api/auth/config" && response.status() === 200);
      const apiOrigins = new Set<string>();
      page.on("request", req => { if (new URL(req.url()).pathname.startsWith("/api/")) apiOrigins.add(new URL(req.url()).origin); });
      await page.goto(base);
      await authConfig; // O SPA servido pelo Go nao pode cair em localStorage silencioso.
      expect([...apiOrigins]).toEqual([base]);
      if (mode === "oidc") {
        await expect(page.getByRole("button", { name: "SSO is currently unavailable" })).toBeVisible();
        await expect(page.getByPlaceholder("Username", { exact: true })).toHaveCount(0);
        expect((await request.post(base + "/api/auth/login", { data: { username: "admin", password: "admin" } })).status()).toBe(403);
        return;
      }
      if (mode === "hybrid") await expect(page.getByText("or use your local account")).toBeVisible();
      await page.getByPlaceholder("Username", { exact: true }).fill("admin");
      await page.getByPlaceholder("Encrypted password").fill("admin");
      await page.getByRole("button", { name: "Sign in", exact: true }).click();
      await page.getByPlaceholder("Enter the new password").fill("synthetic-new-password");
      await page.getByPlaceholder("Repeat the new password").fill("synthetic-new-password");
      const newSession = page.waitForResponse(response => response.url() === base + "/api/auth/login" && response.request().method() === "POST" && response.status() === 200);
      await page.getByRole("button", { name: "Save and sign in" }).click();
      await newSession;
      await expect(page.getByRole("button", { name: "Account", exact: true })).toBeVisible();
      const me = await page.evaluate(async () => { const r = await fetch("/api/auth/me"); return { status: r.status, csrf: r.headers.get("X-CSRF-Token") }; });
      expect(me.status).toBe(200);
      expect(me.csrf).toBeTruthy();
      expect(await page.evaluate(() => localStorage.getItem("regente:authToken"))).toBeNull();
      expect(await page.evaluate(() => document.cookie.includes("regente_session"))).toBe(false);
      await page.reload();
      await expect(page.getByRole("button", { name: "Account", exact: true })).toBeVisible();
      const statuses = await page.evaluate(async () => {
        const me = await fetch("/api/auth/me"); const csrf = me.headers.get("X-CSRF-Token")!;
        const denied = await fetch("/api/auth/logout", { method: "POST" });
        const out = await fetch("/api/auth/logout", { method: "POST", headers: { "X-CSRF-Token": csrf } });
        const after = await fetch("/api/auth/me"); return [denied.status, out.status, after.status];
      });
      expect(statuses).toEqual([403, 204, 401]);
      await page.reload();
      await expect(page.getByPlaceholder("Username", { exact: true })).toBeVisible();
    } finally {
      await testInfo.attach("server.log", { body: output, contentType: "text/plain" });
      child.kill();
      await new Promise<void>(resolveExit => { if (child.exitCode !== null) resolveExit(); else child.once("exit", () => resolveExit()); });
    }
  });
}
