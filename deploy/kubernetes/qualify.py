"""Complete rotation and unwritable-journal qualification after verify.py.

Only synthetic fixtures in the selected isolated laboratory are modified.
Never run this against production or reset an existing journal.
"""

import datetime as dt
import json
import os
import ssl
import urllib.error
import urllib.parse
import urllib.request

import lab
from verify import wait


def effect_count(marker):
    return (
        lab.run(["docker", "exec", lab.WORKER, "cat", "/state/effects.log"])
        .decode()
        .splitlines()
        .count(marker)
    )


def fixture(name, command):
    lab.api(
        "/api/definitions",
        {
            "id": name,
            "label": name,
            "team": "lab",
            "jobType": "COMMAND",
            "agentId": lab.WORKER,
            "environment": "kubernetes-lab",
            "schedule": {"enabled": False},
            "timeout": 90,
            "actionConfig": {"command": command},
        },
    )


def details(instance):
    return lab.api("/api/instances/" + instance + "/executions")


def helper(command):
    return lab.run(
        [
            "docker",
            "run",
            "--rm",
            "--user",
            "0",
            "--entrypoint",
            "/bin/sh",
            "-v",
            lab.JOURNAL_VOLUME + ":/state",
            lab.AGENT,
            "-ec",
            command,
        ]
    )


