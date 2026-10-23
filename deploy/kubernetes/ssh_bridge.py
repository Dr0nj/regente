"""Isolated SSH bridge lab. Synthetic external sshd has no Regente agent."""

import argparse
import datetime as dt
import json
from pathlib import Path
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ed25519
import lab
import http_cell as cell

lab.SERVER = "regente-kubernetes-server:phase03"
lab.AGENT = "regente-kubernetes-agent:phase03"
cell.NAME = "ssh-bridge"
cell.POD = "ssh-bridge-0"
cell.IDENTITY = lab.INSTANCE + "-ssh"
TARGET = lab.INSTANCE + "-ssh-target"
TARGET_IMAGE = "regente-kubernetes-ssh-target:phase03"
COMMANDS = {
    "ssh-ok": "sh /jobs/success.sh",
    "ssh-failure": "sh /jobs/failure.sh",
    "ssh-cancel": "sh /jobs/slow.sh cancel",
    "ssh-timeout": "sh /jobs/slow.sh timeout",
    "ssh-crash": "sh /jobs/slow.sh crash",
    "ssh-revoked": "sh /jobs/success.sh",
    "ssh-hostkey": "sh /jobs/success.sh",
    "ssh-denied": "sh /jobs/success.sh",
    "ssh-user-denied": "sh /jobs/success.sh",
    "ssh-command-denied": "sh /jobs/success.sh",
}


def key(path):
    private = ed25519.Ed25519PrivateKey.generate()
    path.write_bytes(
        private.private_bytes(
            serialization.Encoding.PEM,
            serialization.PrivateFormat.OpenSSH,
            serialization.NoEncryption(),
        )
    )
    return private.public_key().public_bytes(
        serialization.Encoding.OpenSSH, serialization.PublicFormat.OpenSSH
    )


def prepare():
    if lab.INSTANCE == "regente-kubernetes":
        raise RuntimeError("Select a new isolated laboratory instance")
    lab.certs(http_cell=True)
    source = lab.STATE / "ssh-source"
    source.mkdir()
    host = key(source / "host_key")
    public = key(lab.STATE / "ssh_key")
    (source / "authorized_keys").write_bytes(public + b"\n")
    (lab.STATE / "known_hosts").write_bytes(b"[ssh-target]:2222 " + host + b"\n")
    jobs = lab.STATE / "ssh-jobs"
    jobs.mkdir()
    scripts = {
        "success.sh": "echo success >> /data/effects; sync; echo remote-stdout; echo remote-stderr >&2\n",
        "failure.sh": "echo remote-failure >&2; exit 7\n",
        "slow.sh": 'trap "" HUP\necho "$1" >> /data/effects; sync; sleep 12; echo "$1" >> /data/completed; sync; echo remote-complete\n',
    }
    for name, content in scripts.items():
        (jobs / name).write_bytes(content.encode())
    lab.run(["docker", "volume", "create", TARGET + "-data"])
    lab.run(
        [
            "docker",
            "run",
            "-d",
            "--name",
            TARGET,
            "--network",
            "kind",
            "--read-only",
            "--tmpfs",
            "/run",
            "--tmpfs",
            "/keys",
            "--memory",
            "256m",
            "--cpus",
            "0.5",
            "--pids-limit",
            "64",
            "-v",
            str(source) + ":/source:ro",
            "-v",
            str(jobs) + ":/jobs:ro",
            "-v",
            TARGET + "-data:/data",
            TARGET_IMAGE,
        ]
    )
    lab.deploy()
    print("Fresh SSH target and backend created; credentials remain private")


def deploy():
    if (lab.STATE / "ssh-issued.json").exists():
        raise RuntimeError("SSH identity already exists; preserve its journal")
    lab.login()
    cert = x509.load_pem_x509_certificate((lab.STATE / "http-client.crt").read_bytes())
    issued = lab.api(
        "/api/agents/tokens",
        dict(
            label="Kubernetes SSH bridge",
            agentId=cell.IDENTITY,
            environment="kubernetes-lab",
            capabilities=["SSH_REMOTE", "EXECUTION_V2"],
            certificateSHA256=cert.fingerprint(hashes.SHA256()).hex(),
            expiresAt=(
                dt.datetime.now(dt.timezone.utc) + dt.timedelta(days=1)
            ).isoformat(),
        ),
    )
    (lab.STATE / "ssh-issued.json").write_text(json.dumps(issued))
    target_ip = json.loads(lab.run(["docker", "inspect", TARGET]))[0][
        "NetworkSettings"
    ]["Networks"]["kind"]["IPAddress"]
    policy = dict(
        environment="kubernetes-lab",
        jobs={
            name: dict(
                types=["SSH_REMOTE"],
                destinations=[
                    dict(host="ssh-target", port="2222", networks=[target_ip + "/32"])
                ],
                ssh=dict(
                    user="runner",
                    commands=[command],
                    keyPath="/private/ssh_key",
                    knownHostsPath="/private/known_hosts",
                ),
            )
            for name, command in COMMANDS.items()
        },
    )
    files = {
        f: (lab.STATE / f).read_bytes()
        for f in (
            "http-client.crt",
            "http-client.key",
            "ca.crt",
            "ssh_key",
            "known_hosts",
        )
    }
    files.update(
        {
            "token": issued["token"].encode(),
            "policy.json": json.dumps(policy).encode(),
            "secrets.json": b'{"values":{}}',
        }
    )
    cell.secret("ssh-cell-files", files)
    server_ip = json.loads(
        lab.kube("-n", lab.NS, "get", "svc", "regente-server", "-o", "json")
    )["spec"]["clusterIP"]
    dns_ip = json.loads(
        lab.kube("-n", "kube-system", "get", "svc", "kube-dns", "-o", "json")
    )["spec"]["clusterIP"]
    obj = cell.manifest(server_ip, target_ip, dns_ip)
    spec = obj["spec"]["template"]["spec"]
    spec["hostAliases"] = [dict(ip=target_ip, hostnames=["ssh-target"])]
    spec["initContainers"][0]["command"][2] = spec["initContainers"][0]["command"][
        2
    ].replace("http-agent-0", cell.POD)
    spec["initContainers"][1]["command"][2] = spec["initContainers"][1]["command"][
        2
    ].replace(
        f"-d {target_ip}/32 -p tcp --dport 8443",
        f"-d {target_ip}/32 -p tcp --dport 2222",
    )
    spec["containers"][0]["args"][
        spec["containers"][0]["args"].index("-caps") + 1
    ] = "SSH_REMOTE"
    spec["volumes"][0]["secret"]["secretName"] = "ssh-cell-files"
    lab.apply(
        lab.resource(
            "Service",
            cell.NAME,
            spec=dict(
                clusterIP="None", selector={"app": cell.NAME}, ports=[dict(port=2222)]
            ),
        )
    )
    (lab.STATE / "ssh-manifest.json").write_text(json.dumps(obj, indent=2))
    lab.apply(obj)
    print("SSH bridge provisioned with explicit identity, policy and firewall")


if __name__ == "__main__":
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument(
        "action", choices=["prepare", "deploy", "external-agent", "finalize"]
    )
    action = p.parse_args().action
    {
        "prepare": prepare,
        "deploy": deploy,
        "external-agent": lab.agent,
        "finalize": lab.finalize,
    }[action]()
