import { useEffect, useState } from "react";
import type { JobInstance, JobDefinition, JobSchedule } from "@/lib/orchestrator-model";
import {
  fetchInstanceEvents,
  type InstanceEvent,
  fetchInstanceExplain,
  type Explanation,
  type ExplainBlocker,
  fetchBlastRadius,
  type BlastRadius,
  fetchNeighborhood,
  type Neighborhood,
  type NeighborNode,
  fetchRCA,
  type RCA,
} from "@/lib/runtime-bridge";
import { injectFailure, fetchPerfForecast, fetchJobStats, type PerfForecast, type JobStats } from "@/lib/differentials-api";
import { isServerMode } from "@/lib/server-client";
import ForecastPanel from "./ForecastPanel";
import { toast } from "./Toast";
import { useResizablePanel, ResizeHandle } from "./resizable";

/* ──────────────────────────────────────────────────────────────
   InstanceDetailsDrawer — painel lateral direito (Monitoring).
   ──────────────────────────────────────────────────────────────
   Reestruturado em abas nomeadas (2026-07-09):
     General · Output · Logs · Statistics · Schedule ·
     Dependencies · Neighborhood · Why not?
   A instance é IMUTÁVEL (snapshot da ordem). Schedule/Dependencies
   traduzem a DEFINITION de desenho (rótulo "design", read-only) —
   o que o usuário pediu para consultar. Runtime (output/logs/stats/
   why-not/neighborhood) vem da instance e dos endpoints do server.
   ────────────────────────────────────────────────────────────── */

export interface InstanceActionHandlers {
  onHold: (id: string) => void;
  onRelease: (id: string) => void;
  onCancel: (id: string) => void;
  onRerun: (id: string) => void;
  onSkip: (id: string) => void;
  onBypass: (id: string) => void;
  /** Control-M Confirm — libera um job confirm:true parado no gate WAIT_CONFIRM. */
  onConfirm: (id: string) => void;
  onClose: () => void;
}

const STATUS_COLOR: Record<JobInstance["status"], string> = {
  OK: "var(--v2-status-ok)",
  NOTOK: "var(--v2-status-failed)",
  RUNNING: "var(--v2-status-running)",
  WAITING: "var(--v2-status-waiting)",
  HOLD: "var(--v2-text-secondary)",
  CANCELLED: "var(--v2-text-muted)",
};

const STATUS_LABEL: Record<JobInstance["status"], string> = {
  OK: "OK",
  NOTOK: "NOT OK",
  RUNNING: "RUNNING",
  WAITING: "WAITING",
  HOLD: "HOLD",
  CANCELLED: "CANCELLED",
};

function fmtTime(ms?: number): string {
  if (!ms) return "—";
  return new Date(ms).toLocaleTimeString("en-GB", { hour12: false });
}

function fmtDuration(ms?: number): string {
  if (!ms) return "—";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  const m = Math.floor(ms / 60000);
  const s = Math.floor((ms % 60000) / 1000);
  return `${m}m${s.toString().padStart(2, "0")}s`;
}

function fmtVal(v: unknown): string {
  if (v == null) return "";
  if (typeof v === "string") return v;
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  try { return JSON.stringify(v); } catch { return String(v); }
}

interface ActionButton {
  label: string;
  onClick: () => void;
  tone: "neutral" | "danger" | "primary";
  show: boolean;
}

/* ── Abas ── */
type Tab = "general" | "output" | "logs" | "stats" | "schedule" | "deps" | "neighborhood" | "whynot";
const TAB_LABEL: Record<Tab, string> = {
  general: "General",
  output: "Output",
  logs: "Logs",
  stats: "Statistics",
  schedule: "Schedule",
  deps: "Dependencies",
  neighborhood: "Neighborhood",
  whynot: "Why not?",
};
const TAB_ORDER: Tab[] = ["general", "output", "logs", "stats", "schedule", "deps", "neighborhood", "whynot"];

