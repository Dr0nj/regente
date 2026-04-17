import { useState } from "react";
import type { JobNodeData } from "@/lib/job-config";

/* ──────────────────────────────────────────────────────────────
   DesignSidebarV2 — flutuante, robusta, criativa
   ──────────────────────────────────────────────────────────────
   Estratégia:
   - Painel flutuante à esquerda, com gap do topo/borda
   - 3 abas: Palette | Teams | Variables
   - Palette: drag source de tipos de job (ícone + label + hint)
   - Teams: lista de times, pode criar na hora
   - Variables: globais ao workflow, tipadas
   - Visual denso, tipo VS Code Activity Bar + Explorer
   ────────────────────────────────────────────────────────────── */

type Tab = "palette" | "teams" | "variables";

const JOB_TYPES: Array<{
  id: JobNodeData["jobType"];
  label: string;
  hint: string;
}> = [
  { id: "LAMBDA",        label: "Lambda",        hint: "Função serverless AWS" },
  { id: "BATCH",         label: "Batch",         hint: "Container ECS/Batch" },
  { id: "GLUE",          label: "Glue",          hint: "ETL pipeline" },
  { id: "STEP_FUNCTION", label: "Step Function", hint: "State machine" },
  { id: "CHOICE",        label: "Choice",        hint: "Desvio condicional" },
  { id: "PARALLEL",      label: "Parallel",      hint: "Execução concorrente" },
  { id: "WAIT",          label: "Wait",          hint: "Delay / espera" },
  { id: "HTTP",          label: "HTTP",          hint: "REST API call" },
];

const TEAMS = [
  { name: "DATA", count: 12 },
  { name: "FIN", count: 7 },
  { name: "PLAT", count: 5 },
  { name: "RISK", count: 3 },
];

const VARIABLES = [
  { key: "env",            value: "prod",    type: "string" },
  { key: "bucket_landing", value: "pp-land", type: "string" },
  { key: "retry_http_429", value: "true",    type: "boolean" },
];

