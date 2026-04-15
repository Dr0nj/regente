import { X, Clock, RotateCcw, Hash, Tag, Calendar, AlertTriangle, Bell, Copy } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { JOB_TYPES, STATUS_MAP, type JobNodeData } from "@/lib/job-config";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { AppMode } from "@/lib/types";
import { cn } from "@/lib/utils";

interface PropertiesPanelProps {
  nodeData: JobNodeData | null;
  nodeId: string | null;
  mode: AppMode;
  onClose: () => void;
  onUpdate: (nodeId: string, data: Partial<JobNodeData>) => void;
  onDelete?: (nodeId: string) => void;
  onDuplicate?: (nodeId: string) => void;
}

function FieldLabel({ children }: { children: React.ReactNode }) {
  return (
    <label className="block text-[11px] font-semibold uppercase tracking-wider text-text-muted mb-1.5">
      {children}
    </label>
  );
}

export default function PropertiesPanel({
  nodeData,
  nodeId,
  mode,
  onClose,
  onUpdate,
  onDelete,
  onDuplicate,
}: PropertiesPanelProps) {
  if (!nodeData || !nodeId) return null;

  const typeConfig = JOB_TYPES[nodeData.jobType];
  const statusConfig = STATUS_MAP[nodeData.status];
  const Icon = typeConfig.icon;
  const isDesign = mode === "design";

  return (
    <AnimatePresence>
      <motion.aside
        initial={{ x: 320, opacity: 0 }}
        animate={{ x: 0, opacity: 1 }}
        exit={{ x: 320, opacity: 0 }}
        transition={{ type: "spring", damping: 25, stiffness: 300 }}
        className="flex w-[320px] shrink-0 flex-col border-l border-white/[0.04] bg-bg-surface/95 backdrop-blur-xl"
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b border-white/[0.04] px-5 py-3.5">
          <div className="flex items-center gap-2.5">
            <div className={cn("flex h-8 w-8 items-center justify-center rounded-lg", typeConfig.iconBg)}>
              <Icon className="h-4 w-4" strokeWidth={1.8} />
            </div>
            <div>
              <p className="text-[13px] font-semibold text-text-primary">
                {isDesign ? "Properties" : "Status"}
              </p>
              <p className="text-[10px] text-text-muted">{typeConfig.label} Node</p>
            </div>
          </div>
          <Button variant="ghost" size="icon-sm" onClick={onClose}>
            <X className="h-4 w-4" />
          </Button>
        </div>

        <div className="flex-1 overflow-y-auto px-5 py-4 space-y-5">
          {/* Status card */}
          <div className="glass-card rounded-xl p-3.5">
            <div className="flex items-center justify-between mb-2">
              <span className="text-[11px] font-semibold uppercase tracking-wider text-text-muted">
                Current Status
              </span>
              <Badge variant={statusConfig.variant}>
                <span className={cn("inline-block h-1.5 w-1.5 rounded-full", statusConfig.dotColor, nodeData.status === "RUNNING" && "animate-pulse-dot")} />
                {statusConfig.label}
              </Badge>
            </div>
            {nodeData.lastRun && (
              <div className="flex items-center gap-1.5 text-[11px] text-text-muted">
                <Calendar className="h-3 w-3" />
                Last run: {nodeData.lastRun}
              </div>
            )}
          </div>

          {/* Monitoring: execution details */}
          {!isDesign && (
            <>
              <div className="glass-card rounded-xl p-3.5 space-y-2">
                <p className="text-[11px] font-semibold uppercase tracking-wider text-text-muted">
                  Execution Details
                </p>
                <div className="space-y-1.5 text-[12px]">
                  <div className="flex justify-between">
                    <span className="text-text-muted">Duration</span>
                    <span className="text-text-secondary font-mono">
                      {nodeData.status === "RUNNING" ? "02:34 (ongoing)" : "01:12"}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-text-muted">Attempts</span>
                    <span className="text-text-secondary font-mono">1 / {nodeData.retries ?? 3}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-text-muted">Started</span>
                    <span className="text-text-secondary font-mono">14:32:05</span>
                  </div>
                </div>
              </div>

              {nodeData.status === "FAILED" && (
                <div className="rounded-xl border border-red-500/20 bg-red-500/[0.05] p-3.5">
                  <div className="flex items-center gap-1.5 mb-1.5">
                    <AlertTriangle className="h-3.5 w-3.5 text-red-400" />
                    <span className="text-[11px] font-semibold uppercase tracking-wider text-red-400">
                      Error
                    </span>
                  </div>
                  <p className="text-[11px] text-red-300/80 font-mono leading-relaxed">
                    TimeoutError: Lambda execution exceeded 300s limit
                  </p>
                </div>
              )}
            </>
          )}

          {/* Design: editable fields */}
          {isDesign && (
            <>
              <div>
                <FieldLabel>
                  <span className="inline-flex items-center gap-1"><Tag className="h-3 w-3" /> Job Name</span>
                </FieldLabel>
                <input
                  className="properties-input"
                  value={nodeData.label}
                  onChange={(e) => onUpdate(nodeId, { label: e.target.value })}
                />
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <FieldLabel>
                    <span className="inline-flex items-center gap-1"><Clock className="h-3 w-3" /> Timeout (s)</span>
                  </FieldLabel>
                  <input
                    type="number"
                    className="properties-input"
                    placeholder="300"
                    value={nodeData.timeout ?? ""}
                    onChange={(e) => onUpdate(nodeId, { timeout: e.target.value ? Number(e.target.value) : undefined })}
                  />
                </div>
                <div>
                  <FieldLabel>
                    <span className="inline-flex items-center gap-1"><RotateCcw className="h-3 w-3" /> Retries</span>
                  </FieldLabel>
                  <input
                    type="number"
                    className="properties-input"
                    placeholder="3"
                    value={nodeData.retries ?? ""}
                    onChange={(e) => onUpdate(nodeId, { retries: e.target.value ? Number(e.target.value) : undefined })}
                  />
                </div>
              </div>

              <div>
                <FieldLabel>
                  <span className="inline-flex items-center gap-1"><Calendar className="h-3 w-3" /> Schedule (cron)</span>
                </FieldLabel>
                <input
                  className="properties-input"
                  placeholder="0 8 * * MON-FRI"
                  value={nodeData.schedule ?? ""}
                  onChange={(e) => onUpdate(nodeId, { schedule: e.target.value || undefined })}
                />
              </div>

              <div>
                <FieldLabel>
                  <span className="inline-flex items-center gap-1"><Bell className="h-3 w-3" /> Alert Channel</span>
                </FieldLabel>
                <input
                  className="properties-input"
                  placeholder="#alerts-production"
                  value={(nodeData.alertChannel as string) ?? ""}
                  onChange={(e) => onUpdate(nodeId, { alertChannel: e.target.value || undefined })}
                />
              </div>

              <div>
                <FieldLabel>
                  <span className="inline-flex items-center gap-1"><Hash className="h-3 w-3" /> Node ID</span>
                </FieldLabel>
                <div className="properties-input !bg-white/[0.02] text-text-muted text-[12px] font-mono">
                  {nodeId}
                </div>
              </div>

              <div className="pt-2 border-t border-white/[0.04]">
                <div className="flex items-center gap-1.5 mb-2">
                  <AlertTriangle className="h-3 w-3 text-red-400" />
                  <span className="text-[11px] font-semibold uppercase tracking-wider text-red-400/80">
                    Danger Zone
                  </span>
                </div>
                <Button variant="destructive" size="sm" className="w-full" onClick={() => { onDelete?.(nodeId); onClose(); }}>
                  Delete Node
                </Button>
                <Button variant="secondary" size="sm" className="w-full mt-2 gap-1.5" onClick={() => onDuplicate?.(nodeId)}>
                  <Copy className="h-3.5 w-3.5" />
                  Duplicate Node
                </Button>
              </div>
            </>
          )}
        </div>
      </motion.aside>
    </AnimatePresence>
  );
}
