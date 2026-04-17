/**
 * FolderManagerDialog.tsx — F11.6 folder lifecycle UI.
 *
 * Modal que permite:
 *  - Criar folder vazio
 *  - Renomear folder (e atualizar team de todas definitions dentro)
 *  - Archive / Delete (com confirmação dupla quando tiver jobs)
 *  - Load seletivo: checkbox list para escolher quais folders renderizar
 */
import { useEffect, useState } from "react";
import type { FolderInfo } from "@/lib/folder-api";
import { createFolder, renameFolder, deleteFolder, archiveFolder } from "@/lib/folder-api";

interface Props {
  folders: FolderInfo[];
  visibleFolders: Set<string>;
  onVisibleChange: (next: Set<string>) => void;
  onFoldersChanged: () => void;
  onClose: () => void;
}

type Dialog = null | { kind: "rename"; name: string } | { kind: "delete"; name: string; jobCount: number };

export default function FolderManagerDialog({
  folders,
  visibleFolders,
  onVisibleChange,
  onFoldersChanged,
  onClose,
}: Props) {
  const [newName, setNewName] = useState("");
  const [creating, setCreating] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [inner, setInner] = useState<Dialog>(null);

  useEffect(() => {
    const onEsc = (e: KeyboardEvent) => e.key === "Escape" && !inner && onClose();
    window.addEventListener("keydown", onEsc);
    return () => window.removeEventListener("keydown", onEsc);
  }, [onClose, inner]);

  const toggleVisible = (name: string) => {
    const next = new Set(visibleFolders);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    onVisibleChange(next);
  };

  const selectAll = () => onVisibleChange(new Set(folders.map((f) => f.name)));
  const selectNone = () => onVisibleChange(new Set());

  const handleCreate = async () => {
    const name = newName.trim();
    if (!name) return;
    setCreating(true);
    setErr(null);
    try {
      await createFolder(name);
      setNewName("");
      onFoldersChanged();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    } finally {
      setCreating(false);
    }
  };

  const handleRename = async (oldName: string, newName: string) => {
    setErr(null);
    try {
      await renameFolder(oldName, newName);
      setInner(null);
      onFoldersChanged();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  };

  const handleDelete = async (name: string, force: boolean) => {
    setErr(null);
    try {
      await deleteFolder(name, force);
      setInner(null);
      onFoldersChanged();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  };

  const handleArchive = async (name: string) => {
    setErr(null);
    try {
      await archiveFolder(name);
      onFoldersChanged();
    } catch (e) {
      setErr(e instanceof Error ? e.message : String(e));
    }
  };

  return (
    <div
      onClick={() => !inner && onClose()}
      style={{
        position: "fixed",
        inset: 0,
        background: "rgba(0,0,0,0.6)",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        zIndex: 850,
      }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          width: "min(520px, 92vw)",
          maxHeight: "80vh",
          background: "var(--v2-bg-surface)",
          border: "1px solid var(--v2-border-medium)",
          borderRadius: 6,
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
        }}
      >
        <header
          style={{
            padding: "10px 14px",
            borderBottom: "1px solid var(--v2-border-subtle)",
            display: "flex",
            alignItems: "center",
            background: "var(--v2-bg-elevated)",
          }}
        >
          <span style={{ fontSize: 12, fontWeight: 600, color: "var(--v2-text-primary)" }}>
            Folders
          </span>
          <span
            style={{
              marginLeft: 8,
              fontSize: 9,
              fontFamily: "var(--v2-font-mono)",
              color: "var(--v2-text-muted)",
              letterSpacing: "0.06em",
              textTransform: "uppercase",
            }}
          >
            {folders.length} · {visibleFolders.size || "all"} visible
          </span>
          <span style={{ flex: 1 }} />
          <button onClick={onClose} style={btn()}>
            Close
          </button>
        </header>

        {/* Create */}
        <div
          style={{
            padding: 12,
            borderBottom: "1px solid var(--v2-border-subtle)",
            display: "flex",
            gap: 8,
          }}
        >
          <input
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && handleCreate()}
            placeholder="new folder name"
            style={{
              flex: 1,
              padding: "5px 8px",
              background: "var(--v2-bg-elevated)",
              border: "1px solid var(--v2-border-medium)",
              borderRadius: 3,
              color: "var(--v2-text-primary)",
              fontSize: 11,
              fontFamily: "var(--v2-font-sans)",
              outline: "none",
            }}
          />
          <button onClick={handleCreate} disabled={creating || !newName.trim()} style={btn(true)}>
            Create
          </button>
        </div>

        {err && (
          <div
            style={{
              padding: "6px 12px",
              fontSize: 10,
              color: "var(--v2-status-failed)",
              background: "rgba(239,68,68,0.08)",
              borderBottom: "1px solid var(--v2-border-subtle)",
              fontFamily: "var(--v2-font-mono)",
            }}
          >
            {err}
          </div>
        )}

        {/* Toolbar */}
        <div
          style={{
            padding: "6px 12px",
            display: "flex",
            gap: 8,
            borderBottom: "1px solid var(--v2-border-subtle)",
            fontSize: 10,
            fontFamily: "var(--v2-font-mono)",
            color: "var(--v2-text-secondary)",
            letterSpacing: "0.06em",
            textTransform: "uppercase",
          }}
        >
          <button onClick={selectAll} style={linkBtn()}>
            All
          </button>
          <button onClick={selectNone} style={linkBtn()}>
            None
          </button>
          <span style={{ flex: 1 }} />
          <span>Visible = load selective</span>
        </div>

        {/* List */}
        <div style={{ flex: 1, overflow: "auto", padding: 4 }}>
          {folders.length === 0 && (
            <div
              style={{
                padding: 24,
                textAlign: "center",
                color: "var(--v2-text-muted)",
                fontSize: 11,
              }}
            >
              Nenhum folder. Crie o primeiro acima.
            </div>
          )}
          {folders.map((f) => {
            const visible = visibleFolders.size === 0 || visibleFolders.has(f.name);
            return (
              <div
                key={f.name}
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 8,
                  padding: "6px 10px",
                  borderBottom: "1px solid var(--v2-border-subtle)",
                }}
              >
                <input
                  type="checkbox"
                  checked={visible}
                  onChange={() => toggleVisible(f.name)}
                  style={{ accentColor: "#11C76F" }}
                />
                <span style={{ flex: 1, fontSize: 12, color: "var(--v2-text-primary)" }}>
                  {f.name}
                  {f.archived && (
                    <span
                      style={{
                        marginLeft: 6,
                        fontSize: 9,
                        color: "var(--v2-status-hold)",
                        fontFamily: "var(--v2-font-mono)",
                      }}
                    >
                      archived
                    </span>
                  )}
                </span>
                <span
                  style={{
                    fontSize: 10,
                    fontFamily: "var(--v2-font-mono)",
                    color: "var(--v2-text-muted)",
                    padding: "1px 5px",
                    border: "1px solid var(--v2-border-subtle)",
                    borderRadius: 2,
                  }}
                >
                  {f.jobCount}
                </span>
                <button
                  onClick={() => setInner({ kind: "rename", name: f.name })}
                  style={linkBtn()}
                  title="Rename"
                >
                  Rename
                </button>
                <button onClick={() => handleArchive(f.name)} style={linkBtn()} title="Archive">
                  Archive
                </button>
                <button
                  onClick={() => setInner({ kind: "delete", name: f.name, jobCount: f.jobCount })}
                  style={linkBtn(true)}
                  title="Delete"
                >
                  Delete
                </button>
              </div>
            );
          })}
        </div>
      </div>

      {/* Inner confirm dialogs */}
      {inner?.kind === "rename" && (
        <RenamePrompt
          currentName={inner.name}
          onCancel={() => setInner(null)}
          onConfirm={(nn) => handleRename(inner.name, nn)}
        />
      )}
      {inner?.kind === "delete" && (
        <DeleteConfirm
          name={inner.name}
          jobCount={inner.jobCount}
          onCancel={() => setInner(null)}
          onConfirm={(force) => handleDelete(inner.name, force)}
        />
      )}
    </div>
  );
}

