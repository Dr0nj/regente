// F20 — Admin settings dialog.
import { useEffect, useState } from "react";
import { getSettings, putSettings, type ServerSettings } from "../lib/settings-api";
import { fetchGitStatus, setGitToken, clearGitToken, cleanupGitDB, setWebhookSecret, type GitStatus } from "../lib/git-api";
import { invalidateGitInfo } from "../lib/git-info";
import AgentsManager from "./AgentsManager";
import { THEMES, getThemeId, applyTheme, type ThemeId, type ThemeDef } from "../lib/theme";

interface Props {
  onClose: () => void;
}

// Amostra de cores do tema (3 faixas) mostrada nos cards de seleção.
function ThemeSwatch({ colors, size = 40 }: { colors: ThemeDef["flag"]; size?: number }) {
  const h = Math.round((size * 7) / 10);
  return (
    <svg width={size} height={h} viewBox="0 0 60 42"
      style={{ borderRadius: 4, display: "block", flexShrink: 0, border: "1px solid rgba(255,255,255,0.08)" }}
      aria-hidden="true">
      <rect x="0" width="20" height="42" fill={colors.field} />
      <rect x="20" width="20" height="42" fill={colors.rhombus} />
      <rect x="40" width="20" height="42" fill={colors.disc} />
    </svg>
  );
}

