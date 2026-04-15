/**
 * Execution Engine — Phase 7
 *
 * Orchestrates workflow execution:
 * - DAG topological resolution with parallel layer detection
 * - Per-job execution with retry/backoff (from retry.ts)
 * - Cron-based scheduling (from cron.ts)
 * - Event-driven status updates via callbacks
 *
 * Runs entirely in the browser (MVP). Designed so the core logic
 * can later be lifted to an Edge Function / serverless backend.
 */

import type { Node, Edge } from "@xyflow/react";
import type { JobNodeData, JobStatus, JobType } from "@/lib/job-config";
import { withRetry, type RetryResult } from "@/lib/retry";
import { parseCron, nextRun } from "@/lib/cron";

/* ── Types ── */

export interface JobExecutionResult {
  nodeId: string;
  status: "SUCCESS" | "FAILED";
  durationMs: number;
  attempts: number;
  error?: string;
  output?: Record<string, unknown>;
}

export interface ExecutionEvent {
  type: "job-start" | "job-retry" | "job-complete" | "workflow-start" | "workflow-complete";
  nodeId?: string;
  nodeName?: string;
  timestamp: Date;
  data: Record<string, unknown>;
}

export interface WorkflowExecutionResult {
  workflowId: string;
  startedAt: Date;
  completedAt: Date;
  totalDurationMs: number;
  status: "SUCCESS" | "FAILED" | "ABORTED";
  jobResults: JobExecutionResult[];
  layers: string[][];
}

export type ExecutionListener = (event: ExecutionEvent) => void;

export interface ScheduledWorkflow {
  workflowId: string;
  workflowName: string;
  cronExpression: string;
  nextRunAt: Date | null;
  enabled: boolean;
  lastRunAt?: Date;
  lastStatus?: "SUCCESS" | "FAILED";
}

/* ── DAG Resolution ── */

/**
 * Resolve DAG into parallel execution layers via topological sort (Kahn's algorithm).
 * Each layer contains nodes that can execute concurrently.
 */
export function resolveExecutionLayers(
  nodes: Node<JobNodeData>[],
  edges: Edge[],
): string[][] {
  const jobNodes = nodes.filter((n) => n.type === "job" || !n.type);
  const nodeIds = new Set(jobNodes.map((n) => n.id));

  const children = new Map<string, string[]>();
  const inDegree = new Map<string, number>();

  for (const n of jobNodes) {
    children.set(n.id, []);
    inDegree.set(n.id, 0);
  }

  for (const e of edges) {
    if (!nodeIds.has(e.source) || !nodeIds.has(e.target)) continue;
    if (e.source.startsWith("group-") || e.target.startsWith("group-")) continue;
    children.get(e.source)?.push(e.target);
    inDegree.set(e.target, (inDegree.get(e.target) ?? 0) + 1);
  }

  const layers: string[][] = [];
  let queue = jobNodes
    .filter((n) => (inDegree.get(n.id) ?? 0) === 0)
    .map((n) => n.id);

  while (queue.length) {
    layers.push([...queue]);
    const next: string[] = [];
    for (const id of queue) {
      for (const child of children.get(id) ?? []) {
        const deg = (inDegree.get(child) ?? 1) - 1;
        inDegree.set(child, deg);
        if (deg === 0) next.push(child);
      }
    }
    queue = next;
  }

  return layers;
}

/* ── Job Simulation ── */

/**
 * Simulates job execution. In a real system this would invoke
 * a Lambda/container/Glue job and poll for completion.
 *
 * Duration depends on job type to feel realistic.
 */
const JOB_DURATION_MS: Record<JobType, [number, number]> = {
  LAMBDA: [400, 1200],
  BATCH: [1500, 3500],
  GLUE: [2000, 4000],
  STEP_FUNCTION: [800, 2000],
  CHOICE: [100, 300],
  PARALLEL: [200, 500],
  WAIT: [1000, 2000],
};

const FAILURE_RATE = 0.12; // 12% chance of failure per attempt

