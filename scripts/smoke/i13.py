#!/usr/bin/env python3
"""I13 mandatory installed systemd cell: real TLS, secrets, rotation and host egress."""
import datetime,hashlib,http.server,json,os,pathlib,ssl,subprocess,threading,time,urllib.request,urllib.error
ROOT=pathlib.Path("/var/lib/regente-i13-smoke")
ROOT.mkdir(exist_ok=True,mode=0o700)
BASE="https://127.0.0.1:19443"
CANARIES=["Bearer i13-systemd-original","Bearer i13-systemd-rotated"]
effects=[]
processes=[]
def command(args):
    result=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True)
    if result.returncode: raise RuntimeError(f"{args[0]} failed: {result.stdout[-1500:]}")
    return result.stdout
def cert(name,server=False):
    key=ROOT/(name+".key");crt=ROOT/(name+".crt");csr=ROOT/(name+".csr")
    command(["openssl","req","-new","-newkey","ec","-pkeyopt","ec_paramgen_curve:P-256","-nodes","-keyout",str(key),"-out",str(csr),"-subj","/CN="+name])
    ext=ROOT/(name+".ext")
    ext.write_text("extendedKeyUsage="+("serverAuth\nsubjectAltName=IP:127.0.0.1" if server else "clientAuth"))
    command(["openssl","x509","-req","-in",str(csr),"-CA",str(ROOT/"ca.crt"),"-CAkey",str(ROOT/"ca.key"),"-CAcreateserial","-out",str(crt),"-days","1","-extfile",str(ext)])
    os.chmod(key,0o600)
    return crt,key
def fingerprint(crt):
    return hashlib.sha256(ssl.PEM_cert_to_DER_cert(crt.read_text())).hexdigest()
def atomic_file(path,value):
    temp=path.with_suffix(".new")
    temp.write_text(value);os.chmod(temp,0o600)
    if path.parent==pathlib.Path("/etc/regente-agent-http"):
        command(["chown","regente-agent-http:regente-agent-http",str(temp)])
    os.replace(temp,path)