export default function InstanceDetailsDrawer({
  instance,
  definition,
  allDefs = [],
  handlers,
}: {
  instance: JobInstance;
  /** Definition de desenho correspondente (para Schedule/Dependencies/Description). */
  definition?: JobDefinition;
  /** Todas as defs do escopo — para calcular downstream (triggers) sem fetch. */
  allDefs?: JobDefinition[];
  handlers: InstanceActionHandlers;
}) {
  const status = instance.status;
  const color = STATUS_COLOR[status];
  const [tab, setTab] = useState<Tab>("general");

  // Downstream estático: jobs cujo upstream aponta para este (mesma lógica do Design).
  const triggers = allDefs.filter((d) => (d.upstream ?? []).some((u) => u.from === instance.definitionId));
  const depsCount = (definition?.upstream?.length ?? 0) + triggers.length;

  // D-11 chaos — falha sintética pelo fluxo REAL (retry/actions/alerts reagem
  // como numa falha orgânica). Server-mode only; confirmação explícita.
  const chaosInject = async () => {
    if (!window.confirm(`Inject FAILURE into "${instance.label}"?\n\nThe job becomes NOTOK through the real path — retry, On-Do and alerts fire as in a production failure.`)) return;
    try {
      await injectFailure(instance.id);
      toast.info("Failure injected (chaos)", { detail: instance.id });
    } catch (e) {
      toast.error("Chaos failed", { detail: e instanceof Error ? e.message : String(e) });
    }
  };

  const actions: ActionButton[] = ([
    { label: "Hold",    onClick: () => handlers.onHold(instance.id),    tone: "neutral" as const, show: status === "WAITING" },
    { label: "Release", onClick: () => handlers.onRelease(instance.id), tone: "primary" as const, show: status === "HOLD" },
    { label: "Cancel",  onClick: () => handlers.onCancel(instance.id),  tone: "danger"  as const, show: status === "WAITING" || status === "HOLD" },
    { label: "Skip",    onClick: () => handlers.onSkip(instance.id),    tone: "neutral" as const, show: status === "WAITING" || status === "HOLD" },
    { label: "Set OK", onClick: () => handlers.onBypass(instance.id), tone: "primary" as const, show: status === "NOTOK" || status === "CANCELLED" },
    { label: "Rerun",   onClick: () => handlers.onRerun(instance.id),   tone: "primary" as const, show: status === "NOTOK" },
    { label: "💥 Chaos", onClick: chaosInject, tone: "danger" as const,
      show: isServerMode() && (status === "WAITING" || status === "RUNNING" || status === "HOLD") },
  ]).filter((a) => a.show);

  const { width, onMouseDown, reset } = useResizablePanel({
    storageKey: "regente.panel.instance.w", defaultWidth: 360, min: 300, max: 720, edge: "left",
  });

  return (
    <aside
      className="v2-grain v2-edge-highlight"
      data-canvas-inset="right"
      style={{
        position: "absolute",
        top: 10,
        right: 10,
        bottom: 10,
        width,
        background: "var(--v2-bg-surface)",
        border: "1px solid var(--v2-border-medium)",
        borderRadius: 16,
        boxShadow: "inset 0 1px 0 rgba(255,255,255,0.04), 0 10px 30px rgba(0,0,0,0.35)",
        display: "flex",
        flexDirection: "column",
        zIndex: 10,
        fontFamily: "var(--v2-font-sans)",
        color: "var(--v2-text-primary)",
        overflow: "hidden",
      }}
    >
      <ResizeHandle edge="left" onMouseDown={onMouseDown} onReset={reset} />
      {/* Header */}
      <header
        style={{
          padding: "10px 12px",
          borderBottom: "1px solid var(--v2-border-subtle)",
          display: "flex",
          alignItems: "center",
          gap: 8,
        }}
      >
        <span
          style={{
            width: 8,
            height: 8,
            borderRadius: "50%",
            background: color,
            flexShrink: 0,
          }}
        />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: 12,
              fontWeight: 600,
              whiteSpace: "nowrap",
              overflow: "hidden",
              textOverflow: "ellipsis",
            }}
          >
            {instance.label}
          </div>
          <div
            style={{
              fontSize: 10,
              fontFamily: "var(--v2-font-mono)",
              color: "var(--v2-text-muted)",
              letterSpacing: "0.04em",
              marginTop: 2,
            }}
          >
            {instance.jobType} · <span style={{ color: "var(--v2-accent-brand)" }}>{instance.team ?? "—"}</span> · <span style={{ color }}>{STATUS_LABEL[status]}</span>
          </div>
        </div>
        <button
          onClick={handlers.onClose}
          style={{
            background: "transparent",
            border: "none",
            color: "var(--v2-text-muted)",
            cursor: "pointer",
            fontSize: 14,
            padding: 4,
          }}
          aria-label="close"
        >
          ×
        </button>
      </header>

      {/* Tabs — mesmo VISUAL do Design (JobConfigDrawer): sublinhado fino (2px)
          na aba ativa, sem pill/realce laranja. Só a aparência foi padronizada;
          as abas e o que cada uma abre continuam idênticas. */}
      <div style={{ display: "flex", borderBottom: "1px solid var(--v2-border-subtle)", overflowX: "auto" }}>
        {TAB_ORDER.map((t) => (
          <button
            key={t}
            onClick={() => setTab(t)}
            style={{
              padding: "7px 11px", fontSize: 10.5, cursor: "pointer", whiteSpace: "nowrap",
              background: "transparent", border: "none",
              borderBottom: `2px solid ${tab === t ? "var(--v2-accent-brand)" : "transparent"}`,
              color: tab === t ? "var(--v2-text-primary)" : "var(--v2-text-muted)",
              fontWeight: tab === t ? 600 : 500, fontFamily: "var(--v2-font-mono)",
            }}
          >
            {TAB_LABEL[t]}
            {t === "deps" && depsCount > 0 ? ` (${depsCount})` : ""}
          </button>
        ))}
      </div>

      {/* Body */}
      {tab === "logs" ? (
        <LogPanel instanceId={instance.id} status={status} />
      ) : (
        <div style={{ flex: 1, overflowY: "auto", padding: "12px", fontSize: 11 }}>
          {tab === "general" && <GeneralTab instance={instance} definition={definition} />}
          {tab === "output" && <OutputTab instance={instance} />}
          {tab === "stats" && <StatsTab instance={instance} />}
          {tab === "schedule" && <ScheduleTab definition={definition} />}
          {tab === "deps" && <DepsTab definition={definition} triggers={triggers} allDefs={allDefs} />}
          {tab === "neighborhood" && (
            <>
              <NeighborhoodPanel instanceId={instance.id} autoLoad />
              <BlastPanel instanceId={instance.id} />
              <ForecastPanel defId={instance.definitionId} isRunning={status === "RUNNING"} />
            </>
          )}
          {tab === "whynot" && <WhyNotTab instanceId={instance.id} status={status} onConfirm={handlers.onConfirm} />}
        </div>
      )}

      {/* Actions */}
      {actions.length > 0 && (
        <footer
          style={{
            padding: "8px 12px",
            borderTop: "1px solid var(--v2-border-subtle)",
            display: "flex",
            flexWrap: "wrap",
            gap: 6,
          }}
        >
          {actions.map((a) => (
            <button
              key={a.label}
              onClick={a.onClick}
              style={{
                padding: "5px 10px",
                fontSize: 10,
                fontFamily: "var(--v2-font-mono)",
                letterSpacing: "0.06em",
                textTransform: "uppercase",
                cursor: "pointer",
                border: `1px solid ${
                  a.tone === "primary"
                    ? "var(--v2-accent-brand)"
                    : a.tone === "danger"
                    ? "var(--v2-status-failed)"
                    : "var(--v2-border-medium)"
                }`,
                background:
                  a.tone === "primary"
                    ? "var(--v2-accent-deep)"
                    : a.tone === "danger"
                    ? "rgba(239,68,68,0.08)"
                    : "var(--v2-bg-elevated)",
                color:
                  a.tone === "primary"
                    ? "var(--v2-accent-brand)"
                    : a.tone === "danger"
                    ? "var(--v2-status-failed)"
                    : "var(--v2-text-secondary)",
                borderRadius: 3,
                fontWeight: 600,
              }}
            >
              {a.label}
            </button>
          ))}
        </footer>
      )}
    </aside>
  );
}

/* ════════════════════════════════════════════════════════════════
   GENERAL — detalhes do job, timeline e parâmetros/variáveis.
   ════════════════════════════════════════════════════════════════ */

function GeneralTab({ instance, definition }: { instance: JobInstance; definition?: JobDefinition }) {
  const description = definition?.schedule?.description?.trim();

  // Parâmetros = variáveis snapshotadas na instance (array) ∪ variáveis locais
  // vivas da definition (mapa F18). O snapshot vence (foto da ordem).
  const params: Array<[string, string]> = [];
  const seen = new Set<string>();
  for (const v of instance.variables ?? []) { params.push([v.key, v.value]); seen.add(v.key); }
  for (const [k, val] of Object.entries(definition?.localVars ?? {})) {
    if (!seen.has(k)) params.push([k, val]);
  }

  // jobType robusto: a instance é a fonte (snapshot), mas cai pra def se vier
  // vazia — mantém o drawer sincronizado com o card do canvas (que enriquece
  // da def). O V2Preview também já enriquece a instance passada aqui.
  const jobType = instance.jobType || definition?.jobType || "—";

  return (
    <>
      <Section title="Job details">
        <Field label="Job Name" value={instance.label} />
        <Field
          label="Application"
          value={definition?.application ?? "—"}
          hint={definition?.application ? undefined : "not configured yet"}
        />
        <Field label="Job Type" value={jobType} />
        <Field label="Folder" value={instance.team ?? definition?.team ?? "—"} />
        {definition?.environment && <Field label="Environment" value={definition.environment} />}
      </Section>

      <Section title="Description">
        {description ? (
          <div style={{ fontSize: 11, lineHeight: 1.5, color: "var(--v2-text-secondary)", whiteSpace: "pre-wrap", wordBreak: "break-word" }}>
            {description}
          </div>
        ) : (
          <Muted>No description. Add one in Design to explain what this job does.</Muted>
        )}
      </Section>

      <Section title="Action">
        <ActionView jobType={jobType} config={instance.actionConfig ?? {}} />
      </Section>

      <Section title="Timeline">
        <Field label="Ordered"    value={fmtTime(instance.createdAt)} />
        <Field label="Scheduled"  value={fmtTime(instance.scheduledAt)} />
        <Field label="Started"    value={fmtTime(instance.startedAt)} />
        <Field label="Completed"  value={fmtTime(instance.completedAt)} />
        <Field label="Duration"   value={fmtDuration(instance.durationMs)} />
        <Field label="Attempts"   value={`${instance.attempts} / ${instance.retries + 1}`} />
        {instance.carriedFrom && <Field label="Carried from" value={instance.carriedFrom} mono />}
      </Section>

      <Section title="Parameters">
        {params.length === 0 ? (
          <Muted>No parameters set. Variables you define (Design → Variables) are injected here at execution.</Muted>
        ) : (
          <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
            {params.map(([k, v]) => <KV key={k} k={k} v={v} />)}
          </div>
        )}
      </Section>

      <Section title="Execution config">
        <Field label="Order date" value={instance.orderDate} mono />
        <Field label="Manual"     value={instance.manual ? "yes" : "no"} />
        <Field label="Dry run"    value={instance.dryRun ? "yes" : "no"} />
        <Field label="Timeout"    value={`${instance.timeout}s`} />
      </Section>
    </>
  );
}

