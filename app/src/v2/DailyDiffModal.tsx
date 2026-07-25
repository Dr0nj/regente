import { useEffect, useState } from "react";
import { fetchDailyDiff, type DailyDiff, type DiffDefChange } from "@/lib/runtime-bridge";

/* ──────────────────────────────────────────────────────────────
   DailyDiffView — "o que mudou entre duas diárias?"
   Diferencial Git-native: compara snapshots congelados (commit_sha +
   definition_snapshot) de dois order_date. Default: hoje vs a diária anterior.
   Vive como aba do Control Panel (painel inline; o X é do Control Panel).
   ────────────────────────────────────────────────────────────── */

function truncate(s: string, n = 80): string {
  if (!s) return "∅";
  return s.length > n ? s.slice(0, n) + "…" : s;
}

export default function DailyDiffModal() {
  const [diff, setDiff] = useState<DailyDiff | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetchDailyDiff()
      .then((d) => { if (!cancelled) { setDiff(d); setError(d ? null : "Diff unavailable in local mode."); } })
      .catch((e) => { if (!cancelled) setError(e?.message ?? String(e)); })
      .finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, []);

  const c = diff?.counts;

  return (
      <div
        style={{
          width: "100%", height: "100%",
          display: "flex", flexDirection: "column",
          background: "var(--v2-bg-surface)", borderRadius: 6,
          fontFamily: "var(--v2-font-sans)", color: "var(--v2-text-primary)",
          overflow: "hidden",
        }}
      >
        {/* header */}
        <div style={{ padding: "12px 16px", borderBottom: "1px solid var(--v2-border-subtle)", display: "flex", alignItems: "center", gap: 10 }}>
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 13, fontWeight: 700 }}>Daily Diff</div>
            <div style={{ fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)", marginTop: 2 }}>
              {diff ? `${diff.dateA} → ${diff.dateB}` : "comparing…"}
              {diff?.folder ? ` · folder ${diff.folder}` : ""}
              {diff?.sameCommit ? " · same commit (identical defs)" : diff?.commitB ? ` · commit ${diff.commitB.slice(0, 7)}` : ""}
            </div>
          </div>
        </div>

        {/* counts */}
        {c && (
          <div style={{ display: "flex", gap: 0, borderBottom: "1px solid var(--v2-border-subtle)", fontFamily: "var(--v2-font-mono)" }}>
            <Count label="added" n={c.added} color="var(--v2-status-ok)" />
            <Count label="removed" n={c.removed} color="var(--v2-status-failed)" />
            <Count label="changed" n={c.changed} color="var(--v2-status-waiting)" />
            <Count label="same" n={c.unchanged} color="var(--v2-text-muted)" />
          </div>
        )}

        {/* body */}
        <div style={{ flex: 1, overflowY: "auto", padding: "12px 16px" }}>
          {loading && <Muted>evaluating the diff…</Muted>}
          {error && <div style={{ color: "var(--v2-status-failed)", fontSize: 11, fontFamily: "var(--v2-font-mono)" }}>{error}</div>}
          {diff && !loading && (
            <>
              {c && c.added + c.removed + c.changed === 0 && <Muted>No difference — both dailies have the same definitions.</Muted>}

              <DefList title="＋ Added" color="var(--v2-status-ok)" items={diff.added.map((d) => ({ defId: d.defId, label: d.label, team: d.team }))} />
              <DefList title="－ Removed" color="var(--v2-status-failed)" items={diff.removed.map((d) => ({ defId: d.defId, label: d.label, team: d.team }))} />

              {diff.changed.length > 0 && (
                <Section title="✎ Changed" color="var(--v2-status-waiting)">
                  {diff.changed.map((ch) => <ChangedRow key={ch.defId} ch={ch} />)}
                </Section>
              )}

              {diff.truncated && <Muted>… list truncated (the counters above are exact).</Muted>}
            </>
          )}
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

function Section({ title, color, children }: { title: string; color: string; children: React.ReactNode }) {
  return (
    <div style={{ marginBottom: 14 }}>
      <div style={{ fontSize: 10, fontFamily: "var(--v2-font-mono)", color, letterSpacing: "0.06em", textTransform: "uppercase", fontWeight: 700, marginBottom: 6 }}>{title}</div>
      {children}
    </div>
  );
}

function DefList({ title, color, items }: { title: string; color: string; items: { defId: string; label?: string; team?: string }[] }) {
  if (items.length === 0) return null;
  return (
    <Section title={title} color={color}>
      {items.map((it) => (
        <div key={it.defId} style={{ display: "flex", gap: 8, alignItems: "baseline", padding: "3px 0", borderBottom: "1px dashed var(--v2-border-subtle)" }}>
          <span style={{ fontSize: 11, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-primary)" }}>{it.defId}</span>
          {it.label && <span style={{ fontSize: 10, color: "var(--v2-text-muted)" }}>{it.label}</span>}
          {it.team && <span style={{ fontSize: 9, fontFamily: "var(--v2-font-mono)", color: "var(--v2-accent-brand)", marginLeft: "auto" }}>{it.team}</span>}
        </div>
      ))}
    </Section>
  );
}

function ChangedRow({ ch }: { ch: DiffDefChange }) {
  return (
    <div style={{ padding: "6px 0", borderBottom: "1px dashed var(--v2-border-subtle)" }}>
      <div style={{ display: "flex", gap: 8, alignItems: "baseline" }}>
        <span style={{ fontSize: 11, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-primary)" }}>{ch.defId}</span>
        {ch.label && <span style={{ fontSize: 10, color: "var(--v2-text-muted)" }}>{ch.label}</span>}
        {ch.team && <span style={{ fontSize: 9, fontFamily: "var(--v2-font-mono)", color: "var(--v2-accent-brand)", marginLeft: "auto" }}>{ch.team}</span>}
      </div>
      <div style={{ marginTop: 4, display: "flex", flexDirection: "column", gap: 3 }}>
        {ch.changes.map((f, i) => (
          <div key={i} style={{ fontSize: 10, fontFamily: "var(--v2-font-mono)", display: "flex", gap: 6, flexWrap: "wrap", alignItems: "baseline" }}>
            <span style={{ color: "var(--v2-status-waiting)", minWidth: 80 }}>{f.field}</span>
            <span style={{ color: "var(--v2-status-failed)", textDecoration: "line-through", opacity: 0.8 }}>{truncate(f.from)}</span>
            <span style={{ color: "var(--v2-text-muted)" }}>→</span>
            <span style={{ color: "var(--v2-status-ok)" }}>{truncate(f.to)}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
