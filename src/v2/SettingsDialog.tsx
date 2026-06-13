// F20 — Admin settings dialog.
import { useEffect, useState } from "react";
import { getSettings, putSettings, type ServerSettings } from "../lib/settings-api";
import { fetchGitStatus, setGitToken, clearGitToken, cleanupGitDB, type GitStatus } from "../lib/git-api";
import { invalidateGitInfo } from "../lib/git-info";

interface Props {
  onClose: () => void;
}

export function SettingsDialog({ onClose }: Props) {
  const [settings, setSettings] = useState<ServerSettings>({});
  const [envLabel, setEnvLabel] = useState("");
  const [saving, setSaving] = useState(false);
  const [loaded, setLoaded] = useState(false);

  // GitHub token
  const [git, setGit] = useState<GitStatus | null>(null);
  const [tokenInput, setTokenInput] = useState("");
  const [tokenBusy, setTokenBusy] = useState(false);
  const [tokenMsg, setTokenMsg] = useState<{ kind: "ok" | "err"; text: string } | null>(null);
  const [cleanBusy, setCleanBusy] = useState(false);

  useEffect(() => {
    getSettings().then((s) => {
      setSettings(s);
      setEnvLabel(s.env_label ?? "");
      setLoaded(true);
    });
    fetchGitStatus().then(setGit).catch(() => {});
  }, []);

  async function handleSave() {
    setSaving(true);
    try {
      const updated = await putSettings({ ...settings, env_label: envLabel });
      setSettings(updated);
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
      setTokenMsg({ kind: "ok", text: "Token salvo e validado. Push/PR habilitados." });
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
      setTokenMsg({ kind: "ok", text: "Token removido. Modo read-only." });
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
        text: r.changed ? "DB removida do repositório (origin limpo)." : "Repositório já estava limpo.",
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
        className="v2-grain-card"
        style={{
          width: 440, maxHeight: "80vh", overflow: "auto",
          padding: 24, display: "flex", flexDirection: "column", gap: 20,
        }}
      >
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <h2 style={{ fontSize: 15, fontWeight: 600, margin: 0 }}>Configurações</h2>
          <button onClick={onClose} style={{ background: "none", border: "none", color: "var(--v2-text-muted)", cursor: "pointer", fontSize: 16 }}>✕</button>
        </div>

        {!loaded ? (
          <span style={{ fontSize: 12, color: "var(--v2-text-muted)" }}>Carregando...</span>
        ) : (
          <>
            {/* F20 — Environment Label */}
            <fieldset style={{ border: "1px solid var(--v2-border-medium)", borderRadius: 6, padding: "12px 14px" }}>
              <legend style={{ fontSize: 11, fontWeight: 600, color: "var(--v2-text-secondary)", padding: "0 4px" }}>
                Ambiente
              </legend>
              <label style={{ fontSize: 12, display: "block", marginBottom: 6 }}>
                Label do ambiente (aparece no header quando preenchido)
              </label>
              <input
                value={envLabel}
                onChange={(e) => setEnvLabel(e.target.value)}
                placeholder="ex: QA, Production, Staging"
                style={{
                  width: "100%", padding: "6px 10px", fontSize: 13,
                  background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-medium)",
                  borderRadius: 4, color: "var(--v2-text-primary)", outline: "none",
                  boxSizing: "border-box",
                }}
              />
              <span style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 4, display: "block" }}>
                Deixe vazio para ocultar a tag. Ex: "QA", "Production", "DEV".
              </span>
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
                    Token ativo — push/PR habilitados
                  </span>
                ) : (
                  <span style={{ display: "inline-flex", alignItems: "center", gap: 6, color: "var(--v2-status-waiting)", fontWeight: 600 }}>
                    <span style={{ width: 7, height: 7, borderRadius: "50%", background: "var(--v2-status-waiting)" }} />
                    Sem token — read-only
                  </span>
                )}
              </div>

              <label style={{ fontSize: 12, display: "block", marginBottom: 6 }}>
                Personal Access Token (fine-grained ou classic com escopo <code>repo</code>)
              </label>
              <div style={{ display: "flex", gap: 6 }}>
                <input
                  type="password"
                  value={tokenInput}
                  onChange={(e) => setTokenInput(e.target.value)}
                  placeholder={git?.hasToken ? "•••••• (substituir)" : "ghp_… / github_pat_…"}
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
                  {tokenBusy ? "…" : "Salvar token"}
                </button>
              </div>
              <span style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 4, display: "block", lineHeight: 1.5 }}>
                Guardado server-side (SQLite), fora do <code>.git/config</code>. Sobrevive a restart.
                Nunca é devolvido pela API nem exibido depois de salvo.
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
                    Remover token
                  </button>
                )}
                <button
                  onClick={handleCleanupDB}
                  disabled={cleanBusy || !git?.hasToken}
                  title={git?.hasToken ? "Remove a SQLite DB que runs antigas commitaram no repo (resolve o bug do WAL)" : "Precisa de token para limpar o origin"}
                  style={{
                    padding: "5px 12px", fontSize: 11, borderRadius: 4,
                    background: "transparent", border: "1px solid var(--v2-border-medium)",
                    color: "var(--v2-text-secondary)", cursor: cleanBusy || !git?.hasToken ? "not-allowed" : "pointer",
                    opacity: cleanBusy || !git?.hasToken ? 0.5 : 1,
                  }}
                >
                  {cleanBusy ? "Limpando…" : "Limpar DB do repositório"}
                </button>
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

            <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
              <button
                onClick={onClose}
                style={{
                  padding: "6px 14px", fontSize: 12, borderRadius: 4,
                  background: "transparent", border: "1px solid var(--v2-border-medium)",
                  color: "var(--v2-text-secondary)", cursor: "pointer",
                }}
              >
                Cancelar
              </button>
              <button
                onClick={handleSave}
                disabled={saving}
                style={{
                  padding: "6px 14px", fontSize: 12, borderRadius: 4,
                  background: "var(--v2-accent-brand)", border: "none",
                  color: "#000", fontWeight: 600, cursor: saving ? "wait" : "pointer",
                  opacity: saving ? 0.6 : 1,
                }}
              >
                {saving ? "Salvando..." : "Salvar"}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
