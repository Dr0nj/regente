"""Install Regente on an EXISTING Kubernetes cluster; never installs Kubernetes.

One protected state directory owns one namespace and identity. Run again to resume.
No secrets in command arguments, reports, rendered manifests or error messages.
"""
import argparse
import base64
import contextlib
import datetime as dt
import getpass
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import socket
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.request

from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import NameOID, ExtendedKeyUsageOID
import install_resources as model


def run(args, data=None, timeout=180):
    p = subprocess.run(args, input=data, capture_output=True, timeout=timeout)
    if p.returncode:
        # kubectl may echo Secret payloads in diagnostics. Do not relay stderr.
        raise RuntimeError(f"{args[0]} failed (exit {p.returncode}); inspect resources/events in the selected namespace")
    return p.stdout


def validate(c, local=False):
    for key in ("context", "namespace", "environment", "domain", "storageClass", "serverImage", "agentImage", "gitSource", "agentId"):
        if not isinstance(c.get(key), str) or not c[key].strip():
            raise ValueError(f"Missing configuration: {key}")
    if not re.fullmatch(r"[a-z][a-z0-9-]{0,61}[a-z0-9]|[a-z]", c["namespace"]):
        raise ValueError("namespace must be a DNS label")
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,63}", c["environment"]):
        raise ValueError("invalid environment")
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,127}", c["agentId"]) or c["agentId"] == "SERVER-AGENT" or c["agentId"].startswith("SERVER-AGENT."):
        raise ValueError("invalid or reserved agentId")
    if not re.fullmatch(r"[a-zA-Z0-9.-]+", c["domain"]):
        raise ValueError("domain must be a hostname without scheme or port")
    if not local:
        for key in ("serverImage", "agentImage"):
            if not re.fullmatch(r"[^\s]+@sha256:[a-f0-9]{64}", c[key]):
                raise ValueError(f"{key} must use a release digest, not a mutable tag")
        if not c.get("publicIssuer"):
            raise ValueError("publicIssuer is required (existing cert-manager ClusterIssuer)")
        if not c["gitSource"].startswith("https://"):
            raise ValueError("gitSource must use HTTPS")
    if "@" in c["gitSource"].split("://", 1)[-1].split("/", 1)[0]:
        raise ValueError("Do not embed Git credentials in gitSource")


def private_write(path, value):
    path = Path(path)
    tmp = path.with_suffix(path.suffix + ".tmp")
    with open(os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), "w", encoding="utf-8") as f:
        json.dump(value, f, indent=2)
        f.flush()
        os.fsync(f.fileno())
    os.replace(tmp, path)


