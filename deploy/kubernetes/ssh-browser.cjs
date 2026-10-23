// Disposable Chromium with the lab CA trusted in its NSS database; no TLS bypass.
const fs = require('node:fs');
const https = require('node:https');
const { chromium, expect } = require('@playwright/test');
const state = process.env.LAB_STATE, base = process.env.LAB_BROWSER_URL;
const ca = fs.readFileSync(state + '/ca.crt');
const password = JSON.parse(fs.readFileSync(state + '/private.json')).password;
const output = state + '/ssh-browser.json';
if (fs.existsSync(output)) throw new Error('Preserve existing browser evidence');
function request(route, token, body, method) {
  return new Promise((resolve, reject) => {
    const req = https.request(base + route, {ca, method: method || (body ? 'POST' : 'GET'),
      headers: {'Content-Type':'application/json', ...(token ? {Authorization:'Bearer '+token} : {})}}, res => {
      let raw='';res.on('data',b=>raw+=b);res.on('end',()=>{let data=null;try{data=JSON.parse(raw);}catch{} resolve({status:res.statusCode,data});});
    });req.on('error',reject);if(body)req.write(JSON.stringify(body));req.end();
  });
}
(async () => {
  const login = await request('/api/auth/login',null,{username:'admin',password});
  expect(login.status).toBe(200);let token=login.data.token;
  const users=await request('/api/users',token);
  const admin=users.data.find(u=>u.username==='admin');
  expect((await request('/api/users/'+admin.id+'/password',token,{next:password},'PATCH')).status).toBe(204);
  const fresh=await request('/api/auth/login',null,{username:'admin',password});
  expect(fresh.status).toBe(200);token=fresh.data.token;
  const created=await request('/api/design/sessions',token,{folders:['lab']});
  expect(created.status).toBe(201);const sid=created.data.id;
  const browser=await chromium.launch({headless:true});
  const report={status:'running'};
  try {
    const context=await browser.newContext();const page=await context.newPage();
    await page.addInitScript(id=>localStorage.setItem('regente:designSessionId',id),sid);
    await page.goto(base);
    await page.getByPlaceholder('Username',{exact:true}).fill('admin');
    await page.getByPlaceholder('Encrypted password').fill(password);
    await page.getByRole('button',{name:'Sign in',exact:true}).click();
    await expect(page.getByRole('button',{name:'Account',exact:true})).toBeVisible();
    await page.getByRole('button',{name:'design',exact:true}).click();
    await page.getByTitle('Folders',{exact:true}).click();
    await page.getByRole('button',{name:'ssh-ok',exact:true}).click();
    await page.locator('.react-flow__node[data-id="d-ssh-ok"]').click();
    const type=page.getByText('Job Type',{exact:true}).locator('..').locator('select');
    await expect(type).toHaveValue('SSH_REMOTE');
    await expect(type.locator('option[value="SSH_REMOTE"]')).toHaveCount(1);
    const agent=page.getByText('Agent (where it runs)',{exact:true}).locator('..').locator('select');
    await expect(agent).toHaveValue('regente-kubernetes-phase03-ssh');
    await page.getByRole('button',{name:'Action',exact:true}).click();
    await expect(page.getByPlaceholder('/private/known_hosts')).toHaveValue('/private/known_hosts');
    await expect(page.getByPlaceholder('yes',{exact:true})).toHaveValue('yes');
    await page.getByRole('button',{name:'Save',exact:true}).click();
    await expect.poll(async()=>{
      const defs=await request('/api/design/sessions/'+sid+'/definitions',token);
      return defs.data.find(d=>d.id==='ssh-ok')?.agentId;
    }).toBe('regente-kubernetes-phase03-ssh');
    await page.screenshot({path:state+'/ssh-browser.png',fullPage:true});
    report.status='passed';report.checks=['TLS login','SSH_REMOTE type selectable','bridge agent retained','strict host key fields','draft saved with bridge targeting'];
  } catch(e) {report.status='failed';report.error=e.message;throw e;}
  finally {fs.writeFileSync(output,JSON.stringify(report,null,2));await browser.close();}
})().catch(e=>{console.error(e.message);process.exitCode=1;});
