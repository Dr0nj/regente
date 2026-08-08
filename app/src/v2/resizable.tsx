import { useCallback, useEffect, useRef, useState } from "react";

/* ──────────────────────────────────────────────────────────────
   useResizablePanel — torna painéis laterais (absolute) maleáveis.
   ──────────────────────────────────────────────────────────────
   - Arraste a borda interna para aumentar/diminuir a largura.
   - Largura persistida em localStorage por painel (storageKey).
   - Double-click na borda reseta para o default.
   - edge = "right": handle na borda direita (painel ancorado à ESQUERDA).
     edge = "left":  handle na borda esquerda (painel ancorado à DIREITA).
   - railWidth (opcional): habilita COLAPSO — o painel encolhe para um trilho
     dessa largura, sem desmontar. A largura expandida é preservada intacta
     (o drag continua partindo dela), e o estado colapsado é persistido em
     "<storageKey>.collapsed". O painel colapsado CONTINUA ocupando espaço
     medível: quem centraliza o canvas lê os overlays por data-canvas-inset
     (ver useCanvasCamera.visibleInsets), então o trilho tem que existir no
     DOM com a largura real, não sumir.
   ────────────────────────────────────────────────────────────── */

export interface ResizableOpts {
  storageKey: string;
  defaultWidth: number;
  min: number;
  max: number;
  edge: "left" | "right";
  /** Largura do trilho colapsado. Ausente = painel não colapsa. */
  railWidth?: number;
}

// eslint-disable-next-line react-refresh/only-export-components -- hook público convive com o componente ResizeHandle no mesmo módulo; mover = churn de imports sem ganho de prod; ver roadmap §RH
export function useResizablePanel(opts: ResizableOpts) {
  const { storageKey, defaultWidth, min, max, edge, railWidth } = opts;
  // expandedWidth = largura do painel ABERTO. Colapsar não a toca: o trilho é
  // só o que devolvemos como `width`, então reabrir devolve a largura de antes.
  const [expandedWidth, setWidth] = useState<number>(() => {
    const saved = Number(localStorage.getItem(storageKey));
    return saved && saved >= min && saved <= max ? saved : defaultWidth;
  });
  const collapseKey = storageKey + ".collapsed";
  const [collapsed, setCollapsed] = useState<boolean>(
    () => !!railWidth && localStorage.getItem(collapseKey) === "1"
  );
  const widthRef = useRef(expandedWidth);
  // leitores são handlers de mouse (pós-commit) → efeito é equivalente ao write em render; ver roadmap §RH
  useEffect(() => {
    widthRef.current = expandedWidth;
  }, [expandedWidth]);

  const toggleCollapsed = useCallback(() => {
    setCollapsed((prev) => {
      const next = !prev;
      localStorage.setItem(collapseKey, next ? "1" : "0");
      return next;
    });
  }, [collapseKey]);

  const onMouseDown = useCallback(
    (e: React.MouseEvent) => {
      e.preventDefault();
      const startX = e.clientX;
      const startW = widthRef.current;
      const onMove = (ev: MouseEvent) => {
        const dx = ev.clientX - startX;
        // handle na direita: arrastar p/ direita aumenta.
        // handle na esquerda: arrastar p/ esquerda aumenta.
        const delta = edge === "right" ? dx : -dx;
        const next = Math.min(max, Math.max(min, startW + delta));
        setWidth(next);
      };
      const onUp = () => {
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
        window.removeEventListener("mousemove", onMove);
        window.removeEventListener("mouseup", onUp);
        localStorage.setItem(storageKey, String(widthRef.current));
      };
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      window.addEventListener("mousemove", onMove);
      window.addEventListener("mouseup", onUp);
    },
    [edge, min, max, storageKey]
  );

  const reset = useCallback(() => {
    setWidth(defaultWidth);
    localStorage.setItem(storageKey, String(defaultWidth));
  }, [defaultWidth, storageKey]);

  const width = collapsed && railWidth ? railWidth : expandedWidth;
  return { width, onMouseDown, reset, collapsed, toggleCollapsed };
}

/** Faixa arrastável fina, posicionada na borda interna do painel. */
export function ResizeHandle({
  edge,
  onMouseDown,
  onReset,
}: {
  edge: "left" | "right";
  onMouseDown: (e: React.MouseEvent) => void;
  onReset?: () => void;
}) {
  const [hover, setHover] = useState(false);
  return (
    <div
      onMouseDown={onMouseDown}
      onDoubleClick={onReset}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      title="Drag to resize · double-click to reset"
      style={{
        position: "absolute",
        top: 0,
        bottom: 0,
        [edge === "right" ? "right" : "left"]: 0,
        width: 6,
        cursor: "col-resize",
        zIndex: 20,
        background: hover ? "var(--v2-accent-brand)" : "transparent",
        opacity: hover ? 0.6 : 1,
        transition: "background 100ms linear",
      }}
    />
  );
}
