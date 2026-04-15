import { useState } from "react";
import {
  Play,
  Undo2,
  Redo2,
  ZoomIn,
  ZoomOut,
  Maximize2,
  GitBranch,
  Eye,
  Pencil,
  Sparkles,
  ChevronDown,
  Check,
  X,
  Download,
  Upload,
} from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { Button } from "@/components/ui/button";
import type { AppMode } from "@/lib/types";
import { cn } from "@/lib/utils";

interface ToolbarProps {
  mode: AppMode;
  onModeChange: (mode: AppMode) => void;
  onFitView: () => void;
  onZoomIn: () => void;
  onZoomOut: () => void;
  onAutoLayout: () => void;
  onSave?: () => void;
  onRun?: () => void;
  onExport?: () => void;
  onImport?: () => void;
  workflowName?: string;
  folderSelector?: React.ReactNode;
}

function Separator() {
  return <div className="h-5 w-px bg-white/[0.06]" />;
}

export default function Toolbar({
  mode,
  onModeChange,
  onFitView,
  onZoomIn,
  onZoomOut,
  onAutoLayout,
  onSave,
  onRun,
  onExport,
  onImport,
  workflowName = "Production Pipeline",
  folderSelector,
}: ToolbarProps) {
  const [saveToast, setSaveToast] = useState(false);

  const handleSave = () => {
    onSave?.();
    setSaveToast(true);
    setTimeout(() => setSaveToast(false), 4000);
  };

  return (
    <header className="relative flex h-[52px] items-center justify-between border-b border-white/[0.05] bg-bg-surface/70 backdrop-blur-2xl px-4" style={{ boxShadow: "inset 0 -1px 0 rgba(255,255,255,0.02), 0 4px 24px rgba(0,0,0,0.3)" }}>
      {/* Left: folder selector + actions */}
      <div className="flex items-center gap-2">
        {folderSelector ?? (
          <button className="group flex items-center gap-2 rounded-lg border border-white/[0.06] bg-white/[0.02] px-3 py-1.5 transition-all hover:border-white/[0.1] hover:bg-white/[0.04]">
            <Sparkles className="h-3.5 w-3.5 text-accent-cyan" />
            <span className="text-[13px] font-semibold text-text-primary">
              {workflowName}
            </span>
            <ChevronDown className="h-3 w-3 text-text-muted group-hover:text-text-secondary transition-colors" />
          </button>
        )}

        <Separator />

        {mode === "design" && (
          <>
            <div className="flex items-center gap-0.5">
              <Button variant="ghost" size="icon-sm" title="Undo">
                <Undo2 className="h-3.5 w-3.5" />
              </Button>
              <Button variant="ghost" size="icon-sm" title="Redo">
                <Redo2 className="h-3.5 w-3.5" />
              </Button>
            </div>
            <Separator />
            <Button
              variant="secondary"
              size="sm"
              onClick={onAutoLayout}
              title="Auto-layout (dagre)"
            >
              Auto Layout
            </Button>
          </>
        )}
      </div>

      {/* Center: Mode toggle */}
      <div className="absolute left-1/2 top-1/2 -translate-x-1/2 -translate-y-1/2">
        <div className="flex items-center rounded-lg bg-white/[0.03] p-0.5 ring-1 ring-white/[0.06]">
          <button
            onClick={() => onModeChange("design")}
            className={cn(
              "flex items-center gap-1.5 rounded-md px-3 py-1.5 text-[12px] font-semibold transition-all duration-200",
              mode === "design"
                ? "bg-white/[0.08] text-text-primary shadow-sm"
                : "text-text-muted hover:text-text-secondary"
            )}
          >
            <Pencil className="h-3 w-3" />
            Design
          </button>
          <button
            onClick={() => onModeChange("monitoring")}
            className={cn(
              "flex items-center gap-1.5 rounded-md px-3 py-1.5 text-[12px] font-semibold transition-all duration-200",
              mode === "monitoring"
                ? "bg-cyan-500/10 text-cyan-400 shadow-sm ring-1 ring-cyan-500/20"
                : "text-text-muted hover:text-text-secondary"
            )}
          >
            <Eye className="h-3 w-3" />
            Monitoring
          </button>
        </div>
      </div>

      {/* Right: actions */}
      <div className="flex items-center gap-1.5">
        <div className="flex items-center gap-0.5">
          <Button variant="ghost" size="icon-sm" onClick={onZoomOut} title="Zoom Out">
            <ZoomOut className="h-3.5 w-3.5" />
          </Button>
          <Button variant="ghost" size="icon-sm" onClick={onZoomIn} title="Zoom In">
            <ZoomIn className="h-3.5 w-3.5" />
          </Button>
          <Button variant="ghost" size="icon-sm" onClick={onFitView} title="Fit View">
            <Maximize2 className="h-3.5 w-3.5" />
          </Button>
        </div>

        <Separator />

        {mode === "design" ? (
          <>
            <Button variant="ghost" size="icon-sm" onClick={onImport} title="Import JSON">
              <Upload className="h-3.5 w-3.5" />
            </Button>
            <Button variant="ghost" size="icon-sm" onClick={onExport} title="Export JSON">
              <Download className="h-3.5 w-3.5" />
            </Button>
            <Separator />
            <Button variant="secondary" size="sm" onClick={handleSave} className="gap-1.5">
              <GitBranch className="h-3.5 w-3.5" />
              Save
            </Button>
            <Button size="sm" className="gap-1.5" onClick={onRun}>
              <Play className="h-3.5 w-3.5 fill-current" />
              Run Now
            </Button>
          </>
        ) : (
          <Button variant="secondary" size="sm" className="gap-1.5" onClick={onExport}>
            <Download className="h-3.5 w-3.5" />
            Export Log
          </Button>
        )}
      </div>

      {/* Save toast */}
      <AnimatePresence>
        {saveToast && (
          <motion.div
            initial={{ opacity: 0, y: -10 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -10 }}
            className="absolute left-1/2 top-full mt-3 -translate-x-1/2 z-50"
          >
            <div className="flex items-center gap-2.5 rounded-xl bg-emerald-500/10 px-4 py-2.5 ring-1 ring-emerald-500/20 backdrop-blur-xl shadow-2xl">
              <div className="flex h-6 w-6 items-center justify-center rounded-full bg-emerald-500/20">
                <Check className="h-3.5 w-3.5 text-emerald-400" />
              </div>
              <div>
                <p className="text-[12px] font-semibold text-emerald-400">
                  Workflow salvo e PR aberta no GitHub
                </p>
                <p className="text-[10px] text-emerald-400/60">
                  production-pipeline.yaml → main
                </p>
              </div>
              <button
                onClick={() => setSaveToast(false)}
                className="ml-2 text-emerald-400/40 hover:text-emerald-400 transition-colors"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </header>
  );
}
