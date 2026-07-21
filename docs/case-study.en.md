# Case study — Regente: a Control-M-class, Git-native job orchestrator, from scratch

> **TL;DR.** Regente is a batch workload orchestrator in the spirit of BMC Control-M —
> immutable daily, condition-based dependencies, calendars, resources, Confirm,
> forecast — built from scratch as a Go + React monorepo, with **Git as the source of
> truth** for definitions. Validated live with **1,000,000 jobs/day** (materialization
> in 17s, summary in 51ms), it runs from a single binary on a $5 VPS up to multi-node
> HA with Postgres, and exposes the control plane to AI agents via **MCP (22 tools)**.
> ~44k lines of Go, ~24k of TypeScript, 427 tests.

---

## 1. The problem

Enterprise orchestrators (Control-M, Autosys, Tivoli) solve a real problem — the batch
operations day: "materialize today's jobs, honor dependencies/calendars/windows, give
me hold/release/rerun/force and an audit trail". But they charge dearly for it, in
three currencies:

- **License and lock-in** — the company's runbook lives inside a proprietary console.
- **Jobs outside version control** — the definition lives in the tool's database;
  diff, review and rollback become manual processes.
- **DevEx friction** — testing a new job requires a shared environment; "promoting
  from dev to prod" is a ritual.

The project's question: *how much of this category can be rebuilt with modern
engineering — Git, single binary, open API — without losing the operational semantics
that make Control-M good?*

## 2. The architecture bets

**Actually Git-native.** Job definitions are YAML in a separate GitHub repository
(`regente-workspace`). The daily syncs `origin/main` **before** materializing; every
order records the SHA of the commit it came from. Editing in the UI's Design opens a
*design session* (ephemeral server-side clone) and **nothing runs without a publish** —
publish = commit+push (or PR, if policy requires). Drift between runtime and Git is
detected and alerted.

**Immutability as a product rule.** When ordered, the definition is **frozen** into the
instance (JSON snapshot + denormalized columns). Publishing a change in Design never
rewrites the current day — Monitoring is the photograph of what was scheduled, as in
Control-M. This rule became the deciding test for several bugs ("the badge changed by
itself" = something read the live definition).

**Operable monolith, serverless optional.** One Go binary serves API + WebSocket + UI
(single-origin) over SQLite — a one-box deploy. The same flags become a "portable
serverless" deploy: scheduler driven by an external cron (`POST /scheduler/tick`,
idempotent, advisory-lock per tick), pluggable agent transport (WS, long-poll, SSE,
NATS), WASM executor. Postgres + leader election (advisory lock) give multi-node HA
without changing the scheduling code — only the leader materializes and dispatches.

**A single source of decision.** Every execution gate (window, dependency, condition,
agent, resource, Confirm) goes through **one** evaluator (`gateInstance`). The tick
uses it to decide; the UI's "Why not?" uses it to explain. No blocker can exist without
showing up in Explain — by construction, not by discipline.

## 3. Control-M semantics (the hard part)

Parity isn't the feature list — it's the behavior at the edges:

- **Daily / New Day** — set-based batch materialization (1 commit per 5k chunk),
  honest carry-over: an order crossing the rollover **moves forward while keeping its
  original date** — everything scoped by date (conditions `@odat`, variables, windows)
  keeps using the date the order was born on. RUNNING/HELD always cross; unhandled
  NOTOK survives the configured number of days since the failure; WAITING only with
  explicit retention.
- **Dependencies as explicit CONDITIONS in a single pool** — the A→B arrow on the
  canvas is sugar for a named condition (`A-TO-B`): the parent's OK **creates** the
  condition, the successor's input **waits** for it, and a negative output **consumes**
  (deletes) it — which models fan-in with consumption, as in Control-M. The input
  accepts **real AND/OR logic** (groups with a top-level operator) and the `$TIME`
  token ("condition OR time"); rerunning the parent undoes the OK and dependents
  **go back to waiting** for a fresh completion.
- **Force with two gestures** — "Run Now" unblocks the existing instance (bypasses
  window/deps/resources; Confirm and agent are never bypassed); "Order Force" creates
  a new order outside the schedule that **respects** the runtime gates.
- **Conditions IN/OUT, quantitative resources, calendars** (Nth business day, shift to
  next/previous business day, include/exclude, holidays), **cyclic** jobs with window
  and cap, **Confirm** (a runtime wait, not a schedule one), **durable scheduled
  retry** (survives restarts and the day rollover — a goroutine sleeping for 3 days
  would die on the first deploy), **%%variables** with global/local scope and
  business-day offsets (`%%ODATE+3B`).
- **Strictly pinned agents** — a job created on an agent stays **on that** agent; if
  it goes down, the job waits in WAIT AGENT. It never migrates on its own. Every
  server ships with an embedded **SERVER-AGENT** (HTTP/REST) — API calls run without
  installing an external agent.

## 4. Beyond Control-M

Where it can beat the original without inventing gimmicks:

- **Explain ("why didn't it run?")** — a structured, per-instance answer, from the
  same source that decides the dispatch.
- **Blast radius / What-If / Dry-run / Daily diff** — simulations of the day without
  materializing anything.
- **Agent-native (MCP)** — 22 tools (11 read + 11 gated write) expose the control
  plane to AI agents: an LLM can diagnose a stuck job and (with permission) rerun it.
