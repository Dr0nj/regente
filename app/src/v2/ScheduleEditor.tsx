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
import { useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import type { JobSchedule, ScheduleFrequency, CalendarRef } from "@/lib/orchestrator-model";
import type { Calendar } from "@/lib/bloco2-api";

const WEEKDAYS: Array<{ id: string; label: string }> = [
  { id: "mon", label: "Seg" }, { id: "tue", label: "Ter" }, { id: "wed", label: "Qua" },
  { id: "thu", label: "Qui" }, { id: "fri", label: "Sex" }, { id: "sat", label: "Sáb" }, { id: "sun", label: "Dom" },
];

const MONTHS = ["Jan","Fev","Mar","Abr","Mai","Jun","Jul","Ago","Set","Out","Nov","Dez"];

const FREQS: Array<{ id: ScheduleFrequency; label: string }> = [
  { id: "daily", label: "Diário" },
  { id: "weekly", label: "Dias da semana" },
  { id: "monthly", label: "Dias do mês" },
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

  const toggleInArr = <T,>(arr: T[] | undefined, v: T): T[] => {
    const set = new Set(arr ?? []);
    if (set.has(v)) set.delete(v); else set.add(v);
    return [...set];
  };

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      {/* Frequência (segmented) */}
      <Group label="Frequência">
        <div style={{ display: "flex", flexWrap: "wrap", gap: 4 }}>
          {FREQS.map((f) => (
            <Seg key={f.id} active={freq === f.id} onClick={() => patch({ frequency: f.id })}>{f.label}</Seg>
          ))}
        </div>
      </Group>

      {/* Corpo condicional por frequência */}
      {freq === "daily" && (
        <Hint>Roda todos os dias (sujeito aos calendários e ao filtro de meses abaixo).</Hint>
      )}

      {freq === "weekly" && (
        <Group label="Dias da semana">
          <div style={{ display: "flex", gap: 4 }}>
            {WEEKDAYS.map((d) => (
              <Toggle key={d.id} active={(s.daysOfWeek ?? []).includes(d.id)}
                onClick={() => patch({ daysOfWeek: toggleInArr(s.daysOfWeek, d.id) })}>{d.label}</Toggle>
            ))}
          </div>
        </Group>
      )}

      {freq === "monthly" && (
        <Group label="Dias do mês">
          <div style={{ display: "grid", gridTemplateColumns: "repeat(7, 1fr)", gap: 3 }}>
            {Array.from({ length: 31 }, (_, i) => i + 1).map((day) => (
              <Toggle key={day} small active={(s.daysOfMonth ?? []).includes(day)}
                onClick={() => patch({ daysOfMonth: toggleInArr(s.daysOfMonth, day) })}>{day}</Toggle>
            ))}
            <Toggle small wide active={(s.daysOfMonth ?? []).includes(-1)}
              onClick={() => patch({ daysOfMonth: toggleInArr(s.daysOfMonth, -1) })}>último</Toggle>
          </div>
        </Group>
      )}

      {/* Filtro de meses (opcional, vale para todas as frequências) */}
      <Group label="Meses (vazio = todos)">
        <div style={{ display: "grid", gridTemplateColumns: "repeat(6, 1fr)", gap: 3 }}>
          {MONTHS.map((m, i) => (
            <Toggle key={m} small active={(s.monthsOfYear ?? []).includes(i + 1)}
              onClick={() => patch({ monthsOfYear: toggleInArr(s.monthsOfYear, i + 1) })}>{m}</Toggle>
          ))}
        </div>
      </Group>

      {/* Calendários — trabalham junto com as regras (include/exclude) */}
      <CalendarSection calendars={calendars} onChange={onCalendarsChange} available={availableCalendars} />

      {/* Horário / janela / cíclico */}
      <Group label="Horário">
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr", gap: 8 }}>
          <Sub label="Roda às (HH:MM)"><TimeInput value={s.runAt ?? ""} onChange={(v) => patch({ runAt: v })} placeholder="06:00" /></Sub>
          <Sub label="Janela de"><TimeInput value={s.windowFrom ?? ""} onChange={(v) => patch({ windowFrom: v })} placeholder="--:--" /></Sub>
          <Sub label="Janela até"><TimeInput value={s.windowTo ?? ""} onChange={(v) => patch({ windowTo: v })} placeholder="--:--" /></Sub>
        </div>
        <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 11, color: "var(--v2-text-secondary)", marginTop: 8 }}>
          <input type="checkbox" checked={!!s.cyclic} onChange={(e) => patch({ cyclic: e.target.checked })} />
          Cíclico — repete a cada
          <input type="number" min={1} value={s.intervalMin ?? 0} disabled={!s.cyclic}
            onChange={(e) => patch({ intervalMin: Number(e.target.value) || 0 })}
            style={{ width: 56, ...inputBase, opacity: s.cyclic ? 1 : 0.4 }} />
          min (dentro da janela)
        </label>
      </Group>

      {/* Ciclo de vida na diária — carry-over Control-M (Keep Active) */}
      <Group label="Ciclo de vida na diária">
        <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 11, color: "var(--v2-text-secondary)" }}>
          Keep Active — sobrevive
          <input type="number" min={0} value={s.keepActive ?? 0}
            onChange={(e) => patch({ keepActive: Math.max(0, Number(e.target.value) || 0) })}
            style={{ width: 56, ...inputBase }} />
          diárias extras se não terminar OK
        </label>
        <Hint>
          RUNNING e HELD atravessam a virada sempre. 0 = padrão (um NOTOK não-tratado já
          persiste +1 diária). N &gt; 0 mantém o job por N diárias mesmo sem rodar/terminar OK.
        </Hint>
      </Group>

      {/* PREVIEW — embaixo de todas as regras (frequência + meses + calendários + horário) */}
      <SchedulePreview schedule={s} calendars={calendars} calDefs={availableCalendars} />
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
    <Group label="Calendários (trabalham junto com as regras)">
      <Hint>
        <b>Incluir</b> = roda só nos dias do calendário. <b>Negar</b> = NÃO roda nesses dias.
        Combine com as regras acima — ex.: negar "dias úteis" + marcar todos os dias = roda todo dia, exceto dia útil.
      </Hint>
      <div style={{ display: "flex", gap: 6, marginTop: 6 }}>
        <select value={pick} onChange={(e) => setPick(e.target.value)} style={{ ...selectStyle, flex: 1 }}>
          <option value="">— calendário —</option>
          {available.filter((c) => !calendars.some((cr) => cr.name === c.name)).map((c) => (
            <option key={c.name} value={c.name}>{c.name}</option>
          ))}
        </select>
        <button onClick={() => add("include")} disabled={!pick} style={{ ...chipBtn, borderColor: "var(--v2-accent-brand)", color: "var(--v2-accent-brand)" }}><Plus size={11} /> incluir</button>
        <button onClick={() => add("exclude")} disabled={!pick} style={{ ...chipBtn, borderColor: "#7f1d1d", color: "#fca5a5" }}><Plus size={11} /> negar</button>
      </div>
      {available.length === 0 && <Hint>Nenhum calendário criado ainda. Crie em Control-M Panel → Calendars.</Hint>}
      <div style={{ display: "flex", flexDirection: "column", gap: 4, marginTop: 6 }}>
        {calendars.map((c, i) => (
          <div key={`${c.name}-${c.mode}`} style={{ display: "flex", alignItems: "center", gap: 8, padding: "6px 8px", background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)", borderRadius: 3 }}>
            <span style={{ fontSize: 9, fontWeight: 700, textTransform: "uppercase", padding: "1px 6px", borderRadius: 2, fontFamily: "var(--v2-font-mono)",
              background: c.mode === "exclude" ? "#3b1d1d" : "var(--v2-accent-deep)", color: c.mode === "exclude" ? "#fca5a5" : "var(--v2-accent-brand)",
              border: `1px solid ${c.mode === "exclude" ? "#7f1d1d" : "var(--v2-accent-dark)"}` }}>
              {c.mode === "exclude" ? "negar" : "incluir"}
            </span>
            <div style={{ flex: 1, display: "flex", flexDirection: "column", gap: 1 }}>
              <span style={{ fontSize: 12, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-primary)" }}>{c.name}</span>
              <span style={{ fontSize: 10, color: "var(--v2-text-muted)" }}>{describeCalendar(calByName(c.name))}</span>
            </div>
            <button onClick={() => remove(i)} style={iconBtn} title="Remover"><Trash2 size={12} /></button>
          </div>
        ))}
      </div>
    </Group>
  );
}

