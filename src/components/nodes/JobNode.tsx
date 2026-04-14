import { memo } from "react";
import { Handle, Position, type NodeProps, type Node } from "@xyflow/react";
import { Badge } from "@/components/ui/badge";
import { JOB_TYPES, STATUS_MAP, type JobNodeData } from "@/lib/job-config";
import type { AppMode } from "@/lib/types";
import { cn } from "@/lib/utils";

type JobNode = Node<JobNodeData, "job">;

function JobNodeComponent({ data, selected, id }: NodeProps<JobNode>) {
  const typeConfig = JOB_TYPES[data.jobType];
  const statusConfig = STATUS_MAP[data.status];
  const Icon = typeConfig.icon;
  const mode = (data.mode ?? "design") as AppMode;
  const isMonitoring = mode === "monitoring";

  return (
    <div
      className={cn(
        "regente-node group relative w-[240px] rounded-[14px] border border-white/[0.07] bg-[#1f2937]/80 backdrop-blur-lg overflow-hidden shadow-[inset_0_2px_8px_rgba(0,0,0,0.18)]",
        (selected || (isMonitoring && data.status === "RUNNING")) && "node-glow-premium",
        isMonitoring && data.status === "RUNNING" && "animate-pulse-scale",
        isMonitoring && "pointer-events-auto"
      )}
      data-label={data.label}
      data-id={id}
    >
      {/* Accent top gradient bar */}
      <div
        className="absolute inset-x-0 top-0 h-[3px]"
        style={{
          background: `linear-gradient(90deg, transparent 5%, ${typeConfig.accentColor}88 30%, ${typeConfig.accentColor} 50%, ${typeConfig.accentColor}88 70%, transparent 95%)`,
        }}
      />

      {/* Noise/grain overlay for texture */}
      <div className="absolute inset-0 opacity-[0.015] pointer-events-none"
        style={{ backgroundImage: 'url("data:image/svg+xml,%3Csvg viewBox=\'0 0 256 256\' xmlns=\'http://www.w3.org/2000/svg\'%3E%3Cfilter id=\'n\'%3E%3CfeTurbulence type=\'fractalNoise\' baseFrequency=\'0.9\' numOctaves=\'4\' stitchTiles=\'stitch\'/%3E%3C/filter%3E%3Crect width=\'100%25\' height=\'100%25\' filter=\'url(%23n)\'/%3E%3C/svg%3E")', backgroundSize: '128px 128px' }}
      />

      {/* Gradient overlay */}
      <div
        className={cn(
          "absolute inset-0 bg-gradient-to-br opacity-30 pointer-events-none",
          typeConfig.gradient
        )}
      />

      {/* Inner top highlight for depth */}
      <div className="absolute inset-x-0 top-[3px] h-px bg-gradient-to-r from-transparent via-white/[0.06] to-transparent pointer-events-none" />

      {/* Handles — vertical (top/bottom) */}
      <Handle type="target" position={Position.Top} className="!-top-[5px]" />
      <Handle type="source" position={Position.Bottom} className="!-bottom-[5px]" />

      {/* Content */}
      <div className="relative p-3.5">
        <div className="flex items-start gap-3 mb-3">
          <div className="relative">
            <div
              className={cn(
                "flex h-10 w-10 shrink-0 items-center justify-center rounded-xl transition-all duration-300 group-hover:scale-110 group-hover:shadow-lg",
                typeConfig.iconBg
              )}
            >
              <Icon className="h-5 w-5" strokeWidth={1.8} />
            </div>
            {isMonitoring && data.status === "RUNNING" && (
              <div className="absolute -inset-1 rounded-xl animate-running-ring pointer-events-none" />
            )}
          </div>
          <div className="min-w-0 flex-1 pt-0.5">
            <p className="truncate text-[13px] font-semibold text-text-primary leading-tight">
              {data.label}
            </p>
            <p className="text-[11px] text-text-muted mt-0.5 font-medium">
              {typeConfig.label}
            </p>
          </div>
        </div>

        <div className="h-px bg-gradient-to-r from-transparent via-white/[0.06] to-transparent mb-2.5" />

        <div className="flex items-center justify-between">
          <Badge variant={statusConfig.variant}>
            <span
              className={cn(
                "inline-block h-1.5 w-1.5 rounded-full",
                statusConfig.dotColor,
                data.status === "RUNNING" && "animate-pulse-dot"
              )}
            />
            {statusConfig.label}
          </Badge>
          {data.lastRun && (
            <span className="text-[10px] text-text-muted font-medium">
              {data.lastRun}
            </span>
          )}
        </div>
      </div>
    </div>
  );
}

export default memo(JobNodeComponent);
