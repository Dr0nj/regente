/**
 * OnDoEditor — editor visual das regras "On/Do" (Control-M On-Do) de um job.
 *
 * Cada regra: ON <gatilho> DO <ação>. O motor backend (scheduler/actions.go) já
 * existe e faz round-trip via domain.ActionRule; aqui só expomos a config.
 *
 *  Gatilho (on):
 *    result  → status terminal (OK|NOTOK), após esgotar retries
 *    exit    → o exit code terminal casa com a espec ("1,2,3" · "1-4" · ">0")
 *    attempt → a N-ésima tentativa FALHOU
 *    runtime → RUNNING há mais que N minutos
 *  Ação (do):
 *    notify        → alerta (mensagem/severidade/canais)
 *    set-condition → seta condition global no escopo do dia
 *    run-job       → Force Order de outro job
 *    set-ok        → auto-heal: flipa o PRÓPRIO NOTOK→OK
 *
 * Cada regra mostra uma TRADUÇÃO em linguagem natural (describeRule) — o usuário
 * lê o que vai acontecer sem decorar o YAML.
 */
import { Plus, Trash2, Zap } from "lucide-react";
import type { ActionRule, JobDefinition } from "@/lib/orchestrator-model";

const ON_OPTS: Array<{ id: ActionRule["on"]; label: string }> = [
  { id: "result", label: "Ends (OK/NOTOK)" },
  { id: "exit", label: "Ends with exit code X" },
  { id: "attempt", label: "Attempt N fails" },
  { id: "runtime", label: "Runs longer than N min" },
];

const DO_OPTS: Array<{ id: ActionRule["do"]; label: string }> = [
  { id: "notify", label: "Notify" },
  { id: "set-condition", label: "Set condition" },
  { id: "run-job", label: "Run another job" },
  { id: "set-ok", label: "Mark as OK (auto-heal)" },
];

const SEVERITIES: Array<{ id: NonNullable<ActionRule["severity"]>; label: string }> = [
  { id: "info", label: "info" },
  { id: "warning", label: "warning" },
  { id: "critical", label: "critical" },
];

const CHANNELS: Array<{ id: string; label: string }> = [
  { id: "toast", label: "in-app" },
  { id: "slack", label: "Slack" },
  { id: "webhook", label: "Webhook" },
  { id: "email", label: "Email" },
  { id: "pagerduty", label: "PagerDuty" },
];

interface Props {
  value: ActionRule[];
  onChange: (next: ActionRule[]) => void;
  /** Defs do escopo — para o seletor de job-alvo do run-job. */
  allDefs: JobDefinition[];
  /** ID do próprio job — destacar "este job" e evitar auto-referência no run-job. */
  selfId: string;
}

/** Tradução natural da espec de exit codes ("1,2,3" → "1, 2 ou 3"). */
function describeExitSpec(spec?: string): string {
  const toks = (spec ?? "").split(",").map((t) => t.trim()).filter(Boolean);
  if (!toks.length) return "?";
  const parts = toks.map((t) => {
    const cmp = /^(>=|<=|!=|>|<)\s*(-?\d+)$/.exec(t);
    if (cmp) {
      const word = { ">=": "≥", "<=": "≤", "!=": "other than", ">": "greater than", "<": "less than" }[cmp[1]]!;
      return `${word} ${cmp[2]}`;
    }
    const range = /^(-?\d+)\s*-\s*(-?\d+)$/.exec(t);
    if (range) return `between ${range[1]} and ${range[2]}`;
    return t;
  });
  return parts.length === 1 ? parts[0] : `${parts.slice(0, -1).join(", ")} or ${parts[parts.length - 1]}`;
}

/** Tradução natural de uma regra (gatilho → ação). */
// eslint-disable-next-line react-refresh/only-export-components -- helper público `describeRule` convive com o componente no mesmo módulo; mover = churn sem ganho; ver roadmap §RH
export function describeRule(r: ActionRule, jobLabel: (id: string) => string): string {
  let when: string;
  switch (r.on) {
    case "result":
      when = `When the job ends ${r.status === "OK" ? "OK" : "NOTOK"}`;
      break;
    case "exit":
      when = `When the job ends with exit code ${describeExitSpec(r.exitCodes)}`;
      break;
    case "attempt":
      when = `When attempt ${r.attempt || "?"} fails`;
      break;
    case "runtime":
      when = `When the job runs longer than ${r.afterMin || "?"} min`;
      break;
    default:
      when = "When";
  }
  let then: string;
  switch (r.do) {
    case "notify":
      then = `notify${r.channels?.length ? " through " + r.channels.join("/") : ""}${r.severity ? ` (${r.severity})` : ""}`;
      break;
    case "set-condition":
      then = `set the condition "${r.condition || "?"}"`;
      break;
    case "run-job":
      then = `run the job "${r.targetJob ? jobLabel(r.targetJob) : "?"}"`;
      break;
    case "set-ok":
      then = "mark this job as OK (auto-heal)";
      break;
    default:
      then = "…";
  }
  return `${when}, ${then}.`;
}

