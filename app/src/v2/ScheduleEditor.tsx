/**
 * ScheduleEditor — editor visual de recorrência estilo Control-M.
 *
 * Cobre os 4 modos (decisão 2026-06-12):
 *   - weekly:      dias da semana (toggles)
 *   - monthly:     dias do mês (grid 1..31 + último dia)
 *   - businessday: N-ésimo dia útil do mês (chips, inclui último = -1)
 *   - advanced:    regra nomeada
 * + filtro de meses (opcional), horário (runAt), janela e cíclico.
 *
 * Edita um JobSchedule e emite via onChange. Sem estado próprio além de UI.
 */
import type { JobSchedule, ScheduleFrequency, AdvancedRule } from "@/lib/orchestrator-model";

const WEEKDAYS: Array<{ id: string; label: string }> = [
  { id: "mon", label: "Seg" }, { id: "tue", label: "Ter" }, { id: "wed", label: "Qua" },
  { id: "thu", label: "Qui" }, { id: "fri", label: "Sex" }, { id: "sat", label: "Sáb" }, { id: "sun", label: "Dom" },
];

const MONTHS = ["Jan","Fev","Mar","Abr","Mai","Jun","Jul","Ago","Set","Out","Nov","Dez"];

const FREQS: Array<{ id: ScheduleFrequency; label: string }> = [
  { id: "daily", label: "Diário" },
  { id: "weekly", label: "Dias da semana" },
  { id: "monthly", label: "Dias do mês" },
  { id: "businessday", label: "Dia útil" },
  { id: "advanced", label: "Regra avançada" },
];

const ADVANCED_RULES: Array<{ id: AdvancedRule; label: string }> = [
  { id: "first-businessday", label: "Primeiro dia útil do mês" },
  { id: "last-businessday", label: "Último dia útil do mês" },
  { id: "penultimate-businessday", label: "Penúltimo dia útil do mês" },
  { id: "first-businessday-not-monday", label: "1º dia útil que não for segunda" },
];

const NTH_QUICK = [1, 2, 3, 4, 5, 10, 15];

interface Props {
  value: JobSchedule;
  onChange: (next: JobSchedule) => void;
}

export default function ScheduleEditor({ value, onChange }: Props) {
  const s = value;
  const freq: ScheduleFrequency = s.frequency ?? "daily";
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
        <Hint>Roda todos os dias (sujeito aos calendars e ao filtro de meses abaixo).</Hint>
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

      {freq === "businessday" && (
        <Group label="N-ésimo dia útil do mês">
          <div style={{ display: "flex", flexWrap: "wrap", gap: 4, marginBottom: 6 }}>
            {NTH_QUICK.map((n) => (
              <Toggle key={n} small active={(s.nthBusinessDays ?? []).includes(n)}
                onClick={() => patch({ nthBusinessDays: toggleInArr(s.nthBusinessDays, n) })}>{n}º</Toggle>
            ))}
            <Toggle small wide active={(s.nthBusinessDays ?? []).includes(-1)}
              onClick={() => patch({ nthBusinessDays: toggleInArr(s.nthBusinessDays, -1) })}>último útil</Toggle>
          </div>
          <Hint>Dia útil = seg–sex menos feriados do calendar include do job (se houver).</Hint>
        </Group>
      )}

      {freq === "advanced" && (
        <Group label="Regra">
          <select value={s.advancedRule ?? ""} onChange={(e) => patch({ advancedRule: e.target.value as AdvancedRule })} style={selectStyle}>
            <option value="">— selecione —</option>
            {ADVANCED_RULES.map((r) => <option key={r.id} value={r.id}>{r.label}</option>)}
          </select>
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

      <SummaryLine schedule={s} />
    </div>
  );
}

/* ── Resumo legível (tipo "Run at 06:00, every weekday except weekends") ── */
function SummaryLine({ schedule: s }: { schedule: JobSchedule }) {
  const parts: string[] = [];
  const freq = s.frequency ?? "daily";
  if (freq === "daily") parts.push("todo dia");
  else if (freq === "weekly") parts.push(`toda ${(s.daysOfWeek ?? []).map(wd).join(", ") || "(sem dias)"}`);
  else if (freq === "monthly") parts.push(`dia ${(s.daysOfMonth ?? []).map((d) => d === -1 ? "último" : d).join(", ") || "(sem dias)"}`);
  else if (freq === "businessday") parts.push(`${(s.nthBusinessDays ?? []).map((d) => d === -1 ? "último útil" : `${d}º útil`).join(", ") || "(sem dias)"}`);
  else if (freq === "advanced") parts.push(ADVANCED_RULES.find((r) => r.id === s.advancedRule)?.label ?? "(regra não escolhida)");
  if ((s.monthsOfYear ?? []).length) parts.push(`em ${(s.monthsOfYear ?? []).map((m) => MONTHS[m - 1]).join(", ")}`);
  if (s.runAt) parts.push(`às ${s.runAt}`);
  if (s.cyclic && s.intervalMin) parts.push(`a cada ${s.intervalMin}min`);
  if (s.keepActive && s.keepActive > 0) parts.push(`keep active ${s.keepActive}d`);
  return (
    <div style={{ fontSize: 11, color: "var(--v2-accent-brand)", fontFamily: "var(--v2-font-mono)", background: "var(--v2-accent-faint)", border: "1px solid var(--v2-accent-dark)", borderRadius: 4, padding: "6px 8px" }}>
      {parts.join(" · ")}
    </div>
  );
}
function wd(id: string) { return WEEKDAYS.find((w) => w.id === id)?.label ?? id; }

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
function TimeInput({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder?: string }) {
  return <input value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} style={{ ...inputBase, width: "100%" }} />;
}
