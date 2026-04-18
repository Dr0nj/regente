import { useState, useCallback, useRef, useMemo, useEffect, useImperativeHandle, forwardRef, type DragEvent } from "react";
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
import { applyDagreLayout, applyDagreLayoutByFolder } from "@/lib/layout";
import ContextMenu from "@/components/ContextMenu";

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
  onSave?: () => void;
  onRun?: () => void;
  onExport?: () => void;
  onImport?: () => void;
  selectedNodeId: string | null;
  focusNodeId?: string | null;
  initialNodes?: Node[];
  initialEdges?: Edge[];
  workflowName?: string;
  folderSelector?: React.ReactNode;
  searchTerm?: string;
  onSearchChange?: (term: string) => void;
  onValidate?: () => void;
  onVersionHistory?: () => void;
  onTemplates?: () => void;
  onScheduler?: () => void;
  onMetrics?: () => void;
  onAudit?: () => void;
  onAlerts?: () => void;
  onNotificationSettings?: () => void;
  alertCount?: number;
  engineRunning?: boolean;
}

export interface FlowCanvasHandle {
  focusNode: (nodeId: string) => void;
  getState: () => { nodes: Node[]; edges: Edge[] };
  updateNodeData: (nodeId: string, update: Partial<JobNodeData>) => void;
  deleteNode: (nodeId: string) => void;
  duplicateNode: (nodeId: string) => void;
  undo: () => void;
  redo: () => void;
}

let nodeIdCounter = 100;

