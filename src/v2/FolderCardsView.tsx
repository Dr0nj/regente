/**
 * FolderCardsView.tsx — F11 layout principal.
 *
 * Substitui as swimlanes horizontais por folders-as-cards lado-a-lado com
 * scroll horizontal. Cada folder é uma coluna com borda pontilhada, header
 * com nome + contagem + mini-dots de status, e uma lista vertical de jobs.
 *
 * Modo monitoring: renderiza instances, suporta rerun inline + context menu.
 * Modo design: renderiza definitions, suporta click para editar + force order.
 *
 * Intencionalmente FORA do ReactFlow — pure CSS/flex. Edges de condition
 * são visualmente expressas pelo texto "← upstream" dentro do job card.
 */
import { useMemo, useState, useCallback, useEffect } from "react";
import type { JobDefinition, JobInstance, InstanceStatus } from "@/lib/orchestrator-model";

export type Mode = "design" | "monitoring";

export interface FolderCardsHandlers {
  // Monitoring
  onInstanceClick?: (inst: JobInstance) => void;
  onRerun?: (id: string) => void;
  onHold?: (id: string) => void;
  onRelease?: (id: string) => void;
  onCancel?: (id: string) => void;
  onSkip?: (id: string) => void;
  onBypass?: (id: string) => void;
  onViewOutput?: (inst: JobInstance) => void;
  onCopyId?: (id: string) => void;
  // Design
  onDefinitionClick?: (def: JobDefinition) => void;
  onForce?: (def: JobDefinition) => void;
  onDuplicate?: (def: JobDefinition) => void;
  onDelete?: (def: JobDefinition) => void;
  // Folder
  onManageFolders?: () => void;
}

interface FolderBucket {
  name: string;
  instances: JobInstance[];
  definitions: JobDefinition[];
}

interface Props {
  mode: Mode;
  instances: JobInstance[];
  definitions: JobDefinition[];
  /** Folders conhecidas no server (inclui vazias). Em local mode, undefined. */
  knownFolders?: string[];
  /** Se definido, só renderiza folders nesta lista (load seletivo). */
  visibleFolders?: Set<string>;
  handlers: FolderCardsHandlers;
  selectedInstanceId?: string | null;
}

const FALLBACK_FOLDER = "—";

function groupIntoFolders(
  mode: Mode,
  instances: JobInstance[],
  definitions: JobDefinition[],
  knownFolders?: string[],
): FolderBucket[] {
  const buckets = new Map<string, FolderBucket>();
  const ensure = (name: string) => {
    if (!buckets.has(name)) buckets.set(name, { name, instances: [], definitions: [] });
    return buckets.get(name)!;
  };
  // Populate known empty folders first (so they render even if no jobs)
  if (knownFolders) for (const f of knownFolders) ensure(f);

  if (mode === "monitoring") {
    for (const i of instances) ensure(i.team?.trim() || FALLBACK_FOLDER).instances.push(i);
  } else {
    for (const d of definitions) ensure(d.team?.trim() || FALLBACK_FOLDER).definitions.push(d);
  }
  return Array.from(buckets.values()).sort((a, b) => {
    if (a.name === FALLBACK_FOLDER) return 1;
    if (b.name === FALLBACK_FOLDER) return -1;
    return a.name.localeCompare(b.name);
  });
}

const STATUS_COLOR: Record<InstanceStatus, string> = {
  OK: "var(--v2-status-ok)",
  RUNNING: "var(--v2-status-running)",
  NOTOK: "var(--v2-status-failed)",
  WAITING: "var(--v2-status-waiting)",
  HOLD: "var(--v2-status-hold)",
  CANCELLED: "var(--v2-text-muted)",
};

function statusDot(status: InstanceStatus) {
  return (
    <span
      style={{
        display: "inline-block",
        width: 8,
        height: 8,
        borderRadius: "50%",
        background: STATUS_COLOR[status],
        flexShrink: 0,
      }}
    />
  );
}

function fmtHm(ms?: number): string {
  if (!ms) return "";
  return new Date(ms).toLocaleTimeString("en-GB", { hour12: false }).slice(0, 5);
}

