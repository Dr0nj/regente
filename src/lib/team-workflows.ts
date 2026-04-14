/**
 * Team-based workflow storage.
 *
 * Each "folder" (team) holds its own set of nodes + edges.
 * In design mode you load ONE folder at a time — like Control-M.
 *
 * Storage: localStorage now, Supabase when configured.
 */

import { supabase, isSupabaseConfigured } from "./supabase";
import type { WorkflowNode, WorkflowEdge } from "./database.types";

/* ── Types ── */

export interface TeamFolder {
  id: string;          // e.g. "time_a"
  name: string;        // e.g. "TIME_A"
  description: string;
  nodeCount: number;
  updatedAt: string;
}

export interface TeamWorkflow {
  id: string;
  name: string;
  description: string;
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
  updatedAt: string;
}

/* ── Local storage ── */

const LOCAL_KEY = "regente_team_workflows";

interface LocalStore {
  [teamId: string]: TeamWorkflow;
}

function readStore(): LocalStore {
  try {
    const raw = localStorage.getItem(LOCAL_KEY);
    return raw ? JSON.parse(raw) : {};
  } catch {
    return {};
  }
}

function writeStore(store: LocalStore) {
  localStorage.setItem(LOCAL_KEY, JSON.stringify(store));
}

/* ── CRUD ── */

/** List all team folders (metadata only — no nodes/edges) */
export async function listTeamFolders(): Promise<TeamFolder[]> {
  if (!isSupabaseConfigured) {
    const store = readStore();
    return Object.values(store).map((wf) => ({
      id: wf.id,
      name: wf.name,
      description: wf.description,
      nodeCount: wf.nodes.length,
      updatedAt: wf.updatedAt,
    }));
  }

  const { data, error } = await supabase
    .from("workflows")
    .select("id, name, description, nodes, updated_at")
    .order("name", { ascending: true });

  if (error) throw error;
  return (data ?? []).map((row) => ({
    id: row.id,
    name: row.name,
    description: row.description ?? "",
    nodeCount: Array.isArray(row.nodes) ? row.nodes.length : 0,
    updatedAt: row.updated_at,
  }));
}

/** Load a single team's full workflow (nodes + edges) */
export async function loadTeamWorkflow(teamId: string): Promise<TeamWorkflow | null> {
  if (!isSupabaseConfigured) {
    return readStore()[teamId] ?? null;
  }

  const { data, error } = await supabase
    .from("workflows")
    .select("*")
    .eq("id", teamId)
    .single();

  if (error) return null;
  return {
    id: data.id,
    name: data.name,
    description: data.description ?? "",
    nodes: data.nodes as unknown as WorkflowNode[],
    edges: data.edges as unknown as WorkflowEdge[],
    updatedAt: data.updated_at,
  };
}

/** Save (create or update) a team's workflow */
export async function saveTeamWorkflow(
  teamId: string,
  name: string,
  nodes: WorkflowNode[],
  edges: WorkflowEdge[],
  description = ""
): Promise<void> {
  const now = new Date().toISOString();

  if (!isSupabaseConfigured) {
    const store = readStore();
    store[teamId] = { id: teamId, name, description, nodes, edges, updatedAt: now };
    writeStore(store);
    return;
  }

  const { error } = await supabase
    .from("workflows")
    .upsert({
      id: teamId,
      name,
      description,
      nodes: nodes as never,
      edges: edges as never,
      owner_id: null,
    });

  if (error) throw error;
}

/** Delete a team folder */
export async function deleteTeamFolder(teamId: string): Promise<void> {
  if (!isSupabaseConfigured) {
    const store = readStore();
    delete store[teamId];
    writeStore(store);
    return;
  }

  const { error } = await supabase.from("workflows").delete().eq("id", teamId);
  if (error) throw error;
}

/* ── Demo data seeding ── */

