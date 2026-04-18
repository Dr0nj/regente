import { useEffect, useState } from "react";
import {
  createUser, deleteUser, listUsers, resetUserPassword, updateUserRole,
  listUserACLs, replaceUserACLs,
  type AuthUser, type FolderACL, type Role,
} from "../lib/auth-api";
import { listFolders, type FolderInfo } from "../lib/folder-api";

interface UsersDialogProps {
  meId: number;
  onClose: () => void;
}

const ROLES: Role[] = ["admin", "operator", "viewer"];

export function UsersDialog({ meId, onClose }: UsersDialogProps) {
  const [users, setUsers] = useState<AuthUser[]>([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);

  // create row
  const [newU, setNewU] = useState("");
  const [newP, setNewP] = useState("");
  const [newR, setNewR] = useState<Role>("viewer");
  const [creating, setCreating] = useState(false);

  // reset pw
  const [resetFor, setResetFor] = useState<AuthUser | null>(null);
  // F11.10b acls
  const [aclFor, setAclFor] = useState<AuthUser | null>(null);

  async function reload() {
    setLoading(true); setErr(null);
    try { setUsers(await listUsers()); }
    catch (e: unknown) { setErr(e instanceof Error ? e.message : "load falhou"); }
    finally { setLoading(false); }
  }

  useEffect(() => { reload(); }, []);

  async function doCreate(e: React.FormEvent) {
    e.preventDefault();
    if (!newU.trim() || newP.length < 4) { setErr("usuario obrig + senha min 4"); return; }
    setCreating(true); setErr(null);
    try {
      await createUser(newU.trim(), newP, newR);
      setNewU(""); setNewP(""); setNewR("viewer");
      await reload();
    } catch (e: unknown) {
      setErr(e instanceof Error ? e.message : "create falhou");
    } finally { setCreating(false); }
  }

  async function doRole(u: AuthUser, role: Role) {
    if (u.id === meId && role !== "admin") {
      if (!confirm("Tirar admin de voce mesmo?")) return;
    }
    try { await updateUserRole(u.id, role); await reload(); }
    catch (e: unknown) { setErr(e instanceof Error ? e.message : "role falhou"); }
  }

  async function doDelete(u: AuthUser) {
    if (u.id === meId) { alert("nao pode deletar voce mesmo"); return; }
    if (!confirm(`Deletar ${u.username}?`)) return;
    try { await deleteUser(u.id); await reload(); }
    catch (e: unknown) { setErr(e instanceof Error ? e.message : "delete falhou"); }
  }

  return (
    <div style={{
      position: "fixed", inset: 0, background: "rgba(0,0,0,0.6)",
      display: "grid", placeItems: "center", zIndex: 9000,
    }}>
      <div className="v2-grain-card" style={{ width: 720, maxHeight: "85vh", padding: 20, display: "grid", gap: 12, overflow: "auto" }}>
        <header style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <h3 style={{ margin: 0, fontSize: 16 }}>Usuarios</h3>
          <button onClick={onClose}>Fechar</button>
        </header>

        <form onSubmit={doCreate} style={{ display: "grid", gridTemplateColumns: "1fr 1fr 120px auto", gap: 6, alignItems: "end" }}>
          <label style={lbl}><span>novo usuario</span>
            <input value={newU} onChange={e => setNewU(e.target.value)} disabled={creating} />
          </label>
          <label style={lbl}><span>senha inicial</span>
            <input type="password" value={newP} onChange={e => setNewP(e.target.value)} disabled={creating} />
          </label>
          <label style={lbl}><span>role</span>
            <select value={newR} onChange={e => setNewR(e.target.value as Role)} disabled={creating}>
              {ROLES.map(r => <option key={r} value={r}>{r}</option>)}
            </select>
          </label>
          <button type="submit" disabled={creating}>{creating ? "..." : "Criar"}</button>
        </form>

        {err && <div style={{ color: "salmon", fontSize: 12 }}>{err}</div>}

        {loading ? <div>Carregando...</div> : (
          <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 13 }}>
            <thead>
              <tr style={{ textAlign: "left", borderBottom: "1px solid #333" }}>
                <th style={th}>id</th>
                <th style={th}>usuario</th>
                <th style={th}>role</th>
                <th style={th}>criado</th>
                <th style={th}>acoes</th>
              </tr>
            </thead>
            <tbody>
              {users.map(u => (
                <tr key={u.id} style={{ borderBottom: "1px solid #222" }}>
                  <td style={td}>{u.id}</td>
                  <td style={td}>{u.username}{u.id === meId && <span style={{ opacity: 0.6 }}> (voce)</span>}{u.mustChangePassword && <span style={{ color: "orange", marginLeft: 6 }}>[trocar]</span>}</td>
                  <td style={td}>
                    <select value={u.role} onChange={e => doRole(u, e.target.value as Role)}>
                      {ROLES.map(r => <option key={r} value={r}>{r}</option>)}
                    </select>
                  </td>
                  <td style={td}>{u.createdAt ? new Date(u.createdAt).toLocaleString() : "-"}</td>
                  <td style={td}>
                    <button onClick={() => setAclFor(u)} style={{ marginRight: 6 }} disabled={u.role === "admin"}>ACLs</button>
                    <button onClick={() => setResetFor(u)} style={{ marginRight: 6 }}>reset pw</button>
                    <button onClick={() => doDelete(u)} disabled={u.id === meId} style={{ color: u.id === meId ? "gray" : "salmon" }}>del</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {resetFor && <ResetPasswordPrompt user={resetFor} onClose={() => { setResetFor(null); reload(); }} />}
      {aclFor && <AclEditor user={aclFor} onClose={() => setAclFor(null)} />}
    </div>
  );
}

function ResetPasswordPrompt({ user, onClose }: { user: AuthUser; onClose: () => void }) {
  const [pw, setPw] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  async function go(e: React.FormEvent) {
    e.preventDefault();
    if (pw.length < 4) { setErr("min 4"); return; }
    setBusy(true); setErr(null);
    try { await resetUserPassword(user.id, pw); setDone(true); setTimeout(onClose, 1000); }
    catch (e: unknown) { setErr(e instanceof Error ? e.message : "falhou"); }
    finally { setBusy(false); }
  }
  return (
    <div style={{ position: "fixed", inset: 0, background: "rgba(0,0,0,0.7)", display: "grid", placeItems: "center", zIndex: 9100 }}>
      <div className="v2-grain-card" style={{ width: 320, padding: 18, display: "grid", gap: 10 }}>
        <h4 style={{ margin: 0, fontSize: 14 }}>Reset senha — {user.username}</h4>
        {done ? <div style={{ color: "lightgreen" }}>OK</div> : (
          <form onSubmit={go} style={{ display: "grid", gap: 8 }}>
            <input autoFocus type="password" value={pw} onChange={e => setPw(e.target.value)} placeholder="nova senha" disabled={busy} />
            {err && <div style={{ color: "salmon", fontSize: 12 }}>{err}</div>}
            <div style={{ display: "flex", justifyContent: "flex-end", gap: 6 }}>
              <button type="button" onClick={onClose} disabled={busy}>Cancelar</button>
              <button type="submit" disabled={busy}>{busy ? "..." : "Definir"}</button>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}

const lbl: React.CSSProperties = { display: "grid", gap: 4, fontSize: 11 };
const th: React.CSSProperties = { padding: "6px 4px", fontWeight: 600, fontSize: 12, opacity: 0.7 };
const td: React.CSSProperties = { padding: "6px 4px" };

function AclEditor({ user, onClose }: { user: AuthUser; onClose: () => void }) {
  const [folders, setFolders] = useState<FolderInfo[]>([]);
  const [acls, setAcls] = useState<Map<string, string>>(new Map()); // folder -> perms
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    let cancel = false;
    (async () => {
      setLoading(true); setErr(null);
      try {
        const [fs, ac] = await Promise.all([listFolders(), listUserACLs(user.id)]);
        if (cancel) return;
        setFolders(fs);
        const m = new Map<string, string>();
        for (const a of ac) m.set(a.folder, a.perms);
        setAcls(m);
      } catch (e: unknown) {
        setErr(e instanceof Error ? e.message : "load falhou");
      } finally {
        setLoading(false);
      }
    })();
    return () => { cancel = true; };
  }, [user.id]);

  function setPerm(folder: string, perm: "" | "r" | "rw") {
    const next = new Map(acls);
    if (perm === "") next.delete(folder);
    else next.set(folder, perm);
    setAcls(next);
  }

  async function save() {
    setSaving(true); setErr(null);
    try {
      const list = Array.from(acls.entries()).map(([folder, perms]) => ({ folder, perms }));
      await replaceUserACLs(user.id, list);
      onClose();
    } catch (e: unknown) {
      setErr(e instanceof Error ? e.message : "save falhou");
    } finally {
      setSaving(false);
    }
  }

  const restricted = acls.size > 0;

  return (
    <div style={{ position: "fixed", inset: 0, background: "rgba(0,0,0,0.7)", display: "grid", placeItems: "center", zIndex: 9100 }}>
      <div className="v2-grain-card" style={{ width: 540, maxHeight: "85vh", padding: 18, display: "grid", gap: 10, overflow: "auto" }}>
        <header style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
          <h4 style={{ margin: 0, fontSize: 14 }}>ACLs — {user.username} <span style={{ opacity: 0.6, fontWeight: 400 }}>({user.role})</span></h4>
          <button onClick={onClose} disabled={saving}>×</button>
        </header>
        <div style={{ fontSize: 11, opacity: 0.75, lineHeight: 1.5 }}>
          {restricted
            ? "Modo restrito: user só vê / edita folders explicitamente marcadas abaixo."
            : "Modo permissivo: sem ACLs ⇒ user lê todas as folders (operator também escreve em todas). Marcar pelo menos uma folder ativa modo restrito."}
        </div>
        {err && <div style={{ color: "salmon", fontSize: 12 }}>{err}</div>}

        {loading ? <div>Carregando...</div> : (
          <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 12 }}>
            <thead>
              <tr style={{ textAlign: "left", borderBottom: "1px solid #333" }}>
                <th style={th}>folder</th>
                <th style={th}>jobs</th>
                <th style={th}>permissão</th>
              </tr>
            </thead>
            <tbody>
              {folders.map(f => {
                const cur = acls.get(f.name) ?? "";
                return (
                  <tr key={f.name} style={{ borderBottom: "1px solid #222" }}>
                    <td style={td}>{f.name}</td>
                    <td style={td}>{f.jobCount}</td>
                    <td style={td}>
                      <label style={{ marginRight: 8 }}><input type="radio" name={`p-${f.name}`} checked={cur === ""} onChange={() => setPerm(f.name, "")} /> none</label>
                      <label style={{ marginRight: 8 }}><input type="radio" name={`p-${f.name}`} checked={cur === "r"} onChange={() => setPerm(f.name, "r")} /> read</label>
                      <label><input type="radio" name={`p-${f.name}`} checked={cur === "rw" || cur === "wr"} onChange={() => setPerm(f.name, "rw")} disabled={user.role === "viewer"} /> read+write</label>
                    </td>
                  </tr>
                );
              })}
              {folders.length === 0 && <tr><td colSpan={3} style={{ ...td, opacity: 0.6 }}>Nenhuma folder.</td></tr>}
            </tbody>
          </table>
        )}

        <div style={{ display: "flex", gap: 6, justifyContent: "flex-end" }}>
          <button onClick={onClose} disabled={saving}>Cancelar</button>
          <button onClick={save} disabled={saving || loading}>{saving ? "..." : "Salvar"}</button>
        </div>
      </div>
    </div>
  );
}
