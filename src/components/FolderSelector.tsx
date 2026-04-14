import { useState, useEffect } from "react";
import { FolderOpen, Plus, Trash2, ChevronDown, Loader2 } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { cn } from "@/lib/utils";
import { listTeamFolders, deleteTeamFolder, saveTeamWorkflow, type TeamFolder } from "@/lib/team-workflows";

interface FolderSelectorProps {
  activeFolderId: string | null;
  onSelect: (folderId: string) => void;
  onCreated?: (folderId: string) => void;
}

export default function FolderSelector({ activeFolderId, onSelect, onCreated }: FolderSelectorProps) {
  const [folders, setFolders] = useState<TeamFolder[]>([]);
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState("");

  // Load folders
  useEffect(() => {
    listTeamFolders().then((f) => { setFolders(f); setLoading(false); });
  }, [activeFolderId]);

  const activeFolder = folders.find((f) => f.id === activeFolderId);

  const handleCreate = async () => {
    const name = newName.trim().toUpperCase().replace(/\s+/g, "_");
    if (!name) return;
    const id = name.toLowerCase();
    await saveTeamWorkflow(id, name, [], [], "");
    setNewName("");
    setCreating(false);
    const updated = await listTeamFolders();
    setFolders(updated);
    onCreated?.(id);
    onSelect(id);
    setOpen(false);
  };

  const handleDelete = async (e: React.MouseEvent, folderId: string) => {
    e.stopPropagation();
    await deleteTeamFolder(folderId);
    const updated = await listTeamFolders();
    setFolders(updated);
    if (activeFolderId === folderId) {
      onSelect(updated[0]?.id ?? "");
    }
  };

  return (
    <div className="relative">
      {/* Trigger button */}
      <button
        onClick={() => setOpen(!open)}
        className={cn(
          "flex items-center gap-2 rounded-lg border px-3 py-1.5 transition-all",
          "border-white/[0.08] bg-white/[0.03] hover:border-white/[0.12] hover:bg-white/[0.05]",
          open && "border-accent-cyan/30 bg-accent-cyan/5"
        )}
      >
        <FolderOpen className="h-3.5 w-3.5 text-accent-cyan" />
        <span className="text-[13px] font-semibold text-text-primary max-w-[140px] truncate">
          {loading ? "Loading..." : activeFolder?.name ?? "Select Folder"}
        </span>
        <span className="text-[10px] text-text-muted">
          {activeFolder ? `${activeFolder.nodeCount} jobs` : ""}
        </span>
        <ChevronDown className={cn("h-3 w-3 text-text-muted transition-transform", open && "rotate-180")} />
      </button>

      {/* Dropdown */}
      <AnimatePresence>
        {open && (
          <motion.div
            initial={{ opacity: 0, y: -4, scale: 0.98 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: -4, scale: 0.98 }}
            transition={{ duration: 0.15 }}
            className="absolute left-0 top-full mt-2 z-50 min-w-[260px] rounded-xl border border-white/[0.08] bg-bg-card/95 backdrop-blur-xl shadow-2xl"
            style={{ boxShadow: "0 12px 48px rgba(0,0,0,0.5), inset 0 1px 0 rgba(255,255,255,0.03)" }}
          >
            <div className="p-1.5">
              <div className="px-2 py-1.5 text-[10px] font-semibold uppercase tracking-wider text-text-muted">
                Team Folders
              </div>

              {loading ? (
                <div className="flex items-center justify-center py-4">
                  <Loader2 className="h-4 w-4 animate-spin text-text-muted" />
                </div>
              ) : (
                <div className="space-y-0.5">
                  {folders.map((folder) => (
                    <button
                      key={folder.id}
                      onClick={() => { onSelect(folder.id); setOpen(false); }}
                      className={cn(
                        "flex w-full items-center gap-2.5 rounded-lg px-2.5 py-2 text-left transition-all group",
                        folder.id === activeFolderId
                          ? "bg-accent-cyan/10 text-accent-cyan"
                          : "text-text-secondary hover:bg-white/[0.04] hover:text-text-primary"
                      )}
                    >
                      <FolderOpen className="h-3.5 w-3.5 shrink-0" />
                      <div className="flex-1 min-w-0">
                        <div className="text-[12px] font-semibold truncate">{folder.name}</div>
                        <div className="text-[10px] text-text-muted truncate">{folder.description || `${folder.nodeCount} jobs`}</div>
                      </div>
                      <span className="text-[10px] text-text-muted shrink-0">{folder.nodeCount}</span>
                      <button
                        onClick={(e) => handleDelete(e, folder.id)}
                        className="opacity-0 group-hover:opacity-100 transition-opacity p-0.5 rounded hover:bg-red-500/20 hover:text-red-400"
                        title="Delete folder"
                      >
                        <Trash2 className="h-3 w-3" />
                      </button>
                    </button>
                  ))}

                  {folders.length === 0 && (
                    <div className="px-2.5 py-3 text-[11px] text-text-muted text-center">
                      No folders yet. Create one to start.
                    </div>
                  )}
                </div>
              )}
            </div>

            {/* Create new */}
            <div className="border-t border-white/[0.06] p-1.5">
              {creating ? (
                <div className="flex items-center gap-1.5 px-1">
                  <input
                    autoFocus
                    value={newName}
                    onChange={(e) => setNewName(e.target.value)}
                    onKeyDown={(e) => { if (e.key === "Enter") handleCreate(); if (e.key === "Escape") setCreating(false); }}
                    placeholder="TIME_X"
                    className="flex-1 bg-white/[0.04] border border-white/[0.08] rounded-md px-2 py-1 text-[12px] text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent-cyan/40"
                  />
                  <button onClick={handleCreate} className="p-1 rounded hover:bg-emerald-500/20 text-emerald-400">
                    <Plus className="h-3.5 w-3.5" />
                  </button>
                </div>
              ) : (
                <button
                  onClick={() => setCreating(true)}
                  className="flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-[12px] text-text-muted hover:bg-white/[0.04] hover:text-text-secondary transition-all"
                >
                  <Plus className="h-3.5 w-3.5" />
                  New Folder
                </button>
              )}
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}
