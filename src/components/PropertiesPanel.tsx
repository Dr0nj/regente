import { X, Clock, RotateCcw, Hash, Tag, Calendar, AlertTriangle, Bell, Copy, Plus, Trash2, Variable, Globe, Play } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { JOB_TYPES, STATUS_MAP, type JobNodeData, type JobNodeVariable, type HttpMethod } from "@/lib/job-config";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { AppMode } from "@/lib/types";
import { cn } from "@/lib/utils";
import { describeCron, validateCron } from "@/lib/cron";
import { INSTANCE_STATUS_CONFIG } from "@/lib/orchestrator-model";
import type { JobInstance } from "@/lib/orchestrator-model";

interface PropertiesPanelProps {
  nodeData: JobNodeData | null;
  nodeId: string | null;
  mode: AppMode;
  /** Active instance for this node (Monitoring mode) */
  instance?: JobInstance | null;
  onClose: () => void;
  onUpdate: (nodeId: string, data: Partial<JobNodeData>) => void;
  onDelete?: (nodeId: string) => void;
  onDuplicate?: (nodeId: string) => void;
  /** Monitoring actions */
  onHold?: (instanceId: string) => void;
  onRelease?: (instanceId: string) => void;
  onCancel?: (instanceId: string) => void;
  onRerun?: (instanceId: string) => void;
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
  instance,
  onClose,
  onUpdate,
  onDelete,
  onDuplicate,
  onHold,
  onRelease,
  onCancel,
  onRerun,
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

          {/* Monitoring: instance execution details */}
          {!isDesign && instance && (
            <>
              {/* Instance status */}
              <div className="glass-card rounded-xl p-3.5">
                <div className="flex items-center justify-between mb-2">
                  <span className="text-[11px] font-semibold uppercase tracking-wider text-text-muted">
                    Instance Status
                  </span>
                  <Badge variant={instance.status === "OK" ? "success" : instance.status === "RUNNING" ? "running" : instance.status === "NOTOK" ? "failed" : instance.status === "HOLD" ? "inactive" : "waiting"}>
                    <span className={cn("inline-block h-1.5 w-1.5 rounded-full", INSTANCE_STATUS_CONFIG[instance.status].dotColor, instance.status === "RUNNING" && "animate-pulse-dot")} />
                    {INSTANCE_STATUS_CONFIG[instance.status].label}
                  </Badge>
                </div>
                {instance.manual && (
                  <span className="text-[10px] text-amber-400/80 font-medium">Manual (Force/Order)</span>
                )}
              </div>

              <div className="glass-card rounded-xl p-3.5 space-y-2">
                <p className="text-[11px] font-semibold uppercase tracking-wider text-text-muted">
                  Execution Details
                </p>
                <div className="space-y-1.5 text-[12px]">
                  <div className="flex justify-between">
                    <span className="text-text-muted">Duration</span>
                    <span className="text-text-secondary font-mono">
                      {instance.durationMs != null
                        ? `${(instance.durationMs / 1000).toFixed(1)}s`
                        : instance.startedAt
                          ? `${((Date.now() - instance.startedAt) / 1000).toFixed(0)}s (ongoing)`
                          : "—"}
                    </span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-text-muted">Attempts</span>
                    <span className="text-text-secondary font-mono">{instance.attempts} / {instance.retries + 1}</span>
                  </div>
                  <div className="flex justify-between">
                    <span className="text-text-muted">Scheduled</span>
                    <span className="text-text-secondary font-mono">
                      {new Date(instance.scheduledAt).toLocaleTimeString("en-US", { hour12: false })}
                    </span>
                  </div>
                  {instance.startedAt && (
                    <div className="flex justify-between">
                      <span className="text-text-muted">Started</span>
                      <span className="text-text-secondary font-mono">
                        {new Date(instance.startedAt).toLocaleTimeString("en-US", { hour12: false })}
                      </span>
                    </div>
                  )}
                  {instance.completedAt && (
                    <div className="flex justify-between">
                      <span className="text-text-muted">Completed</span>
                      <span className="text-text-secondary font-mono">
                        {new Date(instance.completedAt).toLocaleTimeString("en-US", { hour12: false })}
                      </span>
                    </div>
                  )}
                  <div className="flex justify-between">
                    <span className="text-text-muted">Order Date</span>
                    <span className="text-text-secondary font-mono">{instance.orderDate}</span>
                  </div>
                </div>
              </div>

              {instance.status === "NOTOK" && instance.error && (
                <div className="rounded-xl border border-red-500/20 bg-red-500/[0.05] p-3.5">
                  <div className="flex items-center gap-1.5 mb-1.5">
                    <AlertTriangle className="h-3.5 w-3.5 text-red-400" />
                    <span className="text-[11px] font-semibold uppercase tracking-wider text-red-400">
                      Error
                    </span>
                  </div>
                  <p className="text-[11px] text-red-300/80 font-mono leading-relaxed">
                    {instance.error}
                  </p>
                </div>
              )}

              {/* Instance actions */}
              <div className="pt-2 border-t border-white/[0.04] space-y-1.5">
                <p className="text-[11px] font-semibold uppercase tracking-wider text-text-muted mb-2">Actions</p>
                {instance.status === "WAITING" && (
                  <>
                    <Button variant="secondary" size="sm" className="w-full" onClick={() => onHold?.(instance.id)}>
                      Hold
                    </Button>
                    <Button variant="destructive" size="sm" className="w-full" onClick={() => onCancel?.(instance.id)}>
                      Cancel
                    </Button>
                  </>
                )}
                {instance.status === "HOLD" && (
                  <Button variant="secondary" size="sm" className="w-full" onClick={() => onRelease?.(instance.id)}>
                    Release
                  </Button>
                )}
                {instance.status === "NOTOK" && (
                  <Button variant="secondary" size="sm" className="w-full" onClick={() => onRerun?.(instance.id)}>
                    Rerun
                  </Button>
                )}
              </div>
            </>
          )}

