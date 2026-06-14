import { useEffect, useState } from "react";
import {
  LayoutGrid,
  Folder,
  Variable,
  GitBranch,
  Globe,
  Clock,
  GitFork,
  Layers,
  Zap,
  Box,
  Database,
  Workflow,
  Terminal,
  FileCode,
  Network,
  type LucideIcon,
} from "lucide-react";
import type { JobDefinition } from "@/lib/orchestrator-model";
import type { JobNodeData } from "@/lib/job-config";
import { getGitInfo } from "@/lib/git-info";
import { useResizablePanel, ResizeHandle } from "./resizable";

/* ──────────────────────────────────────────────────────────────
   DesignSidebarV2 — dockada (ancorada à esquerda, sem flutuar)
   ──────────────────────────────────────────────────────────────
   - Activity bar vertical (ícones lucide) + painel: Palette | Folders | Variables
   - Palette HONESTA: tipos sem executor real (P11 — AWS) levam badge "stub".
     Continuam arrastáveis (o server aceita e roda em dry-run), mas o usuário
     sabe o que está criando.
   - Footer com git info REAL (branch@sha do /api/git/status), não hardcode.
   ────────────────────────────────────────────────────────────── */

type Tab = "palette" | "folders" | "variables";

const JOB_TYPES: Array<{
  id: JobNodeData["jobType"];
  label: string;
  hint: string;
  Icon: LucideIcon;
  /** true = sem executor real ainda — badge "stub" na palette. */
  stub?: boolean;
}> = [
  { id: "COMMAND",       label: "Command",       hint: "Comando shell no agente (Win/Linux)",  Icon: Terminal },
  { id: "SCRIPT",        label: "Script",        hint: "Script .sh/.bat/.ps1 no agente",       Icon: FileCode },
  { id: "SSH",           label: "SSH",           hint: "Comando remoto via SSH (sem agente)",  Icon: Network },
  { id: "HTTP",          label: "HTTP",          hint: "Chamada REST com validação de status", Icon: Globe },
  { id: "WAIT",          label: "Wait",          hint: "Delay / espera programada",            Icon: Clock },
  { id: "CHOICE",        label: "Choice",        hint: "Desvio condicional",                   Icon: GitFork },
  { id: "PARALLEL",      label: "Parallel",      hint: "Execução concorrente",                 Icon: Layers },
  { id: "LAMBDA",        label: "Lambda",        hint: "Função serverless AWS (fim do roadmap)", Icon: Zap, stub: true },
  { id: "BATCH",         label: "Batch",         hint: "Container ECS/Batch (fim do roadmap)", Icon: Box, stub: true },
  { id: "GLUE",          label: "Glue",          hint: "ETL pipeline (fim do roadmap)",        Icon: Database, stub: true },
  { id: "STEP_FUNCTION", label: "Step Function", hint: "State machine (fim do roadmap)",       Icon: Workflow, stub: true },
];

