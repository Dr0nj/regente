/**
 * SchedulePreviewCalendar — preview REAL do schedule como calendário (estilo
 * Control-M "View Scheduling"), no tema do Regente.
 *
 * Os dias DESTACADOS são EXATAMENTE os dias em que o job vai rodar — vindos do
 * backend (`POST /api/schedule/preview` → IsScheduledOn, a MESMA regra da daily).
 * Dia destacado = roda sem falta · dia apagado = não roda nem fudendo. Compõe
 * frequência + meses + calendários (include/exclude) num resultado único.
 *
 * Seletor de ANO no topo (as "abas"), 12 mini-meses abaixo. Em browser-mode (sem
 * server) o cálculo exato não está disponível → mostra aviso.
 */
import { useEffect, useMemo, useRef, useState } from "react";
import { ChevronLeft, ChevronRight } from "lucide-react";
import type { JobSchedule, CalendarRef } from "@/lib/orchestrator-model";
import { schedulePreview } from "@/lib/bloco2-api";
import { isServerMode } from "@/lib/server-client";

const MONTH_NAMES = ["Janeiro","Fevereiro","Março","Abril","Maio","Junho","Julho","Agosto","Setembro","Outubro","Novembro","Dezembro"];
const WD_HEAD = ["Se", "Te", "Qu", "Qu", "Se", "Sá", "Do"]; // semana começa na Segunda (como o Control-M)

interface Props {
  schedule: JobSchedule;
  calendars: CalendarRef[];
}

const pad = (n: number) => String(n).padStart(2, "0");

