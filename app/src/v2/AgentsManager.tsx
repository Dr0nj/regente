import { useEffect, useState, useCallback } from "react";
import {
  listAgents, listAgentTokens, createAgentToken, revokeAgentToken,
  type AgentInfo, type AgentToken,
} from "../lib/agents-api";

/* ──────────────────────────────────────────────────────────────
   AgentsManager — tela de Agentes (frota).
   Lista consolidada (online + offline com last-seen) · clique abre modal de
   detalhe (OS, arch, host, versão, uptime, conexão) · gestão de tokens.
   ────────────────────────────────────────────────────────────── */

function fmtDuration(ms: number): string {
  if (ms < 0) ms = 0;
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}min`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ${m % 60}min`;
  const d = Math.floor(h / 24);
  return `${d}d ${h % 24}h`;
}
function uptimeOf(startedAt?: string): string {
  if (!startedAt) return "—";
  const t = Date.parse(startedAt);
  if (!Number.isFinite(t)) return "—";
  return fmtDuration(Date.now() - t);
}
function relativeOf(iso?: string): string {
  if (!iso) return "—";
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return "—";
  const diff = Date.now() - t;
  if (diff < 45000) return "agora";
  return "há " + fmtDuration(diff);
}

export default function AgentsManager() {
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [tokens, setTokens] = useState<AgentToken[]>([]);
  const [detail, setDetail] = useState<AgentInfo | null>(null);
  const [newLabel, setNewLabel] = useState("");
  const [justCreated, setJustCreated] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const reload = useCallback(() => {
    listAgents().then(setAgents).catch(() => {});
    listAgentTokens().then(setTokens).catch(() => {});
  }, []);

  useEffect(() => {
    reload();
    const t = setInterval(reload, 5000); // status ao vivo (online/uptime/last-seen)
    return () => clearInterval(t);
  }, [reload]);

  const online = agents.filter((a) => a.online).length;

  async function handleCreate() {
    setBusy(true); setJustCreated(null);
    try {
      const r = await createAgentToken(newLabel.trim() || "agent");
      setJustCreated(r.token);
      setNewLabel("");
      reload();
    } finally { setBusy(false); }
  }
  async function handleRevoke(id: number) {
    setBusy(true);
    try { await revokeAgentToken(id); reload(); } finally { setBusy(false); }
  }

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      {/* Frota */}
      <fieldset style={{ border: "1px solid var(--v2-border-medium)", borderRadius: 6, padding: "12px 14px" }}>
        <legend style={{ fontSize: 11, fontWeight: 600, color: "var(--v2-text-secondary)", padding: "0 4px" }}>
          Frota — {online} de {agents.length} online
        </legend>

        {agents.length === 0 ? (
          <div style={{ fontSize: 11, color: "var(--v2-text-muted)" }}>Nenhum agente registrado ainda.</div>
        ) : (
          <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
            {agents.map((a) => (
              <button
                key={a.id}
                onClick={() => setDetail(a)}
                title="Ver detalhes"
                style={{
                  display: "flex", alignItems: "center", gap: 10, textAlign: "left",
                  padding: "7px 10px", background: "var(--v2-bg-elevated)",
                  border: "1px solid var(--v2-border-subtle)", borderRadius: 4, cursor: "pointer",
                }}
              >
                <span style={{ width: 8, height: 8, borderRadius: "50%", flexShrink: 0,
                  background: a.online ? "var(--v2-status-ok)" : "var(--v2-text-muted)",
                  boxShadow: a.online ? "0 0 6px var(--v2-status-ok)" : "none" }} />
                <span style={{ fontFamily: "var(--v2-font-mono)", fontSize: 12, color: "var(--v2-text-primary)", minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                  {a.id}
                </span>
                {a.os && <span style={{ fontSize: 9, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)", border: "1px solid var(--v2-border-subtle)", borderRadius: 3, padding: "1px 5px" }}>{a.os}{a.arch ? `/${a.arch}` : ""}</span>}
                <span style={{ fontSize: 10, color: "var(--v2-text-muted)", marginLeft: "auto", whiteSpace: "nowrap" }}>
                  {a.online ? `up ${uptimeOf(a.startedAt)}` : `visto ${relativeOf(a.lastSeen)}`}
                </span>
                <span style={{ fontSize: 10, color: "var(--v2-text-muted)" }}>›</span>
              </button>
            ))}
          </div>
        )}
      </fieldset>

      {/* Tokens */}
      <fieldset style={{ border: "1px solid var(--v2-border-medium)", borderRadius: 6, padding: "12px 14px" }}>
        <legend style={{ fontSize: 11, fontWeight: 600, color: "var(--v2-text-secondary)", padding: "0 4px" }}>
          Tokens de agente
        </legend>
        <label style={{ fontSize: 12, display: "block", marginBottom: 6 }}>Novo token</label>
        <div style={{ display: "flex", gap: 6 }}>
          <input
            value={newLabel}
            onChange={(e) => setNewLabel(e.target.value)}
            placeholder="label (ex: laptop-thiago, ec2-prod)"
            style={{ flex: 1, padding: "6px 10px", fontSize: 13, background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-medium)", borderRadius: 4, color: "var(--v2-text-primary)", outline: "none", boxSizing: "border-box" }}
          />
          <button onClick={handleCreate} disabled={busy}
            style={{ padding: "6px 14px", fontSize: 12, borderRadius: 4, whiteSpace: "nowrap", background: "var(--v2-accent-brand)", border: "none", color: "#000", fontWeight: 600, cursor: busy ? "wait" : "pointer", opacity: busy ? 0.6 : 1 }}>
            Criar token
          </button>
        </div>

        {justCreated && (
          <div style={{ marginTop: 8, padding: "8px 10px", borderRadius: 4, fontSize: 11, lineHeight: 1.5, background: "rgba(34,197,94,.08)", border: "1px solid rgba(34,197,94,.3)", color: "var(--v2-text-primary)" }}>
            <div style={{ color: "var(--v2-status-ok)", fontWeight: 600, marginBottom: 4 }}>Token criado — copie agora (não volta a aparecer):</div>
            <code style={{ fontFamily: "var(--v2-font-mono)", fontSize: 11, wordBreak: "break-all", userSelect: "all" }}>{justCreated}</code>
            <div style={{ color: "var(--v2-text-muted)", marginTop: 6 }}>
              Use: <code style={{ fontFamily: "var(--v2-font-mono)" }}>regente-agent -token {justCreated.slice(0, 12)}… -id &lt;nome&gt;</code>
            </div>
          </div>
        )}

        {tokens.length > 0 && (
          <div style={{ marginTop: 10, display: "flex", flexDirection: "column", gap: 4 }}>
            {tokens.map((t) => (
              <div key={t.id} style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 11, padding: "5px 8px", background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-subtle)", borderRadius: 3 }}>
                <span style={{ flex: 1, color: "var(--v2-text-primary)" }}>{t.label || "—"}</span>
                <span style={{ fontFamily: "var(--v2-font-mono)", fontSize: 10, color: "var(--v2-text-muted)" }}>{t.tokenPrefix}</span>
                <span style={{ fontSize: 9, color: "var(--v2-text-muted)" }} title="último uso">{t.lastUsedAt ? "usado" : "nunca usado"}</span>
                <button onClick={() => handleRevoke(t.id)} disabled={busy}
                  style={{ background: "transparent", border: "1px solid rgba(239,68,68,.4)", color: "var(--v2-status-failed)", borderRadius: 3, fontSize: 10, padding: "2px 8px", cursor: "pointer" }}>
                  revogar
                </button>
              </div>
            ))}
          </div>
        )}
      </fieldset>

      {detail && <AgentDetailModal agent={detail} onClose={() => setDetail(null)} />}
    </div>
  );
}

function AgentDetailModal({ agent, onClose }: { agent: AgentInfo; onClose: () => void }) {
  const a = agent;
  return (
    <div onClick={onClose} style={{ position: "fixed", inset: 0, zIndex: 60, background: "rgba(0,0,0,0.6)", display: "flex", alignItems: "center", justifyContent: "center" }}>
      <div onClick={(e) => e.stopPropagation()} className="v2-neon-card"
        style={{ width: 420, maxWidth: "92vw", background: "var(--v2-bg-surface)", borderRadius: 6, color: "var(--v2-text-primary)", fontFamily: "var(--v2-font-sans)", overflow: "hidden" }}>
        <div style={{ padding: "12px 16px", borderBottom: "1px solid var(--v2-border-subtle)", display: "flex", alignItems: "center", gap: 10 }}>
          <span style={{ width: 9, height: 9, borderRadius: "50%", background: a.online ? "var(--v2-status-ok)" : "var(--v2-text-muted)", boxShadow: a.online ? "0 0 6px var(--v2-status-ok)" : "none" }} />
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 13, fontWeight: 700, fontFamily: "var(--v2-font-mono)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{a.id}</div>
            <div style={{ fontSize: 10, color: a.online ? "var(--v2-status-ok)" : "var(--v2-text-muted)", letterSpacing: "0.06em", textTransform: "uppercase", marginTop: 1 }}>
              {a.online ? "online" : "offline"}
            </div>
          </div>
          <button onClick={onClose} aria-label="close" style={{ background: "transparent", border: "none", color: "var(--v2-text-muted)", cursor: "pointer", fontSize: 16, padding: 4 }}>×</button>
        </div>
        <div style={{ padding: "10px 16px" }}>
          <Row label="Sistema" value={a.os ? `${a.os}${a.arch ? ` / ${a.arch}` : ""}` : "—"} />
          <Row label="Host" value={a.host || "—"} />
          <Row label="Versão" value={a.version || "—"} />
          <Row label="Capabilities" value={a.capabilities.length ? a.capabilities.join(" · ") : "—"} />
          <Row label="Uptime" value={a.online ? uptimeOf(a.startedAt) : "—"} />
          <Row label="Conectado" value={a.online ? `há ${uptimeOf(a.connectedAt)}` : "—"} />
          <Row label="Visto pela 1ª vez" value={relativeOf(a.firstSeen)} />
          <Row label="Último sinal" value={relativeOf(a.lastSeen)} />
        </div>
      </div>
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div style={{ display: "flex", justifyContent: "space-between", gap: 12, padding: "5px 0", borderBottom: "1px dashed var(--v2-border-subtle)", fontSize: 12 }}>
      <span style={{ color: "var(--v2-text-muted)" }}>{label}</span>
      <span style={{ color: "var(--v2-text-primary)", fontFamily: "var(--v2-font-mono)", fontSize: 11, textAlign: "right", wordBreak: "break-word" }}>{value}</span>
    </div>
  );
}