/** Regra nova padrão (gatilho mais comum: terminou NOTOK → notifica). */
function newRule(): ActionRule {
  return { on: "result", status: "NOTOK", do: "notify", severity: "warning" };
}

export default function OnDoEditor({ value, onChange, allDefs, selfId }: Props) {
  const rules = value ?? [];
  const labelOf = (id: string) => allDefs.find((d) => d.id === id)?.label ?? id;

  // Vocabulário de conditions do escopo (mesma colheita do JobConfigDrawer):
  // sugere nomes já usados pra cravar o vínculo produtor→consumidor sem typo.
  const knownConditions = [...new Set(
    allDefs.flatMap((d) => [
      ...(d.conditionsIn ?? []),
      ...(d.conditionsOutAdd ?? []),
      ...(d.conditionsOutRemove ?? []),
      ...(d.actions ?? []).filter((a) => a.do === "set-condition" && a.condition).map((a) => a.condition as string),
    ]),
  )].sort();

  const update = (i: number, patch: Partial<ActionRule>) =>
    onChange(rules.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
  const remove = (i: number) => onChange(rules.filter((_, idx) => idx !== i));
  const add = () => onChange([...rules, newRule()]);

  const toggleChannel = (i: number, ch: string) => {
    const cur = new Set(rules[i].channels ?? []);
    if (cur.has(ch)) cur.delete(ch); else cur.add(ch);
    update(i, { channels: [...cur] });
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
      <Hint>
        <b>On/Do</b> rules: automatic reactions to the job lifecycle. Each rule fires at
        most once per run.
      </Hint>

      {rules.length === 0 && (
        <div style={{ fontSize: 11, color: "var(--v2-text-muted)", padding: "10px 0", lineHeight: 1.5 }}>
          No rule yet. E.g.: <i>when it ends NOTOK, notify on Slack</i>;{" "}
          <i>when it ends with exit code 1, 2 or 3, mark as OK</i>; or{" "}
          <i>when it runs longer than 30 min, raise a critical alert</i>.
        </div>
      )}

      {rules.map((r, i) => (
        <div key={i} style={card}>
          <div style={{ display: "flex", alignItems: "center", gap: 6, marginBottom: 8 }}>
            <Zap size={12} style={{ color: "var(--v2-accent-brand)" }} />
            <span style={{ fontSize: 9, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--v2-text-muted)" }}>Rule {i + 1}</span>
            <button onClick={() => remove(i)} title="Remove rule" style={{ ...iconBtn, marginLeft: "auto" }}><Trash2 size={12} /></button>
          </div>

          {/* ── ON (gatilho) ── */}
          <Row label="When">
            <select value={r.on} onChange={(e) => update(i, { on: e.target.value as ActionRule["on"] })} style={selectStyle}>
              {ON_OPTS.map((o) => <option key={o.id} value={o.id}>{o.label}</option>)}
            </select>
          </Row>
          {r.on === "result" && (
            <Row label="Status">
              <select value={r.status ?? "NOTOK"} onChange={(e) => update(i, { status: e.target.value as ActionRule["status"] })} style={selectStyle}>
                <option value="NOTOK">NOTOK (failed)</option>
                <option value="OK">OK (success)</option>
              </select>
            </Row>
          )}
          {r.on === "exit" && (
            <>
              <Row label="Exit codes">
                <input value={r.exitCodes ?? ""} placeholder="e.g. 1,2,3" inputMode="text"
                  onChange={(e) => update(i, { exitCodes: e.target.value })} style={inputStyle} />
              </Row>
              <Hint>
                Comma-separated list. Each item is a value (<code>3</code>), a range
                (<code>1-4</code>) or a comparison (<code>&gt;0</code>, <code>!=0</code>).
                It matches if ANY item matches; empty never fires.<br />
                The status comes from the code (<code>exit≠0 ⇒ NOTOK</code>) — to treat codes
                as success, pair it with <b>Mark as OK</b>. Cancelling a RUNNING job
                records <code>-1</code>.
              </Hint>
            </>
          )}
          {r.on === "attempt" && (
            <Row label="Attempt #">
              <input type="number" min={1} value={r.attempt ?? 1} onChange={(e) => update(i, { attempt: Number(e.target.value) || 1 })} style={inputStyle} />
            </Row>
          )}
          {r.on === "runtime" && (
            <Row label="After (min)">
              <input type="number" min={1} value={r.afterMin ?? 30} onChange={(e) => update(i, { afterMin: Number(e.target.value) || 1 })} style={inputStyle} />
            </Row>
          )}

          {/* ── DO (ação) ── */}
          <Row label="Do">
            <select value={r.do} onChange={(e) => update(i, { do: e.target.value as ActionRule["do"] })} style={selectStyle}>
              {DO_OPTS.map((o) => <option key={o.id} value={o.id}>{o.label}</option>)}
            </select>
          </Row>

          {r.do === "notify" && (
            <>
              <Row label="Severity">
                <select value={r.severity ?? "warning"} onChange={(e) => update(i, { severity: e.target.value as ActionRule["severity"] })} style={selectStyle}>
                  {SEVERITIES.map((s) => <option key={s.id} value={s.id}>{s.label}</option>)}
                </select>
              </Row>
              <Row label="Message">
                <input value={r.message ?? ""} placeholder="(automatic default)" onChange={(e) => update(i, { message: e.target.value })} style={inputStyle} />
              </Row>
              <div>
                <div style={fieldLabel}>Channels</div>
                <div style={{ display: "flex", flexWrap: "wrap", gap: 5 }}>
                  {CHANNELS.map((c) => {
                    const on = (r.channels ?? []).includes(c.id);
                    return (
                      <button key={c.id} onClick={() => toggleChannel(i, c.id)} style={{
                        ...chipBtn,
                        borderColor: on ? "var(--v2-accent-brand)" : "var(--v2-border-medium)",
                        color: on ? "var(--v2-accent-brand)" : "var(--v2-text-muted)",
                        background: on ? "var(--v2-accent-faint)" : "transparent",
                      }}>{c.label}</button>
                    );
                  })}
                </div>
                <div style={{ fontSize: 9.5, color: "var(--v2-text-muted)", marginTop: 4 }}>
                  Empty = every channel configured in Alerts.
                </div>
              </div>
            </>
          )}

          {r.do === "set-condition" && (
            <>
              <Row label="Condition">
                <input value={r.condition ?? ""} placeholder="e.g. BILLING_DONE" list="ondo-known-conditions"
                  onChange={(e) => update(i, { condition: e.target.value })} style={inputStyle} />
              </Row>
              <datalist id="ondo-known-conditions">
                {knownConditions.map((k) => <option key={k} value={k} />)}
              </datalist>
              <Hint>
                For another job to WAIT on it: on the target job, tab <b>Conditions → In</b>,
                using the SAME name. The link is made by the exact name.
              </Hint>
            </>
          )}

          {r.do === "run-job" && (
            <Row label="Target job">
              <select value={r.targetJob ?? ""} onChange={(e) => update(i, { targetJob: e.target.value })} style={selectStyle}>
                <option value="">— select —</option>
                {allDefs.filter((d) => d.id !== selfId).map((d) => (
                  <option key={d.id} value={d.id}>{d.label} ({d.id})</option>
                ))}
              </select>
            </Row>
          )}

          {r.do === "set-ok" && (
            <Hint>
              Flips this job's own status from NOTOK to OK. Use it with <b>Ends NOTOK</b> or,
              to tolerate specific codes, with <b>Ends with exit code X</b>.
            </Hint>
          )}

          {/* Tradução natural */}
          <div style={{ marginTop: 8, padding: "6px 8px", background: "var(--v2-bg-elevated)", borderRadius: 3, fontSize: 10.5, color: "var(--v2-text-secondary)", lineHeight: 1.4 }}>
            {describeRule(r, labelOf)}
          </div>
        </div>
      ))}

      <button onClick={add} style={{ ...chipBtn, alignSelf: "flex-start", borderColor: "var(--v2-accent-brand)", color: "var(--v2-accent-brand)", padding: "5px 10px" }}>
        <Plus size={12} /> Add rule
      </button>
    </div>
  );
}

/* ── primitivos ── */
function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ display: "grid", gridTemplateColumns: "84px 1fr", alignItems: "center", gap: 8, marginBottom: 7 }}>
      <span style={fieldLabel}>{label}</span>
      {children}
    </div>
  );
}
function Hint({ children }: { children: React.ReactNode }) {
  return <div style={{ fontSize: 10.5, color: "var(--v2-text-muted)", lineHeight: 1.45 }}>{children}</div>;
}

const fieldLabel: React.CSSProperties = { fontSize: 9, letterSpacing: "0.06em", textTransform: "uppercase", color: "var(--v2-text-muted)", marginBottom: 4 };
const inputStyle: React.CSSProperties = {
  width: "100%", background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)",
  color: "var(--v2-text-primary)", padding: "5px 8px", fontSize: 11, fontFamily: "var(--v2-font-mono)",
  borderRadius: 3, outline: "none", boxSizing: "border-box",
};
const selectStyle: React.CSSProperties = { ...inputStyle };
const card: React.CSSProperties = {
  background: "var(--v2-bg-surface)", border: "1px solid var(--v2-border-subtle)",
  borderRadius: 4, padding: "10px",
};
const chipBtn: React.CSSProperties = {
  display: "inline-flex", alignItems: "center", gap: 4, padding: "3px 8px",
  background: "transparent", border: "1px solid var(--v2-border-medium)", borderRadius: 3,
  fontSize: 10, fontFamily: "var(--v2-font-mono)", cursor: "pointer",
};
const iconBtn: React.CSSProperties = { background: "transparent", border: "none", color: "var(--v2-text-muted)", cursor: "pointer", padding: 2, display: "inline-flex" };
