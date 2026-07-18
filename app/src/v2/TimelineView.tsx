import { useEffect, useMemo, useRef, useState } from "react";
import type { JobInstance } from "@/lib/orchestrator-model";
import { fetchDayDurations } from "@/lib/differentials-api";

/* ──────────────────────────────────────────────────────────────
   TimelineView — D-12 Visual Schedule Editor (Gantt da daily)
   ──────────────────────────────────────────────────────────────
   Overlay em cima do canvas do Monitoring: uma linha por job,
   agrupado por folder, eixo horário 00–24h do order_date.

   Barra REAL     startedAt → completedAt (ou → agora se RUNNING)
   Barra PREVISTA scheduledAt → +p50 histórico (WAITING/HOLD),
                  hachurada — é plano, não fato (D-4 alimenta).
   Linha "agora"  vertical, atualiza por minuto.

   SVG puro, escala horizontal fixa por hora com scroll nas duas
   direções — sem lib de chart, sem estado global.
   ────────────────────────────────────────────────────────────── */

const HOUR_W = 68;         // px por hora
const ROW_H = 22;
const LABEL_W = 220;
const HEADER_H = 26;
const MAX_ROWS = 400;      // proteção: dias de 100k jobs não viram um SVG de 100k linhas

const STATUS_BAR: Record<string, string> = {
  OK: "var(--v2-status-ok)",
  NOTOK: "var(--v2-status-failed)",
  RUNNING: "var(--v2-status-running)",
  WAITING: "var(--v2-status-waiting)",
  HOLD: "var(--v2-text-secondary)",
  CANCELLED: "var(--v2-text-muted)",
};

