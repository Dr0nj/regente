# Conditions — behaviour specification (SINGLE SOURCE OF TRUTH)

> **READ THIS WHOLE FILE before touching any code that deals with dependencies,
> conditions, rerun, Set OK or Force.** It is the single source of truth for the
> semantics; the roadmap records *when* each rule landed, but the canonical
> behaviour is defined here. If a patch contradicts this document, either the
> patch is wrong or this document has to change WITH it (same commit, with the
> reasoning).

## The single model (2026-07-17)

**Every dependency is a named CONDITION in a global POOL** (the classic global-condition
model). There are no longer two systems ("graph arrows" versus "F16
conditions") — the user report that unified them: *"all dependency conditions
have to be one single thing architecturally; whether you wire it through the box
or type it by hand, the conditions go to the same place"*.

The pool is the `conditions` table (`name`, `scope_date`, `set_at`, `set_by`):

- `scope_date = 'YYYY-MM-DD'` → a condition for that daily (ODAT);
- `scope_date = ''` → permanent/static (`@stat`).

Each job declares three lists (Design drawer → **Conditions** tab; YAML;
mass update; MCP):

| field | role | when it acts |
|---|---|---|
| `conditionsIn` | **in** — depends on | gate: the job only runs when ALL of them exist in the pool, in the scope resolved from the suffix against ITS OWN ODAT |
| `conditionsOutAdd` | **out＋** — adds | when it ends **OK or on Set OK** |
| `conditionsOutRemove` | **out−** — deletes (CONSUMPTION) | when it ends **OK or on Set OK** |

**The Design arrow is UI sugar.** Wiring A→B creates the condition
`LinkCondName(A,B) = "A-TO-B"` (uppercased ids): **out＋ on A**; **in AND out−
on B** (the deletion is automatic on the link — the user's rule). Typing the
SAME names by hand lands in exactly the same place; whoever wires by hand has to
put the name **in the in-list AND in the out− list** to get consumption
semantics (without out−, the condition survives the OK — deliberate N:M fan-out).

Arrow ergonomics (2026-07-17, report "I drag and it doesn't create the
dependency"): the dot has an 18px hit area (the visual nub stays 6px), the drop
has a magnet (`connectionRadius`) and **dropping on the other job's CARD
connects** (the `onConnectEnd` fallback — you do not have to hit the handle).
The DIRECTION follows the dot you started from: the **bottom** one = this job
produces for the target; the **top** one is inverted — the dragged job now
DEPENDS on the target. The fields written are the same in both gestures
(invariant 9). The Design drawer **AUTOSAVES** (switching jobs or closing saves
whatever was dirty; a brand-new job still requires Save) and receives the LIVE
definition, merging by diff any conditions written from outside — an open drawer
never again overwrites the link the arrow just created.

**Who creates or removes a condition in the pool:**
1. a job ending **OK / Set OK** (out＋/out−) — `ApplyOutcomes`, on the producer's
   ODAT;
2. an **On/Do `set-condition`** action (e.g. on NOTOK — this is how legacy
   `on-failure` edges are expressed);
3. the **operator**, through the Monitoring **Conditions** panel (the button next
   to Organize — list/add/delete with a date) or the API/MCP;
4. an external event (`POST /api/events/ingest`).

Every change to the pool emits the WS event **`condition.changed`** (the panel
and the graph edges are a live reflection) and nudges the tick (whatever was
waiting runs RIGHT AWAY).

## Dates — ODAT / PREV / STAT

**The day turns at `daily_at`, not at midnight.** Every date below is a BUSINESS
date: the day stays the same until the clock crosses the configured daily time,
which is the only moment a new `order_date` is materialised. With `daily_at`
at 15:00, day D runs from 15:00 on D to 14:59 on D+1 — an order placed at 00:10
belongs to D, exactly like one placed at 23:50. The default `daily_at` is 00:00,
where the business date equals the calendar date. The server is the single
source of this date (`Scheduler.BusinessDate`, exposed by `GET /api/daily/status`);
the UI never derives it from the browser clock.

**ODAT** = the order's ORIGIN date (the classic ODATE):
`ODAT = COALESCE(carried_from, order_date)` (`scheduler/odate.go`). Carry-over
advances `order_date` while preserving the origin — **every date scope uses
ODAT**, never the advanced active day (a job carried from the 14th creates and
looks for conditions FROM THE 14th).

A condition's date lives as a **suffix on the name**, edited through the
per-row selector (the user never types an at-sign):

| selector | suffix | meaning (relative to the job's ODAT) |
|---|---|---|
| **Odate** (default) | no suffix / `@odat` | the origin daily (in-conditions look for `scope=ODAT` or permanent; out-conditions are created on the ODAT) |
| **Prev** | `@prev` | the PREVIOUS daily = the last New Day in `daily_runs` before the ODAT; with no record, ODAT−1 |
| **Stat** | `@stat` | permanent: an in-condition only sees `scope_date=''`; an out-condition creates/removes the permanent one |

## The rules (numbered — cite them by number in commits and tests)

**C1 — Gate.** A WAITING job runs when ALL of its `conditionsIn` exist in the
pool (resolved scope). A missing condition = **WAIT COND** (card, Explain
`WAIT_CONDITION`) — it waits indefinitely, NEVER auto-cancels (classic enterprise parity;
whatever never becomes eligible dies at the daily rollover, if keepActive
allows). Hot path: the tick loads the pool ONCE per cycle (`CondIndex`).

**C2 — OK applies the out-conditions.** Ending OK (for real) and **Set OK**
(BUG-3/4) both apply out＋ and out− — in BOTH roles: as a producer it unblocks
successors; as a consumer it **CONSUMES** its own in-condition (the out− armed
by the arrow). A terminal NOTOK applies NOTHING.

**C3 — Consumption = deletion on OK.** The old model's "consumption permanence"
is now a natural consequence: rerunning a consumer that ended OK lands in WAIT
COND, because its own OK deleted the in-condition. **Set OK + rerun = waiting**
(the Set OK went to the pool and deleted it — the user's guiding scenario). A
rerun after **NOTOK/CANCELLED** runs straight away (a failure does not consume;
the condition is still in the pool).

**C4 — Rerun/cancel/delete/hold do NOT touch the pool.** No operator action on
instances touches a condition; only OK/Set OK terminations (C2), On/Do actions
and the operator THROUGH THE PANEL. Rerunning the PARENT creates a NEW condition
on its next OK — that is what unblocks the child's rerun.

**C5 — Force.** `Order Force` (Design) creates a new order that bypasses ONLY
the scheduling — the C1 gate still applies (a copy of an already-consumed
consumer is born in WAIT COND until someone recreates the condition). If the
condition STILL exists in the pool, the copy runs — a pure pool, with no
per-instance lock (a deliberate change from the claims model). `Run Now`
(Monitoring) bypasses C1 entirely; the bypass is NOT sticky (a rerun clears
`forced` when `force_mode=''`).

**C6 — No immutable conditions.** The operator can delete or add ANY condition
in the panel; the effect is immediate (deleted → the dependent goes back to
waiting; added → the dependent runs right away). The user's rule: *"we will not
have any kind of immutable condition"*.

**C7 — JSON is never null.** Every list in the API (Explain's `blockers`, etc.)
serializes as `[]`, never `null` (a nil slice breaks the frontend).

## Boolean entry logic — AND/OR (CL)

> **Status: TOPIC CLOSED — CL-1…CL-6 delivered and validated.** CL-1 (the DNF
> evaluator), CL-2 (the `$TIME` fallback + decoupling from the `windowFrom`
> floor + the UI toggle), CL-3 (data model + immutability), CL-4 (gate +
> OR-aware Explain + the canvas OR EDGES), CL-5 (the group editor in the
> drawer), CL-6 (test battery + docs + live validation).
>
> Live validation (2026-07-20, git-backed server): building `(A) OR (B)` in the
> drawer persists `conditionLogic` in the YAML through a full round trip
> (`conditionsIn` = the union of the members, `conditionsOutAdd` preserved,
> reopens clean); and the **OR edge on the Design canvas** renders dotted with
> the label **"OR"** (the AND baseline was dashed with no label). The
> **Monitoring** OR visual uses the SAME `makeEdge`/`condIsAlternative`, reading
> the frozen `cond_logic` column (schemaV21) — covered by
> `TestList_SerializesCondLogic` and by the shared code.
>
> The UI is the drawer's **Conditions** tab (Design): the entry is a flat list
> (AND) by default, and the **AND/OR** toggle reveals the GROUP editor (each
> group with its own AND/OR plus a top-level operator — the labels use the
> canonical `op:` vocabulary).
>
> **CL-2 discoverability (2026-07-21, report "I couldn't find OR with a
> time"):** the **"OR run at the scheduled time"** shortcut shows up WHENEVER
> there is ONE condition — with `windowFrom` it builds the DNF `(C1) OR
> ($TIME)`; without `windowFrom` it is dimmed and clicking it opens the
> **Schedule** tab (the UI guard stands: `$TIME` without a "From" would be
> satisfied immediately and would cancel out the condition). The per-group
> **＋ time** behaves the same; a ⏱ member orphaned from `windowFrom` (the
> window was cleared after the token was added) turns AMBER with the warning
> "satisfied immediately".

A job's entry stopped being ONLY an implicit AND of `conditionsIn`: it gained an
**optional** `conditionLogic` field — a boolean expression in **DNF form** (a
disjunction of conjunctions, two levels, each level with its own operator):

```
conditionLogic:
  op: OR                      # TOP-level operator between the groups
  groups:
    - { op: AND, members: [C1, C2] }
    - { op: AND, members: [C3] }
```

- **Canonical model:** `topOp( groupOp(member ∈ pool?) for group in groups )`.
  - `(C1 AND C2) OR C3` → `op:OR, groups:[{AND,[C1,C2]},{AND,[C3]}]`.
  - `(C1 OR C2) AND C3` → `op:AND, groups:[{OR,[C1,C2]},{AND,[C3]}]`.
- **OR semantics:** the **first branch to arrive satisfies it and fires** — as
  soon as ANY group becomes true, the job runs (it does not wait for the rest).
- **Members** carry the `@odat/@prev/@stat` date suffix as always. The reserved
  token **`$TIME`** is satisfied when `now >= scheduledAt` (the same clock as the
  window gate) — it is the basis of the "condition OR time" temporal fallback
  (CL-2); the gate already evaluates it, and decoupling it from the `windowFrom`
  floor is CL-2.
- **Backward compatibility (C1 still holds):** an absent/nil `conditionLogic` =
  **ONE AND group** over `conditionsIn` — the old gate, byte for byte (one
  blocker per missing condition). With logic, the gate only blocks when **NO
  branch is satisfiable** and emits ONE blocker carrying the expression
  (`RenderExpr`, e.g. "waiting for (C1 AND C2) OR C3 — no branch satisfied"); the
  Explain shows the same text.
- **Invariant: members ⊆ `conditionsIn`.** Every non-`$TIME` member also appears
  in `conditionsIn` (guaranteed by `NormalizeConditions` at the read chokepoint).
  That way topology (the derived `upstream`), the Monitoring edges and the frozen
  `conds_in` column keep reading `conditionsIn` **without knowing about the
  logic** — only the gate's EVALUATION uses `conditionLogic`.
- **LOOSE members = an AND requirement.** A name in `conditionsIn` that is NOT in
  any group of the logic is ANDed with the whole expression
  (`satisfied = topOp(groups) AND all-loose-members`). This is what makes the
  **arrow** "just work" on a job that has logic: it adds the simple condition to
  `conditionsIn` and that becomes mandatory, without having to fold the edge into
  an ambiguous OR. The UI preserves loose members while you edit the groups.
- **M1 immutability:** `conditionLogic` is frozen in the `definition_snapshot`
  like the rest of the definition (the gate reads it through `defForInstance`);
  changing the logic on the live definition does NOT relax an instance that was
  already ordered. The card and the Explain read the FROZEN logic.
- **Canvas OR edges (CL-4):** an edge P→C is "alternative" (OR) when the
  condition that links it is a member of an OR group in the consumer's logic
  (`condIsAlternative` in `lib/conditions-model.ts`) — rendered dotted with the
  label "OR" (`makeEdge(...,alt)` in `v2/canvas-layout.ts`). In **Design** it
  comes from the live definition (`buildDesignCanvas`); in **Monitoring** it
  comes from the FROZEN `cond_logic` column (schemaV21) —
  `frozenMonitorCols`/`MigrateCondLogicSnapshot`/`instanceRow.CondLogic`/
  `JobInstance.condLogic` (immutable, never the live definition). The arrow
  always creates an AND (a solid edge).
- **Code:** types plus the pure evaluator in `domain/conditions.go`
  (`ConditionLogic`, `CondGroup`, `EvalConditionLogic`, `RenderExpr`,
  `looseMembers`, `UsesTimeToken`); the gate wiring in `scheduler/explain.go`
  (`gateInstance`, where `$TIME` decouples the window floor). Frontend: types in
  `lib/orchestrator-model.ts`, the round trip in
  `lib/adapters/storage/ServerApiAdapter.ts`, and the editor (`EntryConditions`,
  `GroupedLogicEditor`, `GroupBox`, `OpToggle`) in `v2/JobConfigDrawer.tsx`.
  Tests: `domain/conditionlogic_test.go` (pure evaluator/DNF/`$TIME`/loose
  members/normalization), `scheduler/condlogic_gate_test.go`
  (gate+Explain+`$TIME`+immutability) and
  `api/bugs_behavior_test.go::TestList_SerializesCondLogic`.

## `upstream[]` — legacy input and a derived view

The `upstream` field is no longer a gate and is no longer persisted:

1. **Read compatibility**: old YAML with `upstream:` is EXPANDED into explicit
   conditions at the read chokepoint (`FileStore.List` →
   `domain.NormalizeConditions`, idempotent — it covers the scheduler, the API,
   sessions and publish; the TS mirror lives in `lib/conditions-model.ts` for
   local mode):
   - `on-success`/empty → out＋ on the parent; in + out− on the child;
   - `on-complete` → the same, plus an On/Do `set-condition` action on the
     parent's NOTOK;
   - `on-failure` → ONLY the On/Do action on NOTOK (nothing on OK);
   - `always` → no gate (it becomes topology only);
   - `dateRef` becomes the suffix of the child's in/out− conditions; an `@stat`
     edge makes the parent create the PERMANENT one (the only scope a `@stat` IN
     can see).
2. **Derived view**: after the expansion, `upstream` is RECOMPUTED by
   producer→consumer name matching (the base name, ignoring the suffix) and
   serves only as topology for the canvas/WhatIf/RCA/forecast/neighborhood.
   `FileStore.Save` ALWAYS drops it (persisting the view would re-expand it as a
   legacy edge).

**Snapshots** ordered before the unification: `defForInstance` expands the
consumer side in memory (`ExpandSnapshotConditions`, with the idempotency rule
against the live definitions). **`applyConditionsOut` is SNAPSHOT-ONLY (M1,
2026-07-17):** the out-conditions applied on OK/Set OK come ONLY from the
definition frozen at order time — creating a new consumer in Design after the
order does NOT make today's OK produce the new condition (only the next
Force/daily order carries the new OutAdd). The snapshot ∪ live-definition union
that existed for pre-unification compatibility was FROZEN into the in-flight
snapshots once, during the upgrade (`MigrateMonitoringSnapshot`, meta_flags
`monitoring-snapshot-v18`); the live definition is only a fallback for a legacy
instance WITHOUT a snapshot. There is a one-time unification backfill at boot
(`MigrateConditionsUnify`, flagged in `meta_flags`, schemaV17): a pair
(parent already OK, child WAITING and unconsumed) seeds the condition into the
pool. **dep_events/dep_claims (schemaV15) are RETIRED** — the tables stay in the
database for compatibility, but nothing reads or writes them.

## Monitoring edges — a reflection of the pool

Topology = **instance-to-instance matching through the ORDER SNAPSHOTS** (M1,
2026-07-17): each instance carries frozen `condsIn`/`condsOutAdd` (schemaV18) and
the edge exists when one copy's `condsOutAdd` produces an entry in the other's
`condsIn`, respecting the date suffix (`@odat` same origin · `@prev` previous
daily · `@stat` any — something carried from the 14th only gets an edge to the
parent from the 14th). Creating or wiring new jobs in Design does NOT redraw
instances that were already ordered; the live topology (the derived
`def.upstream`) belongs to DESIGN only. The COLOUR is the state of the conditions
that LINK the pair for THIS consumer:

- **green ✓** — the condition exists in the pool, OR the consumer already ran on
  it (RUNNING/OK — on OK it consumes it, but the edge stays green: it worked);
- **red ✗** — the DOWNSTREAM job is NOTOK;
- **grey** — the condition does not exist (not created yet · consumed by an OK —
  e.g. Set OK + rerun · deleted in the panel) and the consumer is waiting.

The CARD (**WAIT COND**) uses the gate's ruler (C1): WAITING with ANY missing
in-condition (all of `conditionsIn`, not just the ones from arrows); `Run Now`
(manual) does not flag it. Frontend source: `isWaitingOnConds` plus the
`conditions-store` pool (the `condition.changed` WS event resyncs it; local mode
uses localStorage with the SAME semantics, including `applyOutcomesLocal` on OK).

## Daily life cycle (carry-over) — who crosses the rollover

(Unchanged by the unification.) The pure rule lives in `carryDecision`
(scheduler.go); ages are in **CALENDAR DAYS** (the daily's timezone):

| state at the rollover | crosses? |
|---|---|
| RUNNING | ALWAYS |
| HELD | ALWAYS, while on hold |
| NOTOK | `days since the FAILURE ≤ keepActive\|1` |
| WAITING with a SCHEDULED retry (`attempts>1` **and** `started_at`) | the NOTOK rule |
| WAITING (CONFIRM included) | `days since the ODAT ≤ keepActive` — keepActive=0 dies at the first rollover |
| OK / CANCELLED | never |

General Hold + Delete: hold applies to any status except RUNNING
(`held_from_status` is restored on release); Delete only works on HOLD. Neither
touches the pool (C4).

## Invariants (what must NEVER happen again)

1. A rerun of a consumer that ended OK running again **without a new condition**
   (C3).
2. Set OK failing to apply out＋ or out− (C2).
3. A deletion in the panel NOT blocking the dependent, or an addition NOT
   releasing it (C6).
4. A Run Now bypass surviving a rerun (C5).
5. The API sending `null` where the frontend expects a list (C7).
6. Monitoring deriving ANYTHING on a card from the live definition (M1: the
   label, the type, the edges, `waitConfirm`, `waitAgent` and the out-conditions
   of `applyConditionsOut` ALL come from the snapshot/frozen columns — WAIT COND
   reads the POOL, which is runtime state rather than definition, and the list of
   in-conditions it checks is the frozen one).
7. A condition from ONE origin satisfying a consumer from ANOTHER origin without
   the suffix asking for it (§Dates — a job from the 14th does not eat today's
   condition).
8. The derived `upstream` being PERSISTED (it would re-expand as a legacy edge).
9. Wiring by arrow and wiring by hand diverging in ANYTHING — both paths write
   the same fields.

## Where it lives in the code

- `server/internal/domain/conditions.go` — the single model: `SplitCondRef`,
  `LinkCondName`, `NormalizeConditions` (idempotent expansion + the derived
  view), `ExpandSnapshotConditions`.
- `server/internal/scheduler/conditions.go` — the pool (`ConditionEngine`):
  Set/Unset (broadcast through OnChange), `CondIndex` (a per-tick photo),
  `MissingIdx` (the single source of "which one is missing?"), `ApplyOutcomes`.
- `server/internal/scheduler/scheduler.go` — the gate in the tick (condIdx),
  `FinishInstance`/`SetOK` → `applyConditionsOut` (snapshot-only, M1),
  `defForInstance` (snapshot expansion), `ForceOrder`.
- `server/internal/scheduler/condmigrate.go` — the one-time backfill
  (meta_flags).
- `server/internal/scheduler/monitorsnapshot.go` — the frozen schemaV18 columns
  (`frozenMonitorCols`) + `MigrateMonitoringSnapshot` (backfill + freezing the
  producer union into in-flight snapshots).
- `server/internal/scheduler/explain.go` — `gateInstance` (WAIT_CONDITION),
  Explain.
- `server/internal/storage/file.go` — List normalizes; Save drops upstream.
- `server/internal/api/bloco2.go` + the router — GET/POST `/api/conditions*`.
- `app/src/lib/conditions-model.ts` — the pure TS mirror (normalization, pool
  helpers, `edgeCondNames`, `missingConds`).
- `app/src/lib/conditions-store.ts` — the pool in the frontend (server WS /
  localStorage; `applyOutcomesLocal`).
- `app/src/v2/ConditionsPanel.tsx` — the Conditions panel (Monitoring, next to
  Organize).
- `app/src/v2/canvas-layout.ts` — pool-aware edges (`evaluateEdgeState`),
  `isWaitingOnConds`, spacing (NODE_GAP_Y=72) and the 9px ✓.
- `app/src/v2/V2Preview.tsx` — `connectDefs` (arrow → conditions on both jobs;
  `onConnect` plus the `onConnectEnd` drop-on-card fallback, direction taken from
  the originating dot), pool state, the Conditions button.
- `app/src/v2/JobConfigDrawer.tsx` — the unified **Conditions** tab; draft
  autosave (on job switch/close) + a diff merge of external conditions.

## The tests that lock the semantics down

`scheduler/conditions_unify_test.go` (normalization/idempotency/the view · the
happy path with consumption · rerun after OK waits and after NOTOK runs · Set OK
in both roles (C2) and Set OK+rerun waiting (C3) · deletion/addition through the
panel (C6) · Order Force follows the pool (C5) · on-failure becomes an action ·
the migration backfill) · `scheduler/lifecycle_test.go` (carryDecision; the
`TestOdat_*` tests rewritten for the pool: origin scope, @prev, @stat with a
permanent producer) · `scheduler/setok_bugs_test.go` (Set OK materializes the
out-conditions) · `api/bugs_behavior_test.go` (Set OK from WAITING; forced is
cleared) · `api/holdall_delete_test.go` (general hold/delete — no claims) ·
`scheduler/explain_test.go` (WAIT_CONDITION for a legacy edge; blockers `[]`).

## Checklist before touching this area

1. Re-read this document (5 minutes) and the header of `domain/conditions.go`.
2. Run `go test ./internal/scheduler/ ./internal/api/` BEFORE and AFTER.
3. Changed the semantics? Update **this document and the tests in the SAME
   commit**.
4. Validate live with a parent→child pair (the arrow creates the three lists;
   deleting in the panel blocks the child; Set OK+rerun waits; rerunning the
   parent unblocks it) — and **rebuild the server AND the frontend** before
   testing (a stale binary will fool you).
