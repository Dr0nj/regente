"""Dedicated phase05 cluster. Node faults never touch previous laboratories."""

import argparse
import datetime as dt
import json
from pathlib import Path
from cryptography import x509
from cryptography.hazmat.primitives import hashes
import lab
import http_cell as cell

if lab.INSTANCE != "regente-kubernetes-phase05":
    raise ValueError("Select REGENTE_LAB_INSTANCE=regente-kubernetes-phase05")
lab.CONTEXT = "kind-regente-kubernetes-phase05"
lab.SERVER = "regente-kubernetes-server:phase03"
lab.AGENT = "regente-kubernetes-agent:phase05"
CLUSTER = "regente-kubernetes-phase05"
CONTROL = CLUSTER + "-control-plane"
NODE = CLUSTER + "-worker"
JOBS = CLUSTER + "-jobs"
cell.NAME = "job-controller"
cell.POD = "job-controller-0"
cell.IDENTITY = CLUSTER + "-controller"
COMMANDS = {
    "pod-ok": "echo pod-stdout; echo pod-stderr >&2; id -u",
    "pod-fail": "echo pod-failure >&2; exit 7",
    "pod-restart": "echo effect-controller; sleep 25; echo completed-controller",
    "pod-cancel": "echo effect-cancel; sleep 60; echo completed-cancel",
    "pod-timeout": "echo effect-timeout; sleep 60",
    "pod-loss": "echo effect-pod; sleep 60",
    "node-loss": "echo effect-node; sleep 90",
    "api-outage": "echo effect-api; sleep 25; echo completed-api",
    "pod-boundary": 'test "$(id -u)" = 10001; test ! -e /var/run/secrets/kubernetes.io/serviceaccount/token; if touch /not-allowed 2>/dev/null; then exit 2; fi; grep CapEff /proc/self/status; if nc -z -w 2 10.96.0.1 443; then exit 3; fi; echo isolated',
    "pod-revoked": "echo should-not-run-with-revoked-rbac",
    "pod-policy": "echo unapproved",
}
original_manifests = lab.manifests


def manifests():
    obj = original_manifests()
    for item in obj["items"]:
        if item["kind"] == "Deployment":
            spec = item["spec"]["template"]["spec"]
            spec["nodeSelector"] = {"kubernetes.io/hostname": CONTROL}
            spec["tolerations"] = [
                {
                    "key": "node-role.kubernetes.io/control-plane",
                    "operator": "Exists",
                    "effect": "NoSchedule",
                }
            ]
    return obj


lab.manifests = manifests


def cluster():
    existing = lab.run(["kind", "get", "clusters"]).decode().splitlines()
    if CLUSTER in existing:
        raise ValueError("Cluster already exists; preserve it")
    lab.run(
        [
            "kind",
            "create",
            "cluster",
            "--config",
            str(Path(__file__).with_name("kind-phase05.yaml")),
            "--image",
            "kindest/node:v1.36.1",
        ],
        timeout=240,
    )
    lab.run(
        [
            "kind",
            "load",
            "docker-image",
            lab.SERVER,
            lab.AGENT,
            cell.FIREWALL,
            "--name",
            CLUSTER,
        ],
        timeout=240,
    )
    digest = json.loads(lab.run(["docker", "image", "inspect", cell.FIREWALL]))[0][
        "RepoDigests"
    ][0]
    reference = (
        "docker.io/library/" + digest if "/" not in digest.split("@")[0] else digest
    )
    for node in (CONTROL, NODE):
        lab.run(
            [
                "docker",
                "exec",
                node,
                "ctr",
                "-n",
                "k8s.io",
                "images",
                "tag",
                "docker.io/library/" + cell.FIREWALL,
                reference,
            ]
        )
    print("Dedicated two-node cluster prepared with immutable workload image")


def prepare():
    lab.certs(http_cell=True)
    lab.deploy()


def scoped(kind, name, **rest):
    return dict(
        apiVersion="rbac.authorization.k8s.io/v1",
        kind=kind,
        metadata=dict(name=name, namespace=JOBS),
        **rest,
    )


