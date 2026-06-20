/**
 * AlertsPanel — Phase 8 alerting UI.
 *
 * Modal with two tabs:
 *   - Events: fired alert events, acknowledge (single / all), filters.
 *   - Rules:  enable/disable alert rules, inspect condition/severity/channels.
 *
 * Reads/writes through @/lib/alerting. Styled with v2 design tokens.
 */
import { useCallback, useEffect, useMemo, useState, type CSSProperties, type ReactNode } from "react";
import { Bell, Check, CheckCheck, X, Send, RotateCcw } from "lucide-react";
import type { AlertChannel, AlertEvent, AlertRule, AlertSeverity } from "@/lib/alerting";
import { ALL_CHANNELS } from "@/lib/alerting";
import {
  fetchAlertEvents,
  fetchAlertRules,
  ackAlert,
  ackAllAlerts,
  toggleRule,
  updateRuleChannels,
} from "@/lib/alerts-api";
import { getSettings, putSettings } from "@/lib/settings-api";
import { isServerMode } from "@/lib/server-client";

type Tab = "events" | "rules";

const SEVERITY_COLOR: Record<AlertSeverity, string> = {
  info: "var(--v2-status-running)",
  warning: "var(--v2-status-waiting)",
  critical: "var(--v2-status-failed)",
};

const SEVERITY_LABEL: Record<AlertSeverity, string> = {
  info: "Info",
  warning: "Warning",
  critical: "Critical",
};

// Ciclo de vida do alerta — como foi tratado pela ação do operador no job.
const RESOLUTION_INFO: Record<string, { label: string; title: string; color: string; icon: ReactNode }> = {
  ack: { label: "reconhecido", title: "Reconhecido manualmente", color: "var(--v2-status-ok)", icon: <Check size={12} /> },
  rerun: { label: "tratado · rerun", title: "Job re-executado pelo operador", color: "var(--v2-status-running)", icon: <RotateCcw size={11} /> },
  set_ok: { label: "tratado · set ok", title: "Marcado como OK pelo operador", color: "var(--v2-status-ok)", icon: <Check size={12} /> },
};

function relativeTime(ts: number): string {
  const diff = Date.now() - ts;
  const s = Math.floor(diff / 1000);
  if (s < 60) return `${s}s atrás`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}min atrás`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h atrás`;
  return new Date(ts).toLocaleString("pt-BR");
}

export function AlertsPanel({ onClose, onChange, isAdmin = false }: { onClose: () => void; onChange?: () => void; isAdmin?: boolean }) {
  const [tab, setTab] = useState<Tab>("events");

  // ESC fecha o modal
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  return (
    <div
      onClick={onClose}
      style={{
        position: "fixed", inset: 0, background: "rgba(0,0,0,0.6)", zIndex: 1000,
        display: "flex", alignItems: "center", justifyContent: "center",
        fontFamily: "var(--v2-font-sans)",
      }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="v2-grain-card v2-neon-card"
        style={{
          background: "var(--v2-bg-surface)", color: "var(--v2-text-primary)",
          width: "min(820px, 95vw)", height: "min(680px, 90vh)",
          display: "flex", flexDirection: "column",
        }}
      >
        <header style={{
          display: "flex", alignItems: "center", justifyContent: "space-between",
          padding: "12px 16px", borderBottom: "1px solid var(--v2-border-subtle)",
        }}>
          <div style={{ display: "flex", alignItems: "center", gap: 8, fontWeight: 600, fontSize: 14 }}>
            <Bell size={16} style={{ color: "var(--v2-accent-brand)" }} />
            Alertas
          </div>
          <button
            onClick={onClose}
            style={{ background: "transparent", color: "var(--v2-text-muted)", border: "none", cursor: "pointer", display: "flex" }}
            title="Fechar"
          >
            <X size={18} />
          </button>
        </header>

        <div style={{ display: "flex", gap: 6, padding: "8px 16px", borderBottom: "1px solid var(--v2-border-subtle)" }}>
          {(["events", "rules"] as const).map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              style={{
                background: tab === t ? "var(--v2-accent-deep)" : "transparent",
                color: tab === t ? "var(--v2-accent-brand)" : "var(--v2-text-secondary)",
                border: "1px solid " + (tab === t ? "var(--v2-accent-brand)" : "var(--v2-border-medium)"),
                padding: "5px 14px", borderRadius: 4, cursor: "pointer",
                fontSize: 11, fontFamily: "var(--v2-font-mono)", letterSpacing: "0.06em",
                textTransform: "uppercase", fontWeight: 600,
              }}
            >
              {t === "events" ? "Eventos" : "Regras"}
            </button>
          ))}
        </div>

        <div style={{ flex: 1, overflow: "auto", padding: 16 }}>
          {tab === "events" ? <EventsView onChange={onChange} /> : <RulesView isAdmin={isAdmin} />}
        </div>
      </div>
    </div>
  );
}

