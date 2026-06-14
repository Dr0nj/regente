/**
 * PublishButton — encerra design session com commit+push (ou PR).
 * Etapa 4 + Etapa 5 (2026-04-26).
 *
 * - direct mode (sem newFolders): commit+push direto pra branch principal.
 * - PR mode (com newFolders): força PR mesmo se workspace estiver em direct.
 *
 * Após sucesso, limpa a session (server já delete) e o caller deve
 * setDesignSessionId(null) + recarregar a lista de defs.
 */
import { useState } from "react";
import { publishDesignSession, type PublishResult } from "@/lib/design-session-api";
import { ErrorDialog } from "./ErrorDialog";

interface Props {
  sessionId: string;
  newFolderCount: number;
  onPublished: (res: PublishResult) => void;
}

export function PublishButton({ sessionId, newFolderCount, onPublished }: Props) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [message, setMessage] = useState("");

  const handle = async () => {
    setBusy(true);
    setError(null);
    try {
      const res = await publishDesignSession(sessionId, message || undefined);
      onPublished(res);
    } catch (e: unknown) {
      setError(e);
      setBusy(false);
    }
  };

  const willForcePR = newFolderCount > 0;

  return (
    <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
      <input
        value={message}
        onChange={(e) => setMessage(e.target.value)}
        placeholder={willForcePR ? "PR title (opcional)" : "commit message (opcional)"}
        disabled={busy}
        style={{
          padding: "4px 8px", background: "#111", border: "1px solid #333",
          color: "#eee", borderRadius: 4, fontSize: 12, width: 220,
        }}
      />
      <button
        onClick={handle}
        disabled={busy}
        title={willForcePR ? `Forçará PR (${newFolderCount} folders novos)` : "Commit + push direto pra main"}
        style={{
          background: willForcePR ? "#a64" : "#2a6", color: "#fff",
          border: "none", padding: "6px 14px", borderRadius: 4,
          cursor: busy ? "wait" : "pointer", fontSize: 13, fontWeight: 600,
        }}
      >
        {busy ? "Publicando…" : willForcePR ? `Publish (PR)` : "Publish"}
      </button>
      {error != null && <ErrorDialog error={error} onClose={() => setError(null)} />}
    </div>
  );
}
