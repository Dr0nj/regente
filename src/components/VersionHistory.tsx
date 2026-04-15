import { useState, useEffect } from "react";
import { History, RotateCcw, X, ChevronDown, ChevronUp, Clock } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { cn } from "@/lib/utils";
import { listVersions, type WorkflowVersion } from "@/lib/workflow-versions";

type VersionMeta = Omit<WorkflowVersion, "nodes" | "edges">;

interface VersionHistoryProps {
  folderId: string | null;
  onRestore: (version: number) => void;
  onClose: () => void;
}

function formatDate(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleString("en-US", {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  });
}

export default function VersionHistory({ folderId, onRestore, onClose }: VersionHistoryProps) {
  const [versions, setVersions] = useState<VersionMeta[]>([]);
  const [expanded, setExpanded] = useState(true);

  useEffect(() => {
    if (!folderId) {
      setVersions([]);
      return;
    }
    setVersions(listVersions(folderId).reverse());
  }, [folderId]);

  return (
    <AnimatePresence>
      <motion.div
        initial={{ opacity: 0, y: 20 }}
        animate={{ opacity: 1, y: 0 }}
        exit={{ opacity: 0, y: 20 }}
        transition={{ type: "spring", damping: 25, stiffness: 300 }}
        className="absolute bottom-4 left-4 z-50 w-[340px] rounded-xl border border-white/[0.06] bg-bg-surface/95 backdrop-blur-xl shadow-2xl overflow-hidden"
        style={{ boxShadow: "0 8px 40px rgba(0,0,0,0.5), inset 0 1px 0 rgba(255,255,255,0.03)" }}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-white/[0.04]">
          <div className="flex items-center gap-2.5">
            <History className="h-4 w-4 text-accent-cyan" />
            <div>
              <p className="text-[13px] font-semibold text-text-primary">Version History</p>
              <p className="text-[10px] text-text-muted">{versions.length} version{versions.length !== 1 ? "s" : ""}</p>
            </div>
          </div>
          <div className="flex items-center gap-1">
            <button
              onClick={() => setExpanded(!expanded)}
              className="p-1.5 rounded-md hover:bg-white/[0.06] text-text-muted hover:text-text-secondary transition-all"
            >
              {expanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronUp className="h-3.5 w-3.5" />}
            </button>
            <button
              onClick={onClose}
              className="p-1.5 rounded-md hover:bg-white/[0.06] text-text-muted hover:text-text-secondary transition-all"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
        </div>

        {expanded && (
          <div className="max-h-[300px] overflow-y-auto">
            {versions.length === 0 ? (
              <div className="flex flex-col items-center justify-center py-8 gap-2">
                <History className="h-8 w-8 text-text-muted/40" />
                <p className="text-[12px] text-text-muted">No versions saved yet</p>
                <p className="text-[10px] text-text-muted/60">Save to create the first version</p>
              </div>
            ) : (
              versions.map((v, i) => (
                <div
                  key={v.version}
                  className={cn(
                    "flex items-center gap-3 px-4 py-2.5 hover:bg-white/[0.02] transition-colors border-b border-white/[0.02] last:border-0",
                    i === 0 && "bg-accent-cyan/[0.03]"
                  )}
                >
                  <div className="flex flex-col items-center shrink-0">
                    <span className={cn(
                      "text-[13px] font-bold tabular-nums",
                      i === 0 ? "text-accent-cyan" : "text-text-muted"
                    )}>
                      v{v.version}
                    </span>
                  </div>
                  <div className="flex-1 min-w-0">
                    <p className="text-[11px] text-text-secondary truncate">{v.label}</p>
                    <div className="flex items-center gap-2 mt-0.5">
                      <span className="flex items-center gap-1 text-[10px] text-text-muted">
                        <Clock className="h-2.5 w-2.5" />
                        {formatDate(v.savedAt)}
                      </span>
                      <span className="text-[10px] text-text-muted">
                        {v.nodeCount}n · {v.edgeCount}e
                      </span>
                    </div>
                  </div>
                  {i > 0 && (
                    <button
                      onClick={() => onRestore(v.version)}
                      className="flex items-center gap-1 text-[10px] text-amber-400/80 hover:text-amber-400 bg-amber-500/[0.06] hover:bg-amber-500/[0.12] px-2 py-1 rounded-md transition-all whitespace-nowrap"
                      title={`Restore version ${v.version}`}
                    >
                      <RotateCcw className="h-3 w-3" />
                      Restore
                    </button>
                  )}
                  {i === 0 && (
                    <span className="text-[10px] text-accent-cyan/60 font-medium px-2 py-1">
                      Current
                    </span>
                  )}
                </div>
              ))
            )}
          </div>
        )}
      </motion.div>
    </AnimatePresence>
  );
}
