# Safe migrations and the integration baseline

Status: accepted for I00/I01. Baseline: `b88af2a` (schema 23).

The migration unit is one version: its DDL, backfill, version record and SHA-256
checksum commit together. SQLite uses `BEGIN IMMEDIATE`; PostgreSQL uses a
transaction advisory lock scoped to database and schema. Each version reacquires
the lock and rereads history, so another process can safely finish an upgrade.
Locks are released by rollback or connection loss, without a lease file or timer.
This follows [SQLite transactions](https://www.sqlite.org/lang_transaction.html)
and [PostgreSQL advisory locks](https://www.postgresql.org/docs/17/explicit-locking.html#ADVISORY-LOCKS).

All existing migrations use transactional operations. The runner accepts only
transactional DDL/DML; nontransactional operations require a separate reviewed,
checkpointed procedure before they can be introduced. There is no unsafe fallback
to autocommit. SQL scripts currently cannot contain embedded semicolons.

Existing version records have no hashes. On first adoption they receive a
`legacy-adopted` provenance; this records the expected baseline, **not proof of
the SQL originally executed**. Adoption is atomic. Subsequently missing hashes,
gaps, unknown versions or changed SQL stop startup. Checksums normalize CRLF to
LF only. Never edit an applied migration; add a new version.

Schema 0–23 can upgrade; the current runtime requires exactly schema 24. The
I02 credential contract retires legacy secrets and requires explicit reissue;
see [the identity upgrade procedure](../agent-identity.md). Future
releases must explicitly update the supported runtime range and test overlap.
These checks run before authentication bootstrap, workspace initialization,
scheduler or HTTP serving. Binaries predating this runner cannot enforce them:
stop all old nodes before the first upgrade. No mixed-version rolling-upgrade
promise is made for those binaries.

Expand/contract: add compatible structures first, backfill transactionally (or
via a separately versioned resumable job), move readers/writers only after
verification, and remove old structures in a later release after the rollback
window closes. Backup and tested restoration precede contract changes. Do not
delete history rows, patch checksums, or run destructive down migrations to
recover an interrupted upgrade. Restore a pre-upgrade backup into an isolated
database if the old autocommit runner left a partial migration.

The integration lab uses disposable PostgreSQL, NATS and Keycloak plus two real
server processes and two real agents. Mandatory CI checks dependencies and rejects
skipped selected integration tests. Release publication depends on this same gate
for the source ref being released. It is a synthetic engineering baseline; capacity,
security remediation and production qualification remain separate roadmap gates.