function RenamePrompt({
  currentName,
  onCancel,
  onConfirm,
}: {
  currentName: string;
  onCancel: () => void;
  onConfirm: (newName: string) => void;
}) {
  const [value, setValue] = useState(currentName);
  return (
    <div
      onClick={onCancel}
      style={{ position: "fixed", inset: 0, background: "rgba(0,0,0,0.5)", display: "flex", alignItems: "center", justifyContent: "center", zIndex: 900 }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{ width: 360, background: "var(--v2-bg-surface)", border: "1px solid var(--v2-border-medium)", borderRadius: 6, padding: 16 }}
      >
        <div style={{ fontSize: 12, fontWeight: 600, marginBottom: 10, color: "var(--v2-text-primary)" }}>
          Rename folder
        </div>
        <input
          autoFocus
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && value.trim() && onConfirm(value.trim())}
          style={{
            width: "100%",
            padding: "6px 8px",
            background: "var(--v2-bg-elevated)",
            border: "1px solid var(--v2-border-medium)",
            color: "var(--v2-text-primary)",
            borderRadius: 3,
            fontSize: 12,
            outline: "none",
          }}
        />
        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end", marginTop: 12 }}>
          <button onClick={onCancel} style={btn()}>
            Cancel
          </button>
          <button
            onClick={() => value.trim() && onConfirm(value.trim())}
            disabled={!value.trim() || value === currentName}
            style={btn(true)}
          >
            Rename
          </button>
        </div>
      </div>
    </div>
  );
}

