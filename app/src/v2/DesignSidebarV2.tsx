import { useEffect, useState } from "react";
import {
  LayoutGrid,
  Folder,
  Variable,
  GitBranch,
  Globe,
  Zap,
  Box,
  Database,
  Workflow,
  Terminal,
  FileCode,
  Network,
  FileSearch,
  ArrowRightLeft,
  type LucideIcon,
} from "lucide-react";
import type { JobDefinition } from "@/lib/orchestrator-model";
import type { JobNodeData } from "@/lib/job-config";
import { getGitInfo } from "@/lib/git-info";
import { fetchTemplates, deleteTemplate, type JobTemplate } from "@/lib/differentials-api";
import { isServerMode } from "@/lib/server-client";
import { FileStack, Trash2 } from "lucide-react";
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

type Tab = "palette" | "templates" | "folders" | "variables";

const JOB_TYPES: Array<{
  id: JobNodeData["jobType"];
  label: string;
  hint: string;
  Icon: LucideIcon;
  /** true = sem executor real ainda — badge "stub" na palette. */
  stub?: boolean;
}> = [
  { id: "COMMAND",       label: "Command",       hint: "Shell command on the agent (Win/Linux)", Icon: Terminal },
  { id: "SCRIPT",        label: "Script",        hint: ".sh/.bat/.ps1 script on the agent",      Icon: FileCode },
  { id: "SSH",           label: "SSH",           hint: "Remote command over SSH (agentless)",    Icon: Network },
  { id: "HTTP",          label: "HTTP",          hint: "REST call with status validation",       Icon: Globe },
  { id: "FILE_WATCH",    label: "File Watch",    hint: "Waits for a file on the agent",          Icon: FileSearch },
  { id: "FILE_TRANSFER", label: "File Transfer", hint: "MFT: local ↔ SFTP ↔ S3 from the agent",  Icon: ArrowRightLeft },
  { id: "DATABASE",      label: "Database",      hint: "SQL on Postgres/MySQL/SQLite from the agent", Icon: Database },
  { id: "LAMBDA",        label: "Lambda",        hint: "AWS serverless function (end of roadmap)", Icon: Zap, stub: true },
  { id: "BATCH",         label: "Batch",         hint: "ECS/Batch container (end of roadmap)",   Icon: Box, stub: true },
  { id: "GLUE",          label: "Glue",          hint: "ETL pipeline (end of roadmap)",          Icon: Database, stub: true },
  { id: "STEP_FUNCTION", label: "Step Function", hint: "State machine (end of roadmap)",         Icon: Workflow, stub: true },
];