def qualify():
    result = lab.STATE / "qualification.json"
    if result.exists():
        raise RuntimeError(
            "Qualification evidence already exists; inspect it before retrying"
        )
    report = {"status": "running", "checks": {}}

    def save():
        result.write_text(json.dumps(report, indent=2))

    save()
    try:
        lab.login()
        assert not any(r["status"] == "RUNNING" for r in lab.api("/api/instances"))
        # The preceding abrupt-loss fixture must be resolved only after process inspection.
        verification = json.loads((lab.STATE / "verification.json").read_text())
        assert verification["status"] == "passed"
        crash = verification["checks"]["orders"]["agent-restart"]
        attempt = details(crash)["attempts"][0]
        processes = lab.run(["docker", "top", lab.WORKER, "-eo", "pid,args"]).decode()
        assert "sleep 60" not in processes
        if attempt["state"] == "uncertain":
            lab.api(
                "/api/executions/" + attempt["executionId"] + "/resolve",
                {
                    "fence": attempt["fence"],
                    "idempotencyKey": "qualification-crash-resolved",
                    "decision": "failed",
                    "effectStopped": True,
                    "reason": "Synthetic crash: restarted container, no sleep process, one effect confirmed",
                },
            )
        assert details(crash)["attempts"][0]["state"] == "failed"
        report["checks"]["crashResolvedAfterInspection"] = True

        fixture(
            "credential-rotation",
            "echo credential-rotation >> /state/effects.log; sleep 12; echo rotation-complete",
        )
        order = lab.api("/api/definitions/credential-rotation/force", {})["instanceId"]
        wait(
            "rotation effect started", lambda: effect_count("credential-rotation") == 1
        )
        execution = details(order)["attempts"][0]["executionId"]
        container_start = lab.run(
            ["docker", "inspect", lab.WORKER, "--format", "{{.State.StartedAt}}"]
        ).decode()
        old_token = (lab.STATE / "token").read_text()
        old_id = json.loads((lab.STATE / "credential-id.json").read_text())["id"]
        issued = lab.api(
            "/api/agents/tokens/" + str(old_id) + "/rotate",
            {
                "expiresAt": (
                    dt.datetime.now(dt.timezone.utc) + dt.timedelta(days=1)
                ).isoformat(),
                "graceSeconds": 0,
            },
        )
        # Preserve the newly issued credential even if volume installation fails.
        (lab.STATE / "token.next").write_text(issued["token"])
        os.chmod(lab.STATE / "token.next", 0o600)
        (lab.STATE / "credential-next-id.json").write_text(
            json.dumps({"id": issued["id"]})
        )
        lab.run(
            [
                "docker",
                "run",
                "--rm",
                "--user",
                "0",
                "--entrypoint",
                "/bin/sh",
                "-v",
                str(lab.STATE) + ":/source:ro",
                "-v",
                lab.CREDENTIAL_VOLUME + ":/credentials",
                lab.AGENT,
                "-ec",
                "cp /source/token.next /credentials/token.next; chmod 600 /credentials/token.next; chown 10001:10001 /credentials/token.next; mv /credentials/token.next /credentials/token",
            ]
        )
        (lab.STATE / "token.next").replace(lab.STATE / "token")
        (lab.STATE / "credential-next-id.json").replace(
            lab.STATE / "credential-id.json"
        )
        ctx = ssl.create_default_context(cafile=str(lab.STATE / "ca.crt"))
        ctx.load_cert_chain(lab.STATE / "client.crt", lab.STATE / "client.key")
        query = urllib.parse.urlencode(
            {
                "id": lab.WORKER,
                "env": "kubernetes-lab",
                "caps": "COMMAND,EXECUTION_V2",
                "journal": 1,
                "ver": "qualification",
                "available": 0,
            }
        )
        request = urllib.request.Request(
            lab.BASE + "/api/agent/v2/poll?" + query,
            headers={"Authorization": "Bearer " + old_token},
        )
        try:
            urllib.request.urlopen(request, context=ctx, timeout=10).close()
            raise AssertionError("Rotated credential was accepted")
        except urllib.error.HTTPError as error:
            assert error.code == 401
        wait(
            "rotation result reconciled",
            lambda: lab.api("/api/instances/" + order)["status"] == "OK",
            60,
        )
        attempts = details(order)["attempts"]
        assert len(attempts) == 1 and attempts[0]["executionId"] == execution
        assert effect_count("credential-rotation") == 1
        assert (
            container_start
            == lab.run(
                ["docker", "inspect", lab.WORKER, "--format", "{{.State.StartedAt}}"]
            ).decode()
        )
        report["checks"]["rotationDuringExecution"] = {
            "oldRejected401": True,
            "sameExecution": True,
            "effects": 1,
            "agentRestarted": False,
        }
        save()

        fixture(
            "storage-recovery",
            "echo storage-recovery >> /state/effects.log; echo storage-recovered",
        )
        lab.run(["docker", "stop", "--time", "10", lab.WORKER])
        try:
            helper("chmod 500 /state; chmod 400 /state/journal*")
            lab.run(["docker", "start", lab.WORKER])
            wait(
                "agent refuses unwritable journal",
                lambda: json.loads(lab.run(["docker", "inspect", lab.WORKER]))[0][
                    "State"
                ]["Status"]
                == "exited",
                30,
            )
            stopped = json.loads(lab.run(["docker", "inspect", lab.WORKER]))[0]["State"]
            assert stopped["ExitCode"] != 0
            content = helper("cat /state/effects.log").decode()
            assert "storage-recovery" not in content
            pending = lab.api("/api/definitions/storage-recovery/force", {})[
                "instanceId"
            ]
            assert not any(a.get("startedAt", 0) for a in details(pending)["attempts"])
            report["checks"]["unwritableJournal"] = {
                "agentExitedNonzero": True,
                "noEffectWhileUnavailable": True,
                "journalPreserved": True,
            }
        finally:
            helper("chmod 700 /state; chmod 600 /state/journal*")
            lab.run(["docker", "start", lab.WORKER])
        wait(
            "storage recovery result",
            lambda: lab.api("/api/instances/" + pending)["status"] == "OK",
            120,
        )
        assert effect_count("storage-recovery") == 1
        report["checks"]["unwritableJournal"]["effectAfterRecovery"] = 1
        report["status"] = "passed"
    except Exception as error:
        report["status"] = "failed"
        report["error"] = str(error)
        raise
    finally:
        save()
        print(json.dumps(report))


if __name__ == "__main__":
    qualify()
