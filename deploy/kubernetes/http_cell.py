"""Single-owner HTTP execution cell for the isolated kind laboratory.

Requires a NEW REGENTE_LAB_INSTANCE. Never force-delete a pod or scale above one.
Secrets are copied to regular 0600 tmpfs files: the agent rejects projected symlinks.
"""

import argparse
import base64
import datetime as dt
import json
import secrets
from pathlib import Path

from cryptography import x509
from cryptography.hazmat.primitives import hashes
import lab

NAME = "http-agent"
POD = NAME + "-0"
IDENTITY = lab.INSTANCE + "-http"
FIREWALL = "regente-kubernetes-firewall:phase02"
NODE = "regente-kubernetes-web-check:latest"
LIMITS = dict(
    requests={"cpu": "50m", "memory": "64Mi"}, limits={"cpu": "1", "memory": "512Mi"}
)
SC = dict(
    runAsUser=10001,
    runAsGroup=10001,
    runAsNonRoot=True,
    allowPrivilegeEscalation=False,
    readOnlyRootFilesystem=True,
    capabilities={"drop": ["ALL"]},
)


def mount(name, path, ro=False):
    return dict(name=name, mountPath=path, readOnly=ro)


def secret(name, files):
    lab.apply(
        lab.resource(
            "Secret",
            name,
            type="Opaque",
            data={
                key: base64.b64encode(value).decode() for key, value in files.items()
            },
        )
    )


def service(name):
    lab.apply(
        lab.resource(
            "Service",
            name,
            spec=dict(
                selector={"app": name},
                ports=[dict(name="https", port=8443, targetPort=8443)],
            ),
        )
    )
    return json.loads(lab.kube("-n", lab.NS, "get", "service", name, "-o", "json"))[
        "spec"
    ]["clusterIP"]


def podexec(*command, data=None):
    return lab.kube(
        "-n", lab.NS, "exec", "-i", POD, "-c", "agent", "--", *command, data=data
    )


def prepare():
    if lab.INSTANCE == "regente-kubernetes":
        raise RuntimeError("Select a new isolated REGENTE_LAB_INSTANCE")
    lab.certs(http_cell=True)
    lab.deploy()


def fixture():
    service("http-fixture")
    # Independently reachable negative control, same endpoint but a different Service IP.
    lab.apply(
        lab.resource(
            "Service",
            "forbidden-fixture",
            spec=dict(
                selector={"app": "http-fixture"},
                ports=[dict(port=8443, targetPort=8443)],
            ),
        )
    )
    secret(
        "fixture-tls",
        {
            f: (lab.STATE / f).read_bytes()
            for f in ("http-fixture.crt", "http-fixture.key")
        },
    )
    lab.apply(
        lab.resource(
            "ConfigMap",
            "http-fixture-code",
            data={
                "fixture.cjs": Path(__file__).with_name("http-fixture.cjs").read_text()
            },
        )
    )
    lab.apply(
        lab.resource(
            "PersistentVolumeClaim",
            "http-ledger",
            spec=dict(
                accessModes=["ReadWriteOnce"],
                storageClassName="standard",
                resources={"requests": {"storage": "1Gi"}},
            ),
        )
    )
    lab.apply(
        dict(
            apiVersion="apps/v1",
            kind="Deployment",
            metadata=dict(name="http-fixture", namespace=lab.NS),
            spec=dict(
                replicas=1,
                strategy={"type": "Recreate"},
                selector={"matchLabels": {"app": "http-fixture"}},
                template=dict(
                    metadata={"labels": {"app": "http-fixture"}},
                    spec=dict(
                        automountServiceAccountToken=False,
                        securityContext=dict(
                            runAsUser=10001,
                            runAsGroup=10001,
                            fsGroup=10001,
                            seccompProfile={"type": "RuntimeDefault"},
                        ),
                        containers=[
                            dict(
                                name="fixture",
                                image=NODE,
                                imagePullPolicy="Never",
                                command=["node", "/code/fixture.cjs"],
                                securityContext=SC,
                                resources=LIMITS,
                                readinessProbe=dict(
                                    httpGet=dict(
                                        scheme="HTTPS", path="/health", port=8443
                                    ),
                                    periodSeconds=2,
                                ),
                                volumeMounts=[
                                    mount("code", "/code", True),
                                    mount("tls", "/tls", True),
                                    mount("auth", "/auth", True),
                                    mount("ledger", "/data"),
                                ],
                            )
                        ],
                        volumes=[
                            dict(name="code", configMap={"name": "http-fixture-code"}),
                            dict(
                                name="tls",
                                secret={
                                    "secretName": "fixture-tls",
                                    "defaultMode": 288,
                                },
                            ),
                            dict(
                                name="auth",
                                secret={
                                    "secretName": "fixture-auth",
                                    "defaultMode": 288,
                                },
                            ),
                            dict(
                                name="ledger",
                                persistentVolumeClaim={"claimName": "http-ledger"},
                            ),
                        ],
                    ),
                ),
            ),
        )
    )


