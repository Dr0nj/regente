import { useEffect, useState } from "react";
import type { JobDefinition } from "@/lib/orchestrator-model";
import { TEAMS } from "@/lib/orchestrator-model";
import type { JobType } from "@/lib/job-config";

/* ──────────────────────────────────────────────────────────────
   JobConfigDrawer — painel direito para editar JobDefinition
   ──────────────────────────────────────────────────────────────
   Usado em Design mode. Casamento 1:1 com InstanceDetailsDrawer
   visualmente. Campos obrigatórios: label, jobType, team.
   Save persiste via `definition-store` (Git ou localStorage).
   ────────────────────────────────────────────────────────────── */

export interface JobConfigHandlers {
  onSave: (def: JobDefinition) => void | Promise<void>;
  onDelete: (id: string) => void | Promise<void>;
  onClose: () => void;
}

const JOB_TYPES: JobType[] = [
  "LAMBDA",
  "BATCH",
  "GLUE",
  "STEP_FUNCTION",
  "CHOICE",
  "PARALLEL",
  "WAIT",
  "HTTP",
];

interface Props {
  definition: JobDefinition;
  isNew: boolean;
  handlers: JobConfigHandlers;
}

export default function JobConfigDrawer({ definition, isNew, handlers }: Props) {
  const [label, setLabel] = useState(definition.label);
  const [id, setId] = useState(definition.id);
  const [jobType, setJobType] = useState<JobType>(definition.jobType as JobType);
  const [team, setTeam] = useState(definition.team ?? "");
  const [cron, setCron] = useState(definition.schedule?.cronExpression ?? "");
  const [enabled, setEnabled] = useState(definition.schedule?.enabled ?? true);
  const [retries, setRetries] = useState(definition.retries ?? 2);
  const [timeout, setTimeoutS] = useState(definition.timeout ?? 300);
  const [dryRun, setDryRun] = useState(definition.dryRun ?? false);
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    setLabel(definition.label);
    setId(definition.id);
    setJobType(definition.jobType as JobType);
    setTeam(definition.team ?? "");
    setCron(definition.schedule?.cronExpression ?? "");
    setEnabled(definition.schedule?.enabled ?? true);
    setRetries(definition.retries ?? 2);
    setTimeoutS(definition.timeout ?? 300);
    setDryRun(definition.dryRun ?? false);
    setErr(null);
  }, [definition.id]); // eslint-disable-line react-hooks/exhaustive-deps

  async function handleSave() {
    if (!label.trim()) { setErr("label obrigatório"); return; }
    if (!team.trim()) { setErr("team (folder) obrigatório"); return; }
    if (!id.trim())    { setErr("id obrigatório"); return; }
    const next: JobDefinition = {
      ...definition,
      id: id.trim(),
      label: label.trim(),
      jobType,
      team: team.trim(),
      schedule: {
        cronExpression: cron.trim(),
        enabled: enabled && !!cron.trim(),
        description: definition.schedule?.description,
      },
      retries,
      timeout,
      dryRun,
    };
    setSaving(true);
    setErr(null);
    try {
      await handlers.onSave(next);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  }

  async function handleDelete() {
    if (isNew) { handlers.onClose(); return; }
    if (!confirm(`Delete definition "${definition.label}"? Vai remover o YAML do repo.`)) return;
    setSaving(true);
    try {
      await handlers.onDelete(definition.id);
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <aside
      style={{
        position: "absolute",
        top: 12,
        right: 12,
        bottom: 12,
        width: 340,
        background: "var(--v2-bg-surface)",
        border: "1px solid var(--v2-border-medium)",
        borderRadius: 6,
        display: "flex",
        flexDirection: "column",
        fontFamily: "var(--v2-font-sans)",
        zIndex: 5,
        overflow: "hidden",
      }}
    >
      {/* Header */}
      <div
        style={{
          padding: "10px 12px",
          borderBottom: "1px solid var(--v2-border-subtle)",
          display: "flex",
          alignItems: "center",
          gap: 8,
        }}
      >
        <span style={{ width: 6, height: 6, borderRadius: "50%", background: "var(--v2-accent-brand)" }} />
        <span style={{ fontSize: 11, fontWeight: 600, letterSpacing: "0.04em" }}>
          {isNew ? "NEW JOB" : "EDIT JOB"}
        </span>
        <button
          onClick={handlers.onClose}
          style={{
            marginLeft: "auto",
            background: "transparent",
            border: "none",
            color: "var(--v2-text-secondary)",
            cursor: "pointer",
            fontSize: 14,
            padding: 0,
            width: 18,
            height: 18,
          }}
          title="Close"
        >
          ×
        </button>
      </div>

      <div style={{ flex: 1, overflowY: "auto", padding: "10px 12px", display: "flex", flexDirection: "column", gap: 12 }}>
        <Field label="ID">
          <Input value={id} onChange={setId} disabled={!isNew} mono />
        </Field>
        <Field label="Label">
          <Input value={label} onChange={setLabel} />
        </Field>
        <Field label="Job Type">
          <select
            value={jobType}
            onChange={(e) => setJobType(e.target.value as JobType)}
            style={selectStyle}
          >
            {JOB_TYPES.map((t) => <option key={t} value={t}>{t}</option>)}
          </select>
        </Field>
        <Field label="Team / Folder">
          <select
            value={team}
            onChange={(e) => setTeam(e.target.value)}
            style={selectStyle}
          >
            <option value="">— select —</option>
            {TEAMS.map((t) => <option key={t} value={t}>{t}</option>)}
          </select>
        </Field>
        <Field label="Cron (min hour dom mon dow)">
          <Input value={cron} onChange={setCron} mono placeholder="0 3 * * *" />
        </Field>
        <Field label="Schedule enabled">
          <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 11, color: "var(--v2-text-secondary)" }}>
            <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
            {enabled ? "enabled" : "disabled"}
          </label>
        </Field>
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
          <Field label="Retries">
            <Input value={String(retries)} onChange={(v) => setRetries(Number(v) || 0)} mono />
          </Field>
          <Field label="Timeout (s)">
            <Input value={String(timeout)} onChange={(v) => setTimeoutS(Number(v) || 0)} mono />
          </Field>
        </div>
        <Field label="Dry run">
          <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 11, color: "var(--v2-text-secondary)" }}>
            <input type="checkbox" checked={dryRun} onChange={(e) => setDryRun(e.target.checked)} />
            log only, don't execute
          </label>
        </Field>

        {(definition.upstream?.length ?? 0) > 0 && (
          <Field label={`Upstream deps (${definition.upstream!.length})`}>
            <div style={{ fontFamily: "var(--v2-font-mono)", fontSize: 10, color: "var(--v2-text-secondary)", lineHeight: 1.6 }}>
              {definition.upstream!.map((u, idx) => (
                <div key={idx}>
                  <span style={{ color: "var(--v2-text-muted)" }}>{u.from}</span>
                  {" "}
                  <span style={{ color: "var(--v2-accent-brand)" }}>{u.condition}</span>
                </div>
              ))}
            </div>
          </Field>
        )}

        {err && (
          <div style={{ padding: 8, background: "rgba(239,68,68,.08)", border: "1px solid rgba(239,68,68,.3)", borderRadius: 3, color: "var(--v2-status-failed)", fontSize: 11, fontFamily: "var(--v2-font-mono)" }}>
            {err}
          </div>
        )}
      </div>

      {/* Actions */}
      <div
        style={{
          borderTop: "1px solid var(--v2-border-subtle)",
          padding: "8px 12px",
          display: "flex",
          gap: 6,
        }}
      >
        <button
          onClick={handleDelete}
          disabled={saving}
          style={{ ...btnStyle, borderColor: "rgba(239,68,68,.4)", color: "var(--v2-status-failed)" }}
        >
          {isNew ? "Cancel" : "Delete"}
        </button>
        <div style={{ flex: 1 }} />
        <button
          onClick={handleSave}
          disabled={saving}
          style={{ ...btnStyle, borderColor: "var(--v2-accent-brand)", color: "var(--v2-accent-brand)", fontWeight: 600 }}
        >
          {saving ? "…" : "Save"}
        </button>
      </div>
    </aside>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div style={{ fontSize: 9, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--v2-text-muted)", marginBottom: 4 }}>
        {label}
      </div>
      {children}
    </div>
  );
}

