/**
 * Scheduler Runtime — Fase 9
 *
 * Orquestra o ciclo diário:
 *   1. `runDaily(defs)` materializa uma JobInstance por definition
 *      habilitada, em estado WAITING.
 *   2. `tickOnce()` promove WAITING→RUNNING (se deps OK e hora chegou),
 *      invoca o executor via container.executorFor, e finaliza
 *      OK/NOTOK. Também cancela instances cujas dependências falharam
 *      em condição incompatível.
 *   3. `startScheduler()` agenda `tickOnce` em intervalo fixo.
 *
 * Não toca em UI. Toda mudança flui pelo `instance-store` (que emite
 * eventos para o React subscribir).
 */

import {
  type JobDefinition,
  type JobInstance,
  type EdgeCondition,
  todayOrderDate,
  createInstance,
  EDGE_CONDITION_DEFAULT,
} from "@/lib/orchestrator-model";
import {
  getTodayInstances,
  updateInstanceStatus,
  getInstances,
} from "@/lib/instance-store";
import { container } from "@/lib/container";
import { localLoad, localSave } from "@/lib/persistence";

const INSTANCES_KEY = "regente:instances";

/** Flag de persistência para evitar rodar daily duas vezes no mesmo dia. */
const DAILY_FLAG_KEY = "regente:daily-run-at";

export function getLastDailyRun(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(DAILY_FLAG_KEY);
}

/**
 * Executa a carga diária. Para cada definition habilitada, cria uma
 * JobInstance em WAITING com `scheduledAt` calculado a partir do cron
 * (ou `Date.now()` quando não houver parse válido).
 *
 * Retorna as instances criadas.
 */
export function runDaily(defs: JobDefinition[]): JobInstance[] {
  const today = todayOrderDate();
  const existing = getInstances(today);
  const existingDefIds = new Set(existing.map((i) => i.definitionId));

  const created: JobInstance[] = [];
  const all = localLoad<JobInstance>(INSTANCES_KEY);

  for (const def of defs) {
    if (!def.schedule?.enabled) continue;
    if (existingDefIds.has(def.id)) continue; // já tem instance hoje

    const scheduledAt = computeScheduledAt(def);
    const inst = createInstance(def, scheduledAt, false);
    all.push(inst);
    created.push(inst);
  }

  localSave(INSTANCES_KEY, all, 500);
  if (typeof window !== "undefined") {
    window.localStorage.setItem(DAILY_FLAG_KEY, new Date().toISOString());
  }
  // Notifica subscribers do instance-store via write subsequente.
  // (localSave não emite — fazemos um no-op update para disparar.)
  if (created.length > 0) pokeInstanceStore();
  return created;
}

/**
 * Limpa a flag de daily (útil para re-executar durante dev/teste).
 */
export function clearDailyFlag(): void {
  if (typeof window !== "undefined") {
    window.localStorage.removeItem(DAILY_FLAG_KEY);
  }
}

/**
 * Calcula um `scheduledAt` razoável a partir do cron. Para MVP,
 * se o cron contém `H M * * *` pegamos H:M de hoje; caso contrário
 * usamos "now" (o job estará pronto imediatamente).
 */
function computeScheduledAt(def: JobDefinition): Date {
  const expr = def.schedule?.cronExpression?.trim() ?? "";
  const parts = expr.split(/\s+/);
  if (parts.length >= 2) {
    const m = Number(parts[0]);
    const h = Number(parts[1]);
    if (!Number.isNaN(m) && !Number.isNaN(h)) {
      const d = new Date();
      d.setHours(h, m, 0, 0);
      return d;
    }
  }
  return new Date();
}

/* ──────────────────────────────────────────────────────────────
   Tick — avança lifecycle de instances
   ────────────────────────────────────────────────────────────── */

/** Evita executar a mesma instance em paralelo no tick. */
const _running = new Set<string>();

/**
 * Avalia condição de uma dependência upstream contra o status da
 * instance correspondente.
 */
function conditionMet(condition: EdgeCondition, upstreamStatus: JobInstance["status"]): boolean {
  if (upstreamStatus === "OK") {
    return condition === "on-success" || condition === "on-complete" || condition === "always";
  }
  if (upstreamStatus === "NOTOK") {
    return condition === "on-failure" || condition === "on-complete" || condition === "always";
  }
  return false; // WAITING/RUNNING/HOLD/CANCELLED → ainda não resolvido
}

/**
 * Verifica se uma dependência é incompatível: pai terminou mas
 * condição jamais será satisfeita. Nesse caso, o filho deve ser
 * cancelado para não ficar preso em WAITING eterno.
 */
function conditionPermanentlyFailed(
  condition: EdgeCondition,
  upstreamStatus: JobInstance["status"],
): boolean {
  if (upstreamStatus === "OK") return condition === "on-failure";
  if (upstreamStatus === "NOTOK") return condition === "on-success";
  if (upstreamStatus === "CANCELLED") return true;
  return false;
}

