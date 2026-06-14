/**
 * FolderOpener — combo type-ahead para abrir/criar folder ativa em Design.
 *
 * Re-alinhamento Design Fase 2 (2026-04-27).
 *
 * UX:
 * - Lista folders existentes do server (filtra por digitação).
 * - Click em folder existente → abre (POST /folders/open). Não força PR.
 * - Enter com texto novo → cria (POST /folders). Força PR no Publish.
 * - Comportamento D2: se nome digitado bate com folder existente, vira "open"
 *   automaticamente (não duplica nem dá erro).
 *
 * Requer session ativa. O caller deve garantir designSessionId !== null.
 */
import { useEffect, useMemo, useRef, useState } from "react";
import { listFolders, type FolderInfo } from "@/lib/folder-api";
import {
  openSessionFolder,
  createSessionFolder,
  createDesignSession,
  type AddFolderResult,
} from "@/lib/design-session-api";

interface Props {
  /** Session ativa, ou null — nesse caso uma session vazia será criada lazily. */
  sessionId: string | null;
  /** Folders já abertas na session — filtradas da lista para evitar duplicar. */
  alreadyActive: Set<string>;
  /** Disparado se uma session NOVA tiver sido criada lazily neste fluxo. */
  onSessionCreated?: (sid: string) => void;
  onAdded: (res: AddFolderResult) => void;
}

export function FolderOpener({ sessionId, alreadyActive, onSessionCreated, onAdded }: Props) {
  const [folders, setFolders] = useState<FolderInfo[]>([]);
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    if (!open) return;
    let cancel = false;
    listFolders()
      .then((fs) => { if (!cancel) setFolders(fs); })
      .catch((e: unknown) => { if (!cancel) setError(String((e as Error).message ?? e)); });
    return () => { cancel = true; };
  }, [open]);

  const candidates = useMemo(() => {
    const q = query.trim().toLowerCase();
    return folders
      .filter((f) => !alreadyActive.has(f.name))
      .filter((f) => !q || f.name.toLowerCase().includes(q));
  }, [folders, alreadyActive, query]);

  const exactMatch = useMemo(
    () => folders.some((f) => f.name === query.trim()),
    [folders, query],
  );

  const submit = async (action: "open" | "create", name: string) => {
    if (!name) return;
    setBusy(true);
    setError(null);
    try {
      // Lazy session: se ainda não há session, cria uma vazia agora.
      let sid = sessionId;
      if (!sid) {
        const sess = await createDesignSession([], []);
        sid = sess.id;
        onSessionCreated?.(sid);
      }
      const res = action === "open"
        ? await openSessionFolder(sid, name)
        : await createSessionFolder(sid, name);
      onAdded(res);
      setQuery("");
      setOpen(false);
    } catch (e: unknown) {
      setError(String((e as Error).message ?? e));
    } finally {
      setBusy(false);
    }
  };

  const handleEnter = () => {
    const name = query.trim();
    if (!name) return;
    // D2: se nome existe, abre; senão cria.
    void submit(exactMatch ? "open" : "create", name);
  };

  return (
    <div style={{ position: "relative", display: "inline-block" }}>
      <button
        onClick={() => { setOpen((v) => !v); setTimeout(() => inputRef.current?.focus(), 0); }}
        style={{
          padding: "5px 10px",
          background: "transparent",
          color: "var(--v2-accent-brand)",
          border: "1px solid var(--v2-accent-brand)",
          borderRadius: 4,
          cursor: "pointer",
          fontSize: 11,
          fontFamily: "var(--v2-font-mono)",
          letterSpacing: "0.04em",
        }}
      >
        + Open / Create folder
      </button>
      {open && (
        <div
          style={{
            position: "absolute",
            top: "calc(100% + 4px)",
            left: 0,
            zIndex: 1000,
            minWidth: 280,
            background: "var(--v2-bg-elevated)",
            border: "1px solid var(--v2-border-medium)",
            borderRadius: 4,
            boxShadow: "0 6px 16px rgba(0,0,0,0.4)",
            padding: 8,
          }}
        >
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") handleEnter();
              if (e.key === "Escape") setOpen(false);
            }}
            placeholder="folder name (existing or new)"
            disabled={busy}
            style={{
              width: "100%",
              boxSizing: "border-box",
              padding: "5px 8px",
              background: "var(--v2-bg-base)",
              color: "var(--v2-text-primary)",
              border: "1px solid var(--v2-border-medium)",
              borderRadius: 3,
              fontSize: 12,
              fontFamily: "var(--v2-font-mono)",
            }}
          />
          {error && (
            <div style={{ color: "#fbb", fontSize: 11, marginTop: 6 }}>{error}</div>
          )}
          <div style={{ marginTop: 6, maxHeight: 220, overflowY: "auto" }}>
            {candidates.length === 0 && query.trim() && !exactMatch && (
              <div
                onClick={() => void submit("create", query.trim())}
                style={{
                  padding: "6px 8px",
                  cursor: busy ? "wait" : "pointer",
                  color: "var(--v2-accent-brand)",
                  fontSize: 12,
                  borderBottom: "1px solid var(--v2-border-subtle)",
                }}
                title="Nova folder forçará PR no Publish"
              >
                + Create &quot;{query.trim()}&quot; (new folder, will force PR)
              </div>
            )}
            {candidates.map((f) => (
              <div
                key={f.name}
                onClick={() => void submit("open", f.name)}
                style={{
                  padding: "5px 8px",
                  cursor: busy ? "wait" : "pointer",
                  color: "var(--v2-text-primary)",
                  fontSize: 12,
                  fontFamily: "var(--v2-font-mono)",
                  display: "flex",
                  justifyContent: "space-between",
                }}
                onMouseEnter={(e) => (e.currentTarget.style.background = "var(--v2-bg-hover)")}
                onMouseLeave={(e) => (e.currentTarget.style.background = "transparent")}
              >
                <span>{f.name}</span>
                <span style={{ color: "var(--v2-text-muted)", fontSize: 10 }}>
                  {f.jobCount ?? 0} jobs
                </span>
              </div>
            ))}
            {candidates.length === 0 && !query.trim() && (
              <div style={{ padding: 8, color: "var(--v2-text-muted)", fontSize: 11 }}>
                Digite para filtrar ou criar.
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
