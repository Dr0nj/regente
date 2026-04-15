/**
 * Execution Context — Phase 7
 *
 * React context that bridges the execution engine to the UI.
 * Converts engine events into LogEntry[], manages execution state,
 * and exposes scheduler + abort controls.
 */

import {
  createContext,
  useContext,
  useCallback,
  useRef,
  useMemo,
  useState,
  useEffect,
  type ReactNode,
} from "react";
import type { Node, Edge } from "@xyflow/react";
import type { JobNodeData } from "@/lib/job-config";
import type { LogEntry } from "@/components/ExecutionLog";
import {
  workflowExecutor,
  workflowScheduler,
  type ExecutionEvent,
  type WorkflowExecutionResult,
  type ScheduledWorkflow,
} from "@/lib/execution-engine";
import { parseCron, nextRun, describeCron } from "@/lib/cron";

/* ── Context shape ── */

interface ExecutionContextValue {
  /** Whether a workflow is currently executing */
  running: boolean;
  /** All execution logs (cumulative, clearable) */
  logs: LogEntry[];
  /** Clear logs */
  clearLogs: () => void;
  /** Run workflow through the engine */
  runWorkflow: (
    workflowId: string,
    nodes: Node<JobNodeData>[],
    edges: Edge[],
    updateNodeStatus: (nodeId: string, data: Partial<JobNodeData>) => void,
  ) => Promise<WorkflowExecutionResult>;
  /** Abort running execution */
  abort: () => void;
  /** Scheduled workflows */
  schedules: ScheduledWorkflow[];
  /** Register a cron schedule  */
  registerSchedule: (workflowId: string, workflowName: string, cron: string) => void;
  /** Unregister a cron schedule */
  unregisterSchedule: (workflowId: string) => void;
  /** Toggle schedule enabled/disabled */
  toggleSchedule: (workflowId: string) => void;
  /** Get human-readable cron description */
  describeCron: (expression: string) => string;
}

const ExecutionContext = createContext<ExecutionContextValue | null>(null);

export function useExecution(): ExecutionContextValue {
  const ctx = useContext(ExecutionContext);
  if (!ctx) throw new Error("useExecution must be used within ExecutionProvider");
  return ctx;
}

/* ── Event → LogEntry conversion ── */

let logCounter = 0;

