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

/* ──────────────────────────────────────────────────────────────
   Job configuration ── Regente PicPay
   ──────────────────────────────────────────────────────────────
   Sem cor por tipo (type é texto, não identidade visual).
   Cor só comunica status. Campos legados (`gradient`, `iconBg`,
   `borderGlow`, `accentColor`) permanecem para compatibilidade
   com componentes v1, mas com valores neutros PicPay.
   ────────────────────────────────────────────────────────────── */

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
  /** Campo legado: valor neutro, sem cor por tipo. */
  gradient: string;
  /** Classe Tailwind para o ícone: fundo neutro uniforme. */
  iconBg: string;
  /** Legado: cor de acento neutra (verde escuro PicPay). */
  accentColor: string;
  /** Legado: sombra neutra. */
  borderGlow: string;
}

const NEUTRAL_ICON_BG =
  "bg-[#161616] text-[#a3a3a3] ring-1 ring-[#262626]";

function make(
  label: string,
  description: string,
  icon: LucideIcon
): JobTypeConfig {
  return {
    label,
    description,
    icon,
    gradient: "",
    iconBg: NEUTRAL_ICON_BG,
    accentColor: "#064E2B",
    borderGlow: "rgba(17, 199, 111, 0.2)",
  };
}

export const JOB_TYPES: Record<JobType, JobTypeConfig> = {
  LAMBDA:        make("Lambda",        "Função serverless",       Zap),
  BATCH:         make("Batch",         "Container / job em lote", Box),
  GLUE:          make("Glue",          "ETL pipeline",            Paintbrush),
  STEP_FUNCTION: make("Step Function", "State machine",           Workflow),
  CHOICE:        make("Choice",        "Desvio condicional",      GitBranch),
  PARALLEL:      make("Parallel",      "Execução concorrente",    Layers),
  WAIT:          make("Wait",          "Delay / timer",           Clock),
  HTTP:          make("HTTP",          "Chamada REST",            Globe),
};

/* ── Status ──────────────────────────────────────────────── */

export type JobStatus =
  | "SUCCESS"
  | "RUNNING"
  | "FAILED"
  | "WAITING"
  | "INACTIVE";

export interface StatusConfig {
  label: string;
  variant: "success" | "running" | "failed" | "waiting" | "inactive";
  /** Classe Tailwind para o dot colorido ── usa paleta PicPay. */
  dotColor: string;
  /** Campo legado ── aplica borda colorida, sem box-shadow elaborada. */
  glowClass: string;
}

export const STATUS_MAP: Record<JobStatus, StatusConfig> = {
  SUCCESS:  { label: "Success",  variant: "success",  dotColor: "bg-[#11C76F]", glowClass: "node-glow-success"  },
  RUNNING:  { label: "Running",  variant: "running",  dotColor: "bg-[#22d3ee]", glowClass: "node-glow-running"  },
  FAILED:   { label: "Failed",   variant: "failed",   dotColor: "bg-[#ef4444]", glowClass: "node-glow-failed"   },
  WAITING:  { label: "Waiting",  variant: "waiting",  dotColor: "bg-[#f59e0b]", glowClass: "node-glow-waiting"  },
  INACTIVE: { label: "Inactive", variant: "inactive", dotColor: "bg-[#525252]", glowClass: "node-glow-inactive" },
};

/* ── Node Data ──────────────────────────────────────────── */

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
  mode?: "design" | "monitoring";
}
