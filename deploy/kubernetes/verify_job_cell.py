"""Resumable phase05 evidence: each named order is submitted at most once."""

import argparse
import json
import time
import subprocess
import job_cell as cell
import lab
from verify import wait

REPORT = lab.STATE / "job-cell-verification.json"


def load():
    return (
        json.loads(REPORT.read_text())
        if REPORT.exists()
        else dict(orders={}, checks={})
    )


def save(report):
    REPORT.write_text(json.dumps(report, indent=2))


def row(i):
    return lab.api("/api/instances/" + i)


def attempt(i):
    return lab.api("/api/instances/" + i + "/executions")["attempts"][0]


def objects(kind):
    return json.loads(lab.kube("-n", cell.JOBS, "get", kind, "-o", "json"))["items"]


def order(name, timeout=90, override=None, retries=0):
    report = load()
    if name in report["orders"]:
        return report["orders"][name]
    config = json.loads((lab.STATE / "workload-config.json").read_text())
    lab.api(
        "/api/definitions",
        dict(
            id=name,
            label=name,
            team="lab",
            jobType="K8S",
            agentId=cell.cell.IDENTITY,
            environment="kubernetes-lab",
            timeout=timeout,
            retries=retries,
            schedule={"enabled": False},
            actionConfig=override
            or dict(
                image=config["image"],
                command=config["commands"][name],
                namespace=cell.JOBS,
            ),
        ),
    )
    i = lab.api("/api/definitions/" + name + "/force", {})["instanceId"]
    report["orders"][name] = i
    save(report)
    return i


def terminal(i):
    wait(
        "terminal execution",
        lambda: row(i)["status"] in ("OK", "NOTOK", "CANCELLED", "UNCERTAIN"),
        160,
    )
    return row(i)


def basic():
    for name, status, output in [
        ("pod-ok", "OK", "10001"),
        ("pod-fail", "NOTOK", "pod-failure"),
        ("pod-boundary", "OK", "isolated"),
    ]:
        i = order(name)
        r = terminal(i)
        report = load()
        report.setdefault("results", {})[name] = dict(
            status=r["status"], output=r.get("output"), attempt=attempt(i)
        )
        save(report)
        assert r["status"] == status, r
        assert output in r["output"], r
        report["checks"][name] = True
        save(report)
    print(json.dumps(load(), indent=2))


def execution_objects(i):
    attempts = lab.api("/api/instances/" + i + "/executions")["attempts"]
    if not attempts:
        return None
    execution = attempts[0]["executionId"]
    jobs = [
        j
        for j in objects("jobs")
        if j["metadata"]["annotations"].get("regente.io/execution") == execution
    ]
    if len(jobs) != 1:
        return None
    job = jobs[0]
    pods = [
        p
        for p in objects("pods")
        if any(
            o.get("controller") and o["uid"] == job["metadata"]["uid"]
            for o in p["metadata"].get("ownerReferences", [])
        )
    ]
    if len(pods) != 1:
        return None
    return job, pods[0]


def started(i):
    def ready():
        pair = execution_objects(i)
        if not pair:
            return None
        job, pod = pair
        if pod["status"]["phase"] != "Running":
            return None
        logs = lab.kube(
            "-n", cell.JOBS, "logs", pod["metadata"]["name"], "-c", "main"
        ).decode()
        return pair if "effect-" in logs else None

    return wait("independent process start witness", ready, 60)


def record(name, i, expected):
    result = terminal(i)
    report = load()
    report.setdefault("results", {})[name] = dict(
        status=result["status"], output=result.get("output"), attempt=attempt(i)
    )
    save(report)
    assert result["status"] == expected, result
    assert len(lab.api("/api/instances/" + i + "/executions")["attempts"]) == 1
    report["checks"][name] = True
    save(report)


