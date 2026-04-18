import { useEffect, useState } from "react";
import type { JobInstance } from "@/lib/orchestrator-model";
import { fetchInstanceEvents, type InstanceEvent } from "@/lib/runtime-bridge";

/* ──────────────────────────────────────────────────────────────
   InstanceDetailsDrawer — painel lateral direito com ações
   ──────────────────────────────────────────────────────────────
   Estilo PicPay: preto, denso, mono para dados técnicos,
   botões compactos semânticos.
   ────────────────────────────────────────────────────────────── */

export interface InstanceActionHandlers {
  onHold: (id: string) => void;
  onRelease: (id: string) => void;
  onCancel: (id: string) => void;
  onRerun: (id: string) => void;
  onSkip: (id: string) => void;
  onBypass: (id: string) => void;
  onClose: () => void;
}

const STATUS_COLOR: Record<JobInstance["status"], string> = {
  OK: "var(--v2-status-ok)",
  NOTOK: "var(--v2-status-failed)",
  RUNNING: "var(--v2-status-running)",
  WAITING: "var(--v2-status-waiting)",
  HOLD: "var(--v2-text-secondary)",
  CANCELLED: "var(--v2-text-muted)",
};

const STATUS_LABEL: Record<JobInstance["status"], string> = {
  OK: "OK",
  NOTOK: "NOT OK",
  RUNNING: "RUNNING",
  WAITING: "WAITING",
  HOLD: "HOLD",
  CANCELLED: "CANCELLED",
};

function fmtTime(ms?: number): string {
  if (!ms) return "—";
  return new Date(ms).toLocaleTimeString("en-GB", { hour12: false });
}

function fmtDuration(ms?: number): string {
  if (!ms) return "—";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  const m = Math.floor(ms / 60000);
  const s = Math.floor((ms % 60000) / 1000);
  return `${m}m${s.toString().padStart(2, "0")}s`;
}

interface ActionButton {
  label: string;
  onClick: () => void;
  tone: "neutral" | "danger" | "primary";
  show: boolean;
}

