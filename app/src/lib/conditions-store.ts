/**
 * conditions-store — o POOL de condições visto pelo front (painel Condições do
 * Monitoring + linhas do grafo + gate do modo local).
 *
 * Server mode: espelho de GET /api/conditions, ressincronizado pelos eventos
 * WS `condition.*` (o server emite condition.changed em TODA mudança — job OK,
 * Set OK, ação On/Do, operador, migração).
 *
 * Local mode: pool em localStorage com a MESMA semântica (add/remove, escopo
 * por data '' = permanente). O tick local e o Set OK local aplicam as saídas
 * (ConditionsOutAdd/Remove) aqui — ver applyOutcomesLocal.
 */

import { isServerMode, isResyncEvent, onServerEvent } from "@/lib/server-client";
import { listConditions, setCondition, unsetCondition } from "@/lib/bloco2-api";
import { localLoad, localSave } from "@/lib/persistence";
import { condKey, resolveCondScope } from "@/lib/conditions-model";
import type { JobDefinition } from "@/lib/orchestrator-model";

export interface CondEntry {
  name: string;
  scopeDate: string; // 'YYYY-MM-DD' | '' (permanente/@stat)
  setAt: string | number;
  setBy: string;
}

const LOCAL_KEY = "regente:conditions";

type Listener = (list: CondEntry[]) => void;
const listeners = new Set<Listener>();
let cache: CondEntry[] = [];
let wsSubscribed = false;
let loaded = false;

function notify(): void {
  for (const fn of listeners) {
    try { fn(cache); } catch { /* */ }
  }
}

/* ── carregamento ── */

async function refreshServer(): Promise<void> {
  try {
    cache = (await listConditions("*")).map((c) => ({
      name: c.name, scopeDate: c.scopeDate ?? "", setAt: c.setAt, setBy: c.setBy,
    }));
    loaded = true;
    notify();
  } catch (err) {
    console.warn("[conditions] refresh failed", err);
  }
}

function loadLocal(): void {
  cache = localLoad<CondEntry>(LOCAL_KEY);
  loaded = true;
}

function saveLocal(): void {
  localSave(LOCAL_KEY, cache, 2000);
  notify();
}

function ensure(): void {
  if (isServerMode()) {
    if (!wsSubscribed) {
      wsSubscribed = true;
      onServerEvent((ev) => {
        // condition.changed cobre tudo; set/unset são os broadcasts legados da API.
        if (ev.event === "condition.changed" || ev.event === "condition.set" ||
            ev.event === "condition.unset" || isResyncEvent(ev)) {
          void refreshServer();
        }
      });
      void refreshServer();
    }
    return;
  }
  if (!loaded) loadLocal();
}

/* ── API pública ── */

/** Foto atual do pool (ordenada: mais recente primeiro). */
export function getConditions(): CondEntry[] {
  ensure();
  return [...cache].sort((a, b) => String(b.setAt).localeCompare(String(a.setAt)));
}

export function onConditionsChange(fn: Listener): () => void {
  ensure();
  listeners.add(fn);
  return () => { listeners.delete(fn); };
}

/** Set de lookup p/ poolHas/missingConds (conditions-model). */
export function conditionPool(): Set<string> {
  ensure();
  return new Set(cache.map((c) => condKey(c.name, c.scopeDate)));
}

/** Adiciona uma condição ao pool (operador). scope '' = permanente. */
export async function addCondition(name: string, scope: string, setBy = "operator"): Promise<void> {
  if (isServerMode()) {
    await setCondition(name, scope);
    await refreshServer();
    return;
  }
  ensure();
  cache = cache.filter((c) => !(c.name === name && c.scopeDate === scope));
  cache.push({ name, scopeDate: scope, setAt: new Date().toISOString(), setBy });
  saveLocal();
}

/** Remove uma condição do pool (operador ou consumo). */
export async function removeCondition(name: string, scope: string): Promise<void> {
  if (isServerMode()) {
    await unsetCondition(name, scope);
    await refreshServer();
    return;
  }
  ensure();
  cache = cache.filter((c) => !(c.name === name && c.scopeDate === scope));
  saveLocal();
}

/* ── Local mode: aplicação de saídas (espelho do ApplyOutcomes do server) ── */

/**
 * applyOutcomesLocal — um job terminou OK (ou Set OK) no modo local: adiciona
 * as ConditionsOutAdd e remove as ConditionsOutRemove, no escopo resolvido do
 * sufixo contra o ODAT do job. Chamado pelo instance-store local.
 */
export function applyOutcomesLocal(def: JobDefinition | undefined, odat: string, actor: string): void {
  if (!def || isServerMode()) return;
  ensure();
  let dirty = false;
  for (const n of def.conditionsOutAdd ?? []) {
    const { base, scope } = resolveCondScope(n, odat);
    cache = cache.filter((c) => !(c.name === base && c.scopeDate === scope));
    cache.push({ name: base, scopeDate: scope, setAt: new Date().toISOString(), setBy: actor });
    dirty = true;
  }
  for (const n of def.conditionsOutRemove ?? []) {
    const { base, scope } = resolveCondScope(n, odat);
    const before = cache.length;
    cache = cache.filter((c) => !(c.name === base && c.scopeDate === scope));
    dirty = dirty || cache.length !== before;
  }
  if (dirty) saveLocal();
}
