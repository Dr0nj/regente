import { useCallback, useRef, useMemo, useEffect, useImperativeHandle, forwardRef, type DragEvent } from "react";
import {
  ReactFlow,
  Background,
  BackgroundVariant,
  MiniMap,
  useNodesState,
  useEdgesState,
  addEdge,
  useReactFlow,
  type Connection,
  type Edge,
  type Node,
} from "@xyflow/react";
import JobNodeComponent from "@/components/nodes/JobNode";
import TeamGroupComponent from "@/components/nodes/TeamGroup";
import type { TeamGroupData } from "@/components/nodes/TeamGroup";
import Toolbar from "@/components/Toolbar";
import type { WorkflowStats } from "@/components/Sidebar";
import type { JobNodeData, JobType } from "@/lib/job-config";
import type { AppMode } from "@/lib/types";
import { applyDagreLayout } from "@/lib/layout";

/* ── Demo data (will be laid out by dagre) ── */

const RAW_NODES: Node<JobNodeData>[] = [
  { id: "1", type: "job", position: { x: 0, y: 0 }, data: { label: "Extract Orders", jobType: "LAMBDA", status: "SUCCESS", lastRun: "2m ago", team: "TIME_A" } },
  { id: "2", type: "job", position: { x: 0, y: 0 }, data: { label: "ETL Pipeline", jobType: "GLUE", status: "RUNNING", lastRun: "now", team: "TIME_A" } },
  { id: "3", type: "job", position: { x: 0, y: 0 }, data: { label: "Process Batch", jobType: "BATCH", status: "SUCCESS", lastRun: "5m ago", team: "TIME_A" } },
  { id: "4", type: "job", position: { x: 0, y: 0 }, data: { label: "Validate Results", jobType: "CHOICE", status: "WAITING", team: "TIME_B" } },
  { id: "5", type: "job", position: { x: 0, y: 0 }, data: { label: "Orchestrate", jobType: "STEP_FUNCTION", status: "INACTIVE", team: "TIME_B" } },
  { id: "6", type: "job", position: { x: 0, y: 0 }, data: { label: "Aggregate", jobType: "PARALLEL", status: "FAILED", lastRun: "1h ago", team: "TIME_C" } },
  { id: "7", type: "job", position: { x: 0, y: 0 }, data: { label: "Cooldown", jobType: "WAIT", status: "INACTIVE", team: "TIME_C" } },
];

const RAW_EDGES: Edge[] = [
  { id: "e1-2", source: "1", target: "2", animated: true },
  { id: "e1-3", source: "1", target: "3", animated: true },
  { id: "e2-4", source: "2", target: "4" },
  { id: "e3-5", source: "3", target: "5" },
  { id: "e4-6", source: "4", target: "6" },
  { id: "e5-6", source: "5", target: "6" },
  { id: "e6-7", source: "6", target: "7" },
];

// Auto-layout on init
const { nodes: INITIAL_NODES, edges: INITIAL_EDGES } = applyDagreLayout(
  RAW_NODES,
  RAW_EDGES,
  "TB"
);

const nodeTypes = { job: JobNodeComponent, teamGroup: TeamGroupComponent };

/* ── Team group boundary computation ── */

const TEAM_COLOR_MAP: Record<string, string> = {
  TIME_A: "#22d3ee",
  TIME_B: "#a855f7",
  TIME_C: "#f59e0b",
  TIME_D: "#10b981",
  TIME_E: "#f43f5e",
};

const NODE_W = 240;
const NODE_H = 110;
const GROUP_PAD = 40;

function computeTeamGroupNodes(jobNodes: Node<JobNodeData>[]): Node<TeamGroupData>[] {
  const teams = new Map<string, Node<JobNodeData>[]>();
  for (const node of jobNodes) {
    const team = node.data.team;
    if (!team) continue;
    if (!teams.has(team)) teams.set(team, []);
    teams.get(team)!.push(node);
  }

  const groups: Node<TeamGroupData>[] = [];
  for (const [teamName, members] of teams) {
    const xs = members.map((n) => n.position.x);
    const ys = members.map((n) => n.position.y);
    const minX = Math.min(...xs) - GROUP_PAD;
    const minY = Math.min(...ys) - GROUP_PAD;
    const maxX = Math.max(...xs) + NODE_W + GROUP_PAD;
    const maxY = Math.max(...ys) + NODE_H + GROUP_PAD;

    const w = maxX - minX;
    const h = maxY - minY;

    groups.push({
      id: `group-${teamName}`,
      type: "teamGroup",
      position: { x: minX, y: minY },
      data: {
        label: teamName,
        color: TEAM_COLOR_MAP[teamName] ?? "#22d3ee",
        jobCount: members.length,
        groupWidth: w,
        groupHeight: h,
      },
      width: w,
      height: h,
      style: { width: w, height: h },
      selectable: false,
      draggable: false,
      zIndex: 0,
    });
  }
  return groups;
}

