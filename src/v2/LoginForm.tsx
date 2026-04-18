import { useState } from "react";
import { changePassword, login, type AuthUser } from "../lib/auth-api";

interface LoginFormProps {
  onLogin: (user: AuthUser) => void;
}

export function LoginForm({ onLogin }: LoginFormProps) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [forceChange, setForceChange] = useState<{ user: AuthUser } | null>(null);
  const [next, setNext] = useState("");
  const [next2, setNext2] = useState("");

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
      const m = e instanceof Error ? e.message : "login falhou";
      setErr(m);
    } finally {
      setBusy(false);
    }
  }

  async function submitChange(e: React.FormEvent) {
    e.preventDefault();
    if (!forceChange) return;
    if (next.length < 4) { setErr("senha muito curta (min 4)"); return; }
    if (next !== next2) { setErr("senhas nao conferem"); return; }
    setBusy(true);
    setErr(null);
    try {
      await changePassword(password, next);
      onLogin({ ...forceChange.user, mustChangePassword: false });
    } catch (e: unknown) {
      const m = e instanceof Error ? e.message : "troca falhou";
      setErr(m);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div style={{
      position: "fixed", inset: 0, display: "grid", placeItems: "center",
      background: "var(--v2-bg, #0b0d10)", zIndex: 9999,
    }}>
      <div className="v2-grain-card" style={{ width: 360, padding: 24, display: "grid", gap: 12 }}>
        <h2 style={{ margin: 0, fontSize: 18 }}>Regente — entrar</h2>
        {!forceChange ? (
          <form onSubmit={submit} style={{ display: "grid", gap: 10 }}>
            <label style={{ display: "grid", gap: 4, fontSize: 12 }}>
              <span>usuario</span>
              <input autoFocus value={username} onChange={e => setUsername(e.target.value)} required disabled={busy} />
            </label>
            <label style={{ display: "grid", gap: 4, fontSize: 12 }}>
              <span>senha</span>
              <input type="password" value={password} onChange={e => setPassword(e.target.value)} required disabled={busy} />
            </label>
            {err && <div style={{ color: "salmon", fontSize: 12 }}>{err}</div>}
            <button type="submit" disabled={busy}>{busy ? "entrando..." : "Entrar"}</button>
            <div style={{ fontSize: 11, opacity: 0.6 }}>Default inicial: admin / admin (sera obrigado a trocar)</div>
          </form>
        ) : (
          <form onSubmit={submitChange} style={{ display: "grid", gap: 10 }}>
            <div style={{ fontSize: 13 }}>Trocar senha obrigatoria para <b>{forceChange.user.username}</b></div>
            <label style={{ display: "grid", gap: 4, fontSize: 12 }}>
              <span>nova senha</span>
              <input autoFocus type="password" value={next} onChange={e => setNext(e.target.value)} required disabled={busy} />
            </label>
            <label style={{ display: "grid", gap: 4, fontSize: 12 }}>
              <span>repita</span>
              <input type="password" value={next2} onChange={e => setNext2(e.target.value)} required disabled={busy} />
            </label>
            {err && <div style={{ color: "salmon", fontSize: 12 }}>{err}</div>}
            <button type="submit" disabled={busy}>{busy ? "salvando..." : "Salvar e entrar"}</button>
          </form>
        )}
      </div>
    </div>
  );
}
