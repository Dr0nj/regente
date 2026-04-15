import { useState, useCallback, useRef, useMemo, useEffect, lazy, Suspense } from "react";
import { ReactFlowProvider } from "@xyflow/react";
import type { Node, Edge } from "@xyflow/react";
import Sidebar, { type WorkflowStats } from "@/components/Sidebar";
import FlowCanvas, { type FlowCanvasHandle } from "@/components/FlowCanvas";
import PropertiesPanel from "@/components/PropertiesPanel";
import FolderSelector from "@/components/FolderSelector";
import ExecutionLog from "@/components/ExecutionLog";
import type { JobNodeData } from "@/lib/job-config";
import type { AppMode } from "@/lib/types";
import type { TreeTeam } from "@/components/MonitoringTree";
import { validateDAG, type ValidationResult } from "@/lib/dag-validation";
import { pushVersion, loadVersion } from "@/lib/workflow-versions";
import type { WorkflowTemplate } from "@/lib/workflow-templates";
import { useExecution } from "@/lib/execution-context";
import { useOrchestrator } from "@/lib/orchestrator-context";
import {
  seedDemoTeams,
  listTeamFolders,
  loadTeamWorkflow,
  saveTeamWorkflow,
} from "@/lib/team-workflows";

/* ── Lazy-loaded panels (code splitting — Phase 9) ── */
const ValidationPanel = lazy(() => import("@/components/ValidationPanel"));
const VersionHistory = lazy(() => import("@/components/VersionHistory"));
const TemplateGallery = lazy(() => import("@/components/TemplateGallery"));
const SchedulerPanel = lazy(() => import("@/components/SchedulerPanel"));
const MetricsDashboard = lazy(() => import("@/components/MetricsDashboard"));
const AuditLog = lazy(() => import("@/components/AuditLog"));
const NotificationSettings = lazy(() => import("@/components/NotificationSettings"));

const EMPTY_STATS: WorkflowStats = {
  total: 0,
  running: 0,
  success: 0,
  failed: 0,
  waiting: 0,
};

// Map nodes to teams for the sidebar tree (uses team field from node data)
function buildTeams(nodes: Node<JobNodeData>[]): TreeTeam[] {
  const teams: Record<string, TreeTeam> = {};
  for (const node of nodes) {
    const team = (node.data as JobNodeData).team ?? "DEFAULT";
    if (!teams[team]) teams[team] = { name: team, jobs: [] };
    teams[team].jobs.push({
      id: node.id,
      label: (node.data as JobNodeData).label,
      status: (node.data as JobNodeData).status,
    });
  }
  return Object.values(teams);
}

