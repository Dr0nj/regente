import { useMemo, useState } from "react";
import type { JobDefinition } from "@/lib/orchestrator-model";
import { runWhatIf, type WhatIfChange, type WhatIfReport, type WhatIfRow } from "@/lib/differentials-api";

/* ──────────────────────────────────────────────────────────────
   WhatIfPanel — ADV-3 What-If (simulação de cenário)
   ──────────────────────────────────────────────────────────────
   "E se o job X atrasar 40min / demorar o dobro / FALHAR / não
   rodar?" — monta um cenário (1..N mutações), POST /api/whatif e
   mostra o impacto downstream: baseline → cenário por job, atraso,
   bloqueios (deps impossíveis) e SLAs que passam a estourar.
   Simulação read-only no server (durações reais p50 do histórico);
   nada é materializado. Overlay no mesmo shell da Timeline (D-12).
   ────────────────────────────────────────────────────────────── */

const STATE_LABEL: Record<WhatIfRow["state"], { label: string; color: string }> = {
  unchanged:        { label: "—",            color: "var(--v2-text-muted)" },
  delayed:          { label: "ATRASA",       color: "var(--v2-status-waiting)" },
  earlier:          { label: "ADIANTA",      color: "var(--v2-status-ok)" },
  blocked:          { label: "BLOQUEIA",     color: "var(--v2-status-failed)" },
  skipped:          { label: "NÃO RODA",     color: "var(--v2-text-muted)" },
  fails:            { label: "FALHA",        color: "var(--v2-status-failed)" },
  "starts-running": { label: "PASSA A RODAR", color: "var(--v2-status-running)" },
  "not-run":        { label: "—",            color: "var(--v2-text-muted)" },
};

function fmtDelta(ms: number): string {
  if (ms === 0) return "0";
  const sign = ms > 0 ? "+" : "−";
  const abs = Math.abs(ms);
  if (abs >= 3_600_000) return `${sign}${(abs / 3_600_000).toFixed(1)}h`;
  if (abs >= 60_000) return `${sign}${Math.round(abs / 60_000)}min`;
  return `${sign}${Math.round(abs / 1000)}s`;
}

function fmtHM(iso?: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
}

const inputStyle: React.CSSProperties = {
  background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)",
  color: "var(--v2-text-primary)", padding: "4px 7px", fontSize: 11,
  fontFamily: "var(--v2-font-mono)", borderRadius: 3, boxSizing: "border-box",
};

