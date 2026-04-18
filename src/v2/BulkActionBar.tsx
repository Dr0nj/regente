/**
 * BulkActionBar — floating bar visible when canvas multi-selection is non-empty.
 * Mode-aware:
 *   - monitoring: Hold All / Release All / Cancel All / Set OK All / Rerun All
 *     buttons are gated by counts of eligible instances in the selection.
 *   - design: Delete All (bulk).
 *
 * Counts em badge ao lado de cada botão indicam quantas instâncias da
 * seleção sofrerão a ação (não o total selecionado). Botão fica disabled
 * se count == 0.
 */
import { useCallback, useMemo } from "react";
import type { JobInstance, JobDefinition } from "@/lib/orchestrator-model";

interface MonitoringHandlers {
  onHoldAll: (ids: string[]) => Promise<void> | void;
  onReleaseAll: (ids: string[]) => Promise<void> | void;
  onCancelAll: (ids: string[]) => Promise<void> | void;
  onSetOkAll: (ids: string[]) => Promise<void> | void;
  onRerunAll: (ids: string[]) => Promise<void> | void;
  onClear: () => void;
}

interface DesignHandlers {
  onDeleteAll: (ids: string[]) => Promise<void> | void;
  onClear: () => void;
}

interface CommonProps {
  selected: Set<string>;
}

interface MonitoringProps extends CommonProps {
  mode: "monitoring";
  instances: JobInstance[];
  handlers: MonitoringHandlers;
}

interface DesignProps extends CommonProps {
  mode: "design";
  defs: JobDefinition[];
  handlers: DesignHandlers;
}

type Props = MonitoringProps | DesignProps;

export default function BulkActionBar(props: Props) {
  if (props.mode === "monitoring") return <MonitoringBar {...props} />;
  return <DesignBar {...props} />;
}

function MonitoringBar({ selected, instances, handlers }: MonitoringProps) {
  const ids = useMemo(() => [...selected], [selected]);
  const selectedInstances = useMemo(
    () => instances.filter((i) => selected.has(i.id)),
    [instances, selected],
  );

  const eligibleHold = selectedInstances.filter((i) => i.status === "WAITING").map((i) => i.id);
  const eligibleRelease = selectedInstances.filter((i) => i.status === "HOLD").map((i) => i.id);
  const eligibleCancel = selectedInstances
    .filter((i) => i.status === "WAITING" || i.status === "HOLD")
    .map((i) => i.id);
  const eligibleSetOk = selectedInstances
    .filter((i) => i.status === "NOTOK" || i.status === "CANCELLED")
    .map((i) => i.id);
  const eligibleRerun = selectedInstances
    .filter((i) => i.status === "OK" || i.status === "NOTOK" || i.status === "CANCELLED")
    .map((i) => i.id);

  const confirmAndRun = useCallback(
    async (label: string, eligible: string[], fn: (ids: string[]) => Promise<void> | void) => {
      if (eligible.length === 0) return;
      if (!window.confirm(`${label} ${eligible.length} instance${eligible.length === 1 ? "" : "s"}?`)) return;
      await fn(eligible);
    },
    [],
  );

  return (
    <Bar count={ids.length} onClear={handlers.onClear}>
      <Btn
        label="Hold all"
        count={eligibleHold.length}
        tone="neutral"
        onClick={() => void confirmAndRun("Hold", eligibleHold, handlers.onHoldAll)}
      />
      <Btn
        label="Release all"
        count={eligibleRelease.length}
        tone="primary"
        onClick={() => void confirmAndRun("Release", eligibleRelease, handlers.onReleaseAll)}
      />
      <Btn
        label="Cancel all"
        count={eligibleCancel.length}
        tone="danger"
        onClick={() => void confirmAndRun("Cancel", eligibleCancel, handlers.onCancelAll)}
      />
      <Btn
        label="Set OK all"
        count={eligibleSetOk.length}
        tone="primary"
        onClick={() => void confirmAndRun("Set OK", eligibleSetOk, handlers.onSetOkAll)}
      />
      <Btn
        label="Rerun all"
        count={eligibleRerun.length}
        tone="neutral"
        onClick={() => void confirmAndRun("Rerun", eligibleRerun, handlers.onRerunAll)}
      />
    </Bar>
  );
}

