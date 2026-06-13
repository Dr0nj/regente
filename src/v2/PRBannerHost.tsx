// F13.2 — Toast/banner global para PR results.
//
// Escuta `regente:write-result` (disparado pelo ServerApiAdapter quando o
// backend devolve `{git: PRResult}`). Mostra toast canto inferior direito
// com link clicável para o PR. Auto-dismiss em 12s.
import { useEffect, useState } from "react";

interface PRDetail {
  prNumber?: number;
  prUrl?: string;
  branch?: string;
  commitSha?: string;
  mode?: string;
}

interface ToastItem {
  id: number;
  action: "save" | "delete";
  team?: string;
  defId?: string;
  pr: PRDetail;
}

export function PRBannerHost() {
  const [toasts, setToasts] = useState<ToastItem[]>([]);

  useEffect(() => {
    let counter = 0;
    function onResult(e: Event) {
      const ce = e as CustomEvent<{ action: string; team?: string; id?: string; git?: PRDetail }>;
      const d = ce.detail;
      if (!d || !d.git) return;
      const pr = d.git;
      if (!pr.prNumber && !pr.prUrl && pr.mode === "direct") return; // direct mode: no banner
      counter += 1;
      const id = counter;
      const item: ToastItem = {
        id,
        action: d.action === "delete" ? "delete" : "save",
        team: d.team,
        defId: d.id,
        pr,
      };
      setToasts((cur) => [...cur, item]);
      setTimeout(() => {
        setToasts((cur) => cur.filter((t) => t.id !== id));
      }, 12000);
    }
    window.addEventListener("regente:write-result", onResult);
    return () => window.removeEventListener("regente:write-result", onResult);
  }, []);

  if (toasts.length === 0) return null;

  return (
    <div
      style={{
        position: "fixed", right: 16, bottom: 16, zIndex: 9999,
        display: "flex", flexDirection: "column", gap: 8,
        maxWidth: 380,
      }}
    >
      {toasts.map((t) => (
        <div
          key={t.id}
          style={{
            background: "#0a0a0a",
            border: "1px solid #11C76F",
            borderRadius: 6,
            padding: "10px 14px",
            color: "#f5f5f5",
            fontSize: 12,
            fontFamily: "monospace",
            boxShadow: "0 4px 16px rgba(0,0,0,0.6)",
          }}
        >
          <div style={{ color: "#11C76F", fontWeight: 600, marginBottom: 4 }}>
            {t.pr.mode === "pr-required" ? "PR opened" : "PR opened (optional)"}
          </div>
          <div style={{ color: "#a3a3a3", marginBottom: 6 }}>
            {t.action} {t.team}/{t.defId}
          </div>
          {t.pr.prUrl ? (
            <a
              href={t.pr.prUrl}
              target="_blank"
              rel="noopener noreferrer"
              style={{ color: "#60a5fa", wordBreak: "break-all" }}
            >
              #{t.pr.prNumber} → {t.pr.prUrl}
            </a>
          ) : (
            <div style={{ color: "#a3a3a3" }}>branch {t.pr.branch}</div>
          )}
          <div style={{ color: "#666", marginTop: 4, fontSize: 10 }}>
            change visible after merge
          </div>
        </div>
      ))}
    </div>
  );
}
