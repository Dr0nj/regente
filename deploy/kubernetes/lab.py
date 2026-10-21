"""Isolated kind laboratory; no cloud account or production credentials.
Requires Python cryptography, docker, kubectl and kind. Runtime secrets stay in
.integration/kubernetes (ignored by Git). Always uses an explicit kube context.
"""

import argparse, base64, datetime as dt, hashlib, ipaddress, json, os, secrets
from pathlib import Path
import ssl, subprocess, time, urllib.request, urllib.error
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec, ed25519
from cryptography.x509.oid import NameOID, ExtendedKeyUsageOID

ROOT = Path(__file__).resolve().parents[2]
STATE = ROOT / ".integration/kubernetes"
CONTEXT = "kind-regente-kubernetes"
NS = "regente-kubernetes-lab"
SERVER = "regente-kubernetes-server:9311778"
AGENT = "regente-kubernetes-agent:9311778"
PG = "postgres:17.6-alpine@sha256:747d5ed1fdeeb124b880fbe3d7c6557d2c4064ae41d6b6297d417882effce4be"
WORKER = "regente-kubernetes-external-agent"


def run(args, data=None, timeout=120):
    p = subprocess.run(args, input=data, capture_output=True, timeout=timeout)
    if p.returncode:
        # Do not include stdin, which can contain Secret objects or credentials.
        raise RuntimeError(
            str(args[:4]) + ": " + p.stderr.decode(errors="replace")[-1800:]
        )
    return p.stdout


def kube(*args, data=None):
    return run(["kubectl", "--context", CONTEXT, "--request-timeout=30s", *args], data)


def apply(obj):
    return kube("apply", "-f", "-", data=json.dumps(obj).encode())


def resource(kind, name, **values):
    return dict(
        apiVersion="v1",
        kind=kind,
        metadata=(
            {"name": name} if kind == "Namespace" else dict(name=name, namespace=NS)
        ),
        **values,
    )


def certs():
    STATE.mkdir(parents=True, exist_ok=True)
    if (STATE / "private.json").exists():
        raise RuntimeError(
            "Existing lab state: preserve it; prepare is deliberately not destructive"
        )
    ca_key = ec.generate_private_key(ec.SECP256R1())
    now = dt.datetime.now(dt.timezone.utc)
    name = x509.Name(
        [x509.NameAttribute(NameOID.COMMON_NAME, "Regente Kubernetes local CA")]
    )
    ca = (
        x509.CertificateBuilder()
        .subject_name(name)
        .issuer_name(name)
        .public_key(ca_key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(now - dt.timedelta(minutes=5))
        .not_valid_after(now + dt.timedelta(days=30))
        .add_extension(x509.BasicConstraints(ca=True, path_length=0), critical=True)
        .add_extension(
            x509.SubjectKeyIdentifier.from_public_key(ca_key.public_key()),
            critical=False,
        )
        .add_extension(
            x509.AuthorityKeyIdentifier.from_issuer_public_key(ca_key.public_key()),
            critical=False,
        )
        .add_extension(
            x509.KeyUsage(
                digital_signature=True,
                content_commitment=False,
                key_encipherment=False,
                data_encipherment=False,
                key_agreement=False,
                key_cert_sign=True,
                crl_sign=True,
                encipher_only=None,
                decipher_only=None,
            ),
            critical=True,
        )
        .sign(ca_key, hashes.SHA256())
    )
    (STATE / "ca.crt").write_bytes(ca.public_bytes(serialization.Encoding.PEM))
    for who in ("server", "client"):
        key = ec.generate_private_key(ec.SECP256R1())
        b = (
            x509.CertificateBuilder()
            .subject_name(x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, who)]))
            .issuer_name(name)
            .public_key(key.public_key())
            .serial_number(x509.random_serial_number())
            .not_valid_before(now - dt.timedelta(minutes=5))
            .not_valid_after(now + dt.timedelta(days=7))
        )
        b = b.add_extension(
            x509.ExtendedKeyUsage(
                [
                    (
                        ExtendedKeyUsageOID.SERVER_AUTH
                        if who == "server"
                        else ExtendedKeyUsageOID.CLIENT_AUTH
                    )
                ]
            ),
            critical=False,
        )
        if who == "server":
            b = b.add_extension(
                x509.SubjectAlternativeName(
                    [
                        x509.DNSName("localhost"),
                        x509.DNSName("regente-kubernetes.local"),
                        x509.DNSName("regente-server"),
                        x509.IPAddress(ipaddress.ip_address("127.0.0.1")),
                    ]
                ),
                critical=False,
            )
        b = (
            b.add_extension(
                x509.BasicConstraints(ca=False, path_length=None), critical=True
            )
            .add_extension(
                x509.SubjectKeyIdentifier.from_public_key(key.public_key()),
                critical=False,
            )
            .add_extension(
                x509.AuthorityKeyIdentifier.from_issuer_public_key(ca_key.public_key()),
                critical=False,
            )
        )
        cert = b.sign(ca_key, hashes.SHA256())
        (STATE / (who + ".crt")).write_bytes(
            cert.public_bytes(serialization.Encoding.PEM)
        )
        (STATE / (who + ".key")).write_bytes(
            key.private_bytes(
                serialization.Encoding.PEM,
                serialization.PrivateFormat.PKCS8,
                serialization.NoEncryption(),
            )
        )
    # Go ed25519.PrivateKey is seed || public key, not PKCS8.
    key = ed25519.Ed25519PrivateKey.generate()
    (STATE / "audit.key").write_bytes(
        key.private_bytes_raw() + key.public_key().public_bytes_raw()
    )
    private = dict(password=secrets.token_urlsafe(30), dbPassword=secrets.token_hex(24))
    (STATE / "private.json").write_text(json.dumps(private))
    for f in STATE.iterdir():
        if f.is_file():
            os.chmod(f, 0o600)
    print("Created local lab credentials (values not printed)")