export default function WhatIfPanel({
  defs,
  onClose,
}: {
  defs: JobDefinition[];
  onClose: () => void;
}) {
  const sortedDefs = useMemo(
    () => [...defs].sort((a, b) => (a.team + a.label).localeCompare(b.team + b.label)),
    [defs],
  );
  const [defId, setDefId] = useState(sortedDefs[0]?.id ?? "");
  const [delayMin, setDelayMin] = useState("");
  const [durationMin, setDurationMin] = useState("");
  const [fail, setFail] = useState(false);
  const [skip, setSkip] = useState(false);
  const [changes, setChanges] = useState<WhatIfChange[]>([]);
  const [report, setReport] = useState<WhatIfReport | null>(null);
  const [running, setRunning] = useState(false);
  const [ranEmpty, setRanEmpty] = useState(false);

  const stageChange = (): WhatIfChange | null => {
    const c: WhatIfChange = { defId };
    if (skip) c.skip = true;
    if (fail) c.fail = true;
    if (Number(delayMin) > 0) c.delayMin = Number(delayMin);
    if (Number(durationMin) > 0) c.durationMs = Number(durationMin) * 60_000;
    if (!c.skip && !c.fail && !c.delayMin && !c.durationMs) return null;
    return c;
  };

  const run = async (extra?: WhatIfChange) => {
    const all = extra ? [...changes.filter((c) => c.defId !== extra.defId), extra] : changes;
    if (all.length === 0) { setRanEmpty(true); return; }
    setRanEmpty(false);
    setChanges(all);
    setRunning(true);
    const out = await runWhatIf(all);
    setRunning(false);
    setReport(out);
  };

  const impacted = report?.rows.filter((r) => r.impacted) ?? [];
  const untouched = (report?.summary.total ?? 0) - impacted.length;

  return (
    <div
      className="v2-grain"
      style={{
        position: "absolute", inset: 10, zIndex: 20,
        background: "var(--v2-bg-surface)", border: "1px solid var(--v2-border-medium)",
        borderRadius: 16, boxShadow: "0 10px 30px rgba(0,0,0,0.45)",
        display: "flex", flexDirection: "column", overflow: "hidden",
        fontFamily: "var(--v2-font-sans)", color: "var(--v2-text-primary)",
      }}
    >
      {/* header */}
      <div style={{
        display: "flex", alignItems: "center", gap: 10, padding: "10px 14px",
        borderBottom: "1px solid var(--v2-border-subtle)",
      }}>
        <span style={{ fontSize: 12, fontWeight: 600, letterSpacing: "0.06em", textTransform: "uppercase" }}>
          What-If — simulação de cenário
        </span>
        <span style={{ fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)" }}>
          read-only · durações reais (p50 do histórico) · nada é materializado
        </span>
        <button
          onClick={onClose}
          style={{
            marginLeft: "auto", background: "transparent", border: "1px solid var(--v2-border-medium)",
            color: "var(--v2-text-secondary)", borderRadius: 3, padding: "3px 10px",
            fontSize: 10, fontFamily: "var(--v2-font-mono)", cursor: "pointer",
          }}
        >
          fechar
        </button>
      </div>

      {/* builder do cenário */}
      <div style={{
        display: "flex", alignItems: "center", gap: 8, padding: "10px 14px", flexWrap: "wrap",
        borderBottom: "1px solid var(--v2-border-subtle)", background: "var(--v2-bg-canvas)",
      }}>
        <span style={{ fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)", textTransform: "uppercase", letterSpacing: "0.06em" }}>E se</span>
        <select value={defId} onChange={(e) => setDefId(e.target.value)} style={{ ...inputStyle, maxWidth: 260 }}>
          {sortedDefs.map((d) => (
            <option key={d.id} value={d.id}>{d.team} / {d.label}</option>
          ))}
        </select>
        <label style={{ display: "flex", alignItems: "center", gap: 4, fontSize: 11 }}>
          atrasar
          <input value={delayMin} onChange={(e) => setDelayMin(e.target.value.replace(/\D/g, ""))} placeholder="0" style={{ ...inputStyle, width: 52 }} />
          min
        </label>
        <label style={{ display: "flex", alignItems: "center", gap: 4, fontSize: 11 }}>
          durar
          <input value={durationMin} onChange={(e) => setDurationMin(e.target.value.replace(/\D/g, ""))} placeholder="p50" style={{ ...inputStyle, width: 52 }} />
          min
        </label>
        <label style={{ display: "flex", alignItems: "center", gap: 4, fontSize: 11, cursor: "pointer" }}>
          <input type="checkbox" checked={fail} onChange={(e) => { setFail(e.target.checked); if (e.target.checked) setSkip(false); }} />
          falhar
        </label>
        <label style={{ display: "flex", alignItems: "center", gap: 4, fontSize: 11, cursor: "pointer" }}>
          <input type="checkbox" checked={skip} onChange={(e) => { setSkip(e.target.checked); if (e.target.checked) setFail(false); }} />
          não rodar
        </label>
        <button
          onClick={() => { const c = stageChange(); void run(c ?? undefined); }}
          disabled={running || sortedDefs.length === 0}
          style={{
            padding: "5px 12px", background: "var(--v2-accent-brand)", color: "var(--v2-bg-canvas)",
            border: "none", borderRadius: 3, fontSize: 10, fontFamily: "var(--v2-font-mono)",
            letterSpacing: "0.06em", textTransform: "uppercase", fontWeight: 700,
            cursor: running ? "wait" : "pointer", opacity: running ? 0.6 : 1,
          }}
        >
          {running ? "simulando…" : "Simular"}
        </button>
        {changes.length > 0 && (
          <span style={{ fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)" }}>
            cenário: {changes.map((c) => {
              const bits = [c.delayMin ? `+${c.delayMin}min` : "", c.durationMs ? `dur ${Math.round(c.durationMs / 60_000)}min` : "", c.fail ? "FALHA" : "", c.skip ? "SKIP" : ""].filter(Boolean).join(" ");
              return `${c.defId} (${bits})`;
            }).join(" · ")}
            <button onClick={() => { setChanges([]); setReport(null); }} style={{ marginLeft: 6, background: "transparent", border: "none", color: "var(--v2-text-muted)", cursor: "pointer", fontSize: 10, textDecoration: "underline" }}>limpar</button>
          </span>
        )}
        {ranEmpty && (
          <span style={{ fontSize: 10, color: "var(--v2-status-waiting)" }}>defina pelo menos uma mudança (atraso, duração, falhar ou não rodar)</span>
        )}
      </div>

      {/* resultado */}
      <div style={{ flex: 1, overflow: "auto", padding: "10px 14px" }}>
        {!report && !running && (
          <div style={{ fontSize: 11, color: "var(--v2-text-muted)", padding: 20, textAlign: "center" }}>
            Monte um cenário e clique em Simular — o impacto rio abaixo aparece aqui
            (quem atrasa, quem bloqueia, que SLA passa a estourar).
          </div>
        )}
        {report && (
          <>
            <div style={{ display: "flex", gap: 18, flexWrap: "wrap", marginBottom: 10, fontSize: 10, fontFamily: "var(--v2-font-mono)" }}>
              <span>impactados <b style={{ color: report.summary.impacted > 0 ? "var(--v2-status-waiting)" : "var(--v2-text-primary)" }}>{report.summary.impacted}</b> de {report.summary.total}</span>
              <span>bloqueados <b style={{ color: report.summary.blocked > 0 ? "var(--v2-status-failed)" : "var(--v2-text-primary)" }}>{report.summary.blocked}</b></span>
              <span>SLAs novos estourados <b style={{ color: report.summary.newSlaBreaches > 0 ? "var(--v2-status-failed)" : "var(--v2-text-primary)" }}>{report.summary.newSlaBreaches}</b></span>
              <span>fim da diária {fmtDelta(report.summary.makespanScenMs - report.summary.makespanBaseMs)}</span>
            </div>
            <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 11 }}>
              <thead>
                <tr style={{ textAlign: "left", fontSize: 9, fontFamily: "var(--v2-font-mono)", textTransform: "uppercase", letterSpacing: "0.06em", color: "var(--v2-text-muted)" }}>
                  <th style={{ padding: "4px 8px" }}>Job</th>
                  <th style={{ padding: "4px 8px" }}>Baseline</th>
                  <th style={{ padding: "4px 8px" }}>Cenário</th>
                  <th style={{ padding: "4px 8px" }}>Δ fim</th>
                  <th style={{ padding: "4px 8px" }}>Efeito</th>
                  <th style={{ padding: "4px 8px" }}>SLA</th>
                </tr>
              </thead>
              <tbody>
                {impacted.map((r) => {
                  const st = STATE_LABEL[r.state];
                  return (
                    <tr key={r.defId} style={{ borderTop: "1px solid var(--v2-border-subtle)" }}>
                      <td style={{ padding: "5px 8px" }}>
                        <span style={{ color: "var(--v2-text-muted)", fontSize: 10 }}>{r.team} / </span>
                        <span style={{ fontWeight: r.changeInjected ? 700 : 500 }}>{r.label}</span>
                        {r.changeInjected && <span title="job mutado no cenário" style={{ marginLeft: 5, fontSize: 9, color: "var(--v2-accent-brand)", fontFamily: "var(--v2-font-mono)" }}>◈ cenário</span>}
                      </td>
                      <td style={{ padding: "5px 8px", fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-secondary)" }}>
                        {r.baseRuns ? `${fmtHM(r.baseStart)}–${fmtHM(r.baseEnd)}` : "não roda"}
                      </td>
                      <td style={{ padding: "5px 8px", fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-secondary)" }}>
                        {r.scenRuns ? `${fmtHM(r.scenStart)}–${fmtHM(r.scenEnd)}${r.scenStatus === "NOTOK" ? " ✗" : ""}` : "não roda"}
                      </td>
                      <td style={{ padding: "5px 8px", fontFamily: "var(--v2-font-mono)", color: r.deltaMs > 0 ? "var(--v2-status-waiting)" : "var(--v2-text-secondary)" }}>
                        {r.baseRuns && r.scenRuns ? fmtDelta(r.deltaMs) : "—"}
                      </td>
                      <td style={{ padding: "5px 8px" }}>
                        <span style={{ fontSize: 9, fontFamily: "var(--v2-font-mono)", fontWeight: 700, color: st.color, border: `1px solid ${st.color}`, borderRadius: 3, padding: "1px 6px" }}>{st.label}</span>
                      </td>
                      <td style={{ padding: "5px 8px", fontSize: 10, fontFamily: "var(--v2-font-mono)" }}>
                        {r.slaBreachScen && !r.slaBreachBase && <span style={{ color: "var(--v2-status-failed)", fontWeight: 700 }}>ESTOURA</span>}
                        {r.slaBreachScen && r.slaBreachBase && <span style={{ color: "var(--v2-text-muted)" }}>já estourava</span>}
                        {!r.slaBreachScen && r.slaBreachBase && <span style={{ color: "var(--v2-status-ok)" }}>salva</span>}
                      </td>
                    </tr>
                  );
                })}
                {impacted.length === 0 && (
                  <tr><td colSpan={6} style={{ padding: 16, textAlign: "center", color: "var(--v2-text-muted)", fontSize: 11 }}>
                    Nenhum job impactado por este cenário.
                  </td></tr>
                )}
              </tbody>
            </table>
            {untouched > 0 && impacted.length > 0 && (
              <div style={{ marginTop: 8, fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)" }}>
                +{untouched} jobs sem mudança (ocultados)
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
