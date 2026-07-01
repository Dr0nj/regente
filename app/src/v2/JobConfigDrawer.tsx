import { useEffect, useState } from "react";
import { ExternalLink, X, Trash2, ArrowRight, ArrowLeft } from "lucide-react";
import type { JobDefinition, CalendarRef, EdgeCondition, ActionRule } from "@/lib/orchestrator-model";
import type { JobType } from "@/lib/job-config";
import JobActionConfigEditor from "./JobActionConfigEditor";
import OnDoEditor from "./OnDoEditor";
import ScheduleEditor from "./ScheduleEditor";
import { DefinitionAuditPanel } from "./DefinitionAuditPanel";
import { ErrorDialog } from "./ErrorDialog";
import { getGitInfo, definitionFileUrl } from "@/lib/git-info";
import { listCalendars, type Calendar } from "@/lib/bloco2-api";
import { listAgents, type AgentInfo } from "@/lib/agents-api";
import { useResizablePanel, ResizeHandle } from "./resizable";

/* ──────────────────────────────────────────────────────────────
   JobConfigDrawer — painel direito (Design). ABAS:
   General · Schedule · Action · On/Do · Dependencies.
   (Calendários foram fundidos na aba Schedule — trabalham junto com
   as regras como include/exclude; ver ScheduleEditor.)
   "Action" = config de EXECUÇÃO do job (command/url/script).
   "On/Do"  = regras REATIVAS ao ciclo (Control-M On-Do); ver OnDoEditor.
   ────────────────────────────────────────────────────────────── */

export interface JobConfigHandlers {
  onSave: (def: JobDefinition) => void | Promise<void>;
  onDelete: (id: string) => void | Promise<void>;
  onClose: () => void;
}

const JOB_TYPES: JobType[] = ["COMMAND", "SCRIPT", "SSH", "HTTP", "LAMBDA", "BATCH", "GLUE", "STEP_FUNCTION", "CHOICE", "PARALLEL", "WAIT"];
type Tab = "general" | "schedule" | "action" | "ondo" | "deps";
const TABS: Array<{ id: Tab; label: string }> = [
  { id: "general", label: "Geral" },
  { id: "schedule", label: "Schedule" },
  { id: "action", label: "Action" },
  { id: "ondo", label: "On/Do" },
  { id: "deps", label: "Dependências" },
];

interface Props {
  definition: JobDefinition;
  isNew: boolean;
  availableFolders: string[];
  /** Todas as defs do escopo — para a aba Dependencies (depende de / dispara). */
  allDefs?: JobDefinition[];
  handlers: JobConfigHandlers;
}

