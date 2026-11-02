import "@/index.css";
import { lazy, Suspense, useEffect, useState } from "react";
import ErrorBoundary from "@/components/ErrorBoundary";
import { fetchMe, type AuthUser } from "@/lib/auth-api";
import { isServerMode, onAuthEvent } from "@/lib/server-client";
import { LoginForm } from "@/v2/LoginForm";

// Loading the console also initializes its stores and canvas. Wait for a real
// server session; cached localStorage identity is never an authentication gate.
const Console = lazy(() => import("@/v2/V2Preview"));
const Portal = lazy(() => import("@/v2/PortalView"));

export default function App() {
  const [session, setSession] = useState<{ checked: boolean; user: AuthUser | null }>({ checked: !isServerMode(), user: null });
  useEffect(() => {
    if (!isServerMode()) return;
    let active = true;
    let revision = 0;
    const off = onAuthEvent(() => {
      revision++;
      setSession({ checked: true, user: null });
    });
    const initialRevision = revision;
    void fetchMe().then(user => {
      if (active && initialRevision === revision) setSession({ checked: true, user });
    });
    return () => { active = false; off(); };
  }, []);
  const isPortal = typeof window !== "undefined" && window.location.pathname.startsWith("/portal");
  return (
    <ErrorBoundary>
      {!session.checked ? <div role="status">Loading…</div> :
        isServerMode() && (!session.user || session.user.mustChangePassword) ?
          <LoginForm onLogin={user => setSession({ checked: true, user })} /> :
          <Suspense fallback={<div role="status">Loading…</div>}>
            {isPortal ? <Portal /> : <Console />}
          </Suspense>}
    </ErrorBoundary>
  );
}
