import {
  Play,
  StopCircle,
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
  Download,
  Upload,
  Search,
  ShieldCheck,
  History,
  LayoutTemplate,
  Calendar,
  BarChart3,
  ScrollText,
  Bell,
  Settings2,
} from "lucide-react";
import { useToast } from "@/components/ToastStack";
import UserMenu from "@/components/UserMenu";
import CollaborationBar, { type PresenceUser } from "@/components/CollaborationBar";
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
  onUndo?: () => void;
  onRedo?: () => void;
  canUndo?: boolean;
  canRedo?: boolean;
  onValidate?: () => void;
  onVersionHistory?: () => void;
  onTemplates?: () => void;
  onScheduler?: () => void;
  onMetrics?: () => void;
  onAudit?: () => void;
  onAlerts?: () => void;
  onNotificationSettings?: () => void;
  alertCount?: number;
  engineRunning?: boolean;
  presenceUsers?: PresenceUser[];
  searchTerm?: string;
  onSearchChange?: (term: string) => void;
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
  onUndo,
  onRedo,
  canUndo = false,
  canRedo = false,
  onValidate,
  onVersionHistory,
  onTemplates,
  onScheduler,
  onMetrics,
  onAudit,
  onAlerts,
  onNotificationSettings,
  alertCount = 0,
  engineRunning = false,
  presenceUsers = [],
  searchTerm = "",
  onSearchChange,
  workflowName = "Production Pipeline",
  folderSelector,
}: ToolbarProps) {
  const { addToast } = useToast();

  const handleSave = () => {
    onSave?.();
    addToast({ type: "success", title: "Workflow saved", description: "Changes persisted successfully" });
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

        {/* Search */}
        <div className="relative flex items-center">
          <Search className="absolute left-2 h-3.5 w-3.5 text-text-muted pointer-events-none" />
          <input
            type="text"
            value={searchTerm}
            onChange={(e) => onSearchChange?.(e.target.value)}
            placeholder="Search nodes…"
            className="h-7 w-36 rounded-md border border-white/[0.06] bg-white/[0.03] pl-7 pr-2 text-[12px] text-text-primary placeholder:text-text-muted/50 outline-none focus:border-accent-cyan/40 focus:ring-1 focus:ring-accent-cyan/20 transition-all"
          />
        </div>

        <Separator />

        {mode === "design" && (
          <>
            <div className="flex items-center gap-0.5">
              <Button variant="ghost" size="icon-sm" title="Undo (Ctrl+Z)" onClick={onUndo} disabled={!canUndo} className={cn(!canUndo && "opacity-30")}>
                <Undo2 className="h-3.5 w-3.5" />
              </Button>
              <Button variant="ghost" size="icon-sm" title="Redo (Ctrl+Shift+Z)" onClick={onRedo} disabled={!canRedo} className={cn(!canRedo && "opacity-30")}>
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
            <Separator />
            <Button variant="ghost" size="icon-sm" onClick={onValidate} title="Validate DAG">
              <ShieldCheck className="h-3.5 w-3.5" />
            </Button>
            <Button variant="ghost" size="icon-sm" onClick={onVersionHistory} title="Version History">
              <History className="h-3.5 w-3.5" />
            </Button>
            <Button variant="ghost" size="icon-sm" onClick={onTemplates} title="Templates">
              <LayoutTemplate className="h-3.5 w-3.5" />
            </Button>
            <Button variant="ghost" size="icon-sm" onClick={onScheduler} title="Scheduler">
              <Calendar className="h-3.5 w-3.5" />
            </Button>
            <Separator />
            <Button variant="ghost" size="icon-sm" onClick={onMetrics} title="Metrics">
              <BarChart3 className="h-3.5 w-3.5" />
            </Button>
            <Button variant="ghost" size="icon-sm" onClick={onAudit} title="Audit Trail">
              <ScrollText className="h-3.5 w-3.5" />
            </Button>
            <Button variant="ghost" size="icon-sm" onClick={onAlerts} title="Alerts" className="relative">
              <Bell className="h-3.5 w-3.5" />
              {alertCount > 0 && (
                <span className="absolute -top-0.5 -right-0.5 flex h-3.5 min-w-[14px] items-center justify-center rounded-full bg-red-500 px-0.5 text-[8px] font-bold text-white">
                  {alertCount}
                </span>
              )}
            </Button>
            <Button variant="ghost" size="icon-sm" onClick={onNotificationSettings} title="Notification Channels">
              <Settings2 className="h-3.5 w-3.5" />
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
              {engineRunning ? (
                <><StopCircle className="h-3.5 w-3.5" /> Stop</>
              ) : (
                <><Play className="h-3.5 w-3.5 fill-current" /> Run Now</>
              )}
            </Button>
          </>
        ) : (
          <Button variant="secondary" size="sm" className="gap-1.5" onClick={onExport}>
            <Download className="h-3.5 w-3.5" />
            Export Log
          </Button>
        )}

        <Separator />
        <CollaborationBar users={presenceUsers} />
        <UserMenu />
      </div>

    </header>
  );
}
