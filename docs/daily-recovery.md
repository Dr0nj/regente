# Daily recovery

A daily is a persisted materialization cycle, separate from the execution of its jobs. New cycles progress through planning, materializing and completed. An error records failed, the last committed checkpoint and a diagnostic. A daily is completed only after reconciling its frozen plan with its order ledger.

## Frozen source and atomic chunks

Before inserting orders, Regente stores the eligible plan, its target Git commit and a checksum. Git definitions are read from immutable commit objects; offline mode freezes the loaded definitions. Schedule/calendar eligibility and business time are frozen at planning. A later publication or settings change cannot replace the plan during recovery.

Orders are inserted in chunks of up to 5,000. Each transaction includes the orders, their ordered events, ledger entries and checkpoint. Failure rolls the whole chunk back. Restart resumes from the last committed checkpoint, using the same plan. Database serialization and unique daily/ordinal and daily/definition ledger keys protect concurrent attempts. Force Order remains a distinct manual order.

The plan is held in memory and persisted as one JSON payload. Chunked order writes do not constitute capacity qualification; large plans require the separate capacity validation described in [capacity guarantees](capacity-guarantees.md).

## Operation

GET /api/daily/status returns the current run and the oldest pending cycle, with state, commit, expected/inserted counts, checkpoint and error. lastRunDate/lastRunAt refer to the last completed or legacy cycle. Monitoring shows an incomplete daily and offers Resume daily to writers. POST /api/daily/resume with {"orderDate":"YYYY-MM-DD"} resumes an existing cycle; it never replans it. Completed cycles are idempotent.

The automatic scheduler recovers pending cycles before opening another daily, including a restart before rollover. A recovery failure prevents the next cycle from starting. A new empty workspace does not mark a day; loaded definitions that are all excluded by scheduling produce a legitimate empty plan. Carry-over updates and events are atomic, with its completion recorded in the same transaction.

Orders created by an incomplete cycle remain WAITING with CONFIGURATION_BLOCKED in Explain, including Run Now. A missing or checksum-mismatched snapshot on a verified daily order also blocks execution. A corrupt order snapshot also blocks execution and never falls back to live code. Restore verified state or reorder the affected job after investigation. A corrupt plan stays failed and cannot be rebuilt silently from current definitions. Retention and daily report delivery do not treat incomplete materialization as completed.

The ledger records ordering, not continued existence of an order. Deleting an order after its checkpoint does not make recovery recreate it.

## Upgrade

Schema 26 adds cycle metadata and the order ledger for SQLite and PostgreSQL. Verify a backup before upgrading. Older binaries reject this schema: rollback requires restoring a verified compatible backup.

Historical daily rows become legacy, preserving their dates and timestamps. No expected count, completion proof or frozen plan is invented. They cannot be resumed as a new cycle; inspect and explicitly reorder missing jobs. An absent order snapshot retains documented legacy compatibility; a present invalid snapshot is corruption, not legacy.