/* ── Action — o que o job FAZ, sincronizado por jobType (read-only) ──
   Espelha o JobActionConfigEditor do Design: cada tipo tem os campos
   relevantes do actionConfig. Renderiza só os presentes; chaves extras
   (fora do schema conhecido) caem em "Other" pra nada ficar escondido. */

interface ActionField { label: string; key: string; code?: boolean; json?: boolean }

const ACTION_VERB: Record<string, string> = {
  COMMAND: "Runs a shell command on the agent",
  SCRIPT: "Runs a script on the agent",
  SSH: "Runs a command over SSH",
  FILE_WATCH: "Waits for a file to appear",
  DATABASE: "Runs SQL against a database",
  HTTP: "Calls an HTTP endpoint",
  LAMBDA: "Invokes an AWS Lambda",
  BATCH: "Submits an AWS Batch job",
  GLUE: "Runs an AWS Glue job",
  STEP_FUNCTION: "Starts a Step Function execution",
  WAIT: "Waits for a duration",
  CHOICE: "Branches on a condition",
  PARALLEL: "Runs branches in parallel",
};

const ACTION_FIELDS: Record<string, ActionField[]> = {
  COMMAND: [{ label: "Command", key: "command", code: true }, { label: "Working dir", key: "cwd" }],
  SCRIPT: [{ label: "Script path", key: "scriptPath" }, { label: "Args", key: "args" }, { label: "Working dir", key: "cwd" }],
  SSH: [{ label: "Host", key: "host" }, { label: "User", key: "user" }, { label: "Port", key: "port" }, { label: "Command", key: "command", code: true }, { label: "Key path", key: "keyPath" }],
  FILE_WATCH: [{ label: "Path", key: "path" }, { label: "Poll interval (s)", key: "intervalSec" }, { label: "Stable (s)", key: "stableSec" }],
  DATABASE: [{ label: "Driver", key: "driver" }, { label: "DSN", key: "dsn" }, { label: "SQL", key: "sql", code: true }, { label: "Max rows", key: "maxRows" }],
  HTTP: [{ label: "Method", key: "method" }, { label: "URL", key: "url" }, { label: "Headers", key: "headers", json: true }, { label: "Body", key: "body", code: true }, { label: "Expected status", key: "expectStatus" }],
  LAMBDA: [{ label: "Function", key: "functionName" }, { label: "Region", key: "region" }, { label: "Payload", key: "payload", json: true }, { label: "Invocation", key: "invocationType" }],
  BATCH: [{ label: "Job queue", key: "jobQueue" }, { label: "Job definition", key: "jobDefinition" }, { label: "Command", key: "command", code: true }, { label: "Env", key: "env", json: true }],
  GLUE: [{ label: "Job name", key: "jobName" }, { label: "Arguments", key: "arguments", json: true }, { label: "Worker type", key: "workerType" }, { label: "# workers", key: "numberOfWorkers" }],
  STEP_FUNCTION: [{ label: "State machine ARN", key: "stateMachineArn" }, { label: "Input", key: "input", json: true }],
  WAIT: [{ label: "Seconds", key: "seconds" }, { label: "Until", key: "until" }],
  CHOICE: [{ label: "Expression", key: "expression", code: true }, { label: "Branches", key: "branches", json: true }],
  PARALLEL: [{ label: "Branches", key: "branches", json: true }, { label: "Max concurrency", key: "maxConcurrency" }],
};

function hasVal(v: unknown): boolean {
  if (v == null) return false;
  if (typeof v === "string") return v.trim().length > 0;
  if (typeof v === "number") return true;
  if (typeof v === "boolean") return true;
  if (Array.isArray(v)) return v.length > 0;
  if (typeof v === "object") return Object.keys(v as object).length > 0;
  return true;
}

function ActionView({ jobType, config }: { jobType: string; config: Record<string, unknown> }) {
  const fields = ACTION_FIELDS[jobType] ?? [];
  const verb = ACTION_VERB[jobType];
  const agent = typeof config._agentId === "string" && config._agentId ? (config._agentId as string) : null;

  const known = new Set(fields.map((f) => f.key));
  const present = fields.filter((f) => hasVal(config[f.key]));
  // Chaves fora do schema conhecido (nada escondido) — ignora as internas _*.
  const other = Object.entries(config).filter(([k, v]) => !k.startsWith("_") && !known.has(k) && hasVal(v));

  const empty = present.length === 0 && other.length === 0;

  return (
    <>
      {/* linha "o que faz" + tipo */}
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: present.length || other.length ? 8 : 0 }}>
        <span style={{ fontSize: 9, fontFamily: "var(--v2-font-mono)", fontWeight: 700, letterSpacing: "0.05em", color: "var(--v2-accent-brand)", border: "1px solid var(--v2-accent-dark)", background: "var(--v2-accent-deep)", borderRadius: 3, padding: "1px 6px" }}>
          {jobType}
        </span>
        {verb && <span style={{ fontSize: 10.5, color: "var(--v2-text-secondary)" }}>{verb}</span>}
      </div>

      {empty ? (
        <Muted>No action configured for this {jobType} job. Set it in Design → Action.</Muted>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
          {present.map((f) => {
            const v = config[f.key];
            if (f.code) return <CodeField key={f.key} label={f.label} value={String(v)} />;
            if (f.json) return <CodeField key={f.key} label={f.label} value={jsonPretty(v)} />;
            return <Field key={f.key} label={f.label} value={fmtVal(v)} mono />;
          })}
          {other.map(([k, v]) => (
            typeof v === "object"
              ? <CodeField key={k} label={k} value={jsonPretty(v)} />
              : <Field key={k} label={k} value={fmtVal(v)} mono />
          ))}
        </div>
      )}

      {agent && (
        <div style={{ marginTop: present.length || other.length ? 8 : 6 }}>
          <Field label="Agent" value={agent} mono />
        </div>
      )}
    </>
  );
}

function CodeField({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div style={{ fontSize: 8.5, color: "var(--v2-text-muted)", textTransform: "uppercase", letterSpacing: "0.06em", marginBottom: 3 }}>{label}</div>
      <Pre tone="var(--v2-text-secondary)" maxHeight={200}>{value}</Pre>
    </div>
  );
}

function jsonPretty(v: unknown): string {
  try { return JSON.stringify(v, null, 2); } catch { return String(v); }
}