export default function SchedulePreviewCalendar({ schedule, calendars }: Props) {
  const server = isServerMode();
  const [year, setYear] = useState(() => new Date().getFullYear());
  const [runDays, setRunDays] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const reqId = useRef(0);

  // chave estável do schedule+calendars p/ disparar o recálculo só quando muda de fato.
  const sig = useMemo(() => JSON.stringify({ schedule, calendars }), [schedule, calendars]);

  useEffect(() => {
    if (!server) return;
    const my = ++reqId.current;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- reset síncrono de loading/err a cada mudança de sig/year antes do debounce (250ms); descarte de resposta obsoleta via reqId; reset-on-dep-change com debounce, não reset de mount; ver roadmap §RH
    setLoading(true);
    setErr(null);
    const t = setTimeout(() => {
      schedulePreview({ schedule, calendars }, `${year}-01-01`, `${year}-12-31`)
        .then((res) => {
          if (my !== reqId.current) return; // resposta obsoleta
          setRunDays(new Set(res.dates));
          setLoading(false);
        })
        .catch((e: unknown) => {
          if (my !== reqId.current) return;
          const msg = e instanceof Error ? e.message : String(e);
          // Rota ausente (404) = server desatualizado: o binário em execução não
          // tem o endpoint novo. O lab recompila ao reabrir pelo .bat.
          setErr(/\b404\b/.test(msg) ? "Servidor desatualizado — reinicie o lab (o .bat recompila o server)." : msg);
          setLoading(false);
        });
    }, 250); // debounce: edição rápida não martela a API
    return () => clearTimeout(t);
  }, [sig, year, server]); // eslint-disable-line react-hooks/exhaustive-deps

  const total = runDays.size;

  return (
    <div>
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
        <div style={{ fontSize: 9, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--v2-text-muted)" }}>
          Preview do calendário
        </div>
        <div style={{ flex: 1 }} />
        {server && (
          <span style={{ fontSize: 10, color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)" }}>
            {loading ? "…" : `${total} dia${total === 1 ? "" : "s"} em ${year}`}
          </span>
        )}
      </div>

      {/* Seletor de ano (as "abas") */}
      <div style={{ display: "flex", alignItems: "center", justifyContent: "center", gap: 4, marginBottom: 10 }}>
        <YearBtn onClick={() => setYear((y) => y - 1)} title="Ano anterior"><ChevronLeft size={13} /></YearBtn>
        {[year - 1, year, year + 1].map((y) => (
          <button key={y} onClick={() => setYear(y)} style={{
            padding: "3px 12px", fontSize: 12, cursor: "pointer", borderRadius: 4,
            fontFamily: "var(--v2-font-mono)", fontWeight: y === year ? 700 : 500,
            background: y === year ? "var(--v2-accent-deep)" : "transparent",
            border: `1px solid ${y === year ? "var(--v2-accent-brand)" : "var(--v2-border-subtle)"}`,
            color: y === year ? "var(--v2-accent-brand)" : "var(--v2-text-secondary)",
          }}>{y}</button>
        ))}
        <YearBtn onClick={() => setYear((y) => y + 1)} title="Próximo ano"><ChevronRight size={13} /></YearBtn>
      </div>

      {/* Legenda */}
      <div style={{ display: "flex", alignItems: "center", gap: 14, marginBottom: 10, fontSize: 10, color: "var(--v2-text-muted)" }}>
        <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
          <span style={{ width: 12, height: 12, borderRadius: 3, background: "var(--v2-accent-brand)", display: "inline-block" }} /> roda
        </span>
        <span style={{ display: "inline-flex", alignItems: "center", gap: 5 }}>
          <span style={{ width: 12, height: 12, borderRadius: 3, background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)", display: "inline-block" }} /> não roda
        </span>
      </div>

      {!server ? (
        <div style={{ fontSize: 11, color: "var(--v2-text-muted)", lineHeight: 1.5, background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)", borderRadius: 4, padding: "10px 12px" }}>
          O calendário exato é calculado pelo servidor (mesma regra da daily). Disponível no modo server/GitOps.
        </div>
      ) : err ? (
        <div style={{ fontSize: 11, color: "var(--v2-status-failed)", lineHeight: 1.5, background: "rgba(239,68,68,.08)", border: "1px solid rgba(239,68,68,.3)", borderRadius: 4, padding: "8px 10px", fontFamily: "var(--v2-font-mono)", wordBreak: "break-word" }}>
          Falha ao calcular o preview: {err}
        </div>
      ) : (
        <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(150px, 1fr))", gap: 12 }}>
          {MONTH_NAMES.map((name, mIdx) => (
            <MiniMonth key={mIdx} year={year} monthIdx={mIdx} name={name} runDays={runDays} />
          ))}
        </div>
      )}
    </div>
  );
}

function MiniMonth({ year, monthIdx, name, runDays }: { year: number; monthIdx: number; name: string; runDays: Set<string> }) {
  // offset p/ semana começar na Segunda: getDay() 0=Dom..6=Sáb → Mon-first 0..6.
  const firstDow = (new Date(year, monthIdx, 1).getDay() + 6) % 7;
  const daysInMonth = new Date(year, monthIdx + 1, 0).getDate();
  const cells: Array<number | null> = [];
  for (let i = 0; i < firstDow; i++) cells.push(null);
  for (let d = 1; d <= daysInMonth; d++) cells.push(d);

  return (
    <div style={{ border: "1px solid var(--v2-border-subtle)", borderRadius: 5, overflow: "hidden" }}>
      <div style={{ fontSize: 10.5, fontWeight: 600, textAlign: "center", padding: "4px 0", background: "var(--v2-bg-canvas)", color: "var(--v2-text-secondary)", borderBottom: "1px solid var(--v2-border-subtle)" }}>
        {name} {year}
      </div>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(7, 1fr)", gap: 1, padding: 3 }}>
        {WD_HEAD.map((w, i) => (
          <div key={`h${i}`} style={{ fontSize: 8, textAlign: "center", color: "var(--v2-text-muted)", padding: "1px 0" }}>{w}</div>
        ))}
        {cells.map((d, i) => {
          if (d === null) return <div key={`b${i}`} />;
          const key = `${year}-${pad(monthIdx + 1)}-${pad(d)}`;
          const runs = runDays.has(key);
          return (
            <div key={key} title={runs ? `${key} — roda` : `${key} — não roda`} style={{
              fontSize: 9.5, textAlign: "center", padding: "2px 0", borderRadius: 3,
              fontFamily: "var(--v2-font-mono)",
              background: runs ? "var(--v2-accent-brand)" : "transparent",
              color: runs ? "var(--v2-bg-canvas)" : "var(--v2-text-muted)",
              fontWeight: runs ? 700 : 400,
            }}>{d}</div>
          );
        })}
      </div>
    </div>
  );
}

function YearBtn({ onClick, title, children }: { onClick: () => void; title: string; children: React.ReactNode }) {
  return (
    <button onClick={onClick} title={title} style={{
      display: "inline-flex", alignItems: "center", justifyContent: "center",
      width: 24, height: 24, borderRadius: 4, cursor: "pointer",
      background: "transparent", border: "1px solid var(--v2-border-subtle)", color: "var(--v2-text-secondary)",
    }}>{children}</button>
  );
}