export default function DesignSidebarV2() {
  const [tab, setTab] = useState<Tab>("palette");

  return (
    <aside
      style={{
        position: "absolute",
        top: 12,
        left: 12,
        bottom: 12,
        width: 280,
        display: "flex",
        fontFamily: "var(--v2-font-sans)",
        zIndex: 5,
      }}
    >
      {/* Activity bar — coluna vertical de ícones (38px) */}
      <nav
        style={{
          width: 38,
          background: "var(--v2-bg-elevated)",
          border: "1px solid var(--v2-border-medium)",
          borderRadius: "6px 0 0 6px",
          borderRight: "none",
          display: "flex",
          flexDirection: "column",
          padding: "6px 0",
          gap: 2,
        }}
      >
        {(
          [
            { id: "palette",   icon: "▤", label: "Components" },
            { id: "teams",     icon: "◐", label: "Teams" },
            { id: "variables", icon: "ƒ", label: "Variables" },
          ] as const
        ).map((t) => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            title={t.label}
            style={{
              height: 32,
              background: "transparent",
              border: "none",
              borderLeft: `2px solid ${tab === t.id ? "var(--v2-accent-brand)" : "transparent"}`,
              color: tab === t.id ? "var(--v2-text-primary)" : "var(--v2-text-muted)",
              fontSize: 16,
              fontFamily: "var(--v2-font-mono)",
              cursor: "pointer",
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              transition: "color 100ms linear",
            }}
            onMouseEnter={(e) => {
              if (tab !== t.id) e.currentTarget.style.color = "var(--v2-text-secondary)";
            }}
            onMouseLeave={(e) => {
              if (tab !== t.id) e.currentTarget.style.color = "var(--v2-text-muted)";
            }}
          >
            {t.icon}
          </button>
        ))}
      </nav>

      {/* Painel de conteúdo */}
      <div
        style={{
          flex: 1,
          background: "var(--v2-bg-surface)",
          border: "1px solid var(--v2-border-medium)",
          borderRadius: "0 6px 6px 0",
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
        }}
      >
        {/* Header da aba */}
        <div
          style={{
            padding: "10px 12px",
            borderBottom: "1px solid var(--v2-border-subtle)",
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
          }}
        >
          <span
            style={{
              fontSize: 11,
              fontWeight: 600,
              letterSpacing: "0.04em",
              color: "var(--v2-text-primary)",
            }}
          >
            {tab === "palette" ? "COMPONENTS" : tab === "teams" ? "TEAMS" : "VARIABLES"}
          </span>
          <button
            style={{
              background: "transparent",
              border: "1px solid var(--v2-border-medium)",
              color: "var(--v2-text-secondary)",
              padding: "2px 6px",
              borderRadius: 2,
              fontSize: 10,
              cursor: "pointer",
              fontFamily: "var(--v2-font-mono)",
            }}
          >
            +
          </button>
        </div>

        {/* Conteúdo */}
        <div style={{ flex: 1, overflowY: "auto" }}>
          {tab === "palette" && (
            <div style={{ padding: "4px 0" }}>
              {JOB_TYPES.map((t) => (
                <div
                  key={t.id}
                  draggable
                  onDragStart={(e) => {
                    e.dataTransfer.setData("application/regente-jobtype", t.id);
                    e.dataTransfer.effectAllowed = "copy";
                  }}
                  style={{
                    padding: "8px 12px",
                    cursor: "grab",
                    borderBottom: "1px solid var(--v2-border-subtle)",
                    display: "flex",
                    alignItems: "flex-start",
                    gap: 10,
                    transition: "background 80ms linear",
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
                      fontFamily: "var(--v2-font-mono)",
                      fontSize: 10,
                      borderRadius: 3,
                      flexShrink: 0,
                      letterSpacing: "0.04em",
                    }}
                  >
                    {t.id.slice(0, 2)}
                  </span>
                  <div style={{ minWidth: 0 }}>
                    <div
                      style={{
                        fontSize: 12,
                        fontWeight: 500,
                        color: "var(--v2-text-primary)",
                      }}
                    >
                      {t.label}
                    </div>
                    <div
                      style={{
                        fontSize: 10,
                        color: "var(--v2-text-muted)",
                        marginTop: 1,
                      }}
                    >
                      {t.hint}
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}

          {tab === "teams" && (
            <div style={{ padding: "4px 0" }}>
              {TEAMS.map((t) => (
                <div
                  key={t.name}
                  style={{
                    padding: "8px 12px",
                    display: "flex",
                    alignItems: "center",
                    gap: 10,
                    borderBottom: "1px solid var(--v2-border-subtle)",
                    cursor: "pointer",
                  }}
                  onMouseEnter={(e) => (e.currentTarget.style.background = "var(--v2-bg-hover)")}
                  onMouseLeave={(e) => (e.currentTarget.style.background = "transparent")}
                >
                  <span
                    style={{
                      width: 8,
                      height: 8,
                      background: "var(--v2-accent-dark)",
                      borderRadius: "50%",
                      flexShrink: 0,
                    }}
                  />
                  <span
                    style={{
                      flex: 1,
                      fontSize: 12,
                      color: "var(--v2-text-primary)",
                      fontFamily: "var(--v2-font-mono)",
                      letterSpacing: "0.04em",
                    }}
                  >
                    {t.name}
                  </span>
                  <span
                    style={{
                      fontSize: 10,
                      fontFamily: "var(--v2-font-mono)",
                      color: "var(--v2-text-muted)",
                    }}
                  >
                    {t.count} jobs
                  </span>
                </div>
              ))}
            </div>
          )}

          {tab === "variables" && (
            <div style={{ padding: "4px 0" }}>
              {VARIABLES.map((v) => (
                <div
                  key={v.key}
                  style={{
                    padding: "8px 12px",
                    borderBottom: "1px solid var(--v2-border-subtle)",
                    fontFamily: "var(--v2-font-mono)",
                    fontSize: 11,
                  }}
                >
                  <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
                    <span
                      style={{
                        color: "var(--v2-accent-brand)",
                        letterSpacing: "0.02em",
                      }}
                    >
                      ${v.key}
                    </span>
                    <span
                      style={{
                        marginLeft: "auto",
                        fontSize: 9,
                        color: "var(--v2-text-muted)",
                        padding: "1px 4px",
                        border: "1px solid var(--v2-border-subtle)",
                        borderRadius: 2,
                        letterSpacing: "0.04em",
                      }}
                    >
                      {v.type}
                    </span>
                  </div>
                  <div style={{ color: "var(--v2-text-secondary)", marginTop: 3, fontSize: 10 }}>
                    {v.value}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Footer com Git status — branding enterprise */}
        <div
          style={{
            padding: "6px 12px",
            borderTop: "1px solid var(--v2-border-subtle)",
            fontSize: 10,
            fontFamily: "var(--v2-font-mono)",
            color: "var(--v2-text-muted)",
            display: "flex",
            justifyContent: "space-between",
            letterSpacing: "0.04em",
          }}
        >
          <span>
            <span style={{ color: "var(--v2-accent-brand)" }}>⎇</span> main
          </span>
          <span>clean</span>
        </div>
      </div>
    </aside>
  );
}
