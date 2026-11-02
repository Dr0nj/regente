// Same local scenario before/after: cold login, console, Settings, portal.
import { chromium } from '../app/node_modules/playwright/index.mjs';
import { spawn } from 'node:child_process';
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
const [output, dist = 'app/dist'] = process.argv.slice(2);
if (!output) throw new Error('Usage: node scripts/frontend-performance.mjs OUTPUT [DIST]');
const dir = await mkdtemp(join(tmpdir(), 'regente-perf-'));
const base = 'http://127.0.0.1:18920';
const env = Object.fromEntries(Object.entries(process.env).filter(([k]) => !k.startsWith('REGENTE_') && !k.startsWith('GITHUB_') && !k.startsWith('OTEL_')));
env.REGENTE_BOOTSTRAP_PASSWORD = 'synthetic-performance-password';
const child = spawn(resolve('.integration/server-i03' + (process.platform === 'win32' ? '.exe' : '')), ['-addr','127.0.0.1:18920','-db',join(dir,'state.db'),'-workspace',join(dir,'workspace'),'-spa-dir',resolve(dist),'-selfmon=false'], {env, windowsHide:true, stdio:'ignore'});
let browser;
try {
 for(let i=0;i<100;i++){try{if((await fetch(base+'/health')).ok)break;}catch{} await new Promise(r=>setTimeout(r,100));}
 browser = await chromium.launch();
 const samples=[];
 for(let run=0;run<3;run++) {
  const context=await browser.newContext(); const page=await context.newPage();
  const stages=[];
  const capture=async(name,action,ready)=>{
   const start=Date.now(); await action(); await ready();
   const readyMs=Date.now()-start;
   await page.waitForTimeout(300);
   const resources=await page.evaluate(()=>performance.getEntriesByType('resource').filter(r=>new URL(r.name).pathname.endsWith('.js')).map(r=>({file:new URL(r.name).pathname,bytes:r.decodedBodySize,transfer:r.transferSize})));
   stages.push({name,readyMs,jsBytes:resources.reduce((n,r)=>n+r.bytes,0),resources});
  };
  await capture('login',()=>page.goto(base),()=>page.getByPlaceholder('Username',{exact:true}).waitFor());
  await page.getByPlaceholder('Username',{exact:true}).fill('admin'); await page.getByPlaceholder('Encrypted password').fill(env.REGENTE_BOOTSTRAP_PASSWORD);
  await capture('console',()=>page.getByRole('button',{name:'Sign in',exact:true}).click(),()=>page.getByRole('button',{name:'Account',exact:true}).waitFor());
  await page.getByRole('button',{name:'Account',exact:true}).click();
  await capture('settings',()=>page.getByRole('button',{name:'Settings',exact:true}).click(),()=>page.getByRole('button',{name:'Agents',exact:true}).waitFor());
  await capture('portal',()=>page.goto(base+'/portal'),()=>page.getByRole('heading',{name:'Portal self-service',exact:true}).first().waitFor());
  samples.push(stages); await context.close();
 }
 await writeFile(output,JSON.stringify({scenario:'local cold browser, synthetic empty SQLite, three runs; uncompressed JS bytes; ready timings are descriptive',samples},null,2)+'\n');
} finally { if(browser)await browser.close(); child.kill(); await new Promise(r=>child.exitCode!==null?r():child.once('exit',r)); await rm(dir,{recursive:true,force:true}); }
