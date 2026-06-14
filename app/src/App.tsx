import "@/index.css";
import ErrorBoundary from "@/components/ErrorBoundary";
import V2Preview from "@/v2/V2Preview";

// 2026-06-12 — v1 (Dashboard/FlowCanvas/providers) REMOVIDA.
// A geração atual da UI é exclusivamente src/v2/ (V2Preview).
// Motivo: duas gerações convivendo era fonte de retrabalho e inconsistência
// visual (ver projects/Regente/ANALISE-2026-06-12.md).
export default function App() {
  return (
    <ErrorBoundary>
      <V2Preview />
    </ErrorBoundary>
  );
}
