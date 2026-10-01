# Business time

Regente uses one injected clock and an explicit IANA business timezone. The default is UTC with a 00:00 rollover. The operating system and browser timezone do not select the active daily. Set daily_timezone and daily_at in Settings before ordering jobs.

## Calendar contract

A business date D starts at daily_at on calendar date D in the configured zone. Times before daily_at belong to calendar date D+1. With rollover 06:00, a 02:00 job for D runs on the following morning. A window whose end precedes its start continues across midnight. Equal start/end is a single boundary, not a 24-hour window.

All instants are stored in UTC. New daily, Force Order and Order Folder snapshots freeze the timezone and rollover. Settings changes affect new orders and the active daily selection; existing orders retain their original timing context and ODAT. Carry-over advances the active order_date only; scheduling windows and SLA remain anchored to the original ODAT. Retry delays are elapsed durations, including delays longer than a day.

## Daylight saving transitions

A repeated wall time resolves to its first occurrence. A nonexistent wall time resolves to the first valid instant after the gap. The same resolver handles rollover, schedule, windows, forecast and SLA. Business days are calendar intervals, not fixed 24-hour durations. These are explicit product rules: Go time.Date does not guarantee a particular choice during a transition ([Go documentation](https://pkg.go.dev/time#Date)).

## Existing orders and upgrade

Pre-I06 snapshots have no provable timezone. Their stored timestamps and ODAT remain unchanged. They are shown as legacy / timezone unknown. A legacy order requiring a wall-clock window is blocked with CONFIGURATION_BLOCKED; reorder it under explicit settings to obtain a frozen context. Legacy deadline SLAs cannot be reconstructed and are not recalculated; elapsed-duration SLAs still apply. Run Now retains its explicit operator bypass of schedule gates. No automatic timezone backfill invents historical evidence.

Invalid timezone or rollover settings are rejected. Empty timezone on a new settings write is normalized to UTC. Existing invalid values block new materialization until corrected. Existing empty settings use the documented UTC default; review this setting before upgrading a server that previously relied on host-local time.

Forecast and What-if project new orders using current settings and return their timezone and rollover. Monitoring displays each order's frozen zone. Offline browser mode remains a local simulation.

Automatic startup does not backfill a missed daily before today's rollover; pending cycles resume their frozen plan under the [daily recovery contract](daily-recovery.md).
