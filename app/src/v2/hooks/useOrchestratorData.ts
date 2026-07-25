/**
 * useOrchestratorData — bootstrap e sincronização de defs + instances da UI.
 * Extraído do V2Preview.tsx (2026-07-01).
 *
 * Dono do ciclo de vida dos DADOS do canvas: carga inicial, subscribe nos
 * stores (definition-store / runtime-bridge), scheduler local e os eventos WS
 * que afetam dados (definition.changed/deleted, folder.changed, _connected).
 * Eventos de UI (alert.fired/alert.changed) ficam no componente — este hook
 * não conhece toasts de alerta nem badges.
 */

import { useCallback, useEffect, useState } from "react";
import type { JobDefinition, JobInstance } from "@/lib/orchestrator-model";
import {
  getTodayInstances,
  onInstanceChange,
  startScheduler,
  stopScheduler,
  updateSchedulerDefs,
} from "@/lib/runtime-bridge";
import {
  loadDefinitions,
  onDefinitionsChange,
  reloadDefinitions,
} from "@/lib/definition-store";
import { container } from "@/lib/container";
import { onServerEvent, isServerMode } from "@/lib/server-client";
import { listPublishedDefinitions } from "@/lib/adapters/storage/ServerApiAdapter";
import { toast } from "../Toast";

export function useOrchestratorData() {
  const [instances, setInstances] = useState<JobInstance[]>([]);
  const [defs, setDefs] = useState<JobDefinition[]>([]);
  // publishedDefs — as defs do WORKSPACE PRINCIPAL (publicadas), sempre de
  // /api/definitions, IGNORANDO a design session ativa. Em server mode com uma
  // session aberta, `defs` é o DRAFT (session-routed) — mas o que o scheduler
  // ordena é o publicado, então Monitoring/Force enxergam por AQUI. null =
  // modo local (sem conceito de draft → use `defs`).
  const [publishedDefs, setPublishedDefs] = useState<JobDefinition[] | null>(
    isServerMode() ? [] : null,
  );

  const refreshPublished = useCallback(() => {
    if (!isServerMode()) return;
    void listPublishedDefinitions().then(setPublishedDefs).catch(() => {
      /* 401 pré-login etc. — o _connected pós-login recarrega */
    });
  }, []);
  // ready — bootstrap inicial (defs + 1ª carga de instances) já assentou. Enquanto
  // false, a UI NÃO mostra o empty state "Ambiente vazio": no server mode as
  // instances chegam async e o overlay piscava antes do board materializar no F5.
  const [defsLoaded, setDefsLoaded] = useState(false);
  const [instancesLoaded, setInstancesLoaded] = useState(!isServerMode());

  /* ── Mount: load definitions + subscribe ── */
  useEffect(() => {
    const serverMode = container.storageBackend === "server";

    if (!serverMode) {
      // One-shot migration: limpa os 15 fakes do seed v1 que ficaram no
      // localStorage em sessões anteriores. Qualquer instance hoje que
      // venha de uma definition inexistente é órfã e deve sumir.
      if (typeof window !== "undefined") {
        const oldSeedFlag = window.localStorage.getItem("regente:v2-seeded:v1");
        if (oldSeedFlag) {
          window.localStorage.removeItem("regente:instances");
          window.localStorage.removeItem("regente:v2-seeded:v1");
          window.localStorage.removeItem("regente:daily-run-at");
        }
      }
    }

    refreshPublished(); // defs PUBLICADAS (workspace principal) p/ Monitoring/Force
    void loadDefinitions().then((list) => {
      setDefs(list);
      // Purga instances órfãs (sem definition correspondente) — só local mode.
      if (!serverMode && typeof window !== "undefined") {
        const raw = window.localStorage.getItem("regente:instances");
        if (raw) {
          try {
            const arr = JSON.parse(raw) as JobInstance[];
            const ids = new Set(list.map((d) => d.id));
            const cleaned = arr.filter((i) => ids.has(i.definitionId));
            if (cleaned.length !== arr.length) {
              window.localStorage.setItem("regente:instances", JSON.stringify(cleaned));
            }
          } catch { /* ignore */ }
        }
      }
      setInstances(getTodayInstances());
      setDefsLoaded(true);
    });
    const unsubDefs = onDefinitionsChange((list) => {
      setDefs([...list]);
      updateSchedulerDefs([...list]);
    });
    // eslint-disable-next-line react-hooks/set-state-in-effect -- leitura inicial pós-subscribe; mover pra useState quebraria o re-sync quando o efeito re-roda; ver roadmap §RH
    setInstances(getTodayInstances());
    // SEM refiltro por todayOrderDate aqui: getTodayInstances já resolve o "hoje"
    // em cada modo (local filtra por data local; server devolve o dia que o SERVER
    // materializou). Refiltrar pela data local do browser zerava o board no primeiro
    // evento WS quando o dia do cliente ≠ dia do server (acesso remoto/virada de dia).
    const unsubInst = onInstanceChange(() => {
      setInstances(getTodayInstances());
      setInstancesLoaded(true);
    });
    startScheduler(2000);

    // Server mode: subscribe a WS para recarregar defs quando mudarem no server
    let unsubWs: (() => void) | null = null;
    if (isServerMode()) {
      unsubWs = onServerEvent((ev) => {
        // WS (re)conectou — inclui o pós-login (setAuthToken reconecta com o token
        // novo). Recarrega defs que podem ter falhado no mount (401) ou mudado
        // enquanto o canal esteve fora. Instances ressincronizam no próprio store.
        if (ev.event === "_connected") {
          void reloadDefinitions().then((list) => { setDefs([...list]); setDefsLoaded(true); });
          refreshPublished();
          return;
        }
        if (ev.event === "definition.changed" || ev.event === "definition.deleted") {
          void reloadDefinitions().then((list) => setDefs([...list]));
          refreshPublished(); // publish/save no workspace principal muda o publicado
          // Loop GitHub→UI fechado: mudança veio do webhook (push/PR merged no
          // GitHub) → avisa o usuário que as caixinhas mudaram sozinhas.
          const payload = (ev.payload ?? {}) as { reason?: string; sha?: string };
          if (payload.reason === "git-webhook") {
            toast.info("Workspace updated via GitHub", {
              detail: payload.sha ? `main now at ${payload.sha}` : "new commit on main",
            });
          }
        }
        // F11.8 — folder.changed: foldermanager já faz refresh interno; aqui só
        // garantimos que defs sigam coerentes (rename/delete podem ter movido jobs).
        if (ev.event === "folder.changed") {
          void reloadDefinitions().then((list) => setDefs([...list]));
          refreshPublished();
        }
      });
    }

    return () => {
      unsubDefs();
      unsubInst();
      if (unsubWs) unsubWs();
      stopScheduler();
    };
  }, [refreshPublished]); // refreshPublished é estável (useCallback []) — efeito roda 1×

  // Mantém scheduler com defs atuais
  useEffect(() => { updateSchedulerDefs(defs); }, [defs]);

  /** Recarrega defs do backend e publica no estado (bulk move, pós-publish etc.). */
  const reloadDefs = useCallback(async () => {
    const list = await reloadDefinitions();
    setDefs([...list]);
    refreshPublished();
  }, [refreshPublished]);

  /** Re-lê o snapshot de instances do store (Run Daily local, ações em lote). */
  const syncInstances = useCallback(() => {
    setInstances(getTodayInstances());
  }, []);

  return { defs, publishedDefs, instances, ready: defsLoaded && instancesLoaded, reloadDefs, syncInstances };
}
