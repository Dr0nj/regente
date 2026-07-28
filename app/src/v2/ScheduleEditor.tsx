/**
 * ScheduleEditor — editor visual de recorrência estilo Control-M.
 *
 * Modos de frequência (decisão 2026-06-29): apenas os que o Regente sabe resolver
 * SOZINHO, sem precisar saber feriados/dia-útil de cada cidade:
 *   - daily:   todo dia
 *   - weekly:  dias da semana (toggles)
 *   - monthly: dias do mês (grid 1..31 + último dia)
 * "Dia útil" / "regra avançada" SAÍRAM daqui: dia útil depende de um CALENDÁRIO
 * (cada lugar tem seus feriados). Em vez disso, os calendários entram NESTA aba e
 * trabalham junto com as regras como INCLUDE (só nesses dias) ou EXCLUDE (menos
 * esses dias). Ex.: negar o calendário "dias úteis" + marcar todos os dias =
 * "roda todo dia, exceto dia útil".
 *
 * O PREVIEW no rodapé traduz frequência + meses + calendários + horário em
 * linguagem natural.
 */
import { useState, useEffect } from "react";
import { Plus, Trash2 } from "lucide-react";
import type { JobSchedule, ScheduleFrequency, CalendarRef } from "@/lib/orchestrator-model";
import type { Calendar } from "@/lib/bloco2-api";
import { fetchDailyStatus } from "@/lib/server-scheduler-runtime";
import SchedulePreviewCalendar from "./SchedulePreviewCalendar";

const WEEKDAYS: Array<{ id: string; label: string }> = [
  { id: "mon", label: "Mon" }, { id: "tue", label: "Tue" }, { id: "wed", label: "Wed" },
  { id: "thu", label: "Thu" }, { id: "fri", label: "Fri" }, { id: "sat", label: "Sat" }, { id: "sun", label: "Sun" },
];

const MONTHS = ["Jan","Feb","Mar","Apr","May","Jun","Jul","Aug","Sep","Oct","Nov","Dec"];

const FREQS: Array<{ id: ScheduleFrequency; label: string }> = [
  { id: "daily", label: "Daily" },
  { id: "weekly", label: "Days of week" },
  { id: "monthly", label: "Days of month" },
];

interface Props {
  value: JobSchedule;
  onChange: (next: JobSchedule) => void;
  /** Calendários chamados/negados pelo job (trabalham junto com as regras). */
  calendars: CalendarRef[];
  onCalendarsChange: (c: CalendarRef[]) => void;
  /** Calendários disponíveis (objetos completos p/ traduzir o comportamento). */
  availableCalendars: Calendar[];
}

