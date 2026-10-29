"""Read-only gates for the isolated migration rehearsal, not a distributed lock.

Inputs must cover all retained business dates, with admin visibility, while
producers/operators are fenced. Never infer journal drain from server status.
"""

TERMINAL = {"OK", "NOTOK", "CANCELLED"}
ATTEMPT_TERMINAL = {"succeeded", "failed", "cancelled"}


def require_drained(inventory, jobs):
    if not inventory.get("complete") or not inventory.get("producersFenced"):
        raise ValueError("Complete inventory and fenced producers required")
    if not jobs or not set(jobs) <= set(inventory["jobs"]):
        raise ValueError("Batch missing from inventory")
    for item in inventory["instances"]:
        if item["definitionId"] not in jobs:
            continue
        if item["status"] not in TERMINAL:
            raise ValueError("Outstanding instance: " + item["id"])
        if "attempts" not in item:
            raise ValueError("Missing attempts: " + item["id"])
        if any(a["state"] not in ATTEMPT_TERMINAL for a in item["attempts"]):
            raise ValueError("Outstanding attempt: " + item["id"])


def require_retirable(inventory, jobs, identity, journal):
    require_drained(inventory, jobs)
    if any(d.get("agentId") == identity for d in inventory["definitions"]):
        raise ValueError("Definition still depends on retiring identity")
    if any(
        not d.get("agentId") and d.get("jobType") in {"COMMAND", "SCRIPT"}
        for d in inventory["definitions"]
    ):
        raise ValueError("Unpinned local definition requires targeting review")
    for item in inventory["instances"]:
        # Check all jobs, not only the selected batch, including old snapshots.
        if (
            item.get("pinnedAgent") == identity
            or item.get("agentId") == identity
            or (
                not item.get("pinnedAgent")
                and item.get("jobType") in {"COMMAND", "SCRIPT"}
            )
        ):
            if item["status"] not in TERMINAL or "attempts" not in item:
                raise ValueError("Identity still has outstanding instance")
            if any(a["state"] not in ATTEMPT_TERMINAL for a in item["attempts"]):
                raise ValueError("Identity still has outstanding attempt")
    if journal.get("identity") != identity or not journal.get("stopped"):
        raise ValueError("Inspect stopped owner's journal")
    if journal.get("pendingEntries") != 0 or journal.get("pendingChunks") != 0:
        raise ValueError("Journal still has unacknowledged results/output")
