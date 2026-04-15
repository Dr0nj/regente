/**
 * Orchestrator Context — React bridge for the new orchestrator model.
 *
 * Provides:
 * - Today's instances (real-time)
 * - Scheduler control (start/stop, load definitions)
 * - Run Now (force/order) per definition
 * - Instance status mapping for the canvas
 * - Hold/Release/Cancel/Rerun actions
 *
 * Complements ExecutionContext (which still handles the legacy
 * workflow-level execution, metrics, and audit).
 */

import {
  createContext,
  useContext,
  useCallback,
  useEffect,
  useMemo,
  useState,
  useRef,
  type ReactNode,
} from "react";
import type { Node } from "@xyflow/react";
import type { JobNodeData, JobStatus } from "@/lib/job-config";
import type { JobDefinition, JobInstance, InstanceStatus } from "@/lib/orchestrator-model";
import {
  getTodayInstances,
  onInstanceChange,
  holdInstance,
  releaseInstance,
  cancelInstance,
  rerunInstance,
  getTodayStats,
} from "@/lib/instance-store";
import { orchestratorScheduler } from "@/lib/orchestrator-scheduler";
import { instanceExecutor } from "@/lib/instance-executor";
import { nodesToDefinitions, getSchedulableDefinitions } from "@/lib/definition-store";

/* ── Types ── */

export interface OrchestratorStats {
  total: number;
  waiting: number;
  running: number;
  ok: number;
  notok: number;
  hold: number;
  cancelled: number;
}

interface OrchestratorContextValue {
  /** Today's instances (reactive) */
  instances: JobInstance[];
  /** Stats for today */
  stats: OrchestratorStats;
  /** Whether the scheduler is running */
  schedulerRunning: boolean;
  /** Start the scheduler */
  startScheduler: () => void;
  /** Stop the scheduler */
  stopScheduler: () => void;
  /** Load definitions from canvas nodes into the scheduler */
  loadFromCanvas: (nodes: Node<JobNodeData>[]) => void;
  /** Run Now: force/order a specific job */
  forceRun: (defId: string) => JobInstance | null;
  /** Hold an instance */
  hold: (instanceId: string) => void;
  /** Release a held instance */
  release: (instanceId: string) => void;
  /** Cancel a waiting instance */
  cancel: (instanceId: string) => void;
  /** Rerun a NOTOK instance */
  rerun: (instanceId: string) => void;
  /** Get the active instance for a given definition ID */
  getInstanceForDef: (defId: string) => JobInstance | undefined;
  /** Map InstanceStatus to a JobStatus-like value for canvas rendering */
  instanceStatusToJobStatus: (status: InstanceStatus) => JobStatus;
}

const OrchestratorContext = createContext<OrchestratorContextValue | null>(null);

export function useOrchestrator(): OrchestratorContextValue {
  const ctx = useContext(OrchestratorContext);
  if (!ctx) throw new Error("useOrchestrator must be used within OrchestratorProvider");
  return ctx;
}

/* ── Status mapping ── */

function instanceStatusToJobStatus(status: InstanceStatus): JobStatus {
  switch (status) {
    case "OK":        return "SUCCESS";
    case "NOTOK":     return "FAILED";
    case "RUNNING":   return "RUNNING";
    case "WAITING":   return "WAITING";
    case "HOLD":      return "INACTIVE";
    case "CANCELLED": return "INACTIVE";
  }
}

/* ── Provider ── */

export function OrchestratorProvider({ children }: { children: ReactNode }) {
  const [instances, setInstances] = useState<JobInstance[]>(() => getTodayInstances());
  const [schedulerRunning, setSchedulerRunning] = useState(false);
  const definitionsRef = useRef<Map<string, JobDefinition>>(new Map());

  // Subscribe to instance changes (real-time)
  useEffect(() => {
    const unsub = onInstanceChange(() => {
      setInstances(getTodayInstances());
    });
    return unsub;
  }, []);

  // Wire the executor to the scheduler on mount
  useEffect(() => {
    orchestratorScheduler.setExecutor(instanceExecutor);
  }, []);

  const stats = useMemo<OrchestratorStats>(() => {
    const s = getTodayStats();
    return {
      total: instances.length,
      waiting: s.waiting,
      running: s.running,
      ok: s.ok,
      notok: s.notOk,
      hold: s.hold,
      cancelled: instances.filter((i) => i.status === "CANCELLED").length,
    };
  }, [instances]);

  const loadFromCanvas = useCallback((nodes: Node<JobNodeData>[]) => {
    const defs = getSchedulableDefinitions(nodes);
    definitionsRef.current.clear();
    for (const d of nodesToDefinitions(nodes)) {
      definitionsRef.current.set(d.id, d);
    }
    orchestratorScheduler.loadDefinitions(defs);
  }, []);

  const startScheduler = useCallback(() => {
    orchestratorScheduler.start();
    setSchedulerRunning(true);
  }, []);

  const stopScheduler = useCallback(() => {
    orchestratorScheduler.stop();
    setSchedulerRunning(false);
  }, []);

  const forceRun = useCallback((defId: string): JobInstance | null => {
    const def = definitionsRef.current.get(defId);
    if (!def) return null;
    return orchestratorScheduler.forceOrder(def);
  }, []);

  const hold = useCallback((instanceId: string) => holdInstance(instanceId), []);
  const release = useCallback((instanceId: string) => releaseInstance(instanceId), []);
  const cancel = useCallback((instanceId: string) => cancelInstance(instanceId), []);
  const rerun = useCallback((instanceId: string) => rerunInstance(instanceId), []);

  const getInstanceForDef = useCallback(
    (defId: string): JobInstance | undefined => {
      // Latest non-cancelled instance for this definition today
      return [...instances]
        .filter((i) => i.definitionId === defId && i.status !== "CANCELLED")
        .sort((a, b) => b.createdAt - a.createdAt)[0];
    },
    [instances],
  );

  const value = useMemo<OrchestratorContextValue>(
    () => ({
      instances,
      stats,
      schedulerRunning,
      startScheduler,
      stopScheduler,
      loadFromCanvas,
      forceRun,
      hold,
      release,
      cancel,
      rerun,
      getInstanceForDef,
      instanceStatusToJobStatus,
    }),
    [instances, stats, schedulerRunning, startScheduler, stopScheduler, loadFromCanvas, forceRun, hold, release, cancel, rerun, getInstanceForDef],
  );

  return (
    <OrchestratorContext.Provider value={value}>
      {children}
    </OrchestratorContext.Provider>
  );
}
