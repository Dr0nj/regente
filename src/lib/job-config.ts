import type { LucideIcon } from "lucide-react";
import {
  Zap,
  Box,
  Paintbrush,
  Workflow,
  GitBranch,
  Layers,
  Clock,
  Globe,
} from "lucide-react";

/* -- Job Types -- */

export type JobType =
  | "LAMBDA"
  | "BATCH"
  | "GLUE"
  | "STEP_FUNCTION"
  | "CHOICE"
  | "PARALLEL"
  | "WAIT"
  | "HTTP";

export interface JobTypeConfig {
  label: string;
  description: string;
  icon: LucideIcon;
  gradient: string;
  iconBg: string;
  accentColor: string;
  borderGlow: string;
}

export const JOB_TYPES: Record<JobType, JobTypeConfig> = {
  LAMBDA: {
    label: "Lambda",
    description: "Serverless function",
    icon: Zap,
    gradient: "from-amber-500/20 via-orange-500/10 to-transparent",
    iconBg: "bg-amber-500/10 text-amber-400 ring-1 ring-amber-500/20",
    accentColor: "#f59e0b",
    borderGlow: "rgba(245, 158, 11, 0.3)",
  },
  BATCH: {
    label: "Batch",
    description: "Container job",
    icon: Box,
    gradient: "from-blue-500/20 via-indigo-500/10 to-transparent",
    iconBg: "bg-blue-500/10 text-blue-400 ring-1 ring-blue-500/20",
    accentColor: "#3b82f6",
    borderGlow: "rgba(59, 130, 246, 0.3)",
  },
  GLUE: {
    label: "Glue",
    description: "ETL pipeline",
    icon: Paintbrush,
    gradient: "from-purple-500/20 via-fuchsia-500/10 to-transparent",
    iconBg: "bg-purple-500/10 text-purple-400 ring-1 ring-purple-500/20",
    accentColor: "#a855f7",
    borderGlow: "rgba(168, 85, 247, 0.3)",
  },
  STEP_FUNCTION: {
    label: "Step Function",
    description: "State machine",
    icon: Workflow,
    gradient: "from-cyan-500/20 via-teal-500/10 to-transparent",
    iconBg: "bg-cyan-500/10 text-cyan-400 ring-1 ring-cyan-500/20",
    accentColor: "#22d3ee",
    borderGlow: "rgba(34, 211, 238, 0.3)",
  },
  CHOICE: {
    label: "Choice",
    description: "Conditional branch",
    icon: GitBranch,
    gradient: "from-emerald-500/20 via-green-500/10 to-transparent",
    iconBg: "bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/20",
    accentColor: "#10b981",
    borderGlow: "rgba(16, 185, 129, 0.3)",
  },
  PARALLEL: {
    label: "Parallel",
    description: "Concurrent execution",
    icon: Layers,
    gradient: "from-rose-500/20 via-pink-500/10 to-transparent",
    iconBg: "bg-rose-500/10 text-rose-400 ring-1 ring-rose-500/20",
    accentColor: "#f43f5e",
    borderGlow: "rgba(244, 63, 94, 0.3)",
  },
  WAIT: {
    label: "Wait",
    description: "Delay / timer",
    icon: Clock,
    gradient: "from-slate-400/20 via-gray-500/10 to-transparent",
    iconBg: "bg-slate-500/10 text-slate-400 ring-1 ring-slate-500/20",
    accentColor: "#64748b",
    borderGlow: "rgba(100, 116, 139, 0.3)",
  },
  HTTP: {
    label: "HTTP",
    description: "REST API call",
    icon: Globe,
    gradient: "from-sky-500/20 via-blue-500/10 to-transparent",
    iconBg: "bg-sky-500/10 text-sky-400 ring-1 ring-sky-500/20",
    accentColor: "#0ea5e9",
    borderGlow: "rgba(14, 165, 233, 0.3)",
  },
};

/* -- Status -- */

export type JobStatus =
  | "SUCCESS"
  | "RUNNING"
  | "FAILED"
  | "WAITING"
  | "INACTIVE";

export interface StatusConfig {
  label: string;
  variant: "success" | "running" | "failed" | "waiting" | "inactive";
  dotColor: string;
  glowClass: string;
}

export const STATUS_MAP: Record<JobStatus, StatusConfig> = {
  SUCCESS: { label: "Success", variant: "success", dotColor: "bg-emerald-400", glowClass: "node-glow-success" },
  RUNNING: { label: "Running", variant: "running", dotColor: "bg-cyan-400", glowClass: "node-glow-running" },
  FAILED:  { label: "Failed",  variant: "failed",  dotColor: "bg-red-400",     glowClass: "node-glow-failed" },
  WAITING: { label: "Waiting", variant: "waiting", dotColor: "bg-amber-400",   glowClass: "node-glow-waiting" },
  INACTIVE:{ label: "Inactive",variant: "inactive", dotColor: "bg-slate-500",  glowClass: "node-glow-inactive" },
};

/* -- Node Data -- */

export interface JobNodeVariable {
  key: string;
  value: string;
}

export type HttpMethod = "GET" | "POST" | "PUT" | "PATCH" | "DELETE";

export interface HttpConfig {
  url: string;
  method: HttpMethod;
  headers?: Record<string, string>;
  body?: string;
}

export interface JobNodeData {
  [key: string]: unknown;
  label: string;
  jobType: JobType;
  status: JobStatus;
  lastRun?: string;
  schedule?: string;
  timeout?: number;
  retries?: number;
  team?: string;
  variables?: JobNodeVariable[];
  httpConfig?: HttpConfig;
  dryRun?: boolean;
}