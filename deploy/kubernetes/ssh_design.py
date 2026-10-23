"""Enable isolated local GitOps for the SSH browser qualification, after jobs finish."""

import json
import subprocess
import lab


def main():
    if lab.INSTANCE == "regente-kubernetes":
        raise RuntimeError("Select the isolated SSH instance")
    lab.login()
    definitions = lab.api("/api/definitions")
    seed = lab.STATE / "design-seed"
    seed.mkdir()  # Never overwrite a previous seed.
    folder = seed / "definitions" / "lab"
    folder.mkdir(parents=True)
    for definition in definitions:
        if definition["team"] != "lab":
            raise RuntimeError("Only synthetic lab definitions are permitted")
        definition["params"] = definition.pop("actionConfig", {})
        (folder / (definition["id"] + ".yaml")).write_text(
            json.dumps(definition), encoding="utf-8"
        )

    def git(*args):
        subprocess.run(["git", "-C", str(seed), *args], check=True, capture_output=True)

    git("init", "-b", "main")
    git("add", "definitions")
    git(
        "-c",
        "user.name=Lab",
        "-c",
        "user.email=lab@example.invalid",
        "commit",
        "-m",
        "Synthetic SSH definitions",
    )
    expose(seed)


def expose(seed):
    # File transport invokes git-upload-pack in go-git; distroless has no git.
    # A read-only synthetic git daemon keeps that helper outside the backend.
    name = lab.INSTANCE + "-git-fixture"
    lab.run(
        [
            "docker",
            "run",
            "-d",
            "--name",
            name,
            "--network",
            "kind",
            "--read-only",
            "--memory",
            "128m",
            "--pids-limit",
            "32",
            "-v",
            str(seed) + ":/repo:ro",
            "regente-kubernetes-git-fixture:phase03",
            "-c",
            "safe.directory=/repo",
            "daemon",
            "--reuseaddr",
            "--export-all",
            "--base-path=/repo",
            "--listen=0.0.0.0",
            "/repo",
        ]
    )
    ip = json.loads(lab.run(["docker", "inspect", name]))[0]["NetworkSettings"][
        "Networks"
    ]["kind"]["IPAddress"]
    if not ip:
        raise RuntimeError(
            "Git fixture stopped; inspect its logs before configuring backend"
        )
    patch = lab.STATE / "design-workdir.json"
    patch.write_text(
        json.dumps(
            {
                "spec": {
                    "template": {
                        "spec": {
                            "containers": [{"name": "server", "workingDir": "/data"}]
                        }
                    }
                }
            }
        ),
        encoding="utf-8",
    )
    lab.kube(
        "-n",
        lab.NS,
        "patch",
        "deployment",
        "regente-server",
        "--type=strategic",
        "--patch-file",
        str(patch),
    )
    lab.kube(
        "-n",
        lab.NS,
        "set",
        "env",
        "deployment/regente-server",
        "REGENTE_GIT_SOURCE=git://" + ip + "/",
        "REGENTE_GIT_BRANCH=main",
        "REGENTE_WORKSPACE=/data/design-workspace",
    )
    lab.kube(
        "-n", lab.NS, "rollout", "status", "deployment/regente-server", "--timeout=120s"
    )
    print("Synthetic read-only Git daemon enabled; no GitHub connection")


if __name__ == "__main__":
    main()
