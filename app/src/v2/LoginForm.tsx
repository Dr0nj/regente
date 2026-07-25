import { useState } from "react";
import { changePassword, login, type AuthUser } from "../lib/auth-api";

/* ── Logo R (asset oficial, branco/transparente) + wordmark ── */
function RegenteLogo({ size = 92 }: { size?: number }) {
  return (
    <div style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: 12 }}>
      <img src="/logo-r.png" alt="Regente" height={size} style={{ height: size, width: "auto", objectFit: "contain" }} />
      <span style={{ fontSize: 26, fontWeight: 500, letterSpacing: "-0.01em", color: "#f5f5f5" }}>Regente</span>
    </div>
  );
}

/* ── Status dots pair ── */
function StatusDots({ green = true }: { green?: boolean }) {
  return (
    <span style={{ display: "inline-flex", gap: 4, alignItems: "center" }}>
      <span style={{
        width: 7, height: 7, borderRadius: "50%",
        background: green ? "#29C5FF" : "#555",
        boxShadow: green ? "0 0 6px #29C5FF55" : "none",
      }} />
      <span style={{
        width: 7, height: 7, borderRadius: "50%",
        background: "#444",
      }} />
    </span>
  );
}

/* ── Eye-off icon ── */
function EyeOff() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#666" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94" />
      <path d="M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19" />
      <line x1="1" y1="1" x2="23" y2="23" />
    </svg>
  );
}

/* ── Ghost workflow background pattern ── */
function GhostWorkflow() {
  return (
    <svg
      style={{ position: "absolute", inset: 0, width: "100%", height: "100%", opacity: 0.06, pointerEvents: "none" }}
      viewBox="0 0 600 700"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      preserveAspectRatio="xMidYMid slice"
    >
      {/* Nodes — small boxes representing workflow cards */}
      {[
        [120, 80], [260, 50], [400, 90],
        [60, 200], [200, 180], [340, 200], [480, 170],
        [90, 320], [240, 310], [380, 330], [520, 300],
        [60, 440], [200, 450], [350, 460], [490, 440],
        [150, 570], [300, 580], [450, 560],
        [220, 660], [370, 650],
      ].map(([x, y], i) => (
        <g key={i}>
          <rect x={x} y={y} width="50" height="32" rx="4" stroke="#666" strokeWidth="1" />
          <line x1={x + 10} y1={y + 10} x2={x + 40} y2={y + 10} stroke="#555" strokeWidth="1" />
          <line x1={x + 10} y1={y + 18} x2={x + 30} y2={y + 18} stroke="#444" strokeWidth="1" />
          <circle cx={x + 6} cy={y + 6} r="2" fill="#555" />
        </g>
      ))}
      {/* Edges — connecting lines */}
      {[
        [170, 96, 200, 180], [310, 66, 340, 200], [450, 106, 480, 170],
        [110, 216, 90, 320], [250, 196, 240, 310], [390, 216, 380, 330],
        [140, 336, 200, 450], [290, 326, 350, 460], [430, 346, 490, 440],
        [110, 456, 150, 570], [250, 466, 300, 580], [400, 476, 450, 560],
        [200, 586, 220, 660], [350, 576, 370, 650],
        [60, 200, 30, 200], [530, 316, 570, 316],
      ].map(([x1, y1, x2, y2], i) => (
        <line key={i} x1={x1} y1={y1} x2={x2} y2={y2} stroke="#555" strokeWidth="1" />
      ))}
      {/* Arrow heads on some edges */}
      {[
        [200, 180], [340, 200], [90, 320], [240, 310],
        [200, 450], [300, 580], [220, 660],
      ].map(([x, y], i) => (
        <polygon key={i} points={`${x},${y} ${x - 4},${y - 6} ${x + 4},${y - 6}`} fill="#555" />
      ))}
    </svg>
  );
}