def manifests():
    env = {
        "REGENTE_PROFILE": "production",
        "REGENTE_ENVIRONMENT": "kubernetes-lab",
        "REGENTE_NETWORK_BOUNDARY": "tls",
        "REGENTE_ADDR": "0.0.0.0:8443",
        "REGENTE_APP_URL": "https://localhost:8443",
        "REGENTE_AUTH_MODE": "local",
        "REGENTE_TOKEN": "",
        "REGENTE_DEMO_MODE": "0",
        "REGENTE_SERVER_AGENT": "0",
        "REGENTE_CONTROL_PLANE_EXECUTION": "deny",
        "REGENTE_DB_DRIVER": "postgres",
        "REGENTE_WORKSPACE": "/data/workspace",
        "REGENTE_SPA_DIR": "/app",
        "REGENTE_TLS_CERT": "/private/server.crt",
        "REGENTE_TLS_KEY": "/private/server.key",
        "REGENTE_TLS_CLIENT_CA": "/private/ca.crt",
        "REGENTE_AUDIT_KEY": "/data/audit.key",
        "REGENTE_NODE_ID": "kubernetes-lab-01",
        "REGENTE_GIT_SOURCE": "",
    }
    items = [
        resource("Namespace", NS),
        resource("ConfigMap", "server-config", data=env),
    ]
    for name in ("postgres-data", "server-data"):
        items.append(
            resource(
                "PersistentVolumeClaim",
                name,
                spec=dict(
                    accessModes=["ReadWriteOnce"],
                    storageClassName="standard",
                    resources=dict(requests={"storage": "2Gi"}),
                ),
            )
        )
    items.append(
        resource(
            "Service",
            "postgres",
            spec=dict(
                selector={"app": "postgres"}, ports=[dict(port=5432, targetPort=5432)]
            ),
        )
    )
    items.append(
        dict(
            apiVersion="apps/v1",
            kind="Deployment",
            metadata=dict(name="postgres", namespace=NS),
            spec=dict(
                replicas=1,
                strategy={"type": "Recreate"},
                selector={"matchLabels": {"app": "postgres"}},
                template=dict(
                    metadata={"labels": {"app": "postgres"}},
                    spec=dict(
                        automountServiceAccountToken=False,
                        securityContext=dict(
                            runAsUser=70,
                            runAsGroup=70,
                            fsGroup=70,
                            seccompProfile={"type": "RuntimeDefault"},
                        ),
                        containers=[
                            dict(
                                name="postgres",
                                image=PG,
                                env=[
                                    dict(name="POSTGRES_USER", value="regente"),
                                    dict(name="POSTGRES_DB", value="regente"),
                                    dict(
                                        name="PGDATA",
                                        value="/var/lib/postgresql/data/pgdata",
                                    ),
                                    dict(
                                        name="POSTGRES_PASSWORD",
                                        valueFrom={
                                            "secretKeyRef": {
                                                "name": "lab-env",
                                                "key": "DB_PASSWORD",
                                            }
                                        },
                                    ),
                                ],
                                resources=dict(
                                    requests={"cpu": "100m", "memory": "128Mi"},
                                    limits={"cpu": "1", "memory": "512Mi"},
                                ),
                                securityContext=dict(
                                    allowPrivilegeEscalation=False,
                                    capabilities={"drop": ["ALL"]},
                                ),
                                readinessProbe=dict(
                                    exec={
                                        "command": [
                                            "pg_isready",
                                            "-U",
                                            "regente",
                                            "-d",
                                            "regente",
                                        ]
                                    },
                                    periodSeconds=3,
                                ),
                                volumeMounts=[
                                    dict(
                                        name="data",
                                        mountPath="/var/lib/postgresql/data",
                                    )
                                ],
                            )
                        ],
                        volumes=[
                            dict(
                                name="data",
                                persistentVolumeClaim={"claimName": "postgres-data"},
                            )
                        ],
                    ),
                ),
            ),
        )
    )
    items.append(
        resource(
            "Service",
            "regente-server",
            spec=dict(
                type="NodePort",
                selector={"app": "regente-server"},
                ports=[dict(name="https", port=8443, targetPort=8443, nodePort=30443)],
            ),
        )
    )
    server = dict(
        name="server",
        image=SERVER,
        imagePullPolicy="Never",
        args=["-selfmon=false", "-tick-ms=200"],
        envFrom=[{"configMapRef": {"name": "server-config"}}],
        env=[
            dict(
                name="REGENTE_DB",
                valueFrom={"secretKeyRef": {"name": "lab-env", "key": "DB_DSN"}},
            ),
            dict(
                name="REGENTE_BOOTSTRAP_PASSWORD",
                valueFrom={
                    "secretKeyRef": {
                        "name": "lab-env",
                        "key": "BOOTSTRAP_PASSWORD",
                        "optional": True,
                    }
                },
            ),
        ],
        ports=[dict(containerPort=8443)],
        securityContext=dict(
            runAsUser=65532,
            runAsGroup=65532,
            runAsNonRoot=True,
            allowPrivilegeEscalation=False,
            readOnlyRootFilesystem=True,
            capabilities={"drop": ["ALL"]},
        ),
        resources=dict(
            requests={"cpu": "200m", "memory": "256Mi"},
            limits={"cpu": "2", "memory": "1Gi"},
        ),
        volumeMounts=[
            dict(name="data", mountPath="/data"),
            dict(name="private", mountPath="/private", readOnly=True),
            dict(name="tmp", mountPath="/tmp"),
        ],
    )
    if (STATE / "bootstrapped").exists():
        server["env"] = [
            e for e in server["env"] if e["name"] != "REGENTE_BOOTSTRAP_PASSWORD"
        ]
    for field, path in [
        ("startupProbe", "/livez"),
        ("livenessProbe", "/livez"),
        ("readinessProbe", "/readyz"),
    ]:
        server[field] = dict(
            httpGet=dict(path=path, port=8443, scheme="HTTPS"),
            periodSeconds=3,
            timeoutSeconds=2,
            failureThreshold=60 if field == "startupProbe" else 3,
        )
    init = dict(
        name="private-files",
        image=AGENT,
        imagePullPolicy="Never",
        command=[
            "/bin/sh",
            "-ec",
            "cp /source/* /private/; chmod 600 /private/*; chown 65532:65532 /private/*; cp -n /source/audit.key /data/audit.key; chmod 600 /data/audit.key; mkdir -p /data/workspace/definitions/lab; cp -n /fixtures/* /data/workspace/definitions/lab/; chown -R 65532:65532 /data",
        ],
        securityContext=dict(
            runAsUser=0,
            allowPrivilegeEscalation=False,
            capabilities={"drop": ["ALL"], "add": ["CHOWN", "DAC_OVERRIDE", "FOWNER"]},
        ),
        resources=dict(
            requests={"cpu": "10m", "memory": "32Mi"},
            limits={"cpu": "200m", "memory": "128Mi"},
        ),
        volumeMounts=[
            dict(name="source", mountPath="/source", readOnly=True),
            dict(name="private", mountPath="/private"),
            dict(name="data", mountPath="/data"),
            dict(name="fixtures", mountPath="/fixtures", readOnly=True),
        ],
    )
    items.append(
        dict(
            apiVersion="apps/v1",
            kind="Deployment",
            metadata=dict(name="regente-server", namespace=NS),
            spec=dict(
                replicas=1,
                strategy={"type": "Recreate"},
                selector={"matchLabels": {"app": "regente-server"}},
                template=dict(
                    metadata={"labels": {"app": "regente-server"}},
                    spec=dict(
                        automountServiceAccountToken=False,
                        terminationGracePeriodSeconds=30,
                        securityContext={"seccompProfile": {"type": "RuntimeDefault"}},
                        initContainers=[
                            init,
                            dict(
                                name="wait-postgres",
                                image=PG,
                                command=[
                                    "sh",
                                    "-ec",
                                    "for i in $(seq 1 60); do pg_isready -h postgres -U regente -d regente && exit 0; sleep 2; done; exit 1",
                                ],
                                securityContext=dict(
                                    runAsUser=70,
                                    runAsNonRoot=True,
                                    allowPrivilegeEscalation=False,
                                    capabilities={"drop": ["ALL"]},
                                ),
                                resources=dict(
                                    requests={"cpu": "10m", "memory": "32Mi"},
                                    limits={"cpu": "200m", "memory": "128Mi"},
                                ),
                            ),
                        ],
                        containers=[server],
                        volumes=[
                            dict(
                                name="data",
                                persistentVolumeClaim={"claimName": "server-data"},
                            ),
                            dict(
                                name="source",
                                secret={
                                    "secretName": "server-files",
                                    "defaultMode": 256,
                                },
                            ),
                            dict(name="private", emptyDir={"medium": "Memory"}),
                            dict(name="tmp", emptyDir={}),
                            dict(name="fixtures", configMap={"name": "lab-fixtures"}),
                        ],
                    ),
                ),
            ),
        )
    )
    return dict(apiVersion="v1", kind="List", items=items)


