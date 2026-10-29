"""One-shot phase04 rehearsal, isolated single producer; never run on production."""

import hashlib
import argparse
import json
import ssl
import time
import urllib.error
import urllib.request
import lab
import migration_lab as migration
from migration_guard import require_drained, require_retirable
from verify import wait
from verify_http_cell import row, attempt
from verify_ssh_bridge import lines, target

JOBS = migration.JOBS
ORIGIN = migration.ORIGIN
BRIDGE = migration.bridge.cell.IDENTITY


def definition(name, mode, timeout=40):
    command = migration.bridge.COMMANDS[name]
    config = dict(command=command)
    if mode == "SSH_REMOTE":
        config.update(
            host="ssh-target",
            port=2222,
            user="runner",
            keyPath="/private/ssh_key",
            knownHostsPath="/private/known_hosts",
            strictHostKey="yes",
        )
    return dict(
        id=name,
        label=name,
        team="migration",
        jobType=mode,
        agentId=BRIDGE if mode == "SSH_REMOTE" else ORIGIN,
        environment="kubernetes-lab",
        timeout=timeout,
        retries=0,
        confirm=True,
        schedule={"enabled": False},
        actionConfig=config,
    )


def inventory():
    # Read all retained dates, not the UI's default current business day.
    ids = (
        lab.kube(
            "-n",
            lab.NS,
            "exec",
            "deployment/postgres",
            "--",
            "psql",
            "-U",
            "regente",
            "-d",
            "regente",
            "-Atc",
            "SELECT id FROM instances ORDER BY id",
        )
        .decode()
        .splitlines()
    )
    items = []
    for i in ids:
        item = row(i)
        item["attempts"] = lab.api("/api/instances/" + i + "/executions")["attempts"]
        items.append(item)
    defs = lab.api("/api/definitions")
    return dict(
        complete=True,
        producersFenced=True,
        jobs=[d["id"] for d in defs],
        definitions=defs,
        instances=items,
    )


def stopped_journal():
    container = json.loads(lab.run(["docker", "inspect", ORIGIN]))[0]
    if container["State"]["Running"]:
        raise ValueError("Stop drained origin before inspecting its journal")
    code = (
        'import sqlite3,json,shutil; shutil.copytree("/state","/snapshot/journal"); '
        'c=sqlite3.connect("file:/snapshot/journal/journal.db?mode=ro",uri=True); '
        'print(json.dumps(dict(identity=c.execute("SELECT agent_id FROM journal_meta").fetchone()[0],'
        "pendingEntries=c.execute(\"SELECT count(*) FROM journal_entries WHERE state != 'acked'\").fetchone()[0],"
        'pendingChunks=c.execute("SELECT count(*) FROM journal_chunks WHERE acked=0").fetchone()[0])))'
    )
    result = json.loads(
        lab.run(
            [
                "docker",
                "run",
                "--rm",
                "--read-only",
                "--user",
                "10001:10001",
                "--tmpfs",
                "/snapshot:rw,noexec,nosuid,size=64m,uid=10001,gid=10001,mode=0700",
                "--network",
                "none",
                "--cap-drop",
                "ALL",
                "--security-opt",
                "no-new-privileges",
                "-v",
                ORIGIN + "-journal:/state:ro",
                "python:3-slim",
                "python",
                "-c",
                code,
            ]
        )
    )
    result["stopped"] = True
    return result


def revoked_request_status():
    ctx = ssl.create_default_context(cafile=str(lab.STATE / "origin/ca.crt"))
    ctx.load_cert_chain(
        str(lab.STATE / "origin/client.crt"), str(lab.STATE / "origin/client.key")
    )
    token = (lab.STATE / "origin/token").read_text()
    req = urllib.request.Request(
        lab.BASE
        + "/api/agent/v2/poll?id="
        + ORIGIN
        + "&env=kubernetes-lab&caps=COMMAND,EXECUTION_V2&protocol=2&journal=1&ver=phase04&available=0",
        headers={"Authorization": "Bearer " + token},
    )
    try:
        with urllib.request.urlopen(req, context=ctx, timeout=10) as response:
            return response.status
    except urllib.error.HTTPError as exc:
        return exc.code


