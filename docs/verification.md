# Verification profiles

Run verification from the repository root. The default is **quick**: its success
means only that the declared local checks passed. It prints the checks it did
not run. **Full** requires Linux/amd64 and real dependencies; missing tools,
failed commands, missing evidence or an omitted mandatory gate cause failure.

| Check | Quick | Full | Additional CI coverage |
|---|---|---|---|
| Runner negative tests | Yes | Yes | Same runner |
| Server and agent build, vet, tests | Yes | Yes | Go 1.25 |
| Staticcheck, both Go modules | No | Yes | Pinned 2025.1.1 |
| Web lint and same-origin build | Installed dependencies | Clean engine-strict install | Declared Node minima |
| Schema, engines/recipes, API contracts | Focused checks | Same checks plus real services | Node matrix and demo |
| Markdown paths, generated anchors, site freshness | Yes | Yes | Same read-only checker |
| Real browser: local/hybrid/OIDC login, scoped WebSocket, frozen business timezone and daily recovery | No | Yes | Chromium against real server |
| PostgreSQL, NATS, OIDC, daily recovery, isolated v2 attempts and synthetic execution | No | Yes | Real containers; evidence required |
| PowerShell demo: offline and Git fixture | No | Linux Docker smoke | Native Windows smoke |
| Release installation in systemd | No | No | Separate release workflow |

Go unit suites include optional external-service tests. Their skips in quick
are **not** integration evidence. Full separately invokes the integration runner,
which rejects skipped or missing required scenarios, including both SQLite and
PostgreSQL variants of `TestI08AttemptIntegration`. I08 also has execution unit
tests for lost notifications, restart, stale output, delivery exhaustion and
backpressure. These prove the development laboratory contract, not an agent
journal or production execution durability. Neither profile qualifies
production load, high availability or the open enterprise milestones; see
[capacity and guarantees](capacity-guarantees.md).

## Quick

Requires Python 3.10+, Go compatible with the module directives, a
[Node version supported by the app](../app/README.md), npm, Git and installed app
dependencies. From a clean checkout, install them with `cd app && npm ci` first.

```bash
bash scripts/verify.sh --quick
# Cross-platform entry point, including Windows:
python scripts/verify.py --quick
# Show coverage only; status is not_run, never a validation pass:
python scripts/verify.py --full --plan
```

Quick runs server/agent build, vet and tests; web lint/build; and the documentation
checks. It does not run staticcheck, a clean dependency install, browser tests,
real service/recovery integration or demo smoke. Do not report it as full CI.

## Full on Linux/amd64

Use an isolated checkout on Linux/amd64 with Python 3.10+, Go 1.25, Node 24,
npm/npx, Git, Bash, tar, OpenSSL, PowerShell 7 and Docker with Compose v2 and a
running daemon. Docker and network access are needed for service images, Go/npm
dependencies and the demo build. Use synthetic data only. Port allocations and
owned-resource cleanup are handled by the existing fixture runners.

Keep app `.env`, `.env.local`, `.env.production` and `.env.production.local`
absent. The runner removes inherited Regente, Vite, OTel, Playwright and GitHub
token variables from child processes. Local private configuration is not a test
fixture.

Install Chromium and its OS dependencies before running:

```bash
cd app
npm ci --engine-strict
npx --no-install playwright install --with-deps chromium
cd ..
bash scripts/verify.sh --full
```

Full performs a fresh engine-strict npm install itself. It builds the server for
browser tests, verifies all six required browser scenarios without skips or
flaky passes, runs [integration and recovery](integration-baseline.md), then
executes the PowerShell demo smoke in both offline and Git-fixture modes. Demo
reports must prove authenticated presence, completed execution, credential
rejection and no public tunnel.

Windows/macOS hosts must use a Linux VM or CI for full. Unsupported hosts fail
before any gates execute; switching automatically to quick would hide missing
coverage and is forbidden.

## Evidence and CI

Each execution writes logs and `verify.json` under
`.integration/verify-<id>/evidence/`. The report records profile, revision, dirty
checkout state, required gates, each completed/failed gate, omitted coverage and
the integration evidence path. A nonzero exit is failure; a plan is not a run.
A missing, duplicate, reordered, skipped or failed required gate cannot yield
a successful report.

[CI](../.github/workflows/ci.yml) runs the exact `--full` entry point on Linux.
The reusable [documentation recipe matrix](../.github/workflows/doc-recipes.yml)
adds builds on the declared Node minima and native Windows PowerShell smoke.
A local full pass covers the fixed Linux profile; the extra platform matrix
still requires its own CI result. CI uploads `verification-evidence`, including
integration evidence; browser failure traces are uploaded separately.

Dead-code reporting is informational and is outside the pass condition.
[Release](../.github/workflows/release.yml) retains its separate integration and
systemd installation smoke. A CI pass does not mean a release or installation
has occurred.

## Documentation regression checks

The documentation gate consolidates focused checks:

- Current schema and compatibility: `TestMigrationRunbookContract` in the DB
  suite compares the current runbook contract with runtime migrations. Negative
  fixtures reject wrong/missing/duplicate values while permitting dated history.
- Node engines and same-origin recipes: `node scripts/check-doc-recipes.cjs`
  checks the lockfile, supported range and canonical recipes, including negative
  in-memory engine fixtures.
- API contracts: the API suite checks documented routes, response contracts,
  references and selected examples. These are focused assertions, not a complete
  OpenAPI standards validator.
- Site and links: `go -C server run ./cmd/docsite -repo .. -out ../docs/site -check`
  parses Markdown links/images (including reference links and raw HTML), checks
  repository-local paths, builds the site in a temporary directory, checks its
  actual HTML anchors and compares every generated file. It detects missing,
  extra and stale files without repairing the checkout. CRLF/LF text differences
  are normalized; binary assets are compared unchanged. External URLs are not
  fetched, and GitHub-only Markdown fragment rendering is outside this check.

To regenerate the versioned site after editing its sources:

```bash
go -C server run ./cmd/docsite -repo .. -out ../docs/site
```

Negative tests change only disposable fixtures: broken paths/reference links,
missing images/anchors, stale/missing/extra HTML and omitted required gates.
A successful negative test means the checker rejected its fixture for the
expected reason. Product claims still require source review; a text search
cannot prove HA, durability, cancellation or capacity guarantees.
