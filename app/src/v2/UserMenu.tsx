import { useEffect, useState } from "react";
import { Bell, Settings, Monitor, ChevronDown, KeyRound, Users, LayoutGrid, LogOut } from "lucide-react";
import { changePassword, logout, type AuthUser } from "../lib/auth-api";

interface UserMenuProps {
  me: AuthUser;
  onLogout: () => void;
  onOpenUsers: () => void;
  onOpenControlM?: () => void;
  onOpenSettings?: () => void;
  unreadAlerts?: number;
  onOpenAlerts?: () => void;
  alertsActive?: boolean;
}

/* Botão de ícone redondo da topbar — hover acompanha o tema. */
function IconButton({
  title, onClick, active, danger, children,
}: {
  title: string;
  onClick: () => void;
  active?: boolean;
  danger?: boolean;
  children: React.ReactNode;
}) {
  const [hover, setHover] = useState(false);
  const border = danger && (active || hover)
    ? "var(--v2-status-failed)"
    : active ? "var(--v2-accent-brand)" : "var(--v2-border-medium)";
  const color = danger && (active || hover)
    ? "var(--v2-status-failed)"
    : active ? "var(--v2-accent-brand)" : "var(--v2-text-secondary)";
  return (
    <button
      onClick={onClick}
      title={title}
      aria-label={title}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        position: "relative",
        display: "inline-flex", alignItems: "center", justifyContent: "center",
        width: 34, height: 34, borderRadius: 10,
        background: active || hover ? "var(--v2-bg-hover)" : "transparent",
        border: `1px solid ${border}`,
        color,
        cursor: "pointer",
        transition: "background 120ms, border-color 120ms, color 120ms",
        padding: 0,
      }}
    >
      {children}
    </button>
  );
}