function eventToLogs(event: ExecutionEvent): LogEntry[] {
  const ts = event.timestamp.toLocaleTimeString("en-US", {
    hour12: false,
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
  const nodeId = event.nodeId ?? "";
  const nodeName = event.nodeName ?? "Workflow";

  switch (event.type) {
    case "workflow-start":
      return [
        {
          id: `log-${++logCounter}`,
          timestamp: ts,
          nodeId: "",
          nodeName: "Scheduler",
          level: "info",
          message: `Workflow execution started — ${event.data.totalNodes} nodes in ${event.data.totalLayers} layers`,
        },
      ];
    case "job-start":
      return [
        {
          id: `log-${++logCounter}`,
          timestamp: ts,
          nodeId,
          nodeName,
          level: "info",
          message: `Job started (max ${event.data.maxAttempts} attempts, timeout ${Math.round((event.data.timeout as number) / 1000)}s)`,
        },
      ];
    case "job-retry":
      return [
        {
          id: `log-${++logCounter}`,
          timestamp: ts,
          nodeId,
          nodeName,
          level: "warn",
          message: `Attempt ${event.data.attempt} failed: ${event.data.error} — retrying in ${Math.round((event.data.nextDelayMs as number) / 1000)}s`,
        },
      ];
    case "job-complete": {
      const status = event.data.status as string;
      const duration = ((event.data.durationMs as number) / 1000).toFixed(1);
      if (status === "SUCCESS") {
        return [
          {
            id: `log-${++logCounter}`,
            timestamp: ts,
            nodeId,
            nodeName,
            level: "success",
            message: `Completed in ${duration}s after ${event.data.attempts} attempt(s)`,
          },
        ];
      }
      return [
        {
          id: `log-${++logCounter}`,
          timestamp: ts,
          nodeId,
          nodeName,
          level: "error",
          message: `Failed after ${event.data.attempts} attempt(s) — ${event.data.error}`,
        },
      ];
    }
    case "workflow-complete": {
      const wfStatus = event.data.status as string;
      const total = ((event.data.totalDurationMs as number) / 1000).toFixed(1);
      return [
        {
          id: `log-${++logCounter}`,
          timestamp: ts,
          nodeId: "",
          nodeName: "Engine",
          level: wfStatus === "SUCCESS" ? "success" : "error",
          message: `Workflow ${wfStatus.toLowerCase()} — ${total}s total, ${event.data.jobsSucceeded} succeeded, ${event.data.jobsFailed} failed`,
        },
      ];
    }
    default:
      return [];
  }
}

/* ── Provider ── */

export function ExecutionProvider({ children }: { children: ReactNode }) {
  const [running, setRunning] = useState(false);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [schedules, setSchedules] = useState<ScheduledWorkflow[]>([]);
  const schedulerStarted = useRef(false);

  // Subscribe to engine events → logs
  useEffect(() => {
    const unsub = workflowExecutor.on((event: ExecutionEvent) => {
      const newLogs = eventToLogs(event);
      if (newLogs.length > 0) {
        setLogs((prev) => [...prev, ...newLogs]);
      }
    });
    return unsub;
  }, []);

  // Start scheduler on mount
  useEffect(() => {
    if (!schedulerStarted.current) {
      workflowScheduler.start();
      schedulerStarted.current = true;
    }
    return () => {
      workflowScheduler.stop();
      schedulerStarted.current = false;
    };
  }, []);

  const clearLogs = useCallback(() => setLogs([]), []);

  const runWorkflow = useCallback(
    async (
      workflowId: string,
      nodes: Node<JobNodeData>[],
      edges: Edge[],
      updateNodeStatus: (nodeId: string, data: Partial<JobNodeData>) => void,
    ) => {
      setRunning(true);
      try {
        const result = await workflowExecutor.execute(
          workflowId,
          nodes,
          edges,
          updateNodeStatus,
        );
        return result;
      } finally {
        setRunning(false);
      }
    },
    [],
  );

  const abort = useCallback(() => {
    workflowExecutor.abort();
  }, []);

  const registerSchedule = useCallback(
    (workflowId: string, workflowName: string, cronExpr: string) => {
      const cron = parseCron(cronExpr);
      if (!cron) return;
      const schedule: ScheduledWorkflow = {
        workflowId,
        workflowName,
        cronExpression: cronExpr,
        nextRunAt: nextRun(cron),
        enabled: true,
      };
      workflowScheduler.register(schedule);
      setSchedules(workflowScheduler.getSchedules());
    },
    [],
  );

  const unregisterSchedule = useCallback((workflowId: string) => {
    workflowScheduler.unregister(workflowId);
    setSchedules(workflowScheduler.getSchedules());
  }, []);

  const toggleSchedule = useCallback((workflowId: string) => {
    const all = workflowScheduler.getSchedules();
    const s = all.find((x) => x.workflowId === workflowId);
    if (!s) return;
    workflowScheduler.register({ ...s, enabled: !s.enabled });
    setSchedules(workflowScheduler.getSchedules());
  }, []);

  const value = useMemo<ExecutionContextValue>(
    () => ({
      running,
      logs,
      clearLogs,
      runWorkflow,
      abort,
      schedules,
      registerSchedule,
      unregisterSchedule,
      toggleSchedule,
      describeCron,
    }),
    [running, logs, clearLogs, runWorkflow, abort, schedules, registerSchedule, unregisterSchedule, toggleSchedule],
  );

  return (
    <ExecutionContext.Provider value={value}>
      {children}
    </ExecutionContext.Provider>
  );
}
