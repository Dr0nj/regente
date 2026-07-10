/**
 * CodeGuidePanel — a "aba" FIXA de referência do dialeto YAML no modo código.
 *
 * Árvore collapsible (▸ recolhido / ▾ expandido, TUDO recolhido por padrão)
 * com TODAS as tags do YAML de job (dados em code-schema.ts — espelho do
 * domain.JobDefinition do server), as formas aceitáveis de preencher cada uma
 * e exemplos prontos pra copiar. Filtro por texto no topo. Estética luxo do
 * Regente (accent do tema, serif nos títulos, zero cor hardcoded).
 */
import { useMemo, useState, type CSSProperties } from "react";
import { YAML_GUIDE, type GuideEntry } from "./code-schema";
import { toast } from "./Toast";

const mono: CSSProperties = { fontFamily: "var(--v2-font-mono)" };

/** Entry (ou descendente) casa com o filtro? Só o NOME da tag conta — não o texto/valores. */
function matches(e: GuideEntry, q: string): boolean {
  if (!q) return true;
  if (e.tag.toLowerCase().includes(q)) return true;
  return (e.children ?? []).some((c) => matches(c, q));
}

function CopyExample({ example }: { example: string }) {
  return (
    <pre
      title="Clique para copiar o exemplo"
      onClick={() => {
        void navigator.clipboard?.writeText(example).then(
          () => toast.info("Exemplo copiado"),
          () => {},
        );
      }}
      style={{
        ...mono,
        fontSize: 10.5,
        lineHeight: "16px",
        margin: "6px 0 2px",
        padding: "8px 10px",
        background: "var(--v2-bg-canvas)",
        border: "1px solid var(--v2-border-subtle)",
        borderLeft: "2px solid var(--v2-accent-brand)",
        borderRadius: 4,
        color: "var(--v2-text-secondary)",
        whiteSpace: "pre",
        overflowX: "auto",
        cursor: "copy",
      }}
    >{example}</pre>
  );
}

