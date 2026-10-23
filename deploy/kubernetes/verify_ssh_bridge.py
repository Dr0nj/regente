"""Short stateful SSH bridge qualification. Run once in a fresh lab."""

import json
import time
import lab
import ssh_bridge as bridge
from verify import wait
from verify_http_cell import pod, replacement, attempt, row


def target(*args, data=None):
    return lab.run(["docker", "exec", "-i", bridge.TARGET, *args], data=data)


def lines(name):
    return (
        target("sh", "-ec", f"if test -f /data/{name}; then cat /data/{name}; fi")
        .decode()
        .splitlines()
    )


def define(name, command=None, host="ssh-target", user="runner", timeout=40, retries=0):
    lab.api(
        "/api/definitions",
        dict(
            id=name,
            label=name,
            team="lab",
            jobType="SSH_REMOTE",
            agentId=bridge.cell.IDENTITY,
            environment="kubernetes-lab",
            timeout=timeout,
            retries=retries,
            schedule={"enabled": False},
            actionConfig=dict(
                host=host,
                port=2222,
                user=user,
                command=command or bridge.COMMANDS[name],
                keyPath="/private/ssh_key",
                knownHostsPath="/private/known_hosts",
                strictHostKey="yes",
            ),
        ),
    )


def verify():
    path = lab.STATE / "ssh-verification.json"
    if path.exists():
        raise RuntimeError("Evidence exists: inspect before any repetition")
    report = dict(status="running", checks={}, orders={})

    def save():
        path.write_text(json.dumps(report, indent=2))

    def order(name):
        i = lab.api("/api/definitions/" + name + "/force", {})["instanceId"]
        report["orders"][name] = i
        save()
        return i

    def known(name, status):
        i = order(name)
        wait(name, lambda: row(i)["status"] == status)
        return i

    def ready_target():
        try:
            target("sh", "-ec", "nc -z -w 1 127.0.0.1 2222")
            return True
        except RuntimeError:
            return False

    save()
    try:
        wait("HTTPS backend", lambda: lab.api("/readyz")["ready"], 120)
        lab.login()
        wait(
            "bridge online",
            lambda: any(
                a["id"] == bridge.cell.IDENTITY and a["online"]
                for a in lab.api("/api/agents")
            ),
            120,
        )
        wait("sshd ready", ready_target)
        assert not lines("effects")
        target(
            "sh",
            "-ec",
            "test ! -e /usr/local/bin/regente-agent; test ! -e /usr/bin/regente-agent",
        )
        assert (
            "sshd"
            in lab.run(["docker", "top", bridge.TARGET, "-eo", "pid,args"]).decode()
        )
        report["checks"]["external_sshd_no_regente_agent"] = True
        define("ssh-ok")
        define("ssh-failure")
        lab.api(
            "/api/definitions",
            dict(
                id="external-coexist",
                label="external-coexist",
                team="lab",
                jobType="COMMAND",
                agentId=lab.WORKER,
                environment="kubernetes-lab",
                timeout=10,
                schedule={"enabled": False},
                actionConfig={"command": "echo external-still-supported"},
            ),
        )
        h, c = order("ssh-ok"), order("external-coexist")
        wait(
            "bridge and external coexistence",
            lambda: row(h)["status"] == row(c)["status"] == "OK",
        )
        assert (
            row(h)["agentId"] == bridge.cell.IDENTITY
            and row(c)["agentId"] == lab.WORKER
        )
        assert (
            "remote-stdout" in row(h)["output"] and "remote-stderr" in row(h)["output"]
        )
        assert lines("effects").count("success") == 1
        failure = known("ssh-failure", "NOTOK")
        assert (
            row(failure)["exitCode"] == 7 and "remote-failure" in row(failure)["output"]
        )
        report["checks"]["routing_stdout_stderr_exit0_exit7_and_coexistence"] = True
        for name, kwargs in [
            ("ssh-denied", {"host": "169.254.169.254"}),
            ("ssh-user-denied", {"user": "root"}),
            ("ssh-command-denied", {"command": "touch /data/unapproved"}),
        ]:
            define(name, **kwargs)
            i = known(name, "NOTOK")
            assert "denied" in row(i)["output"].lower()
        assert lines("effects").count("success") == 1
        report["checks"]["destination_user_command_policy_denied_before_effect"] = True
        # Direct networking cannot bypass the pod firewall.
        forbidden = json.loads(
            lab.kube("-n", lab.NS, "get", "svc", "postgres", "-o", "json")
        )["spec"]["clusterIP"]
        target("sh", "-ec", "nc -z -w 2 127.0.0.1 2222")
        bridge.cell.podexec(
            "sh", "-ec", f"if nc -z -w 2 {forbidden} 5432; then exit 1; fi"
        )
        report["checks"]["bridge_egress_denies_database"] = True
        define("ssh-revoked")
        target("sh", "-ec", ": > /keys/authorized_keys")
        try:
            i = known("ssh-revoked", "NOTOK")
            assert "before submission" in row(i)["output"]
        finally:
            target(
                "sh",
                "-ec",
                "cp /source/authorized_keys /keys/authorized_keys; chmod 644 /keys/authorized_keys",
            )
        define("ssh-hostkey")
        host_path = lab.STATE / "ssh-source" / "host_key"
        original = host_path.read_bytes()
        try:
            bridge.key(host_path)
            lab.run(["docker", "restart", bridge.TARGET])
            wait("changed host key sshd", ready_target)
            i = known("ssh-hostkey", "NOTOK")
            assert "host key verification failed" in row(i)["output"]
        finally:
            host_path.write_bytes(original)
            lab.run(["docker", "restart", bridge.TARGET])
            wait("original host key sshd", ready_target)
        assert lines("effects").count("success") == 1
        report["checks"]["revoked_key_and_wrong_host_key_no_effect"] = True
        save()
        for name in ("ssh-cancel", "ssh-timeout", "ssh-crash"):
            marker = name.removeprefix("ssh-")
            define(name, timeout=3 if marker == "timeout" else 40, retries=3)
            i = order(name)
            wait("remote effect " + marker, lambda: lines("effects").count(marker) == 1)
            execution = attempt(i)["executionId"]
            if marker == "cancel":
                lab.api("/api/instances/" + i + "/cancel", {})
            elif marker == "crash":
                p = pod()
                uid = p["metadata"]["uid"]
                container = p["status"]["containerStatuses"][0][
                    "containerID"
                ].removeprefix("containerd://")
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
                        container,
                    ]
                )
                lab.kube(
                    "-n",
                    lab.NS,
                    "delete",
                    "pod",
                    bridge.cell.POD,
                    "--wait=true",
                    "--timeout=60s",
                )
                replacement(uid)
            wait(
                "remote uncertainty " + marker,
                lambda: attempt(i)["state"] == "uncertain",
            )
            # Cancellation deliberately does not assert the remote process stopped.
            wait(
                "independent remote completion " + marker,
                lambda: lines("completed").count(marker) == 1,
                40,
            )
            assert lines("effects").count(marker) == 1
            assert attempt(i)["executionId"] == execution
            assert len(lab.api("/api/instances/" + i + "/executions")["attempts"]) == 1
            a = attempt(i)
            lab.api(
                "/api/executions/" + execution + "/resolve",
                dict(
                    fence=a["fence"],
                    idempotencyKey="ssh-" + marker + "-observed",
                    decision="failed",
                    effectStopped=True,
                    reason="Synthetic destination completed exactly one observed effect; remote completion inspected; no replay authorized",
                ),
            )
            report["checks"][
                marker + "_uncertain_single_effect_no_retry_resolved_after_observation"
            ] = True
            save()
        assert not any(r["status"] == "RUNNING" for r in lab.api("/api/instances"))
        report["status"] = "passed"
    except Exception as exc:
        report["status"] = "failed"
        report["errorType"] = type(exc).__name__
        raise
    finally:
        save()


if __name__ == "__main__":
    verify()
