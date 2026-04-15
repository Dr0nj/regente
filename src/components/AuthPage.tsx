import { useState } from "react";
import { motion } from "framer-motion";
import { Workflow, Mail, Lock, User, ArrowRight, AlertCircle } from "lucide-react";
import { cn } from "@/lib/utils";
import { useAuth } from "@/lib/auth-context";

export default function AuthPage() {
  const { signIn, signUp } = useAuth();
  const [mode, setMode] = useState<"login" | "signup">("login");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [success, setSuccess] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSuccess(null);
    setLoading(true);

    if (mode === "login") {
      const { error: err } = await signIn(email, password);
      if (err) setError(err);
    } else {
      const { error: err } = await signUp(email, password, displayName || undefined);
      if (err) {
        setError(err);
      } else {
        setSuccess("Account created! Check your email to confirm, then sign in.");
        setMode("login");
      }
    }
    setLoading(false);
  };

  return (
    <div className="flex h-screen items-center justify-center bg-bg-deep">
      {/* Subtle grid background */}
      <div
        className="absolute inset-0 opacity-[0.03]"
        style={{
          backgroundImage: "radial-gradient(circle at 1px 1px, #22d3ee 1px, transparent 0)",
          backgroundSize: "40px 40px",
        }}
      />

      <motion.div
        initial={{ opacity: 0, y: 20 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ type: "spring", damping: 25, stiffness: 300 }}
        className="relative w-[400px] rounded-2xl border border-white/[0.06] bg-bg-surface/95 backdrop-blur-xl shadow-2xl overflow-hidden"
        style={{ boxShadow: "0 16px 64px rgba(0,0,0,0.5), inset 0 1px 0 rgba(255,255,255,0.04)" }}
      >
        {/* Header */}
        <div className="flex flex-col items-center pt-8 pb-6 px-8 border-b border-white/[0.04]">
          <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-accent-cyan/10 ring-1 ring-accent-cyan/20 mb-4">
            <Workflow className="h-6 w-6 text-accent-cyan" />
          </div>
          <h1 className="text-[18px] font-bold text-text-primary">Regente</h1>
          <p className="text-[12px] text-text-muted mt-1">Workflow Orchestration Platform</p>
        </div>

        {/* Mode tabs */}
        <div className="flex px-8 pt-4">
          <button
            onClick={() => { setMode("login"); setError(null); setSuccess(null); }}
            className={cn(
              "flex-1 pb-2 text-[12px] font-semibold border-b-2 transition-all",
              mode === "login"
                ? "border-accent-cyan text-accent-cyan"
                : "border-transparent text-text-muted hover:text-text-secondary"
            )}
          >
            Sign In
          </button>
          <button
            onClick={() => { setMode("signup"); setError(null); setSuccess(null); }}
            className={cn(
              "flex-1 pb-2 text-[12px] font-semibold border-b-2 transition-all",
              mode === "signup"
                ? "border-accent-cyan text-accent-cyan"
                : "border-transparent text-text-muted hover:text-text-secondary"
            )}
          >
            Create Account
          </button>
        </div>

        {/* Form */}
        <form onSubmit={handleSubmit} className="px-8 py-6 space-y-4">
          {error && (
            <div className="flex items-center gap-2 p-3 rounded-lg bg-red-500/[0.08] border border-red-500/20 text-[11px] text-red-400">
              <AlertCircle className="h-3.5 w-3.5 shrink-0" />
              {error}
            </div>
          )}

          {success && (
            <div className="flex items-center gap-2 p-3 rounded-lg bg-emerald-500/[0.08] border border-emerald-500/20 text-[11px] text-emerald-400">
              {success}
            </div>
          )}

          {mode === "signup" && (
            <div>
              <label className="block text-[11px] font-semibold uppercase tracking-wider text-text-muted mb-1.5">
                Display Name
              </label>
              <div className="relative">
                <User className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-muted" />
                <input
                  type="text"
                  value={displayName}
                  onChange={(e) => setDisplayName(e.target.value)}
                  placeholder="Your name"
                  className="w-full h-10 rounded-lg border border-white/[0.08] bg-white/[0.03] pl-9 pr-3 text-[13px] text-text-primary placeholder:text-text-muted/40 outline-none focus:border-accent-cyan/40 focus:ring-1 focus:ring-accent-cyan/20 transition-all"
                />
              </div>
            </div>
          )}

          <div>
            <label className="block text-[11px] font-semibold uppercase tracking-wider text-text-muted mb-1.5">
              Email
            </label>
            <div className="relative">
              <Mail className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-muted" />
              <input
                type="email"
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="you@example.com"
                className="w-full h-10 rounded-lg border border-white/[0.08] bg-white/[0.03] pl-9 pr-3 text-[13px] text-text-primary placeholder:text-text-muted/40 outline-none focus:border-accent-cyan/40 focus:ring-1 focus:ring-accent-cyan/20 transition-all"
              />
            </div>
          </div>

          <div>
            <label className="block text-[11px] font-semibold uppercase tracking-wider text-text-muted mb-1.5">
              Password
            </label>
            <div className="relative">
              <Lock className="absolute left-3 top-1/2 -translate-y-1/2 h-3.5 w-3.5 text-text-muted" />
              <input
                type="password"
                required
                minLength={6}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="••••••••"
                className="w-full h-10 rounded-lg border border-white/[0.08] bg-white/[0.03] pl-9 pr-3 text-[13px] text-text-primary placeholder:text-text-muted/40 outline-none focus:border-accent-cyan/40 focus:ring-1 focus:ring-accent-cyan/20 transition-all"
              />
            </div>
          </div>

          <button
            type="submit"
            disabled={loading}
            className={cn(
              "flex items-center justify-center gap-2 w-full h-10 rounded-lg text-[13px] font-semibold transition-all",
              "bg-accent-cyan/20 text-accent-cyan hover:bg-accent-cyan/30 ring-1 ring-accent-cyan/30",
              loading && "opacity-50 cursor-not-allowed"
            )}
          >
            {loading ? (
              <div className="h-4 w-4 animate-spin rounded-full border-2 border-accent-cyan/30 border-t-accent-cyan" />
            ) : (
              <>
                {mode === "login" ? "Sign In" : "Create Account"}
                <ArrowRight className="h-3.5 w-3.5" />
              </>
            )}
          </button>
        </form>

        {/* Footer */}
        <div className="px-8 pb-6 text-center">
          <p className="text-[10px] text-text-muted/50">
            {mode === "login"
              ? "No account? Click Create Account above."
              : "Already have an account? Click Sign In above."}
          </p>
        </div>
      </motion.div>
    </div>
  );
}
