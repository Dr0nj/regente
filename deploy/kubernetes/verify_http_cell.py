"""Bounded phase02 qualification, once per new cell; no capacity soak."""

import argparse
import json
import secrets
import lab
import http_cell as cell
from verify import wait


def fixture_exec(*args):
    return lab.kube("-n", lab.NS, "exec", "deployment/http-fixture", "--", *args)


def ledger():
    raw = fixture_exec(
        "node",
        "-e",
        'const fs=require("fs");process.stdout.write(fs.existsSync("/data/ledger")?fs.readFileSync("/data/ledger"):"")',
    )
    return [json.loads(line) for line in raw.decode().splitlines()]


def define(name, path="/ok", url=None, kind="HTTP", agent=None):
    lab.api(
        "/api/definitions",
        dict(
            id=name,
            label=name,
            team="lab",
            jobType=kind,
            agentId=agent or cell.IDENTITY,
            environment="kubernetes-lab",
            timeout=90,
            schedule={"enabled": False},
            actionConfig=(
                dict(command="echo phase02-external")
                if kind == "COMMAND"
                else dict(
                    url=url or "https://http-fixture:8443" + path,
                    method="POST",
                    headers={"Authorization": {"secretRef": "fixture.authorization"}},
                )
            ),
        ),
    )


def row(i):
    return lab.api("/api/instances/" + i)


def attempt(i):
    return lab.api("/api/instances/" + i + "/executions")["attempts"][0]


def pod():
    return json.loads(lab.kube("-n", lab.NS, "get", "pod", cell.POD, "-o", "json"))


def online():
    return any(a["id"] == cell.IDENTITY and a["online"] for a in lab.api("/api/agents"))


def replacement(uid):
    def ready():
        try:
            p = pod()
            return (
                p["metadata"]["uid"] != uid
                and all(c["ready"] for c in p["status"].get("containerStatuses", []))
                and bool(p["status"].get("containerStatuses"))
                and online()
            )
        except RuntimeError:
            return False

    return wait("replacement pod and same identity", ready, 120)


def crash_check(report, save):
    previous = sum(e["path"] == "/slow" for e in ledger())
    finished_before = (
        fixture_exec("cat", "/data/completed").decode().splitlines().count("/slow")
    )
    i = lab.api("/api/definitions/http-slow/force", {})["instanceId"]
    report["orders"]["http-crash"] = i
    save()
    wait(
        "independent mutating effect witness",
        lambda: sum(e["path"] == "/slow" for e in ledger()) == previous + 1,
    )
    execution = attempt(i)["executionId"]
    p = pod()
    uid = p["metadata"]["uid"]
    container_id = p["status"]["containerStatuses"][0]["containerID"].removeprefix(
        "containerd://"
    )
    # Signal namespace PID1 from the node runtime; signals from inside PID namespace
    # can be ignored. Only this synthetic agent container is targeted.
    lab.run(
        [
            "docker",
            "exec",
            p["spec"]["nodeName"],
            "ctr",
            "-n",
            "k8s.io",
            "tasks",
            "kill",
            "--signal",
            "SIGKILL",
            container_id,
        ]
    )
    lab.kube("-n", lab.NS, "delete", "pod", cell.POD, "--wait=true", "--timeout=60s")
    replacement(uid)
    wait(
        "durable uncertainty after pod loss", lambda: attempt(i)["state"] == "uncertain"
    )
    wait(
        "fixture request completed independently",
        lambda: fixture_exec("cat", "/data/completed")
        .decode()
        .splitlines()
        .count("/slow")
        == finished_before + 1,
    )
    assert attempt(i)["executionId"] == execution
    assert len(lab.api("/api/instances/" + i + "/executions")["attempts"]) == 1
    assert sum(e["path"] == "/slow" for e in ledger()) == previous + 1
    report["checks"]["pod_loss_mutation_uncertain_single_effect_same_execution"] = True
    a = attempt(i)
    lab.api(
        "/api/executions/" + execution + "/resolve",
        dict(
            fence=a["fence"],
            idempotencyKey="phase02-observed-effect",
            decision="failed",
            effectStopped=True,
            reason="Synthetic TLS fixture: one durable effect, request completed, old pod terminated; no replay authorized",
        ),
    )
    report["checks"]["uncertainty_resolved_after_independent_inspection"] = True


