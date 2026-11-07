// Opt-in, bounded diagnostics. Production builds never collect these samples.
type Metric = { name: string; ms: number; at: number; values: Record<string, number> };
let enabled = false;
const samples: Metric[] = [];
const totals: Record<string, number> = {};
const durations: Record<string, number> = {};
export function enableCanvasMetrics(value: boolean) {
  enabled = value && ((import.meta as unknown as { env?: { DEV?: boolean } }).env?.DEV ?? true);
  samples.length = 0;
  for (const key of Object.keys(totals)) delete totals[key];
  for (const key of Object.keys(durations)) delete durations[key];
}
export function readCanvasMetrics() { return { samples: [...samples], totals: { ...totals }, durations: { ...durations } }; }
export function recordCanvasMetric(name: string, start = performance.now(), values: Record<string, number> | (() => Record<string, number>) = {}) {
  if (!enabled) return;
  totals[name] = (totals[name] ?? 0) + 1;
  const at = performance.now();
  durations[name] = (durations[name] ?? 0) + at - start;
  samples.push({ name, ms: at - start, at, values: typeof values === "function" ? values() : values });
  if (samples.length > 240) samples.shift();
}
