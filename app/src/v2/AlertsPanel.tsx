/**
 * AlertsPanel — Phase 8 alerting UI.
 *
 * Modal with two tabs:
 *   - Events: fired alert events, acknowledge (single / all), filters.
 *   - Rules:  enable/disable alert rules, inspect condition/severity/channels.
 *
 * Reads/writes through @/lib/alerting. Styled with v2 design tokens.
 */
import { useCallback, useEffect, useMemo, useState } from "react";
import { Bell, Check, CheckCheck, X, Send } from "lucide-react";
import type { AlertEvent, AlertRule, AlertSeverity } from "@/lib/alerting";
import {
  fetchAlertEvents,
  fetchAlertRules,
  ackAlert,
  ackAllAlerts,
  toggleRule,
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
        className="v2-grain-card"
        style={{
          background: "var(--v2-bg-surface)", color: "var(--v2-text-primary)",
          borderRadius: 8, width: "min(820px, 95vw)", height: "min(680px, 90vh)",
          display: "flex", flexDirection: "column",
          border: "1px solid var(--v2-border-medium)",
          boxShadow: "0 20px 60px rgba(0,0,0,0.7)",
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
              {e.acknowledged ? (
                <span style={{
                  display: "flex", alignItems: "center", gap: 3, fontSize: 10,
                  color: "var(--v2-status-ok)", fontFamily: "var(--v2-font-mono)", flexShrink: 0, marginTop: 2,
                }}>
                  <Check size={12} /> ok
                </span>
              ) : (
                <button
                  onClick={() => handleAck(e.id)}
                  title="Reconhecer"
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
              )}
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
            <div style={{ display: "flex", gap: 10, marginTop: 4, fontSize: 9, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)" }}>
              <span>canais: {r.channels.join(", ")}</span>
              <span>cooldown: {(r.cooldownMs / 1000).toFixed(0)}s</span>
            </div>
          </div>
          <Toggle on={r.enabled} onClick={() => handleToggle(r.id)} />
        </div>
      ))}
    </div>
  );
}

/* ── Channels config (alert routing — server mode) ── */

function ChannelsConfig({ isAdmin }: { isAdmin: boolean }) {
  const [settings, setSettings] = useState<Record<string, string>>({});
  const [slack, setSlack] = useState("");
  const [hook, setHook] = useState("");
  const [busy, setBusy] = useState(false);
  const [saved, setSaved] = useState(false);

  const load = useCallback(() => {
    getSettings().then(setSettings).catch(() => {});
  }, []);
  useEffect(() => { load(); }, [load]);

  if (!isServerMode()) return null; // routing externo é server-only

  const slackSet = settings["alert_slack_webhook_set"] === "true";
  const hookSet = settings["alert_webhook_url_set"] === "true";

  const save = (patch: Record<string, string>) => {
    setBusy(true);
    putSettings(patch)
      .then((s) => { setSettings(s); setSlack(""); setHook(""); setSaved(true); setTimeout(() => setSaved(false), 2000); })
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
        Sinks globais: todo alerta disparado é enviado para os destinos configurados.
      </div>
      <ChannelRow
        label="Slack / webhook"
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
      {!isAdmin && (
        <div style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 6 }}>
          Apenas admins podem alterar os canais.
        </div>
      )}
    </div>
  );
}

function ChannelRow({ label, placeholder, configured, value, onChange, isAdmin, busy, onSave, onClear }: {
  label: string; placeholder: string; configured: boolean; value: string;
  onChange: (v: string) => void; isAdmin: boolean; busy: boolean; onSave: () => void; onClear: () => void;
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
            type="url"
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
