# Durable agent journal

The protocol 2 agent uses a local SQLite journal with WAL and FULL synchronization. A committed acceptance precedes the server acceptance ACK. A committed start marker and a current server start authorization precede the executor. Output and results remain in the journal until an authenticated receipt for the same execution identity is received.

Run with `regente-agent -transport v2 -server https://server.example -token ... -id worker -caps COMMAND -journal /var/lib/regente/agent.db`. Provision the exact job capabilities plus EXECUTION_V2. The journal belongs to one stable agent ID. Only one worker may own it, enforced by an OS file lock. Release builds report the release tag; source builds report dev. Never share a WAL database across hosts or copy it without its active WAL state.

The worker defaults to four concurrent effects and 1000 unresolved journal entries. Configure -concurrency and -max-pending within the documented flag bounds. Full admission stops dispatch claims while retaining cancellation delivery. Output is capped at 5 MiB per execution. Confirmed content is compacted after seven days; identity fingerprints and tombstones remain to reject changed duplicates. Unconfirmed content is never compacted.

A restart resumes an accepted execution that has no start marker. A persisted result is resent without executing again. A starting or running execution becomes uncertain, including when the process disappeared between a start marker and the actual effect. A PID is diagnostic evidence only. The worker does not infer that child processes or remote effects stopped when its process stopped. Reporting cancellation requires the executor to return; this does not roll back an external effect.

Network failures preserve the journal and retry the same identities. Storage failures stop new effects and preserve files for recovery. Unknown journal versions, changed agent identities, corrupt envelopes and integrity failures block startup. Ephemeral agents must mount persistent local storage for the entire unresolved execution lifetime; an in-memory replacement cannot provide these guarantees.

Protocol 2 uses the frozen definition's actionConfig field. Duplicate acceptance/start receipts include state, current and startAuthorized; a duplicate receipt from an old attempt never authorizes an effect. Agents report uncertain recovery through the authenticated ACK endpoint. The [I10 runtime](durable-execution.md) applies this contract to real orders, including internal HTTP/REST and SSH executors. The isolated I08 laboratory remains development-only.

See [SQLite synchronization](https://www.sqlite.org/pragma.html#pragma_synchronous) and [WAL constraints](https://www.sqlite.org/wal.html). Arbitrary scripts and HTTP endpoints do not become exactly-once systems because the journal is durable.
