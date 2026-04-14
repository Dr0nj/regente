import { useState, useCallback, useRef, useMemo, useEffect } from "react";
import { ReactFlowProvider } from "@xyflow/react";
import type { Node, Edge } from "@xyflow/react";
import Sidebar, { type WorkflowStats } from "@/components/Sidebar";
import FlowCanvas, { type FlowCanvasHandle } from "@/components/FlowCanvas";
import PropertiesPanel from "@/components/PropertiesPanel";
import FolderSelector from "@/components/FolderSelector";
import type { JobNodeData } from "@/lib/job-config";
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
    (_nodeId: string, update: Partial<JobNodeData>) => {
      setSelectedNodeData((prev) => (prev ? { ...prev, ...update } : null));
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
      />
      <ReactFlowProvider>
        <FlowCanvas
          ref={flowRef}
          mode={mode}
          onModeChange={setMode}
          onStatsChange={handleStatsChange}
          onNodeSelect={handleNodeSelect}
          onNodesReady={handleNodesReady}
          onSave={handleSave}
          selectedNodeId={selectedNodeId}
          focusNodeId={focusNodeId}
          initialNodes={initialNodes}
          initialEdges={initialEdges}
          workflowName={activeFolderId?.toUpperCase() ?? "Select Folder"}
          folderSelector={folderSelectorEl}
        />
      </ReactFlowProvider>
      <PropertiesPanel
        nodeData={selectedNodeData}
        nodeId={selectedNodeId}
        mode={mode}
        onClose={handleCloseProperties}
        onUpdate={handleNodeDataUpdate}
      />
    </div>
  );
}