/* ════════════════════════════════════════════════════════════════
   OUTPUT — erro + saída da execução.
   ════════════════════════════════════════════════════════════════ */

function OutputTab({ instance }: { instance: JobInstance }) {
  const hasOutput = instance.output && Object.keys(instance.output).length > 0;
  if (!instance.error && !hasOutput) {
    return <Muted>No output produced by this run yet.</Muted>;
  }
  return (
    <>
      {instance.error && (
        <Section title="Error">
          <Pre tone="var(--v2-status-failed)">{instance.error}</Pre>
        </Section>
      )}
      {hasOutput && (
        <Section title="Output">
          <Pre tone="var(--v2-text-secondary)" maxHeight={480}>
            {JSON.stringify(instance.output, null, 2)}
          </Pre>
        </Section>
      )}
    </>
  );
}

/* ════════════════════════════════════════════════════════════════
   STATISTICS — métricas desta execução + agregados de duração.
   ════════════════════════════════════════════════════════════════ */

function StatsTab({ instance }: { instance: JobInstance }) {
  const [pf, setPf] = useState<PerfForecast | null>(null);
  const [stats, setStats] = useState<JobStats | null>(null);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    let alive = true;
    setLoaded(false);
    Promise.all([
      fetchPerfForecast(instance.definitionId),
      fetchJobStats(instance.definitionId), // ADV-3 — retrato histórico
    ]).then(([forecast, st]) => {
      if (!alive) return;
      setPf(forecast);
      setStats(st);
      setLoaded(true);
    });
    return () => { alive = false; };
  }, [instance.definitionId]);

  const hasHistory = pf && pf.samples.length >= 2;

  return (
    <>
      <Section title="This run">
        <Field label="Status"    value={STATUS_LABEL[instance.status]} />
        <Field label="Duration"  value={fmtDuration(instance.durationMs)} />
        <Field label="Attempts"  value={`${instance.attempts} / ${instance.retries + 1}`} />
        <Field label="Timeout"   value={`${instance.timeout}s`} />
        {instance.cycleRuns != null && instance.cycleRuns > 0 && (
          <Field label="Cycle runs" value={String(instance.cycleRuns)} />
        )}
        <Field label="Order date" value={instance.orderDate} mono />
      </Section>

      {/* ADV-3 — Statistics (Control-M): totais por resultado + distribuição de duração */}
      {loaded && stats && stats.runs > 0 && (
        <Section title={`Run statistics (last ${stats.runs})`}>
          <div style={{ display: "flex", flexWrap: "wrap", gap: 16 }}>
            <StatTile label="success" value={`${Math.round(stats.successRate * 100)}%`}
              tone={stats.successRate < 0.9 ? "var(--v2-status-failed)" : "var(--v2-status-success)"} />
            <StatTile label="ok / notok" value={`${stats.ok} / ${stats.notok}`} />
            <StatTile label="min" value={fmtDuration(stats.minMs)} />
            <StatTile label="avg" value={fmtDuration(stats.avgMs)} />
            <StatTile label="max" value={fmtDuration(stats.maxMs)} />
          </div>
          {stats.lastStatus && (
            <div style={{ marginTop: 8, fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)" }}>
              last run: <span style={{ color: stats.lastStatus === "OK" ? "var(--v2-status-success)" : "var(--v2-status-failed)" }}>{stats.lastStatus}</span>
              {stats.lastDurationMs != null && stats.lastDurationMs > 0 && <> · {fmtDuration(stats.lastDurationMs)}</>}
              {stats.lastFinishedAt && <> · {new Date(stats.lastFinishedAt).toLocaleString()}</>}
            </div>
          )}
        </Section>
      )}

      <Section title="Duration history">
        {!loaded && <Muted>loading…</Muted>}
        {loaded && !hasHistory && <Muted>Not enough history yet — needs at least 2 successful runs.</Muted>}
        {loaded && hasHistory && pf && (
          <>
            <div style={{ display: "flex", flexWrap: "wrap", gap: 16 }}>
              <StatTile label="p50" value={fmtDuration(pf.p50Ms)} />
              <StatTile label="p90" value={fmtDuration(pf.p90Ms)} />
              <StatTile label="next ~" value={fmtDuration(pf.nextMs)} tone={pf.slower ? "var(--v2-status-failed)" : undefined} />
              <StatTile label="samples" value={String(pf.samples.length)} />
            </div>
            {pf.slower && (
              <div style={{ marginTop: 8, fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-status-failed)", border: "1px solid var(--v2-status-failed)", borderRadius: 3, padding: "3px 7px", display: "inline-block" }}>
                ▲ trending slower
              </div>
            )}
          </>
        )}
      </Section>
    </>
  );
}

function StatTile({ label, value, tone }: { label: string; value: string; tone?: string }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 1 }}>
      <span style={{ fontSize: 9, color: "var(--v2-text-muted)", letterSpacing: "0.06em", textTransform: "uppercase" }}>{label}</span>
      <span style={{ fontSize: 14, fontFamily: "var(--v2-font-mono)", color: tone ?? "var(--v2-text-primary)" }}>{value}</span>
    </div>
  );
}

/* ════════════════════════════════════════════════════════════════
   SCHEDULE — tradução read-only do schedule de DESENHO.
   ════════════════════════════════════════════════════════════════ */