export default function InstanceDetailsDrawer({
  instance,
  handlers,
}: {
  instance: JobInstance;
  handlers: InstanceActionHandlers;
}) {
  const status = instance.status;
  const color = STATUS_COLOR[status];
  const [tab, setTab] = useState<"details" | "log">("details");

  const actions: ActionButton[] = ([
    { label: "Hold",    onClick: () => handlers.onHold(instance.id),    tone: "neutral" as const, show: status === "WAITING" },
    { label: "Release", onClick: () => handlers.onRelease(instance.id), tone: "primary" as const, show: status === "HOLD" },
    { label: "Cancel",  onClick: () => handlers.onCancel(instance.id),  tone: "danger"  as const, show: status === "WAITING" || status === "HOLD" },
    { label: "Skip",    onClick: () => handlers.onSkip(instance.id),    tone: "neutral" as const, show: status === "WAITING" || status === "HOLD" },
    { label: "Set OK", onClick: () => handlers.onBypass(instance.id), tone: "primary" as const, show: status === "NOTOK" || status === "CANCELLED" },
    { label: "Rerun",   onClick: () => handlers.onRerun(instance.id),   tone: "primary" as const, show: status === "NOTOK" },
  ]).filter((a) => a.show);

  return (
    <aside
      className="v2-grain v2-edge-highlight"
      style={{
        position: "absolute",
        top: 12,
        right: 12,
        bottom: 12,
        width: 340,
        background: "var(--v2-bg-surface)",
        border: "1px solid var(--v2-border-medium)",
        borderRadius: 4,
        display: "flex",
        flexDirection: "column",
        zIndex: 10,
        fontFamily: "var(--v2-font-sans)",
        color: "var(--v2-text-primary)",
      }}
    >
      {/* Header */}
      <header
        style={{
          padding: "10px 12px",
          borderBottom: "1px solid var(--v2-border-subtle)",
          display: "flex",
          alignItems: "center",
          gap: 8,
        }}
      >
        <span
          style={{
            width: 8,
            height: 8,
            borderRadius: "50%",
            background: color,
            flexShrink: 0,
          }}
        />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div
            style={{
              fontSize: 12,
              fontWeight: 600,
              whiteSpace: "nowrap",
              overflow: "hidden",
              textOverflow: "ellipsis",
            }}
          >
            {instance.label}
          </div>
          <div
            style={{
              fontSize: 10,
              fontFamily: "var(--v2-font-mono)",
              color: "var(--v2-text-muted)",
              letterSpacing: "0.04em",
              marginTop: 2,
            }}
          >
            {instance.jobType} · <span style={{ color: "var(--v2-accent-brand)" }}>{instance.team ?? "—"}</span> · <span style={{ color }}>{STATUS_LABEL[status]}</span>
          </div>
        </div>
        <button
          onClick={handlers.onClose}
          style={{
            background: "transparent",
            border: "none",
            color: "var(--v2-text-muted)",
            cursor: "pointer",
            fontSize: 14,
            padding: 4,
          }}
          aria-label="close"
        >
          ×
        </button>
      </header>

      {/* Tabs */}
      <div
        style={{
          display: "flex",
          borderBottom: "1px solid var(--v2-border-subtle)",
          fontFamily: "var(--v2-font-mono)",
          fontSize: 10,
          letterSpacing: "0.08em",
          textTransform: "uppercase",
        }}
      >
        {(["details", "log"] as const).map((t) => {
          const active = tab === t;
          return (
            <button
              key={t}
              onClick={() => setTab(t)}
              style={{
                flex: 1,
                padding: "7px 8px",
                background: active ? "var(--v2-bg-elevated)" : "transparent",
                color: active ? "var(--v2-accent-brand)" : "var(--v2-text-muted)",
                border: "none",
                borderBottom: active ? "2px solid var(--v2-accent-brand)" : "2px solid transparent",
                cursor: "pointer",
                fontWeight: 600,
              }}
            >
              {t}
            </button>
          );
        })}
      </div>

      {/* Body */}
      {tab === "details" ? (
      <div style={{ flex: 1, overflowY: "auto", padding: "10px 12px", fontSize: 11 }}>
        <Section title="Timeline">
          <Field label="Ordered"    value={fmtTime(instance.createdAt)} />
          <Field label="Scheduled"  value={fmtTime(instance.scheduledAt)} />
          <Field label="Started"    value={fmtTime(instance.startedAt)} />
          <Field label="Completed"  value={fmtTime(instance.completedAt)} />
          <Field label="Duration"   value={fmtDuration(instance.durationMs)} />
          <Field label="Attempts"   value={`${instance.attempts} / ${instance.retries + 1}`} />
        </Section>

        <Section title="Config">
          <Field label="Definition" value={instance.definitionId} mono />
          <Field label="Order date" value={instance.orderDate} mono />
          <Field label="Manual"     value={instance.manual ? "yes" : "no"} />
          <Field label="Dry run"    value={instance.dryRun ? "yes" : "no"} />
          <Field label="Timeout"    value={`${instance.timeout}s`} />
        </Section>

        {instance.error && (
          <Section title="Error">
            <pre
              style={{
                margin: 0,
                padding: "6px 8px",
                background: "var(--v2-bg-deep)",
                border: "1px solid var(--v2-border-subtle)",
                borderRadius: 2,
                fontFamily: "var(--v2-font-mono)",
                fontSize: 10,
                color: "var(--v2-status-failed)",
                whiteSpace: "pre-wrap",
                wordBreak: "break-word",
              }}
            >
              {instance.error}
            </pre>
          </Section>
        )}

        {instance.output && Object.keys(instance.output).length > 0 && (
          <Section title="Output">
            <pre
              style={{
                margin: 0,
                padding: "6px 8px",
                background: "var(--v2-bg-deep)",
                border: "1px solid var(--v2-border-subtle)",
                borderRadius: 2,
                fontFamily: "var(--v2-font-mono)",
                fontSize: 10,
                color: "var(--v2-text-secondary)",
                whiteSpace: "pre-wrap",
                wordBreak: "break-word",
                maxHeight: 140,
                overflowY: "auto",
              }}
            >
              {JSON.stringify(instance.output, null, 2)}
            </pre>
          </Section>
        )}
      </div>
      ) : (
        <LogPanel instanceId={instance.id} status={status} />
      )}

      {/* Actions */}
      {actions.length > 0 && (
        <footer
          style={{
            padding: "8px 12px",
            borderTop: "1px solid var(--v2-border-subtle)",
            display: "flex",
            flexWrap: "wrap",
            gap: 6,
          }}
        >
          {actions.map((a) => (
            <button
              key={a.label}
              onClick={a.onClick}
              style={{
                padding: "5px 10px",
                fontSize: 10,
                fontFamily: "var(--v2-font-mono)",
                letterSpacing: "0.06em",
                textTransform: "uppercase",
                cursor: "pointer",
                border: `1px solid ${
                  a.tone === "primary"
                    ? "var(--v2-accent-brand)"
                    : a.tone === "danger"
                    ? "var(--v2-status-failed)"
                    : "var(--v2-border-medium)"
                }`,
                background:
                  a.tone === "primary"
                    ? "var(--v2-accent-deep)"
                    : a.tone === "danger"
                    ? "rgba(239,68,68,0.08)"
                    : "var(--v2-bg-elevated)",
                color:
                  a.tone === "primary"
                    ? "var(--v2-accent-brand)"
                    : a.tone === "danger"
                    ? "var(--v2-status-failed)"
                    : "var(--v2-text-secondary)",
                borderRadius: 3,
                fontWeight: 600,
              }}
            >
              {a.label}
            </button>
          ))}
        </footer>
      )}
    </aside>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div style={{ marginBottom: 14 }}>
      <div
        style={{
          fontSize: 9,
          fontFamily: "var(--v2-font-mono)",
          color: "var(--v2-text-muted)",
          letterSpacing: "0.1em",
          textTransform: "uppercase",
          marginBottom: 6,
        }}
      >
        {title}
      </div>
      <div>{children}</div>
    </div>
  );
}