export function seedDemoTeams(): void {
  const store = readStore();
  if (Object.keys(store).length > 0) return; // already seeded

  const now = new Date().toISOString();

  store["time_a"] = {
    id: "time_a",
    name: "TIME_A",
    description: "Ingestão de dados — extract, ETL, batch processing",
    nodes: [
      { id: "a1", type: "job", position: { x: 0, y: 0 }, data: { label: "Extract Orders", jobType: "LAMBDA", status: "SUCCESS", team: "TIME_A", lastRun: "2m ago" } },
      { id: "a2", type: "job", position: { x: 0, y: 0 }, data: { label: "ETL Pipeline", jobType: "GLUE", status: "RUNNING", team: "TIME_A", lastRun: "now" } },
      { id: "a3", type: "job", position: { x: 0, y: 0 }, data: { label: "Process Batch", jobType: "BATCH", status: "SUCCESS", team: "TIME_A", lastRun: "5m ago" } },
      { id: "a4", type: "job", position: { x: 0, y: 0 }, data: { label: "Load Warehouse", jobType: "GLUE", status: "INACTIVE", team: "TIME_A" } },
    ],
    edges: [
      { id: "ea1-2", source: "a1", target: "a2" },
      { id: "ea1-3", source: "a1", target: "a3" },
      { id: "ea2-4", source: "a2", target: "a4" },
      { id: "ea3-4", source: "a3", target: "a4" },
    ],
    updatedAt: now,
  };

  store["time_b"] = {
    id: "time_b",
    name: "TIME_B",
    description: "Processamento e validação — orquestração, regras de negócio",
    nodes: [
      { id: "b1", type: "job", position: { x: 0, y: 0 }, data: { label: "Validate Results", jobType: "CHOICE", status: "WAITING", team: "TIME_B" } },
      { id: "b2", type: "job", position: { x: 0, y: 0 }, data: { label: "Orchestrate", jobType: "STEP_FUNCTION", status: "INACTIVE", team: "TIME_B" } },
      { id: "b3", type: "job", position: { x: 0, y: 0 }, data: { label: "Apply Rules", jobType: "LAMBDA", status: "SUCCESS", team: "TIME_B", lastRun: "10m ago" } },
    ],
    edges: [
      { id: "eb1-2", source: "b1", target: "b2" },
      { id: "eb2-3", source: "b2", target: "b3" },
    ],
    updatedAt: now,
  };

  store["time_c"] = {
    id: "time_c",
    name: "TIME_C",
    description: "Agregação e saída — consolidação, relatórios, cooldown",
    nodes: [
      { id: "c1", type: "job", position: { x: 0, y: 0 }, data: { label: "Aggregate", jobType: "PARALLEL", status: "FAILED", team: "TIME_C", lastRun: "1h ago" } },
      { id: "c2", type: "job", position: { x: 0, y: 0 }, data: { label: "Generate Report", jobType: "LAMBDA", status: "INACTIVE", team: "TIME_C" } },
      { id: "c3", type: "job", position: { x: 0, y: 0 }, data: { label: "Cooldown", jobType: "WAIT", status: "INACTIVE", team: "TIME_C" } },
    ],
    edges: [
      { id: "ec1-2", source: "c1", target: "c2" },
      { id: "ec2-3", source: "c2", target: "c3" },
    ],
    updatedAt: now,
  };

  store["time_d"] = {
    id: "time_d",
    name: "TIME_D",
    description: "Monitoramento e alertas — health checks, notificações",
    nodes: [
      { id: "d1", type: "job", position: { x: 0, y: 0 }, data: { label: "Health Check", jobType: "LAMBDA", status: "SUCCESS", team: "TIME_D", lastRun: "30s ago" } },
      { id: "d2", type: "job", position: { x: 0, y: 0 }, data: { label: "Send Alerts", jobType: "STEP_FUNCTION", status: "INACTIVE", team: "TIME_D" } },
    ],
    edges: [
      { id: "ed1-2", source: "d1", target: "d2" },
    ],
    updatedAt: now,
  };

  writeStore(store);
}