/* ── Styles ── */
const S = {
  overlay: {
    position: "fixed" as const, inset: 0, display: "grid", placeItems: "center",
    background: "#04080f", zIndex: 9999, overflow: "hidden",
  },
  card: {
    position: "relative" as const,
    width: 420, maxWidth: "92vw",
    padding: "36px 32px 28px",
    display: "grid", gap: 16,
    background: "rgba(6,16,24,0.94)",
    border: "1px solid rgba(41,197,255,0.18)",
    borderRadius: 14,
    boxShadow: "0 0 80px rgba(41,197,255,0.10), 0 0 2px rgba(41,197,255,0.30), inset 0 1px 0 rgba(255,255,255,0.04)",
    backdropFilter: "blur(12px)",
    zIndex: 2,
  },
  logoWrap: {
    display: "flex", justifyContent: "center", marginBottom: 4,
  },
  titleRow: {
    display: "flex", alignItems: "center", justifyContent: "space-between",
    margin: 0,
  },
  title: {
    margin: 0, fontSize: 22, fontWeight: 400, color: "#f5f5f5",
    fontFamily: "'Inter', -apple-system, system-ui, sans-serif",
    letterSpacing: "-0.01em",
  },
  labelRow: {
    display: "flex", alignItems: "center", justifyContent: "space-between",
    fontSize: 13, fontWeight: 600, color: "#f5f5f5",
    marginBottom: 4,
  },
  input: {
    width: "100%",
    background: "rgba(0,0,0,0.5)",
    color: "#f5f5f5",
    border: "1px solid rgba(41,197,255,0.22)",
    borderRadius: 8,
    padding: "10px 14px",
    fontSize: 14,
    outline: "none",
    transition: "border-color 0.2s, box-shadow 0.2s",
    fontFamily: "'Inter', -apple-system, system-ui, sans-serif",
  },
  inputFocus: {
    borderColor: "rgba(41,197,255,0.55)",
    boxShadow: "0 0 0 2px rgba(41,197,255,0.14)",
  },
  btn: {
    width: "100%",
    padding: "12px 0",
    fontSize: 16, fontWeight: 500,
    color: "#29C5FF",
    background: "linear-gradient(180deg, rgba(41,197,255,0.14) 0%, rgba(8,52,80,0.30) 100%)",
    border: "1px solid rgba(41,197,255,0.32)",
    borderRadius: 8,
    cursor: "pointer",
    transition: "all 0.2s",
    letterSpacing: "0.02em",
    fontFamily: "'Inter', -apple-system, system-ui, sans-serif",
    marginTop: 4,
  },
  btnHover: {
    background: "linear-gradient(180deg, rgba(41,197,255,0.22) 0%, rgba(8,52,80,0.40) 100%)",
    boxShadow: "0 0 22px rgba(41,197,255,0.20)",
  },
  footer: {
    fontSize: 12, color: "#666", textAlign: "center" as const, marginTop: 2,
  },
  err: {
    color: "#ef4444", fontSize: 12, padding: "4px 0",
  },
  greenGlow: {
    position: "absolute" as const,
    top: "50%", left: "50%", transform: "translate(-50%,-50%)",
    width: 500, height: 500,
    background: "radial-gradient(ellipse, rgba(41,197,255,0.07) 0%, transparent 70%)",
    pointerEvents: "none" as const, zIndex: 1,
  },
} as const;

interface LoginFormProps {
  onLogin: (user: AuthUser) => void;
}

