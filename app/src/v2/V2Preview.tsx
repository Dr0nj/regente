import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ReactFlow,
  ReactFlowProvider,
  Background,
  BackgroundVariant,
  type Node,
  type Connection,
  type NodeMouseHandler,
  type OnConnect,
  type OnConnectEnd,
  type ReactFlowInstance,
} from "@xyflow/react";
import JobNodeV2 from "./JobNodeV2";
import LaneLabelNode from "./LaneLabelNode";
import MonitoringSidebarV2 from "./MonitoringSidebarV2";
import DesignSidebarV2 from "./DesignSidebarV2";
import CanvasContextMenu, { type ContextMenuItem } from "./CanvasContextMenu";
import BulkActionBar from "./BulkActionBar";
import type { JobType } from "@/lib/job-config";
import type {
  JobInstance,
  JobDefinition,
} from "@/lib/orchestrator-model";
import { todayOrderDate } from "@/lib/orchestrator-model";
import {
  holdInstance,
  releaseInstance,
  deleteInstance,
  cancelInstance,
  rerunInstance,
  skipInstance,
  bypassInstance,
  confirmInstance,
  forceInstance,
  forceRunInstance,
  fetchInstanceById,
  refreshInstancesFromServer,
} from "@/lib/runtime-bridge";
import { listFolders } from "@/lib/folder-api";
import {
  getDefinitions,
  saveDefinition,
  deleteDefinition,
  reloadDefinitions,
} from "@/lib/definition-store";
import {
  getLastDailyRun,
  fetchDailyStatus,
  normalizeDbTime,
} from "@/lib/runtime-bridge";
import { onServerEvent, isServerMode, onAuthEvent, setAuthToken, SERVER_URL } from "@/lib/server-client";
import { fetchMe, loadCachedUser, type AuthUser } from "@/lib/auth-api";
import { getServerVersion } from "@/lib/version-api";
import { LoginForm } from "./LoginForm";
import { UserMenu } from "./UserMenu";
import { UsersDialog } from "./UsersDialog";
import { setAlertNotifier } from "@/lib/alerting";
import { fetchUnacknowledgedCount } from "@/lib/alerts-api";
import { GitStatusBadge } from "./GitStatusBadge";
import { PRBannerHost } from "./PRBannerHost";
import { PublishButton } from "./PublishButton";
import { getDesignSessionId, setDesignSessionId, onDesignSessionChange, onDesignSessionConflict } from "@/lib/server-client";
import { getDesignSession, getDesignSessionStatus, bulkSessionDefinitions, createDesignSession, openSessionFolder, createSessionFolder, listDesignSessions, deleteDesignSession, type DesignSession, type SessionStatus, type PublishResult } from "@/lib/design-session-api";
import { toast, ToastHost } from "./Toast";
import { getGitInfo, commitUrl } from "@/lib/git-info";
import { FolderOpen, Zap, GitCommitHorizontal, LayoutGrid, ChevronLeft, ChevronRight, Code, Wand2, ListChecks, Boxes } from "lucide-react";
import { pauseFolder, resumeFolder } from "@/lib/differentials-api";

// Code-splitting: views/diálogos PESADOS e condicionais saem do chunk inicial
// (React.lazy + Suspense fallback null — todos montam sob flag/estado, então o
// primeiro paint do Monitoring não paga por eles). O canvas (React Flow) fica
// no chunk principal de propósito: é o coração das duas views.
const InstanceDetailsDrawer = lazy(() => import("./InstanceDetailsDrawer"));
const JobConfigDrawer = lazy(() => import("./JobConfigDrawer"));
const FolderManagerDialog = lazy(() => import("./FolderManagerDialog"));
const ScaleMonitor = lazy(() => import("./ScaleMonitor"));
const CodeModeView = lazy(() => import("./CodeModeView"));
const MassUpdateDialog = lazy(() => import("./MassUpdateDialog"));
const ControlMPanel = lazy(() => import("./ControlMPanel").then((m) => ({ default: m.ControlMPanel })));
const AlertsPanel = lazy(() => import("./AlertsPanel").then((m) => ({ default: m.AlertsPanel })));
const SettingsDialog = lazy(() => import("./SettingsDialog").then((m) => ({ default: m.SettingsDialog })));

import "@xyflow/react/dist/style.css";
import "@/index.css";
import "./tokens.css";

// Layout puro (constantes + builders) e minimap extraídos p/ módulos próprios;
// câmera e bootstrap de dados viraram hooks (2026-07-01).
import {
  readLayoutConfig,
  instanceToMonitoring,
  buildMonitoringCanvas,
  buildDesignCanvas,
  type AgentAvailability,
  type Canvas,
  type LayoutConfig,
  type LayoutOverrides,
  type Mode,
} from "./canvas-layout";
import { linkCondName } from "@/lib/conditions-model";
import { conditionPool, onConditionsChange } from "@/lib/conditions-store";
import ConditionsPanel from "./ConditionsPanel";
import ResourcesPanel from "./ResourcesPanel";
import { listAgents } from "@/lib/agents-api";
import { listResources } from "@/lib/bloco2-api";
import NavMinimap from "./NavMinimap";
import { useCanvasCamera } from "./hooks/useCanvasCamera";
import { useOrchestratorData } from "./hooks/useOrchestratorData";

/* ──────────────────────────────────────────────────────────────
   Inner component (tem acesso a useReactFlow)
   ────────────────────────────────────────────────────────────── */