function DeleteConfirm({
  name,
  jobCount,
  onCancel,
  onConfirm,
}: {
  name: string;
  jobCount: number;
  onCancel: () => void;
  onConfirm: (force: boolean) => void;
}) {
  const [typed, setTyped] = useState("");
  const hasJobs = jobCount > 0;
  const ok = hasJobs ? typed === name : true;
  return (
    <div
      onClick={onCancel}
      style={{ position: "fixed", inset: 0, background: "rgba(0,0,0,0.5)", display: "flex", alignItems: "center", justifyContent: "center", zIndex: 900 }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{ width: 420, background: "var(--v2-bg-surface)", border: "1px solid var(--v2-border-medium)", borderRadius: 6, padding: 16 }}
      >
        <div style={{ fontSize: 12, fontWeight: 600, marginBottom: 6, color: "var(--v2-status-failed)" }}>
          Delete folder
        </div>
        <div style={{ fontSize: 11, color: "var(--v2-text-secondary)", lineHeight: 1.5, marginBottom: 10 }}>
          {hasJobs ? (
            <>
              O folder <strong>{name}</strong> contém <strong>{jobCount}</strong> job(s). Esta ação
              remove TUDO. Digite <code style={{ color: "var(--v2-text-primary)" }}>{name}</code> para
              confirmar.
            </>
          ) : (
            <>Remover folder vazio <strong>{name}</strong>?</>
          )}
        </div>
        {hasJobs && (
          <input
            autoFocus
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            placeholder={name}
            style={{
              width: "100%",
              padding: "6px 8px",
              background: "var(--v2-bg-elevated)",
              border: "1px solid var(--v2-border-medium)",
              color: "var(--v2-text-primary)",
              borderRadius: 3,
              fontSize: 12,
              outline: "none",
              marginBottom: 10,
              fontFamily: "var(--v2-font-mono)",
            }}
          />
        )}
        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
          <button onClick={onCancel} style={btn()}>
            Cancel
          </button>
          <button onClick={() => onConfirm(hasJobs)} disabled={!ok} style={btn(false, true)}>
            Delete
          </button>
        </div>
      </div>
    </div>
  );
}

function btn(primary = false, danger = false): React.CSSProperties {
  return {
    padding: "5px 12px",
    background: primary ? "var(--v2-accent-deep)" : "transparent",
    border: `1px solid ${danger ? "var(--v2-status-failed)" : primary ? "var(--v2-accent-dark)" : "var(--v2-border-medium)"}`,
    color: danger ? "var(--v2-status-failed)" : primary ? "var(--v2-accent-brand)" : "var(--v2-text-primary)",
    borderRadius: 3,
    fontSize: 10,
    fontFamily: "var(--v2-font-mono)",
    letterSpacing: "0.06em",
    textTransform: "uppercase",
    cursor: "pointer",
    fontWeight: 600,
  };
}

function linkBtn(danger = false): React.CSSProperties {
  return {
    padding: "2px 6px",
    background: "transparent",
    border: "none",
    color: danger ? "var(--v2-status-failed)" : "var(--v2-text-secondary)",
    fontSize: 10,
    fontFamily: "var(--v2-font-mono)",
    letterSpacing: "0.06em",
    textTransform: "uppercase",
    cursor: "pointer",
  };
}