/* ── Events tab ── */

type SeverityFilter = "all" | AlertSeverity;

function EventsView({ onChange }: { onChange?: () => void }) {
  const [events, setEvents] = useState<AlertEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [onlyUnack, setOnlyUnack] = useState(false);
  const [severity, setSeverity] = useState<SeverityFilter>("all");

  const reload = useCallback(() => {
    fetchAlertEvents()
      .then((evs) => { setEvents(evs); setLoading(false); })
      .catch(() => setLoading(false));
  }, []);
  useEffect(() => { reload(); }, [reload]);

  const filtered = useMemo(() => {
    let list = [...events].sort((a, b) => b.timestamp - a.timestamp);
    if (onlyUnack) list = list.filter((e) => !e.acknowledged);
    if (severity !== "all") list = list.filter((e) => e.severity === severity);
    return list;
  }, [events, onlyUnack, severity]);

  const unackCount = useMemo(() => events.filter((e) => !e.acknowledged).length, [events]);

  const handleAck = (id: string) => { void ackAlert(id).then(() => { reload(); onChange?.(); }); };
  const handleAckAll = () => { void ackAllAlerts().then(() => { reload(); onChange?.(); }); };

  return (
    <div>
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 12, flexWrap: "wrap" }}>
        <FilterChip label="Não reconhecidos" active={onlyUnack} onClick={() => setOnlyUnack((v) => !v)} />
        <div style={{ display: "flex", gap: 4 }}>
          {(["all", "critical", "warning", "info"] as const).map((s) => (
            <FilterChip
              key={s}
              label={s === "all" ? "Todas" : SEVERITY_LABEL[s]}
              active={severity === s}
              dot={s === "all" ? undefined : SEVERITY_COLOR[s]}
              onClick={() => setSeverity(s)}
            />
          ))}
        </div>
        <div style={{ flex: 1 }} />
        <button
          onClick={handleAckAll}
          disabled={unackCount === 0}
          style={{
            display: "flex", alignItems: "center", gap: 6,
            padding: "5px 10px", borderRadius: 4,
            background: "transparent",
            border: "1px solid var(--v2-border-medium)",
            color: unackCount === 0 ? "var(--v2-text-muted)" : "var(--v2-text-secondary)",
            cursor: unackCount === 0 ? "not-allowed" : "pointer",
            fontSize: 11, fontFamily: "var(--v2-font-mono)",
          }}
        >
          <CheckCheck size={13} /> Reconhecer todos
          {unackCount > 0 && (
            <span style={{
              background: "var(--v2-status-failed)", color: "#fff", borderRadius: 8,
              padding: "0 6px", fontSize: 10, fontWeight: 700,
            }}>{unackCount}</span>
          )}
        </button>
      </div>

      {loading ? (
        <EmptyHint title="Carregando…" hint="Buscando alertas." />
      ) : filtered.length === 0 ? (
        <EmptyHint
          title="Nenhum alerta"
          hint={events.length === 0
            ? "Nenhum alerta foi disparado ainda. Eles aparecem aqui quando uma regra é satisfeita após a execução de um job."
            : "Nenhum alerta corresponde aos filtros selecionados."}
        />
      ) : (
        <div style={{ display: "grid", gap: 6 }}>
          {filtered.map((e) => (
            <div
              key={e.id}
              style={{
                display: "flex", alignItems: "flex-start", gap: 10,
                padding: "10px 12px", borderRadius: 5,
                background: "var(--v2-bg-elevated)",
                border: "1px solid var(--v2-border-subtle)",
                opacity: e.acknowledged ? 0.55 : 1,
              }}
            >
              <span style={{
                width: 8, height: 8, borderRadius: "50%", marginTop: 5, flexShrink: 0,
                background: SEVERITY_COLOR[e.severity],
              }} />
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ display: "flex", alignItems: "baseline", gap: 8, flexWrap: "wrap" }}>
                  <span style={{ fontSize: 12, fontWeight: 600, color: "var(--v2-text-primary)" }}>{e.ruleName}</span>
                  <span style={{
                    fontSize: 9, fontFamily: "var(--v2-font-mono)", textTransform: "uppercase",
                    letterSpacing: "0.06em", color: SEVERITY_COLOR[e.severity],
                  }}>{SEVERITY_LABEL[e.severity]}</span>
                  <span style={{ fontSize: 10, color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)" }}>
                    {relativeTime(e.timestamp)}
                  </span>
                </div>
                <div style={{ fontSize: 11, color: "var(--v2-text-secondary)", marginTop: 2, lineHeight: 1.4 }}>
                  {e.message}
                </div>
              </div>
              {(() => {
                const res = e.resolution || (e.acknowledged ? "ack" : "");
                if (!res) {
                  // alerta NOVO (não tratado) — botão de reconhecimento manual
                  return (
                    <button
                      onClick={() => handleAck(e.id)}
                      title="Reconhecer manualmente"
                      style={{
                        display: "flex", alignItems: "center", gap: 4, flexShrink: 0,
                        padding: "3px 8px", borderRadius: 4,
                        background: "transparent", border: "1px solid var(--v2-border-medium)",
                        color: "var(--v2-text-secondary)", cursor: "pointer",
                        fontSize: 10, fontFamily: "var(--v2-font-mono)",
                      }}
                    >
                      <Check size={12} /> ack
                    </button>
                  );
                }
                const info = RESOLUTION_INFO[res] ?? RESOLUTION_INFO.ack;
                return (
                  <span
                    title={info.title}
                    style={{
                      display: "flex", alignItems: "center", gap: 4, fontSize: 10,
                      color: info.color, fontFamily: "var(--v2-font-mono)", flexShrink: 0, marginTop: 2,
                    }}
                  >
                    {info.icon} {info.label}
                  </span>
                );
              })()}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

/* ── Rules tab ── */

function conditionSummary(rule: AlertRule): string {
  const c = rule.condition;
  switch (c.type) {
    case "failure": return "Quando o workflow falha";
    case "duration_exceeded": return `Quando a duração excede ${(c.thresholdMs / 1000).toFixed(0)}s`;
    case "retry_exceeded": return `Quando há mais de ${c.maxRetries} retries`;
    case "success_rate_below": return `Quando a taxa de sucesso cai abaixo de ${(c.rate * 100).toFixed(0)}%`;
    case "consecutive_failures": return `Quando há ${c.count} falhas consecutivas`;
    default: return "—";
  }
}

function RulesView({ isAdmin }: { isAdmin: boolean }) {
  const [rules, setRules] = useState<AlertRule[]>([]);
  const reload = useCallback(() => {
    fetchAlertRules().then(setRules).catch(() => setRules([]));
  }, []);
  useEffect(() => { reload(); }, [reload]);

  const handleToggle = (id: string) => { void toggleRule(id).then(reload); };

  return (
    <div style={{ display: "grid", gap: 6 }}>
      <ChannelsConfig isAdmin={isAdmin} />
      {rules.length === 0 && (
        <EmptyHint title="Nenhuma regra" hint="As regras padrão serão criadas automaticamente." />
      )}
      {rules.map((r) => (
        <div
          key={r.id}
          style={{
            display: "flex", alignItems: "center", gap: 12,
            padding: "10px 12px", borderRadius: 5,
            background: "var(--v2-bg-elevated)",
            border: "1px solid var(--v2-border-subtle)",
          }}
        >
          <span style={{
            width: 8, height: 8, borderRadius: "50%", flexShrink: 0,
            background: SEVERITY_COLOR[r.severity],
          }} />
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ display: "flex", alignItems: "baseline", gap: 8, flexWrap: "wrap" }}>
              <span style={{ fontSize: 12, fontWeight: 600 }}>{r.name}</span>
              <span style={{
                fontSize: 9, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)",
                padding: "1px 5px", border: "1px solid var(--v2-border-subtle)", borderRadius: 2,
              }}>
                {r.workflowPattern === "*" ? "todos os jobs" : r.workflowPattern}
              </span>
            </div>
            <div style={{ fontSize: 11, color: "var(--v2-text-secondary)", marginTop: 2 }}>
              {conditionSummary(r)}
            </div>
            <RuleChannelChips rule={r} isAdmin={isAdmin} onChanged={reload} />
          </div>
          <Toggle on={r.enabled} onClick={() => handleToggle(r.id)} />
        </div>
      ))}
    </div>
  );
}

/* ── Per-rule channels (routing por regra) ── */

function RuleChannelChips({ rule, isAdmin, onChanged }: { rule: AlertRule; isAdmin: boolean; onChanged: () => void }) {
  const [busy, setBusy] = useState(false);
  const active = new Set(rule.channels);
  const externalSelected = ALL_CHANNELS.some((c) => c !== "toast" && active.has(c));

  const toggle = (ch: AlertChannel) => {
    if (!isAdmin || ch === "toast" || busy) return;
    const next = new Set(active);
    if (next.has(ch)) next.delete(ch);
    else next.add(ch);
    next.add("toast"); // toast é sempre presente
    setBusy(true);
    updateRuleChannels(rule.id, ALL_CHANNELS.filter((c) => next.has(c)))
      .then(onChanged)
      .finally(() => setBusy(false));
  };

  return (
    <div style={{ marginTop: 6 }}>
      <div style={{ display: "flex", gap: 4, flexWrap: "wrap", alignItems: "center" }}>
        {ALL_CHANNELS.map((ch) => {
          const on = active.has(ch);
          const fixed = ch === "toast";
          return (
            <button
              key={ch}
              onClick={() => toggle(ch)}
              disabled={!isAdmin || fixed || busy}
              title={fixed ? "Sempre ativo (aparece na UI)" : isAdmin ? "Alternar canal de routing" : "Somente admin"}
              style={{
                fontSize: 9, fontFamily: "var(--v2-font-mono)", padding: "2px 7px", borderRadius: 8,
                cursor: !isAdmin || fixed ? "default" : "pointer",
                textTransform: "uppercase", letterSpacing: "0.04em",
                background: on ? "var(--v2-accent-deep)" : "transparent",
                color: on ? "var(--v2-accent-brand)" : "var(--v2-text-muted)",
                border: "1px solid " + (on ? "var(--v2-accent-brand)" : "var(--v2-border-medium)"),
                opacity: fixed ? 0.85 : 1,
              }}
            >
              {ch}
            </button>
          );
        })}
        <span style={{ fontSize: 9, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)", marginLeft: 4 }}>
          cooldown: {(rule.cooldownMs / 1000).toFixed(0)}s
        </span>
      </div>
      {isServerMode() && (
        <div style={{ fontSize: 9, color: "var(--v2-text-muted)", marginTop: 3 }}>
          {externalSelected ? "→ roteia só para os canais marcados" : "→ roteia para todos os sinks configurados"}
        </div>
      )}
    </div>
  );
}

/* ── Channels config (alert routing — server mode) ── */

function ChannelsConfig({ isAdmin }: { isAdmin: boolean }) {
  const [settings, setSettings] = useState<Record<string, string>>({});
  const [slack, setSlack] = useState("");
  const [hook, setHook] = useState("");
  const [pd, setPd] = useState("");
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);

  const load = useCallback(() => {
    getSettings().then(setSettings).catch(() => {});
  }, []);
  useEffect(() => { load(); }, [load]);

  if (!isServerMode()) return null; // routing externo é server-only

  const slackSet = settings["alert_slack_webhook_set"] === "true";
  const hookSet = settings["alert_webhook_url_set"] === "true";
  const pdSet = settings["alert_pagerduty_routing_key_set"] === "true";
  // key do SmtpConfig: muda quando os settings de SMTP mudam → remonta com valores novos.
  const smtpKey = ["alert_smtp_host", "alert_smtp_port", "alert_smtp_from", "alert_smtp_to", "alert_smtp_username", "alert_smtp_password_set"]
    .map((k) => settings[k] || "").join("|");

  const save = (patch: Record<string, string>) => {
    setBusy(true);
    putSettings(patch)
      .then((s) => { setSettings(s); setSlack(""); setHook(""); setPd(""); setSaved(true); setTimeout(() => setSaved(false), 2000); })
      .catch(() => {})
      .finally(() => setBusy(false));
  };

  return (
    <div style={{
      padding: "10px 12px", borderRadius: 5, marginBottom: 6,
      background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-subtle)",
    }}>
      <div style={{ display: "flex", alignItems: "center", gap: 6, marginBottom: 8 }}>
        <Send size={13} style={{ color: "var(--v2-accent-brand)" }} />
        <span style={{ fontSize: 12, fontWeight: 600 }}>Canais de notificação</span>
        {saved && <span style={{ fontSize: 10, color: "var(--v2-status-ok)", fontFamily: "var(--v2-font-mono)" }}>✓ salvo</span>}
      </div>
      <div style={{ fontSize: 10, color: "var(--v2-text-muted)", marginBottom: 10, lineHeight: 1.4 }}>
        Configure os destinos abaixo; cada regra escolhe para quais roteia (nos chips de cada regra).
        Regra sem canal externo marcado → roteia para todos os configurados.
      </div>
      <ChannelRow
        label="Slack"
        placeholder="https://hooks.slack.com/services/…"
        configured={slackSet}
        value={slack}
        onChange={setSlack}
        isAdmin={isAdmin}
        busy={busy}
        onSave={() => save({ alert_slack_webhook: slack })}
        onClear={() => save({ alert_slack_webhook: "" })}
      />
      <ChannelRow
        label="Webhook genérico"
        placeholder="https://exemplo.com/alertas (JSON)"
        configured={hookSet}
        value={hook}
        onChange={setHook}
        isAdmin={isAdmin}
        busy={busy}
        onSave={() => save({ alert_webhook_url: hook })}
        onClear={() => save({ alert_webhook_url: "" })}
      />
      <ChannelRow
        label="PagerDuty"
        placeholder="routing/integration key"
        inputType="text"
        configured={pdSet}
        value={pd}
        onChange={setPd}
        isAdmin={isAdmin}
        busy={busy}
        onSave={() => save({ alert_pagerduty_routing_key: pd })}
        onClear={() => save({ alert_pagerduty_routing_key: "" })}
      />
      <SmtpConfig key={smtpKey} settings={settings} isAdmin={isAdmin} busy={busy} onSave={save} />
      {!isAdmin && (
        <div style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 6 }}>
          Apenas admins podem alterar os canais.
        </div>
      )}
    </div>
  );
}

