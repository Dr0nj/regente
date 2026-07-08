import "@/index.css";
import ErrorBoundary from "@/components/ErrorBoundary";
import V2Preview from "@/v2/V2Preview";
import PortalView from "@/v2/PortalView";

// 2026-06-12 — v1 (Dashboard/FlowCanvas/providers) REMOVIDA.
// A geração atual da UI é exclusivamente src/v2/ (V2Preview).
// Motivo: duas gerações convivendo era fonte de retrabalho e inconsistência
// visual (ver projects/Regente/ANALISE-2026-06-12.md).
//
// D-14 — /portal é a ÚNICA rota fora do console (self-service pro negócio);
// roteada por pathname mesmo (o serveSPA do server devolve o index pra
// qualquer path) — um router de verdade só quando houver a 3ª rota.
export default function App() {
  const isPortal = typeof window !== "undefined" && window.location.pathname.startsWith("/portal");
  return (
    <ErrorBoundary>
      {isPortal ? <PortalView /> : <V2Preview />}
    </ErrorBoundary>
  );
}