def deploy():
    # Do not overwrite another lab's identity or password on existing PVCs.
    existing = json.loads(
        kube(
            "-n",
            NS,
            "get",
            "secret",
            "server-files",
            "--ignore-not-found",
            "-o",
            "json",
        )
        or b"{}"
    )
    if (
        existing
        and existing.get("data", {}).get("ca.crt")
        != base64.b64encode((STATE / "ca.crt").read_bytes()).decode()
    ):
        raise RuntimeError(
            "Existing namespace belongs to another lab identity; refusing deployment"
        )
    private = json.loads((STATE / "private.json").read_text())
    apply(resource("Namespace", NS))
    apply(
        resource(
            "Secret",
            "lab-env",
            type="Opaque",
            stringData={
                "DB_PASSWORD": private["dbPassword"],
                "DB_DSN": "postgres://regente:"
                + private["dbPassword"]
                + "@postgres:5432/regente?sslmode=disable",
                "BOOTSTRAP_PASSWORD": private["password"],
            },
        )
    )
    apply(
        resource(
            "Secret",
            "server-files",
            type="Opaque",
            data={
                f: base64.b64encode((STATE / f).read_bytes()).decode()
                for f in ("server.crt", "server.key", "ca.crt", "audit.key")
            },
        )
    )
    fixtures = {}
    commands = {
        **{
            f"success-{i:02}": f'printf "effect-{i:02}\\n" >> /state/effects.log; echo ok-{i:02}'
            for i in range(10)
        },
        "failure": "echo expected-failure >&2; exit 7",
        "cancel": "echo started; sleep 60; echo finished",
        "recovery": "echo recovery >> /state/effects.log; sleep 8; echo recovered",
        "agent-restart": "echo agent-restart >> /state/effects.log; sleep 60; echo done",
    }
    for name, command in commands.items():
        fixtures[name + ".yaml"] = json.dumps(
            dict(
                id=name,
                team="lab",
                jobType="COMMAND",
                agentId=WORKER,
                environment="kubernetes-lab",
                schedule={"enabled": False},
                timeout=90,
                params={"command": command},
            )
        )
    apply(resource("ConfigMap", "lab-fixtures", data=fixtures))
    obj = manifests()
    (STATE / "manifest.json").write_text(json.dumps(obj, indent=2))
    apply(obj)
    print(
        "Applied isolated laboratory; inspect readiness before provisioning the agent"
    )


