/**
 * AuditLog — Phase 8
 *
 * Floating panel showing the audit trail of all user/system actions.
 * Supports filtering by action type and search.
 */

import { useState, useMemo } from "react";
import { X, ScrollText, Search, Trash2 } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { Button } from "@/components/ui/button";
import {
  getRecentAudit,
  clearAudit,
  actionLabel,
  actionColor,
  type AuditEntry,
} from "@/lib/audit";
import { cn } from "@/lib/utils";

interface AuditLogProps {
  onClose: () => void;
}

function formatTime(ts: number): string {
  const d = new Date(ts);
  const today = new Date();
  const isToday = d.toDateString() === today.toDateString();
  const time = d.toLocaleTimeString("en-US", { hour12: false, hour: "2-digit", minute: "2-digit", second: "2-digit" });
  return isToday ? time : `${d.toLocaleDateString("en-US", { month: "short", day: "numeric" })} ${time}`;
}

function EntryRow({ entry }: { entry: AuditEntry }) {
  return (
    <div className="flex items-start gap-3 rounded-lg px-3 py-2 hover:bg-white/[0.02] transition-colors">
      {/* Timestamp */}
      <span className="shrink-0 text-[10px] font-mono text-text-muted/70 mt-0.5 w-[70px]">
        {formatTime(entry.timestamp)}
      </span>

      {/* Action badge */}
      <span
        className={cn(
          "shrink-0 inline-flex items-center rounded-full px-1.5 py-0.5 text-[9px] font-medium ring-1 mt-0.5",
          actionColor(entry.action)
        )}
      >
        {actionLabel(entry.action)}
      </span>

      {/* Details */}
      <div className="flex-1 min-w-0">
        {entry.targetName && (
          <span className="text-[11px] text-text-primary font-medium">
            {entry.targetName}
          </span>
        )}
        {entry.details && (
          <p className="text-[10px] text-text-muted truncate mt-0.5">
            {Object.entries(entry.details)
              .map(([k, v]) => `${k}: ${typeof v === "object" ? JSON.stringify(v) : v}`)
              .join(" · ")}
          </p>
        )}
      </div>

      {/* Actor */}
      <span className="shrink-0 text-[9px] text-text-muted/50 mt-0.5">
        {entry.actor}
      </span>
    </div>
  );
}

export default function AuditLog({ onClose }: AuditLogProps) {
  const [searchTerm, setSearchTerm] = useState("");
  const [, setRefresh] = useState(0);
  const entries = useMemo(() => getRecentAudit(100), []);

  const filtered = useMemo(() => {
    if (!searchTerm) return entries;
    const lower = searchTerm.toLowerCase();
    return entries.filter(
      (e) =>
        actionLabel(e.action).toLowerCase().includes(lower) ||
        e.targetName?.toLowerCase().includes(lower) ||
        e.target.toLowerCase().includes(lower) ||
        e.actor.toLowerCase().includes(lower)
    );
  }, [entries, searchTerm]);

  const handleClear = () => {
    clearAudit();
    setRefresh((r) => r + 1);
  };

  return (
    <AnimatePresence>
      <motion.div
        initial={{ opacity: 0, x: 30 }}
        animate={{ opacity: 1, x: 0 }}
        exit={{ opacity: 0, x: 30 }}
        className="absolute right-4 top-16 z-50 w-[480px] rounded-xl border border-white/[0.06] bg-bg-surface/95 backdrop-blur-xl shadow-2xl"
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b border-white/[0.04] px-4 py-3">
          <div className="flex items-center gap-2">
            <ScrollText className="h-4 w-4 text-accent-cyan" />
            <span className="text-[13px] font-semibold text-text-primary">
              Audit Trail
            </span>
            <span className="text-[10px] text-text-muted">
              ({filtered.length} entries)
            </span>
          </div>
          <div className="flex items-center gap-1">
            <Button variant="ghost" size="icon-sm" onClick={handleClear} title="Clear audit log">
              <Trash2 className="h-3.5 w-3.5 text-text-muted" />
            </Button>
            <Button variant="ghost" size="icon-sm" onClick={onClose}>
              <X className="h-4 w-4" />
            </Button>
          </div>
        </div>

        {/* Search */}
        <div className="px-4 py-2 border-b border-white/[0.04]">
          <div className="relative flex items-center">
            <Search className="absolute left-2 h-3.5 w-3.5 text-text-muted pointer-events-none" />
            <input
              type="text"
              value={searchTerm}
              onChange={(e) => setSearchTerm(e.target.value)}
              placeholder="Filter audit entries..."
              className="h-7 w-full rounded-md border border-white/[0.06] bg-white/[0.03] pl-7 pr-2 text-[11px] text-text-primary placeholder:text-text-muted/50 outline-none focus:border-accent-cyan/40 focus:ring-1 focus:ring-accent-cyan/20 transition-all"
            />
          </div>
        </div>

        {/* Entries */}
        <div className="max-h-[400px] overflow-y-auto py-1">
          {filtered.length === 0 ? (
            <div className="py-8 text-center">
              <ScrollText className="mx-auto mb-2 h-8 w-8 text-text-muted/30" />
              <p className="text-[12px] text-text-muted">
                {entries.length === 0 ? "No audit entries yet" : "No matches"}
              </p>
            </div>
          ) : (
            filtered.map((entry) => <EntryRow key={entry.id} entry={entry} />)
          )}
        </div>
      </motion.div>
    </AnimatePresence>
  );
}
