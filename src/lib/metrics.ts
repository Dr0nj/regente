/**
 * Metrics Collector — Phase 8
 *
 * Tracks execution metrics per workflow and per job:
 * - Duration (min, max, avg, p95)
 * - Success/failure rates
 * - Retry rates
 * - SLA compliance
 *
 * Stored in localStorage (MVP). Supabase-ready interface.
 */

/* ── Types ── */

export interface JobMetricEntry {
  nodeId: string;
  nodeName: string;
  workflowId: string;
  timestamp: number;
  durationMs: number;
  attempts: number;
  status: "SUCCESS" | "FAILED";
}

export interface WorkflowMetricEntry {
  workflowId: string;
  workflowName: string;
  timestamp: number;
  durationMs: number;
  status: "SUCCESS" | "FAILED" | "ABORTED";
  jobsTotal: number;
  jobsSucceeded: number;
  jobsFailed: number;
}

export interface AggregatedMetrics {
  totalRuns: number;
  successCount: number;
  failureCount: number;
  successRate: number;
  avgDurationMs: number;
  minDurationMs: number;
  maxDurationMs: number;
  p95DurationMs: number;
  avgRetries: number;
  totalRetries: number;
}

export interface WorkflowSummary extends AggregatedMetrics {
  workflowId: string;
  workflowName: string;
  lastRunAt: number;
  lastStatus: string;
}

export interface SLAConfig {
  maxDurationMs: number;
  minSuccessRate: number;
}

export interface SLAResult {
  withinDuration: boolean;
  withinSuccessRate: boolean;
  compliant: boolean;
  actualDurationMs: number;
  actualSuccessRate: number;
}

/* ── Storage keys ── */

const JOB_METRICS_KEY = "regente:metrics:jobs";
const WORKFLOW_METRICS_KEY = "regente:metrics:workflows";
const MAX_ENTRIES = 500;

/* ── Helpers ── */

function loadEntries<T>(key: string): T[] {
  try {
    const raw = localStorage.getItem(key);
    return raw ? JSON.parse(raw) : [];
  } catch {
    return [];
  }
}

function saveEntries<T>(key: string, entries: T[]) {
  // Keep only the most recent entries
  const trimmed = entries.slice(-MAX_ENTRIES);
  localStorage.setItem(key, JSON.stringify(trimmed));
}

function percentile(sorted: number[], p: number): number {
  if (sorted.length === 0) return 0;
  const idx = Math.ceil((p / 100) * sorted.length) - 1;
  return sorted[Math.max(0, idx)];
}

function aggregate(durations: number[], statuses: string[], retries?: number[]): AggregatedMetrics {
  const totalRuns = statuses.length;
  const successCount = statuses.filter((s) => s === "SUCCESS").length;
  const failureCount = totalRuns - successCount;
  const sorted = [...durations].sort((a, b) => a - b);

  return {
    totalRuns,
    successCount,
    failureCount,
    successRate: totalRuns > 0 ? successCount / totalRuns : 0,
    avgDurationMs: totalRuns > 0 ? durations.reduce((a, b) => a + b, 0) / totalRuns : 0,
    minDurationMs: sorted[0] ?? 0,
    maxDurationMs: sorted[sorted.length - 1] ?? 0,
    p95DurationMs: percentile(sorted, 95),
    avgRetries: retries && retries.length > 0
      ? retries.reduce((a, b) => a + b, 0) / retries.length
      : 0,
    totalRetries: retries ? retries.reduce((a, b) => a + b, 0) : 0,
  };
}

/* ── Public API ── */

export function recordJobMetric(entry: JobMetricEntry): void {
  const entries = loadEntries<JobMetricEntry>(JOB_METRICS_KEY);
  entries.push(entry);
  saveEntries(JOB_METRICS_KEY, entries);
}

export function recordWorkflowMetric(entry: WorkflowMetricEntry): void {
  const entries = loadEntries<WorkflowMetricEntry>(WORKFLOW_METRICS_KEY);
  entries.push(entry);
  saveEntries(WORKFLOW_METRICS_KEY, entries);
}

