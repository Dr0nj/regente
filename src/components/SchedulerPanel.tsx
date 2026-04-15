/**
 * SchedulerPanel — Phase 7
 *
 * Floating panel showing cron-scheduled workflows, their next run times,
 * and enable/disable toggle. Also shows the execution engine status.
 */

import { X, Calendar, Clock, Play, Pause, Trash2, Zap } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { Button } from "@/components/ui/button";
import { useExecution } from "@/lib/execution-context";
import { cn } from "@/lib/utils";

interface SchedulerPanelProps {
  onClose: () => void;
}

function TimeAgo({ date }: { date: Date | null }) {
  if (!date) return <span className="text-text-muted">—</span>;
  const diff = date.getTime() - Date.now();
  if (diff < 0) return <span className="text-amber-400">overdue</span>;
  const minutes = Math.floor(diff / 60_000);
  const hours = Math.floor(minutes / 60);
  if (hours > 0) return <span>{hours}h {minutes % 60}m</span>;
  return <span>{minutes}m</span>;
}

export default function SchedulerPanel({ onClose }: SchedulerPanelProps) {
  const { schedules, toggleSchedule, unregisterSchedule, running, describeCron: describe } = useExecution();

  return (
    <AnimatePresence>
      <motion.div
        initial={{ opacity: 0, x: 30 }}
        animate={{ opacity: 1, x: 0 }}
        exit={{ opacity: 0, x: 30 }}
        className="absolute right-4 top-16 z-50 w-[380px] rounded-xl border border-white/[0.06] bg-bg-surface/95 backdrop-blur-xl shadow-2xl"
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b border-white/[0.04] px-4 py-3">
          <div className="flex items-center gap-2">
            <Calendar className="h-4 w-4 text-accent-cyan" />
            <span className="text-[13px] font-semibold text-text-primary">
              Scheduler
            </span>
            {running && (
              <span className="flex items-center gap-1 rounded-full bg-cyan-500/10 px-2 py-0.5 text-[10px] font-medium text-cyan-400 ring-1 ring-cyan-500/20">
                <Zap className="h-3 w-3 animate-pulse" />
                Running
              </span>
            )}
          </div>
          <Button variant="ghost" size="icon-sm" onClick={onClose}>
            <X className="h-4 w-4" />
          </Button>
        </div>

        {/* Schedule list */}
        <div className="max-h-[400px] overflow-y-auto p-3 space-y-2">
          {schedules.length === 0 ? (
            <div className="py-8 text-center">
              <Clock className="mx-auto mb-2 h-8 w-8 text-text-muted/30" />
              <p className="text-[12px] text-text-muted">
                No scheduled workflows
              </p>
              <p className="text-[11px] text-text-muted/60 mt-1">
                Add a cron schedule to a workflow's properties to see it here
              </p>
            </div>
          ) : (
            schedules.map((s) => (
              <div
                key={s.workflowId}
                className={cn(
                  "flex items-center justify-between rounded-lg border px-3 py-2.5 transition-all",
                  s.enabled
                    ? "border-white/[0.06] bg-white/[0.02]"
                    : "border-white/[0.03] bg-white/[0.01] opacity-50"
                )}
              >
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-[12px] font-semibold text-text-primary truncate">
                      {s.workflowName}
                    </span>
                    {s.lastStatus && (
                      <span
                        className={cn(
                          "inline-block h-1.5 w-1.5 rounded-full",
                          s.lastStatus === "SUCCESS" ? "bg-emerald-400" : "bg-red-400"
                        )}
                      />
                    )}
                  </div>
                  <div className="mt-0.5 flex items-center gap-2 text-[10px] text-text-muted">
                    <code className="rounded bg-white/[0.04] px-1 py-0.5 font-mono">
                      {s.cronExpression}
                    </code>
                    <span className="text-text-muted/60">
                      {describe(s.cronExpression)}
                    </span>
                  </div>
                  <div className="mt-1 flex items-center gap-3 text-[10px]">
                    <span className="text-text-muted">
                      Next:{" "}
                      <span className="text-text-secondary">
                        <TimeAgo date={s.nextRunAt} />
                      </span>
                    </span>
                    {s.lastRunAt && (
                      <span className="text-text-muted">
                        Last:{" "}
                        <span className="text-text-secondary">
                          {s.lastRunAt.toLocaleTimeString("en-US", {
                            hour12: false,
                            hour: "2-digit",
                            minute: "2-digit",
                          })}
                        </span>
                      </span>
                    )}
                  </div>
                </div>

                {/* Actions */}
                <div className="flex items-center gap-1 ml-2">
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    title={s.enabled ? "Pause schedule" : "Resume schedule"}
                    onClick={() => toggleSchedule(s.workflowId)}
                  >
                    {s.enabled ? (
                      <Pause className="h-3.5 w-3.5 text-amber-400" />
                    ) : (
                      <Play className="h-3.5 w-3.5 text-emerald-400" />
                    )}
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    title="Remove schedule"
                    onClick={() => unregisterSchedule(s.workflowId)}
                  >
                    <Trash2 className="h-3.5 w-3.5 text-red-400/60" />
                  </Button>
                </div>
              </div>
            ))
          )}
        </div>

        {/* Footer */}
        <div className="border-t border-white/[0.04] px-4 py-2">
          <p className="text-[10px] text-text-muted/60">
            Scheduler checks every 30s • Cron format: min hour day month dow
          </p>
        </div>
      </motion.div>
    </AnimatePresence>
  );
}
