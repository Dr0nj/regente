#!/usr/bin/env python3
"""Disposable, mandatory Linux/amd64 integration baseline. Standard library only."""
import json
import os
from pathlib import Path
import platform
import shutil
import socket
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[1]
RUN = ROOT / ".integration" / uuid.uuid4().hex[:12]
EVIDENCE = RUN / "evidence"
COMPOSE = ["docker", "compose", "-p", "regente-it-" + RUN.name,
           "-f", str(ROOT / "integration/compose.yaml")]
ADMIN = "synthetic-lab-admin"
PROCESSES = []
REPORT = {"status": "running", "stages": [], "profile": "synthetic-ci-v1"}
TLS_CONTEXT = None


def command(args, *, env=None, timeout=180, name=None, stdin=None, expected_exit=0):
    start = time.monotonic()
    result = subprocess.run(args, cwd=ROOT, env=env, text=True, input=stdin,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
    if name:
        (EVIDENCE / (name + ".log")).write_text(result.stdout, encoding="utf-8")
        REPORT["stages"].append({"name": name, "seconds": round(time.monotonic()-start, 3),
                                 "exit_code": result.returncode, "expected_exit": expected_exit})
        print(f"{name}: exit={result.returncode}", flush=True)
    if result.returncode != expected_exit:
        raise RuntimeError(f"Command failed: {args[0]} ({name or 'setup'}); {result.stdout[-4000:]}")
    return result.stdout.strip()


def port(service, internal):
    return command(COMPOSE + ["port", service, str(internal)]).rsplit(":", 1)[1]


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def request(url, method="GET", data=None, token=ADMIN):
    raw = None if data is None else json.dumps(data).encode()
    req = urllib.request.Request(url, data=raw, method=method,
                                 headers={"Authorization": "Bearer " + token,
                                          "Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=10, context=TLS_CONTEXT) as response:
        return json.load(response)


def eventually(label, check, timeout=75):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        for proc, _ in PROCESSES:
            if proc.poll() is not None:
                raise RuntimeError(f"Process exited early: {proc.args[0]} ({proc.returncode})")
        try:
            result = check()
            if result:
                return result
        except (OSError, ValueError, urllib.error.URLError) as error:
            last = str(error)
        time.sleep(0.25)
    raise RuntimeError(f"Timeout: {label}; last error: {last}")


def start_process(name, args, cwd, env):
    log = (EVIDENCE / (name + ".log")).open("w", encoding="utf-8")
    proc = subprocess.Popen(args, cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT)
    PROCESSES.append((proc, log))
    return proc


def stop_process(proc):
    proc.terminate()
    try:
        proc.wait(timeout=10)
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait(timeout=5)
    for item in list(PROCESSES):
        if item[0] is proc:
            item[1].close()
            PROCESSES.remove(item)


def validate_test_events(output):
    events = [json.loads(line) for line in output.splitlines() if line.startswith("{")]
    skipped = [e.get("Test", e["Package"]) for e in events if e.get("Action") == "skip"]
    if skipped:
        raise RuntimeError(f"Mandatory integration tests skipped: {skipped}")
    passed = {e.get("Test") for e in events if e.get("Action") == "pass"}
    required = {"TestPostgresMigrateAndCRUD", "TestMigrationSafety/postgres",
                "TestMigrationSafety/sqlite", "TestIntegrationOIDC_AuthCodeFlow",
                "TestMachineIdentity/sqlite", "TestMachineIdentity/postgres",
                "TestHumanIdentity/sqlite", "TestHumanIdentity/postgres",
                "TestWebEventAuthorization/sqlite", "TestWebEventAuthorization/postgres",
                "TestWebEventDistributed/sqlite", "TestWebEventDistributed/postgres",
                "TestWebEventPolicy", "TestWebEventOrigin", "TestWebEventQueuedRevocation"}
    if not required <= passed:
        raise RuntimeError(f"Missing required test evidence: {required - passed}")
    REPORT["passed_tests"] = sorted(p for p in passed if p)


def cluster(env, dsn, nats_url):
    start = time.monotonic()
    nodes = []
    credentials = []
    workloads = json.loads((ROOT / "integration/fixtures/workloads.json").read_text())
    for suffix in ("a", "b"):
        workspace = RUN / ("node-" + suffix)
        defs = workspace / "definitions/lab"
        defs.mkdir(parents=True)
        # JSON é um subconjunto YAML; usa o contrato YAML params, sem dependência Python.
        for definition in workloads:
            definition = dict(definition)
            definition["params"] = definition.pop("actionConfig")
            (defs / (definition["id"] + ".yaml")).write_text(json.dumps(definition))
        address = "127.0.0.1:" + str(free_port())
        args = [str(RUN / "server"), "-addr", address, "-db-driver", "postgres", "-db", dsn,
                "-workspace", str(workspace), "-node-id", "lab-node-"+suffix,
                "-bus", "nats", "-nats-url", nats_url, "-api-token", ADMIN,
                "-server-agent=false", "-selfmon=false", "-tick-ms", "200",
                "-design-session-gc-tick-min", "0", "-auth-mode", "hybrid",
                "-oidc-issuer", env["REGENTE_TEST_OIDC_ISSUER"],
                "-oidc-client-id", env["REGENTE_TEST_OIDC_CLIENT_ID"],
                "-oidc-client-secret", env["REGENTE_TEST_OIDC_CLIENT_SECRET"],
                "-oidc-redirect-url", "http://"+address+"/api/auth/oidc/callback"]
        proc = start_process("node-"+suffix, args, workspace, env)
        base = "http://"+address
        eventually("node "+suffix, lambda: request(base+"/health"))
        nodes.append((proc, base, args, workspace))
        credential = request(base+"/api/agents/tokens", "POST", {"label": "lab-agent-"+suffix, "agentId": "lab-agent-"+suffix, "environment": "", "capabilities": ["COMMAND"], "expiresAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time()+86400))})
        credentials.append(credential)
        token = credential["token"]
        agent_dir = RUN / ("agent-"+suffix)
        agent_dir.mkdir()
        start_process("agent-"+suffix, [str(RUN / "agent"), "-id", "lab-agent-"+suffix,
                      "-server", "ws://"+address+"/ws/agent", "-token", token, "-caps", "COMMAND"], agent_dir, env)
    for _, base, _, _ in nodes:
        eventually("both agents visible on "+base,
                   lambda: len([a for a in request(base+"/api/agents") if a["online"]]) == 2)
    result = request(nodes[0][1]+"/api/folders/lab/order", "POST", {})
    if result["ordered"] != len(workloads):
        raise RuntimeError(f"Unexpected order count: {result}")
    instance_ids = result["instanceIds"]
    for instance_id in instance_ids:
        def finished():
            row = request(nodes[1][1]+"/api/instances/"+instance_id)
            if row["status"] in ("NOTOK", "CANCELLED"):
                raise RuntimeError(f"Job failed: {instance_id}: {row['status']}")
            return row if row["status"] == "OK" else False
        row = eventually("job "+instance_id, finished)
        if "lab-" not in row.get("output", ""):
            raise RuntimeError("Completed job lost its synthetic output: "+instance_id)
    if (RUN / "agent-b/non-idempotent-effects.txt").read_text() != "lab-effect\n":
        raise RuntimeError("Synthetic non-idempotent effect executed more than once")
    # Repetir a ordem não executa novamente o efeito não idempotente.
    again = request(nodes[1][1]+"/api/folders/lab/order", "POST", {})
    if again["ordered"] != 0 or again["skipped"] != len(workloads):
        raise RuntimeError("Order Folder did not preserve existing orders")
    REPORT["cluster"] = {"servers": 2, "agents": 2, "completed_jobs": len(instance_ids),
                          "seconds": round(time.monotonic()-start, 3), "cross_node_presence": True}
    # Restart de processo e preservação de ordens; não é teste de fencing/failover.
    proc, base, args, workspace = nodes[0]
    stop_process(proc)
    start_process("node-a-restarted", args, workspace, env)
    eventually("restarted node", lambda: request(base+"/health"))
    for instance_id in instance_ids:
        if request(base+"/api/instances/"+instance_id)["status"] != "OK":
            raise RuntimeError("Completed job changed after restart")
    REPORT["cluster"]["restart_preserved_jobs"] = True
    eventually("agent A reconnected after restart",
               lambda: any(a["id"] == "lab-agent-a" and a.get("local")
                           for a in request(nodes[0][1]+"/api/agents")))
    # Revogar no nó B deve fechar o WS do agente conectado ao processo A.
    revocation_start = time.monotonic()
    request(nodes[1][1]+"/api/agents/tokens/"+str(credentials[0]["id"]), "DELETE")
    eventually("cross-node credential revocation",
               lambda: not any(a["id"] == "lab-agent-a" and a.get("local")
                               for a in request(nodes[0][1]+"/api/agents")), timeout=5)
    elapsed = time.monotonic()-revocation_start
    if elapsed > 5:
        raise RuntimeError("Cross-node revocation exceeded the 5-second budget")
    try:
        request(nodes[1][1]+"/api/agent/output", "POST", {"instanceId": instance_ids[0], "chunk": "denied"}, token=credentials[0]["token"])
        raise RuntimeError("Revoked credential accepted by peer")
    except urllib.error.HTTPError as error:
        if error.code != 401:
            raise
    REPORT["cluster"]["cross_node_revocation_seconds"] = round(elapsed, 3)
    return len(instance_ids)


def legacy_restore(dsn):
    start = time.monotonic()
    pg = COMPOSE+["exec", "-T", "postgres"]
    command(pg+["createdb", "-U", "regente", "regente_legacy"])
    fixtures = ROOT/"server/internal/db/testdata"
    sql = (fixtures/"legacy-v22-postgres.sql").read_text() + (fixtures/"legacy-data.sql").read_text()
    command(pg+["psql", "-U", "regente", "-d", "regente_legacy", "-v", "ON_ERROR_STOP=1"], stdin=sql)
    command(pg+["pg_dump", "-U", "regente", "-d", "regente_legacy", "-Fc", "-f", "/tmp/legacy.dump"])
    command(pg+["createdb", "-U", "regente", "regente_legacy_restored"])
    command(pg+["pg_restore", "-U", "regente", "-d", "regente_legacy_restored", "--exit-on-error", "/tmp/legacy.dump"])
    restored = dsn.replace("/regente_lab?", "/regente_legacy_restored?")
    command([str(RUN/"server"), "-db-driver", "postgres", "-db", restored, "-migrate-only"], name="legacy-restored-upgrade")
    result = command(pg+["psql", "-U", "regente", "-d", "regente_legacy_restored", "-Atc",
                         "SELECT (SELECT count(*) FROM instance_runs WHERE instance_id='legacy-running'), "
                         "(SELECT count(*) FROM design_sessions WHERE id='legacy-draft'), "
                         "(SELECT count(*) FROM agent_tokens WHERE label='fixture only' AND agent_id IS NULL AND token_hash LIKE 'retired:%'), "
                         "(SELECT count(*) FROM daily_runs WHERE finished_at IS NULL)"])
    if result != "1|1|1|1":
        raise RuntimeError("Legacy backup/restore/upgrade did not preserve the fixture")
    REPORT["legacy_restore"] = {"schema_from": 22, "schema_to": 25, "preserved_entities": 4,
                                "seconds": round(time.monotonic()-start, 3)}
    # O binário real deve recusar schema futuro ANTES de criar o workspace/API.
    command(pg+["psql", "-U", "regente", "-d", "regente_legacy_restored", "-v", "ON_ERROR_STOP=1", "-c",
                "INSERT INTO schema_migrations(version) SELECT max(version)+1 FROM schema_migrations"])
    untouched = RUN/"incompatible-workspace"
    output = command([str(RUN/"server"), "-db-driver", "postgres", "-db", restored,
                      "-workspace", str(untouched), "-addr", "127.0.0.1:"+str(free_port())],
                     name="incompatible-startup-refused", timeout=15, expected_exit=1)
    if untouched.exists() or "incompatible migration history" not in output:
        raise RuntimeError("Incompatible schema was not refused before application startup")
    REPORT["incompatible_startup_refused"] = True


def main():
    global TLS_CONTEXT
    EVIDENCE.mkdir(parents=True)
    started = time.monotonic()
    compose_started = False
    try:
        REPORT["sha"] = command(["git", "rev-parse", "HEAD"])
        REPORT["dirty"] = bool(command(["git", "status", "--porcelain"]))
        REPORT["platform"] = platform.platform()
        if platform.system() != "Linux" or platform.machine() not in ("x86_64", "amd64"):
            raise RuntimeError("This integration profile requires Linux/amd64 (use the CI gate or a Linux VM)")
        for dependency in ("go", "docker", "openssl"):
            if not shutil.which(dependency):
                raise RuntimeError("Missing mandatory dependency: "+dependency)
        REPORT["go"] = command(["go", "version"])
        REPORT["compose"] = command(["docker", "compose", "version"])
        command(["docker", "info"], name="docker-engine")
        tls_dir = RUN/"tls"
        tls_dir.mkdir()
        command(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
                 "-subj", "/CN=regente-lab", "-addext", "subjectAltName=DNS:localhost,IP:127.0.0.1",
                 "-keyout", str(tls_dir/"lab.key"), "-out", str(tls_dir/"lab.crt")], name="lab-certificate")
        # Chave efêmera de lab: leitura pelo UID não-root do container, fora dos artifacts.
        (tls_dir/"lab.key").chmod(0o644)
        os.environ["REGENTE_LAB_TLS_DIR"] = str(tls_dir)
        TLS_CONTEXT = ssl.create_default_context(cafile=str(tls_dir/"lab.crt"))
        compose_started = True
        command(COMPOSE+["up", "-d"], timeout=300, name="dependencies")
        pg_port = port("postgres", 5432)
        nats_url = "nats://127.0.0.1:" + port("nats", 4222)
        issuer = "https://127.0.0.1:"+port("keycloak", 8443)+"/realms/regente-lab"
        eventually("Keycloak discovery", lambda: request(issuer+"/.well-known/openid-configuration"), 150)
        nats_info = request("http://127.0.0.1:"+port("nats", 8222)+"/varz")
        REPORT["nats_version"] = nats_info["version"]
        dsn = f"postgres://regente:synthetic-lab-only@127.0.0.1:{pg_port}/regente_lab?sslmode=disable"
        # Configuração pessoal de Git, TLS, banco ou telemetria não entra no lab.
        clean_env = {k: v for k, v in os.environ.items()
                     if not k.startswith(("REGENTE_", "OTEL_")) and k not in ("GITHUB_TOKEN", "GH_TOKEN")}
        env = dict(clean_env, SSL_CERT_FILE=str(tls_dir/"lab.crt"), REGENTE_REQUIRE_INTEGRATION="1", REGENTE_TEST_PG_DSN=dsn, REGENTE_TEST_NATS_URL=nats_url,
                   REGENTE_TEST_OIDC_ISSUER=issuer, REGENTE_TEST_OIDC_CLIENT_ID="regente-lab",
                   REGENTE_TEST_OIDC_CLIENT_SECRET="synthetic-client-secret",
                   REGENTE_TEST_OIDC_USER="lab-user", REGENTE_TEST_OIDC_PASS="synthetic-password")
        output = command(["go", "test", "-json", "-count=1", "-timeout=5m",
                          "./server/internal/db", "./server/internal/api", "-run",
                          "TestMigration|TestLegacy|TestPostgres|TestOnlineBackup|TestIntegrationOIDC|TestMachineIdentity|TestAgentAuthRejectsHumanCredentials|TestHumanIdentity|TestWebEvent"],
                         env=env, timeout=360, name="database-oidc-tests")
        validate_test_events(output)
        command(["go", "test", "-race", "-count=1", "-timeout=3m", "./server/internal/api", "-run", "^TestWebEvent"],
                env=env, timeout=300, name="web-events-race")
        command(["go", "build", "-o", str(RUN/"server"), "./server"], timeout=180, name="server-build")
        command(["go", "build", "-o", str(RUN/"agent"), "./agent"], timeout=180, name="agent-build")
        legacy_restore(dsn)
        count = cluster(env, dsn, nats_url)
        # Restore em OUTRA base descartável, mantendo a original intacta.
        start = time.monotonic()
        pg = COMPOSE+["exec", "-T", "postgres"]
        command(pg+["pg_dump", "-U", "regente", "-d", "regente_lab", "-Fc", "-f", "/tmp/lab.dump"])
        command(pg+["createdb", "-U", "regente", "regente_restored"])
        command(pg+["pg_restore", "-U", "regente", "-d", "regente_restored", "--exit-on-error", "/tmp/lab.dump"])
        restored_dsn = dsn.replace("/regente_lab?", "/regente_restored?")
        command([str(RUN/"server"), "-db-driver", "postgres", "-db", restored_dsn, "-migrate-only"], name="restored-schema")
        got = command(pg+["psql", "-U", "regente", "-d", "regente_restored", "-Atc",
                          "SELECT count(*) FROM instances WHERE status='OK'"])
        if int(got) != count:
            raise RuntimeError("Restore did not preserve completed orders")
        REPORT["restore"] = {"backend": "postgres", "preserved_orders": count,
                             "seconds": round(time.monotonic()-start, 3)}
        REPORT["images"] = json.loads(command(COMPOSE+["images", "--format", "json"]))
        REPORT["status"] = "passed"
    except Exception as error:
        REPORT["status"] = "failed"
        REPORT["error"] = str(error)
        print(str(error), file=sys.stderr, flush=True)
    finally:
        for proc, _ in list(PROCESSES):
            try:
                stop_process(proc)
            except Exception as error:
                REPORT["status"] = "failed"
                REPORT["process_cleanup_error"] = str(error)
        if compose_started:
            for args, name in [(["logs", "--no-color"], "dependency-logs"),
                               (["down", "-v", "--remove-orphans"], "cleanup")]:
                try:
                    output = command(COMPOSE+args, name=name)
                    if REPORT["status"] == "failed" and name == "dependency-logs":
                        print(output[-12000:], file=sys.stderr, flush=True)
                except Exception as error:
                    REPORT["status"] = "failed"
                    REPORT[name+"_error"] = str(error)
        REPORT["seconds"] = round(time.monotonic()-started, 3)
        (EVIDENCE/"baseline.json").write_text(json.dumps(REPORT, indent=2)+"\n", encoding="utf-8")
        print("Evidence: "+str(EVIDENCE), flush=True)
    return 0 if REPORT["status"] == "passed" else 1


if __name__ == "__main__":
    sys.exit(main())
