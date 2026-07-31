/**
 * CodeModeView — modo CÓDIGO do Design (jobs as code).
 *
 * Substitui o canvas central por um editor YAML do working set (folders
 * abertas da design session): o dev cria/edita jobs como código, no MESMO
 * dialeto dos arquivos do workspace Git. Fluxo: editar → lint AO VIVO
 * (debounce, dry-run no server) → Aplicar (transacional por item; delete
 * exige confirmação).
 *
 * CODE-1 (2026-07-09): estética Matrix aposentada a pedido do usuário —
 * agora é a linha LUXO do Regente (serif no título, accent do tema, zero
 * cor hardcoded de chrome) + painel-guia FIXO à esquerda com TODAS as tags
 * do dialeto (CodeGuidePanel / code-schema.ts) + validação enquanto digita.
 *
 * O editor segue um <textarea> transparente sobre um <pre> com highlight
 * YAML (regex leve) — zero dependência nova, de propósito.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { getSessionCode, applySessionCode, type CodeApplyResult } from "@/lib/design-session-api";
import { toast } from "./Toast";
import CodeGuidePanel from "./CodeGuidePanel";
import { useResizablePanel, ResizeHandle } from "./resizable";

/* ── Highlight YAML minimalista (chave/valor/comentário/---/%%tokens) ──
   Cores do TEMA onde há token; as poucas fixas (número/%%var) seguem a
   paleta de status já usada no app — trocar o tema troca o acento. */