def verify(resume_after_baseline=False, resume_after_uncertain=False):
    if lab.INSTANCE != "regente-kubernetes-phase04":
        raise ValueError("Only the dedicated phase04 instance is supported")
    path = lab.STATE / "migration-verification.json"
    previous = None
    if resume_after_uncertain:
        source = lab.STATE / "migration-verification-resumed.json"
        previous = json.loads(source.read_text(encoding="utf-8"))
        if (
            previous.get("status") != "failed"
            or previous.get("errorType") != "IndexError"
            or len(previous["orders"]) != 7
            or len(previous["transitions"]) != 2
            or not previous["checks"].get("batch_expanded_with_immutable_old_snapshots")
        ):
            raise ValueError("Resume only supports the inspected attempt-creation race")
        parent_hash = hashlib.sha256(source.read_bytes()).hexdigest()
        path = lab.STATE / "migration-verification-final.json"
    elif resume_after_baseline:
        previous = json.loads(path.read_text(encoding="utf-8"))
        if (
            previous.get("status") != "failed"
            or previous.get("errorType") != "KeyError"
            or list(previous["checks"])
            != ["new_dispatch_gated_running_drained_old_pending_cancelled"]
            or len(previous["orders"]) != 3
            or previous["transitions"]
        ):
            raise ValueError(
                "Resume only supports the inspected exitCode baseline failure"
            )
        parent_hash = hashlib.sha256(path.read_bytes()).hexdigest()
        path = lab.STATE / "migration-verification-resumed.json"
    if path.exists():
        raise ValueError("Evidence already exists; inspect it, never rerun blindly")
    report = previous or dict(checks={}, orders=[], transitions=[])
    report["status"] = "running"
    if previous:
        report["resumedFromSHA256"] = parent_hash
        report.pop("errorType", None)

    def save():
        path.write_text(json.dumps(report, indent=2), encoding="utf-8")

    def check(name, value=True):
        report["checks"][name] = value
        save()
        print(name, flush=True)

    def order(name, confirm=True):
        # ForceOrder's source uses second-resolution IDs; never collide.
        time.sleep(1.1)
        i = lab.api("/api/definitions/" + name + "/force", {})["instanceId"]
        report["orders"].append(i)
        save()
        if confirm:
            lab.api("/api/instances/" + i + "/confirm", {})
        return i

    def completed(name, mode):
        before = lines("effects").count(name)
        i = order(name)
        wait("completed " + i, lambda: row(i)["status"] == "OK")
        r = row(i)
        expected = BRIDGE if mode == "SSH_REMOTE" else ORIGIN
        assert r["agentId"] == expected and r["jobType"] == mode
        assert r.get("exitCode", 0) == 0 and "result-" + name in r["output"]
        assert "diagnostic-" + name in r["output"]
        assert lines("effects").count(name) == before + 1
        assert len(lab.api("/api/instances/" + i + "/executions")["attempts"]) == 1
        return r

    def transition(names, mode):
        inv = inventory()
        require_drained(inv, names)
        before = {
            r["id"]: r["snapshotDef"]
            for r in inv["instances"]
            if r["definitionId"] in names
        }
        for name in names:
            lab.api("/api/definitions", definition(name, mode))
        assert all(row(i)["snapshotDef"] == snapshot for i, snapshot in before.items())
        report["transitions"].append(
            dict(jobs=list(names), mode=mode, oldSnapshotsUnchanged=len(before))
        )
        save()

    def blocked(call):
        try:
            call()
        except ValueError:
            return
        raise AssertionError("Unsafe operation was not blocked")

    save()
    try:
        wait("HTTPS ready", lambda: lab.api("/readyz")["ready"], 120)
        lab.login()
        wait(
            "three identities online",
            lambda: {ORIGIN, BRIDGE, lab.WORKER}
            <= {a["id"] for a in lab.api("/api/agents") if a["online"]},
            120,
        )
        hashes = {
            name: hashlib.sha256(
                (lab.STATE / "ssh-jobs" / (name + ".sh")).read_bytes()
            ).hexdigest()
            for name in JOBS
        }
        if not resume_after_uncertain:
            if previous:
                assert hashes == report["scriptSHA256"]
                assert [row(i)["status"] for i in report["orders"]] == [
                    "OK",
                    "CANCELLED",
                    "OK",
                ]
                assert sorted(lines("effects")) == sorted(JOBS)
                assert sorted(lines("completed")) == sorted(JOBS)
                for i in (report["orders"][0], report["orders"][2]):
                    r = row(i)
                    assert r["jobType"] == "COMMAND" and r["agentId"] == ORIGIN
                    assert r.get("exitCode", 0) == 0
                    assert "result-" + r["definitionId"] in r["output"]
                    assert "diagnostic-" + r["definitionId"] in r["output"]
                require_drained(inventory(), JOBS)
                check("baseline_resume_verified_without_repeating_effects")
            else:
                assert not lines("effects")
                report["scriptSHA256"] = hashes
                for name in JOBS:
                    lab.api("/api/definitions", definition(name, "COMMAND"))
                first = order(JOBS[0])
                wait("origin effect", lambda: lines("effects").count(JOBS[0]) == 1)
                blocked(lambda: require_drained(inventory(), JOBS))
                waiting = order(JOBS[1], confirm=False)
                lab.api("/api/instances/" + waiting + "/hold", {})
                time.sleep(2)
                assert row(waiting)["status"] == "HELD" and not row(waiting).get(
                    "startedAt"
                )
                assert lines("effects").count(JOBS[1]) == 0
                wait("drain existing origin", lambda: row(first)["status"] == "OK")
                blocked(lambda: require_drained(inventory(), JOBS))
                lab.api("/api/instances/" + waiting + "/cancel", {})
                wait(
                    "cancel pending snapshot",
                    lambda: row(waiting)["status"] == "CANCELLED",
                )
                require_drained(inventory(), JOBS)
                check("new_dispatch_gated_running_drained_old_pending_cancelled")
                completed(JOBS[1], "COMMAND")
            transition([JOBS[0]], "SSH_REMOTE")
            completed(JOBS[0], "SSH_REMOTE")
            completed(JOBS[1], "COMMAND")
            check("canary_remote_and_remaining_origin_same_scripts_results")
            transition([JOBS[1]], "SSH_REMOTE")
            completed(JOBS[1], "SSH_REMOTE")
            check("batch_expanded_with_immutable_old_snapshots")
            # Short remote deadline deliberately produces no terminal remote receipt.
            lab.api("/api/definitions", definition(JOBS[0], "SSH_REMOTE", timeout=2))
            effects_before = lines("effects").count(JOBS[0])
            done_before = lines("completed").count(JOBS[0])
            uncertain = order(JOBS[0])
        else:
            assert hashes == report["scriptSHA256"]
            uncertain = report["orders"][-1]
            assert row(uncertain)["status"] == "UNCERTAIN"
            assert row(uncertain)["snapshotDef"]["timeout"] == 2
            assert lines("effects").count(JOBS[0]) == 3
            assert lines("completed").count(JOBS[0]) == 3
            assert lines("effects").count(JOBS[1]) == 3
            assert lines("completed").count(JOBS[1]) == 3
            effects_before = done_before = 2
            check("uncertain_resume_verified_existing_order_no_new_submission")
        wait(
            "uncertain remote",
            lambda: any(
                a["state"] == "uncertain"
                for a in lab.api("/api/instances/" + uncertain + "/executions")[
                    "attempts"
                ]
            ),
        )
        blocked(lambda: transition(JOBS, "COMMAND"))
        blocked(lambda: require_retirable(inventory(), JOBS, ORIGIN, {}))
        execution = attempt(uncertain)["executionId"]
        wait(
            "independent remote completion",
            lambda: lines("completed").count(JOBS[0]) == done_before + 1,
        )
        assert lines("effects").count(JOBS[0]) == effects_before + 1
        assert (
            len(lab.api("/api/instances/" + uncertain + "/executions")["attempts"]) == 1
        )
        assert "sh /jobs/" not in target("ps", "-o", "pid,args").decode()
        a = attempt(uncertain)
        lab.api(
            "/api/executions/" + execution + "/resolve",
            dict(
                fence=a["fence"],
                idempotencyKey="phase04-observed-completion",
                decision="failed",
                effectStopped=True,
                reason="Synthetic script finished with one independent completion marker; no remote process remains; no rerun authorized",
            ),
        )
        check("uncertainty_blocks_rollback_retirement_no_replay_resolved_by_witness")
        transition(JOBS, "COMMAND")
        completed(JOBS[0], "COMMAND")
        completed(JOBS[1], "COMMAND")
        check("rollback_new_orders_on_origin_old_remote_snapshot_unchanged")
        blocked(lambda: require_retirable(inventory(), JOBS, ORIGIN, {}))
        check("retirement_blocked_while_definitions_depend_on_origin")
        transition(JOBS, "SSH_REMOTE")
        for name in JOBS:
            completed(name, "SSH_REMOTE")
        require_drained(inventory(), JOBS)
        # Allow result/chunk ACK delivery, then stop the sole journal owner.
        time.sleep(3)
        lab.run(["docker", "stop", "--time", "20", ORIGIN])
        journal = stopped_journal()
        require_retirable(inventory(), JOBS, ORIGIN, journal)
        report["retiredJournal"] = journal
        credential = json.loads((lab.STATE / "origin/credential-id.json").read_text())[
            "id"
        ]
        assert revoked_request_status() == 204
        lab.api("/api/agents/tokens/" + str(credential), method="DELETE")
        assert revoked_request_status() == 401
        assert not json.loads(lab.run(["docker", "inspect", ORIGIN]))[0]["State"][
            "Running"
        ]
        check("origin_stopped_journal_acknowledged_preserved_credential_revoked")
        for name in JOBS:
            completed(name, "SSH_REMOTE")
        lab.api(
            "/api/definitions",
            dict(
                id="retained-external",
                label="retained-external",
                team="lab",
                jobType="COMMAND",
                agentId=lab.WORKER,
                environment="kubernetes-lab",
                timeout=20,
                confirm=True,
                schedule={"enabled": False},
                actionConfig={"command": "echo retained-external"},
            ),
        )
        retained = order("retained-external")
        wait("nonmigrated agent", lambda: row(retained)["status"] == "OK")
        assert row(retained)["agentId"] == lab.WORKER
        target(
            "sh",
            "-ec",
            "test ! -e /usr/local/bin/regente-agent; test ! -e /usr/bin/regente-agent",
        )
        assert attempt(uncertain)["executionId"] == execution
        assert (
            len(lab.api("/api/instances/" + uncertain + "/executions")["attempts"]) == 1
        )
        check("remote_after_retirement_nonmigrated_agent_supported_no_uncertain_rerun")
        report["finalInventory"] = inventory()
        require_drained(report["finalInventory"], JOBS)
        report["effects"] = lines("effects")
        report["completed"] = lines("completed")
        report["status"] = "passed"
    except Exception as exc:
        report["status"] = "failed"
        report["errorType"] = type(exc).__name__
        raise
    finally:
        save()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    resume = parser.add_mutually_exclusive_group()
    resume.add_argument("--resume-after-baseline", action="store_true")
    resume.add_argument("--resume-after-uncertain", action="store_true")
    args = parser.parse_args()
    verify(args.resume_after_baseline, args.resume_after_uncertain)
