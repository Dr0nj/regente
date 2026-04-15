import "@/index.css";
import Dashboard from "@/components/Dashboard";
import ErrorBoundary from "@/components/ErrorBoundary";
import { ToastProvider } from "@/components/ToastStack";

export default function App() {
  return (
    <ErrorBoundary>
      <ToastProvider>
        <div className="flex h-screen flex-col bg-bg-deep text-text-primary">
          <Dashboard />
        </div>
      </ToastProvider>
    </ErrorBoundary>
  );
}