export default function DesignSidebarV2({ definitions = [] }: { definitions?: JobDefinition[] }) {
  const [tab, setTab] = useState<Tab>("palette");
  const [gitLine, setGitLine] = useState<string | null>(null);

  useEffect(() => {
    let cancel = false;
    void getGitInfo().then((st) => {
      if (cancel || !st?.configured) return;
      setGitLine(`${st.branch ?? "main"}@${st.shortSha ?? "?"}`);
    });
    return () => { cancel = true; };
  }, []);

  // Agrupa definitions por folder (campo `team` na model — vocabulário legado)
  const folders = (() => {
    const m = new Map<string, number>();
    for (const d of definitions) {
      const t = (d.team ?? "").trim() || "—";
      m.set(t, (m.get(t) ?? 0) + 1);
    }
    return [...m.entries()]
      .map(([name, count]) => ({ name, count }))
      .sort((a, b) => a.name.localeCompare(b.name));
  })();

  const TABS: Array<{ id: Tab; Icon: LucideIcon; label: string }> = [
    { id: "palette",   Icon: LayoutGrid, label: "Components" },
    { id: "folders",   Icon: Folder,     label: "Folders" },
    { id: "variables", Icon: Variable,   label: "Variables" },
  ];

  const { width, onMouseDown, reset } = useResizablePanel({
    storageKey: "regente.panel.design.w", defaultWidth: 280, min: 220, max: 520, edge: "right",
  });

  return (
    <aside
      style={{
        position: "absolute",
        top: 0,
        left: 0,
        bottom: 0,
        width,
        display: "flex",
        fontFamily: "var(--v2-font-sans)",
        zIndex: 5,
        borderRight: "1px solid var(--v2-border-medium)",
        background: "var(--v2-bg-surface)",
      }}
    >
      <ResizeHandle edge="right" onMouseDown={onMouseDown} onReset={reset} />
      {/* Activity bar — coluna vertical de ícones */}
      <nav
        style={{
          width: 38,
          background: "var(--v2-bg-elevated)",
          borderRight: "1px solid var(--v2-border-subtle)",
          display: "flex",
          flexDirection: "column",
          padding: "6px 0",
          gap: 2,
          flexShrink: 0,
        }}
      >
        {TABS.map((t) => {
          const active = tab === t.id;
          const Icon = t.Icon;
          return (
            <button
              key={t.id}
              onClick={() => setTab(t.id)}
              title={t.label}
              style={{
                height: 32,
                background: "transparent",
                border: "none",
                borderLeft: `2px solid ${active ? "var(--v2-accent-brand)" : "transparent"}`,
                color: active ? "var(--v2-text-primary)" : "var(--v2-text-muted)",
                cursor: "pointer",
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                transition: "color 100ms linear",
              }}
              onMouseEnter={(e) => {
                if (!active) e.currentTarget.style.color = "var(--v2-text-secondary)";
              }}
              onMouseLeave={(e) => {
                if (!active) e.currentTarget.style.color = "var(--v2-text-muted)";
              }}
            >
              <Icon size={15} />
            </button>
          );
        })}
      </nav>

      {/* Painel de conteúdo */}
      <div
        style={{
          flex: 1,
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
          minWidth: 0,
        }}
      >
        {/* Header da aba */}
        <div
          style={{
            padding: "10px 12px",
            borderBottom: "1px solid var(--v2-border-subtle)",
            display: "flex",
            alignItems: "center",
          }}
        >
          <span
            style={{
              fontSize: 11,
              fontWeight: 600,
              letterSpacing: "0.06em",
              color: "var(--v2-text-primary)",
              textTransform: "uppercase",
            }}
          >
            {tab === "palette" ? "Components" : tab === "folders" ? "Folders" : "Variables"}
          </span>
        </div>

        {/* Conteúdo */}
        <div style={{ flex: 1, overflowY: "auto" }}>
          {tab === "palette" && (
            <div style={{ padding: "4px 0" }}>
              {JOB_TYPES.map((t) => {
                const Icon = t.Icon;
                return (
                  <div
                    key={t.id}
                    draggable
                    onDragStart={(e) => {
                      e.dataTransfer.setData("application/regente-jobtype", t.id);
                      e.dataTransfer.effectAllowed = "copy";
                    }}
                    title={t.stub ? "Sem executor real ainda (roda em dry-run). AWS executors = P11 no roadmap." : `Arraste para o canvas para criar um job ${t.label}`}
                    style={{
                      padding: "8px 12px",
                      cursor: "grab",
                      borderBottom: "1px solid var(--v2-border-subtle)",
                      display: "flex",
                      alignItems: "flex-start",
                      gap: 10,
                      transition: "background 80ms linear",
                      opacity: t.stub ? 0.75 : 1,
                    }}
                    onMouseEnter={(e) => (e.currentTarget.style.background = "var(--v2-bg-hover)")}
                    onMouseLeave={(e) => (e.currentTarget.style.background = "transparent")}
                  >
                    <span
                      style={{
                        width: 28,
                        height: 28,
                        display: "inline-flex",
                        alignItems: "center",
                        justifyContent: "center",
                        border: "1px solid var(--v2-border-strong)",
                        color: "var(--v2-text-secondary)",
                        borderRadius: 3,
                        flexShrink: 0,
                      }}
                    >
                      <Icon size={14} />
                    </span>
                    <div style={{ minWidth: 0, flex: 1 }}>
                      <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
                        <span style={{ fontSize: 12, fontWeight: 500, color: "var(--v2-text-primary)" }}>
                          {t.label}
                        </span>
                        {t.stub && (
                          <span
                            style={{
                              fontSize: 8, fontFamily: "var(--v2-font-mono)",
                              padding: "1px 5px", borderRadius: 2,
                              border: "1px solid var(--v2-border-strong)",
                              color: "var(--v2-text-muted)",
                              letterSpacing: "0.06em", textTransform: "uppercase",
                            }}
                          >
                            stub
                          </span>
                        )}
                      </div>
                      <div style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 1 }}>
                        {t.hint}
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          )}

          {tab === "folders" && (
            <div style={{ padding: "4px 0" }}>
              {folders.length === 0 && (
                <div style={{ padding: "12px 14px", fontSize: 11, color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)" }}>
                  Nenhuma folder aberta nesta session.
                </div>
              )}
              {folders.map((t) => (
                <div
                  key={t.name}
                  style={{
                    padding: "8px 12px",
                    display: "flex",
                    alignItems: "center",
                    gap: 10,
                    borderBottom: "1px solid var(--v2-border-subtle)",
                  }}
                >
                  <Folder size={12} style={{ color: "var(--v2-accent-dark)", flexShrink: 0 }} />
                  <span
                    style={{
                      flex: 1,
                      fontSize: 12,
                      color: "var(--v2-text-primary)",
                      fontFamily: "var(--v2-font-mono)",
                      letterSpacing: "0.04em",
                      overflow: "hidden",
                      textOverflow: "ellipsis",
                      whiteSpace: "nowrap",
                    }}
                  >
                    {t.name}
                  </span>
                  <span style={{ fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)" }}>
                    {t.count} job{t.count === 1 ? "" : "s"}
                  </span>
                </div>
              ))}
            </div>
          )}

          {tab === "variables" && (
            <div style={{ padding: "12px 14px", fontSize: 11, color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)", lineHeight: 1.5 }}>
              Variáveis globais são gerenciadas no Control-M Panel
              <br />
              <span style={{ opacity: 0.6 }}>(menu do usuário → Control-M → Variables)</span>
            </div>
          )}
        </div>

        {/* Footer com git info REAL (do /api/git/status) */}
        {gitLine && (
          <div
            style={{
              padding: "6px 12px",
              borderTop: "1px solid var(--v2-border-subtle)",
              fontSize: 10,
              fontFamily: "var(--v2-font-mono)",
              color: "var(--v2-text-muted)",
              display: "flex",
              alignItems: "center",
              gap: 6,
              letterSpacing: "0.04em",
            }}
          >
            <GitBranch size={11} style={{ color: "var(--v2-accent-brand)" }} />
            <span>{gitLine}</span>
          </div>
        )}
      </div>
    </aside>
  );
}
