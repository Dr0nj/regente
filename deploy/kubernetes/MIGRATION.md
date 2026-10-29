# Kubernetes-04: gradual migration and route rollback

This rehearsal moves two synthetic jobs from a COMMAND agent to the SSH_REMOTE
bridge qualified in [phase03](SSH-BRIDGE.md). The origin container and external
sshd share the same script mounts and data volume, modelling two execution paths
to one destination. This is a local sidecar model, not proof of service removal
on a real VM. A separate COMMAND agent remains supported throughout.

## Quiesce the whole batch

Inventory definitions, targeting (including unpinned jobs), every retained business
date, frozen snapshots, attempts and agent journals. The default instance API
filters to today's business date: it is insufficient for retirement. Check all
pages/dates with admin visibility and establish independent journal receipt evidence.

Fence all order producers and concurrent operators before editing routes: automatic
schedules, events, On/Do actions, ingress, manual Force Order, Run Now and rerun.
`pauseFolder` only holds existing instances; it does not prevent future orders.
Changing `confirm` or a schedule affects new snapshots, not old instances.
Hold old waiting instances separately; allow active work to finish. Resolve
UNCERTAIN only after independent destination inspection. Cancel obsolete waiting
instances through the API rather than rewriting their frozen route.

The isolated harness is the sole producer, disables schedules and uses `confirm:true`
for all batch definitions. It explicitly confirms only chosen new instances.
This is not an application-wide maintenance lock or protection from concurrent
administrators. `migration_guard.py` checks supplied inventory, not its provenance
or freshness; a caller's `producersFenced` flag does not acquire a lock. Do not use
this helper as a production migration controller.

## Cut over, observe and roll back

1. Record the original definitions, script hashes, execution identities, result,
   output, destination effects and journal locations. Keep backups/access controlled.
2. Require no outstanding batch instances or nonterminal attempts. HELD/WAITING are
   still dependencies; terminal UI status cannot hide an uncertain attempt.
3. Change one definition to SSH_REMOTE, pin the bridge and authorize the exact
   destination/user/command/key paths in its policy. Keep Confirm enabled.
4. Create and confirm a new canary order. Verify its new snapshot, actual executor,
   stdout/stderr, exit code and independently observed effect. Old snapshots must
   remain byte-equivalent in their decoded content. Observe a nonmigrated job too.
5. Expand the batch only after the canary passes the same criteria.
6. For rollback, quiesce/drain again and restore original definitions for **new**
   orders. The rehearsal blocks rollback while any batch execution is uncertain;
   after independently observed completion and audited resolution, new origin
   orders are exercised. It never reruns the interrupted remote instance.

Rerun of an old instance retains its old frozen route. Neither cutover nor rollback
makes a lost receipt safe to replay. No exactly-once or remote-kill guarantee is made.
Keep destination-side evidence for deciding whether a deliberate new order is safe.

## Retire the origin identity

Do this only after the whole selected destination has migrated and its rollback
window is accepted. Search all definitions and retained snapshots, including jobs
outside the batch and unpinned capability/environment targeting. Stop the drained
sole journal owner; verify zero unacknowledged entries and output chunks. A terminal
server status alone is insufficient. Preserve the journal, tombstones, credential
metadata and result evidence under the applicable retention/access policy.

Disable the old service/restart path and revoke every credential for the retiring
identity. The isolated lab has one credential and a stopped container with no
restart policy. It proves valid mTLS plus token returns 204 before revocation and
401 afterward, then runs both remote jobs and the independent COMMAND agent.
Do not delete journals or issue replacement credentials to bypass uncertainty.
After revocation, a real rollback requires deliberately reprovisioning the origin
identity after review; the rehearsed route rollback occurs **before** retirement.

## Reproduce locally

Use the [phase03 prerequisites and images](SSH-BRIDGE.md), two ready kind nodes,
`python:3-slim` for read-only SQLite journal inspection and fresh phase04 state.
The dedicated proxy binds only loopback 12443; all HTTPS verification stays enabled.
The phase03 runtime is unchanged by this phase. Never reuse a failed evidence file
or reset journals to obtain a green run.

```powershell
$env:REGENTE_LAB_INSTANCE='regente-kubernetes-phase04'
$env:REGENTE_LAB_NODE_PORT='30447'
$env:REGENTE_LAB_URL='https://127.0.0.1:12443'
python deploy/kubernetes/migration_lab.py prepare
python deploy/kubernetes/migration_lab.py proxy
kubectl --context kind-regente-kubernetes -n regente-kubernetes-phase04-lab rollout status deployment/regente-server --timeout=120s
python deploy/kubernetes/migration_lab.py origin
python deploy/kubernetes/migration_lab.py external-agent
python deploy/kubernetes/migration_lab.py deploy
python deploy/kubernetes/migration_lab.py finalize
python deploy/kubernetes/verify_migration.py
```

Inspect `migration-verification.json` in the ignored `.integration/<instance>`;
it records every order before side effects and preserves failure status. Origin
credentials live under `origin/`; never publish them or the full private inventory.
Reports are one-shot, and partial failures require diagnosis rather than blind
reruns. Offline guard tests run in the verification runner and can be run alone:

```sh
python -m unittest discover -s deploy/kubernetes -p test_migration_guard.py -v
```

The observed qualification was resumed in segments with prior failed reports kept.
Harness corrections: zero exit code is omitted by the API; attempt creation is
asynchronous; journal files belong to UID 10001; a read-only WAL database may need
sidecar files; poll requires `protocol=2`; credential IDs are numeric. The stopped
owner's entire journal directory is copied from a read-only mount into private
tmpfs before SQLite inspection, allowing sidecars on the copy only. See SQLite's
[read-only WAL rules](https://www.sqlite.org/wal.html#read_only_databases).
No failed segment was relabelled passed and no already completed effect was rerun
to repair these checks. [Sanitized evidence](../../docs/evidence/kubernetes-04-local.json)
separates the aggregate qualification from an uninterrupted harness run.

AWS/EC2, fleet automation, HA, node failure, arbitrary script transfer and per-job
Kubernetes pods remain outside this phase. See phase05 in the product roadmap.

Source contracts: `server/internal/api/workflow.go` (folder pause),
`server/internal/scheduler/scheduler.go` (ForceOrder snapshots),
`server/internal/api/instances.go` (date filtering and confirmation),
`agent/journal/journal.go` (ACK and tombstones), and
[durable execution](../../docs/durable-execution.md) (uncertainty resolution).
