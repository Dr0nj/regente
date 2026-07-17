// Bloco 2 — Control-M parity panel.
// Tabs: Calendars, Resources, SLA, Forecast, Analytics, Variables.
// (Conditions saiu daqui: o pool de condições vive no MONITORING, botão
// "Condições" ao lado do Organizar — ver ConditionsPanel.tsx.)
import { useEffect, useState } from "react";
import {
  listCalendars, saveCalendar, deleteCalendar, type Calendar,
  listResources, setResourceCapacity, deleteResource, type ResourceState,
  listSLABreaches, type SLABreach,
  getForecast, type ForecastReport,
  fetchSummary, fetchTopFailing, fetchMTTR,
  type AnalyticsSummary, type TopFailingItem, type MTTRItem,
  listVariables, putVariable, deleteVariable, type Variable,
} from "../lib/bloco2-api";

type Tab = "calendars" | "resources" | "sla" | "forecast" | "analytics" | "variables";

const TABS: { id: Tab; label: string }[] = [
  { id: "calendars", label: "Calendars" },
  { id: "resources", label: "Resources" },
  { id: "sla", label: "SLA" },
  { id: "forecast", label: "Forecast" },
  { id: "analytics", label: "Analytics" },
  { id: "variables", label: "Variables" },
];

export function ControlMPanel({ onClose }: { onClose: () => void }) {
  const [tab, setTab] = useState<Tab>("calendars");
  return (
    <div style={{
      position: "fixed", inset: 0, background: "rgba(0,0,0,0.6)", zIndex: 1000,
      display: "flex", alignItems: "center", justifyContent: "center",
    }}>
      <div className="v2-neon-card" style={{
        background: "var(--v2-bg-elevated)", color: "var(--v2-text-primary)", width: "min(1100px, 95vw)",
        height: "min(750px, 90vh)", display: "flex", flexDirection: "column",
      }}>
        <header style={{
          display: "flex", alignItems: "center", justifyContent: "space-between",
          padding: "12px 16px", borderBottom: "1px solid var(--v2-border-medium)",
        }}>
          <h2 style={{ margin: 0, fontSize: 15, fontWeight: 600 }}>Control Panel</h2>
          <button onClick={onClose} className="v2-dialog-x" aria-label="Fechar">✕</button>
        </header>
        <div style={{ display: "flex", gap: 6, padding: "8px 16px", borderBottom: "1px solid var(--v2-border-medium)" }}>
          {TABS.map(t => (
            <button key={t.id} onClick={() => setTab(t.id)} style={{
              background: tab === t.id ? "var(--v2-accent-brand)" : "transparent",
              color: tab === t.id ? "#000" : "var(--v2-text-secondary)",
              border: "1px solid " + (tab === t.id ? "var(--v2-accent-brand)" : "var(--v2-border-strong)"),
              padding: "6px 14px", borderRadius: 4, cursor: "pointer", fontSize: 13,
            }}>{t.label}</button>
          ))}
        </div>
        <div style={{ flex: 1, overflow: "auto", padding: 16 }}>
          {tab === "calendars" && <CalendarsView />}
          {tab === "resources" && <ResourcesView />}
          {tab === "sla" && <SLAView />}
          {tab === "forecast" && <ForecastView />}
          {tab === "analytics" && <AnalyticsView />}
          {tab === "variables" && <VariablesView />}
        </div>
      </div>
    </div>
  );
}