function Field({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div
      style={{
        display: "flex",
        justifyContent: "space-between",
        padding: "3px 0",
        borderBottom: "1px dashed var(--v2-border-subtle)",
        fontSize: 11,
      }}
    >
      <span style={{ color: "var(--v2-text-muted)" }}>{label}</span>
      <span
        style={{
          color: "var(--v2-text-primary)",
          fontFamily: mono ? "var(--v2-font-mono)" : undefined,
          fontSize: mono ? 10 : 11,
          maxWidth: "60%",
          whiteSpace: "nowrap",
          overflow: "hidden",
          textOverflow: "ellipsis",
        }}
        title={value}
      >
        {value}
      </span>
    </div>
  );
}

const KIND_COLOR: Record<string, string> = {
  ordered:        "var(--v2-text-muted)",
  "force-ordered":"var(--v2-accent-brand)",
  started:        "var(--v2-status-running)",
  submitted:      "var(--v2-text-secondary)",
  finished:       "var(--v2-status-ok)",
  timeout:        "var(--v2-status-failed)",
  cancelled:      "var(--v2-text-muted)",
  held:           "var(--v2-text-secondary)",
  released:       "var(--v2-accent-brand)",
  rerun:          "var(--v2-accent-brand)",
  "set-ok":       "var(--v2-accent-brand)",
};

function fmtTS(ts: string): string {
  const d = new Date(ts);
  if (isNaN(d.getTime())) return ts;
  return d.toLocaleTimeString("en-GB", { hour12: false }) +
    "." + String(d.getMilliseconds()).padStart(3, "0");
}

function LogPanel({ instanceId, status }: { instanceId: string; status: JobInstance["status"] }) {
  const [events, setEvents] = useState<InstanceEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setInterval> | undefined;

    const load = () => {
      fetchInstanceEvents(instanceId)
        .then((evs) => {
          if (cancelled) return;
          setEvents(evs);
          setError(null);
        })
        .catch((e) => {
          if (cancelled) return;
          setError(e?.message ?? String(e));
        })
        .finally(() => {
          if (!cancelled) setLoading(false);
        });
    };

    load();
    // Auto-refresh enquanto a instance estiver em estado mutável (RUNNING/WAITING/HOLD)
    if (status === "RUNNING" || status === "WAITING" || status === "HOLD") {
      timer = setInterval(load, 3000);
    }
    return () => {
      cancelled = true;
      if (timer) clearInterval(timer);
    };
  }, [instanceId, status]);

  return (
    <div style={{ flex: 1, overflowY: "auto", padding: "10px 12px", fontSize: 11 }}>
      {loading && events.length === 0 && (
        <div style={{ color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)", fontSize: 10 }}>
          loading…
        </div>
      )}
      {error && (
        <div style={{ color: "var(--v2-status-failed)", fontFamily: "var(--v2-font-mono)", fontSize: 10 }}>
          {error}
        </div>
      )}
      {!loading && !error && events.length === 0 && (
        <div style={{ color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)", fontSize: 10 }}>
          no events yet
        </div>
      )}
      {events.map((e) => {
        const color = KIND_COLOR[e.kind] ?? "var(--v2-text-secondary)";
        return (
          <div
            key={e.id}
            style={{
              display: "grid",
              gridTemplateColumns: "78px 1fr",
              gap: 8,
              padding: "5px 0",
              borderBottom: "1px dashed var(--v2-border-subtle)",
            }}
          >
            <span
              style={{
                fontFamily: "var(--v2-font-mono)",
                fontSize: 9,
                color: "var(--v2-text-muted)",
                whiteSpace: "nowrap",
              }}
              title={e.ts}
            >
              {fmtTS(e.ts)}
            </span>
            <div style={{ minWidth: 0 }}>
              <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
                <span
                  style={{
                    fontFamily: "var(--v2-font-mono)",
                    fontSize: 9,
                    color,
                    fontWeight: 700,
                    letterSpacing: "0.06em",
                    textTransform: "uppercase",
                  }}
                >
                  {e.kind}
                </span>
                {e.actor && (
                  <span style={{ fontSize: 9, color: "var(--v2-text-muted)" }}>
                    · {e.actor}
                  </span>
                )}
              </div>
              {e.message && (
                <div
                  style={{
                    fontSize: 10,
                    color: "var(--v2-text-secondary)",
                    fontFamily: "var(--v2-font-mono)",
                    marginTop: 2,
                    wordBreak: "break-word",
                  }}
                >
                  {e.message}
                </div>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