export function getJobMetrics(nodeId?: string): JobMetricEntry[] {
  const entries = loadEntries<JobMetricEntry>(JOB_METRICS_KEY);
  return nodeId ? entries.filter((e) => e.nodeId === nodeId) : entries;
}

export function getWorkflowMetrics(workflowId?: string): WorkflowMetricEntry[] {
  const entries = loadEntries<WorkflowMetricEntry>(WORKFLOW_METRICS_KEY);
  return workflowId ? entries.filter((e) => e.workflowId === workflowId) : entries;
}

export function getWorkflowSummaries(): WorkflowSummary[] {
  const entries = loadEntries<WorkflowMetricEntry>(WORKFLOW_METRICS_KEY);
  const byWorkflow = new Map<string, WorkflowMetricEntry[]>();

  for (const e of entries) {
    const arr = byWorkflow.get(e.workflowId) ?? [];
    arr.push(e);
    byWorkflow.set(e.workflowId, arr);
  }

  const summaries: WorkflowSummary[] = [];
  for (const [workflowId, wfEntries] of byWorkflow) {
    const sorted = wfEntries.sort((a, b) => a.timestamp - b.timestamp);
    const last = sorted[sorted.length - 1];
    const agg = aggregate(
      sorted.map((e) => e.durationMs),
      sorted.map((e) => e.status),
    );
    summaries.push({
      ...agg,
      workflowId,
      workflowName: last.workflowName,
      lastRunAt: last.timestamp,
      lastStatus: last.status,
    });
  }

  return summaries.sort((a, b) => b.lastRunAt - a.lastRunAt);
}

export function getJobAggregation(nodeId: string): AggregatedMetrics {
  const entries = getJobMetrics(nodeId);
  return aggregate(
    entries.map((e) => e.durationMs),
    entries.map((e) => e.status),
    entries.map((e) => e.attempts - 1), // retries = attempts - 1
  );
}

export function getGlobalMetrics(): AggregatedMetrics {
  const entries = loadEntries<WorkflowMetricEntry>(WORKFLOW_METRICS_KEY);
  return aggregate(
    entries.map((e) => e.durationMs),
    entries.map((e) => e.status),
  );
}

export function checkSLA(workflowId: string, sla: SLAConfig): SLAResult {
  const entries = getWorkflowMetrics(workflowId);
  if (entries.length === 0) {
    return {
      withinDuration: true,
      withinSuccessRate: true,
      compliant: true,
      actualDurationMs: 0,
      actualSuccessRate: 1,
    };
  }

  const last = entries[entries.length - 1];
  const agg = aggregate(
    entries.map((e) => e.durationMs),
    entries.map((e) => e.status),
  );

  return {
    withinDuration: last.durationMs <= sla.maxDurationMs,
    withinSuccessRate: agg.successRate >= sla.minSuccessRate,
    compliant: last.durationMs <= sla.maxDurationMs && agg.successRate >= sla.minSuccessRate,
    actualDurationMs: last.durationMs,
    actualSuccessRate: agg.successRate,
  };
}

/** Duration trend — last N workflow runs as [timestamp, durationMs] pairs */
export function getDurationTrend(workflowId: string, limit = 20): [number, number][] {
  const entries = getWorkflowMetrics(workflowId);
  return entries
    .slice(-limit)
    .map((e) => [e.timestamp, e.durationMs]);
}

/** Hourly execution heatmap — count of runs per hour (0-23) */
export function getHourlyHeatmap(): number[] {
  const entries = loadEntries<WorkflowMetricEntry>(WORKFLOW_METRICS_KEY);
  const heatmap = new Array(24).fill(0);
  for (const e of entries) {
    const hour = new Date(e.timestamp).getHours();
    heatmap[hour]++;
  }
  return heatmap;
}

export function clearAllMetrics(): void {
  localStorage.removeItem(JOB_METRICS_KEY);
  localStorage.removeItem(WORKFLOW_METRICS_KEY);
}