export default function JobConfigDrawer({ definition, isNew, availableFolders, allDefs = [], handlers }: Props) {
  const [tab, setTab] = useState<Tab>("general");
  const [label, setLabel] = useState(definition.label);
  const [id, setId] = useState(definition.id);
  const [jobType, setJobType] = useState<JobType>(definition.jobType as JobType);
  const initialTeam = definition.team ?? (availableFolders.length === 1 ? availableFolders[0] : "");
  const [team, setTeam] = useState(initialTeam);
  const [schedule, setSchedule] = useState(definition.schedule);
  const [retries, setRetries] = useState(definition.retries ?? 2);
  const [timeout, setTimeoutS] = useState(definition.timeout ?? 300);
  const [dryRun, setDryRun] = useState(definition.dryRun ?? false);
  const [actionConfig, setActionConfig] = useState<Record<string, unknown>>(definition.actionConfig ?? {});
  const [calendars, setCalendars] = useState<CalendarRef[]>(definition.calendars ?? []);
  const [actions, setActions] = useState<ActionRule[]>(definition.actions ?? []);
  const [upstream, setUpstream] = useState(definition.upstream ?? []);
  const [agentId, setAgentId] = useState<string>(
    typeof definition.actionConfig?._agentId === "string" ? (definition.actionConfig._agentId as string) : ""
  );
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [saving, setSaving] = useState(false);
  const [showAudit, setShowAudit] = useState(false);
  const [err, setErr] = useState<unknown>(null);
  const [validationErr, setValidationErr] = useState<string | null>(null);
  const [githubUrl, setGithubUrl] = useState<string | null>(null);
  const [calendarDefs, setCalendarDefs] = useState<Calendar[]>([]);

  useEffect(() => {
    setTab("general");
    setLabel(definition.label); setId(definition.id); setJobType(definition.jobType as JobType);
    setTeam(definition.team ?? (availableFolders.length === 1 ? availableFolders[0] : ""));
    setSchedule(definition.schedule);
    setRetries(definition.retries ?? 2); setTimeoutS(definition.timeout ?? 300);
    setDryRun(definition.dryRun ?? false); setActionConfig(definition.actionConfig ?? {});
    setCalendars(definition.calendars ?? []); setActions(definition.actions ?? []); setUpstream(definition.upstream ?? []);
    setAgentId(typeof definition.actionConfig?._agentId === "string" ? (definition.actionConfig._agentId as string) : "");
    setErr(null); setValidationErr(null);
  }, [definition.id]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (isNew || !definition.team || !definition.id) { setGithubUrl(null); return; }
    let cancel = false;
    void getGitInfo().then((st) => { if (!cancel) setGithubUrl(definitionFileUrl(st, definition.team!, definition.id)); });
    return () => { cancel = true; };
  }, [isNew, definition.team, definition.id]);

  useEffect(() => {
    let cancel = false;
    void listCalendars().then((cs) => { if (!cancel) setCalendarDefs(cs); }).catch(() => {});
    void listAgents().then((a) => { if (!cancel) setAgents(a); }).catch(() => {});
    return () => { cancel = true; };
  }, []);

  async function handleSave() {
    if (!label.trim()) { setValidationErr("label obrigatório"); setTab("general"); return; }
    if (!team.trim()) { setValidationErr("folder obrigatória"); setTab("general"); return; }
    if (!id.trim()) { setValidationErr("id obrigatório"); setTab("general"); return; }
    setValidationErr(null);
    const next: JobDefinition = {
      ...definition,
      id: id.trim(), label: label.trim(), jobType, team: team.trim(),
      schedule: { ...schedule, enabled: schedule.enabled ?? true },
      retries, timeout, dryRun,
      // _agentId é o canal que o ServerApiAdapter usa p/ mapear actionConfig→agentId.
      actionConfig: { ...actionConfig, _agentId: agentId.trim() || undefined },
      calendars: calendars.length ? calendars : undefined,
      actions: actions.length ? actions : undefined,
      upstream: upstream.length ? upstream : undefined,
    };
    setSaving(true); setErr(null);
    try { await handlers.onSave(next); } catch (e) { setErr(e); } finally { setSaving(false); }
  }

  async function handleDelete() {
    if (isNew) { handlers.onClose(); return; }
    if (!confirm(`Delete definition "${definition.label}"? Vai remover o YAML do repo.`)) return;
    setSaving(true);
    try { await handlers.onDelete(definition.id); } catch (e) { setErr(e); } finally { setSaving(false); }
  }

  // Jobs que DEPENDEM deste (this.id ∈ outro.upstream.from) → "dispara".
  const triggers = allDefs.filter((d) => (d.upstream ?? []).some((u) => u.from === definition.id));

  const { width, onMouseDown, reset } = useResizablePanel({
    storageKey: "regente.panel.jobConfig.w", defaultWidth: 360, min: 280, max: 720, edge: "left",
  });

  return (
    <aside style={{
      position: "absolute", top: 0, right: 0, bottom: 0, width,
      background: "var(--v2-bg-surface)", borderLeft: "1px solid var(--v2-border-medium)",
      display: "flex", flexDirection: "column", fontFamily: "var(--v2-font-sans)", zIndex: 5, overflow: "hidden",
    }}>
      <ResizeHandle edge="left" onMouseDown={onMouseDown} onReset={reset} />
      {/* Header */}
      <div style={{ padding: "10px 12px", borderBottom: "1px solid var(--v2-border-subtle)", display: "flex", alignItems: "center", gap: 8 }}>
        <span style={{ width: 6, height: 6, borderRadius: "50%", background: "var(--v2-accent-brand)" }} />
        <span style={{ fontSize: 11, fontWeight: 600, letterSpacing: "0.04em" }}>{isNew ? "NEW JOB" : "EDIT JOB"}</span>
        {githubUrl && (
          <a href={githubUrl} target="_blank" rel="noreferrer" title="Ver o YAML no GitHub"
            style={{ display: "inline-flex", alignItems: "center", gap: 4, fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-secondary)", textDecoration: "none", padding: "2px 7px", border: "1px solid var(--v2-border-medium)", borderRadius: 3, marginLeft: 6 }}>
            <ExternalLink size={10} /> GitHub
          </a>
        )}
        <button onClick={handlers.onClose} title="Close"
          style={{ marginLeft: "auto", background: "transparent", border: "none", color: "var(--v2-text-secondary)", cursor: "pointer", padding: 0, width: 18, height: 18, display: "inline-flex", alignItems: "center", justifyContent: "center" }}>
          <X size={14} />
        </button>
      </div>

      {/* Tabs */}
      <div style={{ display: "flex", borderBottom: "1px solid var(--v2-border-subtle)", overflowX: "auto" }}>
        {TABS.map((t) => (
          <button key={t.id} onClick={() => setTab(t.id)} style={{
            padding: "7px 11px", fontSize: 10.5, cursor: "pointer", whiteSpace: "nowrap",
            background: "transparent", border: "none",
            borderBottom: `2px solid ${tab === t.id ? "var(--v2-accent-brand)" : "transparent"}`,
            color: tab === t.id ? "var(--v2-text-primary)" : "var(--v2-text-muted)",
            fontWeight: tab === t.id ? 600 : 500, fontFamily: "var(--v2-font-mono)",
          }}>{t.label}
            {t.id === "deps" && (upstream.length + triggers.length > 0) ? ` (${upstream.length + triggers.length})` : ""}
            {t.id === "ondo" && actions.length > 0 ? ` (${actions.length})` : ""}
          </button>
        ))}
      </div>

      <div style={{ flex: 1, overflowY: "auto", padding: "12px", display: "flex", flexDirection: "column", gap: 12 }}>
        {tab === "general" && (
          <>
            <Field label="Job Name"><Input value={label} onChange={setLabel} /></Field>
            <Field label="Job Type">
              <select value={jobType} onChange={(e) => setJobType(e.target.value as JobType)} style={selectStyle}>
                {JOB_TYPES.map((t) => <option key={t} value={t}>{t}</option>)}
              </select>
            </Field>
            <Field label="Folder">
              <select value={team} onChange={(e) => setTeam(e.target.value)} style={selectStyle}>
                <option value="">— select folder —</option>
                {availableFolders.map((t) => <option key={t} value={t}>{t}</option>)}
              </select>
            </Field>
            <Field label="Agente (onde roda)">
              <select value={agentId} onChange={(e) => setAgentId(e.target.value)} style={selectStyle}>
                <option value="">Automático (por capability)</option>
                {agents.map((a) => (
                  <option key={a.id} value={a.id}>{a.id} — {a.capabilities.join("/") || "sem caps"}</option>
                ))}
                {agentId && !agents.some((a) => a.id === agentId) && (
                  <option value={agentId}>{agentId} (offline)</option>
                )}
              </select>
              <div style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 4, lineHeight: 1.4 }}>
                {agents.length === 0
                  ? "Nenhum agente online. Rode o regente-agent na máquina alvo."
                  : "Vazio = o server escolhe um agente com a capability do jobType."}
              </div>
            </Field>
            <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 11, color: "var(--v2-text-secondary)" }}>
              <input type="checkbox" checked={schedule.enabled} onChange={(e) => setSchedule({ ...schedule, enabled: e.target.checked })} />
              Schedule habilitado
            </label>
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
              <Field label="Retries"><Input value={String(retries)} onChange={(v) => setRetries(Number(v) || 0)} mono /></Field>
              <Field label="Timeout (s)"><Input value={String(timeout)} onChange={(v) => setTimeoutS(Number(v) || 0)} mono /></Field>
            </div>
            <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 11, color: "var(--v2-text-secondary)" }}>
              <input type="checkbox" checked={dryRun} onChange={(e) => setDryRun(e.target.checked)} />
              Dry run (log only, não executa)
            </label>
          </>
        )}

        {tab === "schedule" && (
          <ScheduleEditor
            value={schedule}
            onChange={setSchedule}
            calendars={calendars}
            onCalendarsChange={setCalendars}
            availableCalendars={calendarDefs}
          />
        )}

        {tab === "action" && (
          <JobActionConfigEditor jobType={jobType} config={actionConfig} onChange={setActionConfig} />
        )}

        {tab === "ondo" && (
          <OnDoEditor value={actions} onChange={setActions} allDefs={allDefs} selfId={definition.id} />
        )}

        {tab === "deps" && (
          <DepsTab
            self={definition.id}
            upstream={upstream}
            onChangeUpstream={setUpstream}
            triggers={triggers}
            allDefs={allDefs}
          />
        )}

        {err != null && <ErrorDialog error={err} onClose={() => setErr(null)} />}
        {validationErr && (
          <div style={{ padding: 8, background: "rgba(239,68,68,.08)", border: "1px solid rgba(239,68,68,.3)", borderRadius: 3, color: "var(--v2-status-failed)", fontSize: 11, fontFamily: "var(--v2-font-mono)" }}>{validationErr}</div>
        )}
        {tab === "general" && !isNew && team && id && (
          <Field label="History">
            <button type="button" onClick={() => setShowAudit((x) => !x)} style={{ background: "transparent", border: "1px solid #333", color: "#a3a3a3", padding: "4px 10px", borderRadius: 4, fontSize: 11, cursor: "pointer", marginBottom: showAudit ? 8 : 0 }}>
              {showAudit ? "▾ Hide audit log" : "▸ Show audit log"}
            </button>
            {showAudit && <DefinitionAuditPanel team={team} definitionId={id} />}
          </Field>
        )}
      </div>

      {/* Actions */}
      <div style={{ borderTop: "1px solid var(--v2-border-subtle)", padding: "8px 12px", display: "flex", gap: 6 }}>
        <button onClick={handleDelete} disabled={saving} style={{ ...btnStyle, borderColor: "rgba(239,68,68,.4)", color: "var(--v2-status-failed)" }}>{isNew ? "Cancel" : "Delete"}</button>
        <div style={{ flex: 1 }} />
        <button onClick={handleSave} disabled={saving} style={{ ...btnStyle, borderColor: "var(--v2-accent-brand)", color: "var(--v2-accent-brand)", fontWeight: 600 }}>{saving ? "…" : "Save"}</button>
      </div>
    </aside>
  );
}