def faults(resume=False):
    report = load()
    if report.get("faultsStarted") and not resume:
        raise RuntimeError(
            "Fault sequence already started; inspect persisted evidence before resuming"
        )
    report["faultsStarted"] = True
    save(report)
    i = order("pod-restart")
    job, pod = started(i)
    controller = json.loads(
        lab.kube("-n", lab.NS, "get", "pod", cell.cell.POD, "-o", "json")
    )
    container = controller["status"]["containerStatuses"][0][
        "containerID"
    ].removeprefix("containerd://")
    lab.run(
        [
            "docker",
            "exec",
            cell.CONTROL,
            "ctr",
            "-n",
            "k8s.io",
            "tasks",
            "kill",
            "--signal",
            "SIGKILL",
            container,
        ]
    )
    lab.kube(
        "-n", lab.NS, "delete", "pod", cell.cell.POD, "--wait=true", "--timeout=60s"
    )
    record("pod-restart", i, "OK")
    now = execution_objects(i)
    assert (
        now[0]["metadata"]["uid"] == job["metadata"]["uid"]
        and now[1]["metadata"]["uid"] == pod["metadata"]["uid"]
    )
    report = load()
    report["checks"]["controller_restart_same_job_pod"] = True
    save(report)
    i = order("pod-cancel")
    started(i)
    lab.api("/api/instances/" + i + "/cancel", {})
    record("pod-cancel", i, "NOTOK")
    assert attempt(i)["state"] == "cancelled"
    access_and_deadline()


def access_and_deadline():
    i = order("pod-timeout", timeout=10)
    record("pod-timeout", i, "NOTOK")
    i = order("api-outage")
    job, pod = started(i)
    binding = json.loads(
        lab.kube("-n", cell.JOBS, "get", "rolebinding", "job-controller", "-o", "json")
    )
    for k in ("resourceVersion", "uid", "creationTimestamp", "managedFields"):
        binding["metadata"].pop(k, None)
    lab.kube("-n", cell.JOBS, "delete", "rolebinding", "job-controller")
    try:
        time.sleep(8)
        assert attempt(i)["state"] == "running"
    finally:
        lab.apply(binding)
    record("api-outage", i, "OK")
    now = execution_objects(i)
    assert (
        now[0]["metadata"]["uid"] == job["metadata"]["uid"]
        and now[1]["metadata"]["uid"] == pod["metadata"]["uid"]
    )
    report = load()
    report["checks"]["api_access_restored_without_resubmission"] = True
    save(report)
    print(json.dumps(load()["checks"], indent=2))


def resolve_observed(i):
    a = attempt(i)
    job = [
        j
        for j in objects("jobs")
        if j["metadata"]["annotations"].get("regente.io/execution") == a["executionId"]
    ]
    assert len(job) == 1
    name = job[0]["metadata"]["name"]

    def stopped():
        containers = json.loads(
            lab.run(["docker", "exec", cell.NODE, "crictl", "ps", "-a", "-o", "json"])
        )["containers"]
        return not any(
            c.get("labels", {}).get("io.kubernetes.pod.name", "").startswith(name)
            and c["state"] == "CONTAINER_RUNNING"
            for c in containers
        )

    wait("workload process absent on reachable worker", stopped, 120)
    assert attempt(i)["executionId"] == a["executionId"]
    lab.api(
        "/api/executions/" + a["executionId"] + "/resolve",
        dict(
            fence=a["fence"],
            idempotencyKey="k05-inspected-" + a["executionId"],
            decision="failed",
            effectStopped=True,
            reason="Synthetic fault: worker reachable, no running workload container for the original Job; inspected before resolution, no replay",
        ),
    )


def loss():
    report = load()
    if report.get("lossStarted"):
        raise RuntimeError("Loss faults already started; inspect evidence")
    report["lossStarted"] = True
    save(report)
    i = order("pod-loss")
    job, pod = started(i)
    lab.kube(
        "-n",
        cell.JOBS,
        "delete",
        "pod",
        pod["metadata"]["name"],
        "--wait=true",
        "--timeout=60s",
    )
    result = terminal(i)
    assert result["status"] in ("UNCERTAIN", "NOTOK"), result
    report = load()
    report.setdefault("results", {})["pod-loss"] = dict(
        status=result["status"], attempt=attempt(i)
    )
    save(report)
    if result["status"] == "UNCERTAIN":
        resolve_observed(i)
    assert (
        len(
            [
                j
                for j in objects("jobs")
                if j["metadata"]["annotations"].get("regente.io/execution")
                == attempt(i)["executionId"]
            ]
        )
        == 1
    )
    report = load()
    report["checks"]["pod_loss_no_job_recreation"] = True
    save(report)
    i = order("node-loss", timeout=35)
    job, pod = started(i)
    assert pod["spec"]["nodeName"] == cell.NODE
    lab.run(["docker", "pause", cell.NODE])
    try:
        wait(
            "node unavailable observation becomes uncertain",
            lambda: attempt(i)["state"] == "uncertain",
            140,
        )
        report = load()
        report.setdefault("results", {})["node-loss"] = dict(
            status=row(i)["status"], attempt=attempt(i)
        )
        save(report)
    finally:
        lab.run(["docker", "unpause", cell.NODE])
    resolve_observed(i)
    assert (
        len(
            [
                j
                for j in objects("jobs")
                if j["metadata"]["annotations"].get("regente.io/execution")
                == attempt(i)["executionId"]
            ]
        )
        == 1
    )
    report = load()
    report["checks"]["node_unavailable_uncertain_no_job_recreation"] = True
    save(report)
    print(json.dumps(load()["checks"], indent=2))