function Entry({ entry, depth, query }: { entry: GuideEntry; depth: number; query: string }) {
  const [open, setOpen] = useState(false);
  if (!matches(entry, query)) return null;
  // Filtro ativo força a árvore aberta (senão o achado fica escondido).
  const expanded = query ? true : open;
  return (
    <div style={{ borderBottom: depth === 0 ? "1px solid var(--v2-border-subtle)" : "none" }}>
      <div
        onClick={() => setOpen((v) => !v)}
        style={{
          display: "flex",
          alignItems: "baseline",
          gap: 6,
          padding: depth === 0 ? "7px 10px" : "5px 10px",
          paddingLeft: 10 + depth * 14,
          cursor: "pointer",
          userSelect: "none",
        }}
        onMouseEnter={(e) => (e.currentTarget.style.background = "var(--v2-bg-hover)")}
        onMouseLeave={(e) => (e.currentTarget.style.background = "transparent")}
      >
        <span
          style={{
            ...mono,
            fontSize: 9,
            width: 10,
            flexShrink: 0,
            color: expanded ? "var(--v2-accent-brand)" : "var(--v2-text-muted)",
            display: "inline-block",
            transition: "transform 80ms linear",
            transform: expanded ? "none" : "rotate(-90deg)",
          }}
        >▾</span>
        <span
          style={{
            ...mono,
            fontSize: 11,
            fontWeight: 600,
            color: expanded ? "var(--v2-accent-brand)" : "var(--v2-text-primary)",
            whiteSpace: "nowrap",
          }}
        >
          {entry.tag}
        </span>
        <span
          style={{
            fontSize: 9,
            color: "var(--v2-text-muted)",
            fontFamily: "var(--v2-font-mono)",
            letterSpacing: "0.03em",
            whiteSpace: "nowrap",
            overflow: "hidden",
            textOverflow: "ellipsis",
          }}
        >
          {entry.kind}
        </span>
      </div>
      {expanded && (
        <div style={{ padding: `0 12px 8px ${18 + depth * 14}px` }}>
          <div style={{ fontSize: 11, lineHeight: 1.5, color: "var(--v2-text-secondary)" }}>
            {entry.summary}
          </div>
          {entry.detail && (
            <div style={{ fontSize: 10.5, lineHeight: 1.55, color: "var(--v2-text-muted)", marginTop: 4 }}>
              {entry.detail}
            </div>
          )}
          {(entry.forms ?? []).length > 0 && (
            <div style={{ marginTop: 6, display: "flex", flexDirection: "column", gap: 3 }}>
              {entry.forms!.map((f) => (
                <div key={f.form} style={{ display: "flex", gap: 8, alignItems: "baseline" }}>
                  <code
                    style={{
                      ...mono,
                      fontSize: 10,
                      color: "var(--v2-accent-brand)",
                      background: "var(--v2-bg-canvas)",
                      border: "1px solid var(--v2-border-subtle)",
                      borderRadius: 3,
                      padding: "1px 5px",
                      whiteSpace: "nowrap",
                      flexShrink: 0,
                    }}
                  >{f.form}</code>
                  <span style={{ fontSize: 10.5, color: "var(--v2-text-muted)", lineHeight: 1.45 }}>{f.desc}</span>
                </div>
              ))}
            </div>
          )}
          {entry.example && <CopyExample example={entry.example} />}
          {(entry.children ?? []).length > 0 && (
            <div
              style={{
                marginTop: 6,
                borderLeft: "1px solid var(--v2-border-subtle)",
              }}
            >
              {entry.children!.map((c) => (
                <Entry key={c.tag} entry={c} depth={depth + 1} query={query} />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export default function CodeGuidePanel() {
  const [query, setQuery] = useState("");
  const q = query.trim().toLowerCase();
  const visible = useMemo(() => YAML_GUIDE.filter((e) => matches(e, q)), [q]);

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minHeight: 0,
        background: "var(--v2-bg-surface)",
        borderRight: "1px solid var(--v2-border-medium)",
      }}
    >
      {/* Cabeçalho da guia */}
      <div style={{ padding: "12px 12px 8px", borderBottom: "1px solid var(--v2-border-subtle)" }}>
        <div
          style={{
            fontFamily: "var(--v2-font-serif)",
            fontStyle: "italic",
            fontSize: 17,
            color: "var(--v2-accent-brand)",
            lineHeight: 1.1,
          }}
        >
          Guia do schema
        </div>
        <div
          style={{
            marginTop: 4,
            fontSize: 8.5,
            letterSpacing: "0.22em",
            textTransform: "uppercase",
            color: "var(--v2-text-muted)",
            fontFamily: "var(--v2-font-mono)",
          }}
        >
          todas as tags do YAML
        </div>
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Filtrar por tag…"
          style={{
            width: "100%",
            boxSizing: "border-box",
            marginTop: 8,
            background: "var(--v2-bg-canvas)",
            border: "1px solid var(--v2-border-subtle)",
            color: "var(--v2-text-primary)",
            padding: "5px 8px",
            fontSize: 11,
            borderRadius: 3,
            outline: "none",
            fontFamily: "var(--v2-font-sans)",
          }}
          onFocus={(e) => (e.currentTarget.style.borderColor = "var(--v2-accent-dark)")}
          onBlur={(e) => (e.currentTarget.style.borderColor = "var(--v2-border-subtle)")}
        />
      </div>

      {/* Árvore (tudo recolhido por padrão; filtro expande os achados) */}
      <div style={{ flex: 1, overflowY: "auto", overflowX: "hidden" }}>
        {visible.length === 0 ? (
          <div style={{ padding: 16, fontSize: 11, color: "var(--v2-text-muted)", textAlign: "center" }}>
            nada casa com esse filtro
          </div>
        ) : (
          visible.map((e) => <Entry key={e.tag} entry={e} depth={0} query={q} />)
        )}
      </div>

      <div
        style={{
          borderTop: "1px solid var(--v2-border-subtle)",
          padding: "6px 12px",
          fontSize: 9,
          fontFamily: "var(--v2-font-mono)",
          color: "var(--v2-text-muted)",
          letterSpacing: "0.04em",
        }}
      >
        clique num exemplo pra copiar
      </div>
    </div>
  );
}
