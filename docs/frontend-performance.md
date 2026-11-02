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