interface DepEvaluation {
  allReady: boolean;
  anyPending: boolean;
  permanentlyBlocked: boolean;
}

function evaluateDependencies(
  inst: JobInstance,
  defsById: Map<string, JobDefinition>,
  todayInstances: JobInstance[],
): DepEvaluation {
  const def = defsById.get(inst.definitionId);
  const ups = def?.upstream ?? [];
  if (ups.length === 0) {
    return { allReady: true, anyPending: false, permanentlyBlocked: false };
  }

  let allReady = true;
  let anyPending = false;
  let permanentlyBlocked = false;

  for (const u of ups) {
    const cond = u.condition ?? EDGE_CONDITION_DEFAULT;
    const parent = todayInstances.find((i) => i.definitionId === u.from);
    if (!parent) {
      // pai não foi materializado hoje → bloqueia indefinidamente
      allReady = false;
      permanentlyBlocked = true;
      continue;
    }
    if (parent.status === "WAITING" || parent.status === "RUNNING" || parent.status === "HOLD") {
      allReady = false;
      anyPending = true;
      continue;
    }
    if (conditionPermanentlyFailed(cond, parent.status)) {
      allReady = false;
      permanentlyBlocked = true;
      continue;
    }
    if (!conditionMet(cond, parent.status)) {
      allReady = false;
      anyPending = true;
    }
  }

  return { allReady, anyPending, permanentlyBlocked };
}

/**
 * Executa um único tick. Para cada instance WAITING:
 *   - se `scheduledAt` <= agora E deps prontas → RUNNING + executor.execute
 *   - se dep permanentemente bloqueou → CANCELLED
 */
export async function tickOnce(defs: JobDefinition[]): Promise<void> {
  const now = Date.now();
  const today = getTodayInstances();
  const defsById = new Map(defs.map((d) => [d.id, d] as const));

  for (const inst of today) {
    if (inst.status !== "WAITING") continue;
    if (_running.has(inst.id)) continue;

    const dep = evaluateDependencies(inst, defsById, today);

    if (dep.permanentlyBlocked && !dep.anyPending) {
      updateInstanceStatus(inst.id, "CANCELLED", {
        output: { blockedByUpstream: true },
      });
      continue;
    }
    if (!dep.allReady) continue;
    if (inst.scheduledAt > now) continue;

    // Ready to run
    _running.add(inst.id);
    updateInstanceStatus(inst.id, "RUNNING", { attempts: (inst.attempts ?? 0) + 1 });

    const executor = container.executorFor(inst.jobType);
    try {
      const res = await executor.execute(inst);
      if (res.ok) {
        updateInstanceStatus(inst.id, "OK", {
          durationMs: res.durationMs,
          output: res.output,
        });
      } else {
        updateInstanceStatus(inst.id, "NOTOK", {
          durationMs: res.durationMs,
          error: res.error ?? "unknown error",
          output: res.output,
        });
      }
    } catch (e) {
      updateInstanceStatus(inst.id, "NOTOK", {
        error: e instanceof Error ? e.message : String(e),
      });
    } finally {
      _running.delete(inst.id);
    }
  }
}

/* ──────────────────────────────────────────────────────────────
   Scheduler loop
   ────────────────────────────────────────────────────────────── */

let _tickHandle: ReturnType<typeof setInterval> | null = null;
let _currentDefs: JobDefinition[] = [];

/** Atualiza snapshot de definitions usado pelo loop (chamado pela UI). */
export function updateSchedulerDefs(defs: JobDefinition[]): void {
  _currentDefs = defs;
}

export function startScheduler(tickMs = 2000): void {
  if (_tickHandle !== null) return;
  _tickHandle = setInterval(() => {
    void tickOnce(_currentDefs);
  }, tickMs);
}

export function stopScheduler(): void {
  if (_tickHandle !== null) {
    clearInterval(_tickHandle);
    _tickHandle = null;
  }
}

export function isSchedulerRunning(): boolean {
  return _tickHandle !== null;
}

/** Dispara um notify no instance-store relendo e reescrevendo. */
function pokeInstanceStore(): void {
  // localSave dentro de runDaily já persistiu; faz um re-read+write curto
  // para provocar notify via updateInstanceStatus de um no-op, mas isso
  // seria custoso. Em vez disso, fazemos reload síncrono: o UI chama
  // getTodayInstances na próxima render pois V2Preview já reage a
  // onInstanceChange. Para forçar eventos, mutamos atributo inofensivo.
  const all = getInstances();
  for (const i of all) {
    if (i.orderDate === todayOrderDate() && i.status === "WAITING") {
      // Touch: updateInstanceStatus emite notify
      updateInstanceStatus(i.id, "WAITING");
      return;
    }
  }
}
