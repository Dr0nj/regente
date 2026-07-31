/**
 * server-scheduler-runtime — scheduler é do server Go; o web só dispara
 * comandos e consulta estado. Preserva a assinatura exata de
 * `scheduler-runtime.ts` para drop-in via `runtime-bridge`.
 */

import type { JobDefinition, JobInstance } from "@/lib/orchestrator-model";
import { api } from "@/lib/server-client";
import { syncDailyStatus } from "@/lib/business-date";
import { refreshFromServer, getTodayInstances } from "@/lib/server-instance-store";

const DAILY_FLAG_KEY = "regente:daily-run-at";

export function getLastDailyRun(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(DAILY_FLAG_KEY);
}

/* ── Daily status (fonte da verdade = relógio/DB do SERVER) ── */

export interface DailyStatus {
  orderDate: string;     // "hoje" NA timezone da daily (E1) — a data de negócio
  dailyAt: string;       // "HH:MM" configurado (settings.daily_at, default 00:00)
  timezone?: string;     // E1 — settings.daily_timezone (nome IANA); "" = relógio local do server
  lastRunDate?: string;
  lastRunAt?: string;    // timestamp do daily_runs (RFC3339 UTC)
  serverNow?: string;    // RFC3339 já na timezone da daily (com offset)
  lateStart?: boolean;   // daily materializou > horário configurado + 5min (pontualidade)
}

// SQLite grava CURRENT_TIMESTAMP como "YYYY-MM-DD HH:MM:SS" em UTC SEM
// timezone; new Date() interpretaria como hora LOCAL. Normaliza pra ISO+Z
// quando não vier com offset explícito.
export function normalizeDbTime(s: string): string {
  if (!s) return s;
  if (/Z$|[+-]\d{2}:?\d{2}$/.test(s)) return s;
  return s.replace(" ", "T") + "Z";
}

// DAY-1 — delega pro business-date, que é quem publica a data de negócio pro
// todayOrderDate(). Toda leitura de status da daily tem que passar por lá,
// senão o rodapé mostra um dia e o board pede outro.
export async function fetchDailyStatus(): Promise<DailyStatus | null> {
  return (await syncDailyStatus()) as DailyStatus | null;
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