function escapeHtml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function highlightYaml(src: string): string {
  return src
    .split("\n")
    .map((line) => {
      const esc = escapeHtml(line);
      if (/^\s*#/.test(line)) return `<span style="color:var(--v2-text-muted);font-style:italic">${esc}</span>`;
      if (/^---\s*$/.test(line)) return `<span style="color:var(--v2-accent-brand);font-weight:700">${esc}</span>`;
      // chave: valor — colore a chave e strings/números/bools do valor
      const m = line.match(/^(\s*(?:- )?)([A-Za-z_][\w.-]*)(\s*:)(.*)$/);
      if (m) {
        const [, ind, key, colon, rest] = m;
        const restEsc = escapeHtml(rest)
          .replace(/(&quot;.*?&quot;|'.*?')/g, `<span style="color:var(--v2-text-primary)">$1</span>`)
          .replace(/\b(true|false|null)\b/g, `<span style="color:var(--v2-status-waiting)">$1</span>`)
          .replace(/(?<![\w&#;])(-?\d+(?:\.\d+)?)(?![\w;])/g, `<span style="color:var(--v2-status-running)">$1</span>`)
          .replace(/(%%[A-Za-z][\w]*)/g, `<span style="color:#c4b5fd">$1</span>`);
        return `${escapeHtml(ind)}<span style="color:var(--v2-accent-brand);font-weight:600">${escapeHtml(key)}</span><span style="color:var(--v2-text-muted)">${escapeHtml(colon)}</span>${restEsc}`;
      }
      return `<span style="color:var(--v2-text-secondary)">${esc}</span>`;
    })
    .join("\n");
}

/* ── Componente principal ── */
export default function CodeModeView({
  sessionId,
  folders,
  onExit,
  onApplied,
}: {
  sessionId: string;
  folders: string[];
  onExit: () => void;
  onApplied: () => Promise<void> | void;
}) {
  const [code, setCode] = useState<string>("");
  const [loading, setLoading] = useState(true);
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState(false);
  const [linting, setLinting] = useState(false);
  const [result, setResult] = useState<CodeApplyResult | null>(null);
  const taRef = useRef<HTMLTextAreaElement | null>(null);
  const preRef = useRef<HTMLPreElement | null>(null);
  const gutterRef = useRef<HTMLPreElement | null>(null);

  // Chave ESTÁVEL do escopo: o pai passa `folders` como array novo a cada
  // render — usar o array direto nas deps refetcharia (e resetaria o editor,
  // apagando edições) a cada re-render do V2Preview (WS, tick, etc.).
  const folderKey = folders.join(",");
  const scopeFolders = useMemo(
    () => (folderKey ? folderKey.split(",") : []),
    [folderKey],
  );

  // Carga inicial: setState só nos callbacks da promise (regra set-state-in-effect).
  // O spinner inicial vem do loading=true default.
  useEffect(() => {
    let cancel = false;
    getSessionCode(sessionId, scopeFolders)
      .then((res) => {
        if (cancel) return;
        setCode(res.code);
        setDirty(false);
        setResult(null);
      })
      .catch((e: unknown) => {
        if (!cancel) toast.error("Failed to load the code", { detail: e instanceof Error ? e.message : String(e) });
      })
      .finally(() => { if (!cancel) setLoading(false); });
    return () => { cancel = true; };
  }, [sessionId, scopeFolders]);

  // Recarga manual (botão Recarregar / pós-aplicação): mesma busca, com spinner.
  const load = useCallback(async () => {
    setLoading(true);
    setResult(null);
    try {
      const res = await getSessionCode(sessionId, scopeFolders);
      setCode(res.code);
      setDirty(false);
    } catch (e) {
      toast.error("Failed to load the code", { detail: e instanceof Error ? e.message : String(e) });
    } finally {
      setLoading(false);
    }
  }, [sessionId, scopeFolders]);

  const syncScroll = useCallback(() => {
    const ta = taRef.current, pre = preRef.current, gut = gutterRef.current;
    if (ta && pre) { pre.scrollTop = ta.scrollTop; pre.scrollLeft = ta.scrollLeft; }
    if (ta && gut) gut.scrollTop = ta.scrollTop;
  }, []);

  // Lint AO VIVO (CODE-1): pausa de digitação → dry-run no server (o MESMO
  // validador do Aplicar — parse estrito + plano). Sem toast: o veredito vive
  // no rodapé. `lintSeq` descarta resposta obsoleta (digitou de novo no meio).
  const lintSeq = useRef(0);
  useEffect(() => {
    if (!dirty || loading || busy) return;
    const seq = ++lintSeq.current;
    const t = setTimeout(() => {
      setLinting(true);
      applySessionCode(sessionId, code, { folders: scopeFolders, apply: false })
        .then((res) => { if (seq === lintSeq.current) setResult(res); })
        .catch(() => { /* transiente (rede) — o Validar manual reporta */ })
        .finally(() => { if (seq === lintSeq.current) setLinting(false); });
    }, 900);
    return () => clearTimeout(t);
  }, [code, dirty, loading, busy, sessionId, scopeFolders]);

  const runValidate = useCallback(async () => {
    setBusy(true);
    try {
      const res = await applySessionCode(sessionId, code, { folders, apply: false });
      setResult(res);
    } catch (e) {
      toast.error("Validation failed", { detail: e instanceof Error ? e.message : String(e) });
    } finally {
      setBusy(false);
    }
  }, [sessionId, code, folders]);

  const runApply = useCallback(async () => {
    setBusy(true);
    try {
      // 1ª passada: valida + detecta deletes.
      let res = await applySessionCode(sessionId, code, { folders, apply: true });
      if (!res.applied && (res.errors?.length ?? 0) > 0 && (res.plan?.deletes?.length ?? 0) > 0) {
        const soDeleteGate = !res.errors!.some((e) => !e.includes("DELETED"));
        if (soDeleteGate) {
          const okDel = window.confirm(
            `${res.plan.deletes.length} job(s) missing from the code will be DELETED:\n\n${res.plan.deletes.join(", ")}\n\nConfirm the removal?`,
          );
          if (!okDel) { setResult(res); return; }
          res = await applySessionCode(sessionId, code, { folders, apply: true, allowDelete: true });
        }
      }
      setResult(res);
      if (res.applied) {
        const c = res.plan;
        toast.success("Code applied to the working set", {
          detail: `${c.creates.length} created · ${c.updates.length} updated · ${c.deletes.length} deleted · ${c.unchanged} unchanged`,
        });
        setDirty(false);
        await onApplied();
        await load(); // re-serializa: normaliza o YAML como o server o vê
      }
    } catch (e) {
      toast.error("Apply failed", { detail: e instanceof Error ? e.message : String(e) });
    } finally {
      setBusy(false);
    }
  }, [sessionId, code, folders, onApplied, load]);

  // Ergonomia de YAML no textarea: Tab indenta (Shift+Tab desindenta a linha)
  // e Enter mantém a indentação corrente (+2 se a linha abriu bloco com ":").
  const onKeyDown = useCallback((e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    const ta = e.currentTarget;
    const { selectionStart: s, selectionEnd: en, value } = ta;
    if (e.key === "Tab") {
      e.preventDefault();
      if (e.shiftKey) {
        const ls = value.lastIndexOf("\n", s - 1) + 1;
        const rm = value.slice(ls).startsWith("  ") ? 2 : value.slice(ls).startsWith(" ") ? 1 : 0;
        if (rm === 0) return;
        const next = value.slice(0, ls) + value.slice(ls + rm);
        setCode(next);
        setDirty(true);
        requestAnimationFrame(() => { ta.selectionStart = ta.selectionEnd = Math.max(ls, s - rm); });
        return;
      }
      const next = value.slice(0, s) + "  " + value.slice(en);
      setCode(next);
      setDirty(true);
      requestAnimationFrame(() => { ta.selectionStart = ta.selectionEnd = s + 2; });
      return;
    }
    if (e.key === "Enter") {
      e.preventDefault();
      const ls = value.lastIndexOf("\n", s - 1) + 1;
      const line = value.slice(ls, s);
      const indent = (line.match(/^\s*/)?.[0] ?? "") + (/(^|\S):\s*$/.test(line) ? "  " : "");
      const insert = "\n" + indent;
      const next = value.slice(0, s) + insert + value.slice(en);
      setCode(next);
      setDirty(true);
      requestAnimationFrame(() => { ta.selectionStart = ta.selectionEnd = s + insert.length; });
    }
  }, []);

  const lineCount = useMemo(() => code.split("\n").length, [code]);
  const gutter = useMemo(
    () => Array.from({ length: lineCount }, (_, i) => i + 1).join("\n"),
    [lineCount],
  );
  const highlighted = useMemo(() => highlightYaml(code), [code]);

  // Painel-guia à esquerda: largura redimensionável (mesmo hook da sidebar).
  const guide = useResizablePanel({
    storageKey: "regente.panel.codeguide.w", defaultWidth: 300, min: 220, max: 520, edge: "right",
  });

  const planBadge = (label: string, ids: string[], color: string) =>
    ids.length > 0 && (
      <span title={ids.join(", ")} style={{ color, marginRight: 14 }}>
        {label} {ids.length}
      </span>
    );

  const mono: React.CSSProperties = {
    fontFamily: "var(--v2-font-mono)",
    fontSize: 12.5,
    lineHeight: "19px",
    tabSize: 2,
  };

  const errors = [
    ...(result?.errors ?? []),
    ...((result?.results ?? []).filter((r) => !r.ok).map((r) => `${r.id}: ${r.error}`)),
  ];

  return (
    <div
      data-testid="code-mode"
      className="v2-grain"
      style={{
        position: "absolute", inset: 0, zIndex: 5,
        display: "flex", flexDirection: "column",
        background: "var(--v2-bg-canvas)",
      }}
    >
      {/* Barra do modo código (linha luxo) */}
      <div style={{
        display: "flex", alignItems: "center", gap: 14,
        padding: "10px 16px",
        background: "var(--v2-bg-surface)",
        borderBottom: "1px solid var(--v2-border-medium)",
        position: "relative", zIndex: 2,
      }}>
        <span style={{
          fontFamily: "var(--v2-font-serif)", fontStyle: "italic", fontWeight: 600,
          fontSize: 20, lineHeight: 1, color: "var(--v2-accent-brand)",
        }}>
          Jobs as Code
        </span>
        <span style={{
          fontFamily: "var(--v2-font-mono)", fontSize: 9, letterSpacing: "0.16em",
          textTransform: "uppercase", color: "var(--v2-text-muted)",
        }}>
          {folders.join(" · ") || "no folders"} — multi-doc YAML · Git workspace dialect
        </span>
        <div style={{ flex: 1 }} />
        {dirty && (
          <span style={{ fontFamily: "var(--v2-font-mono)", fontSize: 10, color: "var(--v2-status-waiting)" }}>
            ● not applied
          </span>
        )}
        <button className="code-btn" disabled={busy || loading} onClick={() => void runValidate()}>
          Validate
        </button>
        <button className="code-btn code-btn-solid" disabled={busy || loading || !dirty} onClick={() => void runApply()}>
          Apply
        </button>
        <button
          className="code-btn"
          disabled={busy || loading}
          onClick={() => {
            if (dirty && !window.confirm("Discard the unapplied code edits?")) return;
            void load();
          }}
        >
          Reload
        </button>
        <button className="code-btn" onClick={() => {
          if (dirty && !window.confirm("Leave code mode? Unapplied edits will be lost.")) return;
          onExit();
        }}>
          ⏏ Exit
        </button>
        {/* fio de luxo sob o header (mesmo gesto do FolderManager) */}
        <div style={{
          position: "absolute", left: 16, right: 16, bottom: -1, height: 1,
          background: "linear-gradient(90deg, transparent, var(--v2-accent-brand), transparent)",
          opacity: 0.35, pointerEvents: "none",
        }} />
      </div>

      {/* Corpo: guia do schema (fixa, redimensionável) + editor */}
      <div style={{ flex: 1, display: "flex", minHeight: 0, zIndex: 1 }}>
        <div style={{ position: "relative", width: guide.width, flexShrink: 0, minHeight: 0 }}>
          <CodeGuidePanel />
          <ResizeHandle edge="right" onMouseDown={guide.onMouseDown} onReset={guide.reset} />
        </div>

        {/* Gutter de linhas */}
        <pre aria-hidden ref={gutterRef} style={{
          ...mono, margin: 0, padding: "12px 8px 12px 14px", textAlign: "right",
          color: "var(--v2-text-disabled)", userSelect: "none", overflow: "hidden",
          borderRight: "1px solid var(--v2-border-subtle)", background: "var(--v2-bg-surface)",
          minWidth: 44,
        }}>{gutter}</pre>
        <div style={{ position: "relative", flex: 1, minWidth: 0 }}>
          {/* Camada de highlight */}
          <pre
            ref={preRef}
            aria-hidden
            style={{
              ...mono, position: "absolute", inset: 0, margin: 0,
              padding: "12px 16px", overflow: "hidden",
              whiteSpace: "pre", pointerEvents: "none",
              background: "transparent",
            }}
            dangerouslySetInnerHTML={{ __html: highlighted + "\n" }}
          />
          {/* Textarea transparente por cima */}
          <textarea
            ref={taRef}
            value={code}
            spellCheck={false}
            disabled={loading}
            onChange={(e) => { setCode(e.target.value); setDirty(true); }}
            onScroll={syncScroll}
            onKeyDown={onKeyDown}
            style={{
              ...mono, position: "absolute", inset: 0, width: "100%", height: "100%",
              padding: "12px 16px", border: "none", outline: "none", resize: "none",
              background: "transparent", color: "transparent",
              caretColor: "var(--v2-accent-brand)", whiteSpace: "pre", overflow: "auto",
            }}
          />
          {loading && (
            <div style={{
              position: "absolute", inset: 0, display: "flex", alignItems: "center", justifyContent: "center",
              fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)", fontSize: 12, letterSpacing: "0.18em",
              textTransform: "uppercase",
            }}>
              loading the code…
            </div>
          )}
        </div>
      </div>

      {/* Rodapé fixo: veredito do lint AO VIVO / plano da última validação-aplicação */}
      <div style={{
        maxHeight: 180, overflowY: "auto", zIndex: 2,
        borderTop: "1px solid var(--v2-border-medium)", background: "var(--v2-bg-surface)",
        padding: "7px 16px", fontFamily: "var(--v2-font-mono)", fontSize: 11,
      }}>
        <div style={{ color: "var(--v2-text-secondary)" }}>
          {linting && <span style={{ color: "var(--v2-text-muted)", marginRight: 12 }}>validating…</span>}
          {result ? (
            <>
              {result.parsed} job(s) in the document ·{" "}
              {planBadge("＋create", result.plan?.creates ?? [], "var(--v2-status-ok)")}
              {planBadge("~update", result.plan?.updates ?? [], "var(--v2-status-running)")}
              {planBadge("−delete", result.plan?.deletes ?? [], "var(--v2-status-failed)")}
              <span style={{ color: "var(--v2-text-muted)" }}>{result.plan?.unchanged ?? 0} unchanged</span>
              {result.applied && (
                <span style={{ color: "var(--v2-accent-brand)", marginLeft: 12, fontWeight: 700 }}>✓ applied</span>
              )}
              {!result.applied && errors.length === 0 && (
                <span style={{ color: "var(--v2-status-ok)", marginLeft: 12 }}>✓ valid — nothing applied yet</span>
              )}
            </>
          ) : (
            !linting && (
              <span style={{ color: "var(--v2-text-muted)" }}>
                edit freely — the code is validated automatically on every pause (strict parse + plan)
              </span>
            )
          )}
        </div>
        {errors.map((err, i) => (
          <div key={i} style={{ color: "var(--v2-status-failed)", marginTop: 2 }}>✗ {err}</div>
        ))}
      </div>
    </div>
  );
}