/* ═══════ Context Menu (F11.5) ═══════ */
interface ContextMenuItem {
  label: string;
  shortcut?: string;
  onClick: () => void;
  danger?: boolean;
  separator?: boolean;
}

interface ContextMenuState {
  x: number;
  y: number;
  items: ContextMenuItem[];
}

function ContextMenu({ state, onClose }: { state: ContextMenuState; onClose: () => void }) {
  useEffect(() => {
    const onEsc = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    const onClick = () => onClose();
    window.addEventListener("keydown", onEsc);
    window.addEventListener("click", onClick);
    return () => {
      window.removeEventListener("keydown", onEsc);
      window.removeEventListener("click", onClick);
    };
  }, [onClose]);

  return (
    <div
      onClick={(e) => e.stopPropagation()}
      style={{
        position: "fixed",
        left: state.x,
        top: state.y,
        minWidth: 200,
        background: "var(--v2-bg-surface)",
        border: "1px solid var(--v2-border-medium)",
        borderRadius: 4,
        boxShadow: "0 8px 24px rgba(0,0,0,0.5)",
        zIndex: 1000,
        padding: "4px 0",
        fontSize: 11,
        fontFamily: "var(--v2-font-sans)",
      }}
    >
      {state.items.map((item, idx) =>
        item.separator ? (
          <div key={idx} style={{ height: 1, background: "var(--v2-border-subtle)", margin: "4px 0" }} />
        ) : (
          <button
            key={idx}
            onClick={() => {
              item.onClick();
              onClose();
            }}
            style={{
              display: "flex",
              width: "100%",
              padding: "6px 12px",
              background: "transparent",
              border: "none",
              color: item.danger ? "var(--v2-status-failed)" : "var(--v2-text-primary)",
              cursor: "pointer",
              alignItems: "center",
              justifyContent: "space-between",
              gap: 12,
              textAlign: "left",
            }}
            onMouseEnter={(e) => (e.currentTarget.style.background = "var(--v2-bg-hover)")}
            onMouseLeave={(e) => (e.currentTarget.style.background = "transparent")}
          >
            <span>{item.label}</span>
            {item.shortcut && (
              <span style={{ fontSize: 9, color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)" }}>
                {item.shortcut}
              </span>
            )}
          </button>
        ),
      )}
    </div>
  );
}

/* ═══════ Main View ═══════ */
export default function FolderCardsView(props: Props) {
  const { mode, instances, definitions, knownFolders, visibleFolders, handlers, selectedInstanceId } = props;
  const [ctxMenu, setCtxMenu] = useState<ContextMenuState | null>(null);

  const folders = useMemo(
    () => groupIntoFolders(mode, instances, definitions, knownFolders),
    [mode, instances, definitions, knownFolders],
  );

  const filteredFolders = useMemo(() => {
    if (!visibleFolders || visibleFolders.size === 0) return folders;
    return folders.filter((f) => visibleFolders.has(f.name));
  }, [folders, visibleFolders]);

  const openInstanceMenu = useCallback(
    (e: React.MouseEvent, inst: JobInstance) => {
      e.preventDefault();
      e.stopPropagation();
      const items: ContextMenuItem[] = [
        { label: "Rerun", shortcut: "R", onClick: () => handlers.onRerun?.(inst.id) },
      ];
      if (inst.status === "WAITING") {
        items.push({ label: "Hold", onClick: () => handlers.onHold?.(inst.id) });
      } else if (inst.status === "HOLD") {
        items.push({ label: "Release", onClick: () => handlers.onRelease?.(inst.id) });
      }
      if (inst.status === "RUNNING" || inst.status === "WAITING") {
        items.push({ label: "Cancel", onClick: () => handlers.onCancel?.(inst.id), danger: true });
      }
      items.push(
        { label: "", separator: true, onClick: () => {} },
        { label: "Skip", onClick: () => handlers.onSkip?.(inst.id) },
        { label: "Bypass", onClick: () => handlers.onBypass?.(inst.id) },
        { label: "", separator: true, onClick: () => {} },
        { label: "View Output", onClick: () => handlers.onViewOutput?.(inst) },
        { label: "Copy Instance ID", onClick: () => handlers.onCopyId?.(inst.id) },
      );
      setCtxMenu({ x: e.clientX, y: e.clientY, items });
    },
    [handlers],
  );

  const openDefinitionMenu = useCallback(
    (e: React.MouseEvent, def: JobDefinition) => {
      e.preventDefault();
      e.stopPropagation();
      const items: ContextMenuItem[] = [
        { label: "Edit", onClick: () => handlers.onDefinitionClick?.(def) },
        { label: "Force Order", onClick: () => handlers.onForce?.(def) },
        { label: "Duplicate", onClick: () => handlers.onDuplicate?.(def) },
        { label: "", separator: true, onClick: () => {} },
        { label: "Delete", onClick: () => handlers.onDelete?.(def), danger: true },
      ];
      setCtxMenu({ x: e.clientX, y: e.clientY, items });
    },
    [handlers],
  );

  if (filteredFolders.length === 0) {
    return (
      <div
        style={{
          flex: 1,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          color: "var(--v2-text-secondary)",
          fontSize: 13,
          padding: 40,
        }}
      >
        <div
          style={{
            padding: "16px 24px",
            background: "var(--v2-bg-surface)",
            border: "1px dashed var(--v2-border-medium)",
            borderRadius: 6,
            textAlign: "center",
            maxWidth: 420,
          }}
        >
          <div style={{ fontWeight: 600, marginBottom: 6, color: "var(--v2-text-primary)" }}>
            Nenhum folder visível
          </div>
          <div style={{ fontSize: 11, lineHeight: 1.5 }}>
            Crie um folder ou ajuste o load seletivo para começar.
          </div>
        </div>
      </div>
    );
  }

  return (
    <div
      style={{
        flex: 1,
        display: "flex",
        flexDirection: "row",
        gap: 16,
        overflowX: "auto",
        overflowY: "hidden",
        padding: 16,
        minHeight: 0,
        background: "var(--v2-bg-canvas)",
      }}
    >
      {filteredFolders.map((f) => (
        <FolderCard
          key={f.name}
          folder={f}
          mode={mode}
          selectedInstanceId={selectedInstanceId}
          handlers={handlers}
          onInstanceContextMenu={openInstanceMenu}
          onDefinitionContextMenu={openDefinitionMenu}
        />
      ))}
      {ctxMenu && <ContextMenu state={ctxMenu} onClose={() => setCtxMenu(null)} />}
    </div>
  );
}

/* ═══════ Folder Card ═══════ */
function FolderCard({
  folder,
  mode,
  selectedInstanceId,
  handlers,
  onInstanceContextMenu,
  onDefinitionContextMenu,
}: {
  folder: FolderBucket;
  mode: Mode;
  selectedInstanceId?: string | null;
  handlers: FolderCardsHandlers;
  onInstanceContextMenu: (e: React.MouseEvent, inst: JobInstance) => void;
  onDefinitionContextMenu: (e: React.MouseEvent, def: JobDefinition) => void;
}) {
  const counts = useMemo(() => {
    const c = { ok: 0, running: 0, failed: 0, waiting: 0, hold: 0 };
    for (const i of folder.instances) {
      if (i.status === "OK") c.ok++;
      else if (i.status === "RUNNING") c.running++;
      else if (i.status === "NOTOK") c.failed++;
      else if (i.status === "WAITING") c.waiting++;
      else if (i.status === "HOLD") c.hold++;
    }
    return c;
  }, [folder.instances]);

  const itemCount = mode === "monitoring" ? folder.instances.length : folder.definitions.length;

  return (
    <section
      style={{
        flex: "0 0 auto",
        minWidth: 280,
        maxWidth: 320,
        display: "flex",
        flexDirection: "column",
        background: "var(--v2-bg-surface)",
        border: "1px dashed var(--v2-border-medium)",
        borderRadius: 6,
        overflow: "hidden",
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
          background: "var(--v2-bg-elevated)",
          flexShrink: 0,
        }}
      >
        <span
          style={{
            fontSize: 12,
            fontWeight: 600,
            color: "var(--v2-text-primary)",
            letterSpacing: "0.02em",
            flex: 1,
            whiteSpace: "nowrap",
            overflow: "hidden",
            textOverflow: "ellipsis",
          }}
          title={folder.name}
        >
          {folder.name}
        </span>
        <span
          style={{
            fontSize: 10,
            fontFamily: "var(--v2-font-mono)",
            color: "var(--v2-text-muted)",
            padding: "1px 6px",
            border: "1px solid var(--v2-border-subtle)",
            borderRadius: 3,
          }}
        >
          {itemCount}
        </span>
        {mode === "monitoring" && (
          <span style={{ display: "flex", gap: 4, fontSize: 9, fontFamily: "var(--v2-font-mono)" }}>
            {counts.running > 0 && (
              <span style={{ color: "var(--v2-status-running)" }}>●{counts.running}</span>
            )}
            {counts.ok > 0 && <span style={{ color: "var(--v2-status-ok)" }}>●{counts.ok}</span>}
            {counts.failed > 0 && (
              <span style={{ color: "var(--v2-status-failed)" }}>●{counts.failed}</span>
            )}
            {counts.waiting > 0 && (
              <span style={{ color: "var(--v2-status-waiting)" }}>●{counts.waiting}</span>
            )}
            {counts.hold > 0 && <span style={{ color: "var(--v2-status-hold)" }}>●{counts.hold}</span>}
          </span>
        )}
      </header>

      {/* Body */}
      <div
        style={{
          flex: 1,
          overflowY: "auto",
          padding: 8,
          display: "flex",
          flexDirection: "column",
          gap: 6,
        }}
      >
        {mode === "monitoring" ? (
          folder.instances.length === 0 ? (
            <EmptyFolderHint text="Sem jobs hoje" />
          ) : (
            folder.instances.map((inst) => (
              <InstanceCard
                key={inst.id}
                inst={inst}
                selected={inst.id === selectedInstanceId}
                handlers={handlers}
                onContextMenu={(e) => onInstanceContextMenu(e, inst)}
              />
            ))
          )
        ) : folder.definitions.length === 0 ? (
          <EmptyFolderHint text="Sem definitions" />
        ) : (
          folder.definitions.map((def) => (
            <DefinitionCard
              key={def.id}
              def={def}
              handlers={handlers}
              onContextMenu={(e) => onDefinitionContextMenu(e, def)}
            />
          ))
        )}
      </div>
    </section>
  );
}

function EmptyFolderHint({ text }: { text: string }) {
  return (
    <div
      style={{
        padding: 16,
        textAlign: "center",
        fontSize: 10,
        color: "var(--v2-text-muted)",
        fontFamily: "var(--v2-font-mono)",
        border: "1px dashed var(--v2-border-subtle)",
        borderRadius: 4,
      }}
    >
      {text}
    </div>
  );
}

/* ═══════ Instance Card (Monitoring) ═══════ */
function InstanceCard({
  inst,
  selected,
  handlers,
  onContextMenu,
}: {
  inst: JobInstance;
  selected: boolean;
  handlers: FolderCardsHandlers;
  onContextMenu: (e: React.MouseEvent) => void;
}) {
  const [hover, setHover] = useState(false);

  return (
    <div
      onClick={() => handlers.onInstanceClick?.(inst)}
      onContextMenu={onContextMenu}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        padding: "7px 9px",
        background: selected ? "var(--v2-accent-deep)" : hover ? "var(--v2-bg-hover)" : "var(--v2-bg-elevated)",
        border: `1px solid ${selected ? "var(--v2-accent-dark)" : "var(--v2-border-subtle)"}`,
        borderLeft: `2px solid ${STATUS_COLOR[inst.status]}`,
        borderRadius: 3,
        cursor: "pointer",
        display: "flex",
        flexDirection: "column",
        gap: 3,
        position: "relative",
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
        {statusDot(inst.status)}
        <span
          style={{
            fontSize: 11,
            fontWeight: 500,
            color: "var(--v2-text-primary)",
            flex: 1,
            whiteSpace: "nowrap",
            overflow: "hidden",
            textOverflow: "ellipsis",
          }}
          title={inst.label}
        >
          {inst.label}
        </span>
        {hover && (
          <button
            onClick={(e) => {
              e.stopPropagation();
              handlers.onRerun?.(inst.id);
            }}
            title="Rerun"
            style={{
              background: "transparent",
              border: "1px solid var(--v2-border-medium)",
              color: "var(--v2-text-secondary)",
              borderRadius: 3,
              padding: "1px 6px",
              fontSize: 10,
              cursor: "pointer",
              fontFamily: "var(--v2-font-mono)",
            }}
            onMouseEnter={(e) => {
              e.currentTarget.style.color = "var(--v2-accent-brand)";
              e.currentTarget.style.borderColor = "var(--v2-accent-dark)";
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.color = "var(--v2-text-secondary)";
              e.currentTarget.style.borderColor = "var(--v2-border-medium)";
            }}
          >
            ⟳
          </button>
        )}
      </div>
      <div
        style={{
          display: "flex",
          gap: 8,
          fontSize: 9,
          fontFamily: "var(--v2-font-mono)",
          color: "var(--v2-text-muted)",
          letterSpacing: "0.04em",
        }}
      >
        <span>{inst.jobType}</span>
        {inst.startedAt && <span>· {fmtHm(inst.startedAt)}</span>}
        {inst.manual && <span style={{ color: "var(--v2-status-hold)" }}>· forced</span>}
      </div>
    </div>
  );
}

/* ═══════ Definition Card (Design) ═══════ */
function DefinitionCard({
  def,
  handlers,
  onContextMenu,
}: {
  def: JobDefinition;
  handlers: FolderCardsHandlers;
  onContextMenu: (e: React.MouseEvent) => void;
}) {
  const [hover, setHover] = useState(false);
  const enabled = def.schedule.enabled;

  return (
    <div
      onClick={() => handlers.onDefinitionClick?.(def)}
      onContextMenu={onContextMenu}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        padding: "7px 9px",
        background: hover ? "var(--v2-bg-hover)" : "var(--v2-bg-elevated)",
        border: "1px solid var(--v2-border-subtle)",
        borderLeft: `2px solid ${enabled ? "var(--v2-accent-dark)" : "var(--v2-border-medium)"}`,
        borderRadius: 3,
        cursor: "pointer",
        display: "flex",
        flexDirection: "column",
        gap: 3,
      }}
    >
      <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
        <span
          style={{
            fontSize: 11,
            fontWeight: 500,
            color: enabled ? "var(--v2-text-primary)" : "var(--v2-text-secondary)",
            flex: 1,
            whiteSpace: "nowrap",
            overflow: "hidden",
            textOverflow: "ellipsis",
          }}
          title={def.label}
        >
          {def.label}
        </span>
        {hover && (
          <button
            onClick={(e) => {
              e.stopPropagation();
              handlers.onForce?.(def);
            }}
            title="Force Order (Run Now)"
            style={{
              background: "transparent",
              border: "1px solid var(--v2-border-medium)",
              color: "var(--v2-text-secondary)",
              borderRadius: 3,
              padding: "1px 6px",
              fontSize: 10,
              cursor: "pointer",
              fontFamily: "var(--v2-font-mono)",
            }}
            onMouseEnter={(e) => {
              e.currentTarget.style.color = "var(--v2-accent-brand)";
              e.currentTarget.style.borderColor = "var(--v2-accent-dark)";
            }}
            onMouseLeave={(e) => {
              e.currentTarget.style.color = "var(--v2-text-secondary)";
              e.currentTarget.style.borderColor = "var(--v2-border-medium)";
            }}
          >
            ⚡
          </button>
        )}
      </div>
      <div
        style={{
          display: "flex",
          gap: 8,
          fontSize: 9,
          fontFamily: "var(--v2-font-mono)",
          color: "var(--v2-text-muted)",
          letterSpacing: "0.04em",
        }}
      >
        <span>{def.jobType}</span>
        {def.schedule.cronExpression && <span>· {def.schedule.cronExpression}</span>}
        {def.upstream && def.upstream.length > 0 && (
          <span title={def.upstream.map((u) => u.from).join(", ")}>
            · ←{def.upstream.length}
          </span>
        )}
      </div>
    </div>
  );
}
