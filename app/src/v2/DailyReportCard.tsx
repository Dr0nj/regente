// E5 — card compacto do relatório da daily no topo do Monitoring.
//
// Mostra os NÚMEROS DO SERVER (GET /api/daily/report) — a UI não recalcula
// nada localmente (aceite da spec: os counts vêm do endpoint agregado, que
// enxerga o dia INTEIRO mesmo quando o board está filtrado/paginado).
// Atualiza no mount, quando a daily roda (evento ws) e a cada 60s.
import { useCallback, useEffect, useState } from "react";
import { api, isServerMode, onServerEvent } from "../lib/server-client";

interface ReportCounts {
  ordered: number; ok: number; notok: number; waiting: number;
  running: number; held: number; cancelled: number; carried: number;
}
export interface DailyReportData {
  date: string;
  dailyAt: string;
  timezone?: string;
  lateStart: boolean;
  closed: boolean;
  counts: ReportCounts;
  reportSent: boolean;
}

function Chip({ label, value, color }: { label: string; value: number; color: string }) {
  return (
    <span style={{ display: "inline-flex", alignItems: "center", gap: 4, whiteSpace: "nowrap" }}>
      <span style={{ width: 6, height: 6, borderRadius: "50%", background: color, flexShrink: 0 }} />
      <span style={{ color: "var(--v2-text-primary)", fontWeight: 700 }}>{value}</span>
      <span style={{ color: "var(--v2-text-muted)" }}>{label}</span>
    </span>
  );
}

export function DailyReportCard() {
  const [rep, setRep] = useState<DailyReportData | null>(null);

  const refresh = useCallback(() => {
    if (!isServerMode()) return;
    api<DailyReportData>("/api/daily/report").then(setRep).catch(() => {});
  }, []);

  useEffect(() => {
    refresh();
    const t = window.setInterval(refresh, 60_000);
    const off = onServerEvent((ev) => {
      if (ev.event === "daily.started" || ev.event === "instance.bulk") refresh();
    });
    return () => { window.clearInterval(t); off(); };
  }, [refresh]);

  if (!rep || rep.counts.ordered === 0) return null; // sem diária = sem card

  return (
    <div
      title={`Daily ${rep.date} · configurada ${rep.dailyAt}${rep.timezone ? " " + rep.timezone : ""} · dados de /api/daily/report`}
      style={{
        position: "absolute", top: 10, left: "50%", transform: "translateX(-50%)", zIndex: 18,
        display: "flex", alignItems: "center", gap: 10, padding: "5px 12px",
        background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-medium)",
        borderRadius: 999, fontSize: 10, fontFamily: "var(--v2-font-mono)",
        boxShadow: "0 2px 10px rgba(0,0,0,0.25)", pointerEvents: "auto",
      }}
    >
      <span style={{ color: "var(--v2-text-secondary)", fontWeight: 700 }}>DAILY {rep.date}</span>
      <Chip label="ok" value={rep.counts.ok} color="var(--v2-status-ok)" />
      <Chip label="notok" value={rep.counts.notok} color="var(--v2-status-failed)" />
      <Chip label="abertos" value={rep.counts.waiting + rep.counts.running} color="var(--v2-status-waiting)" />
      {rep.counts.carried > 0 && (
        <Chip label="carregados" value={rep.counts.carried} color="var(--v2-text-muted)" />
      )}
      {rep.lateStart && (
        <span style={{ color: "var(--v2-status-failed)", fontWeight: 700 }}>⏰ atrasada</span>
      )}
      {rep.closed && (
        <span style={{ color: "var(--v2-status-ok)", fontWeight: 700 }}>fechada{rep.reportSent ? " ✓ report" : ""}</span>
      )}
    </div>
  );
}
