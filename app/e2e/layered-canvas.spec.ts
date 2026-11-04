import { test, expect } from "@playwright/test";
import { spawn, execFileSync } from "node:child_process";
import { mkdtemp, mkdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";

test.setTimeout(60000);
test("layered canvas renders padding, spacing and detached edges in Monitoring and Design", async ({ page, request }, testInfo) => {
 const dir = await mkdtemp(join(tmpdir(), "regente-execution-e2e-"));
 const remote=join(dir,"remote");
 await mkdir(join(remote,"definitions","Graph"),{recursive:true});
 await writeFile(join(remote,"definitions","Graph",".keep"),"synthetic fixture");
 for(const args of [["init","-b","main",remote],["-C",remote,"add","."],["-C",remote,"-c","user.name=Lab","-c","user.email=lab@example.invalid","-c","commit.gpgsign=false","commit","-m","synthetic fixture"]]) execFileSync("git",args,{stdio:"pipe",windowsHide:true});
 const base = "http://127.0.0.1:18918";
 const bin = resolve("../.integration/server-i03" + (process.platform === "win32" ? ".exe" : ""));
 const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith("REGENTE_") && !key.startsWith("GITHUB_") && !key.startsWith("OTEL_")));
  env.REGENTE_INSECURE_DEV_AUTH = "1";
 const args = ["-addr", "127.0.0.1:18918", "-db", join(dir,"state.db"), "-workspace", join(dir,"workspace"), "-spa-dir", resolve("dist"), "-execution-mode", "durable", "-auth-mode", "local", "-api-token", "synthetic-exec-admin", "-git-source", remote, "-git-branch", "main", "-git-poll-interval", "0", "-github-repo", "", "-selfmon=false", "-server-agent=false", "-scheduler=external"];
 let output = "";
 const start = () => { const p=spawn(bin,args,{windowsHide:true,stdio:["ignore","pipe","pipe"],env,cwd:dir}); p.stdout.on("data",data=>{output+=data.toString()});p.stderr.on("data",data=>{output+=data.toString()});return p; };
 const child=start();
 const stop = async () => {child.kill();await new Promise<void>(done=>{if(child.exitCode!==null || child.signalCode!==null)done();else child.once("exit",()=>done())});};
 const headers={Authorization:"Bearer synthetic-exec-admin"};
 const ready=async()=>{await expect.poll(async()=>{try{return(await request.get(base+"/health")).status()}catch{return 0}},{timeout:20000}).toBe(200)};
 try {
  await ready();
  const created=await request.post(base+"/api/users",{headers,data:{username:"system",password:"synthetic-password",role:"admin"}});expect(created.status()).toBe(200);const user=await created.json();
  expect((await request.patch(base+"/api/users/"+user.id+"/password",{headers,data:{next:"synthetic-password"}})).status()).toBe(204);
  const defs = [
    {id:"root",conditionsOutAdd:["ready"]},
    {id:"left",conditionsIn:["ready"],conditionsOutAdd:["left-ready"],upstream:[{from:"root",condition:"OK"}]},
    {id:"right",conditionsIn:["ready"],conditionsOutAdd:["right-ready"],upstream:[{from:"root",condition:"OK"}]},
    {id:"join",conditionsIn:["left-ready","right-ready"],upstream:[{from:"left",condition:"OK"},{from:"right",condition:"OK"}]},
  ];
  for(const def of defs) {
    expect((await request.post(base+"/api/definitions",{headers,data:{...def,team:"Graph",label:def.id,jobType:"COMMAND",actionConfig:{command:"echo synthetic"},schedule:{enabled:false}}})).status()).toBe(200);
    expect((await request.post(base+"/api/definitions/"+def.id+"/force",{headers})).status()).toBe(200);
  }
  const createdSession=await request.post(base+"/api/design/sessions",{headers,data:{folders:["Graph"]}});
  expect(createdSession.status()).toBe(201);
  const session=await createdSession.json();
  for(const def of defs){
    const endpoint=base+"/api/design/sessions/"+session.id;
    const current=await request.get(endpoint,{headers});
    expect((await request.post(endpoint+"/definitions",{headers:{...headers,"If-Match":current.headers().etag},data:{...def,team:"Graph",label:def.id,jobType:"COMMAND",actionConfig:{command:"echo synthetic"},schedule:{enabled:false}}})).status()).toBe(200);
  }
  await page.addInitScript(sid=>localStorage.setItem("regente:designSessionId",sid),session.id);
  await page.setViewportSize({width:1600,height:1000});
  await page.goto(base);await page.getByPlaceholder("Username",{exact:true}).fill("system");await page.getByPlaceholder("Encrypted password").fill("synthetic-password");await page.getByRole("button",{name:"Sign in",exact:true}).click();await expect(page.getByRole("button",{name:"Account",exact:true})).toBeVisible();
  for (const mode of ["Monitoring", "Design"]) {
    if(mode==="Design") {
      await page.getByRole("button",{name:"design",exact:true}).click();
    }
    await page.getByRole("button",{name:"Organize",exact:true}).click();
    const cards=page.locator(".react-flow__node-jobV2");
    await expect(cards).toHaveCount(4);
    await expect(page.locator(".react-flow__edge-path")).toHaveCount(4);
    const geometry=await cards.evaluateAll(nodes=>nodes.map(node=>{
      const card=node.firstElementChild as HTMLElement;
      const content=card.querySelector(':scope > div > div:nth-child(2)') as HTMLElement;
      return {id:node.getAttribute("data-id"),width:card.offsetWidth,height:card.offsetHeight,padding:content?getComputedStyle(content).padding:null};
    }));
    for(const card of geometry){expect(card.width).toBe(220);expect(card.height).toBe(mode==="Monitoring"?99:80);expect(card.padding).toBe("20px");}
    const gaps=await page.locator(".react-flow__edge").evaluateAll(edges=>edges.map(edge=>{
      const id=edge.getAttribute("data-id")!;
      const nodes=[...document.querySelectorAll<HTMLElement>(".react-flow__node-jobV2")];
      const source=nodes.find(n=>id.startsWith("e-"+n.dataset.id+"-"))!;
      const target=nodes.find(n=>n.dataset.id===id.slice(3+source.dataset.id!.length))!;
      const sy=new DOMMatrix(source.style.transform).m42;
      const ty=new DOMMatrix(target.style.transform).m42;
      const path=edge.querySelector<SVGPathElement>(".react-flow__edge-path")!;
      return {from:path.getPointAtLength(0).y-sy-(source.firstElementChild as HTMLElement).offsetHeight,to:ty-path.getPointAtLength(path.getTotalLength()).y,gap:ty-sy-(source.firstElementChild as HTMLElement).offsetHeight};
    }));
    for(const gap of gaps){expect(gap.from).toBeCloseTo(16,0);expect(gap.to).toBeCloseTo(16,0);expect(gap.gap).toBeGreaterThanOrEqual(80);}

    const edge=await page.locator(".react-flow__edge-path").first().getAttribute("d");
    expect(edge).not.toContain("NaN");
    await page.screenshot({path:testInfo.outputPath(mode.toLowerCase()+"-layered.png")});
  }
 } finally {await testInfo.attach("server.log",{body:output,contentType:"text/plain"});await stop();}
});
