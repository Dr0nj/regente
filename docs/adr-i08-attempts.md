# ADR: Durable attempt contract in the I08 laboratory

Status: implemented for development laboratory validation. Production activation is unavailable until I09 and I10 integrate the agent journal, effects and executor paths. E04 remains open.

## Context and decision

The existing runtime assigns an agent before transmission, but instanceId is reused across retries and reruns. Its transport delivery is not durable acceptance, and result/output do not identify a particular attempt. I08 introduces a server contract with durable identities and transactional dispatch intent.

The laboratory uses separate lab_orders, execution_attempts, execution_outbox, execution_output and execution_events tables. A laboratory order copies a verified order snapshot and has its own ID. Creating or completing it never changes the source instance, condition pool, legacy statistics, retries or On/Do actions. This boundary is necessary while journal and recovered effects are unfinished.

Each attempt freezes executionId, attempt number, assigned agent, fencing generation and idempotency key. An order row lock serializes all transitions. Attempt creation, current-order identity, planned event and dispatch outbox are one commit. Queue admission also takes a database queue lock to enforce the global pending limit across nodes. SQLite and PostgreSQL share the transaction contract.

## State machine

| Event | Attempt state | Order state / outbox |
|---|---|---|
| Start | dispatch_pending | active; dispatch pending |
| Poll claims message | dispatching | dispatch leased; transmission does not imply acceptance |
| Agent persists acceptance and sends ACK | accepted | dispatch acked; execution lease begins |
| Agent reports start | running | same execution and fence; lease renewed |
| Matching result is committed | succeeded / failed | same terminal order result; all attempt messages acked |
| Cancel before any delivery | cancelled | cancelled; dispatch cancelled |
| Cancel after possible delivery | cancel_requested | uncertain; durable cancel message |
| Agent confirms cancellation | cancelled | cancelled; messages acked |
| Accepted/running/cancel lease expires | uncertain | uncertain; no new attempt automatically |
| Delivery retry limit reached | uncertain | dispatch paused; no new attempt automatically |
| Verified late result for current uncertain attempt | succeeded / failed | terminal result recorded |
| Explicit retry/rerun after proven terminal state | new dispatch_pending attempt | new executionId and higher attempt/fence |

Lease expiration does not prove that a process stopped. Heartbeats can renew contact without erasing an uncertain outcome. Unknown outcomes cannot be rerun by the laboratory API. Operator resolution and external-effect reconciliation belong to I10. Retry is allowed after failed; rerun after succeeded, failed or confirmed cancelled. A pending cancellation does not authorize another execution. A result may win a race with a cancellation request; a confirmed cancellation is terminal and rejects subsequent results.

## Outbox and acknowledgements

HTTP pull claims durable messages for the authenticated assigned agent. Claim commits before response transmission. Losing a response or notification does not lose the record: expired delivery claims become eligible again. Redelivery keeps the same messageId, executionId, fencing and idempotency key. The execution fence increases only for a new attempt, not a transmission retry.

Defaults: 1,000 unresolved attempts (including accepted and uncertain work), 30-second delivery lease, five-minute execution lease, ten delivery attempts. Retry delay is persisted and increases to at most five minutes. Exhaustion records uncertainty instead of reporting execution failure. Queue backpressure returns 429 without creating a partial attempt. Each unresolved attempt has at most one dispatch and one reserved cancellation message, so cancellation remains available when admission is full. Terminal attempts release admission capacity. Pending cancellation messages remain recoverable after restart. Notifications are optional hints; the database is authoritative. Current Core NATS and legacy hub paths do not carry v2 work.

Acceptance ACK closes dispatch delivery only after its durable transaction. Results are matched by executionId + authenticated agent + fence and committed by compare-and-swap with current order identity. Same-content duplicates return a duplicate receipt; conflicting repeats return 409. Result, order state, event and outbox acknowledgement commit together. The receipt is sent only after the commit.