const DOW_LABEL: Record<string, string> = { mon: "Mon", tue: "Tue", wed: "Wed", thu: "Thu", fri: "Fri", sat: "Sat", sun: "Sun" };
const MONTH_LABEL = ["", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const ADV_LABEL: Record<string, string> = {
  "first-businessday": "First business day",
  "last-businessday": "Last business day",
  "first-businessday-not-monday": "First business day (not Monday)",
  "penultimate-businessday": "Penultimate business day",
};
function ordinal(n: number): string {
  if (n === -1) return "last";
  const s = ["th", "st", "nd", "rd"], v = n % 100;
  return n + (s[(v - 20) % 10] || s[v] || s[0]);
}
function describeFrequency(s: JobSchedule): string {
  const f = s.frequency ?? "daily";
  switch (f) {
    case "daily": return "Every day";
    case "weekly": return s.daysOfWeek?.length ? `Weekly · ${s.daysOfWeek.map((d) => DOW_LABEL[d] ?? d).join(", ")}` : "Weekly";
    case "monthly": return s.daysOfMonth?.length ? `Monthly · day ${s.daysOfMonth.map((d) => (d === -1 ? "last" : d)).join(", ")}` : "Monthly";
    case "businessday": return s.nthBusinessDays?.length ? `Business day · ${s.nthBusinessDays.map(ordinal).join(", ")}` : "Business day";
    case "advanced": return s.advancedRule ? (ADV_LABEL[s.advancedRule] ?? s.advancedRule) : "Advanced";
    default: return String(f);
  }
}
const SHIFT_LABEL: Record<string, string> = {
  "next-businessday": "Roll to next business day",
  "prev-businessday": "Roll to previous business day",
};

function ScheduleTab({ definition }: { definition?: JobDefinition }) {
  if (!definition) {
    return <Muted>Definition not available — this instance's design schedule can't be shown (the job may have been deleted or renamed).</Muted>;
  }
  const s = definition.schedule;
  const window = `${s.windowFrom || "—"} → ${s.windowTo || "—"}`;
  const calendars = [
    ...(definition.calendar ? [{ name: definition.calendar, mode: "include" as const }] : []),
    ...(definition.calendars ?? []),
  ];

  return (
    <>
      <div style={{ marginBottom: 10, fontSize: 10, color: "var(--v2-text-muted)", lineHeight: 1.45 }}>
        Read-only translation of the <b style={{ color: "var(--v2-text-secondary)" }}>design</b> schedule — edit it in Design.
      </div>
      <Section title="When">
        <Field label="Status" value={s.enabled ? "Enabled" : "Disabled"} tone={s.enabled ? "var(--v2-status-ok)" : "var(--v2-text-muted)"} />
        <Field label="Frequency" value={describeFrequency(s)} />
        {s.monthsOfYear?.length ? (
          <Field label="Months" value={s.monthsOfYear.map((m) => MONTH_LABEL[m] ?? m).join(", ")} />
        ) : null}
        {s.shift && s.shift !== "none" && SHIFT_LABEL[s.shift] && (
          <Field label="Roll" value={SHIFT_LABEL[s.shift]} />
        )}
      </Section>

      <Section title="Runtime window">
        <Field label="Run at" value={s.runAt || "—"} mono />
        <Field label="Window (from → to)" value={window} mono />
        <Field label="Cyclic" value={s.cyclic ? `every ${s.intervalMin ?? "?"} min${s.cyclicMaxRuns ? `, max ${s.cyclicMaxRuns} runs` : ""}` : "no"} tone={s.cyclic ? "var(--v2-accent-brand)" : undefined} />
        <Field label="Keep active" value={s.keepActive ? `${s.keepActive} extra daily carry-over(s)` : "default"} />
      </Section>

      {calendars.length > 0 && (
        <Section title="Calendars">
          <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
            {calendars.map((c, i) => (
              <div key={`${c.name}-${i}`} style={depRow}>
                <span style={{ flex: 1, fontSize: 11, fontFamily: "var(--v2-font-mono)" }}>{c.name}</span>
                <span style={{ fontSize: 9, color: c.mode === "exclude" ? "var(--v2-status-failed)" : "var(--v2-accent-brand)", fontFamily: "var(--v2-font-mono)", textTransform: "uppercase" }}>{c.mode}</span>
              </div>
            ))}
          </div>
        </Section>
      )}
    </>
  );
}

/* ════════════════════════════════════════════════════════════════
   DEPENDENCIES — relacionamentos, recursos e conditions (design).
   ════════════════════════════════════════════════════════════════ */

function DepsTab({ definition, triggers, allDefs }: { definition?: JobDefinition; triggers: JobDefinition[]; allDefs: JobDefinition[] }) {
  const labelOf = (id: string) => allDefs.find((d) => d.id === id)?.label ?? id;
  const upstream = definition?.upstream ?? [];
  const resources = Object.entries(definition?.resources ?? {});
  const condIn = definition?.conditionsIn ?? [];
  const condOutAdd = definition?.conditionsOutAdd ?? [];
  const condOutRemove = definition?.conditionsOutRemove ?? [];
  const sla = definition?.sla;
  const sub = definition?.subWorkflow;
  const nothing =
    upstream.length === 0 && triggers.length === 0 && resources.length === 0 &&
    condIn.length === 0 && condOutAdd.length === 0 && condOutRemove.length === 0 && !sla && !sub;

  return (
    <>
      <Section title="Depends on">
        {upstream.length === 0 ? (
          <Muted>Nothing upstream — this job doesn't wait on other jobs.</Muted>
        ) : (
          upstream.map((u) => (
            <div key={u.from} style={depRow}>
              <span style={{ flex: 1, fontSize: 11, fontFamily: "var(--v2-font-mono)" }}>{labelOf(u.from)}</span>
              <span style={{ fontSize: 9, color: "var(--v2-accent-brand)", fontFamily: "var(--v2-font-mono)" }}>{u.condition}</span>
            </div>
          ))
        )}
      </Section>

      <Section title="Triggers">
        {triggers.length === 0 ? (
          <Muted>No job depends on this one.</Muted>
        ) : (
          triggers.map((d) => {
            const cond = (d.upstream ?? []).find((u) => u.from === definition?.id)?.condition;
            return (
              <div key={d.id} style={depRow}>
                <span style={{ flex: 1, fontSize: 11, fontFamily: "var(--v2-font-mono)" }}>{d.label}</span>
                <span style={{ fontSize: 9, color: "var(--v2-accent-brand)", fontFamily: "var(--v2-font-mono)" }}>{cond}</span>
              </div>
            );
          })
        )}
      </Section>

      {resources.length > 0 && (
        <Section title="Resource requirements">
          {resources.map(([name, qty]) => (
            <div key={name} style={depRow}>
              <span style={{ flex: 1, fontSize: 11, fontFamily: "var(--v2-font-mono)" }}>{name}</span>
              <span style={{ fontSize: 10, color: "var(--v2-accent-brand)", fontFamily: "var(--v2-font-mono)" }}>×{qty}</span>
            </div>
          ))}
        </Section>
      )}

      {(condIn.length > 0 || condOutAdd.length > 0 || condOutRemove.length > 0) && (
        <Section title="Conditions">
          {condIn.length > 0 && <CondList label="requires (IN)" items={condIn} tone="var(--v2-status-waiting)" />}
          {condOutAdd.length > 0 && <CondList label="adds on OK (OUT +)" items={condOutAdd} tone="var(--v2-status-ok)" />}
          {condOutRemove.length > 0 && <CondList label="removes on OK (OUT −)" items={condOutRemove} tone="var(--v2-status-failed)" />}
        </Section>
      )}

      {sub && (
        <Section title="Sub-workflow">
          <Field label="Waits for folder" value={sub.folder} mono />
        </Section>
      )}

      {sla && (
        <Section title="SLA">
          {sla.expectedDurationMin != null && <Field label="Expected duration" value={`${sla.expectedDurationMin} min`} />}
          {sla.deadlineHM && <Field label="Deadline" value={sla.deadlineHM} mono />}
          {sla.severity && <Field label="Severity" value={sla.severity} />}
        </Section>
      )}

      {nothing && <Muted>No dependencies, resources or conditions configured for this job.</Muted>}
    </>
  );
}

function CondList({ label, items, tone }: { label: string; items: string[]; tone: string }) {
  return (
    <div style={{ marginBottom: 8 }}>
      <div style={{ fontSize: 8.5, color: "var(--v2-text-muted)", textTransform: "uppercase", letterSpacing: "0.06em", marginBottom: 3 }}>{label}</div>
      <div style={{ display: "flex", flexWrap: "wrap", gap: 4 }}>
        {items.map((c) => (
          <span key={c} style={{ fontSize: 9.5, fontFamily: "var(--v2-font-mono)", color: tone, border: `1px solid ${tone}`, borderRadius: 2, padding: "1px 6px" }}>{c}</span>
        ))}
      </div>
    </div>
  );
}

/* ════════════════════════════════════════════════════════════════
   WHY NOT? — Explain (bloqueios/confirm) + causa raiz.
   ════════════════════════════════════════════════════════════════ */

function WhyNotTab({ instanceId, status, onConfirm }: { instanceId: string; status: JobInstance["status"]; onConfirm: (id: string) => void }) {
  return (
    <>
      <ExplainPanel instanceId={instanceId} status={status} onConfirm={onConfirm} />
      <RCAPanel instanceId={instanceId} />
      {!isServerMode() && (
        <Muted>Live explanations (blockers, root cause) are available when connected to a Regente server.</Muted>
      )}
    </>
  );
}

/* ════════════════════════════════════════════════════════════════
   Primitivos de layout
   ════════════════════════════════════════════════════════════════ */

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div style={{ marginBottom: 16 }}>
      <div
        style={{
          fontSize: 9,
          fontFamily: "var(--v2-font-mono)",
          color: "var(--v2-text-muted)",
          letterSpacing: "0.1em",
          textTransform: "uppercase",
          marginBottom: 6,
        }}
      >
        {title}
      </div>
      <div>{children}</div>
    </div>
  );
}