// === F14 ===
function CalendarsView() {
  const [items, setItems] = useState<Calendar[]>([]);
  const [editing, setEditing] = useState<Calendar | null>(null);
  const reload = () => listCalendars().then(setItems).catch(() => setItems([]));
  useEffect(() => { reload(); }, []);
  return (
    <div>
      <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 12 }}>
        <h3 style={{ margin: 0 }}>Calendars (F14)</h3>
        <button onClick={() => setEditing({ name: "", businessDays: ["Mon","Tue","Wed","Thu","Fri"], holidays: [], includeDates: [], excludeDates: [], timezone: "America/Sao_Paulo" })} style={btn}>+ New</button>
      </div>
      <table style={tbl}>
        <thead><tr><th style={th}>Name</th><th style={th}>Business Days</th><th style={th}>Holidays</th><th style={th}></th></tr></thead>
        <tbody>
          {items.length === 0 && <tr><td colSpan={4} style={{ ...td, textAlign: "center", color: "var(--v2-text-muted)" }}>No calendars yet.</td></tr>}
          {items.map(c => (
            <tr key={c.name}>
              <td style={td}>{c.name}</td>
              <td style={td}>{(c.businessDays || []).join(", ")}</td>
              <td style={td}>{(c.holidays || []).length}</td>
              <td style={td}>
                <button onClick={() => setEditing(c)} style={btnSm}>edit</button>{" "}
                <button onClick={() => deleteCalendar(c.name).then(reload)} style={btnDanger}>delete</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {editing && <CalendarEditor cal={editing} onSave={c => saveCalendar(c).then(() => { setEditing(null); reload(); })} onCancel={() => setEditing(null)} />}
    </div>
  );
}

// Calendar builder VISUAL (2026-06-12): grid mensal navegável. Clicar num dia
// cicla o estado: normal → feriado → incluir → excluir → normal. Dias úteis
// (businessDays) aparecem com fundo sutil. Salva holidays/include/exclude.
function CalendarEditor({ cal, onSave, onCancel }: { cal: Calendar; onSave: (c: Calendar) => void; onCancel: () => void }) {
  const [c, setC] = useState<Calendar>(cal);
  const today = new Date();
  const [view, setView] = useState<{ y: number; m: number }>({ y: today.getFullYear(), m: today.getMonth() });
  const days = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
  const dowFull = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];

  const has = (arr: string[] | undefined, iso: string) => (arr || []).includes(iso);
  const stateOf = (iso: string): "holiday" | "include" | "exclude" | "" => {
    if (has(c.excludeDates, iso)) return "exclude";
    if (has(c.includeDates, iso)) return "include";
    if (has(c.holidays, iso)) return "holiday";
    return "";
  };
  const cycle = (iso: string) => {
    const rm = (arr?: string[]) => (arr || []).filter((x) => x !== iso);
    const st = stateOf(iso);
    if (st === "") setC({ ...c, holidays: [...(c.holidays || []), iso] });
    else if (st === "holiday") setC({ ...c, holidays: rm(c.holidays), includeDates: [...(c.includeDates || []), iso] });
    else if (st === "include") setC({ ...c, includeDates: rm(c.includeDates), excludeDates: [...(c.excludeDates || []), iso] });
    else setC({ ...c, excludeDates: rm(c.excludeDates) });
  };

  const first = new Date(view.y, view.m, 1);
  const startPad = (first.getDay() + 6) % 7; // semana começa na segunda
  const daysInMonth = new Date(view.y, view.m + 1, 0).getDate();
  const cells: Array<number | null> = [...Array(startPad).fill(null), ...Array.from({ length: daysInMonth }, (_, i) => i + 1)];
  const monthLabel = first.toLocaleDateString("pt-BR", { month: "long", year: "numeric" });
  const colorFor = (st: string) => st === "exclude" ? { bg: "rgba(239,68,68,0.14)", fg: "var(--v2-status-failed)", br: "rgba(239,68,68,0.5)" }
    : st === "include" ? { bg: "var(--v2-accent-deep)", fg: "var(--v2-accent-brand)", br: "var(--v2-accent-dark)" }
    : st === "holiday" ? { bg: "rgba(245,158,11,0.14)", fg: "var(--v2-status-hold)", br: "rgba(245,158,11,0.45)" }
    : { bg: "transparent", fg: "var(--v2-text-secondary)", br: "var(--v2-border-medium)" };

  return (
    <div style={{ marginTop: 16, padding: 12, border: "1px solid var(--v2-border-strong)", borderRadius: 6 }}>
      <div style={{ marginBottom: 10 }}>
        <label style={lbl}>Name: </label>
        <input value={c.name} onChange={e => setC({ ...c, name: e.target.value })} style={inp} placeholder="ex: dias-uteis-br" />
      </div>
      <div style={{ marginBottom: 10 }}>
        <label style={lbl}>Dias úteis (semana): </label>
        {days.map(d => (
          <label key={d} style={{ marginRight: 8, fontSize: 12 }}>
            <input type="checkbox" checked={(c.businessDays || []).includes(d)} onChange={e => {
              const cur = c.businessDays || [];
              setC({ ...c, businessDays: e.target.checked ? [...cur, d] : cur.filter(x => x !== d) });
            }} /> {d}
          </label>
        ))}
      </div>

      {/* Grid mensal */}
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 6 }}>
        <button onClick={() => setView((v) => v.m === 0 ? { y: v.y - 1, m: 11 } : { y: v.y, m: v.m - 1 })} style={btnSm}>‹</button>
        <span style={{ fontSize: 13, fontWeight: 600, textTransform: "capitalize" }}>{monthLabel}</span>
        <button onClick={() => setView((v) => v.m === 11 ? { y: v.y + 1, m: 0 } : { y: v.y, m: v.m + 1 })} style={btnSm}>›</button>
      </div>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(7, 1fr)", gap: 3 }}>
        {["S","T","Q","Q","S","S","D"].map((d, i) => <div key={i} style={{ textAlign: "center", fontSize: 9, color: "var(--v2-text-muted)", fontFamily: "monospace" }}>{d}</div>)}
        {cells.map((day, i) => {
          if (day === null) return <div key={`p${i}`} />;
          const iso = `${view.y}-${String(view.m + 1).padStart(2, "0")}-${String(day).padStart(2, "0")}`;
          const dow = dowFull[new Date(view.y, view.m, day).getDay()];
          const isBiz = (c.businessDays || []).includes(dow);
          const st = stateOf(iso);
          const col = colorFor(st);
          return (
            <button key={iso} onClick={() => cycle(iso)} title={`${iso}${st ? ` — ${st}` : ""} (clique para alternar)`}
              style={{
                padding: "6px 0", fontSize: 11, cursor: "pointer", borderRadius: 3, fontFamily: "monospace",
                background: st ? col.bg : (isBiz ? "var(--v2-bg-hover)" : "transparent"),
                border: `1px solid ${col.br}`, color: col.fg, fontWeight: st ? 700 : 400,
              }}>{day}</button>
          );
        })}
      </div>
      <div style={{ display: "flex", gap: 12, marginTop: 8, fontSize: 10, color: "var(--v2-text-muted)" }}>
        <span><span style={{ color: "var(--v2-status-hold)" }}>■</span> feriado</span>
        <span><span style={{ color: "var(--v2-accent-brand)" }}>■</span> incluir</span>
        <span><span style={{ color: "var(--v2-status-failed)" }}>■</span> excluir</span>
        <span style={{ marginLeft: "auto" }}>clique cicla os estados</span>
      </div>

      <div style={{ marginTop: 12 }}>
        <button onClick={() => onSave(c)} style={btn} disabled={!c.name}>Save</button>{" "}
        <button onClick={onCancel} style={btnSm}>Cancel</button>
      </div>
    </div>
  );
}

