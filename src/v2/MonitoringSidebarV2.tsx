import { useMemo, useState } from "react";
import type { JobNodeData } from "@/lib/job-config";
import { todayOrderDate } from "@/lib/orchestrator-model";

/* ──────────────────────────────────────────────────────────────
   MonitoringSidebarV2 — flutuante, clean, densidade alta
   ──────────────────────────────────────────────────────────────
   Princípios:
   - Painel destacado (margem 12px das bordas, rounded, shadow sutil)
   - Lista virtualizável de TODOS os jobs do dia
   - Filtro por status + search por nome/team
   - Row: 28px altura → cabe ~25 jobs em viewport 720px
   - Zero gradiente, zero glass, zero animação decorativa
   ────────────────────────────────────────────────────────────── */

export interface MonitoringJob {
  id: string;
  label: string;
  team: string;
  jobType: JobNodeData["jobType"];
  status: JobNodeData["status"];
  durationMs?: number;
  startedAt?: string;
}

type StatusFilter = "ALL" | "RUNNING" | "FAILED" | "SUCCESS" | "WAITING";

const STATUS_DOT: Record<JobNodeData["status"], string> = {
  SUCCESS: "var(--v2-status-ok)",
  RUNNING: "var(--v2-status-running)",
  FAILED: "var(--v2-status-failed)",
  WAITING: "var(--v2-status-waiting)",
  INACTIVE: "var(--v2-text-muted)",
};

