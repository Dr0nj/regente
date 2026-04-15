import { useState, useEffect, useRef } from "react";
import { Terminal, ChevronDown, ChevronUp, Search, Trash2 } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { cn } from "@/lib/utils";
import type { JobStatus } from "@/lib/job-config";

export interface LogEntry {
  id: string;
  timestamp: string;
  nodeId: string;
  nodeName: string;
  level: "info" | "warn" | "error" | "success" | "debug";
  message: string;
}

interface ExecutionLogProps {
  logs: LogEntry[];
  onClear?: () => void;
  selectedNodeId?: string | null;
}

const LEVEL_STYLES: Record<LogEntry["level"], { dot: string; text: string; label: string }> = {
  info:    { dot: "bg-cyan-400",    text: "text-cyan-300",    label: "INFO" },
  warn:    { dot: "bg-amber-400",   text: "text-amber-300",   label: "WARN" },
  error:   { dot: "bg-red-400",     text: "text-red-300",     label: "ERROR" },
  success: { dot: "bg-emerald-400", text: "text-emerald-300", label: "OK" },
  debug:   { dot: "bg-slate-400",   text: "text-slate-400",   label: "DEBUG" },
};

export function generateSimulationLogs(
  nodeId: string,
  nodeName: string,
  status: JobStatus,
  layerIdx: number,
): LogEntry[] {
  const now = new Date();
  const base = now.getTime() - (3 - layerIdx) * 2000;
  const ts = (offset: number) => {
    const d = new Date(base + offset);
    return d.toLocaleTimeString("en-US", { hour12: false, hour: "2-digit", minute: "2-digit", second: "2-digit" });
  };

  const entries: LogEntry[] = [
    { id: `${nodeId}-1`, timestamp: ts(0), nodeId, nodeName, level: "info", message: `Scheduler triggered execution for [${nodeName}]` },
    { id: `${nodeId}-2`, timestamp: ts(120), nodeId, nodeName, level: "debug", message: `Resolving dependencies... 0 upstream pending` },
    { id: `${nodeId}-3`, timestamp: ts(340), nodeId, nodeName, level: "info", message: `Container provisioned — image: regente/${nodeName.toLowerCase().replace(/\s+/g, "-")}:latest` },
    { id: `${nodeId}-4`, timestamp: ts(800), nodeId, nodeName, level: "info", message: `Execution started (attempt 1/3, timeout: 300s)` },
  ];

  if (status === "FAILED") {
    entries.push(
      { id: `${nodeId}-5`, timestamp: ts(1400), nodeId, nodeName, level: "warn", message: `Memory usage spike: 85% → 97%` },
      { id: `${nodeId}-6`, timestamp: ts(1800), nodeId, nodeName, level: "error", message: `TimeoutError: Lambda execution exceeded 300s limit` },
      { id: `${nodeId}-7`, timestamp: ts(1900), nodeId, nodeName, level: "error", message: `Execution FAILED after 1 attempt(s). No retries remaining.` },
    );
  } else if (status === "SUCCESS") {
    entries.push(
      { id: `${nodeId}-5`, timestamp: ts(1100), nodeId, nodeName, level: "info", message: `Processing ${Math.floor(Math.random() * 50000 + 10000).toLocaleString()} records...` },
      { id: `${nodeId}-6`, timestamp: ts(1600), nodeId, nodeName, level: "success", message: `Execution completed successfully in ${(Math.random() * 4 + 0.5).toFixed(1)}s` },
    );
  } else if (status === "RUNNING") {
    entries.push(
      { id: `${nodeId}-5`, timestamp: ts(1100), nodeId, nodeName, level: "info", message: `Processing records... (in progress)` },
    );
  }

  return entries;
}