function Input({
  value, onChange, disabled, mono, placeholder,
}: { value: string; onChange: (v: string) => void; disabled?: boolean; mono?: boolean; placeholder?: string }) {
  return (
    <input
      value={value}
      onChange={(e) => onChange(e.target.value)}
      disabled={disabled}
      placeholder={placeholder}
      style={{
        width: "100%",
        background: "var(--v2-bg-canvas)",
        border: "1px solid var(--v2-border-subtle)",
        color: disabled ? "var(--v2-text-muted)" : "var(--v2-text-primary)",
        padding: "5px 8px",
        fontSize: 11,
        fontFamily: mono ? "var(--v2-font-mono)" : "var(--v2-font-sans)",
        borderRadius: 3,
        outline: "none",
        boxSizing: "border-box",
      }}
    />
  );
}

const selectStyle: React.CSSProperties = {
  width: "100%",
  background: "var(--v2-bg-canvas)",
  border: "1px solid var(--v2-border-subtle)",
  color: "var(--v2-text-primary)",
  padding: "5px 8px",
  fontSize: 11,
  fontFamily: "var(--v2-font-mono)",
  borderRadius: 3,
  outline: "none",
  boxSizing: "border-box",
};

const btnStyle: React.CSSProperties = {
  padding: "4px 10px",
  background: "transparent",
  border: "1px solid var(--v2-border-medium)",
  borderRadius: 3,
  fontSize: 10,
  fontFamily: "var(--v2-font-mono)",
  letterSpacing: "0.06em",
  textTransform: "uppercase",
  cursor: "pointer",
};