/* ── SMTP (e-mail) — sub-form de múltiplos campos ── */

function SmtpConfig({ settings, isAdmin, busy, onSave }: {
  settings: Record<string, string>; isAdmin: boolean; busy: boolean; onSave: (patch: Record<string, string>) => void;
}) {
  // Inicializa dos valores atuais (não-secretos). O parent passa `key` derivada de
  // settings, então quando os settings mudam o componente remonta e re-inicializa
  // — sem setState dentro de effect.
  const [host, setHost] = useState(settings["alert_smtp_host"] || "");
  const [port, setPort] = useState(settings["alert_smtp_port"] || "");
  const [from, setFrom] = useState(settings["alert_smtp_from"] || "");
  const [to, setTo] = useState(settings["alert_smtp_to"] || "");
  const [user, setUser] = useState(settings["alert_smtp_username"] || "");
  const [pass, setPass] = useState("");

  const passSet = settings["alert_smtp_password_set"] === "true";
  const configured = (settings["alert_smtp_host"] || "") !== "" && (settings["alert_smtp_to"] || "") !== "";

  const inputStyle: CSSProperties = {
    minWidth: 0, fontSize: 11, padding: "4px 8px", borderRadius: 4,
    background: "var(--v2-bg-surface)", color: "var(--v2-text-primary)",
    border: "1px solid var(--v2-border-medium)", fontFamily: "var(--v2-font-mono)",
  };

  const save = () => {
    const patch: Record<string, string> = {
      alert_smtp_host: host, alert_smtp_port: port, alert_smtp_from: from,
      alert_smtp_to: to, alert_smtp_username: user,
    };
    if (pass) patch.alert_smtp_password = pass; // só sobrescreve a senha se digitada
    onSave(patch);
    setPass("");
  };
  const clear = () => onSave({
    alert_smtp_host: "", alert_smtp_port: "", alert_smtp_from: "",
    alert_smtp_to: "", alert_smtp_username: "", alert_smtp_password: "",
  });

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 6, marginTop: 8, paddingTop: 8, borderTop: "1px dashed var(--v2-border-subtle)" }}>
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <span style={{ fontSize: 11, color: "var(--v2-text-secondary)", width: 120, flexShrink: 0 }}>E-mail (SMTP)</span>
        <span style={{
          fontSize: 9, fontFamily: "var(--v2-font-mono)", padding: "1px 6px", borderRadius: 8,
          background: configured ? "var(--v2-accent-deep)" : "transparent",
          color: configured ? "var(--v2-status-ok)" : "var(--v2-text-muted)",
          border: "1px solid " + (configured ? "var(--v2-accent-brand)" : "var(--v2-border-medium)"),
        }}>{configured ? "configurado" : "—"}</span>
      </div>
      {isAdmin && (
        <>
          <div style={{ display: "flex", gap: 6 }}>
            <input style={{ ...inputStyle, flex: 2 }} placeholder="host (smtp.exemplo.com)" value={host} disabled={busy} onChange={(e) => setHost(e.target.value)} />
            <input style={{ ...inputStyle, width: 70 }} placeholder="porta" value={port} disabled={busy} onChange={(e) => setPort(e.target.value)} />
          </div>
          <div style={{ display: "flex", gap: 6 }}>
            <input style={{ ...inputStyle, flex: 1 }} placeholder="from (alertas@…)" value={from} disabled={busy} onChange={(e) => setFrom(e.target.value)} />
            <input style={{ ...inputStyle, flex: 1 }} placeholder="to (a@x.com, b@y.com)" value={to} disabled={busy} onChange={(e) => setTo(e.target.value)} />
          </div>
          <div style={{ display: "flex", gap: 6 }}>
            <input style={{ ...inputStyle, flex: 1 }} placeholder="usuário (opcional)" value={user} disabled={busy} onChange={(e) => setUser(e.target.value)} />
            <input style={{ ...inputStyle, flex: 1 }} type="password" placeholder={passSet ? "senha (••• salva)" : "senha (opcional)"} value={pass} disabled={busy} onChange={(e) => setPass(e.target.value)} />
          </div>
          <div style={{ display: "flex", gap: 6 }}>
            <button
              onClick={save}
              disabled={busy || !host || !to}
              style={{
                padding: "4px 10px", borderRadius: 4, fontSize: 10,
                background: host && to ? "var(--v2-accent-brand)" : "transparent",
                color: host && to ? "#000" : "var(--v2-text-muted)",
                border: "1px solid " + (host && to ? "var(--v2-accent-brand)" : "var(--v2-border-medium)"),
                cursor: host && to && !busy ? "pointer" : "not-allowed", fontWeight: 600,
              }}
            >Salvar SMTP</button>
            {configured && (
              <button
                onClick={clear}
                disabled={busy}
                style={{ padding: "4px 8px", borderRadius: 4, fontSize: 10, background: "transparent", color: "var(--v2-status-failed)", border: "1px solid var(--v2-border-medium)", cursor: "pointer" }}
              >limpar</button>
            )}
          </div>
        </>
      )}
    </div>
  );
}