export default function TimelineView({
  instances,
  onSelect,
}: {
  instances: JobInstance[];
  onSelect?: (id: string) => void;
}) {
  const [p50, setP50] = useState<Record<string, number>>({});
  const [now, setNow] = useState<number>(Date.now());
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    fetchDayDurations().then(setP50);
    const t = setInterval(() => setNow(Date.now()), 60_000);
    return () => clearInterval(t);
  }, []);

  // agrupa por folder e ordena por scheduledAt dentro da folder.
  const { rows, truncated, dayStart } = useMemo(() => {
    const sorted = [...instances].sort((a, b) => {
      const fa = a.team || "—", fb = b.team || "—";
      if (fa !== fb) return fa.localeCompare(fb);
      return a.scheduledAt - b.scheduledAt;
    });
    const trunc = sorted.length > MAX_ROWS;
    const visible = trunc ? sorted.slice(0, MAX_ROWS) : sorted;
    // meia-noite do order_date (as instances do dia compartilham o date)
    const od = visible[0]?.orderDate ?? new Date().toISOString().slice(0, 10);
    const start = new Date(`${od}T00:00:00`).getTime();
    type Row = { kind: "folder"; name: string; count: number } | { kind: "job"; inst: JobInstance };
    const out: Row[] = [];
    let lastFolder: string | null = null;
    for (const inst of visible) {
      const f = inst.team || "—";
      if (f !== lastFolder) {
        out.push({ kind: "folder", name: f, count: visible.filter((i) => (i.team || "—") === f).length });
        lastFolder = f;
      }
      out.push({ kind: "job", inst });
    }
    return { rows: out, truncated: trunc, dayStart: start };
  }, [instances]);

  const chartW = 24 * HOUR_W;
  const chartH = rows.length * ROW_H;
  const xOf = (ms: number) => ((ms - dayStart) / 3_600_000) * HOUR_W;
  const nowX = xOf(now);

  // centraliza o scroll na "agora" na primeira render
  useEffect(() => {
    const el = scrollRef.current;
    if (el && nowX > 0 && nowX < chartW) {
      el.scrollLeft = Math.max(0, nowX - el.clientWidth / 2 + LABEL_W);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dayStart]);

  return (
    <div
      style={{
        width: "100%", height: "100%",
        background: "var(--v2-bg-surface)",
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
          Timeline da daily
        </span>
        <span style={{ fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)" }}>
          {instances.length} jobs · barra sólida = real · hachura = previsto (p50 histórico)
        </span>
        {truncated && (
          <span style={{ fontSize: 10, color: "var(--v2-status-waiting)" }}>
            mostrando {MAX_ROWS} de {instances.length}
          </span>
        )}
      </div>

      {/* corpo com scroll (labels fixas via sticky) */}
      <div ref={scrollRef} style={{ flex: 1, overflow: "auto", position: "relative" }}>
        <div style={{ width: LABEL_W + chartW, position: "relative" }}>
          {/* régua de horas (sticky no topo) */}
          <div style={{
            position: "sticky", top: 0, zIndex: 3, display: "flex", height: HEADER_H,
            background: "var(--v2-bg-elevated)", borderBottom: "1px solid var(--v2-border-subtle)",
          }}>
            <div style={{
              position: "sticky", left: 0, width: LABEL_W, flexShrink: 0, zIndex: 4,
              background: "var(--v2-bg-elevated)", borderRight: "1px solid var(--v2-border-subtle)",
            }} />
            {Array.from({ length: 24 }, (_, h) => (
              <div key={h} style={{
                width: HOUR_W, flexShrink: 0, fontSize: 9, fontFamily: "var(--v2-font-mono)",
                color: "var(--v2-text-muted)", padding: "6px 0 0 4px",
                borderLeft: "1px solid var(--v2-border-subtle)",
              }}>
                {String(h).padStart(2, "0")}:00
              </div>
            ))}
          </div>

          {/* linhas */}
          <div style={{ position: "relative" }}>
            {/* grid vertical + linha do agora */}
            <svg width={LABEL_W + chartW} height={chartH}
              style={{ position: "absolute", left: 0, top: 0, pointerEvents: "none" }}>
              {Array.from({ length: 24 }, (_, h) => (
                <line key={h} x1={LABEL_W + h * HOUR_W} x2={LABEL_W + h * HOUR_W} y1={0} y2={chartH}
                  stroke="var(--v2-border-subtle)" strokeWidth={1} />
              ))}
              {nowX >= 0 && nowX <= chartW && (
                <line x1={LABEL_W + nowX} x2={LABEL_W + nowX} y1={0} y2={chartH}
                  stroke="var(--v2-accent-brand)" strokeWidth={1.5} strokeDasharray="2 3" />
              )}
            </svg>

            {rows.map((row, i) =>
              row.kind === "folder" ? (
                <div key={`f-${row.name}-${i}`} style={{
                  height: ROW_H, display: "flex", alignItems: "center",
                  background: "var(--v2-bg-elevated)", borderBottom: "1px solid var(--v2-border-subtle)",
                  position: "relative", zIndex: 1,
                }}>
                  <span style={{
                    position: "sticky", left: 0, paddingLeft: 10, fontSize: 10, fontWeight: 600,
                    fontFamily: "var(--v2-font-mono)", letterSpacing: "0.08em", textTransform: "uppercase",
                  }}>
                    {row.name}
                  </span>
                </div>
              ) : (
                <TimelineRow key={row.inst.id} inst={row.inst} p50Ms={p50[row.inst.definitionId]}
                  xOf={xOf} now={now} onSelect={onSelect} />
              ),
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

function TimelineRow({
  inst, p50Ms, xOf, now, onSelect,
}: {
  inst: JobInstance;
  p50Ms?: number;
  xOf: (ms: number) => number;
  now: number;
  onSelect?: (id: string) => void;
}) {
  const color = STATUS_BAR[inst.status] ?? "var(--v2-text-muted)";
  // barra real: started→completed (RUNNING: →agora). Sem started: prevista.
  let x0: number, x1: number, solid: boolean;
  if (inst.startedAt) {
    x0 = xOf(inst.startedAt);
    x1 = xOf(inst.completedAt ?? now);
    solid = true;
  } else {
    x0 = xOf(inst.scheduledAt);
    x1 = xOf(inst.scheduledAt + Math.max(p50Ms ?? 5 * 60_000, 60_000));
    solid = false;
  }
  const w = Math.max(x1 - x0, 3);

  return (
    <div
      onClick={() => onSelect?.(inst.id)}
      style={{
        height: ROW_H, display: "flex", alignItems: "center", cursor: onSelect ? "pointer" : "default",
        borderBottom: "1px solid var(--v2-border-subtle)", position: "relative",
      }}
      onMouseEnter={(e) => (e.currentTarget.style.background = "var(--v2-bg-hover)")}
      onMouseLeave={(e) => (e.currentTarget.style.background = "transparent")}
    >
      <span
        title={inst.id}
        style={{
          position: "sticky", left: 0, width: LABEL_W, flexShrink: 0, zIndex: 2,
          paddingLeft: 22, paddingRight: 8, fontSize: 10.5,
          whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis",
          background: "var(--v2-bg-surface)", borderRight: "1px solid var(--v2-border-subtle)",
          lineHeight: `${ROW_H}px`, height: ROW_H,
        }}
      >
        <span style={{ display: "inline-block", width: 6, height: 6, borderRadius: "50%", background: color, marginRight: 6 }} />
        {inst.label}
      </span>
      <div style={{
        position: "absolute", left: LABEL_W + x0, width: w, height: 10, top: (ROW_H - 10) / 2,
        background: solid ? color : "transparent",
        border: solid ? "none" : `1px dashed ${color}`,
        opacity: solid ? 0.9 : 0.7, borderRadius: 3,
        backgroundImage: solid ? "none" : `repeating-linear-gradient(45deg, ${color}22 0 4px, transparent 4px 8px)`,
        animation: inst.status === "RUNNING" ? "v2-dot-pulse 1.2s ease-in-out infinite" : "none",
      }} />
    </div>
  );
}