export function UserMenu({
  me, onLogout, onOpenUsers, onOpenControlM, onOpenSettings,
  unreadAlerts = 0, onOpenAlerts, alertsActive,
}: UserMenuProps) {
  const [open, setOpen] = useState(false);
  const [showChange, setShowChange] = useState(false);
  const [isFull, setIsFull] = useState<boolean>(() => typeof document !== "undefined" && !!document.fullscreenElement);

  useEffect(() => {
    const onFs = () => setIsFull(!!document.fullscreenElement);
    document.addEventListener("fullscreenchange", onFs);
    return () => document.removeEventListener("fullscreenchange", onFs);
  }, []);

  function toggleFullscreen() {
    if (!document.fullscreenElement) {
      document.documentElement.requestFullscreen?.().catch(() => {});
    } else {
      document.exitFullscreen?.().catch(() => {});
    }
  }

  const initials = me.username.slice(0, 2).toUpperCase();

  return (
    <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
      {onOpenAlerts && (
        <IconButton title={unreadAlerts > 0 ? `${unreadAlerts} alerta(s) não reconhecido(s)` : "Alertas"} onClick={onOpenAlerts} active={alertsActive} danger={unreadAlerts > 0}>
          <Bell size={16} />
          {unreadAlerts > 0 && (
            <span style={{
              position: "absolute", top: -5, right: -5,
              minWidth: 15, height: 15, padding: "0 3px",
              background: "var(--v2-status-failed)", color: "#fff",
              borderRadius: 8, fontSize: 9, fontWeight: 700,
              display: "flex", alignItems: "center", justifyContent: "center",
              fontFamily: "var(--v2-font-mono)", lineHeight: 1,
            }}>{unreadAlerts > 99 ? "99+" : unreadAlerts}</span>
          )}
        </IconButton>
      )}

      <IconButton title="Configurações e conta" onClick={() => setOpen((v) => !v)} active={open}>
        <Settings size={16} />
      </IconButton>

      <IconButton title={isFull ? "Sair de tela cheia" : "Tela cheia"} onClick={toggleFullscreen} active={isFull}>
        <Monitor size={16} />
      </IconButton>

      <div style={{ position: "relative" }}>
        <button
          onClick={() => setOpen((v) => !v)}
          title={`${me.username} · ${me.role}`}
          aria-label="Conta"
          style={{
            display: "inline-flex", alignItems: "center", gap: 6,
            background: "transparent", border: "none", cursor: "pointer", padding: 0,
          }}
        >
          <span style={{
            width: 30, height: 30, borderRadius: "50%",
            background: "var(--v2-accent-brand)", color: "var(--v2-bg-canvas)",
            display: "flex", alignItems: "center", justifyContent: "center",
            fontSize: 12, fontWeight: 700, letterSpacing: "0.02em",
            fontFamily: "var(--v2-font-sans)", flexShrink: 0,
          }}>{initials}</span>
          <ChevronDown size={14} color="var(--v2-text-secondary)" style={{
            transform: open ? "rotate(180deg)" : "none", transition: "transform 150ms",
          }} />
        </button>

        {open && (
          <>
            <div onClick={() => setOpen(false)} style={{ position: "fixed", inset: 0, zIndex: 100 }} />
            <div className="v2-neon-card" style={{
              position: "absolute", right: 0, top: "calc(100% + 10px)",
              minWidth: 230, padding: 6, display: "grid", gap: 2, zIndex: 101,
              background: "var(--v2-bg-elevated)", borderRadius: 14,
            }}>
              <div style={{
                display: "flex", alignItems: "center", gap: 10,
                padding: "8px 10px 10px", borderBottom: "1px solid var(--v2-border-subtle)", marginBottom: 4,
              }}>
                <span style={{
                  width: 32, height: 32, borderRadius: "50%",
                  background: "var(--v2-accent-brand)", color: "var(--v2-bg-canvas)",
                  display: "flex", alignItems: "center", justifyContent: "center",
                  fontSize: 12, fontWeight: 700, flexShrink: 0,
                }}>{initials}</span>
                <div style={{ display: "grid", lineHeight: 1.3 }}>
                  <span style={{ fontSize: 13, fontWeight: 600, color: "var(--v2-text-primary)" }}>{me.username}</span>
                  <span style={{ fontSize: 11, color: "var(--v2-text-muted)", textTransform: "uppercase", letterSpacing: "0.06em" }}>{me.role}</span>
                </div>
              </div>

              <button onClick={() => { setOpen(false); setShowChange(true); }} style={menuBtn}>
                <KeyRound size={14} /> Trocar senha
              </button>
              {me.role === "admin" && (
                <button onClick={() => { setOpen(false); onOpenUsers(); }} style={menuBtn}>
                  <Users size={14} /> Gerenciar usuários
                </button>
              )}
              {onOpenControlM && (
                <button onClick={() => { setOpen(false); onOpenControlM(); }} style={menuBtn}>
                  <LayoutGrid size={14} /> Control-M Panel
                </button>
              )}
              {onOpenSettings && me.role === "admin" && (
                <button onClick={() => { setOpen(false); onOpenSettings(); }} style={menuBtn}>
                  <Settings size={14} /> Configurações
                </button>
              )}
              <hr style={{ border: 0, borderTop: "1px solid var(--v2-border-medium)", margin: "4px 0" }} />
              <button
                onClick={async () => { setOpen(false); await logout(); onLogout(); }}
                style={{ ...menuBtn, color: "salmon" }}
              >
                <LogOut size={14} /> Sair
              </button>
            </div>
          </>
        )}
      </div>

      {showChange && <ChangePasswordDialog onClose={() => setShowChange(false)} />}
    </div>
  );
}

const menuBtn: React.CSSProperties = {
  textAlign: "left", padding: "8px 10px", background: "transparent",
  border: "none", color: "var(--v2-text-primary)", cursor: "pointer", borderRadius: 8,
  display: "flex", alignItems: "center", gap: 8, fontSize: 13, width: "100%",
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
      <div className="v2-neon-card" style={{ width: 340, padding: 20, display: "grid", gap: 10, background: "var(--v2-bg-elevated)", borderRadius: 14 }}>
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