function DesignBar({ selected, defs, handlers }: DesignProps) {
  const ids = useMemo(() => [...selected], [selected]);
  const eligibleDelete = ids.filter((id) => defs.some((d) => d.id === id));

  const handleDelete = useCallback(async () => {
    if (eligibleDelete.length === 0) return;
    if (!window.confirm(`Delete ${eligibleDelete.length} definition${eligibleDelete.length === 1 ? "" : "s"}? This cannot be undone.`)) return;
    await handlers.onDeleteAll(eligibleDelete);
  }, [eligibleDelete, handlers]);

  return (
    <Bar count={ids.length} onClear={handlers.onClear}>
      <Btn
        label="Delete all"
        count={eligibleDelete.length}
        tone="danger"
        onClick={() => void handleDelete()}
      />
    </Bar>
  );
}

function Bar({ count, onClear, children }: { count: number; onClear: () => void; children: React.ReactNode }) {
  return (
    <div
      className="v2-edge-highlight"
      style={{
        position: "absolute",
        bottom: 16,
        left: "50%",
        transform: "translateX(-50%)",
        display: "flex",
        alignItems: "center",
        gap: 8,
        padding: "8px 12px",
        background: "var(--v2-bg-surface)",
        border: "1px solid var(--v2-border-medium)",
        borderRadius: 4,
        boxShadow: "0 6px 20px rgba(0,0,0,0.5)",
        zIndex: 30,
      }}
    >
      <span style={{
        fontSize: 10, fontFamily: "var(--v2-font-mono)",
        color: "var(--v2-text-secondary)",
        letterSpacing: "0.06em", textTransform: "uppercase", fontWeight: 600,
        marginRight: 4,
      }}>
        <span style={{ color: "var(--v2-accent-brand)" }}>{count}</span> selected
      </span>
      {children}
      <button
        onClick={onClear}
        title="Clear selection (ESC)"
        style={{
          padding: "4px 8px", marginLeft: 4,
          background: "transparent",
          border: "1px solid var(--v2-border-medium)",
          color: "var(--v2-text-secondary)", borderRadius: 3,
          fontSize: 10, fontFamily: "var(--v2-font-mono)",
          letterSpacing: "0.06em", textTransform: "uppercase", cursor: "pointer",
        }}
      >Clear</button>
    </div>
  );
}

function Btn({
  label, count, tone, onClick,
}: { label: string; count: number; tone: "neutral" | "primary" | "danger"; onClick: () => void }) {
  const disabled = count === 0;
  const colors = tone === "primary"
    ? { fg: "var(--v2-accent-brand)", border: "var(--v2-accent-brand)", bg: "var(--v2-accent-deep)" }
    : tone === "danger"
      ? { fg: "#fca5a5", border: "#7f1d1d", bg: "transparent" }
      : { fg: "var(--v2-text-primary)", border: "var(--v2-border-medium)", bg: "transparent" };
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      style={{
        padding: "5px 10px",
        background: disabled ? "transparent" : colors.bg,
        border: `1px solid ${disabled ? "var(--v2-border-medium)" : colors.border}`,
        color: disabled ? "var(--v2-text-muted)" : colors.fg,
        borderRadius: 3,
        fontSize: 10, fontFamily: "var(--v2-font-mono)",
        letterSpacing: "0.06em", textTransform: "uppercase",
        cursor: disabled ? "not-allowed" : "pointer", fontWeight: 600,
        display: "flex", alignItems: "center", gap: 6,
      }}
    >
      <span>{label}</span>
      <span style={{
        fontSize: 9, fontFamily: "var(--v2-font-mono)",
        padding: "0 5px",
        background: disabled ? "transparent" : "rgba(255,255,255,0.06)",
        borderRadius: 2, fontWeight: 700,
      }}>{count}</span>
    </button>
  );
}