def deploy():
    if (lab.STATE / "controller-issued.json").exists():
        raise ValueError("Existing identity; preserve journal")
    lab.login()
    lab.apply(lab.resource("Namespace", JOBS))
    sa = lab.resource("ServiceAccount", "executor", automountServiceAccountToken=False)
    sa["metadata"]["namespace"] = JOBS
    lab.apply(sa)
    lab.apply(
        lab.resource(
            "ServiceAccount", "job-controller", automountServiceAccountToken=False
        )
    )
    role = scoped(
        "Role",
        "job-controller",
        rules=[
            dict(
                apiGroups=["batch"],
                resources=["jobs"],
                verbs=["create", "get", "delete"],
            ),
            dict(apiGroups=[""], resources=["pods"], verbs=["get", "list", "patch"]),
            dict(apiGroups=[""], resources=["pods/log"], verbs=["get"]),
        ],
    )
    lab.apply(role)
    lab.apply(
        scoped(
            "RoleBinding",
            "job-controller",
            roleRef=dict(
                apiGroup="rbac.authorization.k8s.io", kind="Role", name="job-controller"
            ),
            subjects=[
                dict(kind="ServiceAccount", name="job-controller", namespace=lab.NS)
            ],
        )
    )
    quota = lab.resource(
        "ResourceQuota",
        "execution-budget",
        spec={
            "hard": {
                "requests.cpu": "4",
                "requests.memory": "2Gi",
                "limits.cpu": "4",
                "limits.memory": "2Gi",
                "pods": "30",
                "count/jobs.batch": "30",
            }
        },
    )
    quota["metadata"]["namespace"] = JOBS
    lab.apply(quota)
    cert = x509.load_pem_x509_certificate((lab.STATE / "http-client.crt").read_bytes())
    issued = lab.api(
        "/api/agents/tokens",
        dict(
            label="Kubernetes job controller",
            agentId=cell.IDENTITY,
            environment="kubernetes-lab",
            capabilities=["K8S", "EXECUTION_V2"],
            certificateSHA256=cert.fingerprint(hashes.SHA256()).hex(),
            expiresAt=(
                dt.datetime.now(dt.timezone.utc) + dt.timedelta(days=1)
            ).isoformat(),
        ),
    )
    (lab.STATE / "controller-issued.json").write_text(json.dumps(issued))
    digest = json.loads(lab.run(["docker", "image", "inspect", cell.FIREWALL]))[0][
        "RepoDigests"
    ][0]
    image = "docker.io/library/" + digest if "/" not in digest.split("@")[0] else digest
    api_ip = json.loads(
        lab.kube("-n", "default", "get", "service", "kubernetes", "-o", "json")
    )["spec"]["clusterIP"]
    COMMANDS["pod-boundary"] = COMMANDS["pod-boundary"].replace("10.96.0.1", api_ip)
    policy = dict(
        environment="kubernetes-lab",
        jobs={
            name: dict(
                types=["K8S_JOB", "K8S"],
                destinations=[
                    dict(
                        host="kubernetes.default.svc",
                        port="443",
                        networks=[api_ip + "/32"],
                    )
                ],
                kubernetes=dict(
                    apiServer="https://kubernetes.default.svc",
                    caFile="/kubernetes/ca.crt",
                    tokenFile="/kubernetes/token",
                    namespace=JOBS,
                    serviceAccount="executor",
                    images=[image],
                    commands=[command],
                    firewallImage=image,
                    cpu="100m",
                    memory="64Mi",
                    retentionSeconds=3600,
                    maxSeconds=120,
                    nodeSelector={"regente.io/execution": "true"},
                ),
            )
            for name, command in COMMANDS.items()
        },
    )
    files = {
        f: (lab.STATE / f).read_bytes()
        for f in ("http-client.crt", "http-client.key", "ca.crt")
    }
    files.update(
        {
            "token": issued["token"].encode(),
            "policy.json": json.dumps(policy).encode(),
            "secrets.json": b'{"values":{}}',
        }
    )
    cell.secret("http-cell-files", files)
    server_ip = json.loads(
        lab.kube("-n", lab.NS, "get", "svc", "regente-server", "-o", "json")
    )["spec"]["clusterIP"]
    dns_ip = json.loads(
        lab.kube("-n", "kube-system", "get", "svc", "kube-dns", "-o", "json")
    )["spec"]["clusterIP"]
    obj = cell.manifest(server_ip, api_ip, dns_ip)
    spec = obj["spec"]["template"]["spec"]
    spec["nodeSelector"] = {"kubernetes.io/hostname": CONTROL}
    spec["tolerations"] = [
        {
            "key": "node-role.kubernetes.io/control-plane",
            "operator": "Exists",
            "effect": "NoSchedule",
        }
    ]
    spec["serviceAccountName"] = "job-controller"
    spec["initContainers"][0]["command"][2] = spec["initContainers"][0]["command"][
        2
    ].replace("http-agent-0", cell.POD)
    spec["initContainers"][1]["command"][2] = spec["initContainers"][1]["command"][
        2
    ].replace(
        f"-d {api_ip}/32 -p tcp --dport 8443", f"-d {api_ip}/32 -p tcp --dport 443"
    )
    args = spec["containers"][0]["args"]
    args[args.index("-caps") + 1] = "K8S"
    spec["containers"][0]["volumeMounts"].append(
        dict(name="kubernetes", mountPath="/kubernetes", readOnly=True)
    )
    spec["volumes"].append(
        dict(
            name="kubernetes",
            projected=dict(
                defaultMode=292,
                sources=[
                    dict(serviceAccountToken=dict(path="token", expirationSeconds=600)),
                    dict(
                        configMap=dict(
                            name="kube-root-ca.crt",
                            items=[dict(key="ca.crt", path="ca.crt")],
                        )
                    ),
                ],
            ),
        )
    )
    lab.apply(
        lab.resource(
            "Service",
            cell.NAME,
            spec=dict(
                clusterIP="None", selector={"app": cell.NAME}, ports=[dict(port=8443)]
            ),
        )
    )
    (lab.STATE / "controller-manifest.json").write_text(json.dumps(obj, indent=2))
    (lab.STATE / "workload-config.json").write_text(
        json.dumps(dict(image=image, commands=COMMANDS), indent=2)
    )
    lab.apply(obj)
    print(
        "Controller, scoped RBAC and workload quota created; token projection rotates"
    )


if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("action", choices=["cluster", "prepare", "deploy", "finalize"])
    a = p.parse_args().action
    {
        "cluster": cluster,
        "prepare": prepare,
        "deploy": deploy,
        "finalize": lab.finalize,
    }[a]()
