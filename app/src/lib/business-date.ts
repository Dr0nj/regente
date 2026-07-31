/**
 * business-date — DAY-1: quem é "hoje" para a SPA.
 *
 * O dia de negócio vira no `daily_at` do server, não à meia-noite, e o relógio
 * que vale é o DELE (settings.daily_timezone). A SPA não calcula mais isso: lê
 * `/api/daily/status` e publica o `orderDate` em `setBusinessDate`, que é o que
 * `todayOrderDate()` passa a devolver para o board, a sidebar, o ViewPoint e
 * todo `?date=` que sai daqui.
 *
 * Este módulo é o ÚNICO lugar que bate no endpoint, de propósito: o store de
 * instances não pode importar `server-scheduler-runtime` (ele já importa o
 * store — ciclo), e as duas pontas precisam da MESMA data.
 */

import { api } from "@/lib/server-client";
import { setBusinessDate, todayOrderDate } from "@/lib/orchestrator-model";

export interface DailyStatusPayload {
  orderDate: string;
  dailyAt: string;
  timezone?: string;
  lastRunDate?: string;
  lastRunAt?: string;
  serverNow?: string;
  lateStart?: boolean;
}

/* ── Quem mudou de dia precisa avisar ──
 *
 * Nem tudo que busca por data passa pelo store de instances: a sidebar pede
 * `/instances/summary` e `/instances/page`, o ViewPoint pede o dashboard. Esses
 * disparam no mount, ANTES da primeira resposta do /api/daily/status — sem um
 * aviso eles ficam presos na data que o browser chutou (visto ao vivo: o board
 * corrigiu pra 07-30 e o summary continuou pedindo 07-31). Quem busca por data
 * assina aqui e refaz a busca quando o dia realmente muda.
 */
type DateListener = (date: string) => void;
const listeners = new Set<DateListener>();

export function onBusinessDateChange(fn: DateListener): () => void {
  listeners.add(fn);
  return () => { listeners.delete(fn); };
}

/**
 * Busca o status da daily e publica a data de negócio. Devolve o payload (o
 * rodapé do Monitoring e o ScheduleEditor consomem o resto dele).
 *
 * Falha de rede NÃO limpa a data conhecida: melhor seguir no último dia que o
 * server confirmou do que voltar pro relógio do browser no meio de um hiccup.
 */
export async function syncDailyStatus(): Promise<DailyStatusPayload | null> {
  const before = todayOrderDate();
  try {
    const st = await api<DailyStatusPayload>("/api/daily/status");
    if (st?.orderDate) setBusinessDate(st.orderDate);
    return st;
  } catch {
    return null;
  } finally {
    const now = todayOrderDate();
    if (now !== before) {
      for (const fn of listeners) {
        try { fn(now); } catch (err) { console.error("[business-date] listener error", err); }
      }
    }
  }
}

/** `true` quando a data de negócio MUDOU nesta sincronização (virada da daily). */
export async function syncBusinessDate(): Promise<boolean> {
  const before = todayOrderDate();
  await syncDailyStatus();
  return todayOrderDate() !== before;
}