/* ── Aba Dependencies (2 lados) ── */
function DepsTab({ self, upstream, onChangeUpstream, triggers, allDefs }: {
  self: string;
  upstream: Array<{ from: string; condition: EdgeCondition }>;
  onChangeUpstream: (u: Array<{ from: string; condition: EdgeCondition }>) => void;
  triggers: JobDefinition[];
  allDefs: JobDefinition[];
}) {
  const labelOf = (id: string) => allDefs.find((d) => d.id === id)?.label ?? id;
  const remove = (from: string) => onChangeUpstream(upstream.filter((u) => u.from !== from));
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      {/* Depende de (upstream do próprio job) */}
      <div>
        <div style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 9, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--v2-text-muted)", marginBottom: 6 }}>
          <ArrowLeft size={11} /> Depende de
        </div>
        {upstream.length === 0 && <Hint>Este job não espera nenhum outro. Conecte um job → este no canvas para criar.</Hint>}
        {upstream.map((u) => (
          <div key={u.from} style={depRow}>
            <span style={{ flex: 1, fontSize: 12, fontFamily: "var(--v2-font-mono)" }}>{labelOf(u.from)}</span>
            <span style={{ fontSize: 9, color: "var(--v2-accent-brand)", fontFamily: "var(--v2-font-mono)" }}>{u.condition}</span>
            <button onClick={() => remove(u.from)} style={iconBtn} title="Remover"><Trash2 size={12} /></button>
          </div>
        ))}
      </div>
      {/* Dispara (jobs cujo upstream aponta para este) */}
      <div>
        <div style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 9, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--v2-text-muted)", marginBottom: 6 }}>
          <ArrowRight size={11} /> Dispara
        </div>
        {triggers.length === 0 && <Hint>Nenhum job depende deste.</Hint>}
        {triggers.map((d) => {
          const cond = (d.upstream ?? []).find((u) => u.from === self)?.condition;
          return (
            <div key={d.id} style={depRow}>
              <span style={{ flex: 1, fontSize: 12, fontFamily: "var(--v2-font-mono)" }}>{d.label}</span>
              <span style={{ fontSize: 9, color: "var(--v2-accent-brand)", fontFamily: "var(--v2-font-mono)" }}>{cond}</span>
            </div>
          );
        })}
        <Hint>Editado no job de destino (ou conectando no canvas). Mostrado aqui para você ver a relação dos dois lados.</Hint>
      </div>
    </div>
  );
}