def manifest(server_ip, fixture_ip, dns_ip):
    # No host network, no privileged container. NET_ADMIN exists only during init.
    rules = [
        "set -eu",
        "iptables -P OUTPUT DROP",
        "iptables -F OUTPUT",
        "iptables -A OUTPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT",
    ]
    for proto in ("udp", "tcp"):
        rules.append(
            f"iptables -A OUTPUT -d {dns_ip}/32 -p {proto} --dport 53 -j ACCEPT"
        )
    for ip in (server_ip, fixture_ip):
        rules.append(f"iptables -A OUTPUT -d {ip}/32 -p tcp --dport 8443 -j ACCEPT")
    rules += [
        "iptables -A OUTPUT -j REJECT",
        "ip6tables -P OUTPUT DROP",
        "ip6tables -F OUTPUT",
    ]
    init_security = dict(
        runAsUser=0,
        allowPrivilegeEscalation=False,
        readOnlyRootFilesystem=True,
        capabilities={"drop": ["ALL"], "add": ["CHOWN", "FOWNER", "DAC_OVERRIDE"]},
    )
    private_init = dict(
        name="private-files",
        image=lab.AGENT,
        imagePullPolicy="Never",
        resources=LIMITS,
        command=[
            "sh",
            "-ec",
            'test "$HOSTNAME" = http-agent-0; cp /source/* /private/; chmod 600 /private/*; '
            "chown -R 10001:10001 /private /state",
        ],
        securityContext=init_security,
        volumeMounts=[
            mount("source", "/source", True),
            mount("private", "/private"),
            mount("journal", "/state"),
        ],
    )
    firewall = dict(
        name="egress-firewall",
        image=FIREWALL,
        imagePullPolicy="Never",
        resources=LIMITS,
        command=["sh", "-ec", "\n".join(rules)],
        securityContext=dict(
            runAsUser=0,
            allowPrivilegeEscalation=False,
            readOnlyRootFilesystem=True,
            capabilities={"drop": ["ALL"], "add": ["NET_ADMIN", "NET_RAW"]},
        ),
    )
    args = [
        "-transport",
        "v2",
        "-server",
        "https://regente-server:8443",
        "-id",
        IDENTITY,
        "-env",
        "kubernetes-lab",
        "-caps",
        "HTTP,REST",
        "-journal",
        "/state/journal.db",
        "-token-file",
        "/private/token",
        "-tls-cert",
        "/private/http-client.crt",
        "-tls-key",
        "/private/http-client.key",
        "-tls-ca",
        "/private/ca.crt",
        "-execution-policy",
        "/private/policy.json",
        "-job-secrets-file",
        "/private/secrets.json",
        "-concurrency",
        "1",
    ]
    return dict(
        apiVersion="apps/v1",
        kind="StatefulSet",
        metadata=dict(name=NAME, namespace=lab.NS),
        spec=dict(
            serviceName=NAME,
            replicas=1,
            updateStrategy={"type": "OnDelete"},
            selector={"matchLabels": {"app": NAME}},
            template=dict(
                metadata={"labels": {"app": NAME}},
                spec=dict(
                    automountServiceAccountToken=False,
                    terminationGracePeriodSeconds=30,
                    securityContext={"seccompProfile": {"type": "RuntimeDefault"}},
                    initContainers=[private_init, firewall],
                    containers=[
                        dict(
                            name="agent",
                            image=lab.AGENT,
                            imagePullPolicy="Never",
                            args=args,
                            securityContext=SC,
                            resources=LIMITS,
                            env=[dict(name="SSL_CERT_FILE", value="/private/ca.crt")],
                            volumeMounts=[
                                mount("private", "/private", True),
                                mount("journal", "/state"),
                                mount("tmp", "/tmp"),
                            ],
                        )
                    ],
                    volumes=[
                        dict(
                            name="source",
                            secret={
                                "secretName": "http-cell-files",
                                "defaultMode": 256,
                            },
                        ),
                        dict(
                            name="private",
                            emptyDir={"medium": "Memory", "sizeLimit": "4Mi"},
                        ),
                        dict(
                            name="tmp",
                            emptyDir={"medium": "Memory", "sizeLimit": "16Mi"},
                        ),
                    ],
                ),
            ),
            volumeClaimTemplates=[
                dict(
                    metadata={"name": "journal"},
                    spec=dict(
                        accessModes=["ReadWriteOnce"],
                        storageClassName="standard",
                        resources={"requests": {"storage": "1Gi"}},
                    ),
                )
            ],
        ),
    )