/* ── Tradução do COMPORTAMENTO de um calendário (o que ele significa) ── */
const WD_PT: Record<string, string> = { mon: "seg", tue: "ter", wed: "qua", thu: "qui", fri: "sex", sat: "sáb", sun: "dom" };
function describeCalendar(cal?: Calendar): string {
  if (!cal) return "calendário não encontrado";
  const parts: string[] = [];
  const bd = (cal.businessDays ?? []).map((d) => d.toLowerCase().slice(0, 3));
  if (bd.length) {
    const isMonFri = ["mon", "tue", "wed", "thu", "fri"].every((d) => bd.includes(d)) && !bd.includes("sat") && !bd.includes("sun");
    parts.push(isMonFri ? "seg–sex" : bd.map((d) => WD_PT[d] ?? d).join(", "));
  }
  if ((cal.holidays ?? []).length) parts.push(`menos ${cal.holidays!.length} feriado${cal.holidays!.length > 1 ? "s" : ""}`);
  if ((cal.excludeDates ?? []).length) parts.push(`menos ${cal.excludeDates!.length} data(s)`);
  if ((cal.includeDates ?? []).length) parts.push(`+ ${cal.includeDates!.length} exceção(ões)`);
  return parts.length ? parts.join(", ") : "sem dias definidos";
}