def pki(c, previous=None):
    now = dt.datetime.now(dt.timezone.utc)
    key = ec.generate_private_key(ec.SECP256R1())
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "Regente " + c["namespace"] + " internal CA")])
    ca = (x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(key.public_key())
          .serial_number(x509.random_serial_number()).not_valid_before(now - dt.timedelta(minutes=5))
          .not_valid_after(now + dt.timedelta(days=3650))
          .add_extension(x509.BasicConstraints(ca=True, path_length=0), critical=True)
          .add_extension(x509.SubjectKeyIdentifier.from_public_key(key.public_key()), critical=False)
          .add_extension(x509.AuthorityKeyIdentifier.from_issuer_public_key(key.public_key()), critical=False)
          .add_extension(x509.KeyUsage(False, False, False, False, False, True, True, False, False), critical=True)
          .sign(key, hashes.SHA256()))
    if previous:
        ca = x509.load_pem_x509_certificate(previous["ca.crt"].encode())
        key = serialization.load_pem_private_key(previous["ca.key"].encode(), password=None)
        name = ca.subject
    pem = lambda cert: cert.public_bytes(serialization.Encoding.PEM).decode()
    keypem = lambda k: k.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8,
                                       serialization.NoEncryption()).decode()
    result = {"ca.crt": pem(ca), "ca.key": keypem(key)}
    for who in ("server", "postgres", "agent"):
        leafkey = ec.generate_private_key(ec.SECP256R1())
        subject = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, c["agentId"] if who == "agent" else who)])
        cert = (x509.CertificateBuilder().subject_name(subject).issuer_name(name).public_key(leafkey.public_key())
                .serial_number(x509.random_serial_number()).not_valid_before(now - dt.timedelta(minutes=5))
                .not_valid_after(now + dt.timedelta(days=365))
                .add_extension(x509.BasicConstraints(ca=False, path_length=None), critical=True)
                .add_extension(x509.SubjectKeyIdentifier.from_public_key(leafkey.public_key()), critical=False)
                .add_extension(x509.AuthorityKeyIdentifier.from_issuer_public_key(key.public_key()), critical=False)
                .add_extension(x509.ExtendedKeyUsage([ExtendedKeyUsageOID.CLIENT_AUTH if who == "agent"
                                                      else ExtendedKeyUsageOID.SERVER_AUTH]), critical=False))
        if who != "agent":
            host = "regente-server" if who == "server" else "postgres"
            names = [x509.DNSName(host), x509.DNSName(host + "." + c["namespace"] + ".svc")]
            if who == "server":
                names += [x509.DNSName("localhost"), x509.IPAddress(ipaddress.ip_address("127.0.0.1"))]
            cert = cert.add_extension(x509.SubjectAlternativeName(names), critical=False)
        result[who] = {"tls.crt": pem(cert.sign(key, hashes.SHA256())), "tls.key": keypem(leafkey), "ca.crt": pem(ca)}
    return result