async function simulateJob(
  node: Node<JobNodeData>,
  _attempt: number,
  signal?: AbortSignal,
): Promise<Record<string, unknown>> {
  const [minMs, maxMs] = JOB_DURATION_MS[node.data.jobType] ?? [500, 1500];
  const duration = minMs + Math.random() * (maxMs - minMs);

  await new Promise<void>((resolve, reject) => {
    if (signal?.aborted) {
      reject(new DOMException("Aborted", "AbortError"));
      return;
    }
    const timer = setTimeout(resolve, duration);
    signal?.addEventListener("abort", () => {
      clearTimeout(timer);
      reject(new DOMException("Aborted", "AbortError"));
    }, { once: true });
  });

  // Simulate random failure
  if (Math.random() < FAILURE_RATE) {
    throw new Error(`Job ${node.data.label} failed: exit code 1`);
  }

  return {
    exitCode: 0,
    durationMs: Math.round(duration),
    output: `Completed ${node.data.label}`,
  };
}

/* ── Workflow Executor ── */

export class WorkflowExecutor {
  private abortController: AbortController | null = null;
  private listeners: Set<ExecutionListener> = new Set();
  private _running = false;

  get running() {
    return this._running;
  }

  /** Subscribe to execution events */
  on(listener: ExecutionListener): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  private emit(event: ExecutionEvent) {
    for (const listener of this.listeners) {
      try {
        listener(event);
      } catch {
        // listener errors don't break execution
      }
    }
  }

  /** Abort a running execution */
  abort() {
    this.abortController?.abort();
  }

  /**
   * Execute a workflow: resolve DAG layers, run each layer in parallel,
   * apply retry/backoff per job, emit events throughout.
   */
  async execute(
    workflowId: string,
    nodes: Node<JobNodeData>[],
    edges: Edge[],
    updateNodeStatus: (nodeId: string, data: Partial<JobNodeData>) => void,
  ): Promise<WorkflowExecutionResult> {
    if (this._running) {
      throw new Error("Execution already in progress");
    }

    this._running = true;
    this.abortController = new AbortController();
    const signal = this.abortController.signal;
    const startedAt = new Date();
    const jobResults: JobExecutionResult[] = [];

    const layers = resolveExecutionLayers(nodes, edges);
    const nodeMap = new Map(nodes.map((n) => [n.id, n]));

    this.emit({
      type: "workflow-start",
      timestamp: startedAt,
      data: { workflowId, totalNodes: nodes.length, totalLayers: layers.length },
    });

    // Set all job nodes to WAITING
    for (const layer of layers) {
      for (const nodeId of layer) {
        updateNodeStatus(nodeId, { status: "WAITING" as JobStatus, lastRun: undefined });
      }
    }

    let workflowFailed = false;
    let aborted = false;

    for (const layer of layers) {
      if (signal.aborted) {
        aborted = true;
        break;
      }

      // Execute all nodes in this layer concurrently
      const layerPromises = layer.map(async (nodeId) => {
        const node = nodeMap.get(nodeId);
        if (!node) return;

        const maxAttempts = (node.data.retries ?? 2) + 1; // retries + initial attempt
        const timeout = (node.data.timeout ?? 300) * 1000;

        // Mark RUNNING
        updateNodeStatus(nodeId, { status: "RUNNING" as JobStatus, lastRun: "now" });
        this.emit({
          type: "job-start",
          nodeId,
          nodeName: node.data.label,
          timestamp: new Date(),
          data: { maxAttempts, timeout },
        });

        const jobStart = performance.now();

        // Execute with retry
        const result: RetryResult<Record<string, unknown>> = await withRetry(
          (attempt) => simulateJob(node, attempt, signal),
          {
            maxAttempts,
            baseDelayMs: 500,
            maxDelayMs: 10_000,
            jitter: true,
            signal,
            onRetry: (attempt, error, nextDelayMs) => {
              this.emit({
                type: "job-retry",
                nodeId,
                nodeName: node.data.label,
                timestamp: new Date(),
                data: {
                  attempt,
                  error: error instanceof Error ? error.message : String(error),
                  nextDelayMs,
                },
              });
            },
          },
        );

        const durationMs = performance.now() - jobStart;
        const status: JobStatus = result.success ? "SUCCESS" : "FAILED";

        updateNodeStatus(nodeId, {
          status,
          lastRun: result.success
            ? `${(durationMs / 1000).toFixed(1)}s ago`
            : "failed",
        });

        const jobResult: JobExecutionResult = {
          nodeId,
          status: result.success ? "SUCCESS" : "FAILED",
          durationMs: Math.round(durationMs),
          attempts: result.attempts,
          error: result.success ? undefined : String(result.error),
          output: result.success ? result.value as Record<string, unknown> : undefined,
        };

        jobResults.push(jobResult);

        this.emit({
          type: "job-complete",
          nodeId,
          nodeName: node.data.label,
          timestamp: new Date(),
          data: {
            status,
            durationMs: Math.round(durationMs),
            attempts: result.attempts,
            error: jobResult.error,
          },
        });

        if (!result.success) {
          workflowFailed = true;
        }
      });

      await Promise.all(layerPromises);
    }

    const completedAt = new Date();
    this._running = false;

    const finalResult: WorkflowExecutionResult = {
      workflowId,
      startedAt,
      completedAt,
      totalDurationMs: completedAt.getTime() - startedAt.getTime(),
      status: aborted ? "ABORTED" : workflowFailed ? "FAILED" : "SUCCESS",
      jobResults,
      layers,
    };

    this.emit({
      type: "workflow-complete",
      timestamp: completedAt,
      data: {
        status: finalResult.status,
        totalDurationMs: finalResult.totalDurationMs,
        jobsSucceeded: jobResults.filter((r) => r.status === "SUCCESS").length,
        jobsFailed: jobResults.filter((r) => r.status === "FAILED").length,
      },
    });

    return finalResult;
  }
}

