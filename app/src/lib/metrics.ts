/**
 * Metrics Collector — Phase 8 + Phase 11 (dual-mode persistence)
 *
 * Tracks execution metrics per workflow and per job:
 * - Duration (min, max, avg, p95)
 * - Success/failure rates
 * - Retry rates
 * - SLA compliance
 *
 * Uses Supabase when configured, localStorage as fallback.
 */

import {
  insertJobMetric,
  insertWorkflowMetric,
  loadJobMetrics,
  loadWorkflowMetrics,
  clearMetrics as clearMetricsStore,
  localLoad,
} from "@/lib/persistence";

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

/* ── Helpers ── */

const WORKFLOW_METRICS_KEY = "regente:metrics:workflows";

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
  insertJobMetric(entry);
}

export function recordWorkflowMetric(entry: WorkflowMetricEntry): void {
  insertWorkflowMetric(entry);
}

export function getJobMetrics(nodeId?: string): JobMetricEntry[] {
  // Sync read from localStorage (fast path for UI rendering)
  return loadJobMetrics(nodeId) as unknown as JobMetricEntry[];
}

export function getWorkflowMetrics(workflowId?: string): WorkflowMetricEntry[] {
  return loadWorkflowMetrics(workflowId) as unknown as WorkflowMetricEntry[];
}

export function getWorkflowSummaries(): WorkflowSummary[] {
  // Sync fallback — uses localStorage for immediate rendering
  const entries = localLoad<WorkflowMetricEntry>(WORKFLOW_METRICS_KEY);
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
  const entries = localLoad<JobMetricEntry>("regente:metrics:jobs").filter((e) => e.nodeId === nodeId);
  return aggregate(
    entries.map((e) => e.durationMs),
    entries.map((e) => e.status),
    entries.map((e) => e.attempts - 1),
  );
}

export function getGlobalMetrics(): AggregatedMetrics {
  const entries = localLoad<WorkflowMetricEntry>(WORKFLOW_METRICS_KEY);
  return aggregate(
    entries.map((e) => e.durationMs),
    entries.map((e) => e.status),
  );
}

export function checkSLA(workflowId: string, sla: SLAConfig): SLAResult {
  const entries = localLoad<WorkflowMetricEntry>(WORKFLOW_METRICS_KEY)
    .filter((e) => e.workflowId === workflowId);
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
  const entries = localLoad<WorkflowMetricEntry>(WORKFLOW_METRICS_KEY)
    .filter((e) => e.workflowId === workflowId);
  return entries
    .slice(-limit)
    .map((e) => [e.timestamp, e.durationMs]);
}

/** Hourly execution heatmap — count of runs per hour (0-23) */
export function getHourlyHeatmap(): number[] {
  const entries = localLoad<WorkflowMetricEntry>(WORKFLOW_METRICS_KEY);
  const heatmap = new Array(24).fill(0);
  for (const e of entries) {
    const hour = new Date(e.timestamp).getHours();
    heatmap[hour]++;
  }
  return heatmap;
}

export function clearAllMetrics(): void {
  clearMetricsStore();
}
