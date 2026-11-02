import { test, expect } from "@playwright/test";
import { spawn } from "node:child_process";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";

test("REV05: login and portal defer console assets", async ({ page, request }) => {
  const dir = await mkdtemp(join(tmpdir(), "regente-loading-"));
  const base = "http://127.0.0.1:18921";
  const env = Object.fromEntries(Object.entries(process.env).filter(([k]) => !k.startsWith("REGENTE_") && !k.startsWith("GITHUB_") && !k.startsWith("OTEL_")));
  env.REGENTE_BOOTSTRAP_PASSWORD = "synthetic-loading-password";
  const child = spawn(resolve("../.integration/server-i03" + (process.platform === "win32" ? ".exe" : "")),
    ["-addr", "127.0.0.1:18921", "-db", join(dir,"state.db"), "-workspace", join(dir,"workspace"), "-spa-dir", resolve("dist"), "-selfmon=false"],
    { env, windowsHide: true, stdio: "ignore" });
  try {
    await expect.poll(async () => { try { return (await request.get(base+"/health")).status(); } catch { return 0; } }).toBe(200);
    const scripts: string[] = [];
    page.on("request", req => { if (req.resourceType() === "script") scripts.push(req.url()); });
    // A forged cached identity must not initialize console stores before /me.
    await page.addInitScript(() => localStorage.setItem("regente:user", JSON.stringify({id:1,username:"admin",role:"admin"})));
    await page.goto(base+"/portal");
    await expect(page.getByPlaceholder("Username",{exact:true})).toBeVisible();
    expect(scripts.some(s => /V2Preview|PortalView|SettingsDialog/.test(s))).toBe(false);
    const bytes = await page.evaluate(() => performance.getEntriesByType("resource").filter(r => r.name.endsWith(".js")).reduce((sum,r) => sum+(r as PerformanceResourceTiming).decodedBodySize,0));
    expect(bytes).toBeLessThan(350000);
    await page.getByPlaceholder("Username",{exact:true}).fill("admin");
    await page.getByPlaceholder("Encrypted password").fill(env.REGENTE_BOOTSTRAP_PASSWORD);
    await page.getByRole("button",{name:"Sign in",exact:true}).click();
    await expect(page.getByRole("heading",{name:"Portal self-service",exact:true})).toBeVisible();
    expect(scripts.some(s => /PortalView/.test(s))).toBe(true);
    expect(scripts.some(s => /V2Preview|SettingsDialog/.test(s))).toBe(false);
    await page.goto(base);
    await expect(page.getByRole("button",{name:"Account",exact:true})).toBeVisible();
    expect(scripts.some(s => /V2Preview/.test(s))).toBe(true);
    expect(scripts.some(s => /SettingsDialog/.test(s))).toBe(false);
    await page.getByRole("button",{name:"Account",exact:true}).click();
    await page.getByRole("button",{name:"Settings",exact:true}).click();
    await expect(page.getByRole("button",{name:"Agents",exact:true})).toBeVisible();
    expect(scripts.some(s => /SettingsDialog/.test(s))).toBe(true);
  } finally {
    child.kill();
    await new Promise<void>(r => child.exitCode !== null ? r() : child.once("exit",() => r()));
    await rm(dir,{recursive:true,force:true});
  }
});