Output uses executionId and a monotonic sequence, not the attempt currently visible on an instance. Duplicate sequence/content is acknowledged without appending; changed content, gaps, wrong agent/fence or new chunks from an old terminal attempt are rejected. Previously committed duplicate chunks can still be acknowledged after rerun. The per-attempt byte limit is persisted at 5 MiB and survives restart. Output and final result text remain separate records.

## Protocol and coexistence

Version 2 currently has only the HTTP laboratory transport. Poll handshake requires protocol=2 plus the provisioned id/env/caps claims and EXECUTION_V2 capability. Agent ID in result/output bodies is forbidden; the server derives identity from the machine credential. Payloads include protocol, kind, messageId, executionId, orderId, attempt, agentId, fence and idempotencyKey. Dispatch includes the frozen definition.

| Pair | Supported behaviour |
|---|---|
| Existing server and v1 agent | Existing WS/HTTP/SSE/NATS runtime; no new durability claim |
| I08 server, laboratory disabled, v1 agent | Same v1 runtime |
| I08 server, laboratory enabled, synthetic v2 client | Isolated HTTP contract; durable server receipt only |
| I08 server and current v1 agent on v2 endpoint | Rejected: protocol/capability mismatch |
| Production profile with laboratory flag | Startup rejected; handlers also refuse laboratory access |
| I08 server and future I09 agent | Must negotiate v2 and persist acceptance/result before enabling integration; not yet qualified |

The server cannot promise execution deduplication without the I09 journal. Arbitrary scripts do not provide exactly-once external effects. Server receipt durability, agent effect durability and recovered business effects are separate acceptance gates.

## API and operation

Enable only on a disposable development installation with -execution-lab or REGENTE_EXECUTION_LAB=1. The existing agent binary is not a v2 executor. Use synthetic clients and isolated machine credentials.

Administrative endpoints require admin authentication:

- POST /api/lab/orders: sourceInstanceId and idempotencyKey; copies a verified snapshot.
- GET /api/lab/orders/{id}: current identity/state.
- POST /api/lab/orders/{id}/attempts: agentId, idempotencyKey and intent (start/retry/rerun).
- POST /api/lab/orders/{id}/cancel: cancellation according to the state matrix.
- GET /api/lab/executions/{id}: attempt identity/state and contact timestamps.
- GET /api/lab/executions/{id}/output?after=0: at most 100 sequenced chunks; empty arrays are [].
- POST /api/lab/reconcile: expire execution leases into uncertainty. Poll also runs reconciliation.

Machine endpoints use scoped machine Bearer credentials:

- GET /api/agent/v2/poll?id=...&env=...&caps=COMMAND,EXECUTION_V2&protocol=2: message or 204.
- POST /api/agent/v2/ack: protocol, executionId, fence and kind (accepted/started/heartbeat/cancelled).
- POST /api/agent/v2/result: protocol, executionId, fence, exitCode and output.
- POST /api/agent/v2/output: protocol, executionId, fence, seq and chunk.

All persisted contact timestamps and leaseUntil use UTC Unix milliseconds. Metadata reads do not expose command snapshots or final output; output has its authenticated endpoint. Unknown fields and trailing JSON are rejected. Storage failure returns 503 without claiming receipt; retry the same identity/key. Result conflicts return 409, output limits 413 and queue backpressure 429.

Prometheus gauges expose regente_execution_outbox by pending/leased/paused state and regente_execution_uncertain. regente_execution_deliveries_total counts durable delivery claims. No command or credential is a metric label.

## Upgrade and remaining work

Schema 27 adds isolated tables without reconstructing identities for historical instance_runs or outputs. Legacy data is untouched. Older schema-26 binaries reject schema 27; rollback requires a verified compatible backup. The laboratory is disabled by default after upgrade.

I09 must provide durable agent acceptance, dispatch deduplication and pending result journal. I10 must recover conditions/post-actions, uncertain outcomes, operator decisions and internal executors. Laboratory success does not close E04 or qualify HA, DR or capacity.
