"""Fresh synthetic phase04 lab. Preserve prior labs and all journals/evidence."""

import argparse
import shutil
import lab
import ssh_bridge as bridge

JOBS = ("migrate-one", "migrate-two")
ORIGIN = lab.INSTANCE + "-origin-agent"
bridge.COMMANDS = {name: "sh /jobs/" + name + ".sh" for name in JOBS}


def prepare():
    bridge.prepare()
    for name in JOBS:
        (lab.STATE / "ssh-jobs" / (name + ".sh")).write_bytes(
            (
                f"echo {name} >> /data/effects; sync; sleep 4; "
                f"echo {name} >> /data/completed; sync; "
                f"echo result-{name}; echo diagnostic-{name} >&2\n"
            ).encode()
        )


def origin():
    state = lab.STATE
    lab.STATE = state / "origin"
    lab.STATE.mkdir()
    for name in ("ca.crt", "client.crt", "client.key", "private.json"):
        shutil.copyfile(state / name, lab.STATE / name)
    lab.WORKER = ORIGIN
    lab.JOURNAL_VOLUME = ORIGIN + "-journal"
    lab.CREDENTIAL_VOLUME = ORIGIN + "-credentials"
    lab.agent(
        mounts=(str(state / "ssh-jobs") + ":/jobs:ro", bridge.TARGET + "-data:/data")
    )


def proxy():
    lab.run(
        [
            "docker",
            "run",
            "-d",
            "--name",
            lab.INSTANCE + "-proxy",
            "--network",
            "kind",
            "--read-only",
            "--cap-drop",
            "ALL",
            "--security-opt",
            "no-new-privileges",
            "--memory",
            "128m",
            "--cpus",
            "0.25",
            "-p",
            "127.0.0.1:12443:12443",
            "regente-kubernetes-web-check",
            "node",
            "-e",
            'const net=require("node:net");net.createServer(s=>{'
            'const t=net.connect(30447,"regente-kubernetes-control-plane");'
            's.pipe(t).pipe(s);s.on("error",()=>t.destroy());'
            't.on("error",()=>s.destroy());}).listen(12443,"0.0.0.0")',
        ]
    )


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "action",
        choices=["prepare", "proxy", "origin", "external-agent", "deploy", "finalize"],
    )
    action = parser.parse_args().action
    {
        "prepare": prepare,
        "proxy": proxy,
        "origin": origin,
        "external-agent": lab.agent,
        "deploy": bridge.deploy,
        "finalize": lab.finalize,
    }[action]()
