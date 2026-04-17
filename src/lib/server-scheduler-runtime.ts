/**
 * server-scheduler-runtime — scheduler é do server Go; o web só dispara
 * comandos e consulta estado. Preserva a assinatura exata de
 * `scheduler-runtime.ts` para drop-in via `runtime-bridge`.
 */

import type { JobDefinition, JobInstance } from "@/lib/orchestrator-model";
import { api } from "@/lib/server-client";
import { refreshFromServer, getTodayInstances } from "@/lib/server-instance-store";

const DAILY_FLAG_KEY = "regente:daily-run-at";

export function getLastDailyRun(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(DAILY_FLAG_KEY);
}

/** Dispara daily/run no server. A lista local de defs é ignorada (server usa YAML). */
export function runDaily(_defs: JobDefinition[]): JobInstance[] {
  void _defs;
  void (async () => {
    try {
      await api<{ orderDate: string; created: number }>("/api/daily/run", { method: "POST" });
      if (typeof window !== "undefined") {
        window.localStorage.setItem(DAILY_FLAG_KEY, new Date().toISOString());
      }
      await refreshFromServer();
    } catch (err) {
      console.error("[server-scheduler] daily run failed", err);
    }
  })();
  return getTodayInstances();
}

/** No-op — defs no server são atualizadas via `POST /api/definitions`. */
export function updateSchedulerDefs(_defs: JobDefinition[]): void {
  void _defs;
}

/** No-op — o server roda o scheduler 24/7. Mantemos assinatura para compat. */
export function startScheduler(_tickMs = 2000): void {
  void _tickMs;
}

export function stopScheduler(): void {
  // no-op
}
