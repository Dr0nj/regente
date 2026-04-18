import { useState } from "react";
import { changePassword, logout, type AuthUser } from "../lib/auth-api";

interface UserMenuProps {
  me: AuthUser;
  onLogout: () => void;
  onOpenUsers: () => void;
}

export function UserMenu({ me, onLogout, onOpenUsers }: UserMenuProps) {
  const [open, setOpen] = useState(false);
  const [showChange, setShowChange] = useState(false);

  return (
    <>
      <div style={{ position: "relative" }}>
        <button
          onClick={() => setOpen(v => !v)}
          title={`${me.username} (${me.role})`}
          style={{ display: "inline-flex", alignItems: "center", gap: 6 }}
        >
          <span>👤</span>
          <span style={{ fontSize: 12 }}>{me.username}</span>
          <span style={{ fontSize: 10, opacity: 0.6 }}>· {me.role}</span>
        </button>
        {open && (
          <>
            <div onClick={() => setOpen(false)} style={{ position: "fixed", inset: 0, zIndex: 100 }} />
            <div className="v2-grain-card" style={{
              position: "absolute", right: 0, top: "calc(100% + 4px)",
              minWidth: 200, padding: 6, display: "grid", gap: 2, zIndex: 101,
            }}>
              <button onClick={() => { setOpen(false); setShowChange(true); }} style={menuBtn}>Trocar senha</button>
              {me.role === "admin" && (
                <button onClick={() => { setOpen(false); onOpenUsers(); }} style={menuBtn}>Gerenciar usuarios</button>
              )}
              <hr style={{ border: 0, borderTop: "1px solid #333", margin: "4px 0" }} />
              <button
                onClick={async () => { setOpen(false); await logout(); onLogout(); }}
                style={{ ...menuBtn, color: "salmon" }}
              >
                Sair
              </button>
            </div>
          </>
        )}
      </div>
      {showChange && <ChangePasswordDialog onClose={() => setShowChange(false)} />}
    </>
  );
}

const menuBtn: React.CSSProperties = {
  textAlign: "left", padding: "6px 10px", background: "transparent",
  border: "none", color: "inherit", cursor: "pointer", borderRadius: 4,
};

function ChangePasswordDialog({ onClose }: { onClose: () => void }) {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [next2, setNext2] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [done, setDone] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (next.length < 4) { setErr("min 4 caracteres"); return; }
    if (next !== next2) { setErr("nao confere"); return; }
    setBusy(true); setErr(null);
    try {
      await changePassword(current, next);
      setDone(true);
      setTimeout(onClose, 1200);
    } catch (e: unknown) {
      setErr(e instanceof Error ? e.message : "falhou");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div style={{
      position: "fixed", inset: 0, background: "rgba(0,0,0,0.6)",
      display: "grid", placeItems: "center", zIndex: 9999,
    }}>
      <div className="v2-grain-card" style={{ width: 340, padding: 20, display: "grid", gap: 10 }}>
        <h3 style={{ margin: 0, fontSize: 15 }}>Trocar senha</h3>
        {done ? (
          <div style={{ color: "lightgreen", fontSize: 13 }}>Senha trocada.</div>
        ) : (
          <form onSubmit={submit} style={{ display: "grid", gap: 8 }}>
            <label style={lbl}><span>senha atual</span>
              <input type="password" value={current} onChange={e => setCurrent(e.target.value)} required disabled={busy} />
            </label>
            <label style={lbl}><span>nova senha</span>
              <input type="password" value={next} onChange={e => setNext(e.target.value)} required disabled={busy} />
            </label>
            <label style={lbl}><span>repita</span>
              <input type="password" value={next2} onChange={e => setNext2(e.target.value)} required disabled={busy} />
            </label>
            {err && <div style={{ color: "salmon", fontSize: 12 }}>{err}</div>}
            <div style={{ display: "flex", gap: 6, justifyContent: "flex-end" }}>
              <button type="button" onClick={onClose} disabled={busy}>Cancelar</button>
              <button type="submit" disabled={busy}>{busy ? "..." : "Salvar"}</button>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}

const lbl: React.CSSProperties = { display: "grid", gap: 4, fontSize: 12 };
