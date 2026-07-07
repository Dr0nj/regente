/**
 * CodeModeView — modo CÓDIGO do Design (jobs as code, 2026-07-06).
 *
 * Substitui o canvas central por um editor YAML do working set (folders
 * abertas da design session): o dev cria/edita jobs como código, no MESMO
 * dialeto dos arquivos do workspace Git. Fluxo: editar → Validar (dry-run,
 * plano creates/updates/deletes) → Aplicar (transacional por item; delete
 * exige confirmação). Estética Matrix: digital rain no fundo, verde fósforo.
 *
 * O editor é um <textarea> transparente sobre um <pre> com highlight YAML
 * (regex leve) — zero dependência nova. Aperfeiçoamentos no roadmap.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { getSessionCode, applySessionCode, type CodeApplyResult } from "@/lib/design-session-api";
import { toast } from "./Toast";

const MATRIX_GREEN = "#00ff41";
const MATRIX_DIM = "#0a3d1a";

/* ── Highlight YAML minimalista (chave/valor/comentário/---/diretivas) ── */
function escapeHtml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

function highlightYaml(src: string): string {
  return src
    .split("\n")
    .map((line) => {
      const esc = escapeHtml(line);
      if (/^\s*#/.test(line)) return `<span style="color:#2e7d4f;font-style:italic">${esc}</span>`;
      if (/^---\s*$/.test(line)) return `<span style="color:#00ff41;font-weight:700">${esc}</span>`;
      // chave: valor — colore a chave e strings/números do valor
      const m = line.match(/^(\s*(?:- )?)([A-Za-z_][\w.-]*)(\s*:)(.*)$/);
      if (m) {
        const [, ind, key, colon, rest] = m;
        const restEsc = escapeHtml(rest)
          .replace(/(&quot;.*?&quot;|'.*?')/g, `<span style="color:#7dffa8">$1</span>`)
          .replace(/\b(true|false|null)\b/g, `<span style="color:#ffd24d">$1</span>`)
          .replace(/(?<![\w&#;])(-?\d+(?:\.\d+)?)(?![\w;])/g, `<span style="color:#4dd2ff">$1</span>`)
          .replace(/(%%[A-Za-z][\w]*)/g, `<span style="color:#ff7de9">$1</span>`);
        return `${escapeHtml(ind)}<span style="color:#00e63a;font-weight:600">${escapeHtml(key)}</span><span style="color:#1f9e4e">${escapeHtml(colon)}</span>${restEsc}`;
      }
      return `<span style="color:#9df5b8">${esc}</span>`;
    })
    .join("\n");
}

/* ── Digital rain (canvas, bem sutil atrás do editor) ── */
function MatrixRain() {
  const ref = useRef<HTMLCanvasElement | null>(null);
  useEffect(() => {
    const canvas = ref.current;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    let raf = 0;
    const glyphs = "アイウエオカキクケコサシスセソ0123456789ABCDEF<>=/%{}[]";
    const fontSize = 14;
    let cols = 0;
    let drops: number[] = [];
    const resize = () => {
      canvas.width = canvas.offsetWidth;
      canvas.height = canvas.offsetHeight;
      cols = Math.ceil(canvas.width / fontSize);
      drops = Array.from({ length: cols }, () => Math.floor(Math.random() * canvas.height / fontSize));
    };
    resize();
    const ro = new ResizeObserver(resize);
    ro.observe(canvas);
    let last = 0;
    const tick = (t: number) => {
      raf = requestAnimationFrame(tick);
      if (t - last < 66) return; // ~15fps: efeito, não jogo
      last = t;
      ctx.fillStyle = "rgba(0, 8, 2, 0.12)";
      ctx.fillRect(0, 0, canvas.width, canvas.height);
      ctx.font = `${fontSize}px monospace`;
      for (let i = 0; i < cols; i++) {
        const ch = glyphs[Math.floor(Math.random() * glyphs.length)];
        ctx.fillStyle = Math.random() < 0.08 ? MATRIX_GREEN : MATRIX_DIM;
        ctx.fillText(ch, i * fontSize, drops[i] * fontSize);
        if (drops[i] * fontSize > canvas.height && Math.random() > 0.975) drops[i] = 0;
        drops[i]++;
      }
    };
    raf = requestAnimationFrame(tick);
    return () => { cancelAnimationFrame(raf); ro.disconnect(); };
  }, []);
  return (
    <canvas
      ref={ref}
      style={{ position: "absolute", inset: 0, width: "100%", height: "100%", opacity: 0.35, pointerEvents: "none" }}
    />
  );
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
        if (!cancel) toast.error("Falha ao carregar o código", { detail: e instanceof Error ? e.message : String(e) });
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
      toast.error("Falha ao carregar o código", { detail: e instanceof Error ? e.message : String(e) });
    } finally {
      setLoading(false);
    }
  }, [sessionId, scopeFolders]);

  const syncScroll = useCallback(() => {
    const ta = taRef.current, pre = preRef.current, gut = gutterRef.current;
    if (ta && pre) { pre.scrollTop = ta.scrollTop; pre.scrollLeft = ta.scrollLeft; }
    if (ta && gut) gut.scrollTop = ta.scrollTop;
  }, []);

  const runValidate = useCallback(async () => {
    setBusy(true);
    try {
      const res = await applySessionCode(sessionId, code, { folders, apply: false });
      setResult(res);
    } catch (e) {
      toast.error("Validação falhou", { detail: e instanceof Error ? e.message : String(e) });
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
        const soDeleteGate = !res.errors!.some((e) => !e.includes("DELETADOS"));
        if (soDeleteGate) {
          const okDel = window.confirm(
            `${res.plan.deletes.length} job(s) ausentes no código serão DELETADOS:\n\n${res.plan.deletes.join(", ")}\n\nConfirma a remoção?`,
          );
          if (!okDel) { setResult(res); return; }
          res = await applySessionCode(sessionId, code, { folders, apply: true, allowDelete: true });
        }
      }
      setResult(res);
      if (res.applied) {
        const c = res.plan;
        toast.success("Código aplicado ao working set", {
          detail: `${c.creates.length} criados · ${c.updates.length} atualizados · ${c.deletes.length} deletados · ${c.unchanged} intactos`,
        });
        setDirty(false);
        await onApplied();
        await load(); // re-serializa: normaliza o YAML como o server o vê
      }
    } catch (e) {
      toast.error("Aplicação falhou", { detail: e instanceof Error ? e.message : String(e) });
    } finally {
      setBusy(false);
    }
  }, [sessionId, code, folders, onApplied, load]);

  // Tab indenta em vez de sair do editor.
  const onKeyDown = useCallback((e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Tab") {
      e.preventDefault();
      const ta = e.currentTarget;
      const { selectionStart: s, selectionEnd: en, value } = ta;
      const next = value.slice(0, s) + "  " + value.slice(en);
      setCode(next);
      setDirty(true);
      requestAnimationFrame(() => { ta.selectionStart = ta.selectionEnd = s + 2; });
    }
  }, []);

  const lineCount = useMemo(() => code.split("\n").length, [code]);
  const gutter = useMemo(
    () => Array.from({ length: lineCount }, (_, i) => i + 1).join("\n"),
    [lineCount],
  );
  const highlighted = useMemo(() => highlightYaml(code), [code]);

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

  return (
    <div
      data-testid="code-mode"
      style={{
        position: "absolute", inset: 0, zIndex: 5,
        display: "flex", flexDirection: "column",
        background: "#000802",
      }}
    >
      <MatrixRain />
      {/* Barra do modo código */}
      <div style={{
        display: "flex", alignItems: "center", gap: 12,
        padding: "8px 14px", borderBottom: `1px solid ${MATRIX_DIM}`,
        background: "rgba(0, 10, 3, 0.85)", zIndex: 2, backdropFilter: "blur(2px)",
      }}>
        <span style={{
          fontFamily: "var(--v2-font-mono)", fontSize: 12, fontWeight: 700,
          color: MATRIX_GREEN, letterSpacing: "0.12em", textShadow: `0 0 8px ${MATRIX_GREEN}66`,
        }}>
          ⌈ JOBS AS CODE ⌋
        </span>
        <span style={{ fontFamily: "var(--v2-font-mono)", fontSize: 10, color: "#3fae63" }}>
          {folders.join(" · ") || "sem folders"} — YAML multi-doc (mesmo dialeto do workspace Git)
        </span>
        <div style={{ flex: 1 }} />
        {dirty && (
          <span style={{ fontFamily: "var(--v2-font-mono)", fontSize: 10, color: "#ffd24d" }}>● não aplicado</span>
        )}
        <button className="matrix-btn" disabled={busy || loading} onClick={() => void runValidate()}>
          Validar
        </button>
        <button className="matrix-btn matrix-btn-solid" disabled={busy || loading || !dirty} onClick={() => void runApply()}>
          Aplicar
        </button>
        <button
          className="matrix-btn"
          disabled={busy || loading}
          onClick={() => {
            if (dirty && !window.confirm("Descartar as edições de código não aplicadas?")) return;
            void load();
          }}
        >
          Recarregar
        </button>
        <button className="matrix-btn" onClick={() => {
          if (dirty && !window.confirm("Sair do modo código? Edições não aplicadas serão perdidas.")) return;
          onExit();
        }}>
          ⏏ Sair
        </button>
      </div>

      {/* Editor */}
      <div style={{ flex: 1, display: "flex", minHeight: 0, zIndex: 1 }}>
        {/* Gutter de linhas */}
        <pre aria-hidden ref={gutterRef} style={{
          ...mono, margin: 0, padding: "12px 8px 12px 14px", textAlign: "right",
          color: "#1f6e3c", userSelect: "none", overflow: "hidden",
          borderRight: `1px solid ${MATRIX_DIM}`, background: "rgba(0, 10, 3, 0.6)",
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
            onChange={(e) => { setCode(e.target.value); setDirty(true); setResult(null); }}
            onScroll={syncScroll}
            onKeyDown={onKeyDown}
            style={{
              ...mono, position: "absolute", inset: 0, width: "100%", height: "100%",
              padding: "12px 16px", border: "none", outline: "none", resize: "none",
              background: "transparent", color: "transparent",
              caretColor: MATRIX_GREEN, whiteSpace: "pre", overflow: "auto",
            }}
          />
          {loading && (
            <div style={{
              position: "absolute", inset: 0, display: "flex", alignItems: "center", justifyContent: "center",
              fontFamily: "var(--v2-font-mono)", color: MATRIX_GREEN, fontSize: 13, letterSpacing: "0.2em",
            }}>
              CARREGANDO O CONSTRUCTO…
            </div>
          )}
        </div>
      </div>

      {/* Rodapé: plano/erros da última validação/aplicação */}
      {result && (
        <div style={{
          maxHeight: 180, overflowY: "auto", zIndex: 2,
          borderTop: `1px solid ${MATRIX_DIM}`, background: "rgba(0, 10, 3, 0.92)",
          padding: "8px 14px", fontFamily: "var(--v2-font-mono)", fontSize: 11,
        }}>
          <div style={{ marginBottom: 4, color: "#9df5b8" }}>
            {result.parsed} job(s) no documento ·{" "}
            {planBadge("＋criar", result.plan?.creates ?? [], "#7dffa8")}
            {planBadge("~atualizar", result.plan?.updates ?? [], "#4dd2ff")}
            {planBadge("−deletar", result.plan?.deletes ?? [], "#ff6b6b")}
            <span style={{ color: "#3fae63" }}>{result.plan?.unchanged ?? 0} sem mudança</span>
            {result.applied && <span style={{ color: MATRIX_GREEN, marginLeft: 12, fontWeight: 700 }}>✓ APLICADO</span>}
            {!result.applied && (result.errors?.length ?? 0) === 0 && (
              <span style={{ color: "#ffd24d", marginLeft: 12 }}>válido — nada aplicado (dry-run)</span>
            )}
          </div>
          {(result.errors ?? []).map((err, i) => (
            <div key={i} style={{ color: "#ff6b6b" }}>✗ {err}</div>
          ))}
          {(result.results ?? []).filter((r) => !r.ok).map((r) => (
            <div key={r.id} style={{ color: "#ff6b6b" }}>✗ {r.id}: {r.error}</div>
          ))}
        </div>
      )}
    </div>
  );
}