/* ── Component ── */

interface FlowCanvasProps {
  mode: AppMode;
  onModeChange: (mode: AppMode) => void;
  onStatsChange: (stats: WorkflowStats) => void;
  onNodeSelect: (nodeId: string | null, data: JobNodeData | null) => void;
  onNodesReady?: (nodes: Node<JobNodeData>[]) => void;
  selectedNodeId: string | null;
  focusNodeId?: string | null;
}

export interface FlowCanvasHandle {
  focusNode: (nodeId: string) => void;
}

let nodeIdCounter = 100;

const FlowCanvas = forwardRef<FlowCanvasHandle, FlowCanvasProps>(function FlowCanvas({
  mode,
  onModeChange,
  onStatsChange,
  onNodeSelect,
  onNodesReady,
  selectedNodeId,
  focusNodeId,
}, ref) {
  const [nodes, setNodes, onNodesChange] = useNodesState(INITIAL_NODES);
  const [edges, setEdges, onEdgesChange] = useEdgesState(INITIAL_EDGES);
  const reactFlowWrapper = useRef<HTMLDivElement>(null);
  const { screenToFlowPosition, fitView, zoomIn, zoomOut, setCenter, getZoom } = useReactFlow();

  const isDesign = mode === "design";

  // Expose focusNode to parent via ref
  useImperativeHandle(ref, () => ({
    focusNode: (nodeId: string) => {
      const target = nodes.find((n) => n.id === nodeId);
      if (!target) return;
      const x = target.position.x + 120; // center of 240px wide node
      const y = target.position.y + 40;
      const zoom = Math.max(getZoom(), 0.8);
      setCenter(x, y, { zoom, duration: 500 });
      onNodeSelect(nodeId, target.data as JobNodeData);
    },
  }), [nodes, setCenter, getZoom, onNodeSelect]);

  // When focusNodeId changes externally, center on it
  useEffect(() => {
    if (!focusNodeId) return;
    const target = nodes.find((n) => n.id === focusNodeId);
    if (!target) return;
    const x = target.position.x + 120;
    const y = target.position.y + 40;
    const zoom = Math.max(getZoom(), 0.8);
    setCenter(x, y, { zoom, duration: 500 });
  }, [focusNodeId, nodes, setCenter, getZoom]);

  // Report nodes to parent for sidebar tree
  useEffect(() => {
    onNodesReady?.(nodes as Node<JobNodeData>[]);
  }, [nodes, onNodesReady]);

  /* Connect */
  const onConnect = useCallback(
    (params: Connection) => {
      if (!isDesign) return;
      setEdges((eds) => addEdge({ ...params, animated: false }, eds));
    },
    [setEdges, isDesign]
  );

  /* Stats */
  const stats = useMemo(() => {
    const s: WorkflowStats = { total: 0, running: 0, success: 0, failed: 0, waiting: 0 };
    for (const node of nodes) {
      const d = node.data as JobNodeData;
      s.total++;
      if (d.status === "RUNNING") s.running++;
      else if (d.status === "SUCCESS") s.success++;
      else if (d.status === "FAILED") s.failed++;
      else if (d.status === "WAITING") s.waiting++;
    }
    return s;
  }, [nodes]);

  useMemo(() => { onStatsChange(stats); }, [stats, onStatsChange]);

  /* Node click */
  const onNodeClick = useCallback(
    (_: React.MouseEvent, node: Node) => {
      onNodeSelect(node.id, node.data as JobNodeData);
    },
    [onNodeSelect]
  );

  const onPaneClick = useCallback(() => {
    onNodeSelect(null, null);
  }, [onNodeSelect]);

  /* Auto layout */
  const handleAutoLayout = useCallback(() => {
    // Only re-layout job nodes (filter out group nodes)
    const jobNodes = nodes.filter((n) => n.type === "job");
    const { nodes: layouted } = applyDagreLayout(jobNodes, edges, "TB");
    setNodes(layouted);
    setTimeout(() => fitView({ padding: 0.2, duration: 400 }), 50);
  }, [nodes, edges, setNodes, fitView]);

  /* Drag & Drop (design only) */
  const onDragOver = useCallback(
    (event: DragEvent) => {
      if (!isDesign) return;
      event.preventDefault();
      event.dataTransfer.dropEffect = "move";
    },
    [isDesign]
  );

  const onDrop = useCallback(
    (event: DragEvent) => {
      if (!isDesign) return;
      event.preventDefault();
      const jobType = event.dataTransfer.getData("application/regente-job-type") as JobType;
      if (!jobType) return;

      const position = screenToFlowPosition({
        x: event.clientX,
        y: event.clientY,
      });

      const newNode: Node<JobNodeData> = {
        id: `node-${++nodeIdCounter}`,
        type: "job",
        position,
        data: {
          label: `New ${jobType.replace("_", " ")}`,
          jobType,
          status: "INACTIVE",
        },
      };

      setNodes((nds) => [...nds, newNode]);
    },
    [isDesign, screenToFlowPosition, setNodes]
  );

  // Inject mode into node data so nodes know which mode they're in
  const nodesWithMode = useMemo(
    () => {
      const jobNodes = nodes.map((n) => ({
        ...n,
        data: { ...n.data, mode },
        selected: n.id === selectedNodeId,
        zIndex: 10,
      }));
      // Recompute group boundaries from current job node positions
      const groupNodes = computeTeamGroupNodes(nodes as Node<JobNodeData>[]);
      return [...groupNodes, ...jobNodes];
    },
    [nodes, mode, selectedNodeId]
  );

  // Build edges with colors based on source node status
  const edgesWithMode = useMemo(() => {
    const nodeMap = new Map(nodes.map((n) => [n.id, n.data as JobNodeData]));
    return edges.map((e) => {
      const sourceData = nodeMap.get(e.source);
      const sourceStatus = sourceData?.status ?? "INACTIVE";

      let stroke = "#334155"; // default slate
      let animated = false;
      let dasharray: string | undefined;
      let opacity = 0.5;

      if (mode === "monitoring") {
        switch (sourceStatus) {
          case "SUCCESS":
            stroke = "#10b981";
            animated = false;
            opacity = 0.9;
            break;
          case "RUNNING":
            stroke = "#22d3ee";
            animated = true;
            dasharray = "6 4";
            opacity = 1;
            break;
          case "FAILED":
            stroke = "#ef4444";
            animated = false;
            opacity = 0.9;
            break;
          case "WAITING":
            stroke = "#f59e0b";
            animated = false;
            dasharray = "3 3";
            opacity = 0.7;
            break;
          default:
            stroke = "#334155";
            opacity = 0.35;
            break;
        }
      } else {
        // Design mode: neutral
        stroke = "#64748b";
        animated = !!e.animated;
        opacity = 0.5;
      }

      return {
        ...e,
        animated,
        style: {
          stroke,
          strokeWidth: mode === "monitoring" ? 2 : 1.5,
          opacity,
          ...(dasharray && !animated ? { strokeDasharray: dasharray } : {}),
          ...(animated ? { strokeDasharray: "6 4", animation: "flow-dash 1s linear infinite" } : {}),
        },
      };
    });
  }, [edges, nodes, mode]);

  return (
    <div className="flex flex-1 flex-col">
      <Toolbar
        mode={mode}
        onModeChange={onModeChange}
        onFitView={() => fitView({ padding: 0.2, duration: 400 })}
        onZoomIn={() => zoomIn({ duration: 250 })}
        onZoomOut={() => zoomOut({ duration: 250 })}
        onAutoLayout={handleAutoLayout}
      />

      <div ref={reactFlowWrapper} className="flex-1 relative pt-4" style={{ backgroundColor: "#0a0f1c" }}>
        {/* Green dots background for canvas */}
        <ReactFlow
          nodes={nodesWithMode}
          edges={edgesWithMode}
          onNodesChange={isDesign ? onNodesChange : undefined}
          onEdgesChange={isDesign ? onEdgesChange : undefined}
          onConnect={onConnect}
          onNodeClick={onNodeClick}
          onPaneClick={onPaneClick}
          onDragOver={onDragOver}
          onDrop={onDrop}
          nodeTypes={nodeTypes}
          fitView
          fitViewOptions={{ padding: 0.2 }}
          proOptions={{ hideAttribution: true }}
          defaultEdgeOptions={{ type: "smoothstep" }}
          connectionLineStyle={{ stroke: "#64748b", strokeWidth: 1.5, opacity: 0.5 }}
          deleteKeyCode={isDesign ? ["Backspace", "Delete"] : []}
          nodesDraggable={isDesign}
          nodesConnectable={isDesign}
          elementsSelectable={true}
          panOnDrag={true}
          zoomOnScroll={true}
        >
          <Background
            variant={BackgroundVariant.Dots}
            gap={20}
            size={1.6}
            color="#133524"
          />
          <MiniMap
            nodeStrokeWidth={2}
            nodeColor={(n) => {
              const d = n.data as JobNodeData;
              if (d.status === "RUNNING") return "#22d3ee";
              if (d.status === "SUCCESS") return "#10b981";
              if (d.status === "FAILED") return "#ef4444";
              if (d.status === "WAITING") return "#f59e0b";
              return "#1e293b";
            }}
            maskColor="rgba(2, 4, 8, 0.92)"
            pannable
            zoomable
            style={{
              borderRadius: 14,
              border: "1px solid rgba(255,255,255,0.05)",
              background: "rgba(6, 10, 20, 0.95)",
              backdropFilter: "blur(16px)",
              boxShadow: "0 4px 24px rgba(0,0,0,0.4), inset 0 1px 0 rgba(255,255,255,0.03)",
            }}
          />
        </ReactFlow>
      </div>
    </div>
  );
});

export default FlowCanvas;
