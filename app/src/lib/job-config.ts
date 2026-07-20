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
  Terminal,
  FileCode,
  Network,
  FileSearch,
  Database,
  ArrowRightLeft,
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
  | "COMMAND"
  | "SCRIPT"
  | "SSH"
  | "HTTP"
  | "FILE_WATCH"
  | "FILE_TRANSFER"
  | "DATABASE"
  | "LAMBDA"
  | "BATCH"
  | "GLUE"
  | "STEP_FUNCTION"
  | "CHOICE"
  | "PARALLEL"
  | "WAIT";

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
  COMMAND:       make("Command",       "Comando no agente (shell)", Terminal),
  SCRIPT:        make("Script",        "Script .sh/.bat/.ps1 no agente", FileCode),
  SSH:           make("SSH",           "Comando remoto via SSH (agentless)", Network),
  LAMBDA:        make("Lambda",        "Função serverless",       Zap),
  BATCH:         make("Batch",         "Container / job em lote", Box),
  GLUE:          make("Glue",          "ETL pipeline",            Paintbrush),
  STEP_FUNCTION: make("Step Function", "State machine",           Workflow),
  CHOICE:        make("Choice",        "Desvio condicional",      GitBranch),
  PARALLEL:      make("Parallel",      "Execução concorrente",    Layers),
  WAIT:          make("Wait",          "Delay / timer",           Clock),
  HTTP:          make("HTTP",          "Chamada REST",            Globe),
  FILE_WATCH:    make("File Watch",    "Espera arquivo chegar no agente", FileSearch),
  FILE_TRANSFER: make("File Transfer", "MFT: local ↔ SFTP ↔ S3 pelo agente", ArrowRightLeft),
  DATABASE:      make("Database",      "SQL em Postgres/MySQL/SQLite pelo agente", Database),
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
  /**
   * Ordem colocada NA MÃO pelo operador — "Order Force" do Design (Control-M
   * force order, force_mode='order'): ordem NOVA fora do agendamento. O card
   * mostra o selo 🖐 MANUAL. Um "Run Now" (força uma instance EXISTENTE,
   * force_mode='') NÃO seta isto — não leva tag nenhuma (pedido do usuário).
   */
  manualOrder?: boolean;
  /**
   * WAITING segurado por DEPENDÊNCIA (pai não rodou / rodando / falhou) —
   * Control-M "Wait Event". O card mostra "WAIT EVENT" em vez de "WAIT"
   * (que fica para espera de horário/janela).
   */
  waitEvent?: boolean;
  /**
   * WAITING porque NÃO há agente online com a capability (ou o agente pinado
   * está offline). Card em AZUL CLARO "WAIT AGENT"; quando o agente conectar,
   * o server dispara o job na hora (tick nudge no ws handler).
   */
  waitAgent?: boolean;
  /**
   * Instance em HOLD — segurada manualmente por um operador (Control-M "Hold").
   * Não roda até um Release. Como HOLD colapsa para INACTIVE na cor de status
   * (igual a CANCELLED), o card sinaliza o estado com um cadeado sobreposto no
   * canto superior esquerdo — sem cadeado é IDLE de verdade (cancelado/ocioso).
   */
  held?: boolean;
  /**
   * HOLD veio de uma PAUSA DE FOLDER (D-2/schemaV14), não de um hold individual.
   * Muda a tinta do cadeado (âmbar = folder, violeta = individual) e o texto do
   * tooltip; não pode ser liberado individualmente — só o resume da folder. Só
   * relevante quando `held` é true.
   */
  folderHeld?: boolean;
  /**
   * WAITING preso no gate Control-M "Wait for confirmation": a definition exige
   * confirm e a instância ainda NÃO foi confirmada pelo operador. O card fica
   * todo violeta com a tag CONFIRM; confirmar (botão direito → Confirmar, ou o
   * botão no painel de detalhe) libera o job — sai do violeta e executa.
   */
  waitConfirm?: boolean;
  /**
   * WAITING preso no gate F15 "WAIT RESOURCE": a ordem exige um recurso/quota
   * que não tem unidade livre no pool (semáforo Control-M). Só deriva quando o
   * job já está no horário e não está preso por condição/agente/confirm (a
   * ordem dos gates do server). O card mostra o selo âmbar "WAIT RESOURCE".
   */
  waitResource?: boolean;
}
