/**
 * ResourcesPanel — o POOL de RECURSOS/QUOTAS do ambiente (Monitoring, ao lado
 * de "Condições"). É o gêmeo do ConditionsPanel, mas para o eixo de
 * CONCORRÊNCIA (semáforo Control-M), não de dependência lógica:
 *
 *   - VÊ a capacidade e o uso corrente de cada recurso ao vivo.
 *   - AJUSTA a capacidade (quantos jobs com o recurso rodam ao mesmo tempo).
 *   - DELETA um recurso (recusado enquanto houver uso ativo).
 *
 * QUEM consome cada recurso é escolhido no job (Design → aba Condições →
 * bloco Recursos). Aqui é o "control tower" do pool: capacidade + estado.
 * Não há WS de recurso — o snapshot é read-only e barato, então poll curto.
 */

import { useEffect, useMemo, useState } from "react";
import { X, Trash2, RefreshCw, Plus } from "lucide-react";
import { listResources, setResourceCapacity, deleteResource, type ResourceState } from "@/lib/bloco2-api";
import { toast } from "./Toast";
import { useResizablePanel, ResizeHandle } from "./resizable";
import { addToggleStyle } from "./add-toggle";

const RES_ACCENT = "#f59e0b"; // âmbar — mesma cor do bloco Recursos do drawer

