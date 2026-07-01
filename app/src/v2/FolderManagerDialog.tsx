/**
 * FolderManagerDialog — a tela ÚNICA de folders (botão "FOLDERS" da topbar).
 *
 * Estética "Luxury Dashboard" (pedido do usuário, 2026-06-30): contraste fundo
 * ultra-dark × accent do tema, títulos em SERIF (Playfair via --v2-font-serif),
 * dados em mono/sans. Cards de folder como "ativos serializados" (ID REG-xxxx +
 * LED de status), hover que eleva + destaca a borda, MULTI-SELEÇÃO com action bar
 * flutuante (fade-in + slide-up), e stats macro no topo.
 *
 * IMPORTANTE — tema: NADA de cor hardcoded. O "dourado" do mock = `--v2-accent-brand`
 * (verde no default, ouro nos temas Brasil, etc.). Trocar o tema troca o acento.
 *
 * Faz tudo: criar ("+ Nova pasta"), abrir/fechar no Design (multi-select →
 * activeFolders), arquivar, excluir (confirm), renomear (hover), e o filtro de
 * visibilidade do monitor (eye por card). open/create routam pelo caller (V2Preview)
 * que mantém a semântica de design-session + PR.
 */
import { useEffect, useMemo, useState, useCallback } from "react";
import {
  type FolderInfo,
  listFolders,
  renameFolder,
  deleteFolder,
  archiveFolder,
} from "@/lib/folder-api";
import { isServerMode } from "@/lib/server-client";
import type { PublishResult } from "@/lib/design-session-api";
import { PublishButton } from "./PublishButton";
import {
  Folder, Eye, EyeOff, Pencil, Check,
  Plus, X, Trash2, Archive, FolderOpen,
} from "lucide-react";

interface Props {
  visibleFolders: Set<string> | null;
  onChangeVisible: (next: Set<string> | null) => void;
  /** Folders abertas no working set do Design (multi-select). */
  activeFolders: Set<string>;
  /** Abre uma folder existente no Design (ativa). */
  onOpenFolder: (name: string) => Promise<void>;
  /** Cria uma folder nova e já abre (força PR no Publish). */
  onCreateFolder: (name: string) => Promise<void>;
  /** Fecha uma folder do working set (só visão; não toca no Git). */
  onCloseFolder: (name: string) => void;
  /** Design-session ativa (null = sem alterações pendentes). */
  sessionId: string | null;
  /** Quantas folders novas pendentes (força PR no publish). */
  newFolderCount: number;
  /** Publica a session (commit/PR). Fecha a session ao concluir. */
  onPublished: (res: PublishResult) => void;
  /** Descarta a session (perde edições não publicadas). */
  onDiscardSession: () => void;
  /** Já estamos no modo Design? Ajusta o texto do botão de abrir ("Abrir" vs "Abrir no Design"). */
  inDesignMode: boolean;
  /** Chamado após abrir folder(s) selecionadas com sucesso — parent fecha o modal e vai pro Design. */
  onOpened: () => void;
  onClose: () => void;
}

// ID determinístico "de ativo" a partir do nome (estável entre renders/sessões).
function assetId(name: string): string {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
  return `REG-${1000 + (h % 9000)}`;
}

