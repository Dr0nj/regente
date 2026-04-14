import { type DragEvent } from "react";
import {
  Activity,
  CheckCircle2,
  XCircle,
  Loader2,
  Clock,
  GripVertical,
  TrendingUp,
  AlertCircle,
} from "lucide-react";
import { motion } from "framer-motion";
import { JOB_TYPES, type JobType } from "@/lib/job-config";
import type { AppMode } from "@/lib/types";
import { cn } from "@/lib/utils";
import MonitoringTree, { type TreeTeam } from "./MonitoringTree";

export interface WorkflowStats {
  total: number;
  running: number;
  success: number;
  failed: number;
  waiting: number;
}

interface SidebarProps {
  stats: WorkflowStats;
  mode: AppMode;
  teams?: TreeTeam[];
  selectedJobId?: string | null;
  onJobFocus?: (jobId: string) => void;
}

const STAT_CARDS_DESIGN = [
  { key: "total" as const, label: "Total Jobs", icon: Activity, color: "text-slate-200", iconColor: "text-cyan-400", bg: "bg-cyan-500/[0.06]", ring: "ring-cyan-500/10" },
  { key: "running" as const, label: "Running", icon: Loader2, color: "text-cyan-400", iconColor: "text-cyan-400", bg: "bg-cyan-500/[0.06]", ring: "ring-cyan-500/10", spin: true },
  { key: "success" as const, label: "Succeeded", icon: CheckCircle2, color: "text-emerald-400", iconColor: "text-emerald-400", bg: "bg-emerald-500/[0.06]", ring: "ring-emerald-500/10" },
  { key: "failed" as const, label: "Failed", icon: XCircle, color: "text-red-400", iconColor: "text-red-400", bg: "bg-red-500/[0.06]", ring: "ring-red-500/10" },
];

function onDragStart(event: DragEvent, jobType: JobType) {
  event.dataTransfer.setData("application/regente-job-type", jobType);
  event.dataTransfer.effectAllowed = "move";
}

