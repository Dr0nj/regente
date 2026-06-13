// F20 — Admin settings dialog.
import { useEffect, useState } from "react";
import { getSettings, putSettings, type ServerSettings } from "../lib/settings-api";

interface Props {
  onClose: () => void;
}

export function SettingsDialog({ onClose }: Props) {
  const [settings, setSettings] = useState<ServerSettings>({});
  const [envLabel, setEnvLabel] = useState("");
  const [saving, setSaving] = useState(false);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    getSettings().then((s) => {
      setSettings(s);
      setEnvLabel(s.env_label ?? "");
      setLoaded(true);
    });
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