function ChannelRow({ label, placeholder, configured, value, onChange, isAdmin, busy, onSave, onClear, inputType = "url" }: {
  label: string; placeholder: string; configured: boolean; value: string;
  onChange: (v: string) => void; isAdmin: boolean; busy: boolean; onSave: () => void; onClear: () => void;
  inputType?: string;
}) {
  return (
    <div style={{ display: "flex", alignItems: "center", gap: 8, marginTop: 6 }}>
      <span style={{ fontSize: 11, color: "var(--v2-text-secondary)", width: 120, flexShrink: 0 }}>{label}</span>
      <span style={{
        fontSize: 9, fontFamily: "var(--v2-font-mono)", padding: "1px 6px", borderRadius: 8, flexShrink: 0,
        background: configured ? "var(--v2-accent-deep)" : "transparent",
        color: configured ? "var(--v2-status-ok)" : "var(--v2-text-muted)",
        border: "1px solid " + (configured ? "var(--v2-accent-brand)" : "var(--v2-border-medium)"),
      }}>{configured ? "configurado" : "—"}</span>
      {isAdmin && (
        <>
          <input
            type={inputType}
            value={value}
            placeholder={placeholder}
            onChange={(e) => onChange(e.target.value)}
            disabled={busy}
            style={{
              flex: 1, minWidth: 0, fontSize: 11, padding: "4px 8px", borderRadius: 4,
              background: "var(--v2-bg-surface)", color: "var(--v2-text-primary)",
              border: "1px solid var(--v2-border-medium)", fontFamily: "var(--v2-font-mono)",
            }}
          />
          <button
            onClick={onSave}
            disabled={busy || !value}
            style={{
              padding: "4px 10px", borderRadius: 4, fontSize: 10, flexShrink: 0,
              background: value ? "var(--v2-accent-brand)" : "transparent",
              color: value ? "#000" : "var(--v2-text-muted)",
              border: "1px solid " + (value ? "var(--v2-accent-brand)" : "var(--v2-border-medium)"),
              cursor: value && !busy ? "pointer" : "not-allowed", fontWeight: 600,
            }}
          >Salvar</button>
          {configured && (
            <button
              onClick={onClear}
              disabled={busy}
              title="Remover destino"
              style={{
                padding: "4px 8px", borderRadius: 4, fontSize: 10, flexShrink: 0,
                background: "transparent", color: "var(--v2-status-failed)",
                border: "1px solid var(--v2-border-medium)", cursor: "pointer",
              }}
            >limpar</button>
          )}
        </>
      )}
    </div>
  );
}

