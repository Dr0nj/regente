import { useEffect, useRef } from "react";
import { Trash2, Copy, Unplug, Crosshair } from "lucide-react";
import { motion } from "framer-motion";
import { cn } from "@/lib/utils";

interface NodeContextMenuProps {
  x: number;
  y: number;
  onFocus: () => void;
  onDuplicate: () => void;
  onDisconnect: () => void;
  onDelete: () => void;
  onClose: () => void;
}

const ITEMS = [
  { key: "focus", icon: Crosshair, label: "Focus Node" },
  { key: "duplicate", icon: Copy, label: "Duplicate", shortcut: "Ctrl+D" },
  { key: "disconnect", icon: Unplug, label: "Disconnect Edges" },
  { key: "divider" },
  { key: "delete", icon: Trash2, label: "Delete Node", shortcut: "Del", danger: true },
] as const;

export default function ContextMenu({
  x,
  y,
  onFocus,
  onDuplicate,
  onDisconnect,
  onDelete,
  onClose,
}: NodeContextMenuProps) {
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const clickOutside = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as HTMLElement)) {
        onClose();
      }
    };
    const esc = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("mousedown", clickOutside);
    document.addEventListener("keydown", esc);
    return () => {
      document.removeEventListener("mousedown", clickOutside);
      document.removeEventListener("keydown", esc);
    };
  }, [onClose]);

  const actionMap: Record<string, () => void> = {
    focus: onFocus,
    duplicate: onDuplicate,
    disconnect: onDisconnect,
    delete: onDelete,
  };

  return (
    <motion.div
      ref={menuRef}
      initial={{ opacity: 0, scale: 0.95, y: -4 }}
      animate={{ opacity: 1, scale: 1, y: 0 }}
      transition={{ duration: 0.1 }}
      className="fixed z-[90] min-w-[190px] rounded-xl border border-white/[0.08] bg-bg-surface/95 backdrop-blur-xl py-1.5 shadow-2xl"
      style={{ left: x, top: y }}
    >
      {ITEMS.map((item) =>
        item.key === "divider" ? (
          <div key={item.key} className="my-1 h-px bg-white/[0.06] mx-2" />
        ) : (
          <button
            key={item.key}
            onClick={() => {
              actionMap[item.key]?.();
              onClose();
            }}
            className={cn(
              "flex w-full items-center gap-2.5 px-3 py-1.5 text-[12px] font-medium transition-colors",
              item.danger
                ? "text-red-400 hover:bg-red-500/10"
                : "text-text-secondary hover:bg-white/[0.04] hover:text-text-primary",
            )}
          >
            {item.icon && <item.icon className="h-3.5 w-3.5" />}
            <span className="flex-1 text-left">{item.label}</span>
            {item.shortcut && (
              <span className="text-[10px] text-text-muted">{item.shortcut}</span>
            )}
          </button>
        ),
      )}
    </motion.div>
  );
}
