import { useState, useMemo } from "react";
import {
  ShieldCheck,
  ShieldAlert,
  AlertTriangle,
  Info,
  ChevronDown,
  ChevronUp,
  RefreshCw,
  X,
} from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { cn } from "@/lib/utils";
import type { ValidationResult, ValidationIssue, ValidationSeverity } from "@/lib/dag-validation";

interface ValidationPanelProps {
  result: ValidationResult | null;
  onValidate: () => void;
  onFocusNode?: (nodeId: string) => void;
  onClose: () => void;
}

const SEVERITY_CONFIG: Record<ValidationSeverity, { icon: typeof ShieldAlert; color: string; bg: string; ring: string }> = {
  error: { icon: ShieldAlert, color: "text-red-400", bg: "bg-red-500/[0.06]", ring: "ring-red-500/20" },
  warning: { icon: AlertTriangle, color: "text-amber-400", bg: "bg-amber-500/[0.06]", ring: "ring-amber-500/20" },
  info: { icon: Info, color: "text-cyan-400", bg: "bg-cyan-500/[0.06]", ring: "ring-cyan-500/20" },
};

function IssueRow({ issue, onFocusNode }: { issue: ValidationIssue; onFocusNode?: (id: string) => void }) {
  const cfg = SEVERITY_CONFIG[issue.severity];
  const Icon = cfg.icon;

  return (
    <div className={cn("flex items-start gap-2.5 px-4 py-2.5 hover:bg-white/[0.02] transition-colors", cfg.bg)}>
      <Icon className={cn("h-3.5 w-3.5 mt-0.5 shrink-0", cfg.color)} />
      <div className="flex-1 min-w-0">
        <p className="text-[11px] text-text-secondary leading-relaxed">{issue.message}</p>
        {issue.nodeIds && issue.nodeIds.length > 0 && (
          <div className="flex flex-wrap gap-1 mt-1">
            {issue.nodeIds.map((id) => (
              <button
                key={id}
                onClick={() => onFocusNode?.(id)}
                className="text-[10px] font-mono text-accent-cyan/80 hover:text-accent-cyan bg-accent-cyan/[0.06] hover:bg-accent-cyan/[0.12] px-1.5 py-0.5 rounded transition-all"
              >
                {id}
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

export default function ValidationPanel({ result, onValidate, onFocusNode, onClose }: ValidationPanelProps) {
  const [expanded, setExpanded] = useState(true);
  const [severityFilter, setSeverityFilter] = useState<ValidationSeverity | "all">("all");

  const filtered = useMemo(() => {
    if (!result) return [];
    if (severityFilter === "all") return result.issues;
    return result.issues.filter((i) => i.severity === severityFilter);
  }, [result, severityFilter]);

  const errorCount = result?.issues.filter((i) => i.severity === "error").length ?? 0;
  const warnCount = result?.issues.filter((i) => i.severity === "warning").length ?? 0;

  return (
    <AnimatePresence>
      <motion.div
        initial={{ opacity: 0, y: 20 }}
        animate={{ opacity: 1, y: 0 }}
        exit={{ opacity: 0, y: 20 }}
        transition={{ type: "spring", damping: 25, stiffness: 300 }}
        className="absolute bottom-4 right-4 z-50 w-[380px] rounded-xl border border-white/[0.06] bg-bg-surface/95 backdrop-blur-xl shadow-2xl overflow-hidden"
        style={{ boxShadow: "0 8px 40px rgba(0,0,0,0.5), inset 0 1px 0 rgba(255,255,255,0.03)" }}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-white/[0.04]">
          <div className="flex items-center gap-2.5">
            {result?.valid ? (
              <ShieldCheck className="h-4 w-4 text-emerald-400" />
            ) : (
              <ShieldAlert className="h-4 w-4 text-red-400" />
            )}
            <div>
              <p className="text-[13px] font-semibold text-text-primary">
                {result ? (result.valid ? "DAG Valid" : "DAG Issues Found") : "DAG Validation"}
              </p>
              {result && (
                <p className="text-[10px] text-text-muted">
                  {result.stats.nodeCount} nodes · {result.stats.edgeCount} edges
                </p>
              )}
            </div>
          </div>
          <div className="flex items-center gap-1">
            <button
              onClick={onValidate}
              className="p-1.5 rounded-md hover:bg-white/[0.06] text-text-muted hover:text-accent-cyan transition-all"
              title="Re-validate"
            >
              <RefreshCw className="h-3.5 w-3.5" />
            </button>
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
          <>
            {/* Summary badges */}
            {result && result.issues.length > 0 && (
              <div className="flex items-center gap-2 px-4 py-2 border-b border-white/[0.04]">
                <button
                  onClick={() => setSeverityFilter("all")}
                  className={cn(
                    "text-[10px] px-2 py-0.5 rounded-md transition-all",
                    severityFilter === "all"
                      ? "bg-white/[0.08] text-text-primary ring-1 ring-white/[0.1]"
                      : "text-text-muted hover:text-text-secondary"
                  )}
                >
                  All ({result.issues.length})
                </button>
                {errorCount > 0 && (
                  <button
                    onClick={() => setSeverityFilter("error")}
                    className={cn(
                      "text-[10px] px-2 py-0.5 rounded-md transition-all",
                      severityFilter === "error"
                        ? "bg-red-500/10 text-red-400 ring-1 ring-red-500/20"
                        : "text-red-400/60 hover:text-red-400"
                    )}
                  >
                    Errors ({errorCount})
                  </button>
                )}
                {warnCount > 0 && (
                  <button
                    onClick={() => setSeverityFilter("warning")}
                    className={cn(
                      "text-[10px] px-2 py-0.5 rounded-md transition-all",
                      severityFilter === "warning"
                        ? "bg-amber-500/10 text-amber-400 ring-1 ring-amber-500/20"
                        : "text-amber-400/60 hover:text-amber-400"
                    )}
                  >
                    Warnings ({warnCount})
                  </button>
                )}
              </div>
            )}

            {/* Issues list */}
            <div className="max-h-[260px] overflow-y-auto">
              {!result ? (
                <div className="flex items-center justify-center py-8 text-text-muted text-[12px]">
                  Click validate to check workflow DAG
                </div>
              ) : result.issues.length === 0 ? (
                <div className="flex flex-col items-center justify-center py-8 gap-2">
                  <ShieldCheck className="h-8 w-8 text-emerald-400/60" />
                  <p className="text-[12px] text-emerald-400/80 font-medium">
                    No issues found — DAG is valid
                  </p>
                  <p className="text-[10px] text-text-muted">
                    {result.stats.nodeCount} nodes, {result.stats.edgeCount} edges checked
                  </p>
                </div>
              ) : (
                filtered.map((issue) => (
                  <IssueRow key={issue.id} issue={issue} onFocusNode={onFocusNode} />
                ))
              )}
            </div>
          </>
        )}
      </motion.div>
    </AnimatePresence>
  );
}
