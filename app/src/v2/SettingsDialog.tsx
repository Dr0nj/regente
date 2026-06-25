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
  const [tab, setTab] = useState<"geral" | "temas" | "agentes">("geral");
  const [minimap, setMinimap] = useState<boolean>(() => typeof window !== "undefined" && window.localStorage.getItem("regente:minimap") === "1");

  // GitHub token
  const [git, setGit] = useState<GitStatus | null>(null);
  const [tokenInput, setTokenInput] = useState("");
  const [tokenBusy, setTokenBusy] = useState(false);
  const [tokenMsg, setTokenMsg] = useState<{ kind: "ok" | "err"; text: string } | null>(null);
  const [cleanBusy, setCleanBusy] = useState(false);
  const [webhookInput, setWebhookInput] = useState("");

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

  async function handleSaveWebhook() {
    setTokenBusy(true); setTokenMsg(null);
    try {
      const st = await setWebhookSecret(webhookInput.trim());
      setGit(st); setWebhookInput("");
      setTokenMsg({ kind: "ok", text: webhookInput.trim() ? "Webhook secret salvo. Configure o mesmo no GitHub." : "Webhook secret removido." });
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
        className="v2-grain-card v2-neon-card"
        style={{
          width: 440, maxHeight: "80vh", overflow: "auto",
          padding: 24, display: "flex", flexDirection: "column", gap: 20,
        }}
      >
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <h2 style={{ fontSize: 15, fontWeight: 600, margin: 0 }}>Configurações</h2>
          <button onClick={onClose} className="v2-dialog-x" aria-label="Fechar">✕</button>
        </div>

        {!loaded ? (
          <span style={{ fontSize: 12, color: "var(--v2-text-muted)" }}>Carregando...</span>
        ) : (
          <>
            {/* Sub-abas: Geral | Temas */}
            <div style={{ display: "flex", gap: 4, padding: 4, background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-subtle)", borderRadius: 10 }}>
              {([["geral", "Geral"], ["agentes", "Agentes"], ["temas", "Temas"]] as const).map(([id, label]) => (
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

            {tab === "temas" && (
            <fieldset style={{ border: "1px solid var(--v2-border-medium)", borderRadius: 6, padding: "12px 14px" }}>
              <legend style={{ fontSize: 11, fontWeight: 600, color: "var(--v2-text-secondary)", padding: "0 4px" }}>
                Tema
              </legend>
              <div style={{ fontSize: 10, color: "var(--v2-text-muted)", marginBottom: 10 }}>
                Escolha a aparência. Aplica na hora e fica salvo neste navegador.
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

            {tab === "geral" && (
            <>
            {/* Visualização — protótipos opt-in */}
            <fieldset style={{ border: "1px solid var(--v2-border-medium)", borderRadius: 6, padding: "12px 14px" }}>
              <legend style={{ fontSize: 11, fontWeight: 600, color: "var(--v2-text-secondary)", padding: "0 4px" }}>
                Visualização
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
                Minimap de navegação
              </label>
              <span style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 6, display: "block", lineHeight: 1.5 }}>
                Protótipo. Mostra um mapa do ambiente no canto inferior do Monitoring — clique/arraste para
                navegar em ambientes grandes (estilo Control-M). Redimensionável; desligado por padrão.
              </span>
            </fieldset>

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

              {/* P13 — webhook secret (HMAC) */}
              <div style={{ marginTop: 14, paddingTop: 12, borderTop: "1px solid var(--v2-border-subtle)" }}>
                <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 6, fontSize: 12 }}>
                  <span style={{ color: "var(--v2-text-muted)" }}>Webhook:</span>
                  <span style={{ color: git?.webhookConfigured ? "var(--v2-status-ok)" : "var(--v2-text-muted)", fontWeight: 600 }}>
                    {git?.webhookConfigured ? "secret configurado (HMAC ativo)" : "sem secret (validação inativa)"}
                  </span>
                </div>
                <div style={{ display: "flex", gap: 6 }}>
                  <input
                    type="password"
                    value={webhookInput}
                    onChange={(e) => setWebhookInput(e.target.value)}
                    placeholder={git?.webhookConfigured ? "•••••• (substituir / vazio remove)" : "secret do webhook do GitHub"}
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
                    Salvar webhook
                  </button>
                </div>
                <span style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 4, display: "block", lineHeight: 1.5 }}>
                  No GitHub: repo → Settings → Webhooks → payload <code>/api/git/webhook</code>, content-type JSON,
                  secret igual a este. Mudanças no main voltam pra UI na hora.
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

            {tab === "agentes" && <AgentsManager />}

            <div style={{ display: "flex", justifyContent: "flex-end", gap: 8 }}>
              <button onClick={onClose} className="v2-btn">Cancelar</button>
              <button onClick={handleSave} disabled={saving} className="v2-btn v2-btn-primary">
                {saving ? "Salvando..." : "Salvar"}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
