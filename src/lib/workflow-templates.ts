/**
 * Workflow Template Library — Phase 5
 *
 * Pre-built workflow patterns that can be loaded into the canvas.
 */

import type { WorkflowNode, WorkflowEdge } from "./database.types";

export interface WorkflowTemplate {
  id: string;
  name: string;
  description: string;
  category: "data" | "ml" | "devops" | "general";
  nodes: WorkflowNode[];
  edges: WorkflowEdge[];
}

const TEMPLATES: WorkflowTemplate[] = [
  {
    id: "etl-basic",
    name: "Basic ETL Pipeline",
    description: "Extract → Transform → Load pattern with validation",
    category: "data",
    nodes: [
      { id: "t1", type: "job", position: { x: 0, y: 0 }, data: { label: "Extract Source", jobType: "LAMBDA", status: "INACTIVE", team: "ETL" } },
      { id: "t2", type: "job", position: { x: 0, y: 0 }, data: { label: "Transform Data", jobType: "GLUE", status: "INACTIVE", team: "ETL" } },
      { id: "t3", type: "job", position: { x: 0, y: 0 }, data: { label: "Validate Schema", jobType: "CHOICE", status: "INACTIVE", team: "ETL" } },
      { id: "t4", type: "job", position: { x: 0, y: 0 }, data: { label: "Load to Warehouse", jobType: "BATCH", status: "INACTIVE", team: "ETL" } },
    ],
    edges: [
      { id: "te1", source: "t1", target: "t2" },
      { id: "te2", source: "t2", target: "t3" },
      { id: "te3", source: "t3", target: "t4" },
    ],
  },
  {
    id: "fan-out-in",
    name: "Fan-Out / Fan-In",
    description: "Parallel processing with aggregation",
    category: "general",
    nodes: [
      { id: "f1", type: "job", position: { x: 0, y: 0 }, data: { label: "Dispatch", jobType: "LAMBDA", status: "INACTIVE", team: "WORKERS" } },
      { id: "f2", type: "job", position: { x: 0, y: 0 }, data: { label: "Worker A", jobType: "BATCH", status: "INACTIVE", team: "WORKERS" } },
      { id: "f3", type: "job", position: { x: 0, y: 0 }, data: { label: "Worker B", jobType: "BATCH", status: "INACTIVE", team: "WORKERS" } },
      { id: "f4", type: "job", position: { x: 0, y: 0 }, data: { label: "Worker C", jobType: "BATCH", status: "INACTIVE", team: "WORKERS" } },
      { id: "f5", type: "job", position: { x: 0, y: 0 }, data: { label: "Aggregate", jobType: "PARALLEL", status: "INACTIVE", team: "WORKERS" } },
    ],
    edges: [
      { id: "fe1", source: "f1", target: "f2" },
      { id: "fe2", source: "f1", target: "f3" },
      { id: "fe3", source: "f1", target: "f4" },
      { id: "fe4", source: "f2", target: "f5" },
      { id: "fe5", source: "f3", target: "f5" },
      { id: "fe6", source: "f4", target: "f5" },
    ],
  },
  {
    id: "ml-pipeline",
    name: "ML Training Pipeline",
    description: "Data prep → Train → Evaluate → Deploy",
    category: "ml",
    nodes: [
      { id: "m1", type: "job", position: { x: 0, y: 0 }, data: { label: "Fetch Dataset", jobType: "LAMBDA", status: "INACTIVE", team: "ML" } },
      { id: "m2", type: "job", position: { x: 0, y: 0 }, data: { label: "Prep Features", jobType: "GLUE", status: "INACTIVE", team: "ML" } },
      { id: "m3", type: "job", position: { x: 0, y: 0 }, data: { label: "Train Model", jobType: "BATCH", status: "INACTIVE", team: "ML" } },
      { id: "m4", type: "job", position: { x: 0, y: 0 }, data: { label: "Evaluate", jobType: "CHOICE", status: "INACTIVE", team: "ML" } },
      { id: "m5", type: "job", position: { x: 0, y: 0 }, data: { label: "Deploy Model", jobType: "STEP_FUNCTION", status: "INACTIVE", team: "ML" } },
      { id: "m6", type: "job", position: { x: 0, y: 0 }, data: { label: "Notify Team", jobType: "LAMBDA", status: "INACTIVE", team: "ML" } },
    ],
    edges: [
      { id: "me1", source: "m1", target: "m2" },
      { id: "me2", source: "m2", target: "m3" },
      { id: "me3", source: "m3", target: "m4" },
      { id: "me4", source: "m4", target: "m5" },
      { id: "me5", source: "m5", target: "m6" },
    ],
  },
  {
    id: "ci-cd",
    name: "CI/CD Pipeline",
    description: "Build → Test → Stage → Deploy with gate",
    category: "devops",
    nodes: [
      { id: "d1", type: "job", position: { x: 0, y: 0 }, data: { label: "Build", jobType: "BATCH", status: "INACTIVE", team: "DEVOPS" } },
      { id: "d2", type: "job", position: { x: 0, y: 0 }, data: { label: "Unit Tests", jobType: "LAMBDA", status: "INACTIVE", team: "DEVOPS" } },
      { id: "d3", type: "job", position: { x: 0, y: 0 }, data: { label: "Integration Tests", jobType: "LAMBDA", status: "INACTIVE", team: "DEVOPS" } },
      { id: "d4", type: "job", position: { x: 0, y: 0 }, data: { label: "Deploy Staging", jobType: "STEP_FUNCTION", status: "INACTIVE", team: "DEVOPS" } },
      { id: "d5", type: "job", position: { x: 0, y: 0 }, data: { label: "Approval Gate", jobType: "WAIT", status: "INACTIVE", team: "DEVOPS" } },
      { id: "d6", type: "job", position: { x: 0, y: 0 }, data: { label: "Deploy Prod", jobType: "STEP_FUNCTION", status: "INACTIVE", team: "DEVOPS" } },
    ],
    edges: [
      { id: "de1", source: "d1", target: "d2" },
      { id: "de2", source: "d1", target: "d3" },
      { id: "de3", source: "d2", target: "d4" },
      { id: "de4", source: "d3", target: "d4" },
      { id: "de5", source: "d4", target: "d5" },
      { id: "de6", source: "d5", target: "d6" },
    ],
  },
  {
    id: "retry-pattern",
    name: "Retry with Cooldown",
    description: "Job → Check → Retry/Wait loop → Final",
    category: "general",
    nodes: [
      { id: "r1", type: "job", position: { x: 0, y: 0 }, data: { label: "Execute Job", jobType: "LAMBDA", status: "INACTIVE", team: "OPS" } },
      { id: "r2", type: "job", position: { x: 0, y: 0 }, data: { label: "Check Result", jobType: "CHOICE", status: "INACTIVE", team: "OPS" } },
      { id: "r3", type: "job", position: { x: 0, y: 0 }, data: { label: "Cooldown", jobType: "WAIT", status: "INACTIVE", team: "OPS" } },
      { id: "r4", type: "job", position: { x: 0, y: 0 }, data: { label: "Finalize", jobType: "STEP_FUNCTION", status: "INACTIVE", team: "OPS" } },
    ],
    edges: [
      { id: "re1", source: "r1", target: "r2" },
      { id: "re2", source: "r2", target: "r3" },
      { id: "re3", source: "r3", target: "r1" },
      { id: "re4", source: "r2", target: "r4" },
    ],
  },
];

export function getTemplates(): WorkflowTemplate[] {
  return TEMPLATES;
}

export function getTemplateById(id: string): WorkflowTemplate | undefined {
  return TEMPLATES.find((t) => t.id === id);
}

export function getTemplatesByCategory(category: WorkflowTemplate["category"]): WorkflowTemplate[] {
  return TEMPLATES.filter((t) => t.category === category);
}

export const TEMPLATE_CATEGORIES: { id: WorkflowTemplate["category"]; label: string; color: string }[] = [
  { id: "data", label: "Data", color: "text-purple-400" },
  { id: "ml", label: "ML", color: "text-cyan-400" },
  { id: "devops", label: "DevOps", color: "text-amber-400" },
  { id: "general", label: "General", color: "text-emerald-400" },
];
