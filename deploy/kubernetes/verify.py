"""Short, stateful laboratory rehearsal. Run once against a freshly deployed lab.
No capacity soak. Stores evidence without tokens or passwords.
"""

import datetime as dt, json, ssl, time, urllib.request
import lab

report = {"status": "running", "checks": {}}


def save():
    (lab.STATE / "verification.json").write_text(json.dumps(report, indent=2))


def wait(label, check, seconds=90):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        try:
            result = check()
            if result:
                return result
        except (OSError, ValueError):
            pass
        time.sleep(1)
    raise RuntimeError("Timed out: " + label)


def order(name):
    result = lab.api("/api/definitions/" + name + "/force", {})["instanceId"]
    report["checks"].setdefault("orders", {})[name] = result
    save()
    return result


def row(i):
    return lab.api("/api/instances/" + i)


def details(i):
    return lab.api("/api/instances/" + i + "/executions")


def effects():
    return lab.run(["docker", "exec", lab.WORKER, "cat", "/state/effects.log"]).decode()


def scale(n):
    lab.kube("-n", lab.NS, "scale", "deployment/regente-server", "--replicas=" + str(n))


def verify():
    wait("initial HTTPS readiness", lambda: lab.api("/readyz")["ready"], 120)
    lab.login()
    ctx = ssl.create_default_context(cafile=str(lab.STATE / "ca.crt"))
    with urllib.request.urlopen(lab.BASE, context=ctx) as r:
        assert r.status == 200 and b"<html" in r.read()
    report["checks"]["https_spa_login"] = True
    wait(
        "external agent online",
        lambda: any(
            a["id"] == lab.WORKER and a["online"] for a in lab.api("/api/agents")
        ),
    )
    report["checks"]["external_agent_mtls_v2"] = True
    # One earlier single-order smoke is retained as the first of the ten.
    first = lab.STATE / "first-order.json"
    ids = (
        [json.loads(first.read_text())["instanceId"]]
        if first.exists()
        else [order("success-00")]
    )
    for n in range(1, 10):
        ids.append(order(f"success-{n:02}"))
    rows = wait(
        "ten successful commands",
        lambda: (
            [row(i) for i in ids]
            if all(row(i)["status"] == "OK" for i in ids)
            else None
        ),
    )
    content = effects()
    assert all(content.count(f"effect-{i:02}") == 1 for i in range(10))
    assert all(r.get("exitCode", 0) == 0 and "ok-" in r["output"] for r in rows)
    report["checks"]["ten_commands"] = {
        "count": 10,
        "effects_each": 1,
        "instanceIds": ids,
    }
    # Dispatcher latency uses durable creation/start records, not scheduledAt,
    # which can change after a terminal transition.
    durations = []
    for i in ids:
        a = details(i)["attempts"][0]
        durations.append((a["startedAt"] - a["acceptedAt"]) / 1000)
    report["checks"]["accept_to_start_seconds"] = {
        "max": max(durations),
        "samples": durations,
        "not_schedule_latency": True,
    }
    save()
    failed = order("failure")
    r = wait(
        "known failure",
        lambda: row(failed) if row(failed)["status"] == "NOTOK" else None,
    )
    assert r["exitCode"] == 7 and "expected-failure" in r["output"]
    lab.api("/api/instances/" + failed + "/rerun", {})
    wait(
        "explicit retry",
        lambda: len(details(failed)["attempts"]) == 2
        and row(failed)["status"] == "NOTOK",
    )
    report["checks"]["known_failure_and_operator_retry"] = {
        "exitCode": 7,
        "attempts": 2,
    }
    cancelled = order("cancel")
    wait("cancel job running", lambda: row(cancelled)["status"] == "RUNNING")
    lab.api("/api/instances/" + cancelled + "/cancel", {})
    wait(
        "cancel acknowledged",
        lambda: details(cancelled)["attempts"][0]["state"]
        in ("cancelled", "uncertain"),
    )
    report["checks"]["cancel"] = details(cancelled)
    save()
    recovery = order("recovery")
    wait("effect before backend loss", lambda: effects().count("recovery") == 1)
    original = details(recovery)["attempts"][0]["executionId"]
    begin = time.monotonic()
    scale(0)
    try:
        wait(
            "server stopped",
            lambda: not json.loads(
                lab.kube(
                    "-n",
                    lab.NS,
                    "get",
                    "pods",
                    "-l",
                    "app=regente-server",
                    "-o",
                    "json",
                )
            )["items"],
        )
        time.sleep(10)
    finally:
        scale(1)
    wait("readiness after replacement", lambda: lab.api("/readyz")["ready"], 120)
    wait("result replay", lambda: row(recovery)["status"] == "OK")
    a = details(recovery)["attempts"]
    assert (
        len(a) == 1
        and a[0]["executionId"] == original
        and effects().count("recovery") == 1
    )
    report["checks"]["backend_replacement"] = {
        "sameExecutionId": original,
        "effectCount": 1,
        "seconds": round(time.monotonic() - begin, 3),
    }
    assert report["checks"]["backend_replacement"]["seconds"] <= 120
    save()
    crashed = order("agent-restart")
    wait("agent effect started", lambda: effects().count("agent-restart") == 1)
    lab.run(["docker", "kill", "--signal=KILL", lab.WORKER])
    lab.run(["docker", "start", lab.WORKER])
    wait(
        "uncertainty after agent restart",
        lambda: details(crashed)["attempts"][0]["state"] == "uncertain",
        120,
    )
    assert (
        effects().count("agent-restart") == 1 and len(details(crashed)["attempts"]) == 1
    )
    report["checks"]["agent_restart_uncertainty"] = details(crashed)
    report["status"] = "passed"
    save()
    print(json.dumps({"status": report["status"], "checks": list(report["checks"])}))


if __name__ == "__main__":
    if (lab.STATE / "verification.json").exists():
        raise SystemExit("Evidence already exists; do not repeat side effects blindly")
    try:
        verify()
    except Exception as e:
        report["status"] = "failed"
        report["error"] = str(e)
        save()
        raise
