import { useState, useMemo } from "react";
import { LayoutTemplate, X, Layers, GitBranch, Zap, Box } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { cn } from "@/lib/utils";
import { getTemplates, TEMPLATE_CATEGORIES, type WorkflowTemplate } from "@/lib/workflow-templates";

interface TemplateGalleryProps {
  onApply: (template: WorkflowTemplate) => void;
  onClose: () => void;
}

const CATEGORY_ICONS: Record<string, typeof Layers> = {
  data: Layers,
  ml: Zap,
  devops: GitBranch,
  general: Box,
};

export default function TemplateGallery({ onApply, onClose }: TemplateGalleryProps) {
  const templates = useMemo(() => getTemplates(), []);
  const [selectedCategory, setSelectedCategory] = useState<string>("all");

  const filtered = selectedCategory === "all"
    ? templates
    : templates.filter((t) => t.category === selectedCategory);

  return (
    <AnimatePresence>
      <motion.div
        initial={{ opacity: 0, scale: 0.95 }}
        animate={{ opacity: 1, scale: 1 }}
        exit={{ opacity: 0, scale: 0.95 }}
        transition={{ type: "spring", damping: 25, stiffness: 300 }}
        className="absolute inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
        onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
      >
        <div className="w-[560px] max-h-[70vh] rounded-2xl border border-white/[0.06] bg-bg-surface/98 backdrop-blur-xl shadow-2xl overflow-hidden"
          style={{ boxShadow: "0 16px 64px rgba(0,0,0,0.6), inset 0 1px 0 rgba(255,255,255,0.04)" }}
        >
          {/* Header */}
          <div className="flex items-center justify-between px-6 py-4 border-b border-white/[0.04]">
            <div className="flex items-center gap-3">
              <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-accent-cyan/10 ring-1 ring-accent-cyan/20">
                <LayoutTemplate className="h-4.5 w-4.5 text-accent-cyan" />
              </div>
              <div>
                <p className="text-[15px] font-semibold text-text-primary">Workflow Templates</p>
                <p className="text-[11px] text-text-muted">Pre-built patterns to jumpstart your workflow</p>
              </div>
            </div>
            <button
              onClick={onClose}
              className="p-2 rounded-lg hover:bg-white/[0.06] text-text-muted hover:text-text-secondary transition-all"
            >
              <X className="h-4 w-4" />
            </button>
          </div>

          {/* Category tabs */}
          <div className="flex items-center gap-2 px-6 py-3 border-b border-white/[0.04]">
            <button
              onClick={() => setSelectedCategory("all")}
              className={cn(
                "text-[11px] px-3 py-1 rounded-md font-medium transition-all",
                selectedCategory === "all"
                  ? "bg-white/[0.08] text-text-primary ring-1 ring-white/[0.1]"
                  : "text-text-muted hover:text-text-secondary hover:bg-white/[0.04]"
              )}
            >
              All
            </button>
            {TEMPLATE_CATEGORIES.map((cat) => {
              const CatIcon = CATEGORY_ICONS[cat.id] ?? Box;
              return (
                <button
                  key={cat.id}
                  onClick={() => setSelectedCategory(cat.id)}
                  className={cn(
                    "flex items-center gap-1.5 text-[11px] px-3 py-1 rounded-md font-medium transition-all",
                    selectedCategory === cat.id
                      ? "bg-white/[0.08] text-text-primary ring-1 ring-white/[0.1]"
                      : "text-text-muted hover:text-text-secondary hover:bg-white/[0.04]"
                  )}
                >
                  <CatIcon className={cn("h-3 w-3", cat.color)} />
                  {cat.label}
                </button>
              );
            })}
          </div>

          {/* Template grid */}
          <div className="overflow-y-auto p-6 grid grid-cols-2 gap-3" style={{ maxHeight: "calc(70vh - 140px)" }}>
            {filtered.map((tmpl) => {
              const catColor = TEMPLATE_CATEGORIES.find((c) => c.id === tmpl.category)?.color ?? "text-text-muted";
              return (
                <motion.button
                  key={tmpl.id}
                  whileHover={{ scale: 1.02 }}
                  whileTap={{ scale: 0.98 }}
                  onClick={() => onApply(tmpl)}
                  className="flex flex-col items-start text-left p-4 rounded-xl border border-white/[0.06] bg-white/[0.02] hover:bg-white/[0.04] hover:border-white/[0.1] transition-all group"
                >
                  <div className="flex items-center gap-2 mb-2">
                    <LayoutTemplate className={cn("h-4 w-4", catColor)} />
                    <span className="text-[12px] font-semibold text-text-primary group-hover:text-accent-cyan transition-colors">
                      {tmpl.name}
                    </span>
                  </div>
                  <p className="text-[10px] text-text-muted leading-relaxed mb-3">{tmpl.description}</p>
                  <div className="flex items-center gap-3 text-[10px] text-text-muted/60">
                    <span>{tmpl.nodes.length} nodes</span>
                    <span>{tmpl.edges.length} edges</span>
                  </div>
                </motion.button>
              );
            })}
          </div>
        </div>
      </motion.div>
    </AnimatePresence>
  );
}