/* ── Scheduler ── */

/**
 * In-memory cron scheduler. Checks workflows periodically and triggers
 * execution when a cron expression matches.
 *
 * MVP: runs in the browser tab. Production: would be a Supabase Edge Function
 * or a pg_cron job.
 */
export class WorkflowScheduler {
  private schedules = new Map<string, ScheduledWorkflow>();
  private intervalId: ReturnType<typeof setInterval> | null = null;
  private onTrigger: ((schedule: ScheduledWorkflow) => void) | null = null;

  constructor(triggerCallback?: (schedule: ScheduledWorkflow) => void) {
    this.onTrigger = triggerCallback ?? null;
  }

  /** Register or update a workflow schedule */
  register(schedule: ScheduledWorkflow): void {
    const cron = parseCron(schedule.cronExpression);
    if (!cron) return;
    const next = nextRun(cron);
    this.schedules.set(schedule.workflowId, {
      ...schedule,
      nextRunAt: next,
    });
  }

  /** Remove a workflow from the scheduler */
  unregister(workflowId: string): void {
    this.schedules.delete(workflowId);
  }

  /** Get all scheduled workflows */
  getSchedules(): ScheduledWorkflow[] {
    return [...this.schedules.values()].sort((a, b) => {
      if (!a.nextRunAt) return 1;
      if (!b.nextRunAt) return -1;
      return a.nextRunAt.getTime() - b.nextRunAt.getTime();
    });
  }

  /** Start the scheduler loop (checks every 30s) */
  start(): void {
    if (this.intervalId) return;
    this.intervalId = setInterval(() => this.tick(), 30_000);
    this.tick(); // immediate first check
  }

  /** Stop the scheduler loop */
  stop(): void {
    if (this.intervalId) {
      clearInterval(this.intervalId);
      this.intervalId = null;
    }
  }

  private tick(): void {
    const now = new Date();
    for (const [id, schedule] of this.schedules) {
      if (!schedule.enabled || !schedule.nextRunAt) continue;
      if (now >= schedule.nextRunAt) {
        // Trigger execution
        this.onTrigger?.(schedule);

        // Recalculate next run
        const cron = parseCron(schedule.cronExpression);
        const next = cron ? nextRun(cron, now) : null;
        this.schedules.set(id, {
          ...schedule,
          lastRunAt: now,
          nextRunAt: next,
        });
      }
    }
  }
}

/* ── Singleton instances ── */

export const workflowExecutor = new WorkflowExecutor();
export const workflowScheduler = new WorkflowScheduler();