export default function FolderManagerDialog({
  visibleFolders,
  onChangeVisible,
  activeFolders,
  onOpenFolder,
  onCreateFolder,
  onCloseFolder,
  sessionId,
  newFolderCount,
  onPublished,
  onDiscardSession,
  inDesignMode,
  onOpened,
  onClose,
}: Props) {
  const [folders, setFolders] = useState<FolderInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [err, setErr] = useState<string | null>(null);
  const [renaming, setRenaming] = useState<{ from: string; to: string } | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<{ names: string[]; typed: string } | null>(null);
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  const refresh = useCallback(async () => {
    setLoading(true);
    setErr(null);
    try {
      setFolders(await listFolders());
    } catch (e: unknown) {
      setErr((e as Error).message ?? "failed to load folders");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void refresh(); }, [refresh]);

  // ESC fecha (ou cancela seleção/criação primeiro).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      if (creating) { setCreating(false); return; }
      if (selected.size > 0) { setSelected(new Set()); return; }
      onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose, creating, selected.size]);

  /* ── derivados ── */
  const existingNames = useMemo(() => new Set(folders.map((f) => f.name)), [folders]);
  const totalJobs = useMemo(() => folders.reduce((a, f) => a + (f.jobCount ?? 0), 0), [folders]);
  const archivedCount = useMemo(() => folders.filter((f) => f.archived).length, [folders]);
  const allVisible = visibleFolders === null;

  /* ── seleção ── */
  const toggleSelect = useCallback((name: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name); else next.add(name);
      return next;
    });
  }, []);
  const selectAll = useCallback(() => setSelected(new Set(folders.map((f) => f.name))), [folders]);
  const clearSelection = useCallback(() => setSelected(new Set()), []);

  /* ── criar ── */
  const handleCreate = useCallback(async () => {
    const name = newName.trim();
    if (!name || busy) return;
    setBusy(`create:${name}`);
    setErr(null);
    try {
      if (existingNames.has(name)) await onOpenFolder(name); // já existe → abre
      else await onCreateFolder(name);
      setNewName("");
      setCreating(false);
      await refresh();
    } catch (e: unknown) {
      setErr((e as Error).message ?? "create failed");
    } finally {
      setBusy(null);
    }
  }, [newName, busy, existingNames, onOpenFolder, onCreateFolder, refresh]);

  /* ── renomear ── */
  const handleRename = useCallback(async () => {
    if (!renaming) return;
    const to = renaming.to.trim();
    if (!to || to === renaming.from) { setRenaming(null); return; }
    try {
      await renameFolder(renaming.from, to);
      if (visibleFolders?.has(renaming.from)) {
        const next = new Set(visibleFolders);
        next.delete(renaming.from); next.add(to);
        onChangeVisible(next);
      }
      setRenaming(null);
      await refresh();
    } catch (e: unknown) {
      setErr(`Rename: ${(e as Error).message}`);
    }
  }, [renaming, visibleFolders, onChangeVisible, refresh]);

  /* ── abrir/fechar selecionadas ── */
  const openSelected = useCallback(async () => {
    const targets = [...selected].filter((n) => {
      const f = folders.find((x) => x.name === n);
      return f && !f.archived && !activeFolders.has(n);
    });
    if (targets.length === 0) return;
    setBusy("open-bulk");
    try {
      for (const n of targets) await onOpenFolder(n);
      clearSelection();
      onOpened();
    } catch (e: unknown) {
      setErr((e as Error).message ?? "open failed");
    } finally { setBusy(null); }
  }, [selected, folders, activeFolders, onOpenFolder, clearSelection, onOpened]);

  const closeSelected = useCallback(() => {
    for (const n of selected) if (activeFolders.has(n)) onCloseFolder(n);
    clearSelection();
  }, [selected, activeFolders, onCloseFolder, clearSelection]);

  /* ── arquivar selecionadas ── */
  const archiveSelected = useCallback(async () => {
    const targets = [...selected].filter((n) => !folders.find((x) => x.name === n)?.archived);
    if (targets.length === 0) return;
    setBusy("archive-bulk");
    try {
      for (const n of targets) await archiveFolder(n);
      clearSelection();
      await refresh();
    } catch (e: unknown) {
      setErr(`Archive: ${(e as Error).message}`);
    } finally { setBusy(null); }
  }, [selected, folders, clearSelection, refresh]);

  /* ── excluir selecionadas (confirm) ── */
  const delJobsTotal = useMemo(() => {
    if (!confirmDelete) return 0;
    return confirmDelete.names.reduce((a, n) => a + (folders.find((x) => x.name === n)?.jobCount ?? 0), 0);
  }, [confirmDelete, folders]);

  const handleDelete = useCallback(async () => {
    if (!confirmDelete) return;
    if (delJobsTotal > 0 && confirmDelete.typed.trim().toUpperCase() !== "EXCLUIR") return;
    setBusy("delete-bulk");
    try {
      for (const n of confirmDelete.names) {
        const f = folders.find((x) => x.name === n);
        await deleteFolder(n, (f?.jobCount ?? 0) > 0);
        if (visibleFolders?.has(n)) {
          const next = new Set(visibleFolders); next.delete(n); onChangeVisible(next);
        }
      }
      setConfirmDelete(null);
      clearSelection();
      await refresh();
    } catch (e: unknown) {
      setErr(`Delete: ${(e as Error).message}`);
    } finally { setBusy(null); }
  }, [confirmDelete, delJobsTotal, folders, visibleFolders, onChangeVisible, clearSelection, refresh]);

  /* ── visibilidade (filtro do monitor) ── */
  const toggleVisible = useCallback((name: string) => {
    const allNames = folders.map((f) => f.name);
    const current = visibleFolders ?? new Set(allNames);
    const next = new Set(current);
    if (next.has(name)) next.delete(name); else next.add(name);
    if (next.size === allNames.length && allNames.every((n) => next.has(n))) onChangeVisible(null);
    else onChangeVisible(next);
  }, [folders, visibleFolders, onChangeVisible]);

  const acc = "var(--v2-accent-brand)";
  const selectedHasOpen = [...selected].some((n) => activeFolders.has(n));
  const selectedHasClosed = [...selected].some((n) => {
    const f = folders.find((x) => x.name === n);
    return f && !f.archived && !activeFolders.has(n);
  });

  return (
    <div
      onClick={onClose}
      style={{
        position: "fixed", inset: 0, background: "rgba(0,0,0,0.78)",
        backdropFilter: "blur(3px)",
        display: "flex", alignItems: "center", justifyContent: "center",
        zIndex: 100,
      }}
    >
      {/* CSS local: hover-reveal das ações secundárias + lift do card. */}
      <style>{`
        .fmd-card { transition: transform .35s cubic-bezier(.4,0,.2,1), border-color .3s, box-shadow .3s, background .3s; }
        .fmd-card:hover { transform: translateY(-4px); border-color: var(--v2-accent-brand); box-shadow: 0 12px 30px rgba(0,0,0,.45); }
        .fmd-card:hover .fmd-hoveract { opacity: 1; }
        .fmd-hoveract { opacity: 0; transition: opacity .25s; }
        .fmd-iconbtn:hover { color: var(--v2-accent-brand) !important; border-color: var(--v2-accent-brand) !important; }
        @keyframes fmdBarIn { from { opacity: 0; transform: translate(-50%, 24px); } to { opacity: 1; transform: translate(-50%, 0); } }
        @keyframes fmdCardIn { from { opacity: 0; transform: translateY(10px); } to { opacity: 1; transform: translateY(0); } }
      `}</style>

      <div
        onClick={(e) => e.stopPropagation()}
        className="v2-grain v2-edge-highlight"
        style={{
          width: "min(1240px, 95vw)",
          height: "min(88vh, 900px)",
          background: "var(--v2-bg-surface)",
          border: "1px solid var(--v2-border-medium)",
          borderRadius: 8,
          display: "flex", flexDirection: "column",
          position: "relative", isolation: "isolate",
          // Playfair renderiza dígitos como oldstyle (0/1 na altura de minúscula →
          // parecem menores que as maiúsculas). Força LINING figures (altura de caixa
          // alta) — herdado por todos os títulos/stats serif do modal.
          fontVariantNumeric: "lining-nums",
          fontFeatureSettings: '"lnum" 1, "pnum" 1',
        }}
      >
        {/* ── Header ── */}
        <div style={{ padding: "22px 28px 0", position: "relative", zIndex: 2 }}>
          <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-end" }}>
            <div>
              <h1 style={{
                fontFamily: "var(--v2-font-serif)", fontStyle: "italic",
                fontSize: 38, lineHeight: 1, margin: 0, color: acc, fontWeight: 600,
              }}>Folders</h1>
              <div style={{
                marginTop: 8, fontSize: 10, letterSpacing: "0.28em", textTransform: "uppercase",
                color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)",
              }}>Workspace Orchestrator</div>
            </div>
            <div style={{ display: "flex", gap: 10, alignItems: "center" }}>
              <button
                onClick={selectAll}
                style={luxBtn(false)}
              >Selecionar tudo</button>
              <button
                onClick={() => { setCreating(true); setNewName(""); }}
                style={luxBtn(true, acc)}
              ><Plus size={12} style={{ marginRight: 4, verticalAlign: "-2px" }} />Nova pasta</button>
              <button
                onClick={onClose}
                aria-label="Close"
                style={{
                  marginLeft: 4, width: 30, height: 30, display: "grid", placeItems: "center",
                  background: "transparent", border: "1px solid var(--v2-border-medium)",
                  color: "var(--v2-text-secondary)", borderRadius: 4, cursor: "pointer",
                }}
                className="fmd-iconbtn"
              ><X size={15} /></button>
            </div>
          </div>

          {/* gold-line */}
          <div style={{
            height: 1, margin: "20px 0 0",
            background: `linear-gradient(90deg, transparent, ${acc}, transparent)`,
            opacity: 0.35,
          }} />
        </div>

        {/* ── Stats (números reais) ── */}
        <div style={{
          display: "grid", gridTemplateColumns: "repeat(4, 1fr)",
          padding: "18px 28px 16px", position: "relative", zIndex: 2,
        }}>
          {([
            ["Total", String(folders.length), false],
            ["Active Jobs", String(totalJobs), true],
            ["Abertas no Design", String(activeFolders.size), false],
            ["Arquivadas", String(archivedCount), false],
          ] as const).map(([label, value, gold], i) => (
            <div key={label} style={{
              textAlign: "center",
              borderLeft: i === 0 ? "none" : "1px solid var(--v2-border-subtle)",
            }}>
              <div style={{
                fontSize: 9, letterSpacing: "0.22em", textTransform: "uppercase",
                color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)", marginBottom: 4,
              }}>{label}</div>
              <div style={{
                fontFamily: "var(--v2-font-serif)", fontSize: 26, lineHeight: 1,
                color: gold ? acc : "var(--v2-text-primary)",
              }}>{value}</div>
            </div>
          ))}
        </div>

        {/* ── Body: grid de cards ── */}
        <div style={{ flex: 1, overflow: "auto", padding: "8px 28px 28px", position: "relative", zIndex: 2 }}>
          {!isServerMode() && (
            <Empty>Folder management requires server mode.<br />Set <code style={{ color: acc }}>VITE_REGENTE_SERVER_URL</code> and reload.</Empty>
          )}
          {isServerMode() && loading && <Empty>Carregando…</Empty>}
          {isServerMode() && err && (
            <div style={{ padding: 12, background: "rgba(120,20,20,.25)", border: "1px solid #7f1d1d", color: "#fca5a5", borderRadius: 4, fontSize: 11, marginBottom: 12 }}>{err}</div>
          )}
          {isServerMode() && !loading && !err && folders.length === 0 && !creating && (
            <Empty>Nenhuma folder ainda. Clique em <strong style={{ color: acc }}>+ Nova pasta</strong>.</Empty>
          )}

          <div style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fill, minmax(232px, 1fr))",
            gap: 18,
          }}>
            {/* card de criação inline */}
            {creating && (
              <div className="fmd-card" style={{
                ...cardBase(acc), borderStyle: "dashed", borderColor: acc,
                animation: "fmdCardIn .35s ease-out", justifyContent: "center", gap: 10,
              }}>
                <div style={{ fontSize: 10, letterSpacing: "0.18em", textTransform: "uppercase", color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)" }}>Nova folder</div>
                <input
                  autoFocus value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  onKeyDown={(e) => { if (e.key === "Enter") void handleCreate(); if (e.key === "Escape") setCreating(false); }}
                  placeholder="nome (ou existente p/ abrir)"
                  disabled={!!busy}
                  style={{
                    width: "100%", boxSizing: "border-box", padding: "8px 10px",
                    background: "var(--v2-bg-canvas)", border: `1px solid ${acc}`,
                    color: "var(--v2-text-primary)", borderRadius: 4,
                    fontSize: 13, fontFamily: "var(--v2-font-serif)",
                  }}
                />
                <div style={{ display: "flex", gap: 8 }}>
                  <button onClick={() => void handleCreate()} disabled={!newName.trim() || !!busy} style={{ ...luxBtn(true, acc), flex: 1, justifyContent: "center" }}>
                    {existingNames.has(newName.trim()) ? "Abrir" : "Criar"}
                  </button>
                  <button onClick={() => setCreating(false)} style={{ ...luxBtn(false), flex: 1, justifyContent: "center" }}>Cancelar</button>
                </div>
              </div>
            )}

            {folders.map((f) => {
              const isSel = selected.has(f.name);
              const isOpen = activeFolders.has(f.name);
              const isVisible = allVisible || (visibleFolders?.has(f.name) ?? false);
              const isRenaming = renaming?.from === f.name;
              const active = (f.jobCount ?? 0) > 0;
              return (
                <div
                  key={f.name}
                  className="fmd-card"
                  onClick={() => !isRenaming && toggleSelect(f.name)}
                  style={{
                    ...cardBase(acc),
                    cursor: "pointer",
                    opacity: f.archived ? 0.5 : 1,
                    borderColor: isSel || isOpen ? acc : "var(--v2-border-medium)",
                    background: isSel ? "color-mix(in srgb, var(--v2-accent-brand) 5%, var(--v2-bg-elevated))" : "var(--v2-bg-elevated)",
                  }}
                >
                  {/* check de seleção */}
                  {isSel && (
                    <div style={{ position: "absolute", top: 10, right: 10, color: acc, zIndex: 3 }}><Check size={14} strokeWidth={3} /></div>
                  )}

                  {/* topo: ícone + ID + ações hover */}
                  <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", marginBottom: 18 }}>
                    <div style={{
                      width: 34, height: 34, borderRadius: "50%",
                      border: `1px solid color-mix(in srgb, var(--v2-accent-brand) 30%, transparent)`,
                      display: "grid", placeItems: "center", color: acc,
                    }}>
                      <Folder size={16} strokeWidth={1.5} />
                    </div>
                    <div className="fmd-hoveract" style={{ display: "flex", gap: 6, alignItems: "center" }} onClick={(e) => e.stopPropagation()}>
                      <button
                        title={isVisible ? "Visível no monitor — ocultar" : "Oculto no monitor — mostrar"}
                        onClick={() => toggleVisible(f.name)}
                        className="fmd-iconbtn"
                        style={iconBtn()}
                      >{isVisible ? <Eye size={13} /> : <EyeOff size={13} />}</button>
                      {!f.archived && (
                        <button title="Renomear" onClick={() => setRenaming({ from: f.name, to: f.name })} className="fmd-iconbtn" style={iconBtn()}><Pencil size={12} /></button>
                      )}
                    </div>
                  </div>

                  {/* nome (serif) */}
                  {isRenaming ? (
                    <input
                      autoFocus value={renaming.to}
                      onClick={(e) => e.stopPropagation()}
                      onChange={(e) => setRenaming({ from: renaming.from, to: e.target.value })}
                      onKeyDown={(e) => { if (e.key === "Enter") void handleRename(); else if (e.key === "Escape") setRenaming(null); }}
                      onBlur={() => void handleRename()}
                      style={{
                        width: "100%", boxSizing: "border-box", padding: "2px 6px", marginBottom: 6,
                        background: "var(--v2-bg-canvas)", border: `1px solid ${acc}`,
                        color: "var(--v2-text-primary)", borderRadius: 3,
                        fontSize: 19, fontFamily: "var(--v2-font-serif)",
                      }}
                    />
                  ) : (
                    <h3 style={{
                      margin: "0 0 8px", fontFamily: "var(--v2-font-serif)", fontSize: 20, fontWeight: 500,
                      color: "var(--v2-text-primary)", whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis",
                    }}>{f.name}</h3>
                  )}

                  {/* status + badges */}
                  <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8, flexWrap: "wrap" }}>
                    <span style={{ width: 6, height: 6, borderRadius: "50%", background: active ? "#3fb950" : "var(--v2-text-disabled)" }} />
                    <span style={{ fontSize: 9, letterSpacing: "0.18em", textTransform: "uppercase", color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)" }}>
                      {active ? "Active" : "Idle"}
                    </span>
                    {isOpen && (
                      <span style={{ fontSize: 8, fontWeight: 700, letterSpacing: "0.1em", textTransform: "uppercase", color: "#000", background: acc, padding: "1px 5px", borderRadius: 2 }}>open</span>
                    )}
                    {f.archived && (
                      <span style={{ fontSize: 8, letterSpacing: "0.1em", textTransform: "uppercase", color: "var(--v2-text-muted)", border: "1px solid var(--v2-border-medium)", padding: "1px 4px", borderRadius: 2 }}>archived</span>
                    )}
                  </div>

                  {/* rodapé */}
                  <div style={{
                    marginTop: "auto", paddingTop: 14, borderTop: "1px solid var(--v2-border-subtle)",
                    display: "flex", justifyContent: "space-between", alignItems: "center",
                  }}>
                    <span style={{ fontSize: 10, fontStyle: "italic", color: "var(--v2-text-secondary)", fontFamily: "var(--v2-font-sans)" }}>
                      {f.jobCount ?? 0} {(f.jobCount ?? 0) === 1 ? "job" : "jobs"}
                    </span>
                    <span style={{ fontSize: 8, letterSpacing: "0.05em", color: "var(--v2-text-disabled)", fontFamily: "var(--v2-font-mono)" }}>{assetId(f.name)}</span>
                  </div>
                </div>
              );
            })}
          </div>
        </div>

        {/* ── Footer de commit: aparece quando há sessão (alterações pendentes).
              O commit fica DENTRO da tela de Folders, não escondido na topbar. ── */}
        {sessionId && (
          <div style={{
            borderTop: `1px solid color-mix(in srgb, var(--v2-accent-brand) 35%, var(--v2-border-medium))`,
            background: "var(--v2-bg-canvas)",
            padding: "12px 28px", position: "relative", zIndex: 5,
            display: "flex", alignItems: "center", gap: 16,
          }}>
            <div style={{ display: "flex", flexDirection: "column", gap: 2 }}>
              <span style={{ fontSize: 11, color: acc, fontWeight: 600, fontFamily: "var(--v2-font-mono)", letterSpacing: "0.06em", textTransform: "uppercase" }}>
                Alterações não publicadas
              </span>
              <span style={{ fontSize: 10, color: "var(--v2-text-muted)" }}>
                {newFolderCount > 0
                  ? `${newFolderCount} folder(s) nova(s) — Publish abre um PR`
                  : "Edições na sessão — Publish faz commit + push"}
              </span>
            </div>
            <div style={{ flex: 1 }} />
            <PublishButton
              sessionId={sessionId}
              newFolderCount={newFolderCount}
              onPublished={(res) => { onPublished(res); void refresh(); }}
            />
            <button onClick={onDiscardSession} style={{ ...luxBtn(false), borderColor: "#7f1d1d", color: "#f87171" }}>Descartar</button>
          </div>
        )}

        {/* ── Action bar flutuante ── */}
        {selected.size > 0 && (
          <div style={{
            position: "absolute", bottom: sessionId ? 88 : 24, left: "50%",
            background: "var(--v2-bg-canvas)", border: `1px solid color-mix(in srgb, var(--v2-accent-brand) 50%, var(--v2-border-medium))`,
            padding: "12px 24px", borderRadius: 999, zIndex: 20,
            boxShadow: "0 16px 40px rgba(0,0,0,.6)",
            display: "flex", alignItems: "center", gap: 18,
            animation: "fmdBarIn .3s cubic-bezier(.4,0,.2,1)",
          }}>
            <span style={{ color: acc, fontSize: 12, fontWeight: 500 }}>
              {selected.size} {selected.size === 1 ? "selecionada" : "selecionadas"}
            </span>
            <Sep />
            {selectedHasClosed && <BarBtn primary onClick={() => void openSelected()} disabled={!!busy} icon={<FolderOpen size={14} />}>{inDesignMode ? "Abrir" : "Abrir no Design"}</BarBtn>}
            {selectedHasOpen && <BarBtn onClick={closeSelected} disabled={!!busy} icon={<X size={12} />}>Fechar</BarBtn>}
            <BarBtn onClick={() => void archiveSelected()} disabled={!!busy} icon={<Archive size={12} />}>Arquivar</BarBtn>
            <BarBtn onClick={() => setConfirmDelete({ names: [...selected], typed: "" })} disabled={!!busy} danger icon={<Trash2 size={12} />}>Excluir</BarBtn>
            <Sep />
            <BarBtn onClick={clearSelection} muted>Cancelar</BarBtn>
          </div>
        )}
      </div>

      {/* ── Confirm delete ── */}
      {confirmDelete && (
        <div
          onClick={(e) => { e.stopPropagation(); setConfirmDelete(null); }}
          style={{ position: "fixed", inset: 0, background: "rgba(0,0,0,0.85)", display: "flex", alignItems: "center", justifyContent: "center", zIndex: 130 }}
        >
          <div onClick={(e) => e.stopPropagation()} style={{ width: "min(440px, 92vw)", padding: 22, background: "var(--v2-bg-surface)", border: "1px solid #7f1d1d", borderRadius: 6 }}>
            <div style={{ fontFamily: "var(--v2-font-serif)", fontSize: 18, color: "#fca5a5", marginBottom: 10 }}>
              Excluir {confirmDelete.names.length} {confirmDelete.names.length === 1 ? "folder" : "folders"}?
            </div>
            {delJobsTotal > 0 ? (
              <>
                <div style={{ fontSize: 11, color: "var(--v2-text-secondary)", marginBottom: 10, lineHeight: 1.5 }}>
                  Contêm <strong style={{ color: "var(--v2-text-primary)" }}>{delJobsTotal} jobs</strong> no total. Digite <strong>EXCLUIR</strong> para confirmar.
                </div>
                <input
                  autoFocus value={confirmDelete.typed}
                  onChange={(e) => setConfirmDelete({ ...confirmDelete, typed: e.target.value })}
                  onKeyDown={(e) => { if (e.key === "Enter") void handleDelete(); }}
                  placeholder="EXCLUIR"
                  style={{ width: "100%", boxSizing: "border-box", padding: "7px 10px", marginBottom: 12, background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-medium)", color: "var(--v2-text-primary)", borderRadius: 3, fontSize: 12, fontFamily: "var(--v2-font-mono)" }}
                />
              </>
            ) : (
              <div style={{ fontSize: 11, color: "var(--v2-text-secondary)", marginBottom: 12 }}>
                {confirmDelete.names.length === 1 ? "A folder está vazia." : "Folders vazias."} Esta ação não pode ser desfeita.
              </div>
            )}
            <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
              <button onClick={() => setConfirmDelete(null)} style={luxBtn(false)}>Cancelar</button>
              <button
                onClick={() => void handleDelete()}
                disabled={delJobsTotal > 0 && confirmDelete.typed.trim().toUpperCase() !== "EXCLUIR"}
                style={{ padding: "6px 16px", background: "#7f1d1d", border: "1px solid #991b1b", color: "#fee2e2", borderRadius: 4, fontSize: 10, fontFamily: "var(--v2-font-mono)", letterSpacing: "0.08em", textTransform: "uppercase", fontWeight: 700, cursor: "pointer" }}
              >Excluir</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

/* ── helpers de estilo/UI ── */
function cardBase(_acc: string): React.CSSProperties {
  return {
    padding: 18, borderRadius: 6, minHeight: 168,
    background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-medium)",
    display: "flex", flexDirection: "column", position: "relative", isolation: "isolate",
  };
}
function luxBtn(filled: boolean, acc = "var(--v2-accent-brand)"): React.CSSProperties {
  return {
    display: "inline-flex", alignItems: "center",
    padding: "6px 16px", borderRadius: 4,
    border: `1px solid ${acc}`,
    background: filled ? acc : "transparent",
    color: filled ? "#000" : acc,
    fontSize: 10, fontWeight: 600, letterSpacing: "0.1em", textTransform: "uppercase",
    fontFamily: "var(--v2-font-mono)", cursor: "pointer",
  };
}
function iconBtn(): React.CSSProperties {
  return {
    width: 24, height: 24, display: "grid", placeItems: "center",
    background: "transparent", border: "1px solid var(--v2-border-medium)",
    color: "var(--v2-text-secondary)", borderRadius: 4, cursor: "pointer",
  };
}
function Empty({ children }: { children: React.ReactNode }) {
  return <div style={{ padding: 36, textAlign: "center", color: "var(--v2-text-muted)", fontSize: 12, lineHeight: 1.6 }}>{children}</div>;
}
function Sep() {
  return <div style={{ width: 1, height: 16, background: "var(--v2-border-medium)" }} />;
}
function BarBtn({ children, onClick, disabled, danger, muted, primary, icon }: {
  children: React.ReactNode; onClick: () => void; disabled?: boolean; danger?: boolean; muted?: boolean; primary?: boolean; icon?: React.ReactNode;
}) {
  const [hover, setHover] = useState(false);
  // Primary = ação principal (Abrir): preenchido no accent, maior e com glow.
  if (primary) {
    return (
      <button
        onClick={onClick} disabled={disabled}
        onMouseEnter={() => setHover(true)} onMouseLeave={() => setHover(false)}
        style={{
          display: "inline-flex", alignItems: "center", gap: 7,
          background: "var(--v2-accent-brand)", border: "none", borderRadius: 999,
          cursor: disabled ? "wait" : "pointer",
          color: "var(--v2-bg-canvas)", opacity: disabled ? 0.5 : 1,
          fontSize: 12, fontWeight: 800, letterSpacing: "0.1em", textTransform: "uppercase",
          fontFamily: "var(--v2-font-mono)", padding: "8px 18px",
          boxShadow: hover
            ? "0 0 0 1px var(--v2-accent-brand), 0 0 20px var(--v2-accent-glow)"
            : "0 0 14px var(--v2-accent-glow)",
          transform: hover ? "translateY(-1px)" : "none",
          transition: "box-shadow .2s, transform .2s",
        }}
      >{icon}{children}</button>
    );
  }
  const color = danger ? "#f87171" : muted ? "var(--v2-text-muted)" : hover ? "var(--v2-accent-brand)" : "var(--v2-text-secondary)";
  return (
    <button
      onClick={onClick} disabled={disabled}
      onMouseEnter={() => setHover(true)} onMouseLeave={() => setHover(false)}
      style={{
        display: "inline-flex", alignItems: "center", gap: 6,
        background: "transparent", border: "none", cursor: disabled ? "wait" : "pointer",
        color, opacity: disabled ? 0.5 : 1,
        fontSize: 10, fontWeight: 700, letterSpacing: "0.1em", textTransform: "uppercase",
        fontFamily: "var(--v2-font-mono)", transition: "color .2s",
      }}
    >{icon}{children}</button>
  );
}