function Field({ label, value, mono, tone, hint }: { label: string; value: string; mono?: boolean; tone?: string; hint?: string }) {
  return (
    <div
      style={{
        display: "flex",
        justifyContent: "space-between",
        alignItems: "baseline",
        gap: 8,
        padding: "3px 0",
        borderBottom: "1px dashed var(--v2-border-subtle)",
        fontSize: 11,
      }}
    >
      <span style={{ color: "var(--v2-text-muted)", flexShrink: 0 }}>{label}</span>
      <span
        style={{
          color: tone ?? "var(--v2-text-primary)",
          fontFamily: mono ? "var(--v2-font-mono)" : undefined,
          fontSize: mono ? 10 : 11,
          maxWidth: "62%",
          whiteSpace: "nowrap",
          overflow: "hidden",
          textOverflow: "ellipsis",
          textAlign: "right",
        }}
        title={hint ? `${value} — ${hint}` : value}
      >
        {value}
        {hint && <span style={{ color: "var(--v2-text-muted)", fontStyle: "italic", marginLeft: 6, fontSize: 9 }}>{hint}</span>}
      </span>
    </div>
  );
}

function KV({ k, v }: { k: string; v: string }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 2, padding: "4px 8px", background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)", borderRadius: 3 }}>
      <span style={{ fontSize: 9.5, fontFamily: "var(--v2-font-mono)", color: "var(--v2-accent-brand)", wordBreak: "break-all" }}>{k}</span>
      <span style={{ fontSize: 10.5, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-secondary)", whiteSpace: "pre-wrap", wordBreak: "break-word" }}>{v || "—"}</span>
    </div>
  );
}

function Muted({ children }: { children: React.ReactNode }) {
  return <div style={{ fontSize: 10.5, color: "var(--v2-text-muted)", lineHeight: 1.5 }}>{children}</div>;
}

function Pre({ children, tone, maxHeight }: { children: React.ReactNode; tone: string; maxHeight?: number }) {
  return (
    <pre
      style={{
        margin: 0,
        padding: "6px 8px",
        background: "var(--v2-bg-canvas)",
        border: "1px solid var(--v2-border-subtle)",
        borderRadius: 2,
        fontFamily: "var(--v2-font-mono)",
        fontSize: 10,
        color: tone,
        whiteSpace: "pre-wrap",
        wordBreak: "break-word",
        maxHeight,
        overflowY: maxHeight ? "auto" : undefined,
      }}
    >
      {children}
    </pre>
  );
}

const depRow: React.CSSProperties = { display: "flex", alignItems: "center", gap: 8, padding: "6px 8px", background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)", borderRadius: 3 };

const KIND_COLOR: Record<string, string> = {
  ordered:        "var(--v2-text-muted)",
  "force-ordered":"var(--v2-accent-brand)",
  started:        "var(--v2-status-running)",
  submitted:      "var(--v2-text-secondary)",
  finished:       "var(--v2-status-ok)",
  timeout:        "var(--v2-status-failed)",
  cancelled:      "var(--v2-text-muted)",
  held:           "var(--v2-text-secondary)",
  released:       "var(--v2-accent-brand)",
  rerun:          "var(--v2-accent-brand)",
  "set-ok":       "var(--v2-accent-brand)",
  output:         "var(--v2-text-secondary)",
};

function fmtTS(ts: string): string {
  const d = new Date(ts);
  if (isNaN(d.getTime())) return ts;
  return d.toLocaleTimeString("en-GB", { hour12: false }) +
    "." + String(d.getMilliseconds()).padStart(3, "0");
}

/* ── Explain ("why didn't this job run?") ── */

const BLOCKER_COLOR: Record<ExplainBlocker["kind"], string> = {
  WAIT_WINDOW:   "var(--v2-status-waiting)",
  WINDOW_CLOSED: "var(--v2-status-failed)",
  WAIT_CONFIRM:  "#a78bfa",
  WAIT_DEP:      "var(--v2-status-waiting)",
  BLOCKED_DEP:   "var(--v2-status-failed)",
  WAIT_CONDITION:"var(--v2-status-waiting)",
  WAIT_AGENT:    "#38bdf8",
  WAIT_RESOURCE: "var(--v2-accent-brand)",
};
const BLOCKER_LABEL: Record<ExplainBlocker["kind"], string> = {
  WAIT_WINDOW:   "WINDOW",
  WINDOW_CLOSED: "WINDOW CLOSED",
  WAIT_CONFIRM:  "CONFIRM",
  WAIT_DEP:      "DEPENDENCY",
  BLOCKED_DEP:   "BLOCKED",
  WAIT_CONDITION:"CONDITION",
  WAIT_AGENT:    "AGENT",
  WAIT_RESOURCE: "RESOURCE",
};

function ExplainPanel({ instanceId, status, onConfirm }: { instanceId: string; status: JobInstance["status"]; onConfirm: (id: string) => void }) {
  const [exp, setExp] = useState<Explanation | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setInterval> | undefined;
    const load = () => {
      fetchInstanceExplain(instanceId)
        .then((e) => { if (!cancelled) setExp(e); })
        .catch(() => { if (!cancelled) setExp(null); })
        .finally(() => { if (!cancelled) setLoading(false); });
    };
    load();
    // WAITING muda conforme upstreams terminam / recursos liberam → refresca.
    if (status === "WAITING") timer = setInterval(load, 3000);
    return () => { cancelled = true; if (timer) clearInterval(timer); };
  }, [instanceId, status]);

  // Modo local (sem server) devolve null → não mostra o painel.
  if (!loading && !exp) return null;

  const runnable = exp?.runnable ?? false;
  const accent = runnable ? "var(--v2-status-ok)" : "var(--v2-status-waiting)";
  // Control-M Confirm: quando o gate WAIT_CONFIRM está ativo, é AQUI (onde o
  // motivo aparece) que o operador libera — sinal preciso vindo do próprio
  // Explain, sem depender de flag denormalizada na lista.
  const needsConfirm = exp?.blockers.some((b) => b.kind === "WAIT_CONFIRM") ?? false;

  return (
    <div style={{ marginBottom: 16 }}>
      <div style={{ fontSize: 9, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)", letterSpacing: "0.1em", textTransform: "uppercase", marginBottom: 6 }}>
        {runnable ? "Will it run?" : "Why not?"}
      </div>
      <div style={{ padding: "7px 9px", background: "var(--v2-bg-canvas)", border: `1px solid ${accent}`, borderRadius: 3 }}>
        <div style={{ fontSize: 11, color: "var(--v2-text-primary)", lineHeight: 1.45 }}>
          {loading ? "evaluating…" : exp?.summary}
        </div>
        {exp && exp.blockers.length > 0 && (
          <div style={{ marginTop: 8, display: "flex", flexDirection: "column", gap: 6 }}>
            {exp.blockers.map((b, i) => (
              <div key={i} style={{ display: "flex", gap: 7, alignItems: "flex-start" }}>
                <span style={{
                  flexShrink: 0, marginTop: 1, fontSize: 8, fontFamily: "var(--v2-font-mono)", fontWeight: 700,
                  letterSpacing: "0.05em", color: BLOCKER_COLOR[b.kind], border: `1px solid ${BLOCKER_COLOR[b.kind]}`,
                  borderRadius: 2, padding: "1px 4px", textTransform: "uppercase",
                }}>
                  {BLOCKER_LABEL[b.kind]}
                </span>
                <span style={{ fontSize: 10.5, color: "var(--v2-text-secondary)", lineHeight: 1.4 }}>
                  {b.detail}
                </span>
              </div>
            ))}
          </div>
        )}
        {needsConfirm && (
          <button
            onClick={() => onConfirm(instanceId)}
            style={{
              marginTop: 10, width: "100%", padding: "6px 10px", fontSize: 11, fontWeight: 700,
              fontFamily: "var(--v2-font-mono)", letterSpacing: "0.04em", cursor: "pointer",
              background: "#a78bfa", color: "#1a1030", border: "none", borderRadius: 4,
            }}
          >
            ✓ Confirm execution
          </button>
        )}
      </div>
    </div>
  );
}