def deploy():
    if (lab.STATE / "http-cell-issued.json").exists():
        raise RuntimeError(
            "Identity already issued: preserve journal and use existing resources"
        )
    lab.login()
    canary = "Bearer " + secrets.token_urlsafe(24)
    (lab.STATE / "http-authorization").write_text(canary)
    secret("fixture-auth", {"authorization": canary.encode()})
    fixture()
    fixture_ip = service("http-fixture")
    server_ip = json.loads(
        lab.kube("-n", lab.NS, "get", "service", "regente-server", "-o", "json")
    )["spec"]["clusterIP"]
    dns_ip = json.loads(
        lab.kube("-n", "kube-system", "get", "service", "kube-dns", "-o", "json")
    )["spec"]["clusterIP"]
    cert = x509.load_pem_x509_certificate((lab.STATE / "http-client.crt").read_bytes())
    issued = lab.api(
        "/api/agents/tokens",
        dict(
            label="Kubernetes HTTP cell",
            agentId=IDENTITY,
            environment="kubernetes-lab",
            capabilities=["HTTP", "REST", "EXECUTION_V2"],
            certificateSHA256=cert.fingerprint(hashes.SHA256()).hex(),
            expiresAt=(
                dt.datetime.now(dt.timezone.utc) + dt.timedelta(days=1)
            ).isoformat(),
        ),
    )
    (lab.STATE / "http-cell-issued.json").write_text(json.dumps(issued))
    names = (
        "http-ok",
        "http-rotated",
        "http-slow",
        "deny-host",
        "deny-port",
        "deny-cidr",
        "deny-metadata",
    )
    policy = dict(
        environment="kubernetes-lab",
        jobs={
            name: dict(
                types=["HTTP"],
                secrets=["fixture.authorization"],
                destinations=[
                    dict(
                        host="http-fixture", port="8443", networks=[fixture_ip + "/32"]
                    )
                ],
            )
            for name in names
        },
    )
    policy["jobs"]["deny-cidr"]["destinations"][0]["networks"] = ["192.0.2.1/32"]
    files = {
        f: (lab.STATE / f).read_bytes()
        for f in ("http-client.crt", "http-client.key", "ca.crt")
    }
    files.update(
        {
            "token": issued["token"].encode(),
            "policy.json": json.dumps(policy).encode(),
            "secrets.json": json.dumps(
                {"values": {"fixture.authorization": canary}}
            ).encode(),
        }
    )
    secret("http-cell-files", files)
    lab.apply(
        lab.resource(
            "Service",
            NAME,
            spec=dict(
                clusterIP="None", selector={"app": NAME}, ports=[dict(port=8443)]
            ),
        )
    )
    obj = manifest(server_ip, fixture_ip, dns_ip)
    (lab.STATE / "http-cell-manifest.json").write_text(json.dumps(obj, indent=2))
    lab.apply(obj)
    print("HTTP cell provisioned; credentials not printed")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["prepare", "deploy"])
    action = parser.parse_args().action
    prepare() if action == "prepare" else deploy()