export function SettingsDialog({ onClose }: Props) {
  const [settings, setSettings] = useState<ServerSettings>({});
  const [envLabel, setEnvLabel] = useState("");
  const [saving, setSaving] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [theme, setTheme] = useState<ThemeId>(getThemeId());
  const [tab, setTab] = useState<"general" | "themes" | "agents">("general");
  const [minimap, setMinimap] = useState<boolean>(() => typeof window !== "undefined" && window.localStorage.getItem("regente:minimap") === "1");
  const lsInt = (k: string, def: number) => {
    const v = typeof window !== "undefined" ? parseInt(window.localStorage.getItem(k) ?? "", 10) : NaN;
    return Number.isFinite(v) ? v : def;
  };
  const [layoutCols, setLayoutCols] = useState<number>(() => lsInt("regente:layoutCols", 10));
  const [layoutMaxRows, setLayoutMaxRows] = useState<number>(() => lsInt("regente:layoutMaxRows", 30));
  const writeLayout = (cols: number, rows: number) => {
    try {
      window.localStorage.setItem("regente:layoutCols", String(cols));
      window.localStorage.setItem("regente:layoutMaxRows", String(rows));
    } catch { /* ignore */ }
    window.dispatchEvent(new Event("regente:layout-changed"));
  };
  // UI-1 — cap do grafo do Monitoring (quantos jobs o ReactFlow desenha; a lista
  // ACTIVE JOBS e o ViewPoint mostram o dia inteiro independente disto).
  const [legacyCapVal, setLegacyCapVal] = useState<number>(() => lsInt("regente:legacyCap", 2000));
  const writeLegacyCap = (v: number) => {
    try {
      window.localStorage.setItem("regente:legacyCap", String(v));
    } catch { /* ignore */ }
    window.dispatchEvent(new Event("regente:layout-changed"));
  };

  // GitHub token
  const [git, setGit] = useState<GitStatus | null>(null);
  const [tokenInput, setTokenInput] = useState("");
  const [tokenBusy, setTokenBusy] = useState(false);
  const [tokenMsg, setTokenMsg] = useState<{ kind: "ok" | "err"; text: string } | null>(null);
  const [cleanBusy, setCleanBusy] = useState(false);
  const [webhookInput, setWebhookInput] = useState("");

  // Horário da daily (settings.daily_at) — o SERVER roda a daily neste horário,
  // pelo relógio DELE (vazio = default 00:00, meia-noite).
  const [dailyAt, setDailyAt] = useState("");
  // E1 — timezone de NEGÓCIO da daily (settings.daily_timezone, nome IANA).
  // Vazio = relógio local do server. Nome inválido: o server loga e cai no local.
  const [dailyTz, setDailyTz] = useState("");
  // ADV-5 — retenção/archives de instances (0/vazio = infinito) + diretório dos NDJSON.
  const [retentionDays, setRetentionDays] = useState("");
  const [archiveDir, setArchiveDir] = useState("");

  useEffect(() => {
    getSettings().then((s) => {
      setSettings(s);
      setEnvLabel(s.env_label ?? "");
      setDailyAt(s.daily_at ?? "");
      setDailyTz(s.daily_timezone ?? "");
      setRetentionDays(s.instance_retention_days ?? "");
      setArchiveDir(s.archive_dir ?? "");
      setLoaded(true);
    });
    fetchGitStatus().then(setGit).catch(() => {});
  }, []);

  async function handleSave() {
    setSaving(true);
    try {
      const updated = await putSettings({
        ...settings, env_label: envLabel, daily_at: dailyAt, daily_timezone: dailyTz.trim(),
        instance_retention_days: retentionDays.trim(), archive_dir: archiveDir.trim(),
      });
      setSettings(updated);
      onClose(); // salva e fecha o diálogo (o tema já aplica na hora ao selecionar)
    } finally {
      setSaving(false);
    }
  }

  async function handleSaveToken() {
    if (!tokenInput.trim()) return;
    setTokenBusy(true); setTokenMsg(null);
    try {
      const st = await setGitToken(tokenInput.trim());
      setGit(st); setTokenInput(""); invalidateGitInfo();
      setTokenMsg({ kind: "ok", text: "Token saved and validated. Push/PR enabled." });
    } catch (e) {
      setTokenMsg({ kind: "err", text: e instanceof Error ? e.message : String(e) });
    } finally {
      setTokenBusy(false);
    }
  }

  async function handleClearToken() {
    setTokenBusy(true); setTokenMsg(null);
    try {
      const st = await clearGitToken();
      setGit(st); invalidateGitInfo();
      setTokenMsg({ kind: "ok", text: "Token removed. Read-only mode." });
    } catch (e) {
      setTokenMsg({ kind: "err", text: e instanceof Error ? e.message : String(e) });
    } finally {
      setTokenBusy(false);
    }
  }

  async function handleSaveWebhook() {
    setTokenBusy(true); setTokenMsg(null);
    try {
      const st = await setWebhookSecret(webhookInput.trim());
      setGit(st); setWebhookInput("");
      setTokenMsg({ kind: "ok", text: webhookInput.trim() ? "Webhook secret saved. Configure the same one on GitHub." : "Webhook secret removed." });
    } catch (e) {
      setTokenMsg({ kind: "err", text: e instanceof Error ? e.message : String(e) });
    } finally {
      setTokenBusy(false);
    }
  }

  async function handleCleanupDB() {
    setCleanBusy(true); setTokenMsg(null);
    try {
      const r = await cleanupGitDB();
      setTokenMsg({
        kind: "ok",
        text: r.changed ? "DB removed from the repository (origin is clean)." : "The repository was already clean.",
      });
    } catch (e) {
      setTokenMsg({ kind: "err", text: e instanceof Error ? e.message : String(e) });
    } finally {
      setCleanBusy(false);
    }
  }

  return (
    <div
      style={{
        position: "fixed", inset: 0, zIndex: 200,
        background: "rgba(0,0,0,0.7)", display: "flex", alignItems: "center", justifyContent: "center",
      }}
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div
        className="v2-grain-card v2-neon-card"
        style={{
          width: 440, maxHeight: "80vh", overflow: "auto",
          padding: 24, display: "flex", flexDirection: "column", gap: 20,
        }}
      >
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <h2 style={{ fontSize: 15, fontWeight: 600, margin: 0 }}>Settings</h2>
          <button onClick={onClose} className="v2-dialog-x" aria-label="Close">✕</button>
        </div>

        {!loaded ? (
          <span style={{ fontSize: 12, color: "var(--v2-text-muted)" }}>Loading...</span>
        ) : (
          <>
            {/* Sub-abas: Geral | Temas */}
            <div style={{ display: "flex", gap: 4, padding: 4, background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-subtle)", borderRadius: 10 }}>
              {([["general", "General"], ["agents", "Agents"], ["themes", "Themes"]] as const).map(([id, label]) => (
                <button
                  key={id}
                  onClick={() => setTab(id)}
                  style={{
                    flex: 1, padding: "7px 12px", borderRadius: 7, border: "none", cursor: "pointer",
                    fontSize: 12, fontWeight: tab === id ? 700 : 500,
                    background: tab === id ? "var(--v2-accent-brand)" : "transparent",
                    color: tab === id ? "var(--v2-bg-canvas)" : "var(--v2-text-secondary)",
                    transition: "background 140ms, color 140ms",
                  }}
                >{label}</button>
              ))}
            </div>

            {tab === "themes" && (
            <fieldset style={{ border: "1px solid var(--v2-border-medium)", borderRadius: 6, padding: "12px 14px" }}>
              <legend style={{ fontSize: 11, fontWeight: 600, color: "var(--v2-text-secondary)", padding: "0 4px" }}>
                Theme
              </legend>
              <div style={{ fontSize: 10, color: "var(--v2-text-muted)", marginBottom: 10 }}>
                Pick the look. It applies right away and is saved in this browser.
              </div>
              <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
                {THEMES.map((t) => {
                  const active = theme === t.id;
                  return (
                    <button
                      key={t.id}
                      onClick={() => { setTheme(t.id); applyTheme(t.id); }}
                      style={{
                        display: "flex", alignItems: "center", gap: 10, textAlign: "left",
                        padding: "8px 10px", borderRadius: 6, cursor: "pointer",
                        background: active ? "var(--v2-accent-deep)" : "var(--v2-bg-elevated)",
                        border: "1px solid " + (active ? "var(--v2-accent-brand)" : "var(--v2-border-medium)"),
                      }}
                    >
                      <ThemeSwatch colors={t.flag} />
                      <span style={{ display: "flex", flexDirection: "column", minWidth: 0 }}>
                        <span style={{ fontSize: 12, fontWeight: 600, color: active ? "var(--v2-accent-brand)" : "var(--v2-text-primary)" }}>
                          {t.name}{active ? " ✓" : ""}
                        </span>
                        <span style={{ fontSize: 10, color: "var(--v2-text-muted)", lineHeight: 1.3 }}>{t.desc}</span>
                      </span>
                    </button>
                  );
                })}
              </div>
            </fieldset>
            )}

            {tab === "general" && (
            <>
            {/* Visualização — protótipos opt-in */}
            <fieldset style={{ border: "1px solid var(--v2-border-medium)", borderRadius: 6, padding: "12px 14px" }}>
              <legend style={{ fontSize: 11, fontWeight: 600, color: "var(--v2-text-secondary)", padding: "0 4px" }}>
                Visualization
              </legend>
              <label style={{ display: "flex", alignItems: "center", gap: 10, fontSize: 13, cursor: "pointer" }}>
                <input
                  type="checkbox"
                  checked={minimap}
                  onChange={(e) => {
                    const v = e.target.checked;
                    setMinimap(v);
                    try { window.localStorage.setItem("regente:minimap", v ? "1" : "0"); } catch { /* ignore */ }
                    window.dispatchEvent(new Event("regente:minimap-changed"));
                  }}
                />
                Navigation minimap
              </label>
              <span style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 6, display: "block", lineHeight: 1.5 }}>
                Prototype. Shows a map of the environment in the bottom corner of Monitoring — click/drag to
                navigate large environments. Resizable; off by default.
              </span>

              <div style={{ marginTop: 12, borderTop: "1px solid var(--v2-border-subtle)", paddingTop: 10 }}>
                <div style={{ fontSize: 12, fontWeight: 600, marginBottom: 6 }}>Loose job layout (grid)</div>
                <div style={{ display: "flex", gap: 16, alignItems: "flex-start" }}>
                  <label style={{ fontSize: 11, color: "var(--v2-text-secondary)" }}>
                    Columns
                    <input type="number" min={1} max={40} value={layoutCols}
                      onChange={(e) => { const v = Math.max(1, Math.min(40, Number(e.target.value) || 10)); setLayoutCols(v); writeLayout(v, layoutMaxRows); }}
                      style={{ display: "block", marginTop: 4, width: 70, padding: "5px 8px", fontSize: 13, background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-medium)", borderRadius: 4, color: "var(--v2-text-primary)", outline: "none", boxSizing: "border-box" }} />
                  </label>
                  <label style={{ fontSize: 11, color: "var(--v2-text-secondary)" }}>
                    Max. rows (before widening)
                    <input type="number" min={1} max={200} value={layoutMaxRows}
                      onChange={(e) => { const v = Math.max(1, Math.min(200, Number(e.target.value) || 30)); setLayoutMaxRows(v); writeLayout(layoutCols, v); }}
                      style={{ display: "block", marginTop: 4, width: 90, padding: "5px 8px", fontSize: 13, background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-medium)", borderRadius: 4, color: "var(--v2-text-primary)", outline: "none", boxSizing: "border-box" }} />
                  </label>
                </div>
                <span style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 6, display: "block", lineHeight: 1.5 }}>
                  Jobs with NO dependency become a grid of N columns; past "max. rows" the grid widens
                  (adds columns) instead of growing downwards. Applies per folder, right away. Dependents keep the
                  top-down flow (unchanged). Each folder can have its own override (Folders screen → grid icon),
                  saved in the workspace.
                </span>
              </div>

              {/* UI-1 — cap do grafo do Monitoring (o quanto o ReactFlow desenha). */}
              <div style={{ marginTop: 12, borderTop: "1px solid var(--v2-border-subtle)", paddingTop: 10 }}>
                <div style={{ fontSize: 12, fontWeight: 600, marginBottom: 6 }}>Graph cap (Monitoring)</div>
                <label style={{ fontSize: 11, color: "var(--v2-text-secondary)" }}>
                  Max. jobs drawn on the canvas
                  <input type="number" min={500} max={5000} step={100} value={legacyCapVal}
                    onChange={(e) => { const v = Math.max(500, Math.min(5000, Number(e.target.value) || 2000)); setLegacyCapVal(v); writeLegacyCap(v); }}
                    style={{ display: "block", marginTop: 4, width: 90, padding: "5px 8px", fontSize: 13, background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-medium)", borderRadius: 4, color: "var(--v2-text-primary)", outline: "none", boxSizing: "border-box" }} />
                </label>
                <span style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 6, display: "block", lineHeight: 1.5 }}>
                  The graph (ReactFlow) does not virtualize — this cap keeps huge days from freezing the UI. The
                  ACTIVE JOBS list shows the WHOLE day regardless of the cap (virtualized, server-driven above
                  it) and the ViewPoint covers drill-down at 100k–1M. Raising the cap costs graph render time.
                </span>
              </div>
            </fieldset>

            {/* F20 — Environment Label */}
            <fieldset style={{ border: "1px solid var(--v2-border-medium)", borderRadius: 6, padding: "12px 14px" }}>
              <legend style={{ fontSize: 11, fontWeight: 600, color: "var(--v2-text-secondary)", padding: "0 4px" }}>
                Environment
              </legend>
              <label style={{ fontSize: 12, display: "block", marginBottom: 6 }}>
                Environment label (shows in the header when filled in)
              </label>
              <input
                value={envLabel}
                onChange={(e) => setEnvLabel(e.target.value)}
                placeholder="e.g. QA, Production, Staging"
                style={{
                  width: "100%", padding: "6px 10px", fontSize: 13,
                  background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-medium)",
                  borderRadius: 4, color: "var(--v2-text-primary)", outline: "none",
                  boxSizing: "border-box",
                }}
              />
              <span style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 4, display: "block" }}>
                Leave it empty to hide the tag. E.g. "QA", "Production", "DEV".
              </span>

              <div style={{ marginTop: 12, borderTop: "1px solid var(--v2-border-subtle)", paddingTop: 10 }}>
                <div style={{ display: "flex", gap: 16, alignItems: "flex-start", flexWrap: "wrap" }}>
                  <div>
                    <label style={{ fontSize: 12, display: "block", marginBottom: 6 }}>
                      Daily time (New Day)
                    </label>
                    <input
                      type="time"
                      value={dailyAt}
                      onChange={(e) => setDailyAt(e.target.value)}
                      style={{
                        padding: "6px 10px", fontSize: 13,
                        background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-medium)",
                        borderRadius: 4, color: "var(--v2-text-primary)", outline: "none",
                        colorScheme: "dark",
                      }}
                    />
                  </div>
                  <div style={{ flex: 1, minWidth: 180 }}>
                    <label style={{ fontSize: 12, display: "block", marginBottom: 6 }}>
                      Daily timezone
                    </label>
                    <input
                      list="regente-tz-suggestions"
                      value={dailyTz}
                      onChange={(e) => setDailyTz(e.target.value)}
                      placeholder="server local clock"
                      spellCheck={false}
                      style={{
                        width: "100%", padding: "6px 10px", fontSize: 13, fontFamily: "var(--v2-font-mono)",
                        background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-medium)",
                        borderRadius: 4, color: "var(--v2-text-primary)", outline: "none", boxSizing: "border-box",
                      }}
                    />
                    <datalist id="regente-tz-suggestions">
                      {["America/Sao_Paulo", "America/New_York", "America/Chicago", "America/Los_Angeles",
                        "UTC", "Europe/London", "Europe/Madrid", "Europe/Berlin", "Asia/Tokyo",
                        "Asia/Singapore", "Australia/Sydney"].map((tz) => <option key={tz} value={tz} />)}
                    </datalist>
                  </div>
                </div>
                <span style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 4, display: "block", lineHeight: 1.5 }}>
                  The server materializes the daily at this time, on the BUSINESS clock of the timezone
                  (IANA name, e.g. <code>America/Sao_Paulo</code>; empty = the server's local clock —
                  an invalid name falls back to local and is logged). The <code>order_date</code> is the day in that
                  timezone: a server on UTC with business hours in SP crosses midnight at 03:00Z. Applies with no
                  restart; if today's daily already ran, it takes effect tomorrow.
                </span>
              </div>

              {/* ADV-5 — Archives / Retention */}
              <div style={{ marginTop: 12, borderTop: "1px solid var(--v2-border-subtle)", paddingTop: 10 }}>
                <div style={{ display: "flex", gap: 16, alignItems: "flex-start", flexWrap: "wrap" }}>
                  <div>
                    <label style={{ fontSize: 12, display: "block", marginBottom: 6 }}>
                      Instance retention (days)
                    </label>
                    <input
                      type="number" min={0} value={retentionDays}
                      onChange={(e) => setRetentionDays(e.target.value)}
                      placeholder="unlimited"
                      style={{
                        width: 110, padding: "6px 10px", fontSize: 13,
                        background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-medium)",
                        borderRadius: 4, color: "var(--v2-text-primary)", outline: "none", boxSizing: "border-box",
                      }}
                    />
                  </div>
                  <div style={{ flex: 1, minWidth: 180 }}>
                    <label style={{ fontSize: 12, display: "block", marginBottom: 6 }}>
                      Archive directory
                    </label>
                    <input
                      value={archiveDir}
                      onChange={(e) => setArchiveDir(e.target.value)}
                      placeholder="./archive"
                      spellCheck={false}
                      style={{
                        width: "100%", padding: "6px 10px", fontSize: 13, fontFamily: "var(--v2-font-mono)",
                        background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-medium)",
                        borderRadius: 4, color: "var(--v2-text-primary)", outline: "none", boxSizing: "border-box",
                      }}
                    />
                  </div>
                </div>
                <span style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 4, display: "block", lineHeight: 1.5 }}>
                  Dailies older than the retention are ARCHIVED (one NDJSON per day, with status/output/snapshot)
                  into the directory above and removed from the database, right after the daily — empty/0 = keep
                  forever. Download at <code>GET /api/archive</code> (admin). Audit event retention is separate
                  (<code>audit_retention_days</code>).
                </span>
              </div>
            </fieldset>

            {/* GitHub token (via UI — substitui subir o server com GITHUB_TOKEN) */}
            <fieldset style={{ border: "1px solid var(--v2-border-medium)", borderRadius: 6, padding: "12px 14px" }}>
              <legend style={{ fontSize: 11, fontWeight: 600, color: "var(--v2-text-secondary)", padding: "0 4px" }}>
                GitHub
              </legend>

              <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 10, fontSize: 12 }}>
                <span style={{ color: "var(--v2-text-muted)" }}>Status:</span>
                {git?.authMode === "token" || git?.hasToken ? (
                  <span style={{ display: "inline-flex", alignItems: "center", gap: 6, color: "var(--v2-status-ok)", fontWeight: 600 }}>
                    <span style={{ width: 7, height: 7, borderRadius: "50%", background: "var(--v2-status-ok)" }} />
                    Token active — push/PR enabled
                  </span>
                ) : (
                  <span style={{ display: "inline-flex", alignItems: "center", gap: 6, color: "var(--v2-status-waiting)", fontWeight: 600 }}>
                    <span style={{ width: 7, height: 7, borderRadius: "50%", background: "var(--v2-status-waiting)" }} />
                    No token — read-only
                  </span>
                )}
              </div>

              <label style={{ fontSize: 12, display: "block", marginBottom: 6 }}>
                Personal Access Token (fine-grained or classic with the <code>repo</code> scope)
              </label>
              <div style={{ display: "flex", gap: 6 }}>
                <input
                  type="password"
                  value={tokenInput}
                  onChange={(e) => setTokenInput(e.target.value)}
                  placeholder={git?.hasToken ? "•••••• (replace)" : "ghp_… / github_pat_…"}
                  autoComplete="off"
                  style={{
                    flex: 1, padding: "6px 10px", fontSize: 13, fontFamily: "var(--v2-font-mono)",
                    background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-medium)",
                    borderRadius: 4, color: "var(--v2-text-primary)", outline: "none", boxSizing: "border-box",
                  }}
                />
                <button
                  onClick={handleSaveToken}
                  disabled={tokenBusy || !tokenInput.trim()}
                  style={{
                    padding: "6px 14px", fontSize: 12, borderRadius: 4, whiteSpace: "nowrap",
                    background: "var(--v2-accent-brand)", border: "none", color: "#000", fontWeight: 600,
                    cursor: tokenBusy || !tokenInput.trim() ? "not-allowed" : "pointer",
                    opacity: tokenBusy || !tokenInput.trim() ? 0.5 : 1,
                  }}
                >
                  {tokenBusy ? "…" : "Save token"}
                </button>
              </div>
              <span style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 4, display: "block", lineHeight: 1.5 }}>
                Stored server-side (SQLite), outside <code>.git/config</code>. It survives restarts.
                It is never returned by the API nor shown again after being saved.
              </span>

              <div style={{ display: "flex", gap: 6, marginTop: 12, flexWrap: "wrap" }}>
                {git?.hasToken && (
                  <button
                    onClick={handleClearToken}
                    disabled={tokenBusy}
                    style={{
                      padding: "5px 12px", fontSize: 11, borderRadius: 4,
                      background: "transparent", border: "1px solid rgba(239,68,68,.4)",
                      color: "var(--v2-status-failed)", cursor: "pointer",
                    }}
                  >
                    Remove token
                  </button>
                )}
                <button
                  onClick={handleCleanupDB}
                  disabled={cleanBusy || !git?.hasToken}
                  title={git?.hasToken ? "Removes the SQLite DB that old runs committed into the repo (fixes the WAL bug)" : "A token is required to clean the origin"}
                  style={{
                    padding: "5px 12px", fontSize: 11, borderRadius: 4,
                    background: "transparent", border: "1px solid var(--v2-border-medium)",
                    color: "var(--v2-text-secondary)", cursor: cleanBusy || !git?.hasToken ? "not-allowed" : "pointer",
                    opacity: cleanBusy || !git?.hasToken ? 0.5 : 1,
                  }}
                >
                  {cleanBusy ? "Cleaning…" : "Clean DB from the repository"}
                </button>
              </div>

              {/* P13 — webhook secret (HMAC) */}
              <div style={{ marginTop: 14, paddingTop: 12, borderTop: "1px solid var(--v2-border-subtle)" }}>
                <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 6, fontSize: 12 }}>
                  <span style={{ color: "var(--v2-text-muted)" }}>Webhook:</span>
                  <span style={{ color: git?.webhookConfigured ? "var(--v2-status-ok)" : "var(--v2-text-muted)", fontWeight: 600 }}>
                    {git?.webhookConfigured ? "secret configured (HMAC active)" : "no secret (validation off)"}
                  </span>
                </div>
                <div style={{ display: "flex", gap: 6 }}>
                  <input
                    type="password"
                    value={webhookInput}
                    onChange={(e) => setWebhookInput(e.target.value)}
                    placeholder={git?.webhookConfigured ? "•••••• (replace / empty removes)" : "GitHub webhook secret"}
                    autoComplete="off"
                    style={{
                      flex: 1, padding: "6px 10px", fontSize: 13, fontFamily: "var(--v2-font-mono)",
                      background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-medium)",
                      borderRadius: 4, color: "var(--v2-text-primary)", outline: "none", boxSizing: "border-box",
                    }}
                  />
                  <button
                    onClick={handleSaveWebhook}
                    disabled={tokenBusy}
                    style={{
                      padding: "6px 14px", fontSize: 12, borderRadius: 4, whiteSpace: "nowrap",
                      background: "transparent", border: "1px solid var(--v2-border-medium)",
                      color: "var(--v2-text-secondary)", cursor: tokenBusy ? "wait" : "pointer",
                    }}
                  >
                    Save webhook
                  </button>
                </div>
                <span style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 4, display: "block", lineHeight: 1.5 }}>
                  On GitHub: repo → Settings → Webhooks → payload <code>/api/git/webhook</code>, content-type JSON,
                  the same secret as this one. Changes on main come back to the UI right away.
                </span>
              </div>

              {tokenMsg && (
                <div style={{
                  marginTop: 10, padding: "6px 10px", borderRadius: 4, fontSize: 11, lineHeight: 1.5,
                  fontFamily: "var(--v2-font-mono)", wordBreak: "break-word",
                  background: tokenMsg.kind === "ok" ? "rgba(34,197,94,.08)" : "rgba(239,68,68,.08)",
                  border: `1px solid ${tokenMsg.kind === "ok" ? "rgba(34,197,94,.3)" : "rgba(239,68,68,.3)"}`,
                  color: tokenMsg.kind === "ok" ? "var(--v2-status-ok)" : "var(--v2-status-failed)",
                }}>
                  {tokenMsg.text}
                </div>
              )}
            </fieldset>

            </>
            )}

            {tab === "agents" && <AgentsManager />}

            <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
              <button onClick={onClose} className="v2-btn">Cancel</button>
              <button onClick={handleSave} disabled={saving} className="v2-btn v2-btn-primary">
                {saving ? "Saving..." : "Save"}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
