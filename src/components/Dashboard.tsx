import { useState, useCallback, useRef, useMemo, useEffect } from "react";
import { ReactFlowProvider } from "@xyflow/react";
import type { Node, Edge } from "@xyflow/react";
import Sidebar, { type WorkflowStats } from "@/components/Sidebar";
import FlowCanvas, { type FlowCanvasHandle } from "@/components/FlowCanvas";
import PropertiesPanel from "@/components/PropertiesPanel";
import FolderSelector from "@/components/FolderSelector";
import ExecutionLog, { generateSimulationLogs, type LogEntry } from "@/components/ExecutionLog";
import type { JobNodeData, JobStatus } from "@/lib/job-config";
import type { AppMode } from "@/lib/types";
import type { TreeTeam } from "@/components/MonitoringTree";
import {
  seedDemoTeams,
  listTeamFolders,
  loadTeamWorkflow,
  saveTeamWorkflow,
} from "@/lib/team-workflows";

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

  // Sidebar collapse
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);
  const toggleSidebar = useCallback(() => setSidebarCollapsed((c) => !c), []);

  // Search/filter
  const [searchTerm, setSearchTerm] = useState("");

  // Execution logs
  const [execLogs, setExecLogs] = useState<LogEntry[]>([]);
  const clearLogs = useCallback(() => setExecLogs([]), []);

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
  }, [activeFolderId, canvasNodes]);

  const handleStatsChange = useCallback((s: WorkflowStats) => setStats(s), []);

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

  // ── Execution simulation ──
  const simulationRef = useRef<ReturnType<typeof setTimeout>[]>([]);

  const handleRun = useCallback(() => {
    // Clear any previous simulation
    simulationRef.current.forEach(clearTimeout);
    simulationRef.current = [];

    // Switch to monitoring mode
    setMode("monitoring");

    const state = flowRef.current?.getState();
    if (!state) return;

    const jobNodes = state.nodes.filter((n) => n.type === "job");
    const jobEdges = state.edges.filter(
      (e) => !e.source.startsWith("group-") && !e.target.startsWith("group-")
    );

    // Build adjacency list and in-degree for topological ordering
    const children = new Map<string, string[]>();
    const inDegree = new Map<string, number>();
    for (const n of jobNodes) {
      children.set(n.id, []);
      inDegree.set(n.id, 0);
    }
    for (const e of jobEdges) {
      children.get(e.source)?.push(e.target);
      inDegree.set(e.target, (inDegree.get(e.target) ?? 0) + 1);
    }

    // Topological layers (BFS)
    const layers: string[][] = [];
    let queue = jobNodes.filter((n) => (inDegree.get(n.id) ?? 0) === 0).map((n) => n.id);
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

    // Set all to WAITING first
    const update = flowRef.current!.updateNodeData;
    setExecLogs([]); // Clear previous logs
    for (const n of jobNodes) {
      update(n.id, { status: "WAITING" as JobStatus, lastRun: undefined });
    }

    // Build node name lookup
    const nameMap = new Map(jobNodes.map((n) => [n.id, (n.data as JobNodeData).label]));

    // Animate layers: each layer gets RUNNING then SUCCESS
    const LAYER_DELAY = 1500; // ms between layers
    const RUN_DURATION = 1200; // ms that a node shows RUNNING

    layers.forEach((layer, layerIdx) => {
      const startMs = layerIdx * LAYER_DELAY + 400;

      // Set layer to RUNNING + generate logs
      const t1 = setTimeout(() => {
        for (const id of layer) {
          update(id, { status: "RUNNING" as JobStatus, lastRun: "now" });
          const name = nameMap.get(id) ?? id;
          const logs = generateSimulationLogs(id, name, "RUNNING" as JobStatus, layerIdx);
          setExecLogs((prev) => [...prev, ...logs]);
        }
      }, startMs);

      // Set layer to SUCCESS/FAILED + generate completion logs
      const t2 = setTimeout(() => {
        for (const id of layer) {
          const failed = Math.random() < 0.1;
          const finalStatus = (failed ? "FAILED" : "SUCCESS") as JobStatus;
          update(id, {
            status: finalStatus,
            lastRun: failed ? "failed" : `${Math.floor(Math.random() * 5) + 1}s ago`,
          });
          const name = nameMap.get(id) ?? id;
          const logs = generateSimulationLogs(id, name, finalStatus, layerIdx);
          // Only add the completion logs (last 2-3 entries depending on status)
          const completionLogs = logs.slice(4); // skip the startup logs already added
          setExecLogs((prev) => [...prev, ...completionLogs]);
        }
      }, startMs + RUN_DURATION);

      simulationRef.current.push(t1, t2);
    });
  }, []);

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
        stats={stats}
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
      </ReactFlowProvider>
      <PropertiesPanel
        nodeData={selectedNodeData}
        nodeId={selectedNodeId}
        mode={mode}
        onClose={handleCloseProperties}
        onUpdate={handleNodeDataUpdate}
        onDelete={(id) => { flowRef.current?.deleteNode(id); handleCloseProperties(); }}
        onDuplicate={(id) => flowRef.current?.duplicateNode(id)}
      />
    </div>
  );
}
