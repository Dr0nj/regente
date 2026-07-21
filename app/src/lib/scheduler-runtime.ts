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
  todayOrderDate,
  createInstance,
  odateOf,
} from "@/lib/orchestrator-model";
import {
  getTodayInstances,
  updateInstanceStatus,
  getInstances,
} from "@/lib/instance-store";
import { container } from "@/lib/container";
import { localLoad, localSave } from "@/lib/persistence";
import { missingConds } from "@/lib/conditions-model";
import { conditionPool } from "@/lib/conditions-store";

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
 * Executa um único tick. Para cada instance WAITING:
 *   - se `scheduledAt` <= agora E as CONDIÇÕES de entrada existem no pool →
 *     RUNNING + executor.execute (modelo único de condições — a mesma régua
 *     do server: docs/conditions-events.md). Quem cria/apaga condição no pool
 *     é o término OK (instance-store) e o operador (painel Condições);
 *     condição ausente = espera (nunca auto-CANCEL, paridade Control-M).
 */
export async function tickOnce(defs: JobDefinition[]): Promise<void> {
  const now = Date.now();
  const today = getTodayInstances();
  const defsById = new Map(defs.map((d) => [d.id, d] as const));
  const pool = conditionPool();

  for (const inst of today) {
    if (inst.status !== "WAITING") continue;
    if (_running.has(inst.id)) continue;

    // Run Now / Force: uma instance `manual` bypassa as condições — paridade
    // com o ramo forced do server. A janela ainda vale, mas forceRunInstance
    // já zera scheduledAt pra agora.
    if (!inst.manual) {
      const def = defsById.get(inst.definitionId);
      if (missingConds(def, odateOf(inst), pool).length > 0) continue;
    }
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