export default function ResourcesPanel({ onClose }: { onClose: () => void }) {
  const [items, setItems] = useState<ResourceState[]>([]);
  const [filter, setFilter] = useState("");
  const [draftName, setDraftName] = useState("");
  const [draftCap, setDraftCap] = useState("1");
  const [adding, setAdding] = useState(false); // bloco de adição escondido até clicar no ＋ (padrão do produto)
  const [busy, setBusy] = useState(false);

  const reload = () => listResources().then(setItems).catch(() => setItems([]));
  useEffect(() => {
    reload();
    const i = setInterval(reload, 4000); // sem WS de recurso — poll curto
    return () => clearInterval(i);
  }, []);

  const visible = useMemo(() => {
    const q = filter.trim().toUpperCase();
    if (!q) return items;
    return items.filter((r) => r.name.toUpperCase().includes(q));
  }, [items, filter]);

  const submit = async () => {
    const name = draftName.trim();
    if (!name) return;
    const cap = Math.max(0, parseInt(draftCap || "0", 10) || 0);
    setBusy(true);
    try {
      await setResourceCapacity(name, cap);
      setDraftName(""); setDraftCap("1");
      toast.success(`Resource ${name} set`, { detail: `capacity ${cap}${cap === 1 ? " (exclusive lock)" : ""}` });
      await reload();
    } catch (e) {
      toast.error("Failed to set resource", { detail: e instanceof Error ? e.message : String(e) });
    } finally {
      setBusy(false);
    }
  };

  const remove = async (r: ResourceState) => {
    if (r.used > 0) { toast.error(`"${r.name}" in use (${r.used})`, { detail: "release the usage before deleting" }); return; }
    if (!confirm(`Delete the resource "${r.name}"?`)) return;
    setBusy(true);
    try {
      await deleteResource(r.name);
      toast.success(`Resource ${r.name} deleted`);
      await reload();
    } catch (e) {
      toast.error("Failed to delete resource", { detail: e instanceof Error ? e.message : String(e) });
    } finally {
      setBusy(false);
    }
  };

  // Mesmo resizable dos outros painéis (borda esquerda arrasta; 2× reseta).
  const { width, onMouseDown, reset } = useResizablePanel({
    storageKey: "regente.panel.resources.w", defaultWidth: 380, min: 300, max: 760, edge: "left",
  });

  return (
    <aside style={{
      position: "absolute", top: 10, right: 10, bottom: 10, width,
      background: "var(--v2-bg-surface)",
      border: "1px solid var(--v2-border-medium)", borderRadius: 16,
      boxShadow: "0 10px 30px rgba(0,0,0,0.35)",
      display: "flex", flexDirection: "column", fontFamily: "var(--v2-font-sans)", zIndex: 6, overflow: "hidden",
    }}>
      <ResizeHandle edge="left" onMouseDown={onMouseDown} onReset={reset} />
      {/* Header */}
      <div style={{ padding: "10px 12px", borderBottom: "1px solid var(--v2-border-subtle)", display: "flex", alignItems: "center", gap: 8 }}>
        <span style={{ width: 6, height: 6, borderRadius: "50%", background: RES_ACCENT }} />
        <span style={{ fontSize: 11, fontWeight: 600, letterSpacing: "0.04em" }}>ENVIRONMENT RESOURCES</span>
        <span style={{ fontSize: 10, color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)" }}>{items.length}</span>
        <button onClick={() => setAdding((v) => !v)} title={adding ? "Close" : "Add resource"}
          style={{ marginLeft: "auto", ...addToggleStyle(adding, RES_ACCENT) }}>{adding ? <X size={12} /> : <Plus size={12} />}</button>
        <button onClick={() => void reload()} title="Reload" style={iconBtn}><RefreshCw size={12} /></button>
        <button onClick={onClose} title="Close" style={iconBtn}><X size={14} /></button>
      </div>

      {/* Add / set capacity — escondido até clicar no ＋ do cabeçalho (workspace limpo). */}
      {adding && (
      <div style={{ padding: "10px 12px", borderBottom: "1px solid var(--v2-border-subtle)", display: "flex", flexDirection: "column", gap: 6 }}>
        <div style={{ display: "flex", gap: 6 }}>
          {/* Campo NOVO com visual distinto (fundo elevado + borda tracejada). */}
          <input
            autoFocus
            value={draftName}
            placeholder="resource… (e.g. SAP)"
            onChange={(e) => setDraftName(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); void submit(); } else if (e.key === "Escape") { setDraftName(""); setAdding(false); } }}
            onFocus={(e) => { e.currentTarget.style.borderColor = RES_ACCENT; }}
            onBlur={(e) => { e.currentTarget.style.borderColor = "var(--v2-border-medium)"; }}
            style={{ ...inputStyle, background: "var(--v2-bg-elevated)", border: "1px dashed var(--v2-border-medium)" }}
          />
          <input
            type="number" min={0} value={draftCap}
            onChange={(e) => setDraftCap(e.target.value.replace(/\D/g, ""))}
            title="capacity (concurrent runs)"
            style={{ ...inputStyle, width: 64, flex: "none", background: "var(--v2-bg-elevated)", border: "1px dashed var(--v2-border-medium)" }}
          />
          <button onClick={() => void submit()} disabled={busy || !draftName.trim()} title="Set capacity"
            style={{ ...btnStyle, borderColor: draftName.trim() ? RES_ACCENT : "var(--v2-border-medium)", color: draftName.trim() ? RES_ACCENT : "var(--v2-text-muted)" }}>
            Set
          </button>
        </div>
        <div style={{ fontSize: 9.5, color: "var(--v2-text-muted)", lineHeight: 1.4 }}>
          Capacity = how many jobs holding the resource run at the same time. Choose WHICH jobs consume it in Design (Conditions tab → Resources).
        </div>
      </div>
      )}

      {/* Filtro */}
      <div style={{ padding: "8px 12px", borderBottom: "1px solid var(--v2-border-subtle)" }}>
        <input value={filter} placeholder="filter by name…" onChange={(e) => setFilter(e.target.value)} style={inputStyle} />
      </div>

      {/* Lista */}
      <div style={{ flex: 1, overflowY: "auto", padding: "8px 12px", display: "flex", flexDirection: "column", gap: 4 }}>
        {visible.length === 0 && (
          <div style={{ fontSize: 11, color: "var(--v2-text-muted)", padding: "16px 4px", textAlign: "center", lineHeight: 1.5 }}>
            No resource in the pool{filter ? " matching that filter" : ""}.<br />
            Create one above (or while configuring a job — an unknown resource is born with capacity 1).
          </div>
        )}
        {visible.map((r) => {
          const free = r.capacity - r.used;
          return (
            <div key={r.name} style={{
              display: "flex", alignItems: "center", gap: 8, padding: "6px 8px",
              background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)", borderRadius: 3,
            }}>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ fontSize: 12, fontFamily: "var(--v2-font-mono)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>{r.name}</div>
                <div style={{ fontSize: 9.5, color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)" }}>
                  cap {r.capacity} · used {r.used} · <span style={{ color: free <= 0 ? "var(--v2-status-failed)" : "var(--v2-status-ok)" }}>free {free}</span>
                </div>
              </div>
              <input
                type="number" min={0} value={r.capacity}
                onChange={(e) => { const cap = Math.max(0, parseInt(e.target.value, 10) || 0); setItems((prev) => prev.map((x) => x.name === r.name ? { ...x, capacity: cap } : x)); }}
                onBlur={(e) => { const cap = Math.max(0, parseInt(e.target.value, 10) || 0); void setResourceCapacity(r.name, cap).then(reload).catch((err) => toast.error("Failed to adjust capacity", { detail: err instanceof Error ? err.message : String(err) })); }}
                title="adjust capacity (saves when the field loses focus)"
                style={{ ...inputStyle, width: 56, flex: "none" }}
              />
              <button onClick={() => void remove(r)} disabled={busy || r.used > 0} title={r.used > 0 ? "in use; release it first" : "Delete resource"}
                style={{ ...iconBtn, opacity: r.used > 0 ? 0.4 : 1, cursor: r.used > 0 ? "not-allowed" : "pointer" }}>
                <Trash2 size={12} />
              </button>
            </div>
          );
        })}
      </div>
    </aside>
  );
}

const inputStyle: React.CSSProperties = {
  flex: 1, background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)",
  color: "var(--v2-text-primary)", padding: "5px 8px", fontSize: 11,
  fontFamily: "var(--v2-font-mono)", borderRadius: 3, outline: "none", boxSizing: "border-box", width: "100%",
};
const btnStyle: React.CSSProperties = {
  padding: "4px 12px", background: "transparent", border: "1px solid var(--v2-border-medium)",
  borderRadius: 3, fontSize: 10, fontFamily: "var(--v2-font-mono)", cursor: "pointer",
  display: "inline-flex", alignItems: "center",
};
const iconBtn: React.CSSProperties = {
  background: "transparent", border: "none", color: "var(--v2-text-muted)",
  cursor: "pointer", padding: 2, display: "inline-flex",
};