function formatDuration(ms?: number): string {
  if (!ms) return "—";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${Math.floor(ms / 60000)}m${Math.floor((ms % 60000) / 1000)}s`;
}

export default function MonitoringSidebarV2({
  jobs,
  selectedId,
  onSelect,
}: {
  jobs: MonitoringJob[];
  selectedId?: string | null;
  onSelect?: (id: string) => void;
}) {
  const [filter, setFilter] = useState<StatusFilter>("ALL");
  const [query, setQuery] = useState("");
  const [internalSelected, setInternalSelected] = useState<string | null>(null);
  const selected = selectedId !== undefined ? selectedId : internalSelected;
  const handleSelect = (id: string) => {
    if (onSelect) onSelect(id);
    else setInternalSelected(id);
  };

  const counts = useMemo(() => {
    const c = { ALL: jobs.length, RUNNING: 0, FAILED: 0, SUCCESS: 0, WAITING: 0 };
    for (const j of jobs) {
      if (j.status === "RUNNING") c.RUNNING++;
      else if (j.status === "FAILED") c.FAILED++;
      else if (j.status === "SUCCESS") c.SUCCESS++;
      else if (j.status === "WAITING") c.WAITING++;
    }
    return c;
  }, [jobs]);

  const filtered = useMemo(() => {
    return jobs.filter((j) => {
      if (filter !== "ALL" && j.status !== filter) return false;
      if (query && !j.label.toLowerCase().includes(query.toLowerCase()) && !j.team.toLowerCase().includes(query.toLowerCase())) {
        return false;
      }
      return true;
    });
  }, [jobs, filter, query]);

  // Agrupa por folder (Control-M style: folders expansíveis no sidebar)
  const grouped = useMemo(() => {
    const m = new Map<string, MonitoringJob[]>();
    for (const j of filtered) {
      const f = (j.team || "—").trim() || "—";
      if (!m.has(f)) m.set(f, []);
      m.get(f)!.push(j);
    }
    return [...m.entries()].sort((a, b) => a[0].localeCompare(b[0]));
  }, [filtered]);

  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const toggleFolder = (name: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name); else next.add(name);
      return next;
    });
  };

  return (
    <aside
      className="v2-grain v2-edge-highlight"
      style={{
        position: "absolute",
        top: 12,
        left: 12,
        bottom: 12,
        width: 320,
        background: "var(--v2-bg-surface)",
        border: "1px solid var(--v2-border-medium)",
        borderRadius: 6,
        display: "flex",
        flexDirection: "column",
        fontFamily: "var(--v2-font-sans)",
        boxShadow: "0 1px 0 var(--v2-border-subtle)",
        zIndex: 5,
        overflow: "hidden",
      }}
    >
      {/* Header */}
      <div
        style={{
          padding: "10px 12px",
          borderBottom: "1px solid var(--v2-border-subtle)",
          display: "flex",
          alignItems: "center",
          gap: 8,
        }}
      >
        <span style={{ fontSize: 11, fontWeight: 600, letterSpacing: "0.04em", color: "var(--v2-text-primary)" }}>
          ACTIVE JOBS
        </span>
        <span
          style={{
            fontSize: 10,
            fontFamily: "var(--v2-font-mono)",
            color: "var(--v2-text-muted)",
            padding: "1px 5px",
            border: "1px solid var(--v2-border-subtle)",
            borderRadius: 2,
          }}
        >
          {filtered.length}/{jobs.length}
        </span>
        <span style={{ marginLeft: "auto", fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)" }}>
          {todayOrderDate()}
        </span>
      </div>

      {/* Search */}
      <div style={{ padding: "8px 12px", borderBottom: "1px solid var(--v2-border-subtle)" }}>
        <input
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Filtrar por nome ou team…"
          style={{
            width: "100%",
            background: "var(--v2-bg-canvas)",
            border: "1px solid var(--v2-border-subtle)",
            color: "var(--v2-text-primary)",
            padding: "5px 8px",
            fontSize: 11,
            fontFamily: "var(--v2-font-sans)",
            borderRadius: 3,
            outline: "none",
          }}
          onFocus={(e) => (e.currentTarget.style.borderColor = "var(--v2-accent-dark)")}
          onBlur={(e) => (e.currentTarget.style.borderColor = "var(--v2-border-subtle)")}
        />
      </div>

      {/* Filter pills */}
      <div
        style={{
          padding: "8px 12px",
          display: "flex",
          gap: 4,
          borderBottom: "1px solid var(--v2-border-subtle)",
          fontSize: 10,
          fontFamily: "var(--v2-font-mono)",
          letterSpacing: "0.04em",
        }}
      >
        {(
          [
            { key: "ALL", label: "ALL", color: "var(--v2-text-secondary)" },
            { key: "RUNNING", label: "RUN", color: "var(--v2-status-running)" },
            { key: "FAILED", label: "FAIL", color: "var(--v2-status-failed)" },
            { key: "SUCCESS", label: "OK", color: "var(--v2-status-ok)" },
            { key: "WAITING", label: "WAIT", color: "var(--v2-status-waiting)" },
          ] as const
        ).map((p) => (
          <button
            key={p.key}
            onClick={() => setFilter(p.key as StatusFilter)}
            style={{
              flex: 1,
              background: filter === p.key ? "var(--v2-bg-hover)" : "transparent",
              border: `1px solid ${filter === p.key ? "var(--v2-border-strong)" : "var(--v2-border-subtle)"}`,
              color: filter === p.key ? "var(--v2-text-primary)" : p.color,
              padding: "4px 0",
              borderRadius: 3,
              cursor: "pointer",
              fontFamily: "inherit",
              fontSize: "inherit",
              letterSpacing: "inherit",
              fontWeight: filter === p.key ? 600 : 500,
            }}
          >
            {p.label}
            <span style={{ marginLeft: 4, opacity: 0.6 }}>
              {counts[p.key as keyof typeof counts]}
            </span>
          </button>
        ))}
      </div>

      {/* List — agrupada por folder (CTM style) */}
      <div style={{ flex: 1, overflowY: "auto", overflowX: "hidden" }}>
        {filtered.length === 0 ? (
          <div style={{ padding: 20, fontSize: 11, color: "var(--v2-text-muted)", textAlign: "center" }}>
            nenhum job com esse filtro
          </div>
        ) : (
          grouped.map(([folder, jobsOfFolder]) => {
            const isCollapsed = collapsed.has(folder);
            return (
              <div key={folder}>
                {/* Folder header */}
                <div
                  onClick={() => toggleFolder(folder)}
                  style={{
                    height: 26,
                    padding: "0 10px",
                    display: "flex",
                    alignItems: "center",
                    gap: 6,
                    background: "var(--v2-bg-elevated)",
                    borderTop: "1px solid var(--v2-border-subtle)",
                    borderBottom: "1px solid var(--v2-border-subtle)",
                    cursor: "pointer",
                    userSelect: "none",
                  }}
                  onMouseEnter={(e) => (e.currentTarget.style.background = "var(--v2-bg-hover)")}
                  onMouseLeave={(e) => (e.currentTarget.style.background = "var(--v2-bg-elevated)")}
                >
                  <span
                    style={{
                      width: 10,
                      fontSize: 9,
                      color: "var(--v2-text-muted)",
                      fontFamily: "var(--v2-font-mono)",
                      transition: "transform 80ms linear",
                      transform: isCollapsed ? "rotate(-90deg)" : "none",
                      display: "inline-block",
                    }}
                  >▾</span>
                  <span
                    style={{
                      flex: 1,
                      fontSize: 10,
                      fontFamily: "var(--v2-font-mono)",
                      letterSpacing: "0.08em",
                      color: "var(--v2-text-primary)",
                      textTransform: "uppercase",
                      fontWeight: 600,
                    }}
                  >
                    {folder}
                  </span>
                  <span
                    style={{
                      fontSize: 9,
                      fontFamily: "var(--v2-font-mono)",
                      color: "var(--v2-text-muted)",
                      padding: "1px 5px",
                      border: "1px solid var(--v2-border-subtle)",
                      borderRadius: 2,
                    }}
                  >
                    {jobsOfFolder.length}
                  </span>
                </div>

                {/* Jobs do folder */}
                {!isCollapsed && jobsOfFolder.map((j) => (
                  <div
                    key={j.id}
                    onClick={() => handleSelect(j.id)}
                    style={{
                      height: 30,
                      padding: "0 12px 0 22px",
                      display: "flex",
                      alignItems: "center",
                      gap: 8,
                      borderBottom: "1px solid var(--v2-border-subtle)",
                      background: selected === j.id ? "var(--v2-accent-faint)" : "transparent",
                      borderLeft: selected === j.id ? "2px solid var(--v2-accent-brand)" : "2px solid transparent",
                      cursor: "pointer",
                      fontSize: 11,
                      transition: "background 80ms linear",
                    }}
                    onMouseEnter={(e) => {
                      if (selected !== j.id) e.currentTarget.style.background = "var(--v2-bg-hover)";
                    }}
                    onMouseLeave={(e) => {
                      if (selected !== j.id) e.currentTarget.style.background = "transparent";
                    }}
                  >
                    <span
                      style={{
                        width: 6,
                        height: 6,
                        borderRadius: "50%",
                        background: STATUS_DOT[j.status],
                        flexShrink: 0,
                        animation: j.status === "RUNNING" ? "v2-dot-pulse 1.2s ease-in-out infinite" : "none",
                      }}
                    />
                    <span
                      style={{
                        flex: 1,
                        color: "var(--v2-text-primary)",
                        whiteSpace: "nowrap",
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        fontWeight: selected === j.id ? 600 : 400,
                      }}
                    >
                      {j.label}
                    </span>
                    <span
                      style={{
                        fontFamily: "var(--v2-font-mono)",
                        fontSize: 10,
                        color: "var(--v2-text-muted)",
                        width: 48,
                        textAlign: "right",
                        flexShrink: 0,
                      }}
                    >
                      {formatDuration(j.durationMs)}
                    </span>
                  </div>
                ))}
              </div>
            );
          })
        )}
      </div>

      {/* Footer summary */}
      <div
        style={{
          borderTop: "1px solid var(--v2-border-subtle)",
          padding: "6px 12px",
          display: "flex",
          justifyContent: "space-between",
          fontSize: 10,
          fontFamily: "var(--v2-font-mono)",
          color: "var(--v2-text-muted)",
          letterSpacing: "0.04em",
        }}
      >
        <span>NEXT TICK +23s</span>
        <span style={{ color: "var(--v2-accent-brand)" }}>● live</span>
      </div>
    </aside>
  );
}
