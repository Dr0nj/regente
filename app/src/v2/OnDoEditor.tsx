/**
 * OnDoEditor — editor visual das regras "On/Do" (Control-M On-Do) de um job.
 *
 * Cada regra: ON <gatilho> DO <ação>. O motor backend (scheduler/actions.go) já
 * existe e faz round-trip via domain.ActionRule; aqui só expomos a config.
 *
 *  Gatilho (on):
 *    result  → status terminal (OK|NOTOK), após esgotar retries
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
  { id: "result", label: "Terminar (OK/NOTOK)" },
  { id: "attempt", label: "Falhar na tentativa N" },
  { id: "runtime", label: "Rodar mais que N min" },
];

const DO_OPTS: Array<{ id: ActionRule["do"]; label: string }> = [
  { id: "notify", label: "Notificar" },
  { id: "set-condition", label: "Setar condition" },
  { id: "run-job", label: "Rodar outro job" },
  { id: "set-ok", label: "Marcar como OK (auto-heal)" },
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
  { id: "email", label: "E-mail" },
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

/** Tradução natural de uma regra (gatilho → ação). */
// eslint-disable-next-line react-refresh/only-export-components -- helper público `describeRule` convive com o componente no mesmo módulo; mover = churn sem ganho; ver roadmap §RH
export function describeRule(r: ActionRule, jobLabel: (id: string) => string): string {
  let when: string;
  switch (r.on) {
    case "result":
      when = `Quando o job terminar ${r.status === "OK" ? "OK" : "NOTOK"}`;
      break;
    case "attempt":
      when = `Quando a tentativa ${r.attempt || "?"} falhar`;
      break;
    case "runtime":
      when = `Quando o job passar de ${r.afterMin || "?"} min rodando`;
      break;
    default:
      when = "Quando";
  }
  let then: string;
  switch (r.do) {
    case "notify":
      then = `notifica${r.channels?.length ? " via " + r.channels.join("/") : ""}${r.severity ? ` (${r.severity})` : ""}`;
      break;
    case "set-condition":
      then = `seta a condition "${r.condition || "?"}"`;
      break;
    case "run-job":
      then = `roda o job "${r.targetJob ? jobLabel(r.targetJob) : "?"}"`;
      break;
    case "set-ok":
      then = "marca este job como OK (auto-heal)";
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
        Regras <b>On/Do</b>: reações automáticas ao ciclo do job. Cada regra dispara no
        máximo uma vez por execução.
      </Hint>

      {rules.length === 0 && (
        <div style={{ fontSize: 11, color: "var(--v2-text-muted)", padding: "10px 0", lineHeight: 1.5 }}>
          Nenhuma regra. Ex.: <i>quando terminar NOTOK, notificar no Slack</i>; ou{" "}
          <i>quando passar de 30 min rodando, abrir alerta crítico</i>.
        </div>
      )}

      {rules.map((r, i) => (
        <div key={i} style={card}>
          <div style={{ display: "flex", alignItems: "center", gap: 6, marginBottom: 8 }}>
            <Zap size={12} style={{ color: "var(--v2-accent-brand)" }} />
            <span style={{ fontSize: 9, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--v2-text-muted)" }}>Regra {i + 1}</span>
            <button onClick={() => remove(i)} title="Remover regra" style={{ ...iconBtn, marginLeft: "auto" }}><Trash2 size={12} /></button>
          </div>

          {/* ── ON (gatilho) ── */}
          <Row label="Quando">
            <select value={r.on} onChange={(e) => update(i, { on: e.target.value as ActionRule["on"] })} style={selectStyle}>
              {ON_OPTS.map((o) => <option key={o.id} value={o.id}>{o.label}</option>)}
            </select>
          </Row>
          {r.on === "result" && (
            <Row label="Status">
              <select value={r.status ?? "NOTOK"} onChange={(e) => update(i, { status: e.target.value as ActionRule["status"] })} style={selectStyle}>
                <option value="NOTOK">NOTOK (falhou)</option>
                <option value="OK">OK (sucesso)</option>
              </select>
            </Row>
          )}
          {r.on === "attempt" && (
            <Row label="Tentativa nº">
              <input type="number" min={1} value={r.attempt ?? 1} onChange={(e) => update(i, { attempt: Number(e.target.value) || 1 })} style={inputStyle} />
            </Row>
          )}
          {r.on === "runtime" && (
            <Row label="Após (min)">
              <input type="number" min={1} value={r.afterMin ?? 30} onChange={(e) => update(i, { afterMin: Number(e.target.value) || 1 })} style={inputStyle} />
            </Row>
          )}

          {/* ── DO (ação) ── */}
          <Row label="Fazer">
            <select value={r.do} onChange={(e) => update(i, { do: e.target.value as ActionRule["do"] })} style={selectStyle}>
              {DO_OPTS.map((o) => <option key={o.id} value={o.id}>{o.label}</option>)}
            </select>
          </Row>

          {r.do === "notify" && (
            <>
              <Row label="Severidade">
                <select value={r.severity ?? "warning"} onChange={(e) => update(i, { severity: e.target.value as ActionRule["severity"] })} style={selectStyle}>
                  {SEVERITIES.map((s) => <option key={s.id} value={s.id}>{s.label}</option>)}
                </select>
              </Row>
              <Row label="Mensagem">
                <input value={r.message ?? ""} placeholder="(padrão automático)" onChange={(e) => update(i, { message: e.target.value })} style={inputStyle} />
              </Row>
              <div>
                <div style={fieldLabel}>Canais</div>
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
                  Vazio = todos os canais configurados em Alertas.
                </div>
              </div>
            </>
          )}

          {r.do === "set-condition" && (
            <>
              <Row label="Condition">
                <input value={r.condition ?? ""} placeholder="ex.: BILLING_DONE" list="ondo-known-conditions"
                  onChange={(e) => update(i, { condition: e.target.value })} style={inputStyle} />
              </Row>
              <datalist id="ondo-known-conditions">
                {knownConditions.map((k) => <option key={k} value={k} />)}
              </datalist>
              <Hint>
                Para outro job ESPERAR por ela: no job de destino, aba <b>Dependências → Conditions
                → Entrada</b>, com o MESMO nome. O vínculo é pelo nome exato.
              </Hint>
            </>
          )}

          {r.do === "run-job" && (
            <Row label="Job alvo">
              <select value={r.targetJob ?? ""} onChange={(e) => update(i, { targetJob: e.target.value })} style={selectStyle}>
                <option value="">— selecione —</option>
                {allDefs.filter((d) => d.id !== selfId).map((d) => (
                  <option key={d.id} value={d.id}>{d.label} ({d.id})</option>
                ))}
              </select>
            </Row>
          )}

          {r.do === "set-ok" && (
            <Hint>Flipa o próprio status NOTOK→OK. Use com <b>Quando terminar NOTOK</b>.</Hint>
          )}

          {/* Tradução natural */}
          <div style={{ marginTop: 8, padding: "6px 8px", background: "var(--v2-bg-elevated)", borderRadius: 3, fontSize: 10.5, color: "var(--v2-text-secondary)", lineHeight: 1.4 }}>
            {describeRule(r, labelOf)}
          </div>
        </div>
      ))}

      <button onClick={add} style={{ ...chipBtn, alignSelf: "flex-start", borderColor: "var(--v2-accent-brand)", color: "var(--v2-accent-brand)", padding: "5px 10px" }}>
        <Plus size={12} /> Adicionar regra
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
