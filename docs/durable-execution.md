# Durable execution and recovery

Production uses protocol 2 and schema 29. Development defaults to legacy execution unless explicitly configured with REGENTE_EXECUTION_MODE=durable or -execution-mode durable. New Linux service installations select durable mode. A database bound to durable mode cannot switch back to legacy. The isolated I08 laboratory remains development-only and cannot start or cancel runtime orders.

## Upgrade a running installation

1. With the previous server still running, prevent new orders/dispatch and drain every legacy RUNNING execution. Resolve externally interrupted operations before upgrading. The new server refuses durable binding while a legacy running order remains.
2. Stop the server and agents. Save a complete verified recovery set: database, workspace, unpublished design sessions, configuration, credentials and every agent/internal executor journal with its WAL state. Use the documented database backup tools; a live file copy is insufficient.
3. Upgrade the server and agents together. Schema 29 is an additive migration; the runtime accepts exactly schema 29. Existing development service configuration is preserved by installers: select durable mode explicitly when converting it. Production selects durable mode automatically and rejects legacy mode. Mixed old/new runtime binaries and schema downgrade are not supported.
4. Provision each machine credential with the exact agent ID, environment and capabilities, including EXECUTION_V2. Start the agent with -transport v2 -journal PATH and the provisioned job capabilities. The agent adds EXECUTION_V2 to its handshake. Source binaries report dev; release binaries report their tag. v2 runtime polling requires protocol 2, journal version 1 and a build version. Production rejects v1 machine execution.
5. Keep the same journal and stable agent ID across upgrades. Linux installers use /var/lib/regente-agent/journal.db, macOS uses /Library/Application Support/RegenteAgent/journal.db, and Windows uses ProgramData/RegenteAgent/journal.db with SYSTEM/Administrators ACLs. Linux credentials remain in a protected environment file; Windows uses -token-file with a protected credential file. An existing active service is restarted to load the new binary.
6. Verify a synthetic job, fleet presence, Execution metadata, sysout and a controlled recovery before reopening dispatch. Restore the complete pre-upgrade set with its matching binary if rollback is necessary. Never point an older binary at schema 29 or delete a journal to make startup succeed.

## What is committed

A real order creates its current execution, monotonically increasing generation/fence and dispatch intent in one transaction. A committed agent acceptance precedes acceptance ACK. A local start marker plus current server start authorization precede external execution. The immutable daily snapshot, business timezone, ODAT and daily-completion gates remain authoritative. CONFIRM and configuration gates apply to Force Order; Run Now bypasses normal dependency/schedule/resource checks while recording actual resource holds, but cannot bypass configuration, snapshot integrity or an incomplete daily.

A confirmed result commits instance state, attempt history, local variable updates, conditions and post-action intents together. Duplicate results are acknowledged without applying them again. A late result cannot finish a later generation. Retry budgets and cyclic schedules remain business state; execution identities never reset on rerun. A known failure may schedule a retry; an unknown outcome never does.

Notify, run-job, runtime actions, SLA webhooks and global variable assignments have stable effect IDs. Run-job materialization and its receipt are one database transaction. External delivery claims are committed before sending and receipts are committed after completion. An expired claimed effect becomes uncertain and is never automatically repeated. Notification payloads/destinations are frozen privately; UI/API expose descriptors and reasons, not destination secrets. Partial delivery to several channels is uncertain as a whole; manually repeating can duplicate channels already delivered.

Resource holds live in the database and survive unknown effects, restart and retry. Cross-node quota reads and deletion consult those holds. UNCERTAIN carries across daily rollover, cannot be overwritten by Hold/Set OK/Rerun/Delete, prevents archive of the unresolved day, and keeps the daily report open.

## Recover an unconfirmed outcome

The Execution tab and GET /api/instances/{id}/executions show execution ID, attempt, fence, agent, acceptance/start/contact times, reason and post-actions. Output remains available per execution generation, including pending/uncertain chunks. Read access follows folder ACLs. Resolution requires a writer with access to the frozen instance folder.

Verify the external operation and ensure it has stopped before recording succeeded, failed or cancelled. Supply a reason, current fence and stable idempotency key to POST /api/executions/{id}/resolve. The decision and actor are audited in the same transaction. The same key/content replays its receipt; conflicting decisions, stale identity or changed content are rejected. Resolution does not automatically rerun the job.

For an uncertain/pending post-action, POST /api/execution-effects/{id}/resolve requires its current generation, reason, stable key and confirmation that the operation has stopped. Decisions are done, cancelled or retry. Retry additionally requires classification idempotent, verified-absent or duplicate-risk-accepted and explicit acceptance of duplicate risk. Both old workers and stale operator forms are fenced out. An unavailable run-job target remains pending until corrected or explicitly resolved; it consumes bounded admission.

CLI equivalents:

~~~sh
regente ops executions INSTANCE -server URL -token SESSION
regente ops resolve-execution EXECUTION -fence N -key REQUEST -decision failed -reason 'External operation stopped; evidence checked' -effect-stopped
regente ops resolve-effect EFFECT -generation N -key REQUEST -decision retry -reason 'Destination deduplicates this effect ID' -classification idempotent -accept-duplicate-risk -effect-stopped
~~~

## Limits of the guarantee

The server outbox and local journal preserve identity and evidence; they cannot make arbitrary commands, remote systems or multi-channel notifications exactly once. A running agent that restarts reports uncertainty instead of replaying. HTTP/REST receipt loss, OpenSSH transport/deadline loss, and SSH exit status outside 0..254 remain uncertain. Windows OpenSSH can return UINT32_MAX on missing remote receipt. Mutating external adapters without a confirmed completion also preserve uncertainty on failure rather than using normal retry.

Cancel acknowledges the local executor returning after a stop request; it does not undo remote work. Unix cancellation targets the local process group. Windows kills the direct process and bounds pipe waiting; descendant or remote completion requires operator verification. Timeout alone is not evidence that an external operation stopped.

Journals use SQLite WAL/FULL on persistent local storage, one OS-locked owner per stable agent ID. Network/shared filesystems, journal sharing, ephemeral disks, agent replacement without its unresolved journal and storage devices that do not honor synchronization are outside this guarantee. The default bounds are four concurrent effects, 1000 unconfirmed journal entries and 5 MiB output per execution. Admission preserves cancellation. Confirmed journal contents compact after seven days while identity tombstones remain; unconfirmed contents never compact. Server execution/effect audit records currently have no automatic retention deletion; plan storage accordingly.

The SERVER-AGENT HTTP/REST and SSH executors use the same journal/receipt contract under a stable node ID and persistent workspace runtime directory. Production still requires the explicit control-plane execution policy. This increment does not qualify distributed HA, shared-storage durability, capacity targets or a customer pilot; those remain separate roadmap items.

See [agent journal](adr-i09-agent-journal.md), [production profile](production-profile.md), [conditions](conditions-events.md), [business time](business-time.md), [daily recovery](daily-recovery.md) and [integration baseline](integration-baseline.md).

## Shared quotas and leadership (I11)

See [shared resources and leadership](ha-resources.md) for reservation ownership, failover, agent admission limits, queue policy and schema 29 upgrade. Quota reduction does not stop active effects; disconnected or UNCERTAIN executions retain their reservations.