class Target(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        effects.append(self.headers.get("Authorization",""))
        self.send_response(200);self.end_headers()
        self.wfile.write(self.headers.get("Authorization","").encode())
    def log_message(self,*args):pass
target=http.server.ThreadingHTTPServer(("0.0.0.0",19444),Target)
threading.Thread(target=target.serve_forever,daemon=True).start()
try:
    command(["openssl","req","-x509","-newkey","ec","-pkeyopt","ec_paramgen_curve:P-256","-nodes","-keyout",str(ROOT/"ca.key"),"-out",str(ROOT/"ca.crt"),"-days","1","-subj","/CN=I13-smoke-CA"])
    servercrt,serverkey=cert("server",True);clientcrt,clientkey=cert("client");nextcrt,nextkey=cert("replacement")
    context=ssl.create_default_context(cafile=str(ROOT/"ca.crt"))
    env={k:v for k,v in os.environ.items() if not k.startswith("REGENTE_")}
    env["REGENTE_BOOTSTRAP_PASSWORD"]="i13-systemd-admin-password"
    log=open(ROOT/"server.log","w")
    server=subprocess.Popen(["/usr/local/bin/regente-server","-profile","production","-environment","prod","-network-boundary","tls","-addr","127.0.0.1:19443","-app-url",BASE,"-tls-cert",str(servercrt),"-tls-key",str(serverkey),"-tls-client-ca",str(ROOT/"ca.crt"),"-db",str(ROOT/"server.db"),"-workspace",str(ROOT/"workspace"),"-server-agent=false","-git-source",""],env=env,stdout=log,stderr=subprocess.STDOUT)
    processes.append((server,log))
    token=""
    def request(path,method="GET",body=None,credential=None,ctx=None):
        headers={"Content-Type":"application/json"}
        if credential or token:headers["Authorization"]="Bearer "+(credential or token)
        req=urllib.request.Request(BASE+path,method=method,data=None if body is None else json.dumps(body).encode(),headers=headers)
        with urllib.request.urlopen(req,context=ctx or context,timeout=8) as r:
            raw=r.read();return json.loads(raw) if raw else None
    def wait(label,check,seconds=40):
        end=time.monotonic()+seconds
        while time.monotonic()<end:
            if server.poll() is not None:raise RuntimeError("server stopped: "+(ROOT/"server.log").read_text()[-2000:])
            try:
                if check():return
            except (urllib.error.URLError,ConnectionError):pass
            time.sleep(.2)
        raise RuntimeError("timeout: "+label)
    wait("HTTPS probe without machine certificate",lambda:request("/health")["status"]=="ok")
    token=request("/api/auth/login","POST",{"username":"admin","password":env["REGENTE_BOOTSTRAP_PASSWORD"]})["token"]
    expiry=(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(days=1)).isoformat()
    credential=request("/api/agents/tokens","POST",{"agentId":"i13-cell","environment":"prod","capabilities":["HTTP","REST","EXECUTION_V2"],"expiresAt":expiry,"certificateSHA256":fingerprint(clientcrt)})
    atomic_file(ROOT/"token",credential["token"])
    policy={"environment":"prod","jobs":{"i13-http":{"types":["HTTP"],"secrets":["auth"],"destinations":[{"host":"127.0.0.1","port":"19444","networks":["127.0.0.1/32"]}]}}}
    atomic_file(ROOT/"policy.json",json.dumps(policy))
    atomic_file(ROOT/"secrets.json",json.dumps({"values":{"auth":CANARIES[0]}}))
    install_env={**os.environ,"SERVER":BASE,"ID":"i13-cell","ENVIRONMENT":"prod","TOKEN_FILE":str(ROOT/"token"),"TLS_CERT":str(clientcrt),"TLS_KEY":str(clientkey),"TLS_CA":str(ROOT/"ca.crt"),"POLICY_FILE":str(ROOT/"policy.json"),"SECRETS_FILE":str(ROOT/"secrets.json"),"EGRESS_CIDRS":"127.0.0.1/32"}
    subprocess.run(["bash","/root/agent-deploy/install-secure-linux.sh"],env=install_env,check=True)
    wait("installed cell online",lambda:any(a["id"]=="i13-cell" and a["online"] for a in request("/api/agents")))
    pid=command(["systemctl","show","regente-agent-http","-p","MainPID","--value"]).strip()
    uid=[line for line in pathlib.Path("/proc/"+pid+"/status").read_text().splitlines() if line.startswith("Uid:")][0].split()[1]
    assert uid!="0","installed cell runs as root"
    for prop,value in [("TasksMax","128"),("MemoryMax","536870912"),("CPUQuotaPerSecUSec","1s"),("ProtectSystem","strict"),("NoNewPrivileges","yes")]:
        actual=command(["systemctl","show","regente-agent-http","-p",prop,"--value"]).strip()
        assert actual==value,(prop,actual,value)
    conf=pathlib.Path("/etc/regente-agent-http")
    assert all((conf/name).stat().st_mode&0o777==0o600 for name in ["token","client.key","secrets.json","policy.json"])
    # Prova host/cgroup independente do executor: destino acessível fora, negado dentro.
    with urllib.request.build_opener(urllib.request.ProxyHandler({})).open("http://127.0.0.2:19444",timeout=3) as r:r.read()
    before=len(effects)
    probe=subprocess.run(["systemd-run","--quiet","--wait","--pipe","--unit=regente-i13-egress-probe","-p","IPAddressDeny=any","-p","IPAddressAllow=127.0.0.1/32","/usr/bin/curl","--noproxy","*","--fail","--max-time","2","http://127.0.0.2:19444"],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True)
    assert probe.returncode!=0 and len(effects)==before,"host cgroup egress did not block the reachable target"
    effects.clear()
    request("/api/definitions","POST",{"id":"i13-http","label":"I13 installed HTTP","team":"ops","jobType":"HTTP","environment":"prod","agentId":"i13-cell","actionConfig":{"url":"http://127.0.0.1:19444","headers":{"Authorization":{"secretRef":"auth"}}},"schedule":{"enabled":False}})
    def run(want):
        iid=request("/api/definitions/i13-http/force","POST")["instanceId"]
        wait("installed execution "+want,lambda:any(i["id"]==iid and i["status"]==want for i in request("/api/instances")))
        out=request("/api/instances/"+iid+"/output")
        assert not any(secret in json.dumps(out) for secret in CANARIES),"secret leaked in stored output"
        return out
    assert "[REDACTED]" in json.dumps(run("OK")) and effects==[CANARIES[0]]
    replacement=request(f"/api/agents/tokens/{credential['id']}/rotate","POST",{"expiresAt":expiry,"graceSeconds":0,"certificateSHA256":fingerprint(nextcrt)})
    atomic_file(conf/"client.crt",nextcrt.read_text());atomic_file(conf/"client.key",nextkey.read_text());atomic_file(conf/"token",replacement["token"])
    atomic_file(conf/"secrets.json",json.dumps({"values":{"auth":CANARIES[1]}}))
    assert "[REDACTED]" in json.dumps(run("OK")) and effects==CANARIES
    assert command(["systemctl","show","regente-agent-http","-p","MainPID","--value"]).strip()==pid,"rotation restarted the process"
    (conf/"secrets.json").unlink();run("NOTOK");assert effects==CANARIES,"outage ran an external effect"
    machine_ctx=ssl.create_default_context(cafile=str(ROOT/"ca.crt"));machine_ctx.load_cert_chain(str(nextcrt),str(nextkey))
    request(f"/api/agents/tokens/{replacement['id']}","DELETE")
    try:request("/api/agent/v2/poll?id=i13-cell&env=prod&caps=HTTP,REST,EXECUTION_V2&protocol=2&journal=1&ver=smoke",credential=replacement["token"],ctx=machine_ctx);raise AssertionError("revoked certificate credential accepted")
    except urllib.error.HTTPError as e:assert e.code==401
    journal=command(["journalctl","-u","regente-agent-http","--no-pager"])
    assert not any(secret in journal for secret in CANARIES),"secret leaked in system journal"
    atomic_file(conf/"secrets.json",json.dumps({"values":{"auth":CANARIES[1]}}))
    assert (conf/"secrets.json").exists() and (conf/"client.key").exists()
    # Célula de comandos separada: usuário/pool/volumes, sem secrets HTTP.
    commandcrt,commandkey=cert("command-cell")
    command_credential=request("/api/agents/tokens","POST",{"agentId":"i13-command-cell","environment":"prod","capabilities":["COMMAND","SCRIPT","EXECUTION_V2"],"expiresAt":expiry,"certificateSHA256":fingerprint(commandcrt)})
    atomic_file(ROOT/"command-token",command_credential["token"])
    atomic_file(ROOT/"command-policy.json",json.dumps({"environment":"prod","jobs":{"i13-command":{"types":["COMMAND"],"secrets":[],"destinations":[]}}}))
    command_env={**install_env,"CELL_KIND":"command","ID":"i13-command-cell","TOKEN_FILE":str(ROOT/"command-token"),"TLS_CERT":str(commandcrt),"TLS_KEY":str(commandkey),"POLICY_FILE":str(ROOT/"command-policy.json")}
    subprocess.run(["bash","/root/agent-deploy/install-secure-linux.sh"],env=command_env,check=True)
    dropin=pathlib.Path("/etc/systemd/system/regente-agent-command.service.d")
    dropin.mkdir(exist_ok=True)
    (dropin/"probe.conf").write_text("[Service]\nEnvironment=REGENTE_SECRET_PARENT_CANARY=i13-parent-credential\n")
    command(["systemctl","daemon-reload"]);command(["systemctl","restart","regente-agent-command"])
    wait("installed command cell online",lambda:any(a["id"]=="i13-command-cell" and a["online"] for a in request("/api/agents")))
    # MainPID aparece antes do exec/setuid; presença pode pertencer ao processo anterior.
    expected_command_uid=command(["id","-u","regente-agent-command"]).strip()
    def command_identity_ready():
        current_pid=command(["systemctl","show","regente-agent-command","-p","MainPID","--value"]).strip()
        if current_pid=="0":return False
        try:
            current_uid=[line for line in pathlib.Path("/proc/"+current_pid+"/status").read_text().splitlines() if line.startswith("Uid:")][0].split()[1]
            executable=pathlib.Path("/proc/"+current_pid+"/exe").resolve().name
        except FileNotFoundError:return False
        return current_uid==expected_command_uid and executable=="regente-agent"
    wait("command service exec and expected UID",command_identity_ready)
    command_pid=command(["systemctl","show","regente-agent-command","-p","MainPID","--value"]).strip()
    command_uid=[line for line in pathlib.Path("/proc/"+command_pid+"/status").read_text().splitlines() if line.startswith("Uid:")][0].split()[1]
    assert command_uid!="0" and command_uid!=uid,"command and secrets cells share OS identity"
    script="""set -eu
test -z "${REGENTE_SECRET_PARENT_CANARY:-}"
test ! -r /etc/regente-agent-http/client.key
test ! -r /etc/regente-agent-http/secrets.json
test ! -r /var/lib/regente-i13-smoke/ca.key
if touch /etc/regente-i13-forbidden 2>/dev/null; then exit 21; fi
if curl --noproxy '*' --fail --max-time 2 http://127.0.0.2:19444 >/dev/null 2>&1; then exit 22; fi
echo i13-command-isolated
"""
    request("/api/definitions","POST",{"id":"i13-command","label":"I13 command cell","team":"ops","jobType":"COMMAND","environment":"prod","agentId":"i13-command-cell","actionConfig":{"command":script,"cwd":"/var/lib/regente-agent-command"},"schedule":{"enabled":False}})
    command_iid=request("/api/definitions/i13-command/force","POST")["instanceId"]
    wait("command filesystem/environment/egress",lambda:any(i["id"]==command_iid and i["status"]=="OK" for i in request("/api/instances")))
    command_out=json.dumps(request("/api/instances/"+command_iid+"/output"))
    assert "i13-command-isolated" in command_out and "i13-parent-credential" not in command_out,"command inherited credentials or isolation failed"
    assert effects==CANARIES,"command escaped host egress"
    print("I13 COMMAND CELL OK: separate non-root UID/pool; secrets volume denied; system write denied; parent credentials absent; actual host egress denied",flush=True)
    print("I13 SYSTEMD OK: installed non-root cell; mTLS fingerprint; hot token/certificate/secret rotation; no raw output; outage fail-closed; revoked access; cgroup limits and actual denied egress",flush=True)
finally:
    subprocess.run(["systemctl","disable","--now","regente-agent-http","regente-agent-command"],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    for proc,log in processes:proc.terminate();proc.wait(timeout=10);log.close()
    target.shutdown()
