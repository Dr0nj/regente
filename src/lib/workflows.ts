import { supabase, isSupabaseConfigured } from "./supabase";
import type { Database, WorkflowNode, WorkflowEdge } from "./database.types";

type WorkflowRow = Database["public"]["Tables"]["workflows"]["Row"];
type WorkflowInsert = Database["public"]["Tables"]["workflows"]["Insert"];

export interface Workflow {
  id: string;
  name: string;
  description: string | null;
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
  createdAt: string;
  updatedAt: string;
}

/* ── Local storage fallback (demo / offline mode) ── */

const LOCAL_KEY = "regente_workflows";

function readLocal(): Workflow[] {
  try {
    const raw = localStorage.getItem(LOCAL_KEY);
    return raw ? JSON.parse(raw) : [];
  } catch {
    return [];
  }
}

function writeLocal(workflows: Workflow[]) {
  localStorage.setItem(LOCAL_KEY, JSON.stringify(workflows));
}

function toWorkflow(row: WorkflowRow): Workflow {
  return {
    id: row.id,
    name: row.name,
    description: row.description,
    nodes: row.nodes,
    edges: row.edges,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}

/* ── CRUD API ── */

/** List all workflows */
export async function listWorkflows(): Promise<Workflow[]> {
  if (!isSupabaseConfigured) return readLocal();

  const { data, error } = await supabase
    .from("workflows")
    .select("*")
    .order("updated_at", { ascending: false });

  if (error) throw error;
  return (data ?? []).map(toWorkflow);
}

/** Get single workflow by ID */
export async function getWorkflow(id: string): Promise<Workflow | null> {
  if (!isSupabaseConfigured) {
    return readLocal().find((w) => w.id === id) ?? null;
  }

  const { data, error } = await supabase
    .from("workflows")
    .select("*")
    .eq("id", id)
    .single();

  if (error) return null;
  return toWorkflow(data);
}

/** Create a new workflow */
export async function createWorkflow(
  name: string,
  nodes: WorkflowNode[] = [],
  edges: WorkflowEdge[] = [],
  description?: string
): Promise<Workflow> {
  const now = new Date().toISOString();

  if (!isSupabaseConfigured) {
    const wf: Workflow = {
      id: crypto.randomUUID(),
      name,
      description: description ?? null,
      nodes,
      edges,
      createdAt: now,
      updatedAt: now,
    };
    const all = readLocal();
    all.unshift(wf);
    writeLocal(all);
    return wf;
  }

  const insert: WorkflowInsert = { name, nodes, edges, description: description ?? null, owner_id: null };
  const { data, error } = await supabase
    .from("workflows")
    .insert(insert)
    .select()
    .single();

  if (error) throw error;
  return toWorkflow(data);
}

/** Update an existing workflow */
export async function updateWorkflow(
  id: string,
  update: { name?: string; nodes?: WorkflowNode[]; edges?: WorkflowEdge[]; description?: string }
): Promise<Workflow> {
  if (!isSupabaseConfigured) {
    const all = readLocal();
    const idx = all.findIndex((w) => w.id === id);
    if (idx < 0) throw new Error("Workflow not found");
    all[idx] = { ...all[idx], ...update, updatedAt: new Date().toISOString() };
    writeLocal(all);
    return all[idx];
  }

  const { data, error } = await supabase
    .from("workflows")
    .update(update)
    .eq("id", id)
    .select()
    .single();

  if (error) throw error;
  return toWorkflow(data);
}

/** Delete a workflow */
export async function deleteWorkflow(id: string): Promise<void> {
  if (!isSupabaseConfigured) {
    const all = readLocal().filter((w) => w.id !== id);
    writeLocal(all);
    return;
  }

  const { error } = await supabase.from("workflows").delete().eq("id", id);
  if (error) throw error;
}
