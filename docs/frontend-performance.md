# Frontend loading measurement

REV-05 local comparison, 2026-11-02. Baseline: v0.2.61 frontend, Vite 8.3.5,
React 19.3.0. Candidate: lazy console/portal with a lightweight session gate.
Both use the same local synthetic SQLite server, same-origin production build,
Chromium, three fresh browser contexts, no network throttling. Run
`node scripts/frontend-performance.mjs OUTPUT.json` from the repository root.
Capture the baseline before changing frontend loading; build each revision first.

| Stage | JS bytes before | JS bytes after | Median ready ms before | Median ready ms after |
|---|---:|---:|---:|---:|
| login | 670,542 | 246,102 | 341 | 247 |
| console | 670,542 | 669,227 | 140 | 861 |
| settings | 704,501 | 703,168 | 851 | 863 |
| portal | 670,542 | 254,842 | 47 | 853 |

Bytes are decoded resource body sizes, not gzip estimates or installation size.
Login is cold; console and Settings accumulate requests in the same document.
Portal is a new navigation in the authenticated context. Timings are descriptive
local samples, not a production SLO or a controlled latency benchmark.
The first console load now pays for its chunk after login; this is an intentional
tradeoff, not a claim that every navigation became faster. Authenticated console
bytes remain similar because Monitoring and Design share the canvas.

The main entry fell from 655.41 KB (201.75 KB gzip) to 246.10 KB (78.28 KB gzip).
Console is 399.78 KB and portal 4.09 KB as separate chunks. Settings and the other
existing optional dialogs remain lazy. No feature was removed and no visual
redesign was introduced. Further splitting the shared canvas requires separate
workload evidence; file length alone is not a performance reason to rewrite it.

`REV05: login and portal defer console assets` is a mandatory browser scenario:
forged cached identity cannot load the console, cold login stays below 350000 JS
bytes, portal avoids console/Settings assets, and Settings loads only on opening.
Existing browser coverage preserves login modes, password change, sessions,
reconnect, business time, daily recovery, uncertain execution and shared drafts.

## Incremental daily canvas (2026-11-06)

The primary canvas now loads the complete accessible daily via `/api/instances`
without `limit`. The endpoint already supports this projection without action
payloads. The obsolete `legacyCap` setting is ignored and no longer offered.
React Flow's `onlyRenderVisibleElements` remains enabled: offscreen jobs retain
their identities, positions and dependencies in the logical model. Neither the
sidebar nor ViewPoint is a mandatory fallback.

`createCanvasModel` owns a bounded structural cache per view. Its key includes
IDs and ordering, folders, frozen condition links, ODAT, alternative-condition
metadata, layout configuration/overrides and card dimensions. Status, time,
agent/resource availability and selection do not invalidate it.
`buildCanvasGeometry` constructs lanes, positions and links;
`applyCanvasRuntimeState` preserves node/edge objects when their visual values
are unchanged. Independent dependency components are laid out separately, and
components with identical canonical topology reuse their geometry within a build.
Equal node counts with different links never share a geometry entry.

Server mutations apply immediately and publish in 24ms batches with changed IDs.
Unchanged REST rows retain their object identity. Partial events reconcile their
own details with at most six concurrent requests, preserving a trailing request
when needed. Full refresh generation guards, touched timestamps and tombstones
remain in place; late detail replies also cannot resurrect or regress a job.
Condition refreshes are coalesced. Unchanged resource/agent/condition snapshots
stay quiet; resource polling runs only when jobs in the model require resources.
The next scheduled resource time gate has its own timer.

Pan applies the latest pointer sample once per animation frame and cancels pending
frames on release/unmount. Card dragging stays disabled. Selection projects only
changed halos. The minimap draws the full job model to one bitmap; its viewport
rectangle alone subscribes to camera movement. Job dimensions match the actual
220×80 or 220×99 cards; lanes and static handles also exist before measurement.

Representative local Windows/Node/Chromium samples, baseline `b1e3122`, synthetic
independent chains of ten, one status transition (not a production latency SLO):

| Jobs | Initial build before / after | Status build before / after | Recreated nodes before / after |
|---:|---:|---:|---:|
| 100 | 28 / 2 ms | 41 / 0.1 ms | 101 / 1 |
| 500 | 75 / 3 ms | 72 / 0.3 ms | 501 / 1 |
| 1,000 | 236 / 6 ms | 196 / 0.7 ms | 1,001 / 1 |
| 5,000 | 1,199 / 20 ms | 1,172 / 3 ms | 5,001 / 1 |

The 5,000-job status change previously recreated 4,500 edges and spent 1,125ms
in Dagre. It now recreates no edges for this root transition and calls no Dagre.
A separate 28,800-job fixture uses 120 independent blocks, each with 24 ten-job
fork/join groups linked through four prerequisite leaves (45,600 edges): initial
model build ~318ms, one terminal-job transition ~33ms, one changed node and two
changed incoming edges. These are model timings, excluding network/DOM paint.

The browser regression uses 5,100 jobs with the old cap set to 500, enables the
minimap, suppresses node ResizeObserver measurements, focuses the final offscreen
job and checks live status, multiselection, Shift+drag and continuous pan. A burst
of 100 mouse samples produces one camera write; 60 pan frames produce no canvas
builds or Dagre calls. Separate checks cover WS batching, delayed REST/deletion,
partial Hold payloads, native Design connections and deterministic finite geometry.

Reproduce with a same-origin production build and installed Chromium:

```sh
VITE_REGENTE_SERVER_URL=@origin npm run build
npx playwright test
```

Set `REGENTE_CANVAS_LARGE_BENCHMARK=1` to include the additional 28,800-chain
measurement and `REGENTE_CANVAS_METRICS_OUTPUT` to save its JSON. The fork/join
fixture is always tested. In a Vite development page, import
`/src/v2/canvas-metrics.ts` and call `enableCanvasMetrics(true)` then
`readCanvasMetrics()`. Collection is opt-in, bounded to 240 samples, with aggregate
counters/durations; production builds cannot enable it. Browser artifacts report
WS receive → next animation frame after DOM update, a paint proxy rather than a
compositor presentation timestamp, plus render counts and frame intervals.

Remaining bounds: runtime reconciliation still scans/indexes the loaded model;
large graphs with distinct connected topologies still pay synchronous Dagre on
structural changes. No worker or million-node capacity claim is included. The
measurements above do not establish performance of a deployed remote daily.
The virtualization/dimension contract follows the installed React Flow code and
its [node API](https://reactflow.dev/api-reference/types/node) and
[rendering API](https://reactflow.dev/api-reference/react-flow).
