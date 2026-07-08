import { useEffect, useState } from "react";
import { fetchPerfForecast, type PerfForecast } from "@/lib/differentials-api";

/* ──────────────────────────────────────────────────────────────
   ForecastPanel — D-4 performance forecasting (drawer da instance)
   ──────────────────────────────────────────────────────────────
   Sparkline SVG puro (zero dependência) das últimas execuções OK +
   p50/p90, previsão da próxima e tendência ("ficando mais lento").
   Com RUNNING em curso: ETA. Dados do /api/analytics/forecast.
   ────────────────────────────────────────────────────────────── */

function fmtMs(ms?: number): string {
  if (ms == null) return "—";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  const m = Math.floor(ms / 60000);
  const s = Math.floor((ms % 60000) / 1000);
  return `${m}m${s.toString().padStart(2, "0")}s`;
}

function Sparkline({ pf }: { pf: PerfForecast }) {
  const W = 260, H = 56, PAD = 4;
  const durs = pf.samples.map((s) => s.durationMs);
  const max = Math.max(...durs, pf.p90Ms, 1);
  const n = durs.length;
  const x = (i: number) => PAD + (i * (W - 2 * PAD)) / Math.max(n - 1, 1);
  const y = (v: number) => H - PAD - (v / max) * (H - 2 * PAD);
  const path = durs.map((v, i) => `${i === 0 ? "M" : "L"}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join(" ");
  return (
    <svg width={W} height={H} style={{ display: "block", width: "100%" }} viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none">
      {/* linha do p50 (referência) */}
      <line x1={PAD} x2={W - PAD} y1={y(pf.p50Ms)} y2={y(pf.p50Ms)}
        stroke="var(--v2-border-strong)" strokeDasharray="3 3" strokeWidth={1} />
      <path d={path} fill="none" stroke={pf.slower ? "var(--v2-status-failed)" : "var(--v2-accent-brand)"} strokeWidth={1.5} />
      {durs.map((v, i) => (
        <circle key={i} cx={x(i)} cy={y(v)} r={1.8}
          fill={pf.slower ? "var(--v2-status-failed)" : "var(--v2-accent-brand)"} />
      ))}
    </svg>
  );
}

export default function ForecastPanel({ defId, isRunning }: { defId: string; isRunning: boolean }) {
  const [pf, setPf] = useState<PerfForecast | null>(null);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    let alive = true;
    setLoaded(false);
    fetchPerfForecast(defId).then((out) => {
      if (!alive) return;
      setPf(out);
      setLoaded(true);
    });
    return () => { alive = false; };
  }, [defId]);

  if (!loaded || !pf || pf.samples.length < 2) return null; // sem histórico, sem painel

  const stat = (label: string, value: string, tone?: string) => (
    <div style={{ display: "flex", flexDirection: "column", gap: 1 }}>
      <span style={{ fontSize: 9, color: "var(--v2-text-muted)", letterSpacing: "0.06em", textTransform: "uppercase" }}>{label}</span>
      <span style={{ fontSize: 12, fontFamily: "var(--v2-font-mono)", color: tone ?? "var(--v2-text-primary)" }}>{value}</span>
    </div>
  );

  return (
    <div style={{ padding: "10px 0", borderTop: "1px solid var(--v2-border-subtle)", marginTop: 10 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
        <span style={{ fontSize: 10, fontWeight: 600, letterSpacing: "0.08em", color: "var(--v2-text-secondary)", textTransform: "uppercase" }}>
          Forecast · {pf.samples.length} execuções
        </span>
        {pf.slower && (
          <span style={{
            fontSize: 9, fontFamily: "var(--v2-font-mono)", color: "var(--v2-status-failed)",
            border: "1px solid var(--v2-status-failed)", borderRadius: 3, padding: "1px 5px",
          }}>
            ▲ ficando mais lento
          </span>
        )}
      </div>
      <Sparkline pf={pf} />
      <div style={{ display: "flex", gap: 16, marginTop: 8 }}>
        {stat("p50", fmtMs(pf.p50Ms))}
        {stat("p90", fmtMs(pf.p90Ms))}
        {stat("próxima ~", fmtMs(pf.nextMs), pf.slower ? "var(--v2-status-failed)" : undefined)}
        {isRunning && pf.etaMs != null && stat("ETA", fmtMs(pf.etaMs), "var(--v2-status-running)")}
      </div>
    </div>
  );
}
