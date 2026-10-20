"""Select capacity qualification from the complete pushed diff, failing closed."""
import argparse
import json
import os
from pathlib import Path
import subprocess


def needs_capacity(paths):
    """Only known non-runtime changes bypass the engineering soak."""
    for path in paths:
        if path.endswith(".md") or path.startswith(("docs/", ".github/", "app/")):
            continue
        if path.startswith(("server/cmd/docsite/", "server/internal/api/apidocs")):
            continue
        if path in {
            "scripts/ci_scope.py", "scripts/test_ci_scope.py",
            "scripts/ci_release_gate.py", "scripts/test_ci_release_gate.py",
            "scripts/verify.py", "scripts/verify.sh", "scripts/test_verify.py",
            "scripts/check-doc-recipes.cjs", "scripts/test-demo-guards.ps1",
            "deploy/demo/host-demo.ps1",
        }:
            continue
        return True
    return False


def changed_paths(base, head):
    if not base or set(base) == {"0"}:
        raise ValueError("No trustworthy diff base")
    result = subprocess.run(["git", "diff", "--name-only", "-z", base, head],
                            check=True, capture_output=True)
    return [p for p in result.stdout.decode("utf-8").split("\0") if p]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--release", action="store_true")
    args = parser.parse_args()
    event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text(encoding="utf-8"))
    name = os.environ["GITHUB_EVENT_NAME"]
    head = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
    paths = []
    if name == "pull_request" and not args.release:
        required, reason = False, "PR: functional gates run; capacity is qualified on main"
    elif event.get("inputs", {}).get("capacity") in (True, "true"):
        required, reason = True, "Explicit capacity qualification requested"
    else:
        try:
            if args.release or name == "workflow_dispatch":
                base = subprocess.check_output(
                    ["git", "describe", "--tags", "--abbrev=0", "--match", "v[0-9]*", head + "^"],
                    text=True, stderr=subprocess.DEVNULL).strip()
            else:
                base = event.get("before", "")
            paths = changed_paths(base, head)
            required = needs_capacity(paths)
            reason = ("Runtime or unclassified paths changed" if required
                      else "Only known non-runtime paths changed")
        except (ValueError, subprocess.CalledProcessError, UnicodeError):
            required, reason = True, "Diff unavailable: qualify capacity conservatively"
    report = {"sourceSha": head, "capacityRequired": required, "reason": reason,
              "changedPaths": paths}
    print(json.dumps(report, indent=2))
    with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
        output.write("capacity=" + str(required).lower() + "\n")
    with open(os.environ["GITHUB_STEP_SUMMARY"], "a", encoding="utf-8") as summary:
        summary.write(f"Capacity soak required: **{required}**. {reason}.\n")


if __name__ == "__main__":
    main()