export default function Dashboard() {
  const [mode, setMode] = useState<AppMode>("design");
  const [stats, setStats] = useState<WorkflowStats>(EMPTY_STATS);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [selectedNodeData, setSelectedNodeData] = useState<JobNodeData | null>(null);
  const [canvasNodes, setCanvasNodes] = useState<Node<JobNodeData>[]>([]);
  const [focusNodeId, setFocusNodeId] = useState<string | null>(null);
  const flowRef = useRef<FlowCanvasHandle>(null);

  // Execution engine (Phase 7)
  const { logs: execLogs, clearLogs, runWorkflow, running: engineRunning, abort: abortExecution, alertEvents, clearAlerts } = useExecution();

  // Orchestrator (Architecture Pivot)
  const {
    instances,
    stats: orchestratorStats,
    loadFromCanvas,
    forceRun,
    getInstanceForDef,
    hold: holdInst,
    release: releaseInst,
    cancel: cancelInst,
    rerun: rerunInst,
    instanceStatusToJobStatus,
  } = useOrchestrator();

  // Sidebar collapse
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const toggleSidebar = useCallback(() => setSidebarCollapsed((c) => !c), []);

  // Search/filter
  const [searchTerm, setSearchTerm] = useState("");

  // Phase 5 panels
  const [showValidation, setShowValidation] = useState(false);
  const [validationResult, setValidationResult] = useState<ValidationResult | null>(null);
  const [showVersionHistory, setShowVersionHistory] = useState(false);
  const [showTemplates, setShowTemplates] = useState(false);
  const [showScheduler, setShowScheduler] = useState(false);
  const [showMetrics, setShowMetrics] = useState(false);
  const [showAuditLog, setShowAuditLog] = useState(false);
  const [showNotificationSettings, setShowNotificationSettings] = useState(false);

  // Folder-based workflow state
  const [activeFolderId, setActiveFolderId] = useState<string | null>(null);
  const [initialNodes, setInitialNodes] = useState<Node[]>([]);
  const [initialEdges, setInitialEdges] = useState<Edge[]>([]);

  // Seed demo data on mount and auto-select first folder
  useEffect(() => {
    seedDemoTeams();
    listTeamFolders().then((folders) => {
      if (folders.length > 0 && !activeFolderId) {
        setActiveFolderId(folders[0].id);
      }
    });
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // When active folder changes, load that team's workflow
  useEffect(() => {
    if (!activeFolderId) {
      setInitialNodes([]);
      setInitialEdges([]);
      return;
    }
    loadTeamWorkflow(activeFolderId).then((wf) => {
      if (wf) {
        setInitialNodes(wf.nodes as unknown as Node[]);
        setInitialEdges(wf.edges as unknown as Edge[]);
      } else {
        setInitialNodes([]);
        setInitialEdges([]);
      }
    });
  }, [activeFolderId]);

  // Save current canvas state back to the active folder
  const handleSave = useCallback(() => {
    if (!activeFolderId) return;
    const state = flowRef.current?.getState();
    if (!state) return;
    // Filter out non-job nodes (like teamGroup) before saving
    const jobNodes = state.nodes.filter((n) => n.type === "job" || !n.type);
    const jobEdges = state.edges.filter(
      (e) => !e.source.startsWith("group-") && !e.target.startsWith("group-")
    );
    const folder = canvasNodes[0]?.data
      ? ((canvasNodes[0].data as JobNodeData).team ?? activeFolderId)
      : activeFolderId;
    saveTeamWorkflow(
      activeFolderId,
      folder.toUpperCase(),
      jobNodes as never[],
      jobEdges as never[],
    );
    // Push version snapshot
    pushVersion(activeFolderId, jobNodes as never[], jobEdges as never[], "Manual save");
  }, [activeFolderId, canvasNodes]);

  // DAG Validation
  const handleValidate = useCallback(() => {
    const state = flowRef.current?.getState();
    if (!state) return;
    const result = validateDAG(state.nodes, state.edges);
    setValidationResult(result);
    setShowValidation(true);
  }, []);

  // Version restore
  const handleVersionRestore = useCallback((version: number) => {
    if (!activeFolderId) return;
    const v = loadVersion(activeFolderId, version);
    if (!v) return;
    setInitialNodes(v.nodes as unknown as Node[]);
    setInitialEdges(v.edges as unknown as Edge[]);
  }, [activeFolderId]);

  // Template apply
  const handleTemplateApply = useCallback((template: WorkflowTemplate) => {
    setInitialNodes(template.nodes as unknown as Node[]);
    setInitialEdges(template.edges as unknown as Edge[]);
    setShowTemplates(false);
  }, []);

  const handleStatsChange = useCallback((s: WorkflowStats) => setStats(s), []);

  // In monitoring mode, override stats with orchestrator instance data
  const effectiveStats = useMemo<WorkflowStats>(() => {
    if (mode === "monitoring" && instances.length > 0) {
      return {
        total: orchestratorStats.total,
        running: orchestratorStats.running,
        success: orchestratorStats.ok,
        failed: orchestratorStats.notok,
        waiting: orchestratorStats.waiting,
      };
    }
    return stats;
  }, [mode, stats, instances, orchestratorStats]);

  const handleNodeSelect = useCallback(
    (nodeId: string | null, data: JobNodeData | null) => {
      setSelectedNodeId(nodeId);
      setSelectedNodeData(data);
    },
    []
  );

  const handleNodeDataUpdate = useCallback(
    (nodeId: string, update: Partial<JobNodeData>) => {
      setSelectedNodeData((prev) => (prev ? { ...prev, ...update } : null));
      flowRef.current?.updateNodeData(nodeId, update);
    },
    []
  );

  const handleCloseProperties = useCallback(() => {
    setSelectedNodeId(null);
    setSelectedNodeData(null);
  }, []);

  const handleNodesReady = useCallback((nodes: Node<JobNodeData>[]) => {
    setCanvasNodes(nodes);
  }, []);

  // Feed scheduler with definitions whenever canvas nodes change
  useEffect(() => {
    if (canvasNodes.length > 0) {
      loadFromCanvas(canvasNodes);
    }
  }, [canvasNodes, loadFromCanvas]);

  // In monitoring mode, project instance statuses onto canvas nodes
  useEffect(() => {
    if (mode !== "monitoring" || !flowRef.current) return;
    for (const inst of instances) {
      const jobStatus = instanceStatusToJobStatus(inst.status);
      flowRef.current.updateNodeData(inst.definitionId, {
        status: jobStatus,
        lastRun: inst.completedAt
          ? `${((inst.durationMs ?? 0) / 1000).toFixed(1)}s ago`
          : inst.startedAt
            ? "running"
            : undefined,
      });
    }
  }, [mode, instances, instanceStatusToJobStatus]);

  // When a job is clicked in the sidebar tree, focus that node in the canvas
  const handleJobFocus = useCallback((jobId: string) => {
    setFocusNodeId(jobId);
    setSelectedNodeId(jobId);
    const node = canvasNodes.find((n) => n.id === jobId);
    if (node) {
      setSelectedNodeData(node.data as JobNodeData);
    }
    flowRef.current?.focusNode(jobId);
    setTimeout(() => setFocusNodeId(null), 600);
  }, [canvasNodes]);

  // When folder is selected from FolderSelector
  const handleFolderSelect = useCallback((folderId: string) => {
    setActiveFolderId(folderId);
    setSelectedNodeId(null);
    setSelectedNodeData(null);
  }, []);

  // ── Execution via Engine (Phase 7) — updated for orchestrator pivot ──
  const handleRun = useCallback(() => {
    if (engineRunning) {
      abortExecution();
      return;
    }

    // Switch to monitoring mode
    setMode("monitoring");

    // If a specific node is selected, force-run just that one
    if (selectedNodeId) {
      forceRun(selectedNodeId);
      return;
    }

    // Otherwise, run all via the legacy workflow executor
    const state = flowRef.current?.getState();
    if (!state) return;

    const update = flowRef.current!.updateNodeData;

    runWorkflow(
      activeFolderId ?? "untitled",
      state.nodes as Node<JobNodeData>[],
      state.edges,
      update,
    );
  }, [engineRunning, abortExecution, runWorkflow, activeFolderId, selectedNodeId, forceRun]);

  // ── Export / Import workflows as JSON ──
  const handleExport = useCallback(() => {
    const state = flowRef.current?.getState();
    if (!state) return;
    const jobNodes = state.nodes.filter((n) => n.type === "job" || !n.type);
    const jobEdges = state.edges.filter(
      (e) => !e.source.startsWith("group-") && !e.target.startsWith("group-")
    );
    const payload = {
      id: activeFolderId,
      name: activeFolderId?.toUpperCase() ?? "workflow",
      nodes: jobNodes,
      edges: jobEdges,
      exportedAt: new Date().toISOString(),
    };
    const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${activeFolderId ?? "workflow"}.json`;
    a.click();
    URL.revokeObjectURL(url);
  }, [activeFolderId]);

  const handleImport = useCallback(() => {
    const input = document.createElement("input");
    input.type = "file";
    input.accept = ".json";
    input.onchange = async () => {
      const file = input.files?.[0];
      if (!file) return;
      try {
        const text = await file.text();
        const data = JSON.parse(text);
        if (!Array.isArray(data.nodes) || !Array.isArray(data.edges)) return;
        setInitialNodes(data.nodes as Node[]);
        setInitialEdges(data.edges as Edge[]);
        // If the import has an id/name, switch to it
        if (data.id && data.name) {
          await saveTeamWorkflow(data.id, data.name, data.nodes, data.edges, "Imported");
          setActiveFolderId(data.id);
        }
      } catch { /* invalid file */ }
    };
    input.click();
  }, []);

  const teams = useMemo(() => buildTeams(canvasNodes), [canvasNodes]);

  const folderSelectorEl = (
    <FolderSelector
      activeFolderId={activeFolderId}
      onSelect={handleFolderSelect}
      onCreated={handleFolderSelect}
    />
  );

  return (
    <div className="flex flex-1 overflow-hidden">
      <Sidebar
        stats={effectiveStats}
        mode={mode}
        teams={teams}
        selectedJobId={selectedNodeId}
        onJobFocus={handleJobFocus}
        collapsed={sidebarCollapsed}
        onToggleCollapse={toggleSidebar}
      />
      <ReactFlowProvider>
        <div className="flex flex-1 flex-col overflow-hidden">
          <FlowCanvas
            ref={flowRef}
            mode={mode}
            onModeChange={setMode}
            onStatsChange={handleStatsChange}
            onNodeSelect={handleNodeSelect}
            onNodesReady={handleNodesReady}
            onSave={handleSave}
            onRun={handleRun}
            onExport={handleExport}
            onImport={handleImport}
            onValidate={handleValidate}
            onVersionHistory={() => setShowVersionHistory((v) => !v)}
            onTemplates={() => setShowTemplates(true)}
            onScheduler={() => setShowScheduler((v) => !v)}
            onMetrics={() => setShowMetrics((v) => !v)}
            onAudit={() => setShowAuditLog((v) => !v)}
            onAlerts={clearAlerts}
            onNotificationSettings={() => setShowNotificationSettings((v) => !v)}
            alertCount={alertEvents.length}
            engineRunning={engineRunning}
            selectedNodeId={selectedNodeId}
            focusNodeId={focusNodeId}
            initialNodes={initialNodes}
            initialEdges={initialEdges}
            workflowName={activeFolderId?.toUpperCase() ?? "Select Folder"}
            folderSelector={folderSelectorEl}
            searchTerm={searchTerm}
            onSearchChange={setSearchTerm}
          />
          {mode === "monitoring" && (
            <ExecutionLog
              logs={execLogs}
              onClear={clearLogs}
              selectedNodeId={selectedNodeId}
            />
          )}
        </div>

        {/* Lazy-loaded panels (code splitting — Phase 9) */}
        <Suspense fallback={null}>
          {showValidation && (
            <ValidationPanel
              result={validationResult}
              onValidate={handleValidate}
              onFocusNode={handleJobFocus}
              onClose={() => setShowValidation(false)}
            />
          )}
          {showVersionHistory && (
            <VersionHistory
              folderId={activeFolderId}
              onRestore={handleVersionRestore}
              onClose={() => setShowVersionHistory(false)}
            />
          )}
          {showTemplates && (
            <TemplateGallery
              onApply={handleTemplateApply}
              onClose={() => setShowTemplates(false)}
            />
          )}
          {showScheduler && (
            <SchedulerPanel
              onClose={() => setShowScheduler(false)}
            />
          )}
          {showMetrics && (
            <MetricsDashboard
              onClose={() => setShowMetrics(false)}
            />
          )}
          {showAuditLog && (
            <AuditLog
              onClose={() => setShowAuditLog(false)}
            />
          )}
          {showNotificationSettings && (
            <NotificationSettings
              onClose={() => setShowNotificationSettings(false)}
            />
          )}
        </Suspense>
      </ReactFlowProvider>
      <PropertiesPanel
        nodeData={selectedNodeData}
        nodeId={selectedNodeId}
        mode={mode}
        instance={selectedNodeId ? getInstanceForDef(selectedNodeId) : undefined}
        onClose={handleCloseProperties}
        onUpdate={handleNodeDataUpdate}
        onDelete={(id) => { flowRef.current?.deleteNode(id); handleCloseProperties(); }}
        onDuplicate={(id) => flowRef.current?.duplicateNode(id)}
        onHold={holdInst}
        onRelease={releaseInst}
        onCancel={cancelInst}
        onRerun={rerunInst}
      />
    </div>
  );
}
