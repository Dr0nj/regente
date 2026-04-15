/**
 * MetricsDashboard — Phase 8
 *
 * Floating panel showing workflow execution metrics:
 * - Global stats (total runs, success rate, avg duration)
 * - Per-workflow summaries
 * - Duration trend mini-chart (sparkline)
 * - Hourly heatmap
 */

import { useState } from "react";
import { X, BarChart3, TrendingUp, Clock, CheckCircle2, XCircle, Activity } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { Button } from "@/components/ui/button";
import {
  getGlobalMetrics,
  getWorkflowSummaries,
  getDurationTrend,
  getHourlyHeatmap,
  clearAllMetrics,
  type WorkflowSummary,
} from "@/lib/metrics";
import { cn } from "@/lib/utils";

interface MetricsDashboardProps {
  onClose: () => void;
}

function StatCard({ label, value, sub, icon: Icon, color }: {
  label: string;
  value: string;
  sub?: string;
  icon: typeof BarChart3;
  color: string;
}) {
  return (
    <div className="rounded-lg border border-white/[0.06] bg-white/[0.02] p-3">
      <div className="flex items-center gap-2 mb-1">
        <Icon className={cn("h-3.5 w-3.5", color)} />
        <span className="text-[10px] font-medium uppercase tracking-wider text-text-muted">{label}</span>
      </div>
      <p className="text-[18px] font-bold text-text-primary">{value}</p>
      {sub && <p className="text-[10px] text-text-muted mt-0.5">{sub}</p>}
    </div>
  );
}

function Sparkline({ data, height = 32 }: { data: [number, number][]; height?: number }) {
  if (data.length < 2) return <span className="text-[10px] text-text-muted">No data</span>;

  const values = data.map((d) => d[1]);
  const max = Math.max(...values);
  const min = Math.min(...values);
  const range = max - min || 1;
  const w = 200;

  const points = values
    .map((v, i) => {
      const x = (i / (values.length - 1)) * w;
      const y = height - ((v - min) / range) * (height - 4) - 2;
      return `${x},${y}`;
    })
    .join(" ");

  return (
    <svg width={w} height={height} className="overflow-visible">
      <polyline
        points={points}
        fill="none"
        stroke="rgb(34, 211, 238)"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      {/* Last point dot */}
      {values.length > 0 && (() => {
        const lastX = w;
        const lastY = height - ((values[values.length - 1] - min) / range) * (height - 4) - 2;
        return <circle cx={lastX} cy={lastY} r="2.5" fill="rgb(34, 211, 238)" />;
      })()}
    </svg>
  );
}

function HeatmapRow({ data }: { data: number[] }) {
  const max = Math.max(...data, 1);
  return (
    <div className="flex gap-0.5">
      {data.map((v, i) => (
        <div
          key={i}
          title={`${String(i).padStart(2, "0")}:00 — ${v} runs`}
          className="h-4 w-3 rounded-sm transition-all"
          style={{
            backgroundColor: v === 0
              ? "rgba(255,255,255,0.03)"
              : `rgba(34, 211, 238, ${0.15 + (v / max) * 0.7})`,
          }}
        />
      ))}
    </div>
  );
}

function WorkflowRow({ summary }: { summary: WorkflowSummary }) {
  const trend = getDurationTrend(summary.workflowId);
  const rate = (summary.successRate * 100).toFixed(0);

  return (
    <div className="rounded-lg border border-white/[0.06] bg-white/[0.02] p-3">
      <div className="flex items-center justify-between mb-2">
        <span className="text-[12px] font-semibold text-text-primary truncate">
          {summary.workflowName}
        </span>
        <span
          className={cn(
            "inline-flex items-center gap-1 rounded-full px-1.5 py-0.5 text-[9px] font-medium ring-1",
            summary.lastStatus === "SUCCESS"
              ? "text-emerald-400 bg-emerald-500/10 ring-emerald-500/20"
              : "text-red-400 bg-red-500/10 ring-red-500/20"
          )}
        >
          {summary.lastStatus === "SUCCESS" ? (
            <CheckCircle2 className="h-2.5 w-2.5" />
          ) : (
            <XCircle className="h-2.5 w-2.5" />
          )}
          {summary.lastStatus}
        </span>
      </div>

      <div className="grid grid-cols-3 gap-2 text-[10px] mb-2">
        <div>
          <span className="text-text-muted">Runs</span>
          <p className="text-text-primary font-semibold">{summary.totalRuns}</p>
        </div>
        <div>
          <span className="text-text-muted">Success</span>
          <p className={cn("font-semibold", Number(rate) >= 90 ? "text-emerald-400" : Number(rate) >= 70 ? "text-amber-400" : "text-red-400")}>
            {rate}%
          </p>
        </div>
        <div>
          <span className="text-text-muted">Avg</span>
          <p className="text-text-primary font-semibold">
            {(summary.avgDurationMs / 1000).toFixed(1)}s
          </p>
        </div>
      </div>

      <div className="flex items-center gap-2">
        <TrendingUp className="h-3 w-3 text-text-muted" />
        <Sparkline data={trend} />
      </div>
    </div>
  );
}