/* ── Preview legível: frequência + meses + calendários + horário ── */
function SchedulePreview({ schedule: s, calendars, calDefs }: { schedule: JobSchedule; calendars: CalendarRef[]; calDefs: Calendar[] }) {
  const freq = (s.frequency === "weekly" || s.frequency === "monthly") ? s.frequency : "daily";
  const calByName = (n: string) => calDefs.find((c) => c.name === n);

  // base (frequência)
  let base: string;
  if (freq === "weekly") {
    const days = s.daysOfWeek ?? [];
    if (days.length === 0) base = "(nenhum dia da semana marcado)";
    else if (days.length === 7) base = "todos os dias";
    else base = `toda ${days.map((d) => WD_PT[d] ?? d).join(", ")}`;
  } else if (freq === "monthly") {
    const days = s.daysOfMonth ?? [];
    base = days.length ? `no dia ${days.map((d) => (d === -1 ? "último" : d)).join(", ")} do mês` : "(nenhum dia do mês marcado)";
  } else {
    base = "todos os dias";
  }

  // calendários
  const inc = calendars.filter((c) => c.mode === "include");
  const exc = calendars.filter((c) => c.mode === "exclude");
  const calPhrase = (c: CalendarRef) => {
    const d = describeCalendar(calByName(c.name));
    return `${c.name}${d && d !== "sem dias definidos" && d !== "calendário não encontrado" ? ` (${d})` : ""}`;
  };

  const parts: string[] = [base];
  if (inc.length) parts.push(`só nos dias de ${inc.map(calPhrase).join(" e ")}`);
  if (exc.length) parts.push(`exceto ${exc.map(calPhrase).join(" e ")}`);
  if ((s.monthsOfYear ?? []).length) parts.push(`apenas em ${(s.monthsOfYear ?? []).map((m) => MONTHS[m - 1]).join(", ")}`);
  if (s.runAt) parts.push(`às ${s.runAt}`);
  if (s.cyclic && s.intervalMin) parts.push(`repetindo a cada ${s.intervalMin}min`);
  if (s.keepActive && s.keepActive > 0) parts.push(`keep active ${s.keepActive}d`);

  return (
    <div>
      <div style={{ fontSize: 9, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--v2-text-muted)", marginBottom: 6 }}>Preview</div>
      <div style={{ fontSize: 12, color: "var(--v2-accent-brand)", lineHeight: 1.5, background: "var(--v2-accent-faint)", border: "1px solid var(--v2-accent-dark)", borderRadius: 4, padding: "8px 10px" }}>
        Roda {parts.join(", ")}.
        {!s.enabled && <span style={{ color: "var(--v2-text-muted)" }}> (schedule desabilitado)</span>}
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
