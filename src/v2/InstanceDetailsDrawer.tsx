import type { JobInstance } from "@/lib/orchestrator-model";

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

  const actions: ActionButton[] = ([
    { label: "Hold",    onClick: () => handlers.onHold(instance.id),    tone: "neutral" as const, show: status === "WAITING" },
    { label: "Release", onClick: () => handlers.onRelease(instance.id), tone: "primary" as const, show: status === "HOLD" },
    { label: "Cancel",  onClick: () => handlers.onCancel(instance.id),  tone: "danger"  as const, show: status === "WAITING" || status === "HOLD" },
    { label: "Skip",    onClick: () => handlers.onSkip(instance.id),    tone: "neutral" as const, show: status === "WAITING" || status === "HOLD" },
    { label: "Bypass",  onClick: () => handlers.onBypass(instance.id),  tone: "neutral" as const, show: status === "NOTOK" },
    { label: "Rerun",   onClick: () => handlers.onRerun(instance.id),   tone: "primary" as const, show: status === "NOTOK" },
  ]).filter((a) => a.show);

  return (
    <aside
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
            {instance.jobType} · {instance.team ?? "—"} · <span style={{ color }}>{STATUS_LABEL[status]}</span>
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

      {/* Body */}
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
