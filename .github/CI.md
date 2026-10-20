# CI operation and release gates

Pull requests run the complete Linux verification profile (including race,
browser, real integration, recovery and installed systemd security tests), the
Node minima and Windows/Linux recipes. The 30-minute engineering capacity soak
is selected on main only for runtime, dependency, workload or unclassified changes.
Known documentation, frontend and CI-only changes skip it; an unavailable diff
conservatively requires it. Explicit dispatch can force capacity qualification. A PR pass is not a
capacity qualification.

Branch pushes no longer duplicate PR runs. Open a PR to validate a branch, or
use the CI `workflow_dispatch` entry point. New runs cancel superseded CI on the
same PR/ref. Release publication has its own serialized, non-cancelling lock.

When required on main, capacity runs **in parallel** with full verification.
The capacity-policy check requires either a successful measurement or an explicit
non-runtime skip; it never accepts a failed or missing measurement. A successful main
push CI triggers Release through `workflow_run`. The release checks the source
repository, main push event, workflow path, exact SHA, attempt and every required
job using the Actions API. Missing, failed, skipped or duplicate required jobs
are rejected. PR/fork runs cannot authorize release. The release checks out that
verified SHA, not the latest commit at execution time. Superseded main revisions
are skipped before building.

Automatic releases reuse those completed checks instead of running them twice.
Tag/manual releases still run the full checks and recipes, selecting capacity
from changes since the preceding version (or an explicit capacity request). All paths retain signing, native platform validation, installer
smoke tests and payload integrity gates. `sign` and `publish` explicitly require
successful dependencies; intentional reuse skips are allowed only through the
validated main-CI path. A manual dispatch uses its selected source ref and an
unpublished tag name (the tag does not have to exist before checkout).

Go caches include both module lockfiles; npm caches use `app/package-lock.json`.
Caches do not skip tests. Jobs have explicit time limits; network downloads have
connection/total deadlines. Selected capacity qualifications retain the 1,800-second soak and thresholds.
The workflow summary explains whether capacity was required and why.

For failures, inspect the first failed gate and its uploaded evidence before
rerunning. The demo exports allowlisted synthetic failure state even on failure;
never upload its private directory, credentials or raw runtime logs. If failure
occurs before a fixture exists, the artifact step warns and preserves the original
failure. Do not mask flakiness with automatic whole-suite retries.

The former informational `deadcode@latest` step was removed from required CI:
its findings produced error annotations on otherwise successful runs. For a
deliberate maintenance audit, run the pinned tool separately:
`go -C server run golang.org/x/tools/cmd/deadcode@v0.52.0 -test ./...`.
Build, vet, staticcheck and race checks remain mandatory.

Workflow syntax is checked with actionlint v1.7.12. The release trust boundary is
covered by `scripts/test_ci_release_gate.py`, including wrong SHA/event/repository,
missing jobs and skipped/failing checks.