export default function Sidebar({ stats, mode, teams = [], selectedJobId, onJobFocus }: SidebarProps) {
  const isDesign = mode === "design";

  return (
    <aside className="flex w-[270px] shrink-0 flex-col border-r border-white/[0.05] bg-bg-surface/90 backdrop-blur-2xl" style={{ boxShadow: "inset -1px 0 0 rgba(255,255,255,0.02), 4px 0 24px rgba(0,0,0,0.3)" }}>
      {/* Brand */}
      <div className="flex items-center gap-3 border-b border-white/[0.04] px-5 py-4">
        <div className="relative">
          <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-gradient-to-br from-cyan-500 to-blue-600 shadow-lg shadow-cyan-900/30">
            <Activity className="h-4.5 w-4.5 text-white" />
          </div>
          <div
            className={cn(
              "absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full ring-2 ring-bg-surface",
              isDesign ? "bg-amber-400" : "bg-emerald-400 animate-pulse-dot"
            )}
          />
        </div>
        <div>
          <h1 className="text-[14px] font-bold text-text-primary tracking-tight">
            Regente Lite
          </h1>
          <p className={cn(
            "text-[10px] font-semibold uppercase tracking-[0.15em]",
            isDesign ? "text-text-muted" : "text-cyan-400/80"
          )}>
            {isDesign ? "Design Mode" : "Monitoring"}
          </p>
        </div>
      </div>

      {/* Stats */}
      <div className="px-4 pt-4 pb-1">
        <div className="flex items-center gap-1.5 mb-3">
          <TrendingUp className="h-3 w-3 text-text-muted" />
          <p className="text-[10px] font-bold uppercase tracking-[0.15em] text-text-muted">
            {isDesign ? "Overview" : "Live Status"}
          </p>
          {!isDesign && (
            <span className="ml-auto h-1.5 w-1.5 rounded-full bg-emerald-400 animate-pulse-dot" />
          )}
        </div>
        <div className="grid grid-cols-2 gap-2">
          {STAT_CARDS_DESIGN.map(({ key, label, icon: Icon, color, iconColor, bg, ring, spin }, i) => (
            <motion.div
              key={key}
              initial={{ opacity: 0, y: 8 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: i * 0.05 }}
              className={cn(
                "rounded-xl p-3 ring-1 transition-all duration-200 hover:bg-white/[0.02]",
                bg, ring,
                !isDesign && key === "failed" && stats.failed > 0 && "ring-red-500/30 animate-glow-breathe"
              )}
            >
              <div className="flex items-center gap-1.5 mb-1.5">
                <Icon className={cn("h-3.5 w-3.5", iconColor, spin && stats[key] > 0 && "animate-spin")} />
                <span className={cn("text-xl font-bold tabular-nums leading-none", color)}>
                  {stats[key]}
                </span>
              </div>
              <p className="text-[10px] text-text-muted font-medium">{label}</p>
            </motion.div>
          ))}
        </div>
      </div>


      {/* Monitoring: árvore de times e workflows (sempre visível em monitoring) */}
      {!isDesign && (
        <>
          <div className="px-4 pt-3">
            <div className="glass-card rounded-xl p-3 mb-3">
              <div className="flex items-center gap-1.5 mb-2">
                <AlertCircle className="h-3 w-3 text-cyan-400" />
                <p className="text-[10px] font-bold uppercase tracking-[0.15em] text-text-muted">
                  Active Today
                </p>
              </div>
              <div className="space-y-1.5">
                {stats.running > 0 && (
                  <div className="flex items-center gap-2 text-[11px]">
                    <span className="h-1.5 w-1.5 rounded-full bg-cyan-400 animate-pulse-dot" />
                    <span className="text-text-secondary">{stats.running} job(s) running</span>
                  </div>
                )}
                {stats.failed > 0 && (
                  <div className="flex items-center gap-2 text-[11px]">
                    <span className="h-1.5 w-1.5 rounded-full bg-red-400" />
                    <span className="text-red-400/80">{stats.failed} job(s) failed</span>
                  </div>
                )}
                {stats.waiting > 0 && (
                  <div className="flex items-center gap-2 text-[11px]">
                    <span className="h-1.5 w-1.5 rounded-full bg-amber-400" />
                    <span className="text-text-secondary">{stats.waiting} job(s) waiting</span>
                  </div>
                )}
                {stats.running === 0 && stats.failed === 0 && stats.waiting === 0 && (
                  <p className="text-[11px] text-text-muted">All jobs idle</p>
                )}
              </div>
            </div>
            <div className="glass-card rounded-xl p-3">
              <MonitoringTree
                teams={teams}
                selectedJobId={selectedJobId}
                onSelectJob={onJobFocus}
              />
            </div>
          </div>
        </>
      )}

      {/* Divider */}
      <div className="mx-4 my-3 h-px bg-white/[0.04]" />

      {/* Job Types (design only) */}
      {isDesign && (
        <div className="flex-1 overflow-y-auto px-4 pb-4">
          <p className="flex items-center gap-1.5 mb-2.5 text-[10px] font-bold uppercase tracking-[0.15em] text-text-muted">
            <span>Job Types</span>
            <span className="ml-auto rounded-full bg-white/[0.04] px-1.5 py-0.5 text-[9px] tabular-nums ring-1 ring-white/[0.06]">
              {Object.keys(JOB_TYPES).length}
            </span>
          </p>
          <div className="space-y-1">
            {(Object.entries(JOB_TYPES) as [JobType, (typeof JOB_TYPES)[JobType]][]).map(
              ([type, config], i) => {
                const Icon = config.icon;
                return (
                  <motion.div
                    key={type}
                    initial={{ opacity: 0, x: -12 }}
                    animate={{ opacity: 1, x: 0 }}
                    transition={{ delay: 0.15 + i * 0.04 }}
                    draggable
                    onDragStart={(e) => onDragStart(e as unknown as DragEvent, type)}
                    className="group flex cursor-grab items-center gap-2.5 rounded-xl border border-transparent px-2.5 py-2 transition-all duration-200 hover:border-white/[0.06] hover:bg-white/[0.03] active:cursor-grabbing active:scale-[0.98]"
                  >
                    <GripVertical className="h-3 w-3 text-text-muted/50 opacity-0 group-hover:opacity-100 transition-opacity" />
                    <div className={cn("flex h-8 w-8 items-center justify-center rounded-lg transition-transform duration-200 group-hover:scale-110", config.iconBg)}>
                      <Icon className="h-4 w-4" strokeWidth={1.8} />
                    </div>
                    <div className="min-w-0">
                      <p className="text-[12px] font-semibold text-text-primary truncate">{config.label}</p>
                      <p className="text-[10px] text-text-muted">{config.description}</p>
                    </div>
                  </motion.div>
                );
              }
            )}
          </div>
        </div>
      )}

      {/* Footer */}
      <div className="border-t border-white/[0.04] px-4 py-3">
        <div className="flex items-center gap-2">
          <Clock className="h-3 w-3 text-text-muted" />
          <span className="text-[10px] text-text-muted">
            {isDesign ? "Draft · unsaved" : "Synced just now"}
          </span>
          {!isDesign && (
            <span className="ml-auto h-1.5 w-1.5 rounded-full bg-emerald-400 animate-pulse-dot" />
          )}
        </div>
      </div>
    </aside>
  );
}
