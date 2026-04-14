import { useState, useCallback, useRef, useMemo } from "react";
import { ReactFlowProvider } from "@xyflow/react";
import type { Node } from "@xyflow/react";
import Sidebar, { type WorkflowStats } from "@/components/Sidebar";
import FlowCanvas, { type FlowCanvasHandle } from "@/components/FlowCanvas";
import PropertiesPanel from "@/components/PropertiesPanel";
import type { JobNodeData } from "@/lib/job-config";
import type { AppMode } from "@/lib/types";
import type { TreeTeam } from "@/components/MonitoringTree";

const EMPTY_STATS: WorkflowStats = {
  total: 0,
  running: 0,
  success: 0,
  failed: 0,
  waiting: 0,
};

// Map nodes to teams for the sidebar tree (uses team field from node data)
const TEAM_ORDER = ["TIME_A", "TIME_B", "TIME_C", "TIME_D", "TIME_E"];

function buildTeams(nodes: Node<JobNodeData>[]): TreeTeam[] {
  const teams: Record<string, TreeTeam> = {};
  for (const node of nodes) {
    const team = (node.data as JobNodeData).team ?? "TIME_A";
    if (!teams[team]) teams[team] = { name: team, jobs: [] };
    teams[team].jobs.push({
      id: node.id,
      label: (node.data as JobNodeData).label,
      status: (node.data as JobNodeData).status,
    });
  }
  // Return in a stable order
  return TEAM_ORDER.filter((n) => teams[n]).map((n) => teams[n]);
}

export default function Dashboard() {
  const [mode, setMode] = useState<AppMode>("design");
  const [stats, setStats] = useState<WorkflowStats>(EMPTY_STATS);
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [selectedNodeData, setSelectedNodeData] = useState<JobNodeData | null>(null);
  const [canvasNodes, setCanvasNodes] = useState<Node<JobNodeData>[]>([]);
  const [focusNodeId, setFocusNodeId] = useState<string | null>(null);
  const flowRef = useRef<FlowCanvasHandle>(null);

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
    // Find node data for properties panel
    const node = canvasNodes.find((n) => n.id === jobId);
    if (node) {
      setSelectedNodeData(node.data as JobNodeData);
    }
    // Also use ref for immediate centering
    flowRef.current?.focusNode(jobId);
    // Clear focusNodeId after a tick so re-clicking the same node works
    setTimeout(() => setFocusNodeId(null), 600);
  }, [canvasNodes]);

  const teams = useMemo(() => buildTeams(canvasNodes), [canvasNodes]);

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
          selectedNodeId={selectedNodeId}
          focusNodeId={focusNodeId}
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