- **Command-line DevEx** — `regente test` (validates+simulates a workspace,
  CI-friendly), `regente dev` (disposable local environment), `regente promote`
  (promotion between branches), a Go DSL that generates YAML, curated OpenAPI served
  by the binary itself.
- **Signed quick actions** in alerts (Slack/e-mail/PagerDuty) — one-click
  rerun/confirm with an expiring token.

## 5. Scale validated (not estimated)

A 1M jobs/day scenario, seeded and operated live:

- **Daily materialization (write-path):** 1,000,000 instances in **~17s**.
- **Day summary (read-path):** **51ms @100k**, keyset+offset pagination.
- **UI with 1M jobs:** instant dashboard; a folder opens in ~39ms; **37 rows in the
  DOM** for the entire day (virtualization).
- **Canvas board:** configurable cap (500–5000) + server-driven ViewPoint.

The decisions that paid the bill: set-based existence checks (1 query, not 1 per
definition), batched inserts with prepared statements, atomic dispatch claim
(`UPDATE … WHERE status=WAITING`), keyset pagination on the read-path, and a
virtualized sidebar with compressed height (the browser overflows float32 above
~16.7M px — a detail that only shows up with 1M rows).

## 6. Reliability

- **Atomic claim** per instance guarantees ≤1 execution even with multiple dispatch
  paths (tick, force, retry) or multiple nodes.
- **Panic-recovery** at every scheduler entry point; a watchdog for stuck RUNNING;
  self-monitoring that alerts through the product's own channels.
- **Online backup** (`-backup` = VACUUM INTO), documented DR, retention/archives
  with GC.
- **427 Go tests** — including exhaustive calendar suites (two independent
  "special business day" code paths must agree day by day for a whole month) and a
  hand-written forecast oracle, independent of the code it validates.

## 7. Security and enterprise

Per-folder RBAC (ACLs), optional OIDC/SSO, mTLS between server and agents, an audit
trail with SIEM export, per-agent tokens, secrets via provider (env/file — the GitHub
PAT never needs to persist in cleartext in the state store), an execution sandbox for
multi-tenant agents (container with no capabilities, no mounts, dedicated uid).

## 8. The one-box deploy (the acid test)

`curl … | sudo bash` on a Linux VPS: downloads the release bundle (binary + SPA +
deploy files), registers systemd, serves UI+API+WS on a single origin,
`regente-configure` walks through a strong token + PAT via the secrets provider,
nginx+certbot provide TLS with renewal. The same product that does HA with Postgres
runs entirely on a $5 machine — that range was a goal, not an accident.

## 9. Next up: AI that stays inside the perimeter *(roadmap, spec ready)*

The Control-M audience lives in regulated environments — banks, insurers, utilities —
where sysout, logs and host data **cannot leave the perimeter**. Every "AI-powered"
scheduler on the market ships that data to a cloud API; Regente's next job type,
`AI_AGENT`, does the opposite: the LLM runs **on the same host as the agent**
(Ollama/llama.cpp/vLLM), analyzes sysout and logs right there, and the server only
receives the verdict — no data travels. The prompt is part of the definition, so it
**freezes into the order's immutable snapshot**: the exact prompt that ran stays
auditable forever, and Confirm, conditions, SLA, RBAC and the audit trail all work
without a single new line, because `AI_AGENT` is a job type like any other. Phase one
is analysis-only, **no tools** — a prompt injection can at worst produce a wrong
report, never an executed command. Built-in use case: automatic RCA when a job fails,
with JSON-Schema-validated output becoming variables for the successors.

## 10. Lessons that apply to any system

1. **Immutability is a product feature, not a storage detail.** Freezing the
   definition into the order eliminated an entire class of "it changed by itself" bugs.
2. **A single source of decision buys explainability for free.** Explain never
   diverges from dispatch because it IS dispatch.
3. **Durable state > patient goroutines.** A 3-day retry lives in the database, not
   in a sleep.
4. **Dependency satisfaction is explicit state, not a lookup.** Deriving the edge
   from the parent's live status breaks at the first human operation (rerun/cancel);
   named conditions created and consumed in a pool — and frozen into the order —
   model what the operator expects.
5. **Scale is additive if the hot path is set-based from the start.** The same code
   that serves 10 jobs serves 1M — the optimizations were about access patterns, not
   architecture.
6. **Test against an oracle, not against your own function.** The calendar suites
   caught real divergences that mirror tests would never see.

## 11. Project numbers

- **Code:** ~44k lines of Go (server+agent) · ~24k lines of TS/TSX (UI).
- **Tests:** 427 Go test functions across 97 files + live E2E validations.
- **Executors:** COMMAND · SCRIPT · HTTP/REST · agentless SSH · DATABASE · FILE_WATCH
  · MFT · WASM · K8s · Lambda · Batch · Glue · Step Functions · Cloud Run.
- **Scale:** 1M jobs/day validated live (write 17s · summary 51ms · virtualized UI).
- **Deploy:** single binary (SQLite) → multi-node HA (Postgres + leader election) →
  portable serverless (external tick).
- **Interface:** React UI (Design/Monitoring), 17 themes, CLI, OpenAPI, MCP (22 tools).

---

*Written 2026-07-13 at the close of the roadmap's Phase Z; revised 2026-07-21 (AND/OR
conditions and origin-date carry-over replacing retired models · updated numbers ·
AI_AGENT section · article-ready format, no tables). Per-delivery details, with dates
and commits, in [`docs/roadmap.md`](roadmap.md). English version of
[`docs/case-study.md`](case-study.md).*