export function LoginForm({ onLogin }: LoginFormProps) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [showPw, setShowPw] = useState(false);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [forceChange, setForceChange] = useState<{ user: AuthUser } | null>(null);
  const [next, setNext] = useState("");
  const [next2, setNext2] = useState("");
  const [btnHover, setBtnHover] = useState(false);
  const [focusedField, setFocusedField] = useState<string | null>(null);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    setBusy(true);
    try {
      const res = await login(username.trim(), password);
      if (res.user.mustChangePassword) {
        setForceChange({ user: res.user });
      } else {
        onLogin(res.user);
      }
    } catch (e: unknown) {
      const m = e instanceof Error ? e.message : "login failed";
      setErr(m);
    } finally {
      setBusy(false);
    }
  }

  async function submitChange(e: React.FormEvent) {
    e.preventDefault();
    if (!forceChange) return;
    if (next.length < 4) { setErr("password too short (min 4)"); return; }
    if (next !== next2) { setErr("passwords do not match"); return; }
    setBusy(true);
    setErr(null);
    try {
      await changePassword(password, next);
      onLogin({ ...forceChange.user, mustChangePassword: false });
    } catch (e: unknown) {
      const m = e instanceof Error ? e.message : "change failed";
      setErr(m);
    } finally {
      setBusy(false);
    }
  }

  const inputStyle = (field: string) => ({
    ...S.input,
    ...(focusedField === field ? S.inputFocus : {}),
  });

  return (
    <div style={S.overlay}>
      <GhostWorkflow />
      <div style={S.greenGlow} />

      <div style={S.card}>
        {/* Logo */}
        <div style={S.logoWrap}>
          <RegenteLogo />
        </div>

        {!forceChange ? (
          <form onSubmit={submit} style={{ display: "grid", gap: 14 }}>
            {/* Username */}
            <label style={{ display: "grid", gap: 2 }}>
              <div style={S.labelRow}>
                <span>user</span>
                <StatusDots />
              </div>
              <input
                autoFocus
                placeholder="Username"
                value={username}
                onChange={e => setUsername(e.target.value)}
                required
                disabled={busy}
                style={inputStyle("user")}
                onFocus={() => setFocusedField("user")}
                onBlur={() => setFocusedField(null)}
              />
            </label>

            {/* Password */}
            <label style={{ display: "grid", gap: 2 }}>
              <div style={S.labelRow}>
                <span>password</span>
                <span style={{ display: "inline-flex", gap: 6, alignItems: "center" }}>
                  <span style={{ width: 7, height: 7, borderRadius: "50%", background: "#444" }} />
                  <span
                    style={{ cursor: "pointer", display: "flex", alignItems: "center" }}
                    onClick={(e) => { e.preventDefault(); setShowPw(v => !v); }}
                    title={showPw ? "hide password" : "show password"}
                  >
                    <EyeOff />
                  </span>
                </span>
              </div>
              <input
                type={showPw ? "text" : "password"}
                placeholder="Encrypted password"
                value={password}
                onChange={e => setPassword(e.target.value)}
                required
                disabled={busy}
                style={inputStyle("pw")}
                onFocus={() => setFocusedField("pw")}
                onBlur={() => setFocusedField(null)}
              />
            </label>

            {err && <div style={S.err}>{err}</div>}

            <button
              type="submit"
              disabled={busy}
              style={{ ...S.btn, ...(btnHover ? S.btnHover : {}), opacity: busy ? 0.6 : 1 }}
              onMouseEnter={() => setBtnHover(true)}
              onMouseLeave={() => setBtnHover(false)}
            >
              {busy ? "signing in..." : "Sign in"}
            </button>

            <div style={S.footer}>
              Initial default: admin / admin (you will be forced to change it)
            </div>
          </form>
        ) : (
          <form onSubmit={submitChange} style={{ display: "grid", gap: 14 }}>
            <div style={{ fontSize: 14, color: "#a3a3a3" }}>
              Mandatory password change for <b style={{ color: "#f5f5f5" }}>{forceChange.user.username}</b>
            </div>

            <label style={{ display: "grid", gap: 2 }}>
              <div style={S.labelRow}>
                <span>new password</span>
                <StatusDots />
              </div>
              <input
                autoFocus
                type="password"
                placeholder="Enter the new password"
                value={next}
                onChange={e => setNext(e.target.value)}
                required
                disabled={busy}
                style={inputStyle("new")}
                onFocus={() => setFocusedField("new")}
                onBlur={() => setFocusedField(null)}
              />
            </label>

            <label style={{ display: "grid", gap: 2 }}>
              <div style={S.labelRow}>
                <span>repeat</span>
                <StatusDots green={false} />
              </div>
              <input
                type="password"
                placeholder="Repeat the new password"
                value={next2}
                onChange={e => setNext2(e.target.value)}
                required
                disabled={busy}
                style={inputStyle("rep")}
                onFocus={() => setFocusedField("rep")}
                onBlur={() => setFocusedField(null)}
              />
            </label>

            {err && <div style={S.err}>{err}</div>}

            <button
              type="submit"
              disabled={busy}
              style={{ ...S.btn, ...(btnHover ? S.btnHover : {}), opacity: busy ? 0.6 : 1 }}
              onMouseEnter={() => setBtnHover(true)}
              onMouseLeave={() => setBtnHover(false)}
            >
              {busy ? "saving..." : "Save and sign in"}
            </button>
          </form>
        )}
      </div>
    </div>
  );
}
