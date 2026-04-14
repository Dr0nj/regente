import { type HTMLAttributes } from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider transition-all duration-200",
  {
    variants: {
      variant: {
        success: "bg-emerald-500/10 text-emerald-400 ring-1 ring-emerald-500/20",
        running: "bg-cyan-500/10 text-cyan-400 ring-1 ring-cyan-500/20",
        failed: "bg-red-500/10 text-red-400 ring-1 ring-red-500/20",
        waiting: "bg-amber-500/10 text-amber-400 ring-1 ring-amber-500/20",
        inactive: "bg-slate-500/10 text-slate-500 ring-1 ring-slate-500/15",
        default: "bg-white/[0.04] text-slate-400 ring-1 ring-white/[0.06]",
      },
    },
    defaultVariants: { variant: "default" },
  }
);

interface BadgeProps
  extends HTMLAttributes<HTMLSpanElement>,
    VariantProps<typeof badgeVariants> {}

export function Badge({ className, variant, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ variant, className }))} {...props} />;
}
