/**
 * DesignFolderPickerModal — entry-point do modo Design (Etapa 3, 2026-04-26).
 *
 * Quando o usuário entra em Design sem session ativa, esta modal aparece:
 *   "Sobre quais folders você vai trabalhar?"
 *
 * Multi-select de folders existentes + opção de criar folders novos.
 * Confirma → POST /api/design/sessions → sid retornado é seteado em
 * setDesignSessionId(sid), e a tela de Design abre para edição.
 *
 * Conceito: memory/core/regente-product-model.md.
 */
import { useEffect, useState } from "react";
import { listFolders, type FolderInfo } from "@/lib/folder-api";
import {
  createDesignSession,
  listDesignSessions,
  deleteDesignSession,
  type DesignSession,
} from "@/lib/design-session-api";
import { setDesignSessionId } from "@/lib/server-client";

interface Props {
  onSessionCreated: (sid: string, folders: string[], newFolders: string[]) => void;
  onCancel?: () => void;
}

export function DesignFolderPickerModal({ onSessionCreated, onCancel }: Props) {
  const [folders, setFolders] = useState<FolderInfo[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [newFolderInput, setNewFolderInput] = useState("");
  const [newFolders, setNewFolders] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // P5 (2026-04-26) — sessions abertas pra recovery.
  const [openSessions, setOpenSessions] = useState<DesignSession[]>([]);

  useEffect(() => {
    let mounted = true;
    listFolders().then(
      (f) => { if (mounted) { setFolders(f.filter((x) => !x.archived)); setLoading(false); } },
      (e) => { if (mounted) { setError(String(e?.message ?? e)); setLoading(false); } },
    );
    listDesignSessions().then(
      (ss) => { if (mounted) setOpenSessions(ss ?? []); },
      () => { /* ignore — sem sessions abertas é normal */ },
    );
    return () => { mounted = false; };
  }, []);

  const toggle = (name: string) => {
    const next = new Set(selected);
    next.has(name) ? next.delete(name) : next.add(name);
    setSelected(next);
  };

  const addNewFolder = () => {
    const n = newFolderInput.trim();
    if (!n || newFolders.includes(n) || folders.some((f) => f.name === n)) return;
    setNewFolders([...newFolders, n]);
    setNewFolderInput("");
  };

  const removeNewFolder = (n: string) => setNewFolders(newFolders.filter((x) => x !== n));

  const confirm = async () => {
    if (selected.size === 0 && newFolders.length === 0) {
      setError("Selecione ao menos um folder ou crie um novo.");
      return;
    }
    setCreating(true);
    setError(null);
    try {
      const sess = await createDesignSession([...selected], newFolders);
      setDesignSessionId(sess.id);
      onSessionCreated(sess.id, sess.folders, sess.newFolders ?? []);
    } catch (e: unknown) {
      const msg = e && typeof e === "object" && "message" in e ? String((e as { message: unknown }).message) : String(e);
      setError(msg);
      setCreating(false);
    }
  };

  return (
    <div style={overlay}>
      <div style={panel}>
        <h2 style={{ margin: "0 0 4px 0", fontSize: 18 }}>Sobre quais folders você vai trabalhar?</h2>
        <p style={{ margin: "0 0 16px 0", fontSize: 13, color: "#888" }}>
          A sessão criará um workspace efêmero a partir do estado atual do Git.
          Suas edições ficam locais até você clicar em <b>Publish</b>.
        </p>

        {loading && <div style={{ padding: 16 }}>Carregando folders…</div>}
        {error && <div style={{ padding: 8, background: "#3b1d1d", color: "#fbb", borderRadius: 4, marginBottom: 12 }}>{error}</div>}

        {/* P5 — sessions abertas (recovery) */}
        {openSessions.length > 0 && (
          <div style={{ marginBottom: 16, paddingBottom: 12, borderBottom: "1px solid #333" }}>
            <div style={{ fontSize: 12, color: "#aaa", marginBottom: 6 }}>
              Suas sessions abertas <span style={{ color: "#666" }}>(retomar evita perder edições)</span>
            </div>
            <div style={{ display: "flex", flexDirection: "column", gap: 4, maxHeight: 140, overflowY: "auto" }}>
              {openSessions.map((s) => {
                const all = [...(s.folders ?? []), ...(s.newFolders ?? [])];
                const created = new Date(s.createdAt).toLocaleString();
                return (
                  <div key={s.id} style={{ display: "flex", alignItems: "center", gap: 8, padding: "6px 10px", background: "#1a2a1a", borderRadius: 4 }}>
                    <div style={{ flex: 1, fontFamily: "monospace", fontSize: 11 }}>
                      <div style={{ color: "#ccc" }}>{s.id.slice(0, 28)}…</div>
                      <div style={{ color: "#888", fontSize: 10 }}>
                        {all.length > 0 ? all.join(", ") : "(sem folders)"} · criada {created}
                        {(s.newFolders?.length ?? 0) > 0 && <span style={{ color: "#fa6" }}> · +{s.newFolders!.length} novos</span>}
                      </div>
                    </div>
                    <button
                      onClick={() => {
                        setDesignSessionId(s.id);
                        onSessionCreated(s.id, s.folders ?? [], s.newFolders ?? []);
                      }}
                      style={{ ...btnSecondary, background: "#2a6", color: "#fff", padding: "4px 10px", fontSize: 11 }}
                    >
                      Retomar
                    </button>
                    <button
                      onClick={async () => {
                        if (!window.confirm(`Descartar a session ${s.id}? Edições não publicadas serão perdidas.`)) return;
                        try { await deleteDesignSession(s.id); } catch { /* ignore */ }
                        setOpenSessions((cur) => cur.filter((x) => x.id !== s.id));
                      }}
                      style={{ ...btnSecondary, background: "transparent", color: "#a66", border: "1px solid #533", padding: "4px 10px", fontSize: 11 }}
                    >
                      Descartar
                    </button>
                  </div>
                );
              })}
            </div>
          </div>
        )}

        {!loading && (
          <>
            <div style={{ marginBottom: 12 }}>
              <div style={{ fontSize: 12, color: "#aaa", marginBottom: 6 }}>Folders existentes</div>
              {folders.length === 0 && <div style={{ fontSize: 13, color: "#666" }}>Nenhum folder ainda.</div>}
              <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 6, maxHeight: 260, overflowY: "auto" }}>
                {folders.map((f) => (
                  <label key={f.name} style={{ display: "flex", alignItems: "center", gap: 8, padding: "6px 10px", background: selected.has(f.name) ? "#1d3b2a" : "#222", borderRadius: 4, cursor: "pointer" }}>
                    <input type="checkbox" checked={selected.has(f.name)} onChange={() => toggle(f.name)} />
                    <span style={{ flex: 1 }}>{f.name}</span>
                    <span style={{ fontSize: 11, color: "#888" }}>{f.jobCount} jobs</span>
                  </label>
                ))}
              </div>
            </div>

            <div style={{ marginBottom: 12, paddingTop: 12, borderTop: "1px solid #333" }}>
              <div style={{ fontSize: 12, color: "#aaa", marginBottom: 6 }}>
                Criar folder novo <span style={{ color: "#fa6" }}>(forçará PR ao publicar)</span>
              </div>
              <div style={{ display: "flex", gap: 6 }}>
                <input
                  value={newFolderInput}
                  onChange={(e) => setNewFolderInput(e.target.value)}
                  onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); addNewFolder(); } }}
                  placeholder="nome do folder novo"
                  style={{ flex: 1, padding: "6px 10px", background: "#111", border: "1px solid #333", color: "#eee", borderRadius: 4 }}
                />
                <button onClick={addNewFolder} disabled={!newFolderInput.trim()} style={btnSecondary}>+ adicionar</button>
              </div>
              {newFolders.length > 0 && (
                <div style={{ marginTop: 6, display: "flex", flexWrap: "wrap", gap: 4 }}>
                  {newFolders.map((n) => (
                    <span key={n} style={{ background: "#3b2d1d", padding: "3px 8px", borderRadius: 12, fontSize: 12 }}>
                      {n} <button onClick={() => removeNewFolder(n)} style={{ background: "none", border: "none", color: "#fbb", cursor: "pointer", padding: 0 }}>×</button>
                    </span>
                  ))}
                </div>
              )}
            </div>
          </>
        )}

        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end", paddingTop: 12, borderTop: "1px solid #333" }}>
          {onCancel && <button onClick={onCancel} disabled={creating} style={btnSecondary}>Cancelar</button>}
          <button onClick={confirm} disabled={creating || loading || (selected.size === 0 && newFolders.length === 0)} style={btnPrimary}>
            {creating ? "Criando session…" : "Abrir sessão de Design"}
          </button>
        </div>
      </div>
    </div>
  );
}

const overlay: React.CSSProperties = {
  position: "fixed", inset: 0, background: "rgba(0,0,0,0.6)", zIndex: 1000,
  display: "flex", alignItems: "center", justifyContent: "center",
};
const panel: React.CSSProperties = {
  background: "#181818", color: "#eee", padding: 20, borderRadius: 8,
  width: 540, maxHeight: "80vh", display: "flex", flexDirection: "column",
};
const btnPrimary: React.CSSProperties = {
  background: "#2a6", color: "#fff", border: "none", padding: "8px 16px",
  borderRadius: 4, cursor: "pointer", fontSize: 13,
};
const btnSecondary: React.CSSProperties = {
  background: "#333", color: "#ccc", border: "none", padding: "8px 14px",
  borderRadius: 4, cursor: "pointer", fontSize: 13,
};