class Installer:
    def __init__(self, c, directory, local=False, rotate_agent=False):
        self.c, self.directory, self.local = c, Path(directory).resolve(), local
        self.rotate_agent = rotate_agent
        self.token = None
        self.base = None
        self.report = {"status": "incomplete", "checks": {}, "namespace": c["namespace"],
                       "context": c["context"], "agentId": c["agentId"], "environment": c["environment"],
                       "images": {k: c[k] for k in ("serverImage", "agentImage")}}

    def kube(self, *args, data=None):
        return run(["kubectl", "--context", self.c["context"], "--request-timeout=60s", "-n", self.c["namespace"], *args], data)

    def apply(self, obj):
        if obj["kind"] == "Secret" and "stringData" in obj:
            obj["data"] = {k: base64.b64encode(v.encode()).decode() for k, v in obj.pop("stringData").items()}
        run(["kubectl", "--context", self.c["context"], "--request-timeout=60s", "apply", "--server-side",
             "--field-manager=regente-installer", "-f", "-"], data=json.dumps(obj).encode())

    def save(self):
        private_write(self.directory / "state.json", self.state)

    def save_report(self):
        private_write(self.directory / "install-report.json", self.report)
        lines = ["# Regente installation", "", "Status: " + self.report["status"], "",
                 "Jobs execute inside the separate worker pod.", "",
                 *[f"- {k}: {json.dumps(v)}" for k, v in self.report["checks"].items()], "",
                 "Limits: single replica; no HA or off-host backup configured by this installer.",
                 "Internal certificates and machine token expire: see credentialExpiresAt. Renew before expiry."]
        (self.directory / "install-report.md").write_text("\n".join(lines), encoding="utf-8")

    def wait(self, name, fn, seconds=180):
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            try:
                value = fn()
                if value:
                    return value
            except urllib.error.URLError as e:
                if isinstance(e.reason, ssl.SSLCertVerificationError):
                    raise RuntimeError("TLS certificate validation failed: " + e.reason.verify_message) from None
            except (ConnectionError, TimeoutError):
                pass
            time.sleep(2)
        raise RuntimeError("Timed out: " + name)

    def preflight(self):
        validate(self.c, self.local)
        nodes = json.loads(self.kube("get", "nodes", "-o", "json"))["items"]
        if not any(any(v["type"] == "Ready" and v["status"] == "True" for v in n["status"].get("conditions", [])) for n in nodes):
            raise RuntimeError("No Ready node in selected context")
        self.kube("get", "storageclass", self.c["storageClass"])
        if not self.local:
            for crd in ("certificates.cert-manager.io", "ingressroutes.traefik.io", "serverstransports.traefik.io"):
                self.kube("get", "crd", crd)
            issuer = json.loads(self.kube("get", "clusterissuer", self.c["publicIssuer"], "-o", "json"))
            if not any(v["type"] == "Ready" and v["status"] == "True" for v in issuer.get("status", {}).get("conditions", [])):
                raise RuntimeError("Public ClusterIssuer is not Ready")
            socket.getaddrinfo(self.c["domain"], 443)
        self.report["checks"]["preflight"] = True

    def initialize(self):
        self.directory.mkdir(mode=0o700, parents=True, exist_ok=True)
        if os.name != "nt" and self.directory.stat().st_mode & 0o077:
            raise RuntimeError("State directory requires mode 0700")
        filename = self.directory / "state.json"
        config_id = model.digest({k: v for k, v in self.c.items() if k not in ("serverImage", "agentImage")})
        existing = json.loads(self.kube("get", "namespace", self.c["namespace"], "--ignore-not-found", "-o", "json") or b"{}")
        if filename.exists():
            self.state = json.loads(filename.read_text(encoding="utf-8"))
            if self.state["configId"] != config_id:
                raise RuntimeError("Configuration/identity changed. Preserve state and journal; use a planned migration, not reinstall")
        else:
            if existing:
                raise RuntimeError("Namespace already exists without this installer state; refusing to adopt or overwrite it")
            password = getpass.getpass("New administrator password (at least 12 characters): ")
            if not 12 <= len(password.encode()) <= 72 or password != password.strip():
                raise ValueError("Administrator password requires 12-72 bytes without surrounding whitespace")
            git_token = getpass.getpass("Git token (empty for a public read-only workspace): ")
            self.state = dict(configId=config_id, installId=secrets.token_hex(16), username="admin", password=password,
                              gitToken=git_token, dbPassword=secrets.token_hex(32), pki=pki(self.c))
            self.save()
        owner = existing.get("metadata", {}).get("annotations", {}).get("regente.io/install-id")
        if existing and owner != self.state["installId"]:
            raise RuntimeError("Namespace ownership differs from local state; refusing mutation")
        self.apply(dict(apiVersion="v1", kind="Namespace", metadata=dict(name=self.c["namespace"],
                   annotations={"regente.io/install-id": self.state["installId"]})))

    def secrets(self):
        s = self.state
        if self.c.get("registryConfigFile"):
            path = Path(self.c["registryConfigFile"])
            if os.name != "nt" and path.stat().st_mode & 0o077:
                raise RuntimeError("Registry config requires owner-only file permissions")
            config = json.loads(path.read_text(encoding="utf-8"))
            if not config.get("auths"):
                raise RuntimeError("Registry config must contain inline auths, not a credential-helper reference")
            self.apply(model.resource(self.c, "Secret", "regente-registry", type="kubernetes.io/dockerconfigjson",
                                      stringData={".dockerconfigjson": json.dumps(config)}))
        self.apply(model.resource(self.c, "Secret", "regente-trust", type="Opaque", stringData={"ca.crt": s["pki"]["ca.crt"]}))
        for who in ("server", "postgres"):
            self.apply(model.resource(self.c, "Secret", "regente-" + who + "-tls", type="Opaque", stringData=s["pki"][who]))
        values = {k: s[k] for k in ("dbPassword", "gitToken")}
        values["dbDSN"] = "postgres://regente:" + s["dbPassword"] + "@postgres:5432/regente?sslmode=verify-full&sslrootcert=/tls/ca.crt"
        if not s.get("bootstrapRemoved"):
            values.update(username=s["username"], password=s["password"])
        self.apply(model.resource(self.c, "Secret", "regente-install", type="Opaque", stringData=values))

    def deploy(self):
        for obj in model.manifests(self.c, not self.state.get("bootstrapRemoved"), self.state.get("gitEnabled", False)):
            if obj["kind"] == "Deployment" and obj["metadata"]["name"] == "regente-server":
                self.kube("rollout", "status", "statefulset/postgres", "--timeout=180s")
            self.apply(obj)
        self.kube("rollout", "status", "statefulset/postgres", "--timeout=180s")
        self.kube("rollout", "status", "deployment/regente-server", "--timeout=180s")

    @contextlib.contextmanager
    def connection(self):
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        p = subprocess.Popen(["kubectl", "--context", self.c["context"], "-n", self.c["namespace"],
                              "port-forward", "service/regente-server", f"{port}:8443", "--address=127.0.0.1"],
                             stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        self.base = f"https://127.0.0.1:{port}"
        try:
            self.wait("verified HTTPS to server", lambda: self.api("/readyz").get("ready"))
            self.token = self.api("/api/auth/login", {"username": self.state["username"], "password": self.state["password"]})["token"]
            yield
        finally:
            p.terminate()
            p.wait(timeout=10)
            self.token = None

    def api(self, path, body=None, method=None, headers=None):
        ctx = ssl.create_default_context(cadata=self.state["pki"]["ca.crt"])
        req = urllib.request.Request(self.base + path, data=json.dumps(body).encode() if body is not None else None,
                 method=method, headers={"Content-Type": "application/json", **({"Authorization": "Bearer " + self.token} if self.token else {}), **(headers or {})})
        try:
            with urllib.request.urlopen(req, context=ctx, timeout=30) as r:
                raw = r.read()
                return json.loads(raw) if raw else None
        except urllib.error.HTTPError as e:
            detail = e.read(2048).decode(errors="replace").strip()
            for key in ("password", "dbPassword", "gitToken", "agentToken"):
                if self.state.get(key):
                    detail = detail.replace(self.state[key], "[redacted]")
            raise RuntimeError(f"API {method or ('POST' if body is not None else 'GET')} {path}: HTTP {e.code}: {detail}") from None

    def enroll(self):
        if self.state.get("rotationPending"):
            raise RuntimeError("A credential rotation was interrupted before saving its receipt; recover the issued credential explicitly")
        if self.rotate_agent:
            if not self.state.get("agentTokenId"):
                raise RuntimeError("Install first before rotating the worker credential")
            new_cert = pki(self.c, self.state["pki"])["agent"]
            fingerprint = x509.load_pem_x509_certificate(new_cert["tls.crt"].encode()).fingerprint(hashes.SHA256()).hex()
            expires = (dt.datetime.now(dt.timezone.utc) + dt.timedelta(days=90)).isoformat()
            self.state["rotationPending"] = True
            self.save()
            issued = self.api("/api/agents/tokens/" + str(self.state["agentTokenId"]) + "/rotate",
                              dict(expiresAt=expires, graceSeconds=300, certificateSHA256=fingerprint))
            self.state.update(agentToken=issued["token"], agentTokenId=issued["id"], credentialExpiresAt=expires)
            self.state["pki"]["agent"] = new_cert
            self.state.pop("rotationPending")
            self.save()
            self.rotate_agent = False
        cert = x509.load_pem_x509_certificate(self.state["pki"]["agent"]["tls.crt"].encode())
        fingerprint = cert.fingerprint(hashes.SHA256()).hex()
        tokens = self.api("/api/agents/tokens")
        own = [t for t in tokens if t["agentId"] == self.c["agentId"] and t["status"] == "active"]
        if "agentToken" not in self.state:
            if own:
                raise RuntimeError("Agent credential exists but local token is missing. Recover/rotate explicitly; refusing duplicate enrollment")
            expires = (dt.datetime.now(dt.timezone.utc) + dt.timedelta(days=90)).isoformat()
            issued = self.api("/api/agents/tokens", dict(label="Kubernetes installer", agentId=self.c["agentId"],
                environment=self.c["environment"], capabilities=model.CAPS, certificateSHA256=fingerprint, expiresAt=expires))
            self.state.update(agentToken=issued["token"], agentTokenId=issued["id"], credentialExpiresAt=expires)
            self.save()
        else:
            match = next((t for t in own if t["id"] == self.state["agentTokenId"]), None)
            if not match or match.get("certificateSHA256") != fingerprint or match["environment"] != self.c["environment"] or sorted(match["capabilities"]) != sorted(model.CAPS):
                raise RuntimeError("Agent token/identity/certificate differs or expired; explicit credential rotation required")
        credentials = {**self.state["pki"]["agent"], "token": self.state["agentToken"]}
        self.apply(model.resource(self.c, "Secret", "regente-agent-credentials", type="Opaque", stringData=credentials))
        self.apply(model.worker(self.c, model.digest(credentials)))
        self.kube("rollout", "status", "statefulset/regente-agent", "--timeout=180s")
        self.wait("agent authenticated and online", lambda: any(a["id"] == self.c["agentId"] and a["online"] for a in self.api("/api/agents")))
        self.report["checks"]["agent_identity_mtls"] = fingerprint
        self.report["checks"]["credentialExpiresAt"] = self.state["credentialExpiresAt"]
        self.report["checks"]["internalServerCertificateExpiresAt"] = x509.load_pem_x509_certificate(
            self.state["pki"]["server"]["tls.crt"].encode()).not_valid_after_utc.isoformat()

    def smoke(self):
        marker = "install-" + self.state["installId"]
        if "smokeInstance" not in self.state:
            if self.state.get("gitEnabled"):
                raise RuntimeError("Missing smoke receipt after GitOps enabled; refusing to write into user repository")
            definition = dict(id=marker, label="Installation smoke test", team="onboarding", jobType="COMMAND", agentId=self.c["agentId"],
                environment=self.c["environment"], schedule={"enabled": False}, timeout=30,
                params={"command": "printf '%s\\n' " + marker + " >> /state/install-effects.log; echo " + marker})
            self.api("/api/definitions", definition)
            if self.state.get("smokeOrderStarted"):
                raise RuntimeError("Smoke ordering was interrupted without a receipt; inspect instances before any retry (do not repeat effects)")
            self.state["smokeOrderStarted"] = True
            self.save()
            self.state["smokeInstance"] = self.api("/api/definitions/" + marker + "/force", {})["instanceId"]
            self.save()
        iid = self.state["smokeInstance"]
        def finished():
            row = self.api("/api/instances/" + iid)
            if row["status"] in ("FAIL", "NOTOK", "CANCELLED", "UNCERTAIN"):
                raise RuntimeError("Smoke execution failed or uncertain; preserve journal and inspect instance " + iid)
            return row if row["status"] == "OK" else None
        row = self.wait("smoke execution OK", finished)
        effects = self.kube("exec", "statefulset/regente-agent", "-c", "agent", "--", "cat", "/state/install-effects.log").decode().splitlines()
        if effects.count(marker) != 1 or marker not in row.get("output", ""):
            raise RuntimeError("Smoke output/effect verification failed")
        self.report["checks"]["durable_job"] = {"instanceId": iid, "status": "OK", "effectCount": 1}
        tls = self.kube("exec", "statefulset/postgres", "--", "psql", "-U", "regente", "-d", "regente", "-Atc",
            "SELECT count(*) FROM pg_stat_ssl s JOIN pg_stat_activity a USING(pid) WHERE a.usename='regente' AND a.client_addr IS NOT NULL AND s.ssl").decode().strip()
        if not tls.isdigit() or int(tls) < 1:
            raise RuntimeError("No live Regente PostgreSQL TLS session found")
        self.report["checks"]["postgres_tls_connections"] = int(tls)

    def design(self):
        def synchronized():
            status = self.api("/api/git/status")
            return status if status.get("configured") and status.get("sha") and not status.get("error") else None
        status = self.wait("Git workspace synchronization (check source, branch and token)", synchronized)
        folders = self.api("/api/folders")
        session = self.api("/api/design/sessions", {"folders": [folders[0]["name"]] if folders else []})
        self.api("/api/design/sessions/" + session["id"])
        self.api("/api/design/sessions/" + session["id"], method="DELETE", headers={"If-Match": '"' + str(session["revision"]) + '"'})
        self.report["checks"]["design_open_folder"] = True
        # Select public fields only: never include tokens or full status blindly.
        self.report["checks"]["git"] = {k: status[k] for k in ("branch", "head", "commit", "sha") if k in status}

    def install(self):
        print("Preflight: cluster, storage and installation ownership", flush=True)
        self.preflight()
        self.initialize()
        try:
            print("Deploying PostgreSQL and server", flush=True)
            self.secrets()
            self.deploy()
            with self.connection():
                print("Reconciling worker identity and checking durable execution", flush=True)
                self.enroll()
                self.smoke()
            self.state["gitEnabled"] = True
            self.save()
            self.deploy()
            with self.connection():
                print("Checking Git synchronization and opening a Design folder", flush=True)
                self.design()
            # Remove BOTH bootstrap refs first, verify rollout, then remove Secret keys.
            self.state["bootstrapRemoved"] = True
            self.deploy()
            self.save()
            self.secrets()
            with self.connection():
                print("Checking login after bootstrap removal and preserving smoke receipt", flush=True)
                self.smoke()
            keys = json.loads(self.kube("get", "secret", "regente-install", "-o", "json")).get("data", {})
            if "username" in keys or "password" in keys:
                raise RuntimeError("Bootstrap credential keys were not removed")
            volumes = json.loads(self.kube("get", "pvc", "-o", "json"))["items"]
            if any(v.get("status", {}).get("phase") != "Bound" for v in volumes):
                raise RuntimeError("Persistent volumes are not all Bound")
            self.report["checks"]["persistent_volumes_bound"] = len(volumes)
            self.report["checks"]["bootstrap_removed_login_preserved"] = True
            if not self.local:
                for obj in model.ingress(self.c):
                    self.apply(obj)
                self.kube("wait", "certificate/regente-public", "--for=condition=Ready", "--timeout=180s")
                with urllib.request.urlopen("https://" + self.c["domain"] + "/readyz", timeout=30) as r:
                    if not json.load(r).get("ready"):
                        raise RuntimeError("Public HTTPS readiness failed")
                self.report["checks"]["public_https"] = True
            self.report["status"] = "local-verified" if self.local else "functional-verified"
        except Exception as e:
            self.report["error"] = type(e).__name__  # Full diagnostics may contain paths/credentials.
            raise
        finally:
            self.save_report()


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--config", required=True)
    p.add_argument("--state-dir", required=True, help="Protected directory outside Git; keep for reconciliation/recovery")
    p.add_argument("--local-test", action="store_true", help="Isolated test only: allow local image tags and skip public ingress")
    p.add_argument("--rotate-agent-credential", action="store_true", help="Rotate worker token and certificate together before token expiry; preserves identity and journal")
    args = p.parse_args()
    c = json.loads(Path(args.config).read_text(encoding="utf-8-sig"))
    install = Installer(c, args.state_dir, args.local_test, args.rotate_agent_credential)
    try:
        install.install()
    except (ValueError, RuntimeError, subprocess.TimeoutExpired, OSError) as e:
        print("Installation incomplete: " + str(e), file=sys.stderr)
        return 1
    print("Functional checks passed. Report: " + str(install.directory / "install-report.md"))
    return 0


if __name__ == "__main__":
    sys.exit(main())