/* ── primitivos ── */
function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (<div><div style={{ fontSize: 9, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--v2-text-muted)", marginBottom: 4 }}>{label}</div>{children}</div>);
}
function Hint({ children }: { children: React.ReactNode }) {
  return <div style={{ fontSize: 10.5, color: "var(--v2-text-muted)", lineHeight: 1.45 }}>{children}</div>;
}
function Input({ value, onChange, disabled, mono, placeholder }: { value: string; onChange: (v: string) => void; disabled?: boolean; mono?: boolean; placeholder?: string }) {
  return <input value={value} onChange={(e) => onChange(e.target.value)} disabled={disabled} placeholder={placeholder}
    style={{ width: "100%", background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)", color: disabled ? "var(--v2-text-muted)" : "var(--v2-text-primary)", padding: "5px 8px", fontSize: 11, fontFamily: mono ? "var(--v2-font-mono)" : "var(--v2-font-sans)", borderRadius: 3, outline: "none", boxSizing: "border-box" }} />;
}
const selectStyle: React.CSSProperties = { width: "100%", background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)", color: "var(--v2-text-primary)", padding: "5px 8px", fontSize: 11, fontFamily: "var(--v2-font-mono)", borderRadius: 3, outline: "none", boxSizing: "border-box" };
const btnStyle: React.CSSProperties = { padding: "4px 10px", background: "transparent", border: "1px solid var(--v2-border-medium)", borderRadius: 3, fontSize: 10, fontFamily: "var(--v2-font-mono)", letterSpacing: "0.06em", textTransform: "uppercase", cursor: "pointer" };
const iconBtn: React.CSSProperties = { background: "transparent", border: "none", color: "var(--v2-text-muted)", cursor: "pointer", padding: 2, display: "inline-flex" };
const depRow: React.CSSProperties = { display: "flex", alignItems: "center", gap: 8, padding: "6px 8px", background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)", borderRadius: 3, marginBottom: 4 };
