import { test, expect } from "@playwright/test";
import { spawn } from "node:child_process";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";

test.setTimeout(60000);
test("I10: uncertain execution survives restart and requires an audited decision", async ({ page, request }, testInfo) => {
 const dir = await mkdtemp(join(tmpdir(), "regente-execution-e2e-"));
 const base = "http://127.0.0.1:18910";
 const bin = resolve("../.integration/server-i03" + (process.platform === "win32" ? ".exe" : ""));
 const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith("REGENTE_") && !key.startsWith("GITHUB_") && !key.startsWith("OTEL_")));
 const args = ["-addr", "127.0.0.1:18910", "-db", join(dir,"state.db"), "-workspace", join(dir,"workspace"), "-spa-dir", resolve("dist"), "-execution-mode", "durable", "-auth-mode", "local", "-api-token", "synthetic-exec-admin", "-git-source", "", "-selfmon=false", "-server-agent=false", "-scheduler=external"];
 let output = "";
 const start = () => { const p=spawn(bin,args,{windowsHide:true,stdio:["ignore","pipe","pipe"],env}); p.stdout.on("data",data=>{output+=data.toString()});p.stderr.on("data",data=>{output+=data.toString()});return p; };
 let child=start();
 const stop = async () => {child.kill();await new Promise<void>(done=>{if(child.exitCode!==null)done();else child.once("exit",()=>done())});};
 const headers={Authorization:"Bearer synthetic-exec-admin"};
 const ready=async()=>{await expect.poll(async()=>{try{return(await request.get(base+"/health")).status()}catch{return 0}},{timeout:20000}).toBe(200)};
 try {
  await ready();
  const created=await request.post(base+"/api/users",{headers,data:{username:"execution-operator",password:"synthetic-password",role:"admin"}});expect(created.status()).toBe(200);const user=await created.json();
  expect((await request.patch(base+"/api/users/"+user.id+"/password",{headers,data:{next:"synthetic-password"}})).status()).toBe(204);
  const issued=await request.post(base+"/api/agents/tokens",{headers,data:{agentId:"browser-worker",environment:"",capabilities:["COMMAND","EXECUTION_V2"],expiresAt:new Date(Date.now()+3600000).toISOString()}});expect(issued.status()).toBe(200);const machine=await issued.json();const machineHeaders={Authorization:"Bearer "+machine.token};
  expect((await request.post(base+"/api/definitions",{headers,data:{id:"execution-ui",team:"Execution",label:"Execution UI",jobType:"COMMAND",actionConfig:{command:"echo synthetic"},schedule:{enabled:false}}})).status()).toBe(200);
  expect((await request.post(base+"/api/definitions/execution-ui/force",{headers})).status()).toBe(200);
  expect((await request.post(base+"/api/scheduler/tick",{headers})).status()).toBe(200);
  const instances=await(await request.get(base+"/api/instances",{headers})).json();const id=instances.find((i:{definitionId:string})=>i.definitionId==="execution-ui").id;
  const poll=await request.get(base+"/api/agent/v2/poll?id=browser-worker&caps=COMMAND,EXECUTION_V2&protocol=2&journal=1&ver=synthetic&available=1",{headers:machineHeaders});expect(poll.status()).toBe(200);const envelope=await poll.json();
  const identity={protocol:2,executionId:envelope.executionId,fence:envelope.fence};
  for(const kind of ["accepted","started","uncertain"]){expect((await request.post(base+"/api/agent/v2/ack",{headers:machineHeaders,data:{...identity,kind,reason:kind==="uncertain"?"Remote completion receipt lost":""}})).status()).toBe(200)}
  await stop();child=start();await ready();
  await page.goto(base);await page.getByPlaceholder("Username",{exact:true}).fill("execution-operator");await page.getByPlaceholder("Encrypted password").fill("synthetic-password");await page.getByRole("button",{name:"Sign in",exact:true}).click();await expect(page.getByRole("button",{name:"Account",exact:true})).toBeVisible();
  await page.getByText("Execution UI",{exact:true}).first().click();await page.getByRole("button",{name:"Execution",exact:true}).click();
  await expect(page.getByText("Remote completion receipt lost",{exact:false})).toBeVisible();await expect(page.getByText(envelope.executionId,{exact:false})).toBeVisible();
  const form=page.getByRole("form",{name:"Resolve execution"});const button=form.getByRole("button",{name:"Record verified resolution"});await expect(button).toBeDisabled();await form.getByLabel("Evidence and reason").fill("External operation verified stopped; no successful completion");await expect(button).toBeDisabled();await form.getByRole("checkbox").check();await button.click();await expect(form).toHaveCount(0);
  const details=await(await request.get(base+"/api/instances/"+id+"/executions",{headers})).json();expect(details.attempts).toHaveLength(1);expect(details.attempts[0].state).toBe("failed");expect(details.attempts[0].resolvedBy).toBe("execution-operator");expect(details.attempts[0].reason).toContain("External operation verified stopped");
 } finally {await testInfo.attach("server.log",{body:output,contentType:"text/plain"});await stop();}
});