// === F15 ===
function ResourcesView() {
  const [items, setItems] = useState<ResourceState[]>([]);
  const [name, setName] = useState("");
  const [cap, setCap] = useState(1);
  const reload = () => listResources().then(setItems).catch(() => setItems([]));
  useEffect(() => { reload(); const i = setInterval(reload, 5000); return () => clearInterval(i); }, []);
  return (
    <div>
      <h3 style={{ marginTop: 0 }}>Resources / Quotas (F15)</h3>
      <div style={{ display: "flex", gap: 8, marginBottom: 12 }}>
        <input placeholder="resource name" value={name} onChange={e => setName(e.target.value)} style={inp} />
        <input type="number" min={0} value={cap} onChange={e => setCap(parseInt(e.target.value || "0"))} style={{ ...inp, width: 80 }} />
        <button onClick={() => { if (name) setResourceCapacity(name, cap).then(() => { setName(""); reload(); }); }} style={btn}>Set capacity</button>
      </div>
      <table style={tbl}>
        <thead><tr><th style={th}>Name</th><th style={th}>Capacity</th><th style={th}>Used</th><th style={th}>Free</th><th style={th}></th></tr></thead>
        <tbody>
          {items.length === 0 && <tr><td colSpan={5} style={{ ...td, textAlign: "center", color: "var(--v2-text-muted)" }}>No resources yet.</td></tr>}
          {items.map(r => (
            <tr key={r.name}>
              <td style={td}>{r.name}</td>
              <td style={td}>{r.capacity}</td>
              <td style={td}>{r.used}</td>
              <td style={{ ...td, color: r.capacity - r.used <= 0 ? "var(--v2-status-failed)" : "var(--v2-status-ok)" }}>{r.capacity - r.used}</td>
              <td style={td}>
                <button
                  onClick={() => {
                    if (r.used > 0) { alert(`Cannot delete "${r.name}": in use (${r.used}).`); return; }
                    if (confirm(`Delete resource "${r.name}"?`)) deleteResource(r.name).then(reload).catch(e => alert(e.message || String(e)));
                  }}
                  style={btnDanger}
                  title={r.used > 0 ? "in use; release first" : "delete"}
                >delete</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// === F19 ===
function SLAView() {
  const [items, setItems] = useState<SLABreach[]>([]);
  useEffect(() => {
    listSLABreaches().then(setItems).catch(() => setItems([]));
    const i = setInterval(() => listSLABreaches().then(setItems).catch(() => {}), 10000);
    return () => clearInterval(i);
  }, []);
  return (
    <div>
      <h3 style={{ marginTop: 0 }}>SLA Breaches (F19)</h3>
      <table style={tbl}>
        <thead><tr><th style={th}>When</th><th style={th}>Definition</th><th style={th}>Instance</th><th style={th}>Kind</th><th style={th}>Severity</th><th style={th}>Message</th></tr></thead>
        <tbody>
          {items.length === 0 && <tr><td colSpan={6} style={{ ...td, textAlign: "center", color: "var(--v2-status-ok)" }}>No SLA breaches. ✓</td></tr>}
          {items.map(b => (
            <tr key={b.id}>
              <td style={td}>{new Date(b.detectedAt).toLocaleString()}</td>
              <td style={td}>{b.defId}</td>
              <td style={{ ...td, fontFamily: "monospace", fontSize: 11 }}>{b.instanceId.slice(0, 8)}</td>
              <td style={td}>{b.kind}</td>
              <td style={{ ...td, color: b.severity === "critical" ? "var(--v2-status-failed)" : "var(--v2-status-hold)" }}>{b.severity}</td>
              <td style={td}>{b.message}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// === F21 ===
function ForecastView() {
  const [date, setDate] = useState(new Date().toISOString().slice(0, 10));
  const [report, setReport] = useState<ForecastReport | null>(null);
  const run = () => getForecast(date).then(setReport).catch(() => setReport(null));
  useEffect(() => { run(); }, []);
  return (
    <div>
      <h3 style={{ marginTop: 0 }}>Forecast / Dry-run (F21)</h3>
      <div style={{ display: "flex", gap: 8, marginBottom: 12 }}>
        <input type="date" value={date} onChange={e => setDate(e.target.value)} style={inp} />
        <button onClick={run} style={btn}>Run forecast</button>
      </div>
      {report && (
        <>
          {report.peakResourceUsage && Object.keys(report.peakResourceUsage).length > 0 && (
            <div style={{ marginBottom: 12, padding: 8, background: "var(--v2-bg-elevated)", borderRadius: 4 }}>
              <strong>Peak resource usage:</strong>{" "}
              {Object.entries(report.peakResourceUsage).map(([k, v]) => `${k}: ${v}`).join(" · ")}
            </div>
          )}
          <table style={tbl}>
            <thead><tr><th style={th}>Wave</th><th style={th}>Definition</th><th style={th}>Team</th><th style={th}>Eligible</th><th style={th}>Start</th><th style={th}>End</th><th style={th}>SLA</th></tr></thead>
            <tbody>
              {report.jobs.map(j => (
                <tr key={j.defId}>
                  <td style={td}>{j.wave}</td>
                  <td style={td}>{j.label}</td>
                  <td style={td}>{j.team}</td>
                  <td style={{ ...td, color: j.eligible ? "var(--v2-status-ok)" : "var(--v2-text-muted)" }} title={j.reason}>{j.eligible ? "✓" : "skip"}</td>
                  <td style={td}>{new Date(j.startAt).toLocaleTimeString()}</td>
                  <td style={td}>{new Date(j.endAt).toLocaleTimeString()}</td>
                  <td style={{ ...td, color: j.wouldBreachSla ? "var(--v2-status-failed)" : "var(--v2-text-muted)" }}>{j.wouldBreachSla ? "BREACH" : "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
    </div>
  );
}

// === F22 ===
function AnalyticsView() {
  const [summary, setSummary] = useState<AnalyticsSummary | null>(null);
  const [top, setTop] = useState<TopFailingItem[]>([]);
  const [mttr, setMttr] = useState<MTTRItem[]>([]);
  useEffect(() => {
    fetchSummary().then(setSummary).catch(() => {});
    fetchTopFailing().then(r => setTop(r.topFailing)).catch(() => {});
    fetchMTTR().then(r => setMttr(r.mttr)).catch(() => {});
  }, []);
  return (
    <div>
      <h3 style={{ marginTop: 0 }}>Analytics (F22)</h3>
      {summary && (
        <div style={{ marginBottom: 16, padding: 12, background: "var(--v2-bg-elevated)", borderRadius: 4 }}>
          <div style={{ marginBottom: 8, fontSize: 12, color: "var(--v2-text-muted)" }}>Last 7 days (since {summary.since})</div>
          <div style={{ display: "flex", gap: 16 }}>
            {Object.entries(summary.counts).map(([s, n]) => (
              <div key={s} style={{ textAlign: "center", padding: "8px 16px", background: "var(--v2-bg-surface)", borderRadius: 4 }}>
                <div style={{ fontSize: 22, fontWeight: 600 }}>{n}</div>
                <div style={{ fontSize: 11, color: "var(--v2-text-secondary)" }}>{s}</div>
              </div>
            ))}
          </div>
        </div>
      )}
      <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16 }}>
        <div>
          <h4 style={{ marginBottom: 8 }}>Top failing (last 30d)</h4>
          <table style={tbl}>
            <thead><tr><th style={th}>Definition</th><th style={th}>Fails</th></tr></thead>
            <tbody>
              {top.length === 0 && <tr><td colSpan={2} style={{ ...td, textAlign: "center", color: "var(--v2-text-muted)" }}>No failures.</td></tr>}
              {top.map(t => <tr key={t.defId}><td style={td}>{t.defId}</td><td style={td}>{t.fails}</td></tr>)}
            </tbody>
          </table>
        </div>
        <div>
          <h4 style={{ marginBottom: 8 }}>MTTR (last 30d, top by avg duration)</h4>
          <table style={tbl}>
            <thead><tr><th style={th}>Definition</th><th style={th}>Avg (s)</th><th style={th}>Runs</th></tr></thead>
            <tbody>
              {mttr.length === 0 && <tr><td colSpan={3} style={{ ...td, textAlign: "center", color: "var(--v2-text-muted)" }}>No data.</td></tr>}
              {mttr.map(m => <tr key={m.defId}><td style={td}>{m.defId}</td><td style={td}>{Math.round(m.avgSec)}</td><td style={td}>{m.runs}</td></tr>)}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

// === styles ===
// === F18 ===
function VariablesView() {
  const [items, setItems] = useState<Variable[]>([]);
  const [name, setName] = useState("");
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const reload = () => listVariables().then(setItems).catch(() => setItems([]));
  useEffect(() => { reload(); const i = setInterval(reload, 5000); return () => clearInterval(i); }, []);
  const submit = () => {
    if (!name) return;
    setBusy(true);
    putVariable(name, value).then(() => { setName(""); setValue(""); reload(); }).catch(e => alert(e.message || String(e))).finally(() => setBusy(false));
  };
  return (
    <div>
      <h3 style={{ marginTop: 0 }}>Global Variables (F18)</h3>
      <div style={{ fontSize: 12, color: "var(--v2-text-muted)", marginBottom: 12 }}>
        Globals injected as <code>{"${var.NAME}"}</code> on dispatch (params interpolation). Folder/job scopes not yet exposed.
      </div>
      <div style={{ display: "flex", gap: 8, marginBottom: 12, flexWrap: "wrap" }}>
        <input placeholder="VAR_NAME" value={name} onChange={e => setName(e.target.value)} style={inp} />
        <input placeholder="value" value={value} onChange={e => setValue(e.target.value)} style={{ ...inp, minWidth: 240 }} />
        <button onClick={submit} disabled={!name || busy} style={btn}>Set</button>
      </div>
      <table style={tbl}>
        <thead><tr><th style={th}>Name</th><th style={th}>Value</th><th style={th}>Updated</th><th style={th}>By</th><th style={th}></th></tr></thead>
        <tbody>
          {items.length === 0 && <tr><td colSpan={5} style={{ ...td, textAlign: "center", color: "var(--v2-text-muted)" }}>No variables yet.</td></tr>}
          {items.map(v => (
            <tr key={v.name}>
              <td style={td}><code>{v.name}</code></td>
              <td style={{ ...td, fontFamily: "monospace", color: "var(--v2-status-running)" }}>{v.value}</td>
              <td style={{ ...td, color: "var(--v2-text-muted)", fontSize: 11 }}>{new Date(v.updatedAt).toLocaleString()}</td>
              <td style={{ ...td, color: "var(--v2-text-muted)" }}>{v.updatedBy || "—"}</td>
              <td style={td}>
                <button onClick={() => { setName(v.name); setValue(v.value); }} style={btnSm}>edit</button>{" "}
                <button onClick={() => { if (confirm(`Delete variable "${v.name}"?`)) deleteVariable(v.name).then(reload); }} style={btnDanger}>delete</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

const tbl: React.CSSProperties = { width: "100%", borderCollapse: "collapse", fontSize: 13 };
const th: React.CSSProperties = { textAlign: "left", padding: "6px 10px", borderBottom: "1px solid var(--v2-border-medium)", color: "var(--v2-text-secondary)", fontWeight: 500 };
const td: React.CSSProperties = { padding: "6px 10px", borderBottom: "1px solid var(--v2-border-subtle)" };
const btn: React.CSSProperties = { background: "var(--v2-accent-brand)", color: "#000", border: "none", padding: "6px 14px", borderRadius: 4, cursor: "pointer", fontSize: 13 };
const btnSm: React.CSSProperties = { background: "transparent", color: "var(--v2-text-secondary)", border: "1px solid var(--v2-border-strong)", padding: "3px 10px", borderRadius: 4, cursor: "pointer", fontSize: 12 };
const btnDanger: React.CSSProperties = { background: "transparent", color: "var(--v2-status-failed)", border: "1px solid var(--v2-status-failed)", padding: "3px 10px", borderRadius: 4, cursor: "pointer", fontSize: 12 };
const inp: React.CSSProperties = { background: "var(--v2-bg-elevated)", color: "var(--v2-text-primary)", border: "1px solid var(--v2-border-medium)", padding: "5px 8px", borderRadius: 4, fontSize: 13 };
const lbl: React.CSSProperties = { display: "inline-block", marginRight: 8, fontSize: 12, color: "var(--v2-text-secondary)" };
