// F13.5 — Painel de auditoria de uma definição.
//
// Lista as últimas N entradas de definition_audit (save/delete) com
// ator, timestamp, branch/sha/PR. Renderiza inline (não é modal próprio).
import { useEffect, useState } from "react";
import { fetchDefinitionAudit, type DefinitionAuditEntry } from "../lib/git-api";

export function DefinitionAuditPanel({
  team,
  definitionId,
}: {
  team: string;
  definitionId: string;
}) {
  const [entries, setEntries] = useState<DefinitionAuditEntry[] | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    fetchDefinitionAudit(team, definitionId)
      .then((rows) => { if (alive) setEntries(rows); })
      .catch((e) => { if (alive) setErr(e instanceof Error ? e.message : String(e)); });
    return () => { alive = false; };
  }, [team, definitionId]);

  if (err) return <div style={{ color: "salmon", fontSize: 11 }}>audit error: {err}</div>;
  if (entries === null) return <div style={{ color: "#666", fontSize: 11 }}>loading audit…</div>;
  if (entries.length === 0) return <div style={{ color: "#666", fontSize: 11 }}>no history yet</div>;

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 4, fontFamily: "monospace", fontSize: 11 }}>
      {entries.map((e) => (
        <div
          key={e.id}
          style={{
            padding: "6px 8px",
            background: "#0a0a0a",
            border: "1px solid #1a1a1a",
            borderRadius: 4,
            color: "#a3a3a3",
          }}
        >
          <div style={{ display: "flex", gap: 8, alignItems: "baseline" }}>
            <span style={{ color: e.action === "delete" ? "#ef4444" : "#11C76F" }}>{e.action}</span>
            <span style={{ color: "#f5f5f5" }}>{e.actor}</span>
            <span style={{ marginLeft: "auto", color: "#666", fontSize: 10 }}>
              {new Date(e.ts).toLocaleString()}
            </span>
          </div>
          <div style={{ marginTop: 2, fontSize: 10, color: "#666", display: "flex", gap: 8, flexWrap: "wrap" }}>
            {e.branch && <span>br:{e.branch}</span>}
            {e.gitCommitSha && <span>sha:{e.gitCommitSha.slice(0, 7)}</span>}
            {e.prNumber && (
              <a
                href={e.prUrl || "#"}
                target="_blank"
                rel="noopener noreferrer"
                style={{ color: "#60a5fa" }}
              >
                PR #{e.prNumber}
              </a>
            )}
            {e.mode && <span style={{ color: "#444" }}>· {e.mode}</span>}
          </div>
        </div>
      ))}
    </div>
  );
}