function V2PreviewInner() {
  const [mode, setMode] = useState<Mode>("monitoring");
  // P3/escala — ViewPoint server-driven (paginado/virtualizado), p/ 100k–1M jobs.
  const [scaleView, setScaleView] = useState(false);
  // Diff / Dry Run / Timeline / What-If migraram pro Control Panel (2026-07-18):
  // viraram abas de lá, então o estado de overlay saiu daqui.
  // Dados do canvas (defs + instances) e ciclo de vida deles (carga inicial,
  // subscribes, scheduler local, resync via WS) vivem no hook.
  const { defs, publishedDefs, instances, ready, reloadDefs } = useOrchestratorData();
  // Defs PUBLICADAS (workspace principal) — o que o scheduler realmente ordena.
  // Em server mode com design session ativa, `defs` é o DRAFT da session; o
  // Monitoring e o Force enxergam por AQUI (um job que só existe no draft,
  // nunca publicado, NÃO pode ser forçado nem enriquecer o board). No modo
  // local não há draft: publishedDefs=null e caímos em `defs`.
  const runnableDefs = publishedDefs ?? defs;
  const [selectedInstanceId, setSelectedInstanceId] = useState<string | null>(null);
  const [editingDef, setEditingDef] = useState<{ def: JobDefinition; isNew: boolean } | null>(null);
  const [lastDaily, setLastDaily] = useState<string | null>(getLastDailyRun());
  const [dailyLate, setDailyLate] = useState<boolean>(false); // pontualidade da daily (rodapé)
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; items: ContextMenuItem[] } | null>(null);
  // F11.8 — visibleFolders: null = all visible. Persisted in localStorage.
  const [visibleFolders, setVisibleFolders] = useState<Set<string> | null>(() => {
    if (typeof window === "undefined") return null;
    try {
      const raw = window.localStorage.getItem("regente:visibleFolders");
      if (!raw) return null;
      const arr = JSON.parse(raw) as string[] | null;
      return arr === null ? null : new Set(arr);
    } catch { return null; }
  });
  const [showFolderManager, setShowFolderManager] = useState(false);
  // Job-as-code (2026-07-06; reskin luxo 2026-07-09): o palco do Design vira editor YAML.
  const [codeMode, setCodeMode] = useState(false);
  // CTM-3 — Find & Update rico (critério/regex → preview → apply → undo).
  const [showMassUpdate, setShowMassUpdate] = useState(false);
  // F11.10 — auth state
  const [me, setMe] = useState<AuthUser | null>(() => loadCachedUser());
  const [authChecked, setAuthChecked] = useState<boolean>(!isServerMode());
  const [showUsers, setShowUsers] = useState(false);
  const [showControlM, setShowControlM] = useState(false);
  const [showAlerts, setShowAlerts] = useState(false);
  // Minimap de navegação — protótipo opt-in (Settings → Geral), default off.
  const [showMinimap, setShowMinimap] = useState<boolean>(() => typeof window !== "undefined" && window.localStorage.getItem("regente:minimap") === "1");
  const [miniSize, setMiniSize] = useState<{ w: number; h: number }>({ w: 260, h: 168 });
  useEffect(() => {
    const sync = () => setShowMinimap(window.localStorage.getItem("regente:minimap") === "1");
    window.addEventListener("regente:minimap-changed", sync);
    return () => window.removeEventListener("regente:minimap-changed", sync);
  }, []);
  // Versão do build EM EXECUÇÃO (rodapé). Buscada uma vez: ela só muda quando o
  // server reinicia, e aí a UI recarrega junto. É a resposta visual pra
  // "atualizei mesmo?" — trocar o binário no disco não troca o processo.
  // GOTCHA: buscar no mount devolve VAZIO — este componente monta com a tela de
  // login por cima, e /api/version (como todo /api) responde 401 a quem ainda não
  // entrou. Depende do estado de auth, não do mount. Boolean derivado de propósito:
  // `me` troca de identidade a cada refetch e re-dispararia o efeito à toa.
  const [serverVersion, setServerVersion] = useState("");
  const authed = !!me;
  useEffect(() => {
    if (!authed) return;
    let dead = false;
    void getServerVersion().then((v) => {
      if (!dead) setServerVersion(v);
    });
    return () => {
      dead = true;
    };
  }, [authed]);
  // Config da grade de jobs soltos (colunas / max linhas). Pref de visão por browser,
  // editável em Settings; re-layouta o canvas quando muda (evento regente:layout-changed).
  const [layoutCfg, setLayoutCfg] = useState<LayoutConfig>(() => readLayoutConfig());
  useEffect(() => {
    const sync = () => {
      setLayoutCfg(readLayoutConfig());
      // O cap do canvas (regente:legacyCap) também mora em Settings: re-BUSCA o
      // espelho do server (o fetch lê o cap na hora) — o notify() do store já
      // re-renderiza via onInstanceChange do useOrchestratorData.
      void refreshInstancesFromServer();
    };
    window.addEventListener("regente:layout-changed", sync);
    return () => window.removeEventListener("regente:layout-changed", sync);
  }, []);
  // UI-3 — overrides de grade POR FOLDER (do .regente-folder.yaml, via
  // GET /api/folders). Recarrega quando alguém salva layout/renomeia/cria folder.
  const [folderLayouts, setFolderLayouts] = useState<LayoutOverrides | null>(null);
  useEffect(() => {
    if (!isServerMode()) return;
    let dead = false;
    const load = () =>
      listFolders()
        .then((fs) => {
          if (dead) return;
          const m = new Map<string, Partial<LayoutConfig>>();
          for (const f of fs) {
            if (!f.layout) continue;
            const ov: Partial<LayoutConfig> = {};
            if (f.layout.columns && f.layout.columns > 0) ov.columns = f.layout.columns;
            if (f.layout.maxRows && f.layout.maxRows > 0) ov.maxRows = f.layout.maxRows;
            if (ov.columns || ov.maxRows) m.set(f.name, ov);
          }
          setFolderLayouts(m.size > 0 ? m : null);
        })
        .catch(() => {});
    void load();
    const off = onServerEvent((ev) => {
      if (ev.event === "folder.changed" || ev.event === "_connected") void load();
    });
    return () => { dead = true; off(); };
  }, []);
  const [unreadAlerts, setUnreadAlerts] = useState<number>(0);

  // === Design sessions (Etapa 3+4+5, 2026-04-26) ===
  // sessionId === null → picker de folders (FolderManagerDialog) quando entrar em Design.
  // sessionId !== null → habilita PublishButton, e a UI de Design opera no clone.
  const [designSessionId, setDesignSessionIdState] = useState<string | null>(getDesignSessionId());
  const [designSessionNewFolders, setDesignSessionNewFolders] = useState<string[]>([]);
  // P2 (2026-04-26): folders no escopo da session (folders ∪ newFolders).
  // null = sem session (nenhum filtro aplicado por session); Set vazio é estado
  // transitório enquanto carrega — também não filtra (segurança contra esconder tudo).
  const [activeFolders, setActiveFolders] = useState<Set<string> | null>(null);
  // P8 (2026-04-26): drift do clone da session vs origin/<branch>.
  const [sessionStatus, setSessionStatus] = useState<SessionStatus | null>(null);
  useEffect(() => onDesignSessionChange((sid) => setDesignSessionIdState(sid)), []);
  // Session morreu/publicada/descartada → sai do modo código (o editor edita a session).
  // eslint-disable-next-line react-hooks/set-state-in-effect -- lifecycle de session P2/P7/P8; sair do code mode quando a session morre é reset síncrono de estado transitório; ver roadmap §RH
  useEffect(() => { if (!designSessionId) setCodeMode(false); }, [designSessionId]);
  // P7 (2026-04-26): outra aba assumiu a mesma session → libera essa aba.
  useEffect(() =>
    onDesignSessionConflict((sid) => {
      toast.error("Another tab took over this session", {
        detail: `${sid.slice(0, 16)}… — this tab was disconnected to avoid losing edits.`,
      });
      setDesignSessionId(null);
      setDesignSessionNewFolders([]);
    }),
  []);
  // P2: quando entra em session, busca detalhes e popula sessionFolders.
  useEffect(() => {
    if (!designSessionId) {
      // eslint-disable-next-line react-hooks/set-state-in-effect -- lifecycle de session P2; limpa o escopo de folders no ramo sem-session (estado transitório); ver roadmap §RH
      setActiveFolders(null);
      setDesignSessionNewFolders([]);
      return;
    }
    let cancel = false;
    getDesignSession(designSessionId)
      .then((s) => {
        if (cancel) return;
        const all = [...(s.folders ?? []), ...(s.newFolders ?? [])];
        setActiveFolders(new Set(all));
        setDesignSessionNewFolders(s.newFolders ?? []);
      })
      .catch((e: unknown) => {
        if (cancel) return;
        if ((e as { status?: number })?.status === 404) {
          // Session morreu no server (GC/descartada em outra aba): solta o sid
          // persistido pra não continuar apontando pra um draft que não existe.
          // O listener onDesignSessionChange re-roda este effect com null.
          setDesignSessionId(null);
          toast.info("Design session expired on the server", {
            detail: "The working set was released — open the folders again.",
          });
          return;
        }
        // Erro transiente (rede/server fora): NÃO derruba o sid — a session
        // provavelmente ainda existe; sem folders a UI degrada pro empty state.
        setActiveFolders(null);
        setDesignSessionNewFolders([]);
      });
    return () => { cancel = true; };
  }, [designSessionId]);
  // P8: polling 30s do drift status enquanto session ativa.
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- lifecycle de session P8; zera o drift status quando não há session (estado transitório); ver roadmap §RH
    if (!designSessionId) { setSessionStatus(null); return; }
    let cancel = false;
    const tick = () => {
      getDesignSessionStatus(designSessionId)
        .then((s) => { if (!cancel) setSessionStatus(s); })
        .catch(() => { if (!cancel) setSessionStatus(null); });
    };
    tick();
    const id = window.setInterval(tick, 30_000);
    return () => { cancel = true; window.clearInterval(id); };
  }, [designSessionId]);
  // Recuperação de drafts (2026-07-02) — o clone da session sobrevive no server;
  // o que o F5 perdia era só o PONTEIRO (agora persistido no server-client). Aqui:
  //   1. valida a posse do sid restaurado (user trocou no mesmo browser → solta);
  //   2. sessions SUJAS esquecidas do actor viram banner Retomar/Descartar;
  //   3. sessions LIMPAS idosas são auto-descartadas (nada a perder) — o corte de
  //      10min protege uma session limpa ativa em outra aba (polling toca a cada 30s).
  const [orphanSessions, setOrphanSessions] = useState<DesignSession[]>([]);
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- lifecycle de session (boot); sem server/user não há órfãs a reclassificar (estado transitório); ver roadmap §RH
    if (!isServerMode() || !me) { setOrphanSessions([]); return; }
    let cancel = false;
    void listDesignSessions()
      .then((all) => {
        if (cancel) return;
        const mine = all.filter((s) => s.actor === me.username);
        if (designSessionId && !mine.some((s) => s.id === designSessionId)) {
          setDesignSessionId(null);
          return; // effect re-roda com sid=null e reclassifica as órfãs
        }
        const others = mine.filter((s) => s.id !== designSessionId);
        const idleMs = 10 * 60_000;
        for (const s of others) {
          if (!s.dirty && Date.now() - new Date(s.lastTouch).getTime() > idleMs) {
            void deleteDesignSession(s.id).catch(() => {});
          }
        }
        const dirty = others.filter((s) => s.dirty);
        dirty.sort((a, b) => (a.lastTouch < b.lastTouch ? 1 : -1));
        setOrphanSessions(dirty);
      })
      .catch(() => { /* transiente — reavalia no próximo trigger (login/sid) */ });
    return () => { cancel = true; };
  }, [me, designSessionId]);

  const resumeOrphanSession = useCallback(async (sid: string) => {
    setDesignSessionId(sid);
    setOrphanSessions((prev) => prev.filter((s) => s.id !== sid));
    setMode("design");
    // Defs passam a vir do clone da session (roteamento por sid no adapter).
    await reloadDefinitions();
  }, []);

  const discardOrphanSession = useCallback(async (sid: string) => {
    if (!window.confirm("Discard this session? Its unpublished edits will be lost.")) return;
    await deleteDesignSession(sid).catch(() => {});
    setOrphanSessions((prev) => prev.filter((s) => s.id !== sid));
  }, []);
  // F20 — environment label (visual tag)
  const [envLabel, setEnvLabel] = useState<string>("");
  const [showSettings, setShowSettings] = useState(false);
  // F11.9 — multi-selection no canvas (ReactFlow nativo via Shift+click / drag rect)
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const rfInstance = useRef<ReactFlowInstance | null>(null);

  const nodeTypes = useMemo(() => ({ jobV2: JobNodeV2, laneLabel: LaneLabelNode }), []);

  // Disponibilidade de agentes p/ o card WAIT AGENT (azul claro). null = ainda
  // não carregou (canvas não acusa nada). Atualiza em agent.changed/_connected —
  // NÃO por polling: sem agente, nada muda até um agente (re)conectar.
  const [agentAvail, setAgentAvail] = useState<AgentAvailability | null>(null);
  const refreshAgents = useCallback(() => {
    if (!isServerMode()) return;
    void listAgents().then((list) => {
      const ids = new Set<string>();
      const caps = new Set<string>();
      for (const a of list) {
        if (!a.online) continue;
        ids.add(a.id);
        for (const c of a.capabilities ?? []) caps.add(c.trim().toUpperCase());
      }
      setAgentAvail({ ids, caps });
    }).catch(() => {});
  }, []);
  useEffect(() => { refreshAgents(); }, [refreshAgents]);

  // Server mode: o "daily HH:MM" do rodapé vem do SERVER (daily_runs via
  // /api/daily/status), não de localStorage — quem roda a daily é o server, no
  // relógio DELE (meia-noite ou settings.daily_at). Local mode: no-op (null).
  const refreshDailyStatus = useCallback(() => {
    void fetchDailyStatus().then((st) => {
      if (st?.lastRunAt) setLastDaily(normalizeDbTime(st.lastRunAt));
      setDailyLate(!!st?.lateStart);
    });
  }, []);
  useEffect(() => { refreshDailyStatus(); }, [refreshDailyStatus]);

  /* ── Eventos de UI vindos do WS ──
     Dados (defs/instances) ressincronizam no useOrchestratorData/store; aqui
     ficam só os efeitos de UI: alertas (toast + badge) e, no "_connected", o
     resync dos itens "fetch-once" sem outro caminho de recuperação (badge de
     alertas, env label e o /me — server fora do ar no mount deixava o
     LoginForm mesmo com token válido). */
  const meRef = useRef(me);
  useEffect(() => { meRef.current = me; }, [me]);
  useEffect(() => {
    if (!isServerMode()) return;
    return onServerEvent((ev) => {
      // Daily rodou no server (auto ou manual) ou settings mudaram (daily_at):
      // atualiza o carimbo do rodapé pela fonte da verdade.
      if (ev.event === "daily.started" || ev.event === "settings.changed") {
        refreshDailyStatus();
      }
      // Agente conectou/desconectou: re-deriva os cards WAIT AGENT.
      if (ev.event === "agent.changed") {
        refreshAgents();
      }
      // Phase 8 — alerta disparado no server: toast + atualiza o badge.
      if (ev.event === "alert.fired") {
        const p = (ev.payload ?? {}) as { ruleName?: string; message?: string; severity?: string };
        const title = p.ruleName ?? "Alert";
        if (p.severity === "critical" || p.severity === "warning") {
          toast.error(title, { detail: p.message });
        } else {
          toast.info(title, { detail: p.message });
        }
        void fetchUnacknowledgedCount().then(setUnreadAlerts);
      }
      // Ciclo de vida: alertas tratados (rerun/set-ok) no server → atualiza o badge.
      if (ev.event === "alert.changed") {
        void fetchUnacknowledgedCount().then(setUnreadAlerts);
      }
      if (ev.event === "_connected") {
        void fetchUnacknowledgedCount().then(setUnreadAlerts);
        refreshDailyStatus();
        refreshAgents();
        if (SERVER_URL) {
          fetch(`${SERVER_URL}/api/env`)
            .then((r) => (r.ok ? r.json() : null))
            .then((data) => { if (data?.label) setEnvLabel(data.label); })
            .catch(() => {});
        }
        if (!meRef.current) {
          void fetchMe().then((u) => { if (u) setMe(u); });
        }
      }
    });
  }, [refreshDailyStatus, refreshAgents]);

  // Alerting (Phase 8) — surface fired alerts as toasts and keep the topbar
  // badge in sync. In local mode the notifier is invoked from instance-store;
  // in server mode the "alert.fired" WS event (handled in the mount effect)
  // drives the toast. Both paths refresh the unread badge.
  useEffect(() => {
    setAlertNotifier((event) => {
      const opts = { detail: event.message };
      if (event.severity === "critical" || event.severity === "warning") {
        toast.error(event.ruleName, opts);
      } else {
        toast.info(event.ruleName, opts);
      }
      void fetchUnacknowledgedCount().then(setUnreadAlerts);
    });
    return () => setAlertNotifier(null);
  }, []);

  // Recompute unread badge on instance changes (alerts fire during updates)
  // and whenever the panel toggles (acknowledge happens inside it).
  useEffect(() => {
    void fetchUnacknowledgedCount().then(setUnreadAlerts);
  }, [instances, showAlerts]);

  // F11.8 — persist visibleFolders
  useEffect(() => {
    if (typeof window === "undefined") return;
    if (visibleFolders === null) {
      window.localStorage.removeItem("regente:visibleFolders");
    } else {
      window.localStorage.setItem("regente:visibleFolders", JSON.stringify([...visibleFolders]));
    }
  }, [visibleFolders]);

  // F11.10 — resolve /me on mount + handle 401 events
  useEffect(() => {
    if (!isServerMode()) return; // authChecked já inicia !isServerMode() (A1) — set redundante
    let cancel = false;

    // F20 — fetch env label (public endpoint, no auth)
    if (SERVER_URL) {
      fetch(`${SERVER_URL}/api/env`)
        .then((r) => r.ok ? r.json() : null)
        .then((data) => { if (!cancel && data?.label) setEnvLabel(data.label); })
        .catch(() => {});
    }

    (async () => {
      const u = await fetchMe();
      if (cancel) return;
      setMe(u);
      setAuthChecked(true);
    })();
    const off = onAuthEvent((ev) => {
      if (ev === "unauthorized") {
        setAuthToken(null);
        setMe(null);
      }
    });
    return () => { cancel = true; off(); };
  }, []);

  // Re-alinhamento Design (Fase 1, 2026-04-27):
  // `activeFolders` é o conjunto de folders abertas para trabalho (multi-select).
  // - Em design: filtro = visibleFolders ∩ activeFolders. Sem activeFolders, NADA é mostrado.
  // - Em monitoring: filtro = visibleFolders apenas (activeFolders ignorado, ver tudo).
  const hasActiveFolders = activeFolders !== null && activeFolders.size > 0;
  const effectiveFolders = useMemo<Set<string> | null>(() => {
    if (mode === "design") {
      // Design SEMPRE filtra por activeFolders. Sem folder ativa = empty set (nada).
      if (!hasActiveFolders) return new Set<string>();
      if (visibleFolders === null) return activeFolders;
      const inter = new Set<string>();
      for (const f of activeFolders!) if (visibleFolders.has(f)) inter.add(f);
      return inter;
    }
    // Monitoring: comportamento F11.8 (visibleFolders apenas).
    return visibleFolders;
  }, [mode, hasActiveFolders, activeFolders, visibleFolders]);
  const filteredDefs = useMemo(() => {
    if (effectiveFolders === null) return defs;
    return defs.filter((d) => effectiveFolders.has(d.team ?? ""));
  }, [defs, effectiveFolders]);
  // Versão PUBLICADA do filtro — alimenta o canvas do Monitoring (as arestas/
  // waitConfirm/waitAgent devem refletir a def que o SCHEDULER usa, não o draft).
  const filteredRunnableDefs = useMemo(() => {
    if (effectiveFolders === null) return runnableDefs;
    return runnableDefs.filter((d) => effectiveFolders.has(d.team ?? ""));
  }, [runnableDefs, effectiveFolders]);
  // Draft def: mostra o nó no canvas enquanto a definition nova está sendo
  // configurada no drawer (antes de salvar). Some quando salva (vira real)
  // ou quando o drawer é fechado.
  const designDefsWithDraft = useMemo(() => {
    if (mode !== "design") return filteredDefs;
    if (!editingDef || !editingDef.isNew) return filteredDefs;
    if (filteredDefs.some((d) => d.id === editingDef.def.id)) return filteredDefs;
    return [...filteredDefs, editingDef.def];
  }, [mode, filteredDefs, editingDef]);
  const filteredInstances = useMemo(() => {
    if (effectiveFolders === null) return instances;
    const defsById = new Map(runnableDefs.map((d) => [d.id, d] as const));
    return instances.filter((i) => {
      const team = i.team || defsById.get(i.definitionId)?.team || "";
      return effectiveFolders.has(team);
    });
  }, [instances, runnableDefs, effectiveFolders]);

  // POOL de condições — o "lugar" único que as linhas do grafo refletem e que
  // o painel Condições lista. Ressincronizado via WS (server) / storage (local).
  const [condPool, setCondPool] = useState<ReadonlySet<string>>(() => conditionPool());
  const [showConditions, setShowConditions] = useState(false);
  // Painel Recursos (pool de quotas) — irmão do Condições, só no Monitoring.
  const [showResources, setShowResources] = useState(false);
  useEffect(() => onConditionsChange(() => setCondPool(conditionPool())), []);

  // Pool de recursos (F15) p/ o card WAIT RESOURCE — mesma fonte do painel
  // Recursos. Sem WS de recurso, poll curto no Monitoring; alimenta o canvas.
  const [resourcePool, setResourcePool] = useState<Map<string, { capacity: number; used: number }>>(new Map());
  useEffect(() => {
    if (mode !== "monitoring" || !isServerMode()) return;
    let cancel = false;
    const load = () => listResources().then((rs) => {
      if (cancel) return;
      setResourcePool(new Map(rs.map((r) => [r.name, { capacity: r.capacity, used: r.used }])));
    }).catch(() => {});
    load();
    const i = setInterval(load, 4000);
    return () => { cancel = true; clearInterval(i); };
  }, [mode]);

  const canvas = useMemo<Canvas>(
    () => (mode === "monitoring" ? buildMonitoringCanvas(filteredInstances, filteredRunnableDefs, layoutCfg, agentAvail, folderLayouts, condPool, resourcePool) : buildDesignCanvas(designDefsWithDraft, layoutCfg, folderLayouts)),
    [mode, filteredInstances, filteredRunnableDefs, designDefsWithDraft, layoutCfg, agentAvail, folderLayouts, condPool, resourcePool],
  );

  // Seleção threaded no prop CONTROLADO de nós. O ReactFlow guarda seleção no
  // store interno, mas como passamos `nodes` controlado e o canvas é rebuildado
  // a cada tick (monitoring) ou ao abrir o drawer (design), a seleção interna do
  // RF era ZERADA no rebuild — o highlight piscava e sumia. Aqui reaplicamos
  // `selected` a partir do selectedIds (fonte da verdade, alimentado pelo
  // onSelectionChange) para que o destaque neon PERSISTA entre rebuilds.
  // Sem seleção, devolvemos canvas.nodes intacto (o RF já mostra tudo sem halo).
  // Geometria/câmera continuam em canvas.nodes — selecionar NÃO mexe na câmera.
  // SEMPRE mapeia com `selected` booleano explícito — inclusive com a seleção
  // vazia. Com `selected: undefined` o RF preservava a seleção INTERNA dele
  // (que um micro-drag de clique rápido ativa), acumulando nós destacados que
  // o selectedIds não conhecia — o "travou com 2 selecionados" dos cliques
  // rápidos alternados: sem estado nosso, nada limpava o highlight fantasma.
  const displayNodes = useMemo<Node[]>(() => {
    return canvas.nodes.map((n) => {
      const sel = n.type !== "laneLabel" && selectedIds.has(n.id.replace(/^[md]-/, ""));
      return sel === !!n.selected ? (n.selected === undefined ? { ...n, selected: false } : n) : { ...n, selected: sel };
    });
  }, [canvas.nodes, selectedIds]);

  // Abrir/criar folder no Design — absorvido do antigo FolderOpener pro botão
  // FOLDERS (FolderManagerDialog). Mantém a semântica de design-session: cria a
  // session lazily, abre/cria via API da session, rastreia folders novas pro PR.
  const addFolder = useCallback(async (action: "open" | "create", name: string) => {
    const trimmed = name.trim();
    if (!trimmed) return;
    let sid = designSessionId;
    const creatingSession = !sid;
    if (!sid) {
      // Sem session ativa: se existe session SUJA esquecida deste actor, RETOMA
      // ela em vez de criar outra por cima (era isso que "bugava" abrir folder
      // sobre folder pós-F5: duas sessions do mesmo actor, trabalho invisível).
      // Sessions limpas não retomam — o server auto-descarta as idle no Create.
      const dirtyMine = orphanSessions.length > 0
        ? orphanSessions
        : (await listDesignSessions().catch(() => [] as DesignSession[]))
            .filter((s) => s.actor === (me?.username ?? "") && s.dirty)
            .sort((a, b) => (a.lastTouch < b.lastTouch ? 1 : -1));
      if (dirtyMine.length > 0) {
        sid = dirtyMine[0].id;
        toast.info("Resuming unpublished session", {
          detail: `Your earlier edits (${dirtyMine[0].folders.join(", ") || "no folders"}) are still in it — publish or discard whenever you want.`,
        });
      } else {
        const sess = await createDesignSession([], []);
        sid = sess.id;
      }
    }
    // Cria/abre a folder na session ANTES de publicar o sid no global. Assim, quando
    // o effect [designSessionId] refetcha getDesignSession, a folder já existe →
    // sem corrida que zere activeFolders/newFolders (bug do "Publish não apareceu").
    const res = action === "open"
      ? await openSessionFolder(sid, trimmed)
      : await createSessionFolder(sid, trimmed);
    if (creatingSession) setDesignSessionId(sid);
    setActiveFolders((prev) => {
      const next = new Set(prev ?? []);
      next.add(res.name);
      return next;
    });
    if (res.willForcePR) {
      setDesignSessionNewFolders((prev) => prev.includes(res.name) ? prev : [...prev, res.name]);
    }
    await reloadDefinitions();
  }, [designSessionId, orphanSessions, me]);

  // Fechar folder do working set: só remove da visão (não toca no Git nem na session).
  const closeFolder = useCallback((name: string) => {
    setActiveFolders((prev) => {
      if (!prev) return prev;
      const next = new Set(prev);
      next.delete(name);
      return next;
    });
  }, []);

  // Publish/Discard da design-session — extraídos para reuso na topbar E dentro do
  // modal de FOLDERS (o commit precisa estar visível na própria tela de criação).
  const handlePublished = useCallback(async (res: PublishResult) => {
    if (res.mode === "noop") {
      toast.info("Nothing to publish", { detail: "Working tree is clean — make an edit first." });
      return;
    }
    setDesignSessionId(null);
    setDesignSessionNewFolders([]);
    if (res.prUrl) {
      toast.success(`Published as PR #${res.prNumber}`, { linkUrl: res.prUrl, linkLabel: `PR #${res.prNumber} on GitHub` });
    } else {
      const st = await getGitInfo();
      toast.success("Published to GitHub", {
        detail: `commit ${res.commitSha?.slice(0, 7)}`,
        linkUrl: commitUrl(st, res.commitSha) ?? undefined,
        linkLabel: res.commitSha ? `view commit ${res.commitSha.slice(0, 7)}` : undefined,
      });
    }
    await reloadDefinitions();
  }, []);

  const handleDiscardSession = useCallback(async () => {
    if (!window.confirm("Discard the session? All unpublished edits will be lost.")) return;
    try {
      const sid = designSessionId;
      setDesignSessionId(null);
      setDesignSessionNewFolders([]);
      if (sid) {
        await deleteDesignSession(sid).catch(() => {});
      }
      await reloadDefinitions();
    } catch { /* ignore */ }
  }, [designSessionId]);

  // Chave do CONTEXTO de visão: SÓ o modo (Design/Monitoring). Mudar folders,
  // filtros ou dado NUNCA re-enquadra — a câmera é do usuário e cada aba volta
  // exatamente pra onde ele a deixou; se o conteúdo mudar e ela ficar fora do
  // limite, o clamp do useCanvasCamera puxa de volta o mínimo necessário.
  const viewContextKey = mode === "design" ? "design" : "monitoring";

  // Câmera do canvas (trava de pan + âncora de entrada + centralizações) — todo
  // movimento programático é clampado pelo mesmo limite do translateExtent.
  const { panExtent, organizeView, focusOnPoint, focusNode, setPendingFocusId, attachStage } =
    useCanvasCamera(canvas, mode, viewContextKey);

  const monitoringJobs = useMemo(() => {
    const defsById = new Map(runnableDefs.map((d) => [d.id, d] as const));
    return filteredInstances.map((inst) => {
      const def = defsById.get(inst.definitionId);
      const enriched: JobInstance = def
        ? {
            ...inst,
            team: inst.team || def.team,
            // M1: label CONGELADO vence (mesmo == id); vazio (legado) → def viva.
            label: inst.label || def.label,
            jobType: inst.jobType || def.jobType,
          }
        : inst;
      return instanceToMonitoring(enriched);
    });
  }, [filteredInstances, runnableDefs]);
  const selectedInstance = selectedInstanceId ? instances.find((i) => i.id === selectedInstanceId) : null;

  /* ── Sidebar click → centralize node (focusNode vem do useCanvasCamera) ── */
  // Foco = drawer + câmera na instance clicada. Comum ao clique simples e à
  // faixa do Shift (que foca a ÚLTIMA clicada, não a âncora).
  const focusInstance = useCallback((instId: string) => {
    setSelectedInstanceId(instId);
    focusNode(`m-${instId}`);
    // UI-1 — sidebar windowed: a row clicada pode estar FORA do espelho local
    // (dia > cap do grafo). Puxa a instance pro cache pra o drawer abrir; o
    // syncInstances do onInstanceChange re-renderiza quando ela chegar.
    if (isServerMode() && !instances.some((i) => i.id === instId)) {
      void fetchInstanceById(instId);
    }
  }, [focusNode, instances]);

  // Shift+clique na lista = FAIXA: troca a seleção pelo intervalo (ou soma, com
  // Ctrl/Cmd junto). Os ids já vêm resolvidos pela sidebar — ela é quem conhece
  // a ordem visível (agrupada por folder, filtrada, virtualizada).
  const handleSidebarRange = useCallback((ids: string[], additive: boolean, focusId: string) => {
    setSelectedIds((prev) => {
      const next = additive ? new Set(prev) : new Set<string>();
      for (const id of ids) next.add(id);
      return next;
    });
    focusInstance(focusId);
  }, [focusInstance]);

  const handleSidebarSelect = useCallback((instId: string, additive = false) => {
    // A row da sidebar é uma forma de SELECIONAR o job, não só de olhar pra ele:
    // alimenta o mesmo `selectedIds` do onNodeClick (fonte única do highlight
    // neon e do que a BulkActionBar opera). Antes daqui só saíam a câmera e o
    // drawer, então o card ficava sem destaque e Shift/Ctrl na lista não montava
    // seleção múltipla — o bulk delete era inalcançável pela sidebar.
    setSelectedIds((prev) => {
      if (additive) {
        const next = new Set(prev);
        if (next.has(instId)) next.delete(instId); else next.add(instId);
        return next;
      }
      if (prev.size === 1 && prev.has(instId)) return prev; // já é o único
      return new Set([instId]);
    });
    focusInstance(instId);
  }, [focusInstance]);

  /* ── Canvas node click ── */
  const onNodeClick: NodeMouseHandler = useCallback((evt, node) => {
    // Highlight de seleção — o onNodeClick é AUTORITATIVO (fonte da verdade =
    // selectedIds; displayNodes força o `selected` do RF). Não dependemos da
    // seleção nativa do RF no clique: ela briga com o prop controlado (dava o
    // "preciso clicar 2×" e o acúmulo A+B ao clicar em jobs diferentes).
    // Clique simples = seleção ÚNICA (troca); Shift/Ctrl/Cmd = alterna (multi),
    // casando com o multiSelectionKeyCode e o rubber-band. Lanes não selecionam.
    if (node.type !== "laneLabel") {
      const bare = node.id.replace(/^[md]-/, "");
      const additive = evt.shiftKey || evt.metaKey || evt.ctrlKey;
      setSelectedIds((prev) => {
        if (additive) {
          const next = new Set(prev);
          if (next.has(bare)) next.delete(bare); else next.add(bare);
          return next;
        }
        if (prev.size === 1 && prev.has(bare)) return prev; // já é o único selecionado
        return new Set([bare]);
      });
    }
    if (mode === "monitoring") {
      const id = node.id.replace(/^m-/, "");
      setSelectedInstanceId(id);
    } else {
      const id = node.id.replace(/^d-/, "");
      const def = defs.find((d) => d.id === id);
      if (def) setEditingDef({ def, isNew: false });
    }
  }, [mode, defs]);

  /* ── Drag & drop de palette ── */
  const onDragOver = useCallback((e: React.DragEvent) => {
    if (mode !== "design") return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "copy";
  }, [mode]);

  const onDrop = useCallback((e: React.DragEvent) => {
    if (mode !== "design") return;
    e.preventDefault();
    const type = e.dataTransfer.getData("application/regente-jobtype") as JobType;
    if (!type) return;
    const rf = rfInstance.current;
    if (!rf) return;
    // Posição do drop é ignorada — o canvas organiza por swimlane.
    // Criamos um ID sugerido único.
    const suggestedId = `${type.toLowerCase()}-${Date.now().toString(36).slice(-5)}`;
    // Pré-seleciona folder se houver apenas uma ativa na session.
    const folderList = activeFolders ? Array.from(activeFolders) : [];
    const draftTeam = folderList.length === 1 ? folderList[0] : "";
    const draft: JobDefinition = {
      id: suggestedId,
      label: suggestedId,
      jobType: type,
      team: draftTeam,
      // Horário começa VAZIO de propósito: o operador escolhe a janela/horário no
      // drawer. Sem runAt e sem janela, o daily ordena o job pra rodar já (sem trava
      // de horário) — o padrão 06:00 antigo escondia esse passo e confundia testes.
      schedule: { enabled: true, frequency: "daily" },
      // Retry começa em 0 de propósito: job novo roda UMA vez: quem quer
      // re-tentativa liga explicitamente no drawer. O default 2 antigo
      // escondia re-execuções que o operador não pediu.
      retries: 0,
      timeout: 300,
    };
    setEditingDef({ def: draft, isNew: true });
  }, [mode, activeFolders]);

  // D-13 — cria um job novo a partir da FORMA de um template (id/team frescos;
  // o server já descartou id/team/upstream do molde, mas garantimos aqui também).
  const createFromTemplate = useCallback((tpl: JobDefinition) => {
    const suggestedId = `${(tpl.jobType || "job").toLowerCase()}-${Date.now().toString(36).slice(-5)}`;
    const folderList = activeFolders ? Array.from(activeFolders) : [];
    const draftTeam = folderList.length === 1 ? folderList[0] : "";
    setEditingDef({
      def: { ...tpl, id: suggestedId, label: tpl.label || suggestedId, team: draftTeam, upstream: undefined },
      isNew: true,
    });
  }, [activeFolders]);

  /* ── connectDefs — a SETINHA escreve CONDIÇÕES (modelo único, 2026-07-17) ──
     Ligar A→B cria a condição A-TO-B: saída＋ no produtor, entrada + saída−
     (deleção automática) no consumidor — exatamente o que o usuário faria
     digitando à mão no drawer; os dois caminhos vão pro MESMO lugar (o pool).
     Sem modal: a data default é Odate (ajustável no drawer, seletor por linha). */
  const connectDefs = useCallback((fromId: string, toId: string) => {
    if (!fromId || !toId || fromId === toId) return;
    const producer = defs.find((d) => d.id === fromId);
    const consumer = defs.find((d) => d.id === toId);
    if (!producer || !consumer) return;
    const name = linkCondName(fromId, toId);
    const has = (list: string[] | undefined, v: string) => (list ?? []).includes(v);
    if (has(producer.conditionsOutAdd, name) && has(consumer.conditionsIn, name)) return; // já ligados
    const save = async () => {
      if (!has(producer.conditionsOutAdd, name)) {
        await saveDefinition({ ...producer, conditionsOutAdd: [...(producer.conditionsOutAdd ?? []), name] });
      }
      if (!has(consumer.conditionsIn, name) || !has(consumer.conditionsOutRemove, name)) {
        await saveDefinition({
          ...consumer,
          conditionsIn: has(consumer.conditionsIn, name) ? consumer.conditionsIn : [...(consumer.conditionsIn ?? []), name],
          conditionsOutRemove: has(consumer.conditionsOutRemove, name) ? consumer.conditionsOutRemove : [...(consumer.conditionsOutRemove ?? []), name],
        });
      }
      toast.success(`Condition ${name} created`, {
        detail: `${producer.label} adds it on OK · ${consumer.label} waits for it and deletes it on OK`,
      });
    };
    void save().catch((e) => {
      toast.error("Failed to save the link condition", { detail: e instanceof Error ? e.message : String(e) });
    });
  }, [defs]);

  const onConnect: OnConnect = useCallback((conn: Connection) => {
    if (mode !== "design") return;
    if (!conn.source || !conn.target) return;
    // O RF normaliza source/target mesmo quando o arrasto COMEÇOU pela bolinha
    // de cima (target): puxar o topo de A até a base de B chega aqui como
    // {source: B, target: A} — A passa a depender de B, como o usuário pediu.
    connectDefs(conn.source.replace(/^d-/, ""), conn.target.replace(/^d-/, ""));
  }, [mode, connectDefs]);

  /* ── onConnectEnd — soltar a bolinha SOBRE O CARD também liga ──
     O RF só completa a conexão quando o mouse cai no handle certo; na prática
     o usuário solta no CORPO do card do outro job e nada acontecia (o report
     "arrasto e não cria a dependência"). Fallback: conexão não fechou → achamos
     o card sob o cursor e ligamos. A DIREÇÃO segue a bolinha de ORIGEM: a de
     baixo (source) = este job produz pro alvo; a de cima (target) = o alvo
     produz e este job passa a DEPENDER dele. */
  const onConnectEnd: OnConnectEnd = useCallback((event, connectionState) => {
    if (mode !== "design") return;
    if (connectionState.isValid) return; // caiu num handle — o onConnect resolveu
    const from = connectionState.fromNode;
    if (!from) return;
    const pt = "changedTouches" in event ? event.changedTouches[0] : event;
    const el = document.elementFromPoint(pt.clientX, pt.clientY)?.closest(".react-flow__node");
    const toRfId = el?.getAttribute("data-id");
    if (!toRfId || toRfId === from.id) return;
    const fromId = from.id.replace(/^d-/, "");
    const toId = toRfId.replace(/^d-/, "");
    if (connectionState.fromHandle?.type === "target") {
      connectDefs(toId, fromId); // bolinha de cima: o alvo é quem produz
    } else {
      connectDefs(fromId, toId);
    }
  }, [mode, connectDefs]);

  /* ── Save/Delete definition ── */
  const handleSaveDef = useCallback(async (def: JobDefinition) => {
    await saveDefinition(def);
    setEditingDef(null);
    // Job novo NÃO re-enquadra (regra: a câmera só anda por comando do usuário).
    // Pra achar o job: clique nele na sidebar (centraliza) ou botão Organizar.
  }, []);
  // Autosave do drawer (2026-07-17): salva SEM fechar — usado quando o usuário
  // troca de job ou fecha o drawer com edições pendentes (Design é draft; o
  // commit de verdade é o Publish).
  const handleAutoSaveDef = useCallback(async (def: JobDefinition) => {
    await saveDefinition(def);
  }, []);
  // Def VIVA pro drawer: a setinha (connectDefs) salva condições na def
  // ENQUANTO ela pode estar aberta no drawer — com o snapshot congelado do
  // clique, o próximo Save de lá SOBRESCREVIA a ligação recém-criada (raiz do
  // "liguei e a dependência sumiu"). Job novo segue no draft local do clique.
  const editingDefLive = useMemo(() => {
    if (!editingDef || editingDef.isNew) return editingDef;
    const live = defs.find((d) => d.id === editingDef.def.id);
    return live && live !== editingDef.def ? { def: live, isNew: false } : editingDef;
  }, [editingDef, defs]);
  const handleDeleteDef = useCallback(async (id: string) => {
    await deleteDefinition(id);
    // Limpa as condições de LIGAÇÃO automáticas (X-TO-Y) que envolvem o job
    // deletado nas outras defs — senão o consumidor fica preso esperando uma
    // condição que ninguém mais produz. Condições MANUAIS (nomes do usuário)
    // ficam intactas: podem ter outros produtores/consumidores.
    const touches = (n: string) => {
      const base = n.split("@")[0];
      return base.startsWith(`${id.toUpperCase()}-TO-`) || base.endsWith(`-TO-${id.toUpperCase()}`);
    };
    for (const d of getDefinitions()) {
      const cIn = (d.conditionsIn ?? []).filter((n) => !touches(n));
      const cAdd = (d.conditionsOutAdd ?? []).filter((n) => !touches(n));
      const cRm = (d.conditionsOutRemove ?? []).filter((n) => !touches(n));
      if (cIn.length !== (d.conditionsIn ?? []).length ||
          cAdd.length !== (d.conditionsOutAdd ?? []).length ||
          cRm.length !== (d.conditionsOutRemove ?? []).length) {
        await saveDefinition({
          ...d,
          conditionsIn: cIn.length ? cIn : undefined,
          conditionsOutAdd: cAdd.length ? cAdd : undefined,
          conditionsOutRemove: cRm.length ? cRm : undefined,
        });
      }
    }
    setEditingDef(null);
  }, []);

  // A daily é AUTOMÁTICA (server: autoDailyIfDue roda no tick e faz catch-up se
  // o server estava offline na virada). O antigo botão "Run Daily" foi removido
  // (2026-07-18) — materializar um job criado depois da virada é papel do Force.

  const handleRerunInstance = useCallback((id: string) => {
    Promise.resolve(rerunInstance(id)).then((fresh) => {
      if (fresh) setSelectedInstanceId(fresh.id);
    });
  }, []);

  /* ── F11.9 Bulk handlers ── */
  const clearSelection = useCallback(() => setSelectedIds(new Set()), []);

  // Clique no canvas vazio limpa a seleção (RF não faz por padrão com pan custom).
  const onPaneClick = useCallback(() => clearSelection(), [clearSelection]);

  // Rubber-band (Shift+arrasto) é o ÚNICO caminho que deixamos a seleção nativa
  // do RF escrever em selectedIds — cliques em nó são donos do onNodeClick. Sem
  // este gate, o eco do onSelectionChange (RF brigando com o prop controlado)
  // reintroduzia o acúmulo A+B. Marcado por onSelectionStart/onSelectionEnd.
  const rubberBanding = useRef(false);
  const onSelectionStart = useCallback(() => { rubberBanding.current = true; }, []);
  const onSelectionEnd = useCallback(() => { rubberBanding.current = false; }, []);

  // Stable ref required: ReactFlow v12 has onSelectionChange in its effect deps →
  // an inline arrow would create a new reference every render and loop infinitely.
  const handleSelectionChange = useCallback(({ nodes: sel }: { nodes: Node[] }) => {
    if (!rubberBanding.current) return; // só o rubber-band escreve aqui
    setSelectedIds((prev) => {
      const ids = new Set<string>();
      for (const n of sel) {
        if (n.type === "laneLabel") continue;
        ids.add(n.id.replace(/^[md]-/, ""));
      }
      if (ids.size === prev.size && [...ids].every((id) => prev.has(id))) return prev;
      return ids;
    });
  }, []);

  const handleBulk = useCallback(
    async (ids: string[], op: (id: string) => Promise<unknown> | unknown, label = "action") => {
      const results = await Promise.allSettled(ids.map((id) => Promise.resolve(op(id))));
      const failed = results.filter((r) => r.status === "rejected");
      if (failed.length > 0) {
        console.warn(`[bulk] ${failed.length}/${ids.length} failed`, failed);
        toast.error(`Bulk ${label}: ${failed.length} of ${ids.length} failed`, {
          detail: "Details in the browser console.",
        });
      } else {
        toast.success(`Bulk ${label}: ${ids.length} instance${ids.length === 1 ? "" : "s"} ok`);
      }
      clearSelection();
    },
    [clearSelection],
  );

  // F11.8 — bulk move de definitions para outra folder (via session bulk endpoint).
  const handleBulkMoveDefs = useCallback(
    async (ids: string[], targetFolder: string) => {
      if (!designSessionId) {
        toast.error("Bulk move requires an active design session");
        return;
      }
      try {
        const res = await bulkSessionDefinitions(designSessionId, "move-folder", ids, { targetFolder });
        if (res.failed > 0) {
          const firstErr = res.results.find((r) => !r.ok)?.error ?? "";
          toast.error(`Move: ${res.failed} of ${res.total} failed`, { detail: firstErr });
        } else {
          toast.success(`${res.ok} job${res.ok === 1 ? "" : "s"} moved to ${targetFolder}`);
        }
        await reloadDefs();
      } catch (e) {
        toast.error("Bulk move failed", { detail: e instanceof Error ? e.message : String(e) });
      }
      clearSelection();
    },
    [designSessionId, clearSelection, reloadDefs],
  );

  const handleBulkDeleteDefs = useCallback(
    async (ids: string[]) => {
      // delete each def + remove upstream refs
      const allDefs = getDefinitions();
      for (const id of ids) {
        try { await deleteDefinition(id); } catch (e) { console.error("[bulk delete]", id, e); }
      }
      for (const d of allDefs) {
        if (d.upstream?.some((u) => ids.includes(u.from))) {
          try {
            await saveDefinition({ ...d, upstream: d.upstream.filter((u) => !ids.includes(u.from)) });
          } catch (e) { console.error("[bulk delete upstream cleanup]", d.id, e); }
        }
      }
      clearSelection();
    },
    [clearSelection],
  );

  // ESC clears selection (ReactFlow doesn't do this by default)
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && selectedIds.size > 0) {
        clearSelection();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [selectedIds.size, clearSelection]);

  /* ── Force Order (Run Now) ── */
  const [forceMenuOpen, setForceMenuOpen] = useState(false);
  const handleForce = useCallback((def: JobDefinition) => {
    setForceMenuOpen(false);
    // Force só ordena o que está PUBLICADO (o server 404aria de qualquer jeito;
    // aqui a mensagem fica clara): job que só existe no draft → publique antes.
    if (publishedDefs && !publishedDefs.some((d) => d.id === def.id)) {
      toast.error("Job not published yet", {
        detail: "This job only exists in the Design session draft — publish it before ordering (Force).",
      });
      return;
    }
    Promise.resolve(forceInstance(def)).then((fresh) => {
      if (fresh) {
        setSelectedInstanceId(fresh.id);
        // Direciona a câmera pro job recém-forçado quando ele materializar (não
        // reenquadra o board inteiro — só centraliza no novo, mantendo o zoom).
        setPendingFocusId(fresh.id);
      }
    }).catch((err) => {
      console.error("[force] failed", err);
      toast.error("Force Order failed", { detail: err?.message ?? String(err) });
    });
  }, [setPendingFocusId, publishedDefs]);

  /* ── Run Now sobre a instance EXISTENTE (Monitoring) ── */
  // Bypassa os gates de impedimento (janela/deps/conditions/recursos) e roda o
  // MESMO job — não cria uma nova ordem. Agente indisponível e Confirm NÃO são
  // bypassados (o tick segura até resolver).
  const handleRunNowInstance = useCallback((instanceId: string) => {
    // Só seleciona (highlight) — NÃO mexe na câmera. Run Now age sobre um job que
    // já está no board; reenquadrar dava um "pulo" a cada Run Now, ao contrário
    // de Hold/Release/Cancel/Confirm que não movem a câmera. A câmera é do usuário
    // (só drag/wheel/sidebar-click/Organizar movem) — Run Now agora segue a regra.
    setSelectedInstanceId(instanceId);
    Promise.resolve(forceRunInstance(instanceId)).catch((err) => {
      console.error("[run-now] failed", err);
      toast.error("Run Now failed", { detail: err?.message ?? String(err) });
    });
  }, []);

  /* ── Context menu (right-click no canvas) ── */
  const onNodeContextMenu = useCallback(
    (e: React.MouseEvent, node: Node) => {
      e.preventDefault();

      // Design mode: menu por definition
      if (mode === "design") {
        const id = node.id.replace(/^d-/, "");
        const def = defs.find((d) => d.id === id);
        if (!def) return;
        const items: ContextMenuItem[] = [
          { label: "Run Now", tone: "primary", onClick: () => handleForce(def) },
          { label: "Edit",                onClick: () => setEditingDef({ def, isNew: false }) },
          {
            label: "Duplicate",
            onClick: () => {
              // Clona a definition com novo id/label; abre o drawer como NEW
              // (gesto Control-M: criar a partir de um existente).
              const suffix = Date.now().toString(36).slice(-4);
              const clone: JobDefinition = {
                ...def,
                id: `${def.id}-copy-${suffix}`,
                label: `${def.label} (copy)`,
                upstream: def.upstream ? [...def.upstream] : undefined,
              };
              setEditingDef({ def: clone, isNew: true });
            },
          },
          { label: "Delete", tone: "danger", onClick: () => { void handleDeleteDef(def.id); } },
        ];
        setCtxMenu({ x: e.clientX, y: e.clientY, items });
        return;
      }

      // Monitoring mode: menu por instance — a def de referência é a PUBLICADA
      // (o gate real do scheduler), nunca o draft da session.
      const id = node.id.replace(/^m-/, "");
      const inst = instances.find((i) => i.id === id);
      if (!inst) return;
      const def = runnableDefs.find((d) => d.id === inst.definitionId);
      const status = inst.status;

      const items: ContextMenuItem[] = [];

      // Estados derivados do PRÓPRIO card (canvas-layout): mesma régua visual.
      const confirmGate = !inst.confirmed && !!def?.confirm;

      // BUG-5 — job parado no gate CONFIRM (card violeta): as ÚNICAS ações são
      // Confirm e Hold. Sem Run Now (a confirmação não é bypassável) e sem
      // Cancel — a decisão do operador é confirmar ou segurar.
      if (status === "WAITING" && confirmGate) {
        // BUG-6 — rótulo inglês, alinhado com o restante das ações.
        items.push({ label: "Confirm", tone: "primary", onClick: () => { void confirmInstance(inst.id); } });
        items.push({ label: "Hold", onClick: () => { void holdInstance(inst.id); } });
        setCtxMenu({ x: e.clientX, y: e.clientY, items });
        return;
      }

      // Job em HOLD que ainda exige confirmação: dá pra confirmar desde já
      // (ele roda quando for liberado).
      if (status === "HOLD" && confirmGate) {
        items.push({ label: "Confirm", tone: "primary", onClick: () => { void confirmInstance(inst.id); } });
      }

      // Run Now: força ESTA instance (WAITING/HOLD) a bypassar os gates de
      // impedimento e executar — o MESMO job, sem criar nova ordem.
      if (status === "WAITING" || status === "HOLD") {
        items.push({ label: "Run Now", tone: "primary", onClick: () => handleRunNowInstance(inst.id) });
      }

      // Hold / Release / Cancel — hold GERAL (2026-07-16): qualquer status
      // exceto RUNNING (execução já no agente) e o próprio HOLD; o Release
      // restaura o status original (heldFrom), não WAITING cego.
      if (status !== "RUNNING" && status !== "HOLD") {
        items.push({ label: "Hold",   onClick: () => { void holdInstance(inst.id); } });
      }
      if (status === "HOLD") {
        // Job segurado por uma PAUSA DE FOLDER (schemaV14): não pode ser liberado
        // individualmente — só o ▶ Retomar da folder destrava (paridade Control-M).
        // Mostra o item desabilitado apontando o caminho certo em vez de sumir com
        // ele (o server também barra com 409, mas o front nem deixa tentar).
        if (inst.holdScope === "folder") {
          items.push({
            label: "Release (held by the folder)",
            disabled: true,
            onClick: () => {},
          });
        } else {
          items.push({
            // Hold geral: o release restaura o status congelado (heldFrom) —
            // deixa isso explícito quando não é o WAITING clássico.
            label: inst.heldFrom && inst.heldFrom !== "WAITING" ? `Release (back to ${inst.heldFrom})` : "Release",
            tone: "primary",
            onClick: () => {
              Promise.resolve(releaseInstance(inst.id)).catch((err) => {
                toast.error("Release failed", { detail: err instanceof Error ? err.message : String(err) });
              });
            },
          });
        }
        // Delete (Control-M "Delete job"): SÓ aparece em HOLD — RUNNING nunca é
        // deletável (não é segurável); os demais status passam pelo Hold antes.
        items.push({
          label: "Delete",
          tone: "danger",
          onClick: () => {
            if (!window.confirm(`Delete "${inst.label}"?\n\nRemoves the order from the board and from the day — the definition in Design is untouched.`)) return;
            Promise.resolve(deleteInstance(inst.id)).catch((err) => {
              toast.error("Delete failed", { detail: err instanceof Error ? err.message : String(err) });
            });
          },
        });
      }
      // Cancel = MATAR um job RUNNING: kill do processo no agente + finaliza
      // NOTOK sem retry. Só RUNNING — job WAITING se resolve com Set OK/Skip.
      if (status === "RUNNING") {
        items.push({ label: "Cancel", tone: "danger", onClick: () => { void cancelInstance(inst.id); } });
      }

      // Set OK: flip clássico de NOTOK/CANCELLED ou dar um WAITING como OK na hora
      // (sem esperar horário/condição). Confirm gate já tratado acima (return).
      if (status === "NOTOK" || status === "CANCELLED" || status === "WAITING") {
        items.push({ label: "Set OK", tone: "primary", onClick: () => { void bypassInstance(inst.id); } });
      }

      // Rerun: OK ou NOTOK (ou CANCELLED)
      if (status === "OK" || status === "NOTOK" || status === "CANCELLED") {
        items.push({ label: "Rerun", onClick: () => handleRerunInstance(inst.id) });
      }

      // View Output: sempre que houver run (started_at existe)
      if (inst.startedAt) {
        items.push({
          label: "View Output",
          onClick: () => setSelectedInstanceId(inst.id),
        });
      }

      setCtxMenu({ x: e.clientX, y: e.clientY, items });
    },
    [mode, defs, runnableDefs, instances, handleForce, handleRunNowInstance, handleDeleteDef, handleRerunInstance],
  );

  // hasDefs gate o botão Force do Monitoring — conta o PUBLICADO.
  const hasDefs = runnableDefs.length > 0;
  const hasInstances = instances.length > 0;

  // F11.10 — gate render: only show login overlay in server mode after we know
  // there is no user. Local mode skips auth entirely.
  if (isServerMode() && authChecked && !me) {
    return <LoginForm onLogin={(u) => setMe(u)} />;
  }

  return (
    <div
      className="v2-root"
      style={{
        height: "100vh",
        display: "flex",
        flexDirection: "column",
        background: "var(--v2-bg-canvas)",
      }}
    >
      {/* Topbar */}
      <header
        style={{
          margin: "10px 12px 2px",
          height: 58,
          padding: "0 18px",
          borderRadius: 16,
          border: "1px solid var(--v2-border-medium)",
          background: "var(--v2-bg-surface)",
          boxShadow: "0 10px 30px rgba(0,0,0,0.35)",
          display: "flex",
          alignItems: "center",
          gap: 14,
          flexShrink: 0,
          // z-index acima do canvas para dropdowns (UserMenu) escaparem do stacking context.
          position: "relative",
          zIndex: 50,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <img
            className="app-logo"
            src="/logo-r.png"
            alt="Regente"
            height={30}
            style={{ height: 30, width: "auto", objectFit: "contain", display: "block" }}
          />
          <span style={{ fontSize: 20, fontWeight: 500, letterSpacing: "-0.01em", color: "var(--v2-text-primary)" }}>Regente</span>
          {envLabel && (
            <span style={{
              fontSize: 10, fontWeight: 700, letterSpacing: "0.04em",
              padding: "2px 7px", borderRadius: 3,
              background: "var(--v2-accent-brand)", color: "#000",
              lineHeight: 1, textTransform: "uppercase",
            }}>{envLabel}</span>
          )}
        </div>

        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <ChevronLeft size={16} style={{ color: "var(--v2-text-muted)" }} />
          <div
            style={{
              display: "flex", gap: 4, padding: 4,
              background: "var(--v2-bg-elevated)",
              border: "1px solid var(--v2-border-subtle)", borderRadius: 12,
            }}
          >
            {(["design", "monitoring"] as const).map((m) => (
              <button
                key={m}
                onClick={() => { setMode(m); setSelectedInstanceId(null); setEditingDef(null); }}
                style={{
                  padding: "6px 16px",
                  background: mode === m ? "var(--v2-accent-brand)" : "transparent",
                  border: "none", borderRadius: 8,
                  color: mode === m ? "var(--v2-bg-canvas)" : "var(--v2-text-secondary)",
                  fontSize: 11, fontFamily: "var(--v2-font-mono)",
                  letterSpacing: "0.06em", cursor: "pointer",
                  fontWeight: mode === m ? 700 : 500, textTransform: "uppercase",
                  transition: "background 140ms, color 140ms",
                }}
              >{m}</button>
            ))}
          </div>
          <ChevronRight size={16} style={{ color: "var(--v2-text-muted)" }} />
        </div>

        {/* F11.8 — Folders picker button (apenas em design) */}
        {mode === "design" && (
        <button
          onClick={() => setShowFolderManager(true)}
          title="Manage folders / filter visible folders"
          style={{
            padding: "5px 10px",
            background: visibleFolders !== null ? "var(--v2-accent-deep)" : "transparent",
            border: `1px solid ${visibleFolders !== null ? "var(--v2-accent-brand)" : "var(--v2-border-medium)"}`,
            color: visibleFolders !== null ? "var(--v2-accent-brand)" : "var(--v2-text-secondary)",
            borderRadius: 3,
            fontSize: 10, fontFamily: "var(--v2-font-mono)",
            letterSpacing: "0.06em", textTransform: "uppercase",
            cursor: "pointer", fontWeight: 600,
            display: "flex", alignItems: "center", gap: 6,
          }}
        >
          <FolderOpen size={12} />
          <span>Folders</span>
          {visibleFolders !== null && (
            <span style={{
              padding: "0 5px", background: "var(--v2-accent-brand)", color: "#000",
              borderRadius: 2, fontSize: 9, fontWeight: 700,
            }}>{visibleFolders.size}</span>
          )}
        </button>
        )}

        {/* Job-as-code — o palco vira editor YAML do working set (linha luxo). */}
        {mode === "design" && isServerMode() && (
          <button
            className={`code-toggle${codeMode ? " code-on" : ""}`}
            disabled={!designSessionId || !hasActiveFolders}
            title={
              !designSessionId || !hasActiveFolders
                ? "Open a folder first — code mode edits the session's working set"
                : codeMode
                  ? "Back to the visual canvas"
                  : "Jobs as code — edit the working set as YAML (same dialect as Git)"
            }
            style={!designSessionId || !hasActiveFolders ? { opacity: 0.35, cursor: "not-allowed" } : undefined}
            onClick={() => {
              setEditingDef(null);
              setCodeMode((v) => !v);
            }}
          >
            <Code size={12} />
            <span>{codeMode ? "canvas" : "code"}</span>
          </button>
        )}

        {/* CTM-3 — Find & Update rico (mass update com preview + undo). */}
        {mode === "design" && isServerMode() && designSessionId && hasActiveFolders && (
          <button
            onClick={() => setShowMassUpdate(true)}
            title="Find & Update — search by criteria/regex and update in bulk with preview and undo"
            style={{
              padding: "5px 10px",
              background: "transparent",
              border: "1px solid var(--v2-border-medium)",
              color: "var(--v2-text-primary)",
              borderRadius: 3,
              fontSize: 10, fontFamily: "var(--v2-font-mono)",
              letterSpacing: "0.06em", textTransform: "uppercase",
              cursor: "pointer", fontWeight: 600,
              display: "flex", alignItems: "center", gap: 6,
            }}
          >
            <Wand2 size={11} /> Find &amp; Update
          </button>
        )}

        {mode === "monitoring" && (
          <>
            <button
              onClick={() => setScaleView((v) => !v)}
              title="Server-driven ViewPoint — paginated/virtualized, handles 100k–1M jobs/day"
              style={{
                padding: "5px 10px",
                background: scaleView ? "var(--v2-accent-deep)" : "transparent",
                border: `1px solid ${scaleView ? "var(--v2-accent-brand)" : "var(--v2-border-medium)"}`,
                color: scaleView ? "var(--v2-accent-brand)" : "var(--v2-text-primary)",
                borderRadius: 3,
                fontSize: 10, fontFamily: "var(--v2-font-mono)",
                letterSpacing: "0.06em", textTransform: "uppercase",
                cursor: "pointer", fontWeight: 600,
                display: "flex", alignItems: "center", gap: 6,
              }}
            >
              <Zap size={11} /> ViewPoint
            </button>

            <button
              onClick={() => { setShowResources((v) => !v); setShowConditions(false); }}
              title="Resources — the environment's quota pool: capacity and usage of each resource (semaphore). A job with no free unit sits in WAIT RESOURCE. Who consumes what is chosen in Design (Conditions tab → Resources)."
              style={{
                padding: "5px 10px",
                background: showResources ? "rgba(245,158,11,0.15)" : "transparent",
                border: `1px solid ${showResources ? "#f59e0b" : "var(--v2-border-medium)"}`,
                color: showResources ? "#f59e0b" : "var(--v2-text-primary)",
                borderRadius: 3,
                fontSize: 10, fontFamily: "var(--v2-font-mono)",
                letterSpacing: "0.06em", textTransform: "uppercase",
                cursor: "pointer", fontWeight: 600,
                display: "flex", alignItems: "center", gap: 6,
              }}
            >
              <Boxes size={11} /> Resources
            </button>

            <button
              onClick={() => { setShowConditions((v) => !v); setShowResources(false); }}
              title="Conditions — the environment pool: every condition added (by a job ending OK, Set OK, an On/Do action or an operator), with its date. Deleting one here blocks whoever depends on it; adding one releases them."
              style={{
                padding: "5px 10px",
                background: showConditions ? "var(--v2-accent-deep)" : "transparent",
                border: `1px solid ${showConditions ? "var(--v2-accent-brand)" : "var(--v2-border-medium)"}`,
                color: showConditions ? "var(--v2-accent-brand)" : "var(--v2-text-primary)",
                borderRadius: 3,
                fontSize: 10, fontFamily: "var(--v2-font-mono)",
                letterSpacing: "0.06em", textTransform: "uppercase",
                cursor: "pointer", fontWeight: 600,
                display: "flex", alignItems: "center", gap: 6,
              }}
            >
              <ListChecks size={11} /> Conditions
            </button>

            <button
              onClick={() => organizeView(300)}
              title="Organize — reframes the canvas to the same bounds it opened with (jobs already lay themselves out: dependents in a flow, loose ones on a grid)"
              style={{
                padding: "5px 10px",
                background: "transparent",
                border: "1px solid var(--v2-border-medium)",
                color: "var(--v2-text-primary)",
                borderRadius: 3,
                fontSize: 10, fontFamily: "var(--v2-font-mono)",
                letterSpacing: "0.06em", textTransform: "uppercase",
                cursor: "pointer", fontWeight: 600,
                display: "flex", alignItems: "center", gap: 6,
              }}
            >
              <LayoutGrid size={11} /> Organize
            </button>

            <div style={{ position: "relative" }}>
              <button
                onClick={() => setForceMenuOpen((v) => !v)}
                disabled={!hasDefs}
                title={hasDefs ? "Force Order — create an instance now (Run Now)" : "Create definitions in Design first"}
                style={{
                  padding: "5px 10px",
                  background: forceMenuOpen ? "var(--v2-accent-deep)" : "transparent",
                  border: "1px solid var(--v2-border-medium)",
                  color: hasDefs ? "var(--v2-text-primary)" : "var(--v2-text-muted)",
                  borderRadius: 3,
                  fontSize: 10, fontFamily: "var(--v2-font-mono)",
                  letterSpacing: "0.06em", textTransform: "uppercase",
                  cursor: hasDefs ? "pointer" : "not-allowed", fontWeight: 600,
                  display: "flex", alignItems: "center", gap: 6,
                }}
              >
                <Zap size={11} /> Force ▾
              </button>
              {forceMenuOpen && hasDefs && (
                <div
                  style={{
                    position: "absolute",
                    top: "calc(100% + 4px)",
                    left: 0,
                    minWidth: 260,
                    maxHeight: 360,
                    overflowY: "auto",
                    background: "var(--v2-bg-surface)",
                    border: "1px solid var(--v2-border-medium)",
                    borderRadius: 4,
                    boxShadow: "0 6px 20px rgba(0,0,0,0.4)",
                    zIndex: 20,
                    padding: "4px 0",
                  }}
                  onMouseLeave={() => setForceMenuOpen(false)}
                >
                  <div style={{
                    padding: "6px 10px",
                    fontSize: 9,
                    fontFamily: "var(--v2-font-mono)",
                    color: "var(--v2-text-muted)",
                    letterSpacing: "0.08em",
                    textTransform: "uppercase",
                    borderBottom: "1px solid var(--v2-border-subtle)",
                  }}>
                    Order Job — Run Now
                  </div>
                  {/* SÓ defs publicadas: job que existe apenas no draft da design
                      session não aparece aqui (nunca foi pushado — o scheduler
                      nem o conhece; bug reportado 2026-07-13). */}
                  {runnableDefs.map((d) => (
                    <button
                      key={d.id}
                      onClick={() => handleForce(d)}
                      style={{
                        display: "flex",
                        width: "100%",
                        padding: "7px 10px",
                        background: "transparent",
                        border: "none",
                        textAlign: "left",
                        color: "var(--v2-text-primary)",
                        fontSize: 11,
                        fontFamily: "var(--v2-font-sans)",
                        cursor: "pointer",
                        alignItems: "center",
                        gap: 8,
                      }}
                      onMouseEnter={(e) => (e.currentTarget.style.background = "var(--v2-bg-hover)")}
                      onMouseLeave={(e) => (e.currentTarget.style.background = "transparent")}
                    >
                      <span style={{ color: "var(--v2-accent-brand)" }}>▶</span>
                      <span style={{ flex: 1, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
                        {d.label}
                      </span>
                      <span style={{
                        fontSize: 9,
                        fontFamily: "var(--v2-font-mono)",
                        color: "var(--v2-text-muted)",
                        padding: "1px 5px",
                        border: "1px solid var(--v2-border-subtle)",
                        borderRadius: 2,
                      }}>
                        {d.team ?? "—"}
                      </span>
                    </button>
                  ))}
                </div>
              )}
            </div>
          </>
        )}

        <div style={{ flex: 1 }} />

        {/* GitStatusBadge é artefato do Design (ver memory/core/regente-product-model.md). */}
        {mode === "design" && !designSessionId && (
          <GitStatusBadge canSync={!!me && me.role === "admin"} />
        )}
        {/* Abrir/criar folder agora vive 100% no botão FOLDERS (FolderManagerDialog). */}
        {mode === "design" && designSessionId && (
          <>
            <span style={{
              fontSize: 11, fontFamily: "var(--v2-font-mono)",
              color: "var(--v2-text-secondary)", padding: "3px 8px",
              background: "var(--v2-bg-elevated)", borderRadius: 4,
              border: "1px solid var(--v2-border-subtle)",
              display: "inline-flex", alignItems: "center", gap: 6,
            }} title={`session=${designSessionId}`}>
              <GitCommitHorizontal size={12} />
              SESSION • {designSessionId.slice(0, 12)}…
              {designSessionNewFolders.length > 0 && (
                <span style={{ color: "#fa6", marginLeft: 6 }}>+{designSessionNewFolders.length} novos</span>
              )}
            </span>
            {sessionStatus && (sessionStatus.ahead > 0 || sessionStatus.behind > 0) && (
              <span
                style={{
                  fontSize: 10, fontFamily: "var(--v2-font-mono)",
                  color: sessionStatus.behind > 0 ? "#fbb" : "#bfb",
                  padding: "3px 7px",
                  background: sessionStatus.behind > 0 ? "#3b1d1d" : "#1d3b1d",
                  borderRadius: 4,
                  border: `1px solid ${sessionStatus.behind > 0 ? "#533" : "#353"}`,
                }}
                title={
                  sessionStatus.behind > 0
                    ? `main moved ${sessionStatus.behind} commit(s) ahead since the clone — publish will have to resolve the drift`
                    : `${sessionStatus.ahead} commit(s) ahead of main`
                }
              >
                {sessionStatus.ahead > 0 && `↑${sessionStatus.ahead}`}
                {sessionStatus.ahead > 0 && sessionStatus.behind > 0 && " "}
                {sessionStatus.behind > 0 && `↓${sessionStatus.behind}`}
              </span>
            )}
            <PublishButton
              sessionId={designSessionId}
              newFolderCount={designSessionNewFolders.length}
              onPublished={handlePublished}
            />
            <button
              onClick={() => void handleDiscardSession()}
              style={{ background: "transparent", color: "#a66", border: "1px solid #533", padding: "5px 10px", borderRadius: 4, cursor: "pointer", fontSize: 11 }}
            >
              Descartar
            </button>
          </>
        )}

        {me && (
          <UserMenu
            me={me}
            onLogout={() => setMe(null)}
            onOpenUsers={() => setShowUsers(true)}
            onOpenControlM={() => setShowControlM(true)}
            onOpenSettings={() => setShowSettings(true)}
            unreadAlerts={unreadAlerts}
            onOpenAlerts={() => setShowAlerts(true)}
            alertsActive={showAlerts}
          />
        )}

        {/* (as 4 bolinhas de contagem por status saíram daqui: a sidebar ACTIVE
            JOBS já dá o mesmo tracking com pills e números — redundância removida
            a pedido do usuário, 2026-07-13) */}
      </header>

      {/* Stage */}
      <main
        ref={attachStage}
        style={{ flex: 1, position: "relative", minHeight: 0 }}
        onDragOver={onDragOver}
        onDrop={onDrop}
      >
        {/* P3/escala — não monta o canvas legado quando o ViewPoint cobre a tela.
            Job-as-code — nem quando o modo código cobre o palco do Design. */}
        {!(mode === "monitoring" && scaleView) && !(mode === "design" && codeMode) && (
        <ReactFlow
          nodes={displayNodes}
          edges={canvas.edges}
          nodeTypes={nodeTypes}
          // (sem o prop fitView: o fit inicial embutido do RF aplicava DEPOIS do
          // organizeView de entrada — corrida — e deixava a câmera descentrada/fora
          // do extent. A entrada é 100% do organizeView, no gate 0→N.)
          proOptions={{ hideAttribution: true }}
          nodesDraggable={mode === "design"}
          nodesConnectable={mode === "design"}
          elementsSelectable
          // Pan (arrasto) e zoom (roda/pinch/dbl-click) NATIVOS desligados:
          // reimplementados em useCanvasCamera.attachStage com sensibilidade
          // reduzida e clamp por conteúdo (o ReactFlow não expõe knob de
          // velocidade e só clamparia input nativo). Seleção segue no Shift+drag;
          // translateExtent fica só de cinto de segurança.
          panOnDrag={false}
          translateExtent={panExtent}
          zoomOnScroll={false}
          zoomOnPinch={false}
          zoomOnDoubleClick={false}
          selectionOnDrag={false}
          selectionKeyCode="Shift"
          multiSelectionKeyCode={["Shift", "Meta", "Control"]}
          // Cliques rápidos viram micro-DRAGS — com selectNodesOnDrag (default
          // true) o RF selecionava o nó internamente no dragstart, fora do
          // selectedIds (o dono da seleção é o onNodeClick). Acumulava
          // destaque fantasma em 2 nós sob cliques alternados rápidos.
          selectNodesOnDrag={false}
          onNodeClick={onNodeClick}
          onNodeContextMenu={onNodeContextMenu}
          onPaneClick={onPaneClick}
          onConnect={onConnect}
          onConnectEnd={onConnectEnd}
          // Ímã da setinha: soltar PERTO da bolinha fecha a conexão (o nub tem
          // 6px visuais — sem o raio, só um drop cirúrgico conectava).
          connectionRadius={40}
          onSelectionStart={onSelectionStart}
          onSelectionEnd={onSelectionEnd}
          onSelectionChange={handleSelectionChange}
          onInit={(inst) => { rfInstance.current = inst; }}
        >
          <Background variant={BackgroundVariant.Dots} gap={18} size={1} color="#1a1a1a" />
        </ReactFlow>
        )}
        {showMinimap && !(mode === "monitoring" && scaleView) && !(mode === "design" && codeMode) && (
          <>
            <div
              style={{
                position: "absolute", zIndex: 10,
                right: mode === "monitoring" && selectedInstance ? 392 : 16, bottom: 16,
                width: miniSize.w, height: miniSize.h,
                background: "var(--v2-bg-surface)",
                border: "1px solid var(--v2-border-medium)",
                borderRadius: 8, overflow: "hidden",
                boxShadow: "0 6px 20px rgba(0,0,0,0.4)",
              }}
            >
              <NavMinimap nodes={canvas.nodes} width={miniSize.w} height={miniSize.h} onNavigate={focusOnPoint} />
            </div>
            <div
              title="Redimensionar minimap"
              onPointerDown={(e) => {
                e.preventDefault();
                const sx = e.clientX, sy = e.clientY, sw = miniSize.w, sh = miniSize.h;
                const move = (ev: PointerEvent) => setMiniSize({
                  w: Math.min(560, Math.max(170, sw + (sx - ev.clientX))),
                  h: Math.min(380, Math.max(110, sh + (sy - ev.clientY))),
                });
                const up = () => {
                  window.removeEventListener("pointermove", move);
                  window.removeEventListener("pointerup", up);
                };
                window.addEventListener("pointermove", move);
                window.addEventListener("pointerup", up);
              }}
              style={{
                position: "absolute", zIndex: 11, cursor: "nwse-resize",
                right: (mode === "monitoring" && selectedInstance ? 392 : 16) + miniSize.w - 7,
                bottom: 16 + miniSize.h - 7,
                width: 14, height: 14, borderRadius: 4,
                background: "var(--v2-bg-elevated)", border: "1px solid var(--v2-border-strong)",
              }}
            />
          </>
        )}

        {/* Empty state overlay — só depois do bootstrap assentar (ready): no F5 as
            instances chegam async e o "Ambiente vazio" piscava antes do board. */}
        {mode === "monitoring" && !hasInstances && ready && (
          <EmptyState
            title={hasDefs ? "No instance today" : "Empty environment"}
            hint={hasDefs
              ? "The daily materializes jobs automatically at the scheduled time. To run a job right now, use Force."
              : "Go to Design mode and create jobs by dragging types from the palette."}
          />
        )}
        {mode === "design" && !hasActiveFolders && (
          <EmptyState
            title="No folder open"
            hint="Open or create a folder to start working. With no active folder there is nowhere to put jobs."
          />
        )}
        {mode === "design" && hasActiveFolders && designDefsWithDraft.length === 0 && !codeMode && (
          <EmptyState
            title="No definition"
            hint="Drag a type from the palette onto the canvas — or switch to CODE mode and write the jobs as YAML."
          />
        )}

        {/* Job-as-code — editor YAML cobre o palco do Design (linha luxo + guia do schema). */}
        {mode === "design" && codeMode && designSessionId && (
          <Suspense fallback={null}>
            <CodeModeView
              sessionId={designSessionId}
              folders={activeFolders ? Array.from(activeFolders).sort() : []}
              onExit={() => setCodeMode(false)}
              onApplied={reloadDefs}
            />
          </Suspense>
        )}

        {/* Recuperação de draft: sessions sujas esquecidas deste actor (F5, aba
            fechada, outra máquina). Retomar volta EXATAMENTE onde parou. */}
        {mode === "design" && orphanSessions.length > 0 && (
          <div style={{
            position: "absolute", top: 12, left: "50%", transform: "translateX(-50%)",
            zIndex: 30, display: "flex", flexDirection: "column", gap: 8, maxWidth: "min(680px, 90%)",
          }}>
            {orphanSessions.map((s) => (
              <div key={s.id} style={{
                display: "flex", alignItems: "center", gap: 12, padding: "10px 14px",
                background: "var(--v2-bg-surface)",
                border: "1px solid var(--v2-accent-brand)", borderRadius: 8,
                boxShadow: "0 8px 24px rgba(0,0,0,0.45)",
              }}>
                <GitCommitHorizontal size={15} style={{ color: "var(--v2-accent-brand)", flexShrink: 0 }} />
                <div style={{ fontSize: 11, minWidth: 0 }}>
                  <div style={{ fontWeight: 600, color: "var(--v2-text-primary)" }}>
                    Session with unpublished edits
                  </div>
                  <div style={{ color: "var(--v2-text-secondary)", marginTop: 2, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
                    {(s.folders.length > 0 ? s.folders.join(", ") : "no folders open")}
                    {" · last edit "}
                    {new Date(s.lastTouch).toLocaleString("en-GB", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" })}
                  </div>
                </div>
                <div style={{ flex: 1 }} />
                <button
                  onClick={() => void resumeOrphanSession(s.id)}
                  style={{
                    padding: "6px 14px", background: "var(--v2-accent-brand)", border: "none",
                    color: "var(--v2-bg-canvas)", borderRadius: 4, cursor: "pointer",
                    fontSize: 10, fontWeight: 700, fontFamily: "var(--v2-font-mono)",
                    letterSpacing: "0.06em", textTransform: "uppercase", flexShrink: 0,
                  }}
                >Resume</button>
                <button
                  onClick={() => void discardOrphanSession(s.id)}
                  style={{
                    padding: "6px 12px", background: "transparent", border: "1px solid #533",
                    color: "#a66", borderRadius: 4, cursor: "pointer", fontSize: 10,
                    fontFamily: "var(--v2-font-mono)", letterSpacing: "0.06em",
                    textTransform: "uppercase", flexShrink: 0,
                  }}
                >Discard</button>
              </div>
            ))}
          </div>
        )}

        {mode === "monitoring" ? (
          // P3/escala — esconde a sidebar legada (não-virtualizada) sob o ViewPoint.
          scaleView ? null : (
            <MonitoringSidebarV2
              jobs={monitoringJobs}
              selectedId={selectedInstanceId}
              // A lista compartilha a MESMA seleção do canvas: 2 jobs no Ctrl+clique
              // acendem 2 cards E 2 rows, venha o clique de onde vier.
              selectedIds={selectedIds}
              onSelect={handleSidebarSelect}
              onSelectRange={handleSidebarRange}
              // UI-1 — dia > cap: a lista vira windowed server-driven (dia inteiro);
              // o botão do aviso leva pro ViewPoint. O eye do FolderManager vale
              // nos dois modos.
              onOpenViewPoint={() => setScaleView(true)}
              visibleFolders={visibleFolders}
              // D-2 — pause/resume de workflow (server mode; local não tem o endpoint)
              onPauseFolder={isServerMode() ? async (name) => {
                try {
                  const n = await pauseFolder(name);
                  toast.info(`Workflow ${name} paused`, { detail: `${n} job(s) held — state preserved` });
                } catch (e) {
                  toast.error("Pause failed", { detail: e instanceof Error ? e.message : String(e) });
                }
              } : undefined}
              onResumeFolder={isServerMode() ? async (name) => {
                try {
                  const n = await resumeFolder(name);
                  toast.info(`Workflow ${name} resumed`, { detail: `${n} job(s) released` });
                } catch (e) {
                  toast.error("Resume failed", { detail: e instanceof Error ? e.message : String(e) });
                }
              } : undefined}
            />
          )
        ) : (
          // Fase 1: palette de drag só aparece com folder ativa.
          // Sem folder, não há destino válido para drop → esconde para evitar UX quebrada.
          // Modo código: o palco é o editor — palette não tem onde dropar.
          hasActiveFolders && !codeMode ? (
            <DesignSidebarV2
              definitions={filteredDefs}
              onJobClick={(id) => focusNode(`d-${id}`)}
              onUseTemplate={createFromTemplate}
            />
          ) : null
        )}

        {mode === "monitoring" && selectedInstance && (() => {
          // Def de referência = a PUBLICADA (a mesma que o scheduler usa) — nunca
          // o draft da design session ativa.
          const selDef = runnableDefs.find((d) => d.id === selectedInstance.definitionId);
          // Enriquece igual aos cards do canvas (monitoringJobs): se a instance
          // vier com jobType/label/team vazios ou defasados, cai pra def — assim
          // o drawer mostra o MESMO que o card, sem "job type dessincronizado".
          const enriched = selDef ? {
            ...selectedInstance,
            team: selectedInstance.team || selDef.team,
            // M1: label CONGELADO vence (mesmo == id); vazio (legado) → def viva.
            label: selectedInstance.label || selDef.label,
            jobType: selectedInstance.jobType || selDef.jobType,
          } : selectedInstance;
          return (
            <Suspense fallback={null}>
              <InstanceDetailsDrawer
                instance={enriched}
                definition={selDef}
                allDefs={runnableDefs}
                handlers={{
                  onHold: holdInstance,
                  onRelease: releaseInstance,
                  onDelete: (id) => {
                    Promise.resolve(deleteInstance(id))
                      .then(() => setSelectedInstanceId(null)) // a ordem sumiu — fecha o drawer
                      .catch((err) => toast.error("Delete failed", { detail: err instanceof Error ? err.message : String(err) }));
                  },
                  onCancel: cancelInstance,
                  onSkip: skipInstance,
                  onBypass: bypassInstance,
                  onRerun: handleRerunInstance,
                  onConfirm: confirmInstance,
                  onClose: () => setSelectedInstanceId(null),
                }}
              />
            </Suspense>
          );
        })()}

        {mode === "design" && editingDefLive && (
          <Suspense fallback={null}>
            <JobConfigDrawer
              definition={editingDefLive.def}
              isNew={editingDefLive.isNew}
              availableFolders={activeFolders ? Array.from(activeFolders).sort() : []}
              allDefs={defs}
              handlers={{
                onSave: handleSaveDef,
                onAutoSave: handleAutoSaveDef,
                onDelete: handleDeleteDef,
                onClose: () => setEditingDef(null),
              }}
            />
          </Suspense>
        )}

        {ctxMenu && (
          <CanvasContextMenu
            x={ctxMenu.x}
            y={ctxMenu.y}
            items={ctxMenu.items}
            onClose={() => setCtxMenu(null)}
          />
        )}

        {showFolderManager && (
          <Suspense fallback={null}>
            <FolderManagerDialog
              visibleFolders={visibleFolders}
              onChangeVisible={setVisibleFolders}
              activeFolders={activeFolders ?? new Set()}
              onOpenFolder={(name) => addFolder("open", name)}
              onCreateFolder={(name) => addFolder("create", name)}
              onCloseFolder={closeFolder}
              sessionId={designSessionId}
              newFolderCount={designSessionNewFolders.length}
              onPublished={handlePublished}
              onDiscardSession={handleDiscardSession}
              inDesignMode={mode === "design"}
              onOpened={() => { setMode("design"); setShowFolderManager(false); }}
              onClose={() => setShowFolderManager(false)}
            />
          </Suspense>
        )}

        {showUsers && me && me.role === "admin" && (
          <UsersDialog meId={me.id} onClose={() => setShowUsers(false)} />
        )}

        {showControlM && (
          <Suspense fallback={null}>
            <ControlMPanel onClose={() => setShowControlM(false)} />
          </Suspense>
        )}

        {/* CTM-3 — Find & Update rico (preview + undo, transacional por item). */}
        {showMassUpdate && designSessionId && (
          <Suspense fallback={null}>
            <MassUpdateDialog
              sessionId={designSessionId}
              folders={activeFolders ? Array.from(activeFolders).sort() : []}
              defs={filteredDefs}
              presetIds={selectedIds.size > 0 ? [...selectedIds] : undefined}
              onClose={() => setShowMassUpdate(false)}
              onChanged={reloadDefs}
            />
          </Suspense>
        )}

        {showAlerts && (
          <Suspense fallback={null}>
            <AlertsPanel
              onClose={() => setShowAlerts(false)}
              onChange={() => { void fetchUnacknowledgedCount().then(setUnreadAlerts); }}
              isAdmin={me?.role === "admin"}
            />
          </Suspense>
        )}

        {showSettings && (
          <Suspense fallback={null}>
            <SettingsDialog onClose={() => setShowSettings(false)} />
          </Suspense>
        )}
        {/* Diff / Dry Run / Timeline / What-If migraram pro Control Panel (abas). */}

        <PRBannerHost />

        {/* F11.9 — Bulk action bar */}
        {selectedIds.size > 0 && mode === "monitoring" && (
          <BulkActionBar
            mode="monitoring"
            selected={selectedIds}
            instances={filteredInstances}
            handlers={{
              onHoldAll:    (ids) => handleBulk(ids, holdInstance, "hold"),
              onReleaseAll: (ids) => handleBulk(ids, releaseInstance, "release"),
              onDeleteAll:  (ids) => handleBulk(ids, deleteInstance, "delete"),
              onCancelAll:  (ids) => handleBulk(ids, cancelInstance, "cancel"),
              onSetOkAll:   (ids) => handleBulk(ids, bypassInstance, "set-ok"),
              onRerunAll:   (ids) => handleBulk(ids, rerunInstance, "rerun"),
              onClear:      clearSelection,
            }}
          />
        )}
        {selectedIds.size > 0 && mode === "design" && (
          <BulkActionBar
            mode="design"
            selected={selectedIds}
            defs={filteredDefs}
            folders={activeFolders ? Array.from(activeFolders).sort() : []}
            handlers={{
              onDeleteAll: handleBulkDeleteDefs,
              onMoveAll: handleBulkMoveDefs,
              onClear: clearSelection,
            }}
          />
        )}

        {/* Painel Condições — o POOL do ambiente (só no Monitoring, ao lado
            do Organizar): lista/adiciona/deleta as condições com data. */}
        {showConditions && mode === "monitoring" && (
          <ConditionsPanel onClose={() => setShowConditions(false)} />
        )}

        {/* Painel Recursos — o POOL de quotas (só no Monitoring, irmão do
            Condições): capacidade/uso de cada recurso, com ajuste e delete. */}
        {showResources && mode === "monitoring" && (
          <ResourcesPanel onClose={() => setShowResources(false)} />
        )}

        {/* P3/escala — ViewPoint server-driven cobre o canvas quando ligado */}
        {mode === "monitoring" && scaleView && (
          <Suspense fallback={null}>
            <ScaleMonitor onClose={() => setScaleView(false)} />
          </Suspense>
        )}

        <ToastHost />
      </main>

      {/* Footer */}
      <footer
        className="v2-edge-highlight"
        style={{
          height: 24, padding: "0 16px",
          borderTop: "1px solid var(--v2-border-subtle)",
          background: "var(--v2-bg-surface)",
          display: "flex", alignItems: "center", gap: 20,
          fontSize: 10, fontFamily: "var(--v2-font-mono)",
          color: "var(--v2-text-muted)",
          letterSpacing: "0.04em", flexShrink: 0,
        }}
      >
        <span>
          <span style={{ color: "var(--v2-text-secondary)", fontWeight: 500 }}>{defs.length}</span>
          <span style={{ opacity: 0.7 }}> definitions · </span>
          <span style={{ color: "var(--v2-text-secondary)", fontWeight: 500 }}>{instances.length}</span>
          <span style={{ opacity: 0.7 }}> instances · </span>
          <span style={{ color: "var(--v2-text-secondary)", fontWeight: 500 }}>{todayOrderDate()}</span>
        </span>
        {lastDaily && (
          <span>
            <span style={{ opacity: 0.7 }}>daily </span>
            <span style={{ color: "var(--v2-text-secondary)", fontWeight: 500 }}>
              {new Date(lastDaily).toLocaleTimeString("en-GB", { hour12: false })}
            </span>
            <span style={{ opacity: 0.7 }}> · </span>
            <span
              title={dailyLate ? "materialized after the configured time (+5min)" : "materialized on time"}
              style={{ color: dailyLate ? "var(--v2-status-failed)" : "var(--v2-status-ok)", fontWeight: 600 }}
            >
              {dailyLate ? "⏰ late" : "on time"}
            </span>
          </span>
        )}
        {serverVersion && (
          <span style={{ marginLeft: "auto" }} title="Build of the server that is running right now — not the one on disk. After an upgrade, this is what proves the new version took over.">
            <span style={{ opacity: 0.7 }}>build </span>
            <span style={{ color: "var(--v2-text-secondary)", fontWeight: 500 }}>{serverVersion}</span>
          </span>
        )}
        <span style={{ marginLeft: serverVersion ? undefined : "auto", color: "var(--v2-text-secondary)", fontWeight: 600, textTransform: "uppercase" }}>{mode}</span>
      </footer>
    </div>
  );
}

/* ──────────────────────────────────────────────────────────────
   Subcomponents
   ────────────────────────────────────────────────────────────── */

function EmptyState({ title, hint }: { title: string; hint: string }) {
  return (
    <div
      style={{
        position: "absolute",
        inset: 0,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        pointerEvents: "none",
      }}
    >
      <div
        style={{
          padding: "16px 24px",
          background: "var(--v2-bg-surface)",
          border: "1px solid var(--v2-border-medium)",
          borderRadius: 6,
          textAlign: "center",
          maxWidth: 360,
        }}
      >
        <div style={{ fontSize: 13, fontWeight: 600, color: "var(--v2-text-primary)", marginBottom: 6 }}>{title}</div>
        <div style={{ fontSize: 11, color: "var(--v2-text-secondary)", lineHeight: 1.5 }}>{hint}</div>
      </div>
    </div>
  );
}

/* ──────────────────────────────────────────────────────────────
   Root (wraps ReactFlowProvider)
   ────────────────────────────────────────────────────────────── */

export default function V2Preview() {
  return (
    <ReactFlowProvider>
      <V2PreviewInner />
    </ReactFlowProvider>
  );
}
