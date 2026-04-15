import { createContext, useContext, useState, useCallback, type ReactNode } from "react";
import { motion, AnimatePresence } from "framer-motion";
import { Check, X, AlertTriangle, Info } from "lucide-react";
import { cn } from "@/lib/utils";

type ToastType = "success" | "error" | "warning" | "info";

interface Toast {
  id: string;
  type: ToastType;
  title: string;
  description?: string;
}

interface ToastContextType {
  addToast: (toast: Omit<Toast, "id">) => void;
}

const ToastContext = createContext<ToastContextType>({ addToast: () => {} });

export function useToast() {
  return useContext(ToastContext);
}

const TOAST_ICONS: Record<ToastType, typeof Check> = {
  success: Check,
  error: X,
  warning: AlertTriangle,
  info: Info,
};

const TOAST_STYLES: Record<ToastType, { bg: string; ring: string; icon: string; text: string }> = {
  success: {
    bg: "bg-emerald-500/10",
    ring: "ring-emerald-500/20",
    icon: "text-emerald-400 bg-emerald-500/20",
    text: "text-emerald-400",
  },
  error: {
    bg: "bg-red-500/10",
    ring: "ring-red-500/20",
    icon: "text-red-400 bg-red-500/20",
    text: "text-red-400",
  },
  warning: {
    bg: "bg-amber-500/10",
    ring: "ring-amber-500/20",
    icon: "text-amber-400 bg-amber-500/20",
    text: "text-amber-400",
  },
  info: {
    bg: "bg-cyan-500/10",
    ring: "ring-cyan-500/20",
    icon: "text-cyan-400 bg-cyan-500/20",
    text: "text-cyan-400",
  },
};

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);

  const addToast = useCallback((t: Omit<Toast, "id">) => {
    const id = Math.random().toString(36).slice(2, 9);
    setToasts((prev) => [...prev, { ...t, id }]);
    setTimeout(() => setToasts((prev) => prev.filter((x) => x.id !== id)), 4000);
  }, []);

  const removeToast = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  return (
    <ToastContext.Provider value={{ addToast }}>
      {children}
      <div
        className="fixed bottom-4 right-4 z-[100] flex flex-col gap-2 pointer-events-none"
        style={{ maxWidth: 360 }}
      >
        <AnimatePresence mode="popLayout">
          {toasts.map((toast) => {
            const s = TOAST_STYLES[toast.type];
            const Icon = TOAST_ICONS[toast.type];
            return (
              <motion.div
                key={toast.id}
                layout
                initial={{ opacity: 0, y: 20, scale: 0.95 }}
                animate={{ opacity: 1, y: 0, scale: 1 }}
                exit={{ opacity: 0, x: 60, scale: 0.95 }}
                transition={{ type: "spring", damping: 25, stiffness: 350 }}
                className={cn(
                  "pointer-events-auto flex items-center gap-2.5 rounded-xl px-4 py-2.5 ring-1 backdrop-blur-xl shadow-2xl",
                  s.bg,
                  s.ring,
                )}
              >
                <div
                  className={cn(
                    "flex h-6 w-6 shrink-0 items-center justify-center rounded-full",
                    s.icon,
                  )}
                >
                  <Icon className="h-3.5 w-3.5" />
                </div>
                <div className="min-w-0 flex-1">
                  <p className={cn("text-[12px] font-semibold truncate", s.text)}>
                    {toast.title}
                  </p>
                  {toast.description && (
                    <p className={cn("text-[10px] opacity-60 truncate", s.text)}>
                      {toast.description}
                    </p>
                  )}
                </div>
                <button
                  onClick={() => removeToast(toast.id)}
                  className={cn("shrink-0 opacity-40 hover:opacity-100 transition-opacity", s.text)}
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              </motion.div>
            );
          })}
        </AnimatePresence>
      </div>
    </ToastContext.Provider>
  );
}