/* ── Blast Radius ("what if I cancel/hold this job now?") ── */

function BlastPanel({ instanceId }: { instanceId: string }) {
  const [br, setBr] = useState<BlastRadius | null>(null);
  const [loading, setLoading] = useState(false);
  const [opened, setOpened] = useState(false);

  const load = () => {
    setOpened(true);
    setLoading(true);
    fetchBlastRadius(instanceId)
      .then((b) => setBr(b))
      .catch(() => setBr(null))
      .finally(() => setLoading(false));
  };

  if (!opened) {
    return (
      <div style={{ marginBottom: 16 }}>
        <button
          onClick={load}
          style={{
            width: "100%", padding: "7px 10px", fontSize: 10, fontFamily: "var(--v2-font-mono)",
            letterSpacing: "0.05em", textTransform: "uppercase", cursor: "pointer",
            border: "1px solid var(--v2-status-failed)", background: "rgba(239,68,68,0.06)",
            color: "var(--v2-status-failed)", borderRadius: 3, fontWeight: 600,
          }}
        >
          ⚠ Impact if cancelled / held
        </button>
      </div>
    );
  }

  const c = br?.counts;
  return (
    <div style={{ marginBottom: 16 }}>
      <div style={{ fontSize: 9, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)", letterSpacing: "0.1em", textTransform: "uppercase", marginBottom: 6 }}>
        Blast radius
      </div>
      <div style={{ padding: "8px 10px", background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-status-failed)", borderRadius: 3 }}>
        {loading && <span style={{ fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)" }}>computing impact…</span>}
        {!loading && !br && <span style={{ fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)" }}>unavailable</span>}
        {!loading && br && (
          <>
            <div style={{ fontSize: 11, color: "var(--v2-text-primary)", lineHeight: 1.5 }}>
              Cancelling/holding this job blocks <b style={{ color: "var(--v2-status-failed)" }}>{c!.downstream}</b> downstream job(s)
              {c!.slaAtRisk > 0 && <> · <b style={{ color: "var(--v2-status-waiting)" }}>{c!.slaAtRisk}</b> SLA(s) at risk</>}
              {c!.teamsAffected > 0 && <> · {c!.teamsAffected} folder(s)</>}
              {c!.maxDepth > 0 && <> · cascade up to {c!.maxDepth} level(s)</>}.
            </div>
            {br.downstream.length > 0 && (
              <div style={{ marginTop: 8, display: "flex", flexDirection: "column", gap: 3, maxHeight: 180, overflowY: "auto" }}>
                {br.downstream.map((n) => (
                  <div key={n.defId} style={{ display: "flex", gap: 7, alignItems: "baseline", fontSize: 10, fontFamily: "var(--v2-font-mono)" }}>
                    <span style={{ color: "var(--v2-text-muted)", width: 16, textAlign: "right" }}>{n.depth}·</span>
                    <span style={{ color: "var(--v2-text-primary)" }}>{n.defId}</span>
                    {n.hasSla && <span style={{ color: "var(--v2-status-waiting)", fontSize: 8, border: "1px solid var(--v2-status-waiting)", borderRadius: 2, padding: "0 3px" }}>SLA</span>}
                    {n.team && <span style={{ color: "var(--v2-accent-brand)", marginLeft: "auto" }}>{n.team}</span>}
                  </div>
                ))}
                {br.truncated && <span style={{ color: "var(--v2-text-muted)", fontSize: 9 }}>… list truncated (counts are exact).</span>}
              </div>
            )}
            {br.downstream.length === 0 && (
              <div style={{ marginTop: 6, fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-status-ok)" }}>
                No downstream impacted — safe to cancel.
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

/* ── RCA ("what's the root cause?") — auto-loads for NOTOK/blocked ── */

function RCAPanel({ instanceId }: { instanceId: string }) {
  const [rca, setRca] = useState<RCA | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    fetchRCA(instanceId)
      .then((r) => { if (!cancelled) setRca(r); })
      .catch(() => { if (!cancelled) setRca(null); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [instanceId]);

  // Modo local (null) ou sem raízes → não mostra o painel (evita ruído em job OK).
  if (!loading && (!rca || rca.roots.length === 0)) return null;

  return (
    <div style={{ marginBottom: 16 }}>
      <div style={{ fontSize: 9, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)", letterSpacing: "0.1em", textTransform: "uppercase", marginBottom: 6 }}>
        Root cause
      </div>
      <div style={{ padding: "8px 10px", background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-status-failed)", borderRadius: 3 }}>
        {loading && <span style={{ fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)" }}>tracing…</span>}
        {!loading && rca && (
          <>
            <div style={{ fontSize: 11, color: "var(--v2-text-primary)", lineHeight: 1.5 }}>{rca.summary}</div>
            <div style={{ marginTop: 8, display: "flex", flexDirection: "column", gap: 4 }}>
              {rca.roots.map((r) => (
                <div key={r.defId} style={{ display: "flex", gap: 7, alignItems: "baseline", fontSize: 10, fontFamily: "var(--v2-font-mono)" }}>
                  <span style={{ color: "var(--v2-status-failed)", fontSize: 8, border: "1px solid var(--v2-status-failed)", borderRadius: 2, padding: "0 3px" }}>{r.status}</span>
                  <span style={{ color: "var(--v2-text-primary)" }}>{r.label || r.defId}</span>
                  <span style={{ color: "var(--v2-text-muted)" }}>· {r.depth === 0 ? "this job" : `${r.depth} hop(s) up`}</span>
                  {r.team && <span style={{ color: "var(--v2-accent-brand)", marginLeft: "auto" }}>{r.team}</span>}
                </div>
              ))}
            </div>
            {rca.chain && rca.chain.length > 1 && (
              <div style={{ marginTop: 8, fontSize: 9.5, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)" }}>
                chain: {rca.chain.join(" → ")}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

/* ── Job Neighborhood ("who blocks me, whom do I block?") ── */

function NeighborhoodPanel({ instanceId, autoLoad }: { instanceId: string; autoLoad?: boolean }) {
  const [nb, setNb] = useState<Neighborhood | null>(null);
  const [loading, setLoading] = useState(false);
  const [opened, setOpened] = useState(false);
  const [radius, setRadius] = useState(1);

  const load = (r: number) => {
    setOpened(true);
    setLoading(true);
    setRadius(r);
    fetchNeighborhood(instanceId, r)
      .then((n) => setNb(n))
      .catch(() => setNb(null))
      .finally(() => setLoading(false));
  };

  // Aba dedicada: carrega sozinho ao montar (radius 1).
  useEffect(() => {
    if (autoLoad) load(1);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [instanceId, autoLoad]);

  if (!opened) {
    return (
      <div style={{ marginBottom: 16 }}>
        <button
          onClick={() => load(1)}
          style={{
            width: "100%", padding: "7px 10px", fontSize: 10, fontFamily: "var(--v2-font-mono)",
            letterSpacing: "0.05em", textTransform: "uppercase", cursor: "pointer",
            border: "1px solid var(--v2-border-medium)", background: "transparent",
            color: "var(--v2-text-secondary)", borderRadius: 3, fontWeight: 600,
          }}
        >
          ⇅ Job neighborhood
        </button>
      </div>
    );
  }

  const row = (n: NeighborNode) => (
    <div key={n.defId} style={{ display: "flex", gap: 7, alignItems: "baseline", fontSize: 10, fontFamily: "var(--v2-font-mono)" }}>
      <span style={{ color: "var(--v2-text-muted)", width: 16, textAlign: "right" }}>{n.depth}·</span>
      <span style={{ color: "var(--v2-text-primary)" }}>{n.label || n.defId}</span>
      {n.status && <span style={{ color: "var(--v2-text-muted)", fontSize: 8 }}>{n.status}</span>}
      {n.condition && <span style={{ color: "var(--v2-accent-brand)", fontSize: 8 }}>{n.condition}</span>}
      {n.team && <span style={{ color: "var(--v2-accent-brand)", marginLeft: "auto" }}>{n.team}</span>}
    </div>
  );

  return (
    <div style={{ marginBottom: 16 }}>
      <div style={{ display: "flex", alignItems: "center", marginBottom: 6 }}>
        <span style={{ fontSize: 9, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)", letterSpacing: "0.1em", textTransform: "uppercase" }}>
          Neighborhood
        </span>
        <div style={{ marginLeft: "auto", display: "flex", gap: 3 }}>
          {[1, 2, 3].map((r) => (
            <button key={r} onClick={() => load(r)} disabled={loading} style={{
              fontSize: 9, fontFamily: "var(--v2-font-mono)", cursor: "pointer", padding: "1px 6px", borderRadius: 2,
              background: radius === r ? "var(--v2-accent-deep)" : "transparent",
              border: `1px solid ${radius === r ? "var(--v2-accent-brand)" : "var(--v2-border-medium)"}`,
              color: radius === r ? "var(--v2-accent-brand)" : "var(--v2-text-muted)",
            }}>±{r}</button>
          ))}
        </div>
      </div>
      <div style={{ padding: "8px 10px", background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-medium)", borderRadius: 3 }}>
        {loading && <span style={{ fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)" }}>loading…</span>}
        {!loading && !nb && <span style={{ fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)" }}>unavailable</span>}
        {!loading && nb && (
          <div style={{ display: "flex", flexDirection: "column", gap: 8, maxHeight: 320, overflowY: "auto" }}>
            <div>
              <div style={{ fontSize: 8.5, color: "var(--v2-text-muted)", textTransform: "uppercase", letterSpacing: "0.06em", marginBottom: 3 }}>Depends on ({nb.upstream.length})</div>
              {nb.upstream.length === 0 ? <span style={{ fontSize: 10, color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)" }}>— none</span> : nb.upstream.map(row)}
            </div>
            <div>
              <div style={{ fontSize: 8.5, color: "var(--v2-text-muted)", textTransform: "uppercase", letterSpacing: "0.06em", marginBottom: 3 }}>Triggers ({nb.downstream.length})</div>
              {nb.downstream.length === 0 ? <span style={{ fontSize: 10, color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)" }}>— none</span> : nb.downstream.map(row)}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function LogPanel({ instanceId, status }: { instanceId: string; status: JobInstance["status"] }) {
  const [events, setEvents] = useState<InstanceEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setInterval> | undefined;

    const load = () => {
      fetchInstanceEvents(instanceId)
        .then((evs) => {
          if (cancelled) return;
          setEvents(evs);
          setError(null);
        })
        .catch((e) => {
          if (cancelled) return;
          setError(e?.message ?? String(e));
        })
        .finally(() => {
          if (!cancelled) setLoading(false);
        });
    };

    load();
    // Auto-refresh enquanto a instance estiver em estado mutável (RUNNING/WAITING/HOLD)
    if (status === "RUNNING" || status === "WAITING" || status === "HOLD") {
      timer = setInterval(load, 3000);
    }
    return () => {
      cancelled = true;
      if (timer) clearInterval(timer);
    };
  }, [instanceId, status]);

  return (
    <div style={{ flex: 1, overflowY: "auto", padding: "10px 12px", fontSize: 11 }}>
      {loading && events.length === 0 && (
        <div style={{ color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)", fontSize: 10 }}>
          loading…
        </div>
      )}
      {error && (
        <div style={{ color: "var(--v2-status-failed)", fontFamily: "var(--v2-font-mono)", fontSize: 10 }}>
          {error}
        </div>
      )}
      {!loading && !error && events.length === 0 && (
        <div style={{ color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)", fontSize: 10 }}>
          no events yet
        </div>
      )}
      {events.map((e) => {
        const color = KIND_COLOR[e.kind] ?? "var(--v2-text-secondary)";
        return (
          <div
            key={e.id}
            style={{
              display: "grid",
              gridTemplateColumns: "78px 1fr",
              gap: 8,
              padding: "5px 0",
              borderBottom: "1px dashed var(--v2-border-subtle)",
            }}
          >
            <span
              style={{
                fontFamily: "var(--v2-font-mono)",
                fontSize: 9,
                color: "var(--v2-text-muted)",
                whiteSpace: "nowrap",
              }}
              title={e.ts}
            >
              {fmtTS(e.ts)}
            </span>
            <div style={{ minWidth: 0 }}>
              <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
                <span
                  style={{
                    fontFamily: "var(--v2-font-mono)",
                    fontSize: 9,
                    color,
                    fontWeight: 700,
                    letterSpacing: "0.06em",
                    textTransform: "uppercase",
                  }}
                >
                  {e.kind}
                </span>
                {e.actor && (
                  <span style={{ fontSize: 9, color: "var(--v2-text-muted)" }}>
                    · {e.actor}
                  </span>
                )}
              </div>
              {e.message && (
                <div
                  style={{
                    fontSize: 10,
                    color: "var(--v2-text-secondary)",
                    fontFamily: "var(--v2-font-mono)",
                    marginTop: 2,
                    wordBreak: "break-word",
                  }}
                >
                  {e.message}
                </div>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
