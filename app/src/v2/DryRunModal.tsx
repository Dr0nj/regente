import { useEffect, useState, useCallback } from "react";
import { fetchDryRun, type DryRun, type DryRunJob } from "@/lib/runtime-bridge";

/* ──────────────────────────────────────────────────────────────
   DryRunModal — "simular a daily de uma data futura, sem materializar"
   Quem RODA · quem ESPERA (depois de quem) · quem NUNCA dispara (e por quê).
   Reusa IsScheduledOn (a mesma decisão do RunDaily) no servidor.
   ────────────────────────────────────────────────────────────── */

function tomorrow(): string {
  const d = new Date();
  d.setDate(d.getDate() + 1);
  return d.toISOString().slice(0, 10);
}

const OUTCOME: Record<DryRunJob["outcome"], { label: string; color: string }> = {
  RUN:           { label: "RODA", color: "var(--v2-status-ok)" },
  WAIT:          { label: "ESPERA", color: "var(--v2-status-waiting)" },
  BLOCKED:       { label: "NUNCA", color: "var(--v2-status-failed)" },
  NOT_SCHEDULED: { label: "FORA", color: "var(--v2-text-muted)" },
};
const ORDER: DryRunJob["outcome"][] = ["BLOCKED", "WAIT", "RUN", "NOT_SCHEDULED"];

export default function DryRunModal() {
  const [date, setDate] = useState<string>(tomorrow());
  const [dr, setDr] = useState<DryRun | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback((d: string) => {
    setLoading(true);
    setError(null);
    fetchDryRun(d)
      .then((r) => { setDr(r); if (!r) setError("Dry Run indisponível no modo local."); })
      .catch((e) => setError(e?.message ?? String(e)))
      .finally(() => setLoading(false));
  }, []);

  // eslint-disable-next-line react-hooks/set-state-in-effect -- load() reseta loading/error de forma síncrona ao trocar de data (feedback de troca); reset-on-dep-change legítimo (não mount); ver roadmap §RH
  useEffect(() => { load(date); }, [date, load]);

  const c = dr?.counts;
  // agrupa por outcome na ordem ORDER
  const grouped = ORDER.map((o) => ({ o, jobs: (dr?.jobs ?? []).filter((j) => j.outcome === o) })).filter((g) => g.jobs.length > 0);

  return (
      <div
        style={{ width: "100%", height: "100%", display: "flex", flexDirection: "column", background: "var(--v2-bg-surface)", borderRadius: 6, fontFamily: "var(--v2-font-sans)", color: "var(--v2-text-primary)", overflow: "hidden" }}
      >
        {/* header */}
        <div style={{ padding: "12px 16px", borderBottom: "1px solid var(--v2-border-subtle)", display: "flex", alignItems: "center", gap: 12 }}>
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 13, fontWeight: 700 }}>Dry Run — simular daily</div>
            <div style={{ fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)", marginTop: 2 }}>
              quem roda · quem espera · quem nunca dispara — sem materializar
            </div>
          </div>
          <input
            type="date"
            value={date}
            onChange={(e) => setDate(e.target.value)}
            style={{ background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-medium)", color: "var(--v2-text-primary)", padding: "5px 8px", fontSize: 11, fontFamily: "var(--v2-font-mono)", borderRadius: 3, colorScheme: "dark" }}
          />
        </div>

        {/* counts */}
        {c && (
          <div style={{ display: "flex", borderBottom: "1px solid var(--v2-border-subtle)", fontFamily: "var(--v2-font-mono)" }}>
            <Count label="roda" n={c.run} color="var(--v2-status-ok)" />
            <Count label="espera" n={c.wait} color="var(--v2-status-waiting)" />
            <Count label="nunca" n={c.blocked} color="var(--v2-status-failed)" />
            <Count label="fora da daily" n={c.notScheduled} color="var(--v2-text-muted)" />
          </div>
        )}

        {/* body */}
        <div style={{ flex: 1, overflowY: "auto", padding: "12px 16px" }}>
          {loading && <Muted>simulando {date}…</Muted>}
          {error && <div style={{ color: "var(--v2-status-failed)", fontSize: 11, fontFamily: "var(--v2-font-mono)" }}>{error}</div>}
          {!loading && dr && dr.jobs.length === 0 && <Muted>Nenhum job habilitado pra simular.</Muted>}
          {!loading && grouped.map((g) => (
            <div key={g.o} style={{ marginBottom: 14 }}>
              <div style={{ fontSize: 10, fontFamily: "var(--v2-font-mono)", color: OUTCOME[g.o].color, letterSpacing: "0.06em", textTransform: "uppercase", fontWeight: 700, marginBottom: 6 }}>
                {OUTCOME[g.o].label} · {g.jobs.length}
              </div>
              {g.jobs.map((j) => (
                <div key={j.defId} style={{ padding: "4px 0", borderBottom: "1px dashed var(--v2-border-subtle)" }}>
                  <div style={{ display: "flex", gap: 8, alignItems: "baseline" }}>
                    <span style={{ fontSize: 11, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-primary)" }}>{j.defId}</span>
                    {j.label && <span style={{ fontSize: 10, color: "var(--v2-text-muted)" }}>{j.label}</span>}
                    {j.team && <span style={{ fontSize: 9, fontFamily: "var(--v2-font-mono)", color: "var(--v2-accent-brand)", marginLeft: "auto" }}>{j.team}</span>}
                  </div>
                  {(j.outcome === "BLOCKED" || j.outcome === "WAIT") && (
                    <div style={{ fontSize: 10, color: OUTCOME[j.outcome].color, fontFamily: "var(--v2-font-mono)", marginTop: 1 }}>{j.reason}</div>
                  )}
                </div>
              ))}
            </div>
          ))}
          {dr?.truncated && <Muted>… lista truncada (contadores acima são exatos).</Muted>}
        </div>
      </div>
  );
}

function Count({ label, n, color }: { label: string; n: number; color: string }) {
  return (
    <div style={{ flex: 1, padding: "9px 6px", textAlign: "center", borderRight: "1px solid var(--v2-border-subtle)" }}>
      <div style={{ fontSize: 18, fontWeight: 700, color }}>{n}</div>
      <div style={{ fontSize: 8.5, color: "var(--v2-text-muted)", letterSpacing: "0.06em", textTransform: "uppercase" }}>{label}</div>
    </div>
  );
}
function Muted({ children }: { children: React.ReactNode }) {
  return <div style={{ color: "var(--v2-text-muted)", fontSize: 11, fontFamily: "var(--v2-font-mono)", padding: "8px 0" }}>{children}</div>;
}
