# Failure-resistant audit

The mandatory security ledger is separate from operational logs. From schema 32 onward, runtime database mutations commit their ledger record and delivery entry in the same transaction. An audit write failure or a full security backlog rolls back the mutation. A successful database commit survives an immediate process crash without a shutdown flush.

## Coverage and data policy

The database adapter covers identities, sessions, credentials, ACLs, settings, definitions/drafts/publications, daily orders, runtime transitions, execution attempts, effects, conditions, resource holds and recovery decisions. Records contain the request actor, route, correlation ID, statement fingerprint, affected row count and bounded identifiers. Prepared batches produce one aggregate per statement per transaction. SQL text, bound values, passwords, tokens, output and definition payloads are not exported.

Presence, heartbeat capacity, transient authentication challenges, web tickets, output, SLA/alert feeds, migration markers and legacy instance events are operational telemetry. The legacy audit feed and stderr sink remain historical diagnostics. Their retention/export does not establish the mandatory ledger's guarantees.

API mutation requests persist an intent before entering the handler. Database commits carry the same request correlation ID. Responses and access denials are recorded before being returned; a failed audit write returns 503. Git and filesystem effects cannot share a database transaction: the durable intent is the admission record, I12 publication receipts remain the recovery evidence, and an interrupted request may have an unknown external outcome. Reconcile the receipt/Git state before retrying an external effect. Multi-step legacy operations can be partially completed, with each committed database step recorded.

## Keys, export and independent permissions

SQLite defaults to a signing key beside the database: `state.db.audit.key`. PostgreSQL requires `-audit-key` / `REGENTE_AUDIT_KEY` explicitly; all nodes of one stream need the same protected key. Initialize the empty stream on one node before starting peers. The key is Ed25519, stored outside the database, and must be backed up separately. A missing, mismatched or altered key refuses startup. Unix key files require owner-only permissions; Windows development deployments require operator-managed NTFS ACLs and do not claim the installed Linux permission proof.

```sh
regente-server -db /var/lib/regente/state.db -audit-verify
# Copy only the .pub file to the independent collector host.
sudo bash /var/lib/regente/deploy/audit/install-audit-collector.sh /secure/source.pub /secure/collector-token /secure/tls.crt /secure/tls.key 127.0.0.1:9445
```

The installer creates `regente-audit` with its own private directory and hardened systemd unit. It receives the public signing key, a separate bearer token of at least 32 characters and a TLS certificate/key. The control plane must not own or have read/write permission on the collector journal. Prefer a different host/account and protect off-host backups. Root or compromise of both hosts is outside this guarantee.

Configure the producer with `REGENTE_AUDIT_SIEM_URL=https://collector.example:9445` and `REGENTE_AUDIT_TOKEN_FILE=/etc/regente/audit-token`. This URL now requires the Regente collector protocol; generic SIEM endpoints returning a 2xx response are not ACKs. The application emits signed JSON records in order. The collector verifies sequence, previous hash, content hash and signature, appends and syncs its journal, then returns `{"stream":"...","seq":1,"hash":"..."}`. Lost receipts retry the same identity; duplicate records return the original receipt. Redirects are rejected. Production requires HTTPS and normal certificate verification.

Transport/5xx/429 failures retry with bounded exponential delay. Invalid ACKs and permanent 4xx responses enter dead letter; 20 unsuccessful attempts also stop delivery. No cursor skips the failed record. An administrator fixes the destination and calls `POST /api/audit/security/retry` to reset the oldest pending item; this action itself is durably recorded. Do not edit delivery rows manually in production.

## Capacity, retention and observation

`-audit-capacity` defaults to 100000 unacknowledged security records. Access denials have a separate capacity of 1000 and control intents/results have 10000. A full category refuses new records in that category. Output and operational telemetry never consume those budgets. Delivery is at least once; acknowledgement only changes transport state and never deletes the signed record. With no destination configured the local ledger remains durable and eventually refuses work at the configured capacity.

`GET /api/audit/security?after=SEQ` returns up to 500 signed records. `GET /api/audit/security/status` reports the head, pending count, dead letters and full integrity verification. Both require administrator access. Alert on pending growth, dead letters and failed integrity. Prometheus exposes `regente_audit_pending`, `regente_audit_dead_letters`, `regente_audit_oldest_pending_seconds` and a storage-unavailable gauge on read failure. Alert when lag exceeds the installation budget or dead letters are nonzero. The exporter currently drains one record per second; transaction serialization and export throughput are explicit capacity limits to measure in I16, not an enterprise throughput claim.

Mandatory ledger and collector retention is indefinite. The operational `audit_retention_days` setting does not purge either ledger or unacknowledged outbox entries. Archive complete verified snapshots to protected storage according to the installation's retention policy. Online pruning, key rotation and multi-stream collector rotation are not implemented; plan disk capacity and use one collector journal per stream. Do not delete prefixes or claim a shorter retention guarantee without an implemented archive/checkpoint protocol.

## Integrity and restore

Each signed record links to its predecessor. Startup and verification detect changed content, missing records and head inconsistencies. A database-only attacker cannot forge a new signature without the external private key. A complete rollback to an older internally valid database needs comparison with the independent collector checkpoint; local verification alone cannot detect that rollback.

Save the collector's last verified receipt (`stream`, `seq`, `hash`) independently of database backups. Restore the database, protected signing key and collector journal into new targets. Verify before admitting jobs:

```sh
regente-server -db restored.db -audit-key /secure/restored-signing.key -audit-verify -audit-checkpoint /independent/checkpoint.json
# PostgreSQL: use -db-driver postgres -db "$RESTORED_DSN" with the same flags.
```

The database must include the checkpoint record with its exact hash. If a backup predates the collector checkpoint, restore a newer database or perform explicit incident recovery; do not reset the collector to hide a gap. An incomplete collector tail refuses startup and requires a verified recovery copy. Database backups, the signing key and collector backups have separate permissions and custody. Existing pre-I14 history is not retroactively signed.

The mandatory gate proves SQLite/PostgreSQL rollback on storage failure, full backlog refusal, immediate process exit after commit, ACK loss/deduplication, invalid ACK dead letter, alteration detection and collector restore. Real processes additionally prove SIGKILL during a destination outage and PostgreSQL pg_dump/pg_restore against an independent checkpoint. The installed Linux smoke proves separate UIDs, inaccessible signing key/journal, TLS, outage and crash recovery.