          {/* Monitoring: no instance available */}
          {!isDesign && !instance && (
            <div className="glass-card rounded-xl p-3.5">
              <p className="text-[11px] text-text-muted text-center">
                No instance for today
              </p>
            </div>
          )}

          {/* Design: editable fields */}
          {isDesign && (
            <>
              <div>
                <FieldLabel>
                  <span className="inline-flex items-center gap-1"><Tag className="h-3 w-3" /> Job Name</span>
                </FieldLabel>
                <input
                  className={cn("properties-input", !nodeData.label.trim() && "!border-red-500/40 !ring-red-500/20")}
                  value={nodeData.label}
                  onChange={(e) => onUpdate(nodeId, { label: e.target.value })}
                />
                {!nodeData.label.trim() && (
                  <p className="text-[10px] text-red-400 mt-1">Name is required</p>
                )}
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <FieldLabel>
                    <span className="inline-flex items-center gap-1"><Clock className="h-3 w-3" /> Timeout (s)</span>
                  </FieldLabel>
                  <input
                    type="number"
                    min="0"
                    className={cn("properties-input", nodeData.timeout !== undefined && nodeData.timeout < 0 && "!border-red-500/40")}
                    placeholder="300"
                    value={nodeData.timeout ?? ""}
                    onChange={(e) => onUpdate(nodeId, { timeout: e.target.value ? Number(e.target.value) : undefined })}
                  />
                  {nodeData.timeout !== undefined && nodeData.timeout < 0 && (
                    <p className="text-[10px] text-red-400 mt-0.5">Must be ≥ 0</p>
                  )}
                </div>
                <div>
                  <FieldLabel>
                    <span className="inline-flex items-center gap-1"><RotateCcw className="h-3 w-3" /> Retries</span>
                  </FieldLabel>
                  <input
                    type="number"
                    min="0"
                    max="10"
                    className={cn("properties-input", nodeData.retries !== undefined && (nodeData.retries < 0 || nodeData.retries > 10) && "!border-red-500/40")}
                    placeholder="3"
                    value={nodeData.retries ?? ""}
                    onChange={(e) => onUpdate(nodeId, { retries: e.target.value ? Number(e.target.value) : undefined })}
                  />
                  {nodeData.retries !== undefined && (nodeData.retries < 0 || nodeData.retries > 10) && (
                    <p className="text-[10px] text-red-400 mt-0.5">Must be 0–10</p>
                  )}
                </div>
              </div>

              <div>
                <FieldLabel>
                  <span className="inline-flex items-center gap-1"><Calendar className="h-3 w-3" /> Schedule (cron)</span>
                </FieldLabel>
                <input
                  className={cn("properties-input", nodeData.schedule && !/^\S+(\s+\S+){4,5}$/.test(nodeData.schedule.trim()) && "!border-amber-500/40")}
                  placeholder="0 8 * * MON-FRI"
                  value={nodeData.schedule ?? ""}
                  onChange={(e) => onUpdate(nodeId, { schedule: e.target.value || undefined })}
                />
                {nodeData.schedule && !/^\S+(\s+\S+){4,5}$/.test(nodeData.schedule.trim()) && (
                  <p className="text-[10px] text-amber-400 mt-1">Expected 5 or 6 fields</p>
                )}
                {nodeData.schedule && /^\S+(\s+\S+){4,5}$/.test(nodeData.schedule.trim()) && (
                  <p className="text-[10px] text-emerald-400/80 mt-1">
                    {describeCron(nodeData.schedule)}
                  </p>
                )}
                {(() => {
                  if (nodeData.schedule) {
                    const err = validateCron(nodeData.schedule);
                    if (err) return <p className="text-[10px] text-red-400 mt-0.5">{err}</p>;
                  }
                  return null;
                })()}
                {/* Preset buttons */}
                <div className="flex flex-wrap gap-1 mt-2">
                  {([
                    ["*/5 * * * *", "5min"],
                    ["0 * * * *", "Hourly"],
                    ["0 8 * * MON-FRI", "Weekdays 8h"],
                    ["0 0 * * *", "Daily 0h"],
                    ["0 6,12,18 * * *", "3x day"],
                  ] as [string, string][]).map(([cron, label]) => (
                    <button
                      key={cron}
                      onClick={() => onUpdate(nodeId, { schedule: cron })}
                      className={cn(
                        "text-[9px] px-1.5 py-0.5 rounded border transition-all",
                        nodeData.schedule === cron
                          ? "border-accent-cyan/40 bg-accent-cyan/10 text-accent-cyan"
                          : "border-white/[0.06] bg-white/[0.02] text-text-muted hover:text-text-secondary hover:border-white/10"
                      )}
                    >
                      {label}
                    </button>
                  ))}
                </div>
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

              {/* Dry Run toggle */}
              <div className="flex items-center justify-between">
                <FieldLabel>
                  <span className="inline-flex items-center gap-1"><Play className="h-3 w-3" /> Dry Run</span>
                </FieldLabel>
                <button
                  onClick={() => onUpdate(nodeId, { dryRun: !nodeData.dryRun })}
                  className={cn(
                    "relative inline-flex h-5 w-9 items-center rounded-full transition-colors",
                    nodeData.dryRun ? "bg-amber-500/60" : "bg-white/10"
                  )}
                >
                  <span
                    className={cn(
                      "inline-block h-3.5 w-3.5 rounded-full bg-white transition-transform",
                      nodeData.dryRun ? "translate-x-4" : "translate-x-0.5"
                    )}
                  />
                </button>
              </div>
              {nodeData.dryRun && (
                <p className="text-[10px] text-amber-400 -mt-3">Jobs will log but not execute real requests</p>
              )}

              {/* HTTP Config (only for HTTP jobs) */}
              {nodeData.jobType === "HTTP" && (
                <div className="space-y-3 rounded-xl border border-sky-500/20 bg-sky-500/[0.03] p-3.5">
                  <div className="flex items-center gap-1.5 mb-1">
                    <Globe className="h-3.5 w-3.5 text-sky-400" />
                    <span className="text-[11px] font-semibold uppercase tracking-wider text-sky-400">
                      HTTP Request
                    </span>
                  </div>

                  <div className="flex gap-2">
                    <div className="w-24">
                      <FieldLabel>Method</FieldLabel>
                      <select
                        className="properties-input text-[11px]"
                        value={nodeData.httpConfig?.method ?? "GET"}
                        onChange={(e) =>
                          onUpdate(nodeId, {
                            httpConfig: {
                              ...(nodeData.httpConfig ?? { url: "", method: "GET" }),
                              method: e.target.value as HttpMethod,
                            },
                          })
                        }
                      >
                        <option value="GET">GET</option>
                        <option value="POST">POST</option>
                        <option value="PUT">PUT</option>
                        <option value="PATCH">PATCH</option>
                        <option value="DELETE">DELETE</option>
                      </select>
                    </div>
                    <div className="flex-1">
                      <FieldLabel>URL</FieldLabel>
                      <input
                        className={cn(
                          "properties-input text-[11px] font-mono",
                          !nodeData.httpConfig?.url && "!border-red-500/40"
                        )}
                        placeholder="https://api.example.com/endpoint"
                        value={nodeData.httpConfig?.url ?? ""}
                        onChange={(e) =>
                          onUpdate(nodeId, {
                            httpConfig: {
                              ...(nodeData.httpConfig ?? { url: "", method: "GET" }),
                              url: e.target.value,
                            },
                          })
                        }
                      />
                    </div>
                  </div>

                  <div>
                    <FieldLabel>Headers (JSON)</FieldLabel>
                    <textarea
                      className="properties-input text-[10px] font-mono !h-16 resize-none"
                      placeholder='{"Authorization": "Bearer ..."}'
                      value={
                        nodeData.httpConfig?.headers
                          ? JSON.stringify(nodeData.httpConfig.headers, null, 2)
                          : ""
                      }
                      onChange={(e) => {
                        let headers: Record<string, string> | undefined;
                        try {
                          headers = e.target.value ? JSON.parse(e.target.value) : undefined;
                        } catch {
                          // invalid JSON — keep raw text until valid
                          return;
                        }
                        onUpdate(nodeId, {
                          httpConfig: {
                            ...(nodeData.httpConfig ?? { url: "", method: "GET" }),
                            headers,
                          },
                        });
                      }}
                    />
                  </div>

                  {nodeData.httpConfig?.method !== "GET" && nodeData.httpConfig?.method !== "DELETE" && (
                    <div>
                      <FieldLabel>Body</FieldLabel>
                      <textarea
                        className="properties-input text-[10px] font-mono !h-20 resize-none"
                        placeholder='{"key": "value"}'
                        value={nodeData.httpConfig?.body ?? ""}
                        onChange={(e) =>
                          onUpdate(nodeId, {
                            httpConfig: {
                              ...(nodeData.httpConfig ?? { url: "", method: "POST" }),
                              body: e.target.value || undefined,
                            },
                          })
                        }
                      />
                    </div>
                  )}
                </div>
              )}

              {/* Custom Variables */}
              <div>
                <FieldLabel>
                  <span className="inline-flex items-center gap-1"><Variable className="h-3 w-3" /> Variables</span>
                </FieldLabel>
                <div className="space-y-1.5">
                  {(nodeData.variables ?? []).map((v: JobNodeVariable, idx: number) => (
                    <div key={idx} className="flex items-center gap-1.5">
                      <input
                        className="properties-input !py-1 flex-1 font-mono text-[11px]"
                        placeholder="KEY"
                        value={v.key}
                        onChange={(e) => {
                          const vars = [...(nodeData.variables ?? [])];
                          vars[idx] = { ...vars[idx], key: e.target.value };
                          onUpdate(nodeId, { variables: vars });
                        }}
                      />
                      <span className="text-text-muted text-[10px]">=</span>
                      <input
                        className="properties-input !py-1 flex-1 font-mono text-[11px]"
                        placeholder="value"
                        value={v.value}
                        onChange={(e) => {
                          const vars = [...(nodeData.variables ?? [])];
                          vars[idx] = { ...vars[idx], value: e.target.value };
                          onUpdate(nodeId, { variables: vars });
                        }}
                      />
                      <button
                        onClick={() => {
                          const vars = (nodeData.variables ?? []).filter((_: JobNodeVariable, i: number) => i !== idx);
                          onUpdate(nodeId, { variables: vars });
                        }}
                        className="p-1 rounded hover:bg-red-500/10 text-text-muted hover:text-red-400 transition-all"
                      >
                        <Trash2 className="h-3 w-3" />
                      </button>
                    </div>
                  ))}
                  <button
                    onClick={() => {
                      const vars = [...(nodeData.variables ?? []), { key: "", value: "" }];
                      onUpdate(nodeId, { variables: vars });
                    }}
                    className="flex items-center gap-1.5 text-[10px] text-accent-cyan/80 hover:text-accent-cyan transition-all mt-1"
                  >
                    <Plus className="h-3 w-3" />
                    Add variable
                  </button>
                </div>
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
