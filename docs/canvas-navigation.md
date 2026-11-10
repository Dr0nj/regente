# Canvas navigation and status

The main canvas keeps the complete logical workflow. Zoom and the navigation map do
not impose a job limit or switch the view to a list. Positions and dependencies
continue to come from the existing canvas layout.

## Main canvas zoom

Use the mouse wheel or the **− / +** controls to zoom from **5% to 400%**. The
percentage refers to the canvas, not browser zoom. **Fit** frames the workflow
within the camera limits. Camera navigation respects the sidebar and detail drawer.

| Canvas zoom | Job appearance |
| --- | --- |
| Above 55% | Name, type, status and existing badges |
| Above 25%, up to 55% | Name and status color; secondary details hidden |
| 5% through 25% | Solid status blocks, with selection and relationship outlines |

At **17%**, cards become solid blocks. Their dimensions, positions, connections
and click targets stay the same. Zooming back in restores details. Hidden status
animations stop at the solid level. Hover text retains the job name and status.

## Navigation map

Enable the minimap using the canvas preference. The map has its own zoom:
changing it does not change the main canvas zoom.

- Click a region or a folder label to center the main canvas there.
- Drag the viewport rectangle to move the main camera while preserving its zoom.
- Use the wheel or **− / +** inside the minimap to explore its detail levels.
- **Fit** restores the complete map; **Center** focuses the selected jobs.
- The arrow button toggles connections; **?** opens the status legend.
- Collapse/expand is remembered on this browser.
- With the map focused, arrow keys explore it, **+ / −** zoom, **Home** fits,
  and **Enter** centers the main canvas on the map center. Folder labels also
  accept **Enter** or **Space**.

Distant views aggregate jobs into density cells. Intermediate views add failed,
uncertain and running jobs, selections and aggregated connections. Close views
show individual markers and connections, with names where space permits. Folder
colors are stable by name, and tooltips show their complete names and status counts.
A minority failure remains visible even when most jobs in the cell succeeded.

## Status and selection

Cards and map markers share status tokens. Detailed cards have a softly tinted
surface, a strong status strip and neutral text. Solid cards use the same status
color as the map. Selection and dependency outlines remain separate from status.

| Status | Visual family |
| --- | --- |
| Failed | Red |
| Uncertain | Orange |
| Running | Cyan |
| Held | Amber |
| Waiting for confirmation | Violet |
| Waiting for agent/resource | Blue |
| Waiting for conditions/time or inactive | Gray |
| Successful | Green |

When multiple flags exist, display priority is failure, uncertainty, execution,
hold, confirmation, agent/resource, condition, waiting, success, inactive. This
only selects a visual treatment; it does not change scheduler gate ordering or
job state. Themes adjust color tokens. The rest of the application's pre-existing
status treatments are unchanged.

## Implementation and diagnostics

The minimap derives positions, folder bounds and topology from the current canvas.
Status patches update affected counts and preserve unrelated marker identities.
Pan updates the viewport component without rebuilding or rasterizing the map.
The heavy layers use Canvas 2D, not one React component per job. Pointer writes
are coalesced with `requestAnimationFrame`.

Opt-in development diagnostics in `canvas-metrics.ts` include
`minimapModelBuild`, `minimapModelPatch`, `minimapRaster`,
`minimapViewportUpdate` and `minimapPointerFrame`. Their bounded samples expose
counts and durations; pointer samples include estimated dropped frames. These
are diagnostic observations, not a capacity qualification.

Pure geometry/status tests are in `app/e2e/minimap-model.spec.ts`. The browser
scenario `canvas zoom LOD and minimap navigation` in `canvas-browser.spec.ts`
produces dark/light screenshots at 17% and a detailed view at 100%. Run browser
and load tests outside an active capacity measurement. Release readiness and
remaining validation are tracked only in [the roadmap](roadmap.md).
