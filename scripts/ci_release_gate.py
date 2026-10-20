"""Accept only a completed, successful main-push CI at the release source SHA."""
import argparse
import json
import os
from pathlib import Path
import re
import urllib.request

REQUIRED = {
    "workflow contract",
    "verification scope",
    "capacity policy",
    "verification / full verification (Linux)",
    "doc-recipes / toolchain minimum Node 20.19.0",
    "doc-recipes / toolchain minimum Node 22.13.0",
    "doc-recipes / toolchain minimum Node 24.0.0",
    "doc-recipes / PowerShell demo ubuntu-latest",
    "doc-recipes / PowerShell demo windows-latest",
}


def validate(run, jobs, repository, sha, run_id):
    expected = {
        "id": run_id, "head_sha": sha, "event": "push",
        "head_branch": "main", "status": "completed", "conclusion": "success",
        "path": ".github/workflows/ci.yml",
    }
    if not re.fullmatch(r"[0-9a-f]{40}", sha):
        raise ValueError("Invalid source SHA")
    if any(run.get(key) != value for key, value in expected.items()):
        raise ValueError("CI identity, revision or outcome does not match")
    if any(run.get(key, {}).get("full_name") != repository
           for key in ("repository", "head_repository")):
        raise ValueError("CI repository does not match")
    for name in REQUIRED:
        matches = [job for job in jobs if job.get("name") == name]
        if (len(matches) != 1 or matches[0].get("status") != "completed"
                or matches[0].get("conclusion") != "success"):
            raise ValueError("Missing, duplicated or unsuccessful CI job: " + name)
    capacity = [job for job in jobs
                if job.get("name") == "verification / measured capacity and developer soak"]
    if (len(capacity) != 1 or capacity[0].get("status") != "completed"
            or capacity[0].get("conclusion") not in ("success", "skipped")):
        raise ValueError("Missing or unsuccessful capacity result")
    return {"capacityResult": capacity[0]["conclusion"], "sourceSha": sha, "runId": run_id, "runAttempt": run["run_attempt"],
            "repository": repository, "requiredJobs": sorted(REQUIRED),
            "status": "passed"}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--run-id", required=True, type=int)
    parser.add_argument("--sha", required=True)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    repository = os.environ["GITHUB_REPOSITORY"]
    token = os.environ["GH_TOKEN"]
    base = os.environ.get("GITHUB_API_URL", "https://api.github.com")

    def get(path):
        request = urllib.request.Request(base + "/repos/" + repository + path,
                                         headers={"Authorization": "Bearer " + token,
                                                  "Accept": "application/vnd.github+json"})
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.load(response)

    run = get(f"/actions/runs/{args.run_id}")
    jobs = []
    for page in range(1, 101):
        batch = get(f"/actions/runs/{args.run_id}/attempts/{run['run_attempt']}"
                    f"/jobs?per_page=100&page={page}")["jobs"]
        jobs.extend(batch)
        if len(batch) < 100:
            break
    else:
        raise ValueError("CI jobs pagination exceeded")
    proof = validate(run, jobs, repository, args.sha, args.run_id)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(proof, indent=2) + "\n", encoding="utf-8")
    print(f"Verified CI run {args.run_id} at {args.sha}: all required jobs passed.")


if __name__ == "__main__":
    main()
