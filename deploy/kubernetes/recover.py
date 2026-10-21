"""Quiesced backup and isolated restore rehearsal; leaves source service running.
The restore server has no NodePort or attached agent. Never reuse its database
with the source agent to retry effects that happened after the backup.
"""

import hashlib, json, shutil, subprocess, time
import lab
from verify import wait


def archiver(ns):
    obj = lab.resource(
        "Pod",
        "archive-helper",
        spec=dict(
            restartPolicy="Never",
            automountServiceAccountToken=False,
            containers=[
                dict(
                    name="archive",
                    image=lab.AGENT,
                    imagePullPolicy="Never",
                    command=["/bin/sh", "-c", "sleep 600"],
                    securityContext=dict(runAsUser=0, allowPrivilegeEscalation=False),
                    volumeMounts=[dict(name="data", mountPath="/data")],
                )
            ],
            volumes=[
                dict(name="data", persistentVolumeClaim={"claimName": "server-data"})
            ],
        ),
    )
    lab.apply(obj)
    wait(
        "archive helper",
        lambda: json.loads(
            lab.kube("-n", ns, "get", "pod", "archive-helper", "-o", "json")
        )
        .get("status", {})
        .get("phase")
        == "Running",
    )


def recover():
    backup = lab.STATE / "backup"
    backup.mkdir(exist_ok=False)
    source = lab.NS
    lab.login()
    instances = lab.api("/api/instances")
    if any(r.get("status") == "RUNNING" for r in instances):
        raise RuntimeError("Drain running executions before the backup rehearsal")
    expected = len(instances)
    try:
        lab.kube("-n", source, "scale", "deployment/regente-server", "--replicas=0")
        lab.run(["docker", "stop", "--time", "10", lab.WORKER])
        wait(
            "source stopped",
            lambda: not json.loads(
                lab.kube(
                    "-n", source, "get", "pod", "-l", "app=regente-server", "-o", "json"
                )
            )["items"],
        )
        archiver(source)
        (backup / "data.tar").write_bytes(
            lab.kube(
                "-n",
                source,
                "exec",
                "archive-helper",
                "--",
                "tar",
                "cf",
                "-",
                "-C",
                "/data",
                ".",
            )
        )
        (backup / "postgres.dump").write_bytes(
            lab.kube(
                "-n",
                source,
                "exec",
                "deployment/postgres",
                "--",
                "pg_dump",
                "-U",
                "regente",
                "-d",
                "regente",
                "-Fc",
            )
        )
        (backup / "journal.tar").write_bytes(
            lab.run(
                [
                    "docker",
                    "run",
                    "--rm",
                    "--user",
                    "0",
                    "--entrypoint",
                    "tar",
                    "-v",
                    "regente-kubernetes-journal:/state:ro",
                    lab.AGENT,
                    "cf",
                    "-",
                    "-C",
                    "/state",
                    ".",
                ]
            )
        )
        for f in (
            "ca.crt",
            "server.crt",
            "server.key",
            "client.crt",
            "client.key",
            "audit.key",
            "private.json",
            "token",
            "manifest.json",
        ):
            shutil.copyfile(lab.STATE / f, backup / f)
        lab.kube("-n", source, "delete", "pod", "archive-helper", "--wait=true")
    finally:
        try:
            lab.kube(
                "-n",
                source,
                "delete",
                "pod",
                "archive-helper",
                "--ignore-not-found",
                "--wait=true",
            )
        finally:
            try:
                lab.kube(
                    "-n", source, "scale", "deployment/regente-server", "--replicas=1"
                )
            finally:
                lab.run(["docker", "start", lab.WORKER])
    start = time.monotonic()
    restored = source + "-restore"
    lab.NS = restored
    lab.apply(lab.resource("Namespace", restored))
    for kind, name in [
        ("Secret", "lab-env"),
        ("Secret", "server-files"),
        ("ConfigMap", "lab-fixtures"),
    ]:
        obj = json.loads(lab.kube("-n", source, "get", kind, name, "-o", "json"))
        obj["metadata"] = {"name": name, "namespace": restored}
        obj.pop("status", None)
        lab.apply(obj)
    obj = lab.manifests()
    for item in obj["items"]:
        if (
            item["kind"] == "Deployment"
            and item["metadata"]["name"] == "regente-server"
        ):
            item["spec"]["replicas"] = 0
        if item["kind"] == "Service" and item["metadata"]["name"] == "regente-server":
            item["spec"]["type"] = "ClusterIP"
            for port in item["spec"]["ports"]:
                port.pop("nodePort", None)
    lab.apply(obj)
    wait(
        "restore PostgreSQL",
        lambda: json.loads(
            lab.kube("-n", restored, "get", "deployment", "postgres", "-o", "json")
        )
        .get("status", {})
        .get("readyReplicas", 0)
        == 1,
    )
    lab.kube(
        "-n",
        restored,
        "exec",
        "-i",
        "deployment/postgres",
        "--",
        "pg_restore",
        "-U",
        "regente",
        "-d",
        "regente",
        "--no-owner",
        data=(backup / "postgres.dump").read_bytes(),
    )
    archiver(restored)
    lab.kube(
        "-n",
        restored,
        "exec",
        "-i",
        "archive-helper",
        "--",
        "tar",
        "xf",
        "-",
        "-C",
        "/data",
        data=(backup / "data.tar").read_bytes(),
    )
    lab.kube("-n", restored, "delete", "pod", "archive-helper", "--wait=true")
    lab.kube("-n", restored, "scale", "deployment/regente-server", "--replicas=1")
    wait(
        "restored server",
        lambda: json.loads(
            lab.kube(
                "-n", restored, "get", "deployment", "regente-server", "-o", "json"
            )
        )
        .get("status", {})
        .get("readyReplicas", 0)
        == 1,
    )
    log = (lab.STATE / "restore-forward.log").open("wb")
    forward = subprocess.Popen(
        [
            "kubectl",
            "--context",
            lab.CONTEXT,
            "-n",
            restored,
            "port-forward",
            "--address=127.0.0.1",
            "service/regente-server",
            "8843:8443",
        ],
        stdout=log,
        stderr=log,
    )
    try:
        lab.BASE = "https://127.0.0.1:8843"
        lab.TOKEN = None
        wait("restored HTTPS", lambda: lab.api("/readyz")["ready"])
        lab.login()
        actual = len(lab.api("/api/instances"))
        assert actual == expected
        report = {
            "status": "passed",
            "restoredInstanceCount": actual,
            "expectedInstanceCount": expected,
            "seconds": round(time.monotonic() - start, 3),
            "sourceNamespace": source,
            "restoreNamespace": restored,
            "agentAttachedToRestore": False,
            "schemaBinary": "32 / kubernetes-01-9311778",
            "sha256": {
                f.name: hashlib.sha256(f.read_bytes()).hexdigest()
                for f in backup.iterdir()
                if f.is_file()
            },
        }
        assert report["seconds"] <= 600
        (lab.STATE / "recovery.json").write_text(json.dumps(report, indent=2))
        print(json.dumps({k: v for k, v in report.items() if k != "sha256"}))
    finally:
        forward.terminate()
        forward.wait(timeout=10)
        log.close()
        lab.kube("-n", restored, "scale", "deployment/regente-server", "--replicas=0")
        lab.NS = source
        lab.BASE = "https://127.0.0.1:8443"
        lab.TOKEN = None


if __name__ == "__main__":
    recover()