export default function MetricsDashboard({ onClose }: MetricsDashboardProps) {
  const [, setRefresh] = useState(0);
  const global = getGlobalMetrics();
  const summaries = getWorkflowSummaries();
  const heatmap = getHourlyHeatmap();

  const handleClear = () => {
    clearAllMetrics();
    setRefresh((r) => r + 1);
  };

  return (
    <AnimatePresence>
      <motion.div
        initial={{ opacity: 0, x: 30 }}
        animate={{ opacity: 1, x: 0 }}
        exit={{ opacity: 0, x: 30 }}
        className="absolute right-4 top-16 z-50 w-[420px] rounded-xl border border-white/[0.06] bg-bg-surface/95 backdrop-blur-xl shadow-2xl"
      >
        {/* Header */}
        <div className="flex items-center justify-between border-b border-white/[0.04] px-4 py-3">
          <div className="flex items-center gap-2">
            <BarChart3 className="h-4 w-4 text-accent-cyan" />
            <span className="text-[13px] font-semibold text-text-primary">
              Metrics
            </span>
          </div>
          <div className="flex items-center gap-1">
            <Button variant="ghost" size="icon-sm" onClick={handleClear} title="Clear metrics">
              <XCircle className="h-3.5 w-3.5 text-text-muted" />
            </Button>
            <Button variant="ghost" size="icon-sm" onClick={onClose}>
              <X className="h-4 w-4" />
            </Button>
          </div>
        </div>

        <div className="max-h-[500px] overflow-y-auto p-4 space-y-4">
          {/* Global stats */}
          <div className="grid grid-cols-3 gap-2">
            <StatCard
              label="Total Runs"
              value={String(global.totalRuns)}
              icon={Activity}
              color="text-cyan-400"
            />
            <StatCard
              label="Success Rate"
              value={global.totalRuns > 0 ? `${(global.successRate * 100).toFixed(0)}%` : "—"}
              sub={`${global.successCount}/${global.totalRuns}`}
              icon={CheckCircle2}
              color={global.successRate >= 0.9 ? "text-emerald-400" : global.successRate >= 0.7 ? "text-amber-400" : "text-red-400"}
            />
            <StatCard
              label="Avg Duration"
              value={global.totalRuns > 0 ? `${(global.avgDurationMs / 1000).toFixed(1)}s` : "—"}
              sub={global.totalRuns > 0 ? `p95: ${(global.p95DurationMs / 1000).toFixed(1)}s` : undefined}
              icon={Clock}
              color="text-purple-400"
            />
          </div>

          {/* Hourly heatmap */}
          {global.totalRuns > 0 && (
            <div>
              <div className="flex items-center gap-2 mb-2">
                <span className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
                  Hourly Activity
                </span>
              </div>
              <HeatmapRow data={heatmap} />
              <div className="flex justify-between mt-1 text-[8px] text-text-muted/50">
                <span>00:00</span>
                <span>06:00</span>
                <span>12:00</span>
                <span>18:00</span>
                <span>23:00</span>
              </div>
            </div>
          )}

          {/* Per-workflow summaries */}
          {summaries.length > 0 ? (
            <div className="space-y-2">
              <span className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
                Workflows
              </span>
              {summaries.map((s) => (
                <WorkflowRow key={s.workflowId} summary={s} />
              ))}
            </div>
          ) : (
            <div className="py-6 text-center">
              <BarChart3 className="mx-auto mb-2 h-8 w-8 text-text-muted/30" />
              <p className="text-[12px] text-text-muted">No execution data yet</p>
              <p className="text-[11px] text-text-muted/60 mt-1">Run a workflow to see metrics</p>
            </div>
          )}
        </div>
      </motion.div>
    </AnimatePresence>
  );
}