export default function ExecutionLog({ logs, onClear, selectedNodeId }: ExecutionLogProps) {
  const [expanded, setExpanded] = useState(true);
  const [filter, setFilter] = useState("");
  const [filterNode, setFilterNode] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);

  // Auto-scroll on new logs
  useEffect(() => {
    if (scrollRef.current && expanded) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [logs, expanded]);

  const filtered = logs.filter((l) => {
    if (filterNode && selectedNodeId && l.nodeId !== selectedNodeId) return false;
    if (filter && !l.message.toLowerCase().includes(filter.toLowerCase()) && !l.nodeName.toLowerCase().includes(filter.toLowerCase())) return false;
    return true;
  });

  const height = expanded ? 220 : 36;

  return (
    <motion.div
      animate={{ height }}
      transition={{ type: "spring", damping: 25, stiffness: 300 }}
      className="shrink-0 border-t border-white/[0.06] bg-bg-surface/95 backdrop-blur-xl overflow-hidden"
      style={{ boxShadow: "inset 0 1px 0 rgba(255,255,255,0.02), 0 -4px 24px rgba(0,0,0,0.3)" }}
    >
      {/* Header bar */}
      <div className="flex items-center gap-2 px-4 h-[36px] border-b border-white/[0.04]">
        <Terminal className="h-3.5 w-3.5 text-text-muted" />
        <span className="text-[11px] font-semibold text-text-secondary uppercase tracking-wider">
          Execution Log
        </span>
        <span className="text-[10px] text-text-muted tabular-nums">
          ({filtered.length} entries)
        </span>

        <div className="flex-1" />

        {expanded && (
          <>
            {/* Filter by selected node */}
            {selectedNodeId && (
              <button
                onClick={() => setFilterNode(!filterNode)}
                className={cn(
                  "text-[10px] px-2 py-0.5 rounded-md transition-all",
                  filterNode
                    ? "bg-accent-cyan/10 text-accent-cyan ring-1 ring-accent-cyan/20"
                    : "text-text-muted hover:text-text-secondary hover:bg-white/[0.04]"
                )}
              >
                Selected only
              </button>
            )}

            {/* Search */}
            <div className="flex items-center gap-1 bg-white/[0.03] rounded-md px-2 py-0.5 ring-1 ring-white/[0.06]">
              <Search className="h-3 w-3 text-text-muted" />
              <input
                className="bg-transparent text-[11px] text-text-primary outline-none w-[100px] placeholder:text-text-muted"
                placeholder="Filter..."
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
              />
            </div>

            <button onClick={onClear} className="p-1 rounded hover:bg-white/[0.04] text-text-muted hover:text-text-secondary transition-all" title="Clear logs">
              <Trash2 className="h-3 w-3" />
            </button>
          </>
        )}

        <button onClick={() => setExpanded(!expanded)} className="p-1 rounded hover:bg-white/[0.04] text-text-muted hover:text-text-secondary transition-all">
          {expanded ? <ChevronDown className="h-3.5 w-3.5" /> : <ChevronUp className="h-3.5 w-3.5" />}
        </button>
      </div>

      {/* Log entries */}
      <AnimatePresence>
        {expanded && (
          <div ref={scrollRef} className="overflow-y-auto h-[calc(100%-36px)] font-mono text-[11px] leading-relaxed">
            {filtered.length === 0 ? (
              <div className="flex items-center justify-center h-full text-text-muted text-[12px]">
                No logs yet. Run a workflow to see execution output.
              </div>
            ) : (
              filtered.map((log, i) => {
                const style = LEVEL_STYLES[log.level];
                return (
                  <motion.div
                    key={log.id + i}
                    initial={{ opacity: 0, y: 4 }}
                    animate={{ opacity: 1, y: 0 }}
                    className={cn(
                      "flex items-start gap-2 px-4 py-1 hover:bg-white/[0.02] transition-colors",
                      selectedNodeId === log.nodeId && "bg-accent-cyan/[0.03]"
                    )}
                  >
                    <span className="text-text-muted shrink-0 w-[62px]">{log.timestamp}</span>
                    <span className={cn("shrink-0 w-[42px] font-bold", style.text)}>
                      {style.label}
                    </span>
                    <span className="shrink-0 text-accent-cyan/60 w-[120px] truncate" title={log.nodeName}>
                      [{log.nodeName}]
                    </span>
                    <span className={cn(
                      "flex-1",
                      log.level === "error" ? "text-red-300/90" :
                      log.level === "warn" ? "text-amber-300/90" :
                      log.level === "success" ? "text-emerald-300/90" :
                      "text-text-secondary"
                    )}>
                      {log.message}
                    </span>
                  </motion.div>
                );
              })
            )}
          </div>
        )}
      </AnimatePresence>
    </motion.div>
  );
}
