/**
 * FolderManagerDialog — modal Control-M Planning style com:
 *  - grid de folder cards (name, jobCount, archived badge)
 *  - filtro de visibilidade (checkbox por card; null = tudo visível)
 *  - actions por card: rename, archive, delete (double-confirm se jobCount>0)
 *  - "+ New Folder" inline
 *
 * Chamado pelo V2Preview via topbar button "Folders".
 * Em local mode mostra estado vazio + hint para configurar server.
 */
import { useEffect, useState, useCallback } from "react";
import {
  type FolderInfo,
  listFolders,
  createFolder,
  renameFolder,
  deleteFolder,
  archiveFolder,
} from "@/lib/folder-api";
import { isServerMode } from "@/lib/server-client";

interface Props {
  visibleFolders: Set<string> | null;
  onChangeVisible: (next: Set<string> | null) => void;
  onClose: () => void;
}

export default function FolderManagerDialog({ visibleFolders, onChangeVisible, onClose }: Props) {
  const [folders, setFolders] = useState<FolderInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [newName, setNewName] = useState("");
  const [renaming, setRenaming] = useState<{ from: string; to: string } | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<{ name: string; typed: string } | null>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    setErr(null);
    try {
      const list = await listFolders();
      setFolders(list);
    } catch (e: unknown) {
      setErr((e as Error).message ?? "failed to load folders");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void refresh(); }, [refresh]);

  // ESC closes
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === "Escape") onClose(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const handleCreate = useCallback(async () => {
    const name = newName.trim();
    if (!name) return;
    try {
      await createFolder(name);
      setNewName("");
      await refresh();
    } catch (e: unknown) {
      alert(`Create failed: ${(e as Error).message}`);
    }
  }, [newName, refresh]);

  const handleRename = useCallback(async () => {
    if (!renaming) return;
    const to = renaming.to.trim();
    if (!to || to === renaming.from) { setRenaming(null); return; }
    try {
      await renameFolder(renaming.from, to);
      // migra seleção se essa folder estava visível
      if (visibleFolders?.has(renaming.from)) {
        const next = new Set(visibleFolders);
        next.delete(renaming.from);
        next.add(to);
        onChangeVisible(next);
      }
      setRenaming(null);
      await refresh();
    } catch (e: unknown) {
      alert(`Rename failed: ${(e as Error).message}`);
    }
  }, [renaming, visibleFolders, onChangeVisible, refresh]);

  const handleArchive = useCallback(async (name: string) => {
    try {
      await archiveFolder(name);
      await refresh();
    } catch (e: unknown) {
      alert(`Archive failed: ${(e as Error).message}`);
    }
  }, [refresh]);

  const handleDelete = useCallback(async () => {
    if (!confirmDelete) return;
    const f = folders.find((x) => x.name === confirmDelete.name);
    if (!f) return;
    if (f.jobCount > 0 && confirmDelete.typed !== confirmDelete.name) {
      alert(`Type "${confirmDelete.name}" to confirm delete (${f.jobCount} jobs).`);
      return;
    }
    try {
      await deleteFolder(confirmDelete.name, f.jobCount > 0);
      if (visibleFolders?.has(confirmDelete.name)) {
        const next = new Set(visibleFolders);
        next.delete(confirmDelete.name);
        onChangeVisible(next);
      }
      setConfirmDelete(null);
      await refresh();
    } catch (e: unknown) {
      alert(`Delete failed: ${(e as Error).message}`);
    }
  }, [confirmDelete, folders, visibleFolders, onChangeVisible, refresh]);

  const toggleVisible = useCallback((name: string) => {
    const allNames = folders.map((f) => f.name);
    // null = "all visible"; first toggle materializes the set
    const current = visibleFolders ?? new Set(allNames);
    const next = new Set(current);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    // se o usuário voltou a ter todas marcadas, normaliza para null
    if (next.size === allNames.length && allNames.every((n) => next.has(n))) {
      onChangeVisible(null);
    } else {
      onChangeVisible(next);
    }
  }, [folders, visibleFolders, onChangeVisible]);

  const allVisible = visibleFolders === null;
  const selectAll = useCallback(() => onChangeVisible(null), [onChangeVisible]);
  const selectNone = useCallback(() => onChangeVisible(new Set()), [onChangeVisible]);

  return (
    <div
      onClick={onClose}
      style={{
        position: "fixed", inset: 0, background: "rgba(0,0,0,0.7)",
        display: "flex", alignItems: "center", justifyContent: "center",
        zIndex: 100,
      }}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="v2-grain v2-edge-highlight"
        style={{
          width: "min(820px, 92vw)",
          maxHeight: "84vh",
          background: "var(--v2-bg-surface)",
          border: "1px solid var(--v2-border-medium)",
          borderRadius: 6,
          display: "flex",
          flexDirection: "column",
          position: "relative",
          isolation: "isolate",
        }}
      >
        {/* Header */}
        <div style={{
          padding: "14px 18px",
          borderBottom: "1px solid var(--v2-border-subtle)",
          display: "flex", alignItems: "center", gap: 12,
        }}>
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 13, fontWeight: 600, color: "var(--v2-text-primary)" }}>Folders</div>
            <div style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 2, fontFamily: "var(--v2-font-mono)", letterSpacing: "0.06em", textTransform: "uppercase" }}>
              {folders.length} total · {allVisible ? "all visible" : `${visibleFolders?.size ?? 0} visible`}
            </div>
          </div>
          <button
            onClick={selectAll}
            style={{
              padding: "4px 10px", background: "transparent",
              border: "1px solid var(--v2-border-medium)",
              color: "var(--v2-text-secondary)", borderRadius: 3,
              fontSize: 10, fontFamily: "var(--v2-font-mono)",
              letterSpacing: "0.06em", textTransform: "uppercase", cursor: "pointer",
            }}
          >Select all</button>
          <button
            onClick={selectNone}
            style={{
              padding: "4px 10px", background: "transparent",
              border: "1px solid var(--v2-border-medium)",
              color: "var(--v2-text-secondary)", borderRadius: 3,
              fontSize: 10, fontFamily: "var(--v2-font-mono)",
              letterSpacing: "0.06em", textTransform: "uppercase", cursor: "pointer",
            }}
          >Clear</button>
          <button
            onClick={onClose}
            style={{
              padding: "4px 10px", background: "transparent",
              border: "none", color: "var(--v2-text-secondary)",
              fontSize: 16, cursor: "pointer", lineHeight: 1,
            }}
            aria-label="Close"
          >×</button>
        </div>

        {/* Create row */}
        {isServerMode() && (
          <div style={{
            padding: "10px 18px",
            borderBottom: "1px solid var(--v2-border-subtle)",
            display: "flex", gap: 8, alignItems: "center",
          }}>
            <input
              type="text"
              placeholder="new folder name (a-z 0-9 - _)"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              onKeyDown={(e) => { if (e.key === "Enter") void handleCreate(); }}
              style={{
                flex: 1, padding: "6px 10px",
                background: "var(--v2-bg-elevated)",
                border: "1px solid var(--v2-border-medium)",
                color: "var(--v2-text-primary)", borderRadius: 3,
                fontSize: 11, fontFamily: "var(--v2-font-mono)",
              }}
            />
            <button
              onClick={() => void handleCreate()}
              disabled={!newName.trim()}
              style={{
                padding: "6px 14px",
                background: newName.trim() ? "var(--v2-accent-deep)" : "transparent",
                border: "1px solid var(--v2-accent-brand)",
                color: newName.trim() ? "var(--v2-accent-brand)" : "var(--v2-text-muted)",
                borderColor: newName.trim() ? "var(--v2-accent-brand)" : "var(--v2-border-medium)",
                borderRadius: 3, fontSize: 10,
                fontFamily: "var(--v2-font-mono)",
                letterSpacing: "0.06em", textTransform: "uppercase",
                cursor: newName.trim() ? "pointer" : "not-allowed", fontWeight: 600,
              }}
            >+ New Folder</button>
          </div>
        )}

        {/* Body */}
        <div style={{ flex: 1, overflow: "auto", padding: 16, position: "relative", zIndex: 2 }}>
          {!isServerMode() && (
            <div style={{ padding: 24, textAlign: "center", color: "var(--v2-text-muted)", fontSize: 12 }}>
              Folder management requires server mode.<br />
              Set <code style={{ color: "var(--v2-accent-brand)" }}>VITE_REGENTE_SERVER_URL</code> and reload.
            </div>
          )}
          {isServerMode() && loading && (
            <div style={{ padding: 24, textAlign: "center", color: "var(--v2-text-muted)", fontSize: 12 }}>Loading…</div>
          )}
          {isServerMode() && err && (
            <div style={{ padding: 12, background: "#450a0a", border: "1px solid #7f1d1d", color: "#fca5a5", borderRadius: 3, fontSize: 11 }}>{err}</div>
          )}
          {isServerMode() && !loading && !err && folders.length === 0 && (
            <div style={{ padding: 24, textAlign: "center", color: "var(--v2-text-muted)", fontSize: 12 }}>
              No folders yet. Create one above.
            </div>
          )}

          <div style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fill, minmax(220px, 1fr))",
            gap: 10,
          }}>
            {folders.map((f) => {
              const visible = allVisible || (visibleFolders?.has(f.name) ?? false);
              const isRenaming = renaming?.from === f.name;
              return (
                <div
                  key={f.name}
                  className="v2-grain-card"
                  style={{
                    padding: 12,
                    background: "var(--v2-bg-elevated)",
                    border: `1px solid ${visible ? "var(--v2-accent-deep)" : "var(--v2-border-medium)"}`,
                    borderRadius: 4,
                    display: "flex", flexDirection: "column", gap: 8,
                    opacity: f.archived ? 0.55 : 1,
                    position: "relative",
                    isolation: "isolate",
                  }}
                >
                  <div style={{ display: "flex", alignItems: "center", gap: 8, position: "relative", zIndex: 2 }}>
                    <input
                      type="checkbox"
                      checked={visible}
                      onChange={() => toggleVisible(f.name)}
                      style={{ accentColor: "var(--v2-accent-brand)" }}
                    />
                    {isRenaming ? (
                      <input
                        type="text"
                        autoFocus
                        value={renaming.to}
                        onChange={(e) => setRenaming({ from: renaming.from, to: e.target.value })}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") void handleRename();
                          else if (e.key === "Escape") setRenaming(null);
                        }}
                        onBlur={() => void handleRename()}
                        style={{
                          flex: 1, padding: "3px 6px",
                          background: "var(--v2-bg-surface)",
                          border: "1px solid var(--v2-accent-brand)",
                          color: "var(--v2-text-primary)", borderRadius: 2,
                          fontSize: 12, fontFamily: "var(--v2-font-mono)",
                        }}
                      />
                    ) : (
                      <span style={{
                        flex: 1, fontSize: 12, fontWeight: 600,
                        color: "var(--v2-text-primary)",
                        fontFamily: "var(--v2-font-mono)",
                        whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis",
                      }}>{f.name}</span>
                    )}
                    {f.archived && (
                      <span style={{
                        fontSize: 8, fontFamily: "var(--v2-font-mono)",
                        color: "var(--v2-text-muted)", padding: "1px 4px",
                        border: "1px solid var(--v2-border-medium)", borderRadius: 2,
                        letterSpacing: "0.08em", textTransform: "uppercase",
                      }}>archived</span>
                    )}
                  </div>

                  <div style={{
                    fontSize: 10, fontFamily: "var(--v2-font-mono)",
                    color: "var(--v2-text-muted)", letterSpacing: "0.04em",
                    position: "relative", zIndex: 2,
                  }}>
                    {f.jobCount} {f.jobCount === 1 ? "job" : "jobs"}
                  </div>

                  <div style={{ display: "flex", gap: 6, position: "relative", zIndex: 2 }}>
                    <button
                      onClick={() => setRenaming({ from: f.name, to: f.name })}
                      disabled={isRenaming}
                      style={{
                        flex: 1, padding: "4px 6px",
                        background: "transparent",
                        border: "1px solid var(--v2-border-medium)",
                        color: "var(--v2-text-secondary)", borderRadius: 2,
                        fontSize: 9, fontFamily: "var(--v2-font-mono)",
                        letterSpacing: "0.06em", textTransform: "uppercase",
                        cursor: isRenaming ? "default" : "pointer",
                      }}
                    >Rename</button>
                    {!f.archived && (
                      <button
                        onClick={() => void handleArchive(f.name)}
                        style={{
                          flex: 1, padding: "4px 6px",
                          background: "transparent",
                          border: "1px solid var(--v2-border-medium)",
                          color: "var(--v2-text-secondary)", borderRadius: 2,
                          fontSize: 9, fontFamily: "var(--v2-font-mono)",
                          letterSpacing: "0.06em", textTransform: "uppercase",
                          cursor: "pointer",
                        }}
                      >Archive</button>
                    )}
                    <button
                      onClick={() => setConfirmDelete({ name: f.name, typed: "" })}
                      style={{
                        flex: 1, padding: "4px 6px",
                        background: "transparent",
                        border: "1px solid #7f1d1d",
                        color: "#fca5a5", borderRadius: 2,
                        fontSize: 9, fontFamily: "var(--v2-font-mono)",
                        letterSpacing: "0.06em", textTransform: "uppercase",
                        cursor: "pointer",
                      }}
                    >Delete</button>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      </div>

      {/* Confirm delete dialog */}
      {confirmDelete && (
        <div
          onClick={(e) => { e.stopPropagation(); setConfirmDelete(null); }}
          style={{
            position: "fixed", inset: 0, background: "rgba(0,0,0,0.85)",
            display: "flex", alignItems: "center", justifyContent: "center", zIndex: 110,
          }}
        >
          <div
            onClick={(e) => e.stopPropagation()}
            style={{
              width: "min(420px, 90vw)",
              padding: 18,
              background: "var(--v2-bg-surface)",
              border: "1px solid #7f1d1d",
              borderRadius: 4,
            }}
          >
            <div style={{ fontSize: 13, fontWeight: 600, color: "#fca5a5", marginBottom: 8 }}>
              Delete folder "{confirmDelete.name}"?
            </div>
            {(folders.find((x) => x.name === confirmDelete.name)?.jobCount ?? 0) > 0 ? (
              <>
                <div style={{ fontSize: 11, color: "var(--v2-text-secondary)", marginBottom: 10, lineHeight: 1.5 }}>
                  This folder contains{" "}
                  <strong style={{ color: "var(--v2-text-primary)" }}>
                    {folders.find((x) => x.name === confirmDelete.name)?.jobCount} jobs
                  </strong>
                  . Type the folder name to confirm.
                </div>
                <input
                  type="text"
                  autoFocus
                  value={confirmDelete.typed}
                  onChange={(e) => setConfirmDelete({ ...confirmDelete, typed: e.target.value })}
                  onKeyDown={(e) => { if (e.key === "Enter") void handleDelete(); }}
                  placeholder={confirmDelete.name}
                  style={{
                    width: "100%", padding: "6px 10px", marginBottom: 10,
                    background: "var(--v2-bg-elevated)",
                    border: "1px solid var(--v2-border-medium)",
                    color: "var(--v2-text-primary)", borderRadius: 3,
                    fontSize: 11, fontFamily: "var(--v2-font-mono)",
                  }}
                />
              </>
            ) : (
              <div style={{ fontSize: 11, color: "var(--v2-text-secondary)", marginBottom: 10 }}>
                The folder is empty. This action cannot be undone.
              </div>
            )}
            <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
              <button
                onClick={() => setConfirmDelete(null)}
                style={{
                  padding: "6px 14px", background: "transparent",
                  border: "1px solid var(--v2-border-medium)",
                  color: "var(--v2-text-secondary)", borderRadius: 3,
                  fontSize: 10, fontFamily: "var(--v2-font-mono)",
                  letterSpacing: "0.06em", textTransform: "uppercase", cursor: "pointer",
                }}
              >Cancel</button>
              <button
                onClick={() => void handleDelete()}
                style={{
                  padding: "6px 14px", background: "#7f1d1d",
                  border: "1px solid #991b1b",
                  color: "#fee2e2", borderRadius: 3,
                  fontSize: 10, fontFamily: "var(--v2-font-mono)",
                  letterSpacing: "0.06em", textTransform: "uppercase",
                  cursor: "pointer", fontWeight: 600,
                }}
              >Delete</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