def verify(resume_crash=False):
    output = lab.STATE / "http-cell-verification.json"
    if output.exists() and not resume_crash:
        raise RuntimeError(
            "Evidence exists: inspect before any repeat, never silently replay effects"
        )
    report = dict(status="running", checks={}, orders={})

    def save():
        output.write_text(json.dumps(report, indent=2))

    def order(name):
        i = lab.api("/api/definitions/" + name + "/force", {})["instanceId"]
        report["orders"][name] = i
        save()
        return i

    if resume_crash:
        previous = json.loads(output.read_text())
        assert previous["status"] == "failed"
        assert previous["checks"]["drained_secret_rotation_same_pvc_journal"]
        archive = lab.STATE / "http-cell-graceful-interruption.json"
        if archive.exists():
            raise RuntimeError("Resume already attempted; inspect effects manually")
        archive.write_text(output.read_text())
        report = previous
        report["status"] = "running"
        report.pop("errorType", None)
        report["priorHarnessFailure"] = (
            "In-pod STOP did not enforce crash; graceful interruption returned failed. Original report retained."
        )
        lab.login()
        try:
            assert attempt(report["orders"]["http-slow"])["state"] == "failed"
            assert not any(r["status"] == "RUNNING" for r in lab.api("/api/instances"))
            crash_check(report, save)
            report["status"] = "passed"
        except Exception as exc:
            report["status"] = "failed"
            report["errorType"] = type(exc).__name__
            raise
        finally:
            save()
        return
    save()
    try:
        wait("backend ready", lambda: lab.api("/readyz")["ready"], 120)
        lab.login()
        wait("HTTP cell online", online, 120)
        wait(
            "external agent online",
            lambda: any(
                a["id"] == lab.WORKER and a["online"] for a in lab.api("/api/agents")
            ),
        )
        assert not ledger(), "Fresh fixture required"
        p = pod()
        sc = p["spec"]["containers"][0]["securityContext"]
        assert (
            sc["runAsUser"] == 10001
            and sc["readOnlyRootFilesystem"]
            and sc["capabilities"]["drop"] == ["ALL"]
        )
        status = cell.podexec("cat", "/proc/1/status").decode()
        assert "CapEff:\t0000000000000000" in status and "NoNewPrivs:\t1" in status
        assert (
            cell.podexec(
                "sh",
                "-ec",
                'test ! -e /var/run/secrets/kubernetes.io/serviceaccount/token; stat -c "%a" /private/secrets.json',
            )
            .decode()
            .strip()
            == "600"
        )
        report["checks"]["nonroot_readonly_no_caps_no_sa_regular0600"] = True
        define("http-ok")
        define("external-coexist", kind="COMMAND", agent=lab.WORKER)
        h, c = order("http-ok"), order("external-coexist")
        wait(
            "HTTP and external COMMAND",
            lambda: row(h)["status"] == row(c)["status"] == "OK",
        )
        canary = (lab.STATE / "http-authorization").read_text()
        assert canary not in json.dumps(row(h)) and "[REDACTED]" in row(h)["output"]
        assert row(h)["agentId"] == cell.IDENTITY and row(c)["agentId"] == lab.WORKER
        assert len(ledger()) == 1
        report["checks"]["tls_service_dns_redaction_and_coexistence"] = True
        forbidden = json.loads(
            lab.kube("-n", lab.NS, "get", "svc", "forbidden-fixture", "-o", "json")
        )["spec"]["clusterIP"]
        # A live negative control: succeeds from another pod, fails from agent netns even bypassing local policy.
        fixture_exec(
            "node",
            "-e",
            f'const s=require("net").connect(8443,"{forbidden}");s.on("connect",()=>process.exit(0));s.on("error",()=>process.exit(1));setTimeout(()=>process.exit(2),3000)',
        )
        cell.podexec("sh", "-ec", f"if nc -z -w 3 {forbidden} 8443; then exit 1; fi")
        cell.podexec("sh", "-ec", "nc -z -w 3 http-fixture 8443")
        report["checks"]["kernel_egress_live_negative_control"] = True
        for name, url in [
            ("deny-host", "https://forbidden-fixture:8443/denied"),
            ("deny-port", "https://http-fixture:8444/denied"),
            ("deny-cidr", "https://http-fixture:8443/denied"),
            ("deny-metadata", "http://169.254.169.254/latest/meta-data/"),
        ]:
            define(name, url=url)
            i = order(name)
            wait(name, lambda: row(i)["status"] == "NOTOK")
            assert "denied" in row(i)["output"].lower()
        assert len(ledger()) == 1
        report["checks"]["host_port_cidr_metadata_denied_before_effect"] = True
        save()
        # Drain (all results confirmed), rotate source Secret, and remount regular files on replacement.
        assert not any(r["status"] == "RUNNING" for r in lab.api("/api/instances"))
        uid = pod()["metadata"]["uid"]
        journal = (
            cell.podexec(
                "sh", "-ec", 'test -s /state/journal.db; stat -c "%i" /state/journal.db'
            )
            .decode()
            .strip()
        )
        new_canary = "Bearer " + secrets.token_urlsafe(24)
        cell.secret("fixture-auth", {"authorization": new_canary.encode()})
        encoded = (
            __import__("base64")
            .b64encode(
                json.dumps({"values": {"fixture.authorization": new_canary}}).encode()
            )
            .decode()
        )
        patch = lab.STATE / "secret-rotation-patch.json"
        patch.write_text(json.dumps({"data": {"secrets.json": encoded}}))
        try:
            lab.kube(
                "-n",
                lab.NS,
                "patch",
                "secret",
                "http-cell-files",
                "--type=merge",
                "--patch-file=" + str(patch),
            )
        finally:
            patch.unlink()
        (lab.STATE / "http-authorization").write_text(new_canary)
        wait(
            "fixture projected secret updated",
            lambda: fixture_exec("cat", "/auth/authorization").decode() == new_canary,
            120,
        )
        lab.kube(
            "-n", lab.NS, "delete", "pod", cell.POD, "--wait=true", "--timeout=60s"
        )
        replacement(uid)
        assert (
            cell.podexec("stat", "-c", "%i", "/state/journal.db").decode().strip()
            == journal
        )
        define("http-rotated", "/rotated")
        i = order("http-rotated")
        wait("rotated secret", lambda: row(i)["status"] == "OK")
        assert new_canary not in json.dumps(row(i)) and "[REDACTED]" in row(i)["output"]
        assert len(ledger()) == 2
        report["checks"]["drained_secret_rotation_same_pvc_journal"] = True
        save()
        define("http-slow", "/slow")
        crash_check(report, save)
        assert not any(r["status"] == "RUNNING" for r in lab.api("/api/instances"))
        report["status"] = "passed"
    except Exception as exc:
        report["status"] = "failed"
        report["errorType"] = type(exc).__name__
        raise
    finally:
        save()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--resume-crash",
        action="store_true",
        help="Resume only the recorded graceful-interruption harness failure",
    )
    verify(parser.parse_args().resume_crash)
