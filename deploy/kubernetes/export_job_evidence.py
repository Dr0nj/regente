"""Export only synthetic qualification metadata; never export Secret objects."""

import datetime as dt
import json
from pathlib import Path
import verify_job_cell as v


def export():
    v.lab.login()
    report = v.load()
    required = (
        "pod-ok",
        "pod-fail",
        "pod-boundary",
        "controller_restart_same_job_pod",
        "pod-cancel",
        "pod-timeout",
        "api_access_restored_without_resubmission",
        "pod_loss_no_job_recreation",
        "node_unavailable_uncertain_no_job_recreation",
        "policy_and_rbac_denied_without_job",
        "configured_retry_distinct_execution_and_job",
        "final_binary_success",
    )
    assert all(report["checks"].get(k) is True for k in required)
    ids = set(report["orders"].values()) | set(
        report.get("previousValidatedOrders", {}).values()
    )
    for key in ("harnessCorrections", "observedFailures"):
        ids.update(x["order"] for x in report.get(key, []))
    orders = []
    for i in sorted(ids):
        row = v.row(i)
        attempts = v.lab.api("/api/instances/" + i + "/executions")["attempts"]
        assert row["status"] in ("OK", "NOTOK", "CANCELLED"), row["status"]
        assert all(a["state"] in ("succeeded", "failed", "cancelled") for a in attempts)
        orders.append(
            dict(
                id=i,
                status=row["status"],
                exitCode=row.get("exitCode"),
                attempts=attempts,
            )
        )
    jobs = [
        dict(
            name=j["metadata"]["name"],
            uid=j["metadata"]["uid"],
            execution=j["metadata"]["annotations"].get("regente.io/execution"),
            conditions=j["status"].get("conditions", []),
        )
        for j in v.objects("jobs")
    ]
    data = dict(
        status="passed",
        scope="local kind v1.36.1; segmented qualification",
        generatedAt=dt.datetime.now(dt.timezone.utc).isoformat(),
        checks=report["checks"],
        orders=orders,
        jobs=jobs,
        harnessCorrections=report.get("harnessCorrections", []),
        observedFailures=report.get("observedFailures", []),
        limitations=[
            "No AWS, production fleet, HA or exactly-once guarantee",
            "Node unavailability simulated by pausing only the dedicated worker container",
            "API access outage simulated by temporary RoleBinding removal",
            "ACK/retention/delete/tombstone verified by deterministic journal plus TLS API tests; no one-hour real retention wait in this qualification",
            "Token re-read tested against TLS fixture; projected token configured in cluster",
            "Logs fetched at completion, bounded to 1 MiB; no live application log streaming",
            "Firewall init is a trusted Pod Security exception; no Restricted/PID-limit certification",
            "Interrupted harness assertions and original deadline failure retained; no claim of a single uninterrupted run",
        ],
    )
    target = (
        Path(__file__).resolve().parents[2] / "docs/evidence/kubernetes-05-local.json"
    )
    target.write_text(json.dumps(data, indent=2) + "\n")
    print(f"Exported {len(orders)} orders and {len(jobs)} Jobs to {target}")


if __name__ == "__main__":
    export()
