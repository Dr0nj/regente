# Shared Design drafts

Schema 30 stores each draft revision in SQLite/PostgreSQL, including its Git base,
working files, native Git objects, folder metadata and Mass Update undo stack.
An acknowledged edit has committed its snapshot and audit records in one database
transaction. Local session directories are reconstructible caches. Opening a draft
on another node requires the same database, Git source/branch and compatible build;
reading preserved content does not require a new clone or a reachable Git remote.
PostgreSQL is the supported shared database for multiple hosts. SQLite coverage is
single-host recovery and concurrency testing, not shared-file HA qualification.

## Ownership and editing conflicts

A draft belongs to its recorded actor. Other users cannot read, edit, publish or
discard it; an administrator can intervene and the acting identity is audited.
Folder read/write permissions are rechecked. Read endpoints filter inaccessible
folders; publication requires current write access to all opened/new folders.
Exports include the repository base and therefore require an administrator.

Create/get/content responses include a numeric `revision` in session metadata and
an `ETag` such as `"3"`. Every mutation of an existing durable draft must send
`If-Match: "3"`. Missing revisions return **428**; stale revisions return **409**.
This includes definition forms, delete, folders/layout, CODE, bulk, Mass Update,
undo, publication and discard. CODE/bulk still report partial item results, but the
accepted result is saved as a single revision before its HTTP success is sent.

The browser orders its own draft requests and sends the revision automatically.
Status polling does not advance its editing revision. A conflict pauses further
writes and keeps the editor open. Copy any unapplied edits, then explicitly reload
the shared draft before saving again. The server never silently retries a stale
write against a newer revision. Undo participates in the same snapshot/CAS and
survives node changes/restarts; it is limited to the last ten Mass Updates.

## Publication and recovery

Publication first checks the Git branch against the recorded base. If it advanced,
**409** preserves the draft. Review both versions: open a fresh session on the latest
base, copy/apply the reviewed changes through CODE or the forms, and publish that
session. Retain the old draft/export until the reviewed publication is confirmed.
There is no automatic force push, rebase or overwrite of another draft.

Before pushing, the prepared commit is durably recorded and the draft enters
`publishing`. New edits/discard are blocked. A retry uses that same commit after a
restart; a commit already present in the remote ancestry is recognized. Pull request
retries look up the session's unique head branch before creating another PR. A
publication receipt remains in the database after the session closes, so a repeated
publish can return its original result after a lost HTTP response. This does not
promise exactly-once GitHub/network effects or availability during a partition.

If a failed push is proven absent from the remote, an explicit
`POST /api/design/sessions/{sid}/publication/cancel` with the current `If-Match`
releases the draft for editing. If publication already reached Git, retry publication
to recover its receipt. When the remote cannot be read, retain the pending state and
export; never infer that an uncertain publication failed.

## Legacy migration and export/import

Schema migration preserves old metadata as `state=legacy`, `revision=0`. Boot/open
only migrates a clone whose resolved path is inside that node's actual session root.
It verifies the origin, base/HEAD, paths, object hashes and reconstructibility before
committing a shared snapshot. The original clone is retained. Missing/foreign or
unsupported clones remain listed with `recoveryRequired=true`; they are never
deleted merely because another node lacks their disk. Content access reports **503**
until recovery is possible. Restart the matching node with its original clone and
configuration, or recover a verified administrative export. Preserve the original
DB/files set before upgrading; `-migrate-only` does not import filesystem drafts.

Administrative endpoints:

| Method/path | Contract |
|---|---|
| `GET /api/design/sessions/{sid}/export` | Portable JSON envelope with format, actor, revision, compressed payload and SHA-256 checksum |
| `POST /api/design/sessions/import` | Submit that envelope; verifies checksum, source/branch, Git objects and paths; creates a new draft owned by the importing administrator |

Import never replaces an existing draft. Verify its actual content/base/dirty status
on another node before manually retiring old copies. Export files and DB backups
contain repository history and draft content: protect them as sensitive artifacts.
No PAT or `.git/config` is copied into the snapshot; credentials remain supplied by
the server's secret provider.

## Backup and limits

A consistent DB backup now includes migrated/new draft content, revision history,
undo and publication receipts. The mandatory laboratory restores actual unpublished
content into a different database/process with no original session directory, in
both SQLite and PostgreSQL. Legacy drafts still need their original clones or a
verified export. Published/local-only workspace changes and external configuration
remain separate artifacts: follow the [complete recovery guide](dr-backup.md).

Snapshots use native go-git; the server does not require the Git executable. Each
revision is a full compressed snapshot, bounded to **16 MiB compressed** and
**64 MiB expanded serialized payload**. Oversized, unsafe-path, symlink/special-file
snapshots fail explicitly while preserving the previous durable revision/original
legacy clone. Supported files are portable regular files and their modes. Revision
history remains until explicit discard/successful close; audit and publication
receipts survive close. There is no automatic history retention or claimed
throughput for large repositories. Measure DB growth, request cost and workload
limits in I16; broader DR/partition/pilot qualification remains open.

Upgrade all nodes together: the current runtime accepts exactly schema 30. Restore
a verified pre-upgrade recovery set with its matching binary for rollback; do not
point an older binary at the upgraded database.
