import { useCallback, useEffect, useState } from "react";
import { isServerMode } from "@/lib/server-client";
import { fetchMe, loadCachedUser, type AuthUser } from "@/lib/auth-api";
import { LoginForm } from "./LoginForm";
import { fetchSelfServiceJobs, runSelfServiceJob, type SelfServiceJob } from "@/lib/differentials-api";
import { toast, ToastHost } from "./Toast";
import "./tokens.css";

/* ──────────────────────────────────────────────────────────────
   PortalView — D-14 Self-Service Portal (rota /portal)
   ──────────────────────────────────────────────────────────────
   A porta do NEGÓCIO: só os jobs com selfService:true, um botão
   Rodar por card, status ao vivo (poll leve). Nada de Design,
   nada de Monitoring — quem opera workflow não entra aqui, quem
   precisa "rodar o relatório de novo" não sai daqui.
   Mesmo login do Regente (viewer basta: o gate é o opt-in na def).
   ────────────────────────────────────────────────────────────── */

const STATUS_META: Record<string, { label: string; color: string }> = {
  OK: { label: "done", color: "var(--v2-status-ok)" },
  NOTOK: { label: "failed", color: "var(--v2-status-failed)" },
  RUNNING: { label: "running…", color: "var(--v2-status-running)" },
  WAITING: { label: "queued", color: "var(--v2-status-waiting)" },
  HELD: { label: "paused", color: "var(--v2-text-secondary)" },
  CANCELLED: { label: "cancelled", color: "var(--v2-text-muted)" },
};

export default function PortalView() {
  const [me, setMe] = useState<AuthUser | null>(() => loadCachedUser());
  // A1: em local mode não há auth a checar → inicia true (era set síncrono no efeito); ver roadmap §RH
  const [authChecked, setAuthChecked] = useState(() => !isServerMode());
  const [jobs, setJobs] = useState<SelfServiceJob[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [firing, setFiring] = useState<Set<string>>(new Set());

  useEffect(() => {
    if (!isServerMode()) return; // authChecked já inicia true em local mode (A1)
    fetchMe().then((u) => { setMe(u); setAuthChecked(true); }).catch(() => setAuthChecked(true));
  }, []);

  const refresh = useCallback(() => {
    fetchSelfServiceJobs()
      .then((js) => { setJobs(js); setLoaded(true); })
      .catch(() => setLoaded(true));
  }, []);

  useEffect(() => {
    if (!me) return;
    refresh();
    const t = setInterval(refresh, 5000); // poll leve: portal é tela de espera
    return () => clearInterval(t);
  }, [me, refresh]);

  const run = async (job: SelfServiceJob) => {
    setFiring((prev) => new Set(prev).add(job.id));
    try {
      await runSelfServiceJob(job.id);
      toast.success(`"${job.label}" fired`);
      refresh();
    } catch (e) {
      toast.error("Failed to fire", { detail: e instanceof Error ? e.message : String(e) });
    } finally {
      setFiring((prev) => { const next = new Set(prev); next.delete(job.id); return next; });
    }
  };

  if (!authChecked) return null;
  if (isServerMode() && !me) {
    return <LoginForm onLogin={(u) => setMe(u)} />;
  }

  return (
    <div style={{
      minHeight: "100vh", background: "var(--v2-bg-canvas, #0b0f1a)",
      color: "var(--v2-text-primary, #e2e8f0)", fontFamily: "var(--v2-font-sans, system-ui)",
      padding: "24px 16px",
    }}>
      <ToastHost />
      <div style={{ maxWidth: 720, margin: "0 auto" }}>
        <header style={{ display: "flex", alignItems: "baseline", gap: 12, marginBottom: 20 }}>
          <img className="app-logo" src="/logo-r.png" alt="" style={{ height: 28, width: "auto" }} />
          <h1 style={{ fontSize: 18, fontWeight: 600, letterSpacing: "-0.01em" }}>Portal self-service</h1>
          <span style={{ fontSize: 11, color: "var(--v2-text-muted, #64748b)" }}>
            {me ? `@${me.username}` : "local mode"}
          </span>
          <a href="/" style={{ marginLeft: "auto", fontSize: 11, color: "var(--v2-text-secondary, #94a3b8)" }}>
            ← console
          </a>
        </header>

        {!loaded ? (
          <p style={{ color: "var(--v2-text-muted, #64748b)", fontSize: 13 }}>loading…</p>
        ) : jobs.length === 0 ? (
          <div style={{
            border: "1px dashed var(--v2-border-medium, #334155)", borderRadius: 12,
            padding: 32, textAlign: "center", color: "var(--v2-text-muted, #64748b)", fontSize: 13,
          }}>
            No job exposed to the portal yet.<br />
            Ask the engineering team to mark <code style={{ fontFamily: "monospace" }}>selfService: true</code> on the
            jobs the business can fire.
          </div>
        ) : (
          <div style={{ display: "grid", gap: 10 }}>
            {jobs.map((job) => {
              const meta = STATUS_META[job.lastStatus ?? ""] ?? null;
              const busy = firing.has(job.id) || job.running;
              return (
                <div key={job.id} style={{
                  display: "flex", alignItems: "center", gap: 14, padding: "14px 16px",
                  background: "var(--v2-bg-surface, #111827)", borderRadius: 12,
                  border: "1px solid var(--v2-border-medium, #1f2937)", flexWrap: "wrap",
                }}>
                  <div style={{ flex: 1, minWidth: 200 }}>
                    <div style={{ fontSize: 14, fontWeight: 600 }}>{job.label}</div>
                    <div style={{ fontSize: 11, color: "var(--v2-text-muted, #64748b)", marginTop: 2 }}>
                      {job.description || job.id} · {job.team}
                    </div>
                  </div>
                  {meta && (
                    <span style={{
                      fontSize: 11, fontFamily: "var(--v2-font-mono, monospace)", color: meta.color,
                      display: "inline-flex", alignItems: "center", gap: 6,
                    }}>
                      <span style={{
                        width: 7, height: 7, borderRadius: "50%", background: meta.color,
                        animation: job.running ? "v2-dot-pulse 1.2s ease-in-out infinite" : "none",
                      }} />
                      {meta.label}
                    </span>
                  )}
                  <button
                    onClick={() => run(job)}
                    disabled={busy}
                    style={{
                      background: busy ? "var(--v2-bg-elevated, #1e293b)" : "var(--v2-accent-brand, #2563eb)",
                      color: busy ? "var(--v2-text-muted, #64748b)" : "#fff",
                      border: 0, borderRadius: 8, padding: "10px 22px", fontSize: 13, fontWeight: 600,
                      cursor: busy ? "default" : "pointer", minWidth: 96,
                    }}
                  >
                    {job.running ? "running…" : firing.has(job.id) ? "…" : "▶ Run"}
                  </button>
                </div>
              );
            })}
          </div>
        )}

        <p style={{ marginTop: 24, fontSize: 10.5, color: "var(--v2-text-muted, #475569)", textAlign: "center" }}>
          Jobs listados aqui foram aprovados pela engenharia (selfService no YAML, versionado no Git).
        </p>
      </div>
    </div>
  );
}