/* ── Small UI pieces ── */

function FilterChip({ label, active, dot, onClick }: { label: string; active: boolean; dot?: string; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      style={{
        display: "inline-flex", alignItems: "center", gap: 5,
        padding: "4px 9px", borderRadius: 4, cursor: "pointer",
        background: active ? "var(--v2-accent-deep)" : "transparent",
        border: "1px solid " + (active ? "var(--v2-accent-brand)" : "var(--v2-border-medium)"),
        color: active ? "var(--v2-accent-brand)" : "var(--v2-text-secondary)",
        fontSize: 10, fontFamily: "var(--v2-font-mono)", letterSpacing: "0.04em",
      }}
    >
      {dot && <span style={{ width: 7, height: 7, borderRadius: "50%", background: dot }} />}
      {label}
    </button>
  );
}

function Toggle({ on, onClick }: { on: boolean; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      title={on ? "Desabilitar regra" : "Habilitar regra"}
      style={{
        position: "relative", width: 36, height: 20, borderRadius: 10, flexShrink: 0,
        background: on ? "var(--v2-accent-brand)" : "var(--v2-border-medium)",
        border: "none", cursor: "pointer", transition: "background 0.15s",
      }}
    >
      <span style={{
        position: "absolute", top: 2, left: on ? 18 : 2,
        width: 16, height: 16, borderRadius: "50%", background: "#fff",
        transition: "left 0.15s",
      }} />
    </button>
  );
}

function EmptyHint({ title, hint }: { title: string; hint: string }) {
  return (
    <div style={{ textAlign: "center", padding: "40px 20px", color: "var(--v2-text-muted)" }}>
      <div style={{ fontSize: 13, fontWeight: 600, color: "var(--v2-text-secondary)", marginBottom: 6 }}>{title}</div>
      <div style={{ fontSize: 11, lineHeight: 1.5, maxWidth: 380, margin: "0 auto" }}>{hint}</div>
    </div>
  );
}
