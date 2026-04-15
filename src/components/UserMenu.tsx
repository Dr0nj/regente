import { useState, useRef, useEffect } from "react";
import { LogOut, User, ChevronDown } from "lucide-react";
import { motion, AnimatePresence } from "framer-motion";
import { useAuth } from "@/lib/auth-context";
import { cn } from "@/lib/utils";

export default function UserMenu() {
  const { user, signOut, isConfigured } = useAuth();
  const [open, setOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement>(null);

  // Close on click outside
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as HTMLElement)) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [open]);

  if (!isConfigured || !user) return null;

  const displayName = user.user_metadata?.display_name ?? user.email?.split("@")[0] ?? "User";
  const initials = displayName.slice(0, 2).toUpperCase();

  return (
    <div className="relative" ref={menuRef}>
      <button
        onClick={() => setOpen(!open)}
        className="flex items-center gap-2 rounded-lg px-2 py-1.5 hover:bg-white/[0.04] transition-all"
      >
        <div className="flex h-7 w-7 items-center justify-center rounded-full bg-accent-cyan/15 text-[10px] font-bold text-accent-cyan ring-1 ring-accent-cyan/20">
          {initials}
        </div>
        <span className="text-[11px] text-text-secondary font-medium max-w-[100px] truncate hidden sm:inline">
          {displayName}
        </span>
        <ChevronDown className={cn("h-3 w-3 text-text-muted transition-transform", open && "rotate-180")} />
      </button>

      <AnimatePresence>
        {open && (
          <motion.div
            initial={{ opacity: 0, y: -4 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -4 }}
            transition={{ duration: 0.15 }}
            className="absolute right-0 top-full mt-1 w-[200px] rounded-xl border border-white/[0.06] bg-bg-surface/98 backdrop-blur-xl shadow-2xl overflow-hidden z-50"
          >
            <div className="px-3 py-2.5 border-b border-white/[0.04]">
              <p className="text-[12px] font-semibold text-text-primary truncate">{displayName}</p>
              <p className="text-[10px] text-text-muted truncate">{user.email}</p>
            </div>
            <div className="py-1">
              <button
                onClick={() => { signOut(); setOpen(false); }}
                className="flex items-center gap-2 w-full px-3 py-2 text-[11px] text-text-secondary hover:bg-white/[0.04] hover:text-red-400 transition-all"
              >
                <LogOut className="h-3.5 w-3.5" />
                Sign Out
              </button>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}