TOKEN = None
BASE = "https://127.0.0.1:8443"


def api(path, body=None, method=None):
    ctx = ssl.create_default_context(cafile=str(STATE / "ca.crt"))
    req = urllib.request.Request(
        BASE + path,
        data=json.dumps(body).encode() if body is not None else None,
        method=method or ("POST" if body is not None else "GET"),
        headers={
            "Content-Type": "application/json",
            **({"Authorization": "Bearer " + TOKEN} if TOKEN else {}),
        },
    )
    with urllib.request.urlopen(req, context=ctx, timeout=10) as response:
        data = response.read()
        return json.loads(data) if data else None


def login():
    global TOKEN
    private = json.loads((STATE / "private.json").read_text())
    TOKEN = api(
        "/api/auth/login", dict(username="admin", password=private["password"])
    )["token"]


def finalize():
    login()
    (STATE / "bootstrapped").touch()
    deploy()
    print(
        "Bootstrap password removed from server environment; keep private.json protected"
    )


def agent():
    existing = (
        run(
            [
                "docker",
                "ps",
                "-a",
                "--filter",
                "name=^/" + WORKER + "$",
                "--format",
                "{{.Names}}",
            ]
        )
        .decode()
        .strip()
    )
    if existing:
        raise RuntimeError(
            "Agent container already exists; restart it instead of replacing its identity"
        )
    login()
    cert = x509.load_pem_x509_certificate((STATE / "client.crt").read_bytes())
    issued = api(
        "/api/agents/tokens",
        dict(
            label="Kubernetes external laboratory",
            agentId=WORKER,
            environment="kubernetes-lab",
            capabilities=["COMMAND", "EXECUTION_V2"],
            certificateSHA256=cert.fingerprint(hashes.SHA256()).hex(),
            expiresAt=(
                dt.datetime.now(dt.timezone.utc) + dt.timedelta(days=1)
            ).isoformat(),
        ),
    )
    (STATE / "credential-id.json").write_text(json.dumps({"id": issued["id"]}))
    (STATE / "token").write_text(issued["token"])
    os.chmod(STATE / "token", 0o600)
    for name in ("regente-kubernetes-journal", "regente-kubernetes-credentials"):
        run(["docker", "volume", "create", name])
    # Linux volume owns credentials; Windows bind permissions are not the security contract.
    run(
        [
            "docker",
            "run",
            "--rm",
            "--user",
            "0",
            "--entrypoint",
            "/bin/sh",
            "-v",
            str(STATE) + ":/source:ro",
            "-v",
            "regente-kubernetes-credentials:/credentials",
            "-v",
            "regente-kubernetes-journal:/state",
            AGENT,
            "-ec",
            "cp /source/client.crt /source/client.key /source/ca.crt /source/token /credentials/; chmod 600 /credentials/*; chown -R 10001:10001 /credentials /state",
        ]
    )
    node = next(
        n
        for n in json.loads(kube("get", "nodes", "-o", "json"))["items"]
        if n["metadata"]["name"] == "regente-kubernetes-control-plane"
    )
    address = next(
        a["address"] for a in node["status"]["addresses"] if a["type"] == "InternalIP"
    )
    run(
        [
            "docker",
            "run",
            "-d",
            "--name",
            WORKER,
            "--network",
            "kind",
            "--add-host",
            "regente-kubernetes.local:" + address,
            "--read-only",
            "--tmpfs",
            "/tmp",
            "--cap-drop",
            "ALL",
            "--security-opt",
            "no-new-privileges",
            "--memory",
            "512m",
            "--cpus",
            "1",
            "--pids-limit",
            "128",
            "-v",
            "regente-kubernetes-credentials:/credentials:ro",
            "-v",
            "regente-kubernetes-journal:/state",
            AGENT,
            "-transport",
            "v2",
            "-server",
            "https://regente-kubernetes.local:30443",
            "-id",
            WORKER,
            "-env",
            "kubernetes-lab",
            "-caps",
            "COMMAND",
            "-journal",
            "/state/journal.db",
            "-token-file",
            "/credentials/token",
            "-tls-cert",
            "/credentials/client.crt",
            "-tls-key",
            "/credentials/client.key",
            "-tls-ca",
            "/credentials/ca.crt",
            "-concurrency",
            "2",
        ]
    )
    print("External agent started with certificate-bound v2 identity")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "action", choices=["prepare", "deploy", "agent", "render", "finalize"]
    )
    args = parser.parse_args()
    if args.action == "prepare":
        certs()
    elif args.action == "deploy":
        deploy()
    elif args.action == "agent":
        agent()
    elif args.action == "finalize":
        finalize()
    else:
        print(json.dumps(manifests(), indent=2))
