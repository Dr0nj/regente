// Run inside the disposable browser container with its own NSS trust database.
const fs = require('node:fs');
const path = require('node:path');
const https = require('node:https');
const crypto = require('node:crypto');
const { chromium, expect } = require('@playwright/test');
const state = process.env.LAB_STATE;
const base = process.env.LAB_BROWSER_URL;
if (!state || !base) throw new Error('LAB_STATE and LAB_BROWSER_URL are required');
const ca = fs.readFileSync(path.join(state, 'ca.crt'));
const privateData = JSON.parse(fs.readFileSync(path.join(state, 'private.json')));
const report = { status: 'running', roles: {} };
const output = path.join(state, 'browser-check.json');
if (fs.existsSync(output)) throw new Error('Preserve existing browser evidence before another run');
function request(route, token, body, method) {
  return new Promise((resolve, reject) => {
    const req = https.request(base + route, {
      ca, method: method || (body === undefined ? 'GET' : 'POST'),
      headers: { 'Content-Type': 'application/json', ...(token ? { Authorization: 'Bearer ' + token } : {}) },
      timeout: 10000,
    }, res => {
      let raw = '';
      res.on('data', b => { raw += b; });
      res.on('end', () => {
        let data; try { data = JSON.parse(raw); } catch { data = null; }
        resolve({ status: res.statusCode, data });
      });
    });
    req.on('error', reject);
    req.on('timeout', () => req.destroy(new Error('HTTPS request timed out')));
    if (body !== undefined) req.write(JSON.stringify(body));
    req.end();
  });
}
async function main() {
  const login = await request('/api/auth/login', null, { username: 'admin', password: privateData.password });
  expect(login.status).toBe(200);
  const admin = login.data.token;
  const accountFile = path.join(state, 'browser-accounts.json');
  const accounts = fs.existsSync(accountFile) ? JSON.parse(fs.readFileSync(accountFile)) : {};
  for (const role of ['viewer', 'operator']) {
    if (accounts[role]) continue;
    const password = crypto.randomBytes(24).toString('base64url');
    const username = 'qualification-' + role;
    const created = await request('/api/users', admin, { username, password, role });
    expect(created.status).toBe(200);
    const id = created.data.id;
    expect((await request('/api/users/' + id + '/password', admin, { next: password }, 'PATCH')).status).toBe(204);
    const session = await request('/api/auth/login', null, { username, password });
    expect(session.status).toBe(200);
    accounts[role] = { id, username, password, token: session.data.token };
  }
  fs.writeFileSync(path.join(state, 'browser-accounts.json'), JSON.stringify(accounts), { mode: 0o600 });
  for (const role of ['viewer', 'operator', 'admin']) {
    const token = role === 'admin' ? admin : accounts[role].token;
    const read = await request('/api/definitions', token);
    const write = await request('/api/definitions', token, {
      id: 'role-' + role, label: 'Qualification ' + role, team: 'browser-qualification', jobType: 'COMMAND',
      actionConfig: { command: 'echo role-check' }, schedule: { enabled: false },
    });
    const manage = await request('/api/users', token);
    expect(read.status).toBe(200);
    expect(write.status).toBe(role === 'viewer' ? 403 : 200);
    expect(manage.status).toBe(role === 'admin' ? 200 : 403);
    report.roles[role] = { read: read.status, write: write.status, administer: manage.status };
  }
  const viewer = accounts.viewer;
  const acl = rows => request('/api/users/' + viewer.id + '/acls', admin, rows, 'PUT');
  expect((await acl([{ folder: 'lab', perms: 'r' }])).status).toBe(200);
  const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext(); // No ignoreHTTPSErrors.
    const page = await context.newPage();
    const sockets = [];
    const frames = [];
    page.on('websocket', socket => {
      sockets.push(socket.url());
      socket.on('framereceived', f => { try { frames.push(JSON.parse(f.payload.toString())); } catch {} });
    });
    await page.goto(base);
    await page.getByPlaceholder('Username', { exact: true }).fill(viewer.username);
    await page.getByPlaceholder('Encrypted password').fill(viewer.password);
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    await expect(page.getByRole('button', { name: 'Account', exact: true })).toBeVisible();
    await expect.poll(() => sockets.length).toBeGreaterThan(0);
    const before = sockets.length;
    expect((await acl([{ folder: 'browser-qualification', perms: 'r' }])).status).toBe(200);
    await expect.poll(() => sockets.length, { timeout: 15000 }).toBeGreaterThan(before);
    const start = frames.length;
    expect((await request('/api/settings', admin, { env_label: 'Kubernetes qualification' }, 'PUT')).status).toBe(200);
    await expect.poll(() => frames.slice(start).some(f => f.event === 'settings.changed')).toBe(true);
    const visible = await page.evaluate(async () => (await fetch('/api/definitions')).json());
    expect(visible.length).toBeGreaterThan(0);
    expect(visible.every(d => d.team === 'browser-qualification')).toBe(true);
    for (const url of sockets) {
      expect(new URL(url).searchParams.has('ticket')).toBe(true);
      expect(new URL(url).searchParams.has('token')).toBe(false);
    }
    await page.screenshot({ path: path.join(state, 'browser-qualified.png') });
    report.browser = { login: true, tlsVerified: true, websocketConnections: sockets.length,
      automaticReconnect: true, eventReceivedAfterReconnect: true, aclScopedReads: true,
      ticketWithoutBearerInURL: true, chromium: browser.version() };
  } finally { await browser.close(); }
  report.status = 'passed';
}
main().catch(error => {
  report.status = 'failed'; report.error = error.message;
  process.exitCode = 1;
}).finally(() => {
  fs.writeFileSync(output, JSON.stringify(report, null, 2));
  console.log(JSON.stringify(report));
});