export default function ScheduleEditor({ value, onChange, calendars, onCalendarsChange, availableCalendars }: Props) {
  const s = value;
  // "daily" é o default; frequências legadas (businessday/advanced) caem em daily na UI.
  const freq: ScheduleFrequency = (s.frequency === "weekly" || s.frequency === "monthly") ? s.frequency : "daily";
  const patch = (p: Partial<JobSchedule>) => onChange({ ...s, ...p });

  // Relógio do SERVER — o horário da janela é SEMPRE no fuso do server (report do
  // usuário: "o horário sempre é o do server"). Mostrar o relógio dele aqui evita
  // configurar no fuso errado. Uma busca só (barato); silencioso se falhar.
  const [serverClock, setServerClock] = useState<{ now: string; tz: string } | null>(null);
  useEffect(() => {
    let alive = true;
    void fetchDailyStatus().then((st) => {
      if (!alive || !st?.serverNow) return;
      const d = new Date(st.serverNow);
      const hhmm = Number.isNaN(d.getTime()) ? "" : d.toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit", hour12: false });
      setServerClock({ now: hhmm, tz: st.timezone || "server local time" });
    });
    return () => { alive = false; };
  }, []);

  // "A partir de" consolida o antigo "Roda às" (runAt): o início da janela É o
  // horário de início. Jobs legados com runAt aparecem no campo; qualquer edição
  // migra pra windowFrom e zera o runAt (o server já prefere runAt→windowFrom).
  const fromVal = s.windowFrom || s.runAt || "";

  const toggleInArr = <T,>(arr: T[] | undefined, v: T): T[] => {
    const set = new Set(arr ?? []);
    if (set.has(v)) set.delete(v); else set.add(v);
    return [...set];
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      {/* Frequência (segmented) */}
      <Group label="Frequency">
        <div style={{ display: "flex", flexWrap: "wrap", gap: 4 }}>
          {FREQS.map((f) => (
            <Seg key={f.id} active={freq === f.id} onClick={() => patch({ frequency: f.id })}>{f.label}</Seg>
          ))}
        </div>
      </Group>

      {/* Corpo condicional por frequência */}
      {freq === "daily" && (
        <Hint>Runs every day (subject to the calendars and the month filter below).</Hint>
      )}

      {freq === "weekly" && (
        <Group label="Days of week">
          <div style={{ display: "flex", gap: 4 }}>
            {WEEKDAYS.map((d) => (
              <Toggle key={d.id} active={(s.daysOfWeek ?? []).includes(d.id)}
                onClick={() => patch({ daysOfWeek: toggleInArr(s.daysOfWeek, d.id) })}>{d.label}</Toggle>
            ))}
          </div>
        </Group>
      )}

      {freq === "monthly" && (
        <Group label="Days of month">
          <div style={{ display: "grid", gridTemplateColumns: "repeat(7, 1fr)", gap: 3 }}>
            {Array.from({ length: 31 }, (_, i) => i + 1).map((day) => (
              <Toggle key={day} small active={(s.daysOfMonth ?? []).includes(day)}
                onClick={() => patch({ daysOfMonth: toggleInArr(s.daysOfMonth, day) })}>{day}</Toggle>
            ))}
            <Toggle small wide active={(s.daysOfMonth ?? []).includes(-1)}
              onClick={() => patch({ daysOfMonth: toggleInArr(s.daysOfMonth, -1) })}>last</Toggle>
          </div>
        </Group>
      )}

      {/* Filtro de meses (opcional, vale para todas as frequências) */}
      <Group label="Months (empty = all)">
        <div style={{ display: "grid", gridTemplateColumns: "repeat(6, 1fr)", gap: 3 }}>
          {MONTHS.map((m, i) => (
            <Toggle key={m} small active={(s.monthsOfYear ?? []).includes(i + 1)}
              onClick={() => patch({ monthsOfYear: toggleInArr(s.monthsOfYear, i + 1) })}>{m}</Toggle>
          ))}
        </div>
      </Group>

      {/* Calendários — trabalham junto com as regras (include/exclude) */}
      <CalendarSection calendars={calendars} onChange={onCalendarsChange} available={availableCalendars} />

      {/* Janela de execução — 2 campos: início ("a partir de") e fim ("até").
          O antigo "Roda às" saiu: o início da janela É o horário de início. */}
      <Group label="Execution window">
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
          <Sub label="From (HH:MM)"><TimeInput value={fromVal} onChange={(v) => patch({ windowFrom: v, runAt: "" })} placeholder="--:--" /></Sub>
          <Sub label="To (HH:MM)"><TimeInput value={s.windowTo ?? ""} onChange={(v) => patch({ windowTo: v })} placeholder="--:--" /></Sub>
        </div>
        <Hint>
          Times are in the <b>server timezone</b>{serverClock ? <> (now there: <b style={{ color: "var(--v2-accent-brand)", fontFamily: "var(--v2-font-mono)" }}>{serverClock.now}</b> · {serverClock.tz})</> : ""}.
          <br /><b>Both empty</b> = runs as soon as possible (with no condition, as soon as it enters the daily; with a condition, as soon as it is satisfied).
          <br /><b>From</b> = only starts after that time. <b>To</b> = does not run after that time — not even if the condition arrives later.
        </Hint>
        <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 11, color: "var(--v2-text-secondary)", marginTop: 8, flexWrap: "wrap" }}>
          <input type="checkbox" checked={!!s.cyclic} onChange={(e) => patch({ cyclic: e.target.checked })} />
          Cyclic — repeats every
          <input type="number" min={1} value={s.intervalMin ?? 0} disabled={!s.cyclic}
            onChange={(e) => patch({ intervalMin: Number(e.target.value) || 0 })}
            style={{ width: 56, ...inputBase, opacity: s.cyclic ? 1 : 0.4 }} />
          min, up to
          <input type="number" min={0} value={s.cyclicMaxRuns ?? 0} disabled={!s.cyclic}
            onChange={(e) => patch({ cyclicMaxRuns: Math.max(0, Number(e.target.value) || 0) })}
            style={{ width: 56, ...inputBase, opacity: s.cyclic ? 1 : 0.4 }} />
          laps (0 = no cap)
        </label>
        {s.cyclic && (
          <Hint>
            On every OK lap the job re-arms to run in {s.intervalMin || "N"}min. The cycle ends when it
            hits the lap cap, passes the window &quot;To&quot; time, or the daily rolls over. A failure (NOTOK)
            does NOT cycle — it waits for a rerun/Set OK.
          </Hint>
        )}
      </Group>

      {/* Ciclo de vida na diária — carry-over Control-M (Keep Active) */}
      <Group label="Lifecycle across dailies">
        <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 11, color: "var(--v2-text-secondary)" }}>
          Keep Active — survives
          <input type="number" min={0} value={s.keepActive ?? 0}
            onChange={(e) => patch({ keepActive: Math.max(0, Number(e.target.value) || 0) })}
            style={{ width: 56, ...inputBase }} />
          extra dailies if it does not end OK
        </label>
        <Hint>
          RUNNING and HELD always cross the rollover. 0 = default (an untreated NOTOK already
          survives +1 daily). N &gt; 0 keeps the job for N dailies even without running/ending OK.
        </Hint>
      </Group>

      {/* Shift — Control-M "roll": dia nominal em feriado/fim de semana */}
      <Group label="If it falls on a non-business day (shift)">
        <select
          value={s.shift ?? ""}
          onChange={(e) => patch({ shift: e.target.value as NonNullable<typeof s.shift> })}
          style={{ ...inputBase, width: "100%" }}
        >
          <option value="">Does not run (default)</option>
          <option value="next-businessday">Roll to the NEXT business day</option>
          <option value="prev-businessday">Pull forward to the PREVIOUS business day</option>
        </select>
        <Hint>
          Applies when the recurrence day falls on an ineligible day: a holiday/exclude from the
          job's calendars — or, with no calendar, Saturday/Sunday. The preview below already
          reflects the shift (same rule as the daily).
        </Hint>
      </Group>

      {/* PREVIEW — embaixo de todas as regras. Gist em texto + calendário REAL (dias
          destacados = dias que o job roda, pela mesma regra da daily). */}
      <SchedulePreview schedule={s} calendars={calendars} calDefs={availableCalendars} />
      <SchedulePreviewCalendar schedule={s} calendars={calendars} />
    </div>
  );
}