def permissions():
    report = load()
    if report.get("permissionsStarted"):
        raise RuntimeError("Permission sequence already started; inspect evidence")
    report["permissionsStarted"] = True
    save(report)
    before = len(objects("jobs"))
    config = json.loads((lab.STATE / "workload-config.json").read_text())
    i = order(
        "pod-policy",
        override=dict(
            image=config["image"],
            command="echo outside-the-allowlist",
            namespace=cell.JOBS,
        ),
    )
    record("pod-policy", i, "NOTOK")
    assert len(objects("jobs")) == before
    binding = json.loads(
        lab.kube("-n", cell.JOBS, "get", "rolebinding", "job-controller", "-o", "json")
    )
    for k in ("resourceVersion", "uid", "creationTimestamp", "managedFields"):
        binding["metadata"].pop(k, None)
    lab.kube("-n", cell.JOBS, "delete", "rolebinding", "job-controller")
    try:
        i = order("pod-revoked")
        record("pod-revoked", i, "NOTOK")
        assert "403" in row(i)["output"]
        assert len(objects("jobs")) == before
    finally:
        lab.apply(binding)
    identity = "system:serviceaccount:" + lab.NS + ":job-controller"
    for resource in ("secrets", "pods/exec", "serviceaccounts/token"):
        result = subprocess.run(
            [
                "kubectl",
                "--context",
                lab.CONTEXT,
                "auth",
                "can-i",
                "create" if resource != "secrets" else "get",
                resource,
                "-n",
                cell.JOBS,
                "--as=" + identity,
            ],
            capture_output=True,
            timeout=30,
        )
        assert (
            result.returncode == 1 and result.stdout.decode().strip() == "no"
        ), resource
    report = load()
    report["checks"]["policy_and_rbac_denied_without_job"] = True
    save(report)
    print(json.dumps(load()["checks"], indent=2))


def final_binary():
    report = load()
    if report.get("finalBinaryStarted"):
        raise RuntimeError("Final binary check already started; inspect evidence")
    report["finalBinaryStarted"] = True
    for name in ("pod-ok", "pod-fail"):
        report.setdefault("previousValidatedOrders", {})[
            name + "-before-final-review"
        ] = report["orders"].pop(name)
    save(report)
    i = order("pod-ok")
    record("pod-ok", i, "OK")
    i = order("pod-fail", retries=1)

    def retried():
        attempts = lab.api("/api/instances/" + i + "/executions")["attempts"]
        return (
            attempts
            if len(attempts) == 2 and all(a["state"] == "failed" for a in attempts)
            else None
        )

    attempts = wait("one configured retry with two known failures", retried, 120)
    assert len({a["executionId"] for a in attempts}) == 2
    jobs = [
        j
        for j in objects("jobs")
        if j["metadata"]["annotations"].get("regente.io/execution")
        in {a["executionId"] for a in attempts}
    ]
    assert len(jobs) == 2 and len({j["metadata"]["uid"] for j in jobs}) == 2
    report = load()
    report["checks"]["configured_retry_distinct_execution_and_job"] = True
    report.setdefault("results", {})["configured-retry"] = dict(
        order=i, attempts=attempts, status=row(i)["status"]
    )
    report["checks"]["final_binary_success"] = True
    save(report)
    print(json.dumps(report["checks"], indent=2))


if __name__ == "__main__":
    p = argparse.ArgumentParser()
    p.add_argument(
        "action", choices=["basic", "faults", "loss", "permissions", "final", "status"]
    )
    a = p.parse_args()
    lab.login()
    if a.action == "basic":
        basic()
    elif a.action == "faults":
        faults()
    elif a.action == "loss":
        loss()
    elif a.action == "permissions":
        permissions()
    elif a.action == "final":
        final_binary()
    else:
        print(json.dumps({n: row(i) for n, i in load()["orders"].items()}, indent=2))