const FlowCanvas = forwardRef<FlowCanvasHandle, FlowCanvasProps>(function FlowCanvas({
  mode,
  onModeChange,
  onStatsChange,
  onNodeSelect,
  onNodesReady,
  onSave,
  onRun,
  onExport,
  onImport,
  selectedNodeId,
  focusNodeId,
  initialNodes = [],
  initialEdges = [],
  workflowName,
  folderSelector,
  searchTerm = "",
  onSearchChange,
  onValidate,
  onVersionHistory,
  onTemplates,
  onScheduler,
  onMetrics,
  onAudit,
  onAlerts,
  onNotificationSettings,
  alertCount,
  engineRunning,
}, ref) {
  const [nodes, setNodes, onNodesChange] = useNodesState(initialNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(initialEdges);
  const reactFlowWrapper = useRef<HTMLDivElement>(null);
  const { screenToFlowPosition, fitView, zoomIn, zoomOut, setCenter, getZoom } = useReactFlow();

  const isDesign = mode === "design";

  // ── Undo/Redo history ──
  const historyRef = useRef<{ nodes: Node[]; edges: Edge[] }[]>([]);
  const historyPointerRef = useRef(-1);
  const skipHistoryRef = useRef(false);
  const latestStateRef = useRef({ nodes, edges });
  const [, _forceHistoryRender] = useState(0);

  useEffect(() => { latestStateRef.current = { nodes, edges }; });

  const pushHistory = useCallback(() => {
    if (skipHistoryRef.current) return;
    const { nodes: ns, edges: es } = latestStateRef.current;
    const snap = { nodes: ns.map((n) => ({ ...n, data: { ...n.data } })), edges: es.map((e) => ({ ...e })) };
    const h = historyRef.current.slice(0, historyPointerRef.current + 1);
    h.push(snap);
    if (h.length > 50) h.shift();
    historyRef.current = h;
    historyPointerRef.current = h.length - 1;
    _forceHistoryRender((c) => c + 1);
  }, []);

  const handleUndo = useCallback(() => {
    if (historyPointerRef.current <= 0) return;
    historyPointerRef.current--;
    const snap = historyRef.current[historyPointerRef.current];
    skipHistoryRef.current = true;
    setNodes(snap.nodes);
    setEdges(snap.edges);
    skipHistoryRef.current = false;
    _forceHistoryRender((c) => c + 1);
  }, [setNodes, setEdges]);

  const handleRedo = useCallback(() => {
    if (historyPointerRef.current >= historyRef.current.length - 1) return;
    historyPointerRef.current++;
    const snap = historyRef.current[historyPointerRef.current];
    skipHistoryRef.current = true;
    setNodes(snap.nodes);
    setEdges(snap.edges);
    skipHistoryRef.current = false;
    _forceHistoryRender((c) => c + 1);
  }, [setNodes, setEdges]);

  const canUndo = historyPointerRef.current > 0;
  const canRedo = historyPointerRef.current < historyRef.current.length - 1;

  // ── Context menu ──
  const [contextMenu, setContextMenu] = useState<{ x: number; y: number; nodeId: string } | null>(null);
  const closeContextMenu = useCallback(() => setContextMenu(null), []);

  /* Disconnect all edges from a node */
  const disconnectNode = useCallback(
    (nodeId: string) => {
      pushHistory();
      setEdges((eds) => eds.filter((e) => e.source !== nodeId && e.target !== nodeId));
    },
    [pushHistory, setEdges],
  );

  // Patch a canvas node's data (called from handle / parent ref)
  const updateNodeData = useCallback(
    (nodeId: string, update: Partial<JobNodeData>) => {
      setNodes((nds) =>
        nds.map((n) =>
          n.id === nodeId ? { ...n, data: { ...n.data, ...update } } : n
        )
      );
    },
    [setNodes]
  );

  // Delete a node and its connected edges
  const deleteNode = useCallback(
    (nodeId: string) => {
      pushHistory();
      setNodes((nds) => nds.filter((n) => n.id !== nodeId));
      setEdges((eds) => eds.filter((e) => e.source !== nodeId && e.target !== nodeId));
    },
    [setNodes, setEdges]
  );

  // Duplicate a node (offset position slightly)
  const duplicateNode = useCallback(
    (nodeId: string) => {
      pushHistory();
      const source = nodes.find((n) => n.id === nodeId);
      if (!source) return;
      const newId = `node-${++nodeIdCounter}`;
      const clone: Node<JobNodeData> = {
        id: newId,
        type: source.type ?? "job",
        position: { x: source.position.x + 40, y: source.position.y + 40 },
        data: { ...(source.data as JobNodeData), label: `${(source.data as JobNodeData).label} (copy)` },
      };
      setNodes((nds) => [...nds, clone]);
    },
    [nodes, setNodes]
  );

  // Expose focusNode to parent via ref
  useImperativeHandle(ref, () => ({
    focusNode: (nodeId: string) => {
      const target = nodes.find((n) => n.id === nodeId);
      if (!target) return;
      const x = target.position.x + 120;
      const y = target.position.y + 40;
      const zoom = Math.max(getZoom(), 0.8);
      setCenter(x, y, { zoom, duration: 500 });
      onNodeSelect(nodeId, target.data as JobNodeData);
    },
    getState: () => ({ nodes, edges }),
    updateNodeData,
    deleteNode,
    duplicateNode,
    undo: handleUndo,
    redo: handleRedo,
  }), [nodes, edges, setCenter, getZoom, onNodeSelect, updateNodeData, deleteNode, duplicateNode, handleUndo, handleRedo]);

  // When external data changes (folder switch), reload canvas
  useEffect(() => {
    if (initialNodes.length === 0 && initialEdges.length === 0) {
      setNodes([]);
      setEdges([]);
      return;
    }
    const { nodes: laid, edges: laidE } = applyDagreLayoutByFolder(initialNodes, initialEdges);
    setNodes(laid);
    setEdges(laidE);
    // Push initial state as first history entry
    historyRef.current = [{ nodes: laid.map((n) => ({ ...n, data: { ...n.data } })), edges: laidE.map((e) => ({ ...e })) }];
    historyPointerRef.current = 0;
    _forceHistoryRender((c) => c + 1);
    setTimeout(() => fitView({ padding: 0.2, duration: 400 }), 100);
  }, [initialNodes, initialEdges]); // eslint-disable-line react-hooks/exhaustive-deps

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

  // Keyboard shortcuts (design mode only)
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      // Ignore when typing in inputs
      const tag = (e.target as HTMLElement)?.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;

      if (e.ctrlKey && e.key === "s") {
        e.preventDefault();
        onSave?.();
      } else if (e.ctrlKey && e.shiftKey && e.key.toLowerCase() === "z" && isDesign) {
        e.preventDefault();
        handleRedo();
      } else if (e.ctrlKey && e.key === "z" && isDesign) {
        e.preventDefault();
        handleUndo();
      } else if (e.ctrlKey && e.key === "d" && isDesign) {
        e.preventDefault();
        const sel = nodes.find((n) => n.id === selectedNodeId);
        if (sel) duplicateNode(sel.id);
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [isDesign, nodes, selectedNodeId, onSave, duplicateNode, handleUndo, handleRedo]);

  // Report nodes to parent for sidebar tree
  useEffect(() => {
    onNodesReady?.(nodes as Node<JobNodeData>[]);
  }, [nodes, onNodesReady]);

  /* Connect */
  const onConnect = useCallback(
    (params: Connection) => {
      if (!isDesign) return;
      pushHistory();
      // Auto-label edges from CHOICE nodes
      const sourceNode = nodes.find((n) => n.id === params.source);
      const sourceData = sourceNode?.data as JobNodeData | undefined;
      let label: string | undefined;
      if (sourceData?.jobType === "CHOICE") {
        const existingFromSource = edges.filter((e) => e.source === params.source).length;
        if (existingFromSource === 0) label = "True";
        else if (existingFromSource === 1) label = "False";
        else label = `Branch ${existingFromSource + 1}`;
      }
      setEdges((eds) => addEdge({ ...params, animated: false, ...(label ? { label, data: { conditionLabel: label } } : {}) }, eds));
    },
    [setEdges, isDesign, pushHistory, nodes, edges]
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
    closeContextMenu();
  }, [onNodeSelect, closeContextMenu]);

  /* Auto layout */
  const handleAutoLayout = useCallback(() => {
    pushHistory();
    // Only re-layout job nodes (filter out group nodes)
    const jobNodes = nodes.filter((n) => n.type === "job");
    const { nodes: layouted } = applyDagreLayoutByFolder(jobNodes, edges);
    setNodes(layouted);
    setTimeout(() => fitView({ padding: 0.2, duration: 400 }), 50);
  }, [nodes, edges, setNodes, fitView, pushHistory]);

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

      pushHistory();
      setNodes((nds) => [...nds, newNode]);
    },
    [isDesign, screenToFlowPosition, setNodes, pushHistory]
  );

  // Inject mode into node data so nodes know which mode they're in
  const nodesWithMode = useMemo(
    () => {
      const term = searchTerm.toLowerCase().trim();
      const jobNodes = nodes.map((n) => {
        const label = ((n.data as JobNodeData).label ?? "").toLowerCase();
        const dimmed = term.length > 0 && !label.includes(term);
        return {
          ...n,
          data: { ...n.data, mode },
          selected: n.id === selectedNodeId,
          zIndex: 10,
          style: dimmed ? { opacity: 0.2, transition: "opacity 0.2s" } : { opacity: 1, transition: "opacity 0.2s" },
        };
      });
      // Recompute group boundaries from current job node positions
      const groupNodes = computeTeamGroupNodes(nodes as Node<JobNodeData>[]);
      return [...groupNodes, ...jobNodes];
    },
    [nodes, mode, selectedNodeId, searchTerm]
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
        // Preserve edge labels (from CHOICE routing)
        ...(e.label ? {
          label: e.label,
          labelStyle: { fill: "#94a3b8", fontSize: 10, fontWeight: 600 },
          labelBgStyle: { fill: "#0f172a", fillOpacity: 0.9 },
          labelBgPadding: [6, 3] as [number, number],
          labelBgBorderRadius: 6,
        } : {}),
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
        onSave={onSave}
        onRun={onRun}
        onExport={onExport}
        onImport={onImport}
        onUndo={handleUndo}
        onRedo={handleRedo}
        canUndo={canUndo}
        canRedo={canRedo}
        searchTerm={searchTerm}
        onSearchChange={onSearchChange}
        onValidate={onValidate}
        onVersionHistory={onVersionHistory}
        onTemplates={onTemplates}
        onScheduler={onScheduler}
        onMetrics={onMetrics}
        onAudit={onAudit}
        onAlerts={onAlerts}
        onNotificationSettings={onNotificationSettings}
        alertCount={alertCount}
        engineRunning={engineRunning}
        workflowName={workflowName}
        folderSelector={folderSelector}
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
          onNodeContextMenu={(event, node) => {
            event.preventDefault();
            if (!isDesign) return;
            setContextMenu({ x: event.clientX, y: event.clientY, nodeId: node.id });
          }}
          onNodeDragStop={() => pushHistory()}
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

      {/* Context menu */}
      {contextMenu && isDesign && (
        <ContextMenu
          x={contextMenu.x}
          y={contextMenu.y}
          onFocus={() => {
            const target = nodes.find((n) => n.id === contextMenu.nodeId);
            if (target) {
              setCenter(target.position.x + 120, target.position.y + 40, { zoom: Math.max(getZoom(), 0.8), duration: 500 });
              onNodeSelect(contextMenu.nodeId, target.data as JobNodeData);
            }
          }}
          onDuplicate={() => duplicateNode(contextMenu.nodeId)}
          onDisconnect={() => disconnectNode(contextMenu.nodeId)}
          onDelete={() => deleteNode(contextMenu.nodeId)}
          onClose={closeContextMenu}
        />
      )}
    </div>
  );
});

export default FlowCanvas;
