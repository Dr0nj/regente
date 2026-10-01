# Shared resources and scheduler leadership

The durable runtime uses PostgreSQL for a shared control plane. A single admission
lock serializes reservation reconciliation, attempt transitions and quota changes;
throughput must be measured under the capacity qualification. SQLite remains a
single-node profile. These controls cover scheduling and reservations; they do
not qualify capacity, disaster recovery or a critical production pilot.

## Reservation ownership

A reservation records the order, resource quantity and executionId. Admission,
attempt creation and dispatch intent commit together. Resource capacity is read
from the database; node-local maps are not an admission authority. Updates made
through the Resources API on any node affect subsequent admission. Lowering a
quota below current usage blocks new work without terminating existing effects.
Deleting an in-use resource is rejected.

A known failed attempt retains its reservation during its scheduled retry delay.
The next attempt transfers ownership atomically and rechecks capacity. A receipt
from the previous attempt cannot release the new reservation. Terminal completion
or an audited resolution confirming that the effect stopped releases the current
attempt's reservation. A cancel request, lost connection or expired execution
lease does not prove that the physical process stopped: UNCERTAIN continues to
hold capacity until a valid receipt or explicit resolution.

Run Now keeps its explicit resource bypass. It still records resource usage and
cannot bypass the agent's admission limits or execution identity checks. Operators
must account for the possibility of overlap when using this bypass.

## Leadership and recovery

PostgreSQL leadership belongs to a dedicated session holding the scheduler's
advisory lock. It is not a time lease. Each acquisition publishes a monotonically
increasing term and the owning backend. Scheduling transactions check the term
and actual lock ownership, then hold the term row until commit. A successor cannot
publish its term until earlier authorized transactions end. A process with a
stale local leader indicator cannot authorize new work after succession.

Daily planning, chunks/carry-over, execution admission and deferred effect claims
use this gate. Agent receipts and authenticated operator actions can be processed
on a follower: execution identity and database transactions protect them independently.
An already committed dispatch or effect claim may finish after leadership changes;
failover does not retract a previously authorized physical effect.

Before admission resumes, each durable tick reconciles reservations from verified
runtime snapshots and current attempts. Missing reservations are restored;
conflicting ownership fails closed. Unknown or orphaned reservations are retained
for investigation, not deleted by a timeout. Database failure prevents admission.

## Agents and queue policy

Authenticated protocol-2 polls advertise concurrency, journal pending limit and
availability to the shared database. Presence becomes stale after 15 seconds
without a successful poll. Admission counts unresolved attempts against both limits;
UNCERTAIN consumes a slot. Agents attached to a follower remain eligible on the
leader. A disconnected agent receives no newly admitted work, and its existing
reservations remain intact. The agent journal independently enforces local limits.
Older protocol-2 agents without the new fields receive conservative one-slot,
one-pending admission until upgraded.

Eligible orders are considered by scheduled instant, creation instant and ID.
Blocked orders do not prevent another eligible order from using available capacity.
Agents with matching identity/environment/capabilities are selected by unresolved
load, then ID. This is deterministic work-conserving ordering, not a promise of
weighted tenant fairness or starvation freedom for large resource requests.

## Upgrade and validation

Schema 30 adds execution ownership to existing holds using the recorded current
attempt, the leadership term registry and shared agent capacity. Unlinked legacy
holds remain reserved with an empty owner for diagnosis. Upgrade all control-plane
binaries together; the supported runtime schema is exactly 30. Restore a complete
pre-upgrade backup with its matching binary for rollback. Do not run an older
server against schema 30 or remove an agent journal to reset uncertain work.

The mandatory integration runner exercises SQLite/PostgreSQL reservation contracts,
concurrent admission, retry ownership, cancellation/uncertainty, reconstruction,
agent limits and FIFO eligibility. PostgreSQL tests terminate the leader backend
while its local flag remains true and prove transaction/term ordering. A separate
process scenario runs two servers with a real agent on the follower, changes quota
there, interrupts PostgreSQL/NATS, kills the leader and checks stable execution
identity, exact reserve release and two isolated effects. Inspect its i11_ha report
alongside the same-SHA CI and release evidence before claiming validation.

References: [PostgreSQL locking](https://www.postgresql.org/docs/current/explicit-locking.html),
[lock ownership view](https://www.postgresql.org/docs/current/view-pg-locks.html),
[durable execution](durable-execution.md), [integration laboratory](integration-baseline.md).
