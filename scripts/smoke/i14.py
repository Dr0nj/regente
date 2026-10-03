#!/usr/bin/env python3
"""I14 installed collector: separate UIDs, HTTPS, durable ACK and crash recovery."""
import json,os,pathlib,ssl,subprocess,time,urllib.request
ROOT=pathlib.Path("/var/lib/regente-i14-smoke")
ROOT.mkdir(exist_ok=True,mode=0o755)
CONTROL=ROOT/"control";CONTROL.mkdir(exist_ok=True,mode=0o700)
TOKEN="synthetic-i14-control-admin-token"
COLLECTOR_TOKEN="synthetic-i14-independent-token-32"
BASE="http://127.0.0.1:19544"
def command(args,env=None):
    result=subprocess.run(args,env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True)
    if result.returncode:raise RuntimeError(result.stdout[-1800:])
    return result.stdout
try:command(["id","i14-control"])
except RuntimeError:command(["useradd","--system","--shell","/usr/sbin/nologin","i14-control"])
command(["chown","i14-control:i14-control",str(CONTROL)])
cert=ROOT/"collector.crt";key=ROOT/"collector.key"
command(["openssl","req","-x509","-newkey","ec","-pkeyopt","ec_paramgen_curve:P-256","-nodes","-days","1","-subj","/CN=i14-collector","-addext","subjectAltName=IP:127.0.0.1","-keyout",str(key),"-out",str(cert)])
os.chmod(key,0o600)
token_file=ROOT/"token";token_file.write_text(COLLECTOR_TOKEN);os.chmod(token_file,0o600)
control_token=CONTROL/"token";control_token.write_text(COLLECTOR_TOKEN);os.chmod(control_token,0o600);command(["chown","i14-control:i14-control",str(control_token)])
common=["runuser","-u","i14-control","--","/usr/local/bin/regente-server","-db",str(CONTROL/"state.db")]
command(common+["-audit-verify"])
command(["bash","/var/lib/regente/deploy/audit/install-audit-collector.sh",str(CONTROL/"state.db.audit.key.pub"),str(token_file),str(cert),str(key),"127.0.0.1:19545"])
args=common+["-addr","127.0.0.1:19544","-workspace",str(CONTROL/"workspace"),"-role","api","-api-token",TOKEN,"-selfmon=false","-server-agent=false","-audit-siem-url","https://127.0.0.1:19545","-audit-token-file",str(control_token)]
env={k:v for k,v in os.environ.items() if not k.startswith("REGENTE_")};env["SSL_CERT_FILE"]=str(cert)
process=None;log=open(ROOT/"control.log","w")
def start():return subprocess.Popen(args,env=env,stdout=log,stderr=subprocess.STDOUT)
def request(path,method="GET",body=None):
    req=urllib.request.Request(BASE+path,method=method,data=None if body is None else json.dumps(body).encode(),headers={"Authorization":"Bearer "+TOKEN,"Content-Type":"application/json"})
    with urllib.request.urlopen(req,timeout=5) as response:return json.load(response)
def eventually(fn):
    deadline=time.monotonic()+90
    while time.monotonic()<deadline:
        try:
            result=fn()
            if result:return result
        except Exception:pass
        time.sleep(.2)
    raise RuntimeError("I14 installed proof timed out: "+(ROOT/"control.log").read_text()[-1800:])
try:
    process=start();eventually(lambda:request("/health"))
    request("/api/settings","PUT",{"env_label":"i14-accepted"})
    eventually(lambda:request("/api/audit/security/status")["pending"]==0)
    journal=pathlib.Path("/var/lib/regente-audit/journal.jsonl")
    if subprocess.run(["runuser","-u","i14-control","--","test","-r",str(journal)]).returncode==0:raise RuntimeError("control plane can read independent journal")
    if subprocess.run(["runuser","-u","regente-audit","--","test","-r",str(CONTROL/"state.db.audit.key")]).returncode==0:raise RuntimeError("collector can read signing key")
    command(["systemctl","stop","regente-audit-collector"])
    request("/api/settings","PUT",{"env_label":"i14-offline-accepted"})
    # runuser possui processo filho; SIGKILL direto no PID de regente-server.
    command(["pkill","-KILL","-u","i14-control","-x","regente-server"])
    process.wait(timeout=10)
    command(["systemctl","start","regente-audit-collector"])
    process=start();eventually(lambda:request("/health"));eventually(lambda:request("/api/audit/security/status")["pending"]==0)
    if request("/api/settings")["env_label"]!="i14-offline-accepted":raise RuntimeError("accepted mutation lost")
    records=[json.loads(line) for line in journal.read_text().splitlines()]
    if [r["seq"] for r in records]!=list(range(1,len(records)+1)):raise RuntimeError("collector gap or duplicate")
    print("I14_SYSTEMD_OK: separate UIDs and permissions, TLS ACK, destination outage, SIGKILL and resumed gap-free ledger",flush=True)
finally:
    if process is not None and process.poll() is None:
        subprocess.run(["pkill","-TERM","-u","i14-control","-x","regente-server"]);process.wait(timeout=15)
    log.close()
