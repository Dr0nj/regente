import "@/index.css";
import Dashboard from "@/components/Dashboard";
import AuthPage from "@/components/AuthPage";
import ErrorBoundary from "@/components/ErrorBoundary";
import { ToastProvider } from "@/components/ToastStack";
import { AuthProvider, useAuth } from "@/lib/auth-context";
import { ExecutionProvider } from "@/lib/execution-context";
import { OrchestratorProvider } from "@/lib/orchestrator-context";
import V2Preview from "@/v2/V2Preview";

// Fase 1 concluída — v2 é a rota padrão. `?v1=1` mantém legado acessível
// enquanto Fase 5 não termina o wire-up real do monitoring.
const isLegacyV1 =
  typeof window !== "undefined" && window.location.search.includes("v1");

function AppContent() {
  const { user, loading, isConfigured } = useAuth();

  if (loading) {
    return (
      <div className="flex h-screen items-center justify-center bg-bg-deep">
        <div className="h-8 w-8 animate-spin rounded-full border-2 border-accent-cyan/30 border-t-accent-cyan" />
      </div>
    );
  }

  // If Supabase is configured and no user, show auth
  if (isConfigured && !user) {
    return <AuthPage />;
  }

  return (
    <div className="flex h-screen flex-col bg-bg-deep text-text-primary">
      <Dashboard />
    </div>
  );
}

export default function App() {
  if (isLegacyV1) {
    return (
      <ErrorBoundary>
        <AuthProvider>
          <ToastProvider>
            <ExecutionProvider>
              <OrchestratorProvider>
                <AppContent />
              </OrchestratorProvider>
            </ExecutionProvider>
          </ToastProvider>
        </AuthProvider>
      </ErrorBoundary>
    );
  }

  return (
    <ErrorBoundary>
      <V2Preview />
    </ErrorBoundary>
  );
}