/* ── Calendários na aba Schedule (include/exclude, junto com as regras) ── */
function CalendarSection({ calendars, onChange, available }: { calendars: CalendarRef[]; onChange: (c: CalendarRef[]) => void; available: Calendar[] }) {
  const [pick, setPick] = useState("");
  const add = (mode: "include" | "exclude") => {
    if (!pick) return;
    if (calendars.some((c) => c.name === pick && c.mode === mode)) return;
    onChange([...calendars, { name: pick, mode }]);
    setPick("");
  };
  const remove = (i: number) => onChange(calendars.filter((_, idx) => idx !== i));
  const calByName = (n: string) => available.find((c) => c.name === n);

  return (
    <Group label="Calendars (they work together with the rules)">
      <Hint>
        <b>Include</b> = only runs on the calendar's days. <b>Exclude</b> = does NOT run on those days.
        Combine them with the rules above — e.g. excluding "business days" + selecting every day = runs every day except business days.
      </Hint>
      <div style={{ display: "flex", gap: 6, marginTop: 6 }}>
        <select value={pick} onChange={(e) => setPick(e.target.value)} style={{ ...selectStyle, flex: 1 }}>
          <option value="">— calendar —</option>
          {available.filter((c) => !calendars.some((cr) => cr.name === c.name)).map((c) => (
            <option key={c.name} value={c.name}>{c.name}</option>
          ))}
        </select>
        <button onClick={() => add("include")} disabled={!pick} style={{ ...chipBtn, borderColor: "var(--v2-accent-brand)", color: "var(--v2-accent-brand)" }}><Plus size={11} /> include</button>
        <button onClick={() => add("exclude")} disabled={!pick} style={{ ...chipBtn, borderColor: "#7f1d1d", color: "#fca5a5" }}><Plus size={11} /> exclude</button>
      </div>
      {available.length === 0 && <Hint>No calendar created yet. Create one in Control Panel → Calendars.</Hint>}
      <div style={{ display: "flex", flexDirection: "column", gap: 4, marginTop: 6 }}>
        {calendars.map((c, i) => (
          <div key={`${c.name}-${c.mode}`} style={{ display: "flex", alignItems: "center", gap: 8, padding: "6px 8px", background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)", borderRadius: 3 }}>
            <span style={{ fontSize: 9, fontWeight: 700, textTransform: "uppercase", padding: "1px 6px", borderRadius: 2, fontFamily: "var(--v2-font-mono)",
              background: c.mode === "exclude" ? "#3b1d1d" : "var(--v2-accent-deep)", color: c.mode === "exclude" ? "#fca5a5" : "var(--v2-accent-brand)",
              border: `1px solid ${c.mode === "exclude" ? "#7f1d1d" : "var(--v2-accent-dark)"}` }}>
              {c.mode === "exclude" ? "exclude" : "include"}
            </span>
            <div style={{ flex: 1, display: "flex", flexDirection: "column", gap: 1 }}>
              <span style={{ fontSize: 12, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-primary)" }}>{c.name}</span>
              <span style={{ fontSize: 10, color: "var(--v2-text-muted)" }}>{describeCalendar(calByName(c.name))}</span>
            </div>
            <button onClick={() => remove(i)} style={iconBtn} title="Remove"><Trash2 size={12} /></button>
          </div>
        ))}
      </div>
    </Group>
  );
}

/* ── Tradução do COMPORTAMENTO de um calendário (o que ele significa) ── */
const WD_EN: Record<string, string> = { mon: "Mon", tue: "Tue", wed: "Wed", thu: "Thu", fri: "Fri", sat: "Sat", sun: "Sun" };
function describeCalendar(cal?: Calendar): string {
  if (!cal) return "calendar not found";
  const parts: string[] = [];
  const bd = (cal.businessDays ?? []).map((d) => d.toLowerCase().slice(0, 3));
  if (bd.length) {
    const isMonFri = ["mon", "tue", "wed", "thu", "fri"].every((d) => bd.includes(d)) && !bd.includes("sat") && !bd.includes("sun");
    parts.push(isMonFri ? "Mon–Fri" : bd.map((d) => WD_EN[d] ?? d).join(", "));
  }
  if ((cal.holidays ?? []).length) parts.push(`minus ${cal.holidays!.length} holiday${cal.holidays!.length > 1 ? "s" : ""}`);
  if ((cal.excludeDates ?? []).length) parts.push(`minus ${cal.excludeDates!.length} date(s)`);
  if ((cal.includeDates ?? []).length) parts.push(`+ ${cal.includeDates!.length} exception(s)`);
  return parts.length ? parts.join(", ") : "no days defined";
}

/* ── Preview legível: frequência + meses + calendários + horário ── */
function SchedulePreview({ schedule: s, calendars, calDefs }: { schedule: JobSchedule; calendars: CalendarRef[]; calDefs: Calendar[] }) {
  const freq = (s.frequency === "weekly" || s.frequency === "monthly") ? s.frequency : "daily";
  const calByName = (n: string) => calDefs.find((c) => c.name === n);

  // base (frequência)
  let base: string;
  if (freq === "weekly") {
    const days = s.daysOfWeek ?? [];
    if (days.length === 0) base = "(no weekday selected)";
    else if (days.length === 7) base = "every day";
    else base = `every ${days.map((d) => WD_EN[d] ?? d).join(", ")}`;
  } else if (freq === "monthly") {
    const days = s.daysOfMonth ?? [];
    base = days.length ? `on day ${days.map((d) => (d === -1 ? "last" : d)).join(", ")} of the month` : "(no day of month selected)";
  } else {
    base = "every day";
  }

  // calendários
  const inc = calendars.filter((c) => c.mode === "include");
  const exc = calendars.filter((c) => c.mode === "exclude");
  const calPhrase = (c: CalendarRef) => {
    const d = describeCalendar(calByName(c.name));
    return `${c.name}${d && d !== "no days defined" && d !== "calendar not found" ? ` (${d})` : ""}`;
  };

  const parts: string[] = [base];
  if (inc.length) parts.push(`only on days from ${inc.map(calPhrase).join(" and ")}`);
  if (exc.length) parts.push(`except ${exc.map(calPhrase).join(" and ")}`);
  if ((s.monthsOfYear ?? []).length) parts.push(`only in ${(s.monthsOfYear ?? []).map((m) => MONTHS[m - 1]).join(", ")}`);
  const from = s.windowFrom || s.runAt;
  if (from && s.windowTo) parts.push(`from ${from} to ${s.windowTo}`);
  else if (from) parts.push(`from ${from} onwards`);
  else if (s.windowTo) parts.push(`until ${s.windowTo}`);
  if (s.cyclic && s.intervalMin) parts.push(`repeating every ${s.intervalMin}min`);
  if (s.keepActive && s.keepActive > 0) parts.push(`keep active ${s.keepActive}d`);

  return (
    <div>
      <div style={{ fontSize: 9, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--v2-text-muted)", marginBottom: 6 }}>Preview</div>
      <div style={{ fontSize: 12, color: "var(--v2-accent-brand)", lineHeight: 1.5, background: "var(--v2-accent-faint)", border: "1px solid var(--v2-accent-dark)", borderRadius: 4, padding: "8px 10px" }}>
        Runs {parts.join(", ")}.
        {!s.enabled && <span style={{ color: "var(--v2-text-muted)" }}> (schedule disabled)</span>}
      </div>
    </div>
  );
}

/* ── primitivos visuais ── */
function Group({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div style={{ fontSize: 9, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--v2-text-muted)", marginBottom: 6 }}>{label}</div>
      {children}
    </div>
  );
}
function Sub({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div style={{ fontSize: 8.5, color: "var(--v2-text-muted)", marginBottom: 3 }}>{label}</div>
      {children}
    </div>
  );
}
function Hint({ children }: { children: React.ReactNode }) {
  return <div style={{ fontSize: 10.5, color: "var(--v2-text-muted)", lineHeight: 1.4 }}>{children}</div>;
}
function Seg({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button onClick={onClick} style={{
      padding: "5px 10px", fontSize: 11, cursor: "pointer", borderRadius: 3,
      background: active ? "var(--v2-accent-deep)" : "transparent",
      border: `1px solid ${active ? "var(--v2-accent-brand)" : "var(--v2-border-medium)"}`,
      color: active ? "var(--v2-accent-brand)" : "var(--v2-text-secondary)", fontWeight: active ? 600 : 500,
    }}>{children}</button>
  );
}
function Toggle({ active, onClick, children, small, wide }: { active: boolean; onClick: () => void; children: React.ReactNode; small?: boolean; wide?: boolean }) {
  return (
    <button onClick={onClick} style={{
      padding: small ? "4px 0" : "5px 10px", fontSize: small ? 10 : 11, cursor: "pointer", borderRadius: 3,
      gridColumn: wide ? "span 2" : undefined,
      background: active ? "var(--v2-accent-deep)" : "var(--v2-bg-canvas)",
      border: `1px solid ${active ? "var(--v2-accent-brand)" : "var(--v2-border-subtle)"}`,
      color: active ? "var(--v2-accent-brand)" : "var(--v2-text-secondary)", fontWeight: active ? 600 : 500,
      fontFamily: "var(--v2-font-mono)",
    }}>{children}</button>
  );
}
const inputBase: React.CSSProperties = {
  background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)",
  color: "var(--v2-text-primary)", padding: "5px 8px", fontSize: 11,
  fontFamily: "var(--v2-font-mono)", borderRadius: 3, outline: "none", boxSizing: "border-box",
};
const selectStyle: React.CSSProperties = { ...inputBase, width: "100%" };
const chipBtn: React.CSSProperties = {
  display: "inline-flex", alignItems: "center", gap: 4, padding: "5px 9px", fontSize: 10.5,
  cursor: "pointer", borderRadius: 3, background: "transparent", border: "1px solid",
  fontFamily: "var(--v2-font-mono)", whiteSpace: "nowrap",
};
const iconBtn: React.CSSProperties = {
  background: "transparent", border: "none", color: "var(--v2-text-muted)", cursor: "pointer",
  padding: 2, display: "inline-flex", alignItems: "center",
};
function TimeInput({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder?: string }) {
  return <input value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} style={{ ...inputBase, width: "100%" }} />;
}
