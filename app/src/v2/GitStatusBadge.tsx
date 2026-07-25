// F13.1 + F13.4 — Badge de status Git no topbar.
//
// Mostra branch@shortSha + tempo desde último sync. Botão "↻" força sync.
// F13.4: drift detection — se remote avançou, mostra ⚠ amarelo + botão "Sync now".
// Quando git não configurado: nada renderiza (null).
import { useEffect, useState, useCallback } from "react";
import { ExternalLink } from "lucide-react";
import { fetchGitStatus, syncGit, checkDrift, type GitStatus } from "../lib/git-api";

export function GitStatusBadge({ canSync }: { canSync: boolean }) {
  const [status, setStatus] = useState<GitStatus | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const refresh = useCallback(() => {
    fetchGitStatus().then(setStatus).catch(() => {});
  }, []);

  useEffect(() => {
    let alive = true;
    refresh();
    // Poll status every 30s
    const t = setInterval(() => {
      if (alive) refresh();
    }, 30000);
    // Also poll drift every 45s (lightweight)
    const d = setInterval(() => {
      if (alive) checkDrift().then(setStatus).catch(() => {});
    }, 45000);
    // Listen WS drift broadcast
    function onDrift(e: Event) {
      const ce = e as CustomEvent<{ drift?: boolean }>;
      if (ce.detail?.drift) refresh();
    }
    window.addEventListener("regente:ws:git.drift", onDrift);
    return () => { alive = false; clearInterval(t); clearInterval(d); window.removeEventListener("regente:ws:git.drift", onDrift); };
  }, [refresh]);

  if (!status || !status.configured) return null;

  async function doSync() {
    setBusy(true);
    setErr(null);
    try {
      const s = await syncGit();
      setStatus(s);
    } catch (e: unknown) {
      setErr(e instanceof Error ? e.message : "sync failed");
    } finally {
      setBusy(false);
    }
  }

  const ago = status.lastSync ? humanAgo(status.lastSync) : "never";
  const drifted = !!status.drift;

  return (
    <div
      title={`source=${status.source}\nbranch=${status.branch}\nsha=${status.sha}\nlastSync=${status.lastSync}${drifted ? `\n⚠ DRIFT: remote=${status.remoteSha}` : ""}`}
      style={{
        display: "inline-flex", alignItems: "center", gap: 8,
        padding: "4px 10px",
        background: "#000",
        border: `1px solid ${drifted ? "#f59e0b" : "#262626"}`,
        borderRadius: 6,
        fontSize: 11,
        fontFamily: "monospace",
        color: "#a3a3a3",
      }}
    >
      <span style={{ color: drifted ? "#f59e0b" : "#11C76F" }}>git</span>
      {status.webUrl ? (
        <a
          href={status.sha ? `${status.webUrl}/commit/${status.sha}` : status.webUrl}
          target="_blank"
          rel="noreferrer"
          title="Open on GitHub"
          style={{ color: "#f5f5f5", textDecoration: "none", display: "inline-flex", alignItems: "center", gap: 4 }}
        >
          {status.branch}@{status.shortSha || "?"}
          <ExternalLink size={10} style={{ opacity: 0.6 }} />
        </a>
      ) : (
        <span style={{ color: "#f5f5f5" }}>{status.branch}@{status.shortSha || "?"}</span>
      )}
      {drifted ? (
        <>
          <span style={{ color: "#f59e0b", fontWeight: 600 }}>⚠ out of sync</span>
          {canSync && (
            <button
              onClick={doSync}
              disabled={busy}
              title="Remote has new commits — click to sync"
              style={{
                background: "rgba(245,158,11,0.15)", border: "1px solid #f59e0b",
                color: "#f59e0b", padding: "1px 8px", borderRadius: 4,
                cursor: busy ? "wait" : "pointer", fontSize: 11, fontWeight: 600,
              }}
            >
              {busy ? "syncing..." : "Sync now"}
            </button>
          )}
        </>
      ) : (
        <>
          <span style={{ opacity: 0.6 }}>· synced {ago}</span>
          {canSync && (
            <button
              onClick={doSync}
              disabled={busy}
              title="Force sync from remote"
              style={{
                background: "transparent", border: "1px solid #333",
                color: "#a3a3a3", padding: "1px 6px", borderRadius: 4,
                cursor: busy ? "wait" : "pointer", fontSize: 11,
              }}
            >
              {busy ? "..." : "↻"}
            </button>
          )}
        </>
      )}
      {err && <span style={{ color: "salmon" }}>{err}</span>}
    </div>
  );
}

function humanAgo(iso: string): string {
  const t = new Date(iso).getTime();
  if (!t || t < 10000) return "never";
  const sec = Math.floor((Date.now() - t) / 1000);
  if (sec < 60) return `${sec}s ago`;
  if (sec < 3600) return `${Math.floor(sec / 60)}m ago`;
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ago`;
  return `${Math.floor(sec / 86400)}d ago`;
}