export default function DesignSidebarV2({
  definitions = [],
  onJobClick,
  onUseTemplate,
}: {
  definitions?: JobDefinition[];
  /** Clique num job da aba Folders → centraliza/navega até o nó no canvas. */
  onJobClick?: (defId: string) => void;
  /** D-13 — clique num template → cria um job novo a partir da forma dele. */
  onUseTemplate?: (def: JobDefinition) => void;
}) {
  const [tab, setTab] = useState<Tab>("palette");
  const [gitLine, setGitLine] = useState<string | null>(null);
  const [templates, setTemplates] = useState<JobTemplate[]>([]);
  const serverMode = isServerMode();

  const loadTemplates = () => { if (serverMode) void fetchTemplates().then(setTemplates); };
  useEffect(() => { loadTemplates(); }, []); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    let cancel = false;
    void getGitInfo().then((st) => {
      if (cancel || !st?.configured) return;
      setGitLine(`${st.branch ?? "main"}@${st.shortSha ?? "?"}`);
    });
    return () => { cancel = true; };
  }, []);

  // Agrupa definitions por folder (campo `team` na model — vocabulário legado),
  // guardando os jobs de cada folder pra listar clicáveis embaixo do nome.
  const folders = (() => {
    const m = new Map<string, JobDefinition[]>();
    for (const d of definitions) {
      const t = (d.team ?? "").trim() || "—";
      const arr = m.get(t) ?? [];
      arr.push(d);
      m.set(t, arr);
    }
    return [...m.entries()]
      .map(([name, jobs]) => ({
        name,
        jobs: jobs.slice().sort((a, b) => (a.label || a.id).localeCompare(b.label || b.id)),
      }))
      .sort((a, b) => a.name.localeCompare(b.name));
  })();

  const TABS: Array<{ id: Tab; Icon: LucideIcon; label: string }> = [
    { id: "palette",   Icon: LayoutGrid, label: "Components" },
    ...(serverMode ? [{ id: "templates" as Tab, Icon: FileStack, label: "Templates" }] : []),
    { id: "folders",   Icon: Folder,     label: "Folders" },
    { id: "variables", Icon: Variable,   label: "Variables" },
  ];

  const { width, onMouseDown, reset } = useResizablePanel({
    storageKey: "regente.panel.design.w", defaultWidth: 280, min: 220, max: 520, edge: "right",
  });

  return (
    <aside
      data-canvas-inset="left"
      style={{
        position: "absolute",
        top: 10,
        left: 10,
        bottom: 10,
        width,
        display: "flex",
        fontFamily: "var(--v2-font-sans)",
        zIndex: 5,
        border: "1px solid var(--v2-border-medium)",
        borderRadius: 16,
        boxShadow: "0 10px 30px rgba(0,0,0,0.35)",
        background: "var(--v2-bg-surface)",
        overflow: "hidden",
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
                background: active ? "var(--v2-accent-deep)" : "transparent",
                border: "none",
                borderLeft: `2px solid ${active ? "var(--v2-accent-brand)" : "transparent"}`,
                boxShadow: active ? "inset 0 0 12px var(--v2-accent-glow)" : "none",
                color: active ? "var(--v2-accent-brand)" : "var(--v2-text-muted)",
                cursor: "pointer",
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                transition: "color 100ms linear, box-shadow 120ms linear",
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
            {tab === "palette" ? "Components" : tab === "templates" ? "Templates" : tab === "folders" ? "Folders" : "Variables"}
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
                    title={t.stub ? "No real executor yet (runs in dry-run). AWS executors = P11 on the roadmap." : `Drag onto the canvas to create a ${t.label} job`}
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

          {tab === "templates" && (
            <div style={{ padding: "4px 0" }}>
              {templates.length === 0 && (
                <div style={{ padding: "12px 14px", fontSize: 11, color: "var(--v2-text-muted)", lineHeight: 1.5 }}>
                  No template yet. Open a job and click
                  <br />
                  <span style={{ fontFamily: "var(--v2-font-mono)" }}>☆ Template</span> to save its shape here.
                </div>
              )}
              {templates.map((tpl) => (
                <div
                  key={tpl.name}
                  onClick={() => onUseTemplate?.({ ...tpl.definition, label: tpl.definition.label || tpl.name })}
                  title={`Create a job from "${tpl.name}" (${tpl.definition.jobType})`}
                  style={{
                    padding: "8px 12px", cursor: "pointer",
                    borderBottom: "1px solid var(--v2-border-subtle)",
                    display: "flex", alignItems: "center", gap: 10,
                  }}
                  onMouseEnter={(e) => (e.currentTarget.style.background = "var(--v2-bg-hover)")}
                  onMouseLeave={(e) => (e.currentTarget.style.background = "transparent")}
                >
                  <FileStack size={13} style={{ color: "var(--v2-accent-brand)", flexShrink: 0 }} />
                  <div style={{ minWidth: 0, flex: 1 }}>
                    <div style={{ fontSize: 12, fontWeight: 500, color: "var(--v2-text-primary)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                      {tpl.name}
                    </div>
                    <div style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                      {tpl.definition.jobType}{tpl.description ? ` · ${tpl.description}` : ""}
                    </div>
                  </div>
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      if (!window.confirm(`Delete the template "${tpl.name}"?`)) return;
                      void deleteTemplate(tpl.name).then(loadTemplates);
                    }}
                    title="Delete template"
                    style={{ background: "transparent", border: "none", color: "var(--v2-text-muted)", cursor: "pointer", padding: 2, display: "inline-flex", flexShrink: 0 }}
                  >
                    <Trash2 size={12} />
                  </button>
                </div>
              ))}
            </div>
          )}

          {tab === "folders" && (
            <div style={{ padding: "4px 0" }}>
              {folders.length === 0 && (
                <div style={{ padding: "12px 14px", fontSize: 11, color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)" }}>
                  No folder open in this session.
                </div>
              )}
              {folders.map((t) => (
                <div key={t.name} style={{ borderBottom: "1px solid var(--v2-border-subtle)" }}>
                  {/* Cabeçalho da folder */}
                  <div
                    style={{
                      padding: "8px 12px",
                      display: "flex",
                      alignItems: "center",
                      gap: 10,
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
                      {t.jobs.length} job{t.jobs.length === 1 ? "" : "s"}
                    </span>
                  </div>
                  {/* Jobs clicáveis — clique navega até o nó no canvas */}
                  {t.jobs.map((j) => (
                    <JobRow key={j.id} label={j.label || j.id} onClick={() => onJobClick?.(j.id)} />
                  ))}
                </div>
              ))}
            </div>
          )}

          {tab === "variables" && (
            <div style={{ padding: "12px 14px", fontSize: 11, color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)", lineHeight: 1.5 }}>
              Global variables are managed in the Control-M Panel
              <br />
              <span style={{ opacity: 0.6 }}>(user menu → Control-M → Variables)</span>
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

/* Linha de job clicável na aba Folders — hover destaca; clique navega até o nó. */
function JobRow({ label, onClick }: { label: string; onClick: () => void }) {
  const [hover, setHover] = useState(false);
  return (
    <button
      onClick={onClick}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      title={`Go to "${label}" on the canvas`}
      style={{
        width: "100%",
        display: "flex",
        alignItems: "center",
        gap: 8,
        padding: "5px 12px 5px 30px",
        background: hover ? "var(--v2-bg-elevated)" : "transparent",
        border: "none",
        borderLeft: `2px solid ${hover ? "var(--v2-accent-brand)" : "transparent"}`,
        cursor: "pointer",
        textAlign: "left",
      }}
    >
      <FileCode size={11} style={{ color: hover ? "var(--v2-accent-brand)" : "var(--v2-text-muted)", flexShrink: 0 }} />
      <span
        style={{
          flex: 1,
          fontSize: 11,
          color: hover ? "var(--v2-text-primary)" : "var(--v2-text-secondary)",
          fontFamily: "var(--v2-font-mono)",
          overflow: "hidden",
          textOverflow: "ellipsis",
          whiteSpace: "nowrap",
        }}
      >
        {label}
      </span>
    </button>
  );
}
