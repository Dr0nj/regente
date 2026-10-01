import { test, expect } from "@playwright/test";
import { spawn, execFileSync } from "node:child_process";
import { mkdtemp, mkdir, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";

test.setTimeout(90000);
test("I12: shared draft resumes after restart and rejects stale browser edits", async ({ page, request }, testInfo) => {
 const dir = await mkdtemp(join(tmpdir(), "regente-draft-e2e-"));
 const remote = join(dir,"remote");await mkdir(join(remote,"definitions","lab"),{recursive:true});
 await writeFile(join(remote,"definitions","lab",".keep"),"synthetic fixture\n");
 for(const args of [["init","-b","main",remote],["-C",remote,"add","."],["-C",remote,"-c","user.name=Lab","-c","user.email=lab@example.invalid","-c","commit.gpgsign=false","commit","-m","synthetic fixture"]]) execFileSync("git",args,{stdio:"pipe",windowsHide:true});
 const bin=resolve("../.integration/server-i03"+(process.platform==="win32"?".exe":""));
 const env=Object.fromEntries(Object.entries(process.env).filter(([key])=>!key.startsWith("REGENTE_")&&!key.startsWith("GITHUB_")&&!key.startsWith("OTEL_")));
 const bases=["http://127.0.0.1:18912","http://127.0.0.1:18913"];
 const dirs=[join(dir,"a"),join(dir,"b")];for(const cwd of dirs)await mkdir(cwd);
 let output="";
 const launch=(i:number)=>{const child=spawn(bin,["-addr","127.0.0.1:"+(18912+i),"-db",join(dir,"state.db"),"-workspace",join(dirs[i],"workspace"),"-spa-dir",resolve("dist"),"-git-source",remote,"-git-branch","main","-github-repo","","-git-poll-interval","0","-auth-mode","local","-api-token","synthetic-draft-admin","-selfmon=false","-server-agent=false","-scheduler=external","-design-session-gc-tick-min","0"],{cwd:dirs[i],env,windowsHide:true,stdio:["ignore","pipe","pipe"]});child.stdout.on("data",d=>{output+=d.toString()});child.stderr.on("data",d=>{output+=d.toString()});return child;};
 const children=[launch(0),launch(1)];
 const stop=async(i:number)=>{const child=children[i];if(child.exitCode!==null||child.signalCode!==null)return;child.kill();await new Promise<void>(done=>child.once("exit",()=>done()));};
 const ready=async(i:number)=>{await expect.poll(async()=>{try{return(await request.get(bases[i]+"/health")).status()}catch{return 0}},{timeout:25000}).toBe(200);};
 const headers={Authorization:"Bearer synthetic-draft-admin"};
 try {
  await ready(0);await ready(1);
  const created=await request.post(bases[0]+"/api/users",{headers,data:{username:"system",password:"synthetic-password",role:"admin"}});expect(created.status()).toBe(200);const user=await created.json();expect((await request.patch(bases[0]+"/api/users/"+user.id+"/password",{headers,data:{next:"synthetic-password"}})).status()).toBe(204);
  const sessionResponse=await request.post(bases[0]+"/api/design/sessions",{headers,data:{folders:["lab"]}});expect(sessionResponse.status()).toBe(201);const session=await sessionResponse.json();const endpoint="/api/design/sessions/"+session.id;
  const def={id:"draft-browser",label:"Draft initial",team:"lab",jobType:"COMMAND",schedule:{enabled:false},actionConfig:{command:"echo synthetic-only"}};
  expect((await request.post(bases[0]+endpoint+"/definitions",{headers:{...headers,"If-Match":sessionResponse.headers().etag},data:def})).status()).toBe(200);
  await page.addInitScript(sid=>{localStorage.setItem("regente:designSessionId",sid)},session.id);
  await page.goto(bases[0]);await page.getByPlaceholder("Username",{exact:true}).fill("system");await page.getByPlaceholder("Encrypted password").fill("synthetic-password");await page.getByRole("button",{name:"Sign in",exact:true}).click();await expect(page.getByRole("button",{name:"Account",exact:true})).toBeVisible();
  const openCode=async()=>{await page.getByRole("button",{name:"design",exact:true}).click();await page.getByRole("button",{name:"code",exact:true}).click();await expect(page.getByTestId("code-mode")).toBeVisible();};
  await openCode();const editor=page.getByTestId("code-mode").locator("textarea");await expect(editor).toHaveValue(/Draft initial/);
  const current=await request.get(bases[1]+endpoint,{headers});expect(current.status()).toBe(200);
  expect((await request.post(bases[1]+endpoint+"/definitions",{headers:{...headers,"If-Match":current.headers().etag},data:{...def,label:"Remote winner"}})).status()).toBe(200);
  const local=(await editor.inputValue()).replace("Draft initial","Local unapplied edit");await editor.fill(local);
  await expect(page.getByTestId("draft-revision-conflict")).toBeVisible();await expect(editor).toHaveValue(local);
  const durable=await(await request.get(bases[1]+endpoint+"/definitions",{headers})).json();expect(durable[0].label).toBe("Remote winner");
  await page.getByRole("button",{name:"Reload shared draft",exact:true}).click();await expect(page.getByRole("button",{name:"Account",exact:true})).toBeVisible();await openCode();await expect(editor).toHaveValue(/Remote winner/);
  await stop(0);await rm(join(dirs[0],"sessions"),{recursive:true,force:true});children[0]=launch(0);await ready(0);await page.reload();await expect(page.getByRole("button",{name:"Account",exact:true})).toBeVisible();await openCode();await expect(editor).toHaveValue(/Remote winner/);
 } finally {await testInfo.attach("server.log",{body:output,contentType:"text/plain"});await stop(0);await stop(1);}
});
