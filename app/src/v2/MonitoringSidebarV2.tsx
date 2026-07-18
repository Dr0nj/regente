import { useCallback, useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { Lock } from "lucide-react";
import type { JobNodeData } from "@/lib/job-config";
import { useResizablePanel, ResizeHandle } from "./resizable";
import { api, onServerEvent, isServerMode } from "@/lib/server-client";
import { todayOrderDate } from "@/lib/orchestrator-model";
import { legacyCap } from "@/lib/server-instance-store";

/* ──────────────────────────────────────────────────────────────
   MonitoringSidebarV2 — flutuante, clean, densidade alta
   ──────────────────────────────────────────────────────────────
   UI-1 (2026-07-09): lista VIRTUALIZADA de verdade + dual-mode.
   - DOM = só as linhas visíveis (headers de folder + jobs), qualquer volume.
   - Modo LOCAL (dia ≤ cap do canvas): dados do espelho local (props.jobs),
     filtro/busca client-side — comportamento de sempre, DOM enxuto.
   - Modo WINDOWED (dia > cap): o dia INTEIRO sem baixar o dia inteiro —
     estrutura/contagens do /api/instances/summary (totais REAIS) e linhas
     por página do /api/instances/page (offset = random-access no salto de
     scrollbar). Placeholders enquanto a janela carrega; filtro/busca são
     reescopados NO SERVER. Aguenta 1M como o ViewPoint (mesmo padrão).
   - O contador do header NUNCA mostra número truncado como total: é
     "N de <total real do summary>".
   - Alturas virtuais acima de MAX_DISPLAY_H são comprimidas (scrollbar
     mapeada por fator) — 1M×30px = 30M px estoura o limite de altura do
     browser (~17M Firefox) e a precisão float32 de transform (~16.7M).
   ────────────────────────────────────────────────────────────── */

export interface MonitoringJob {
  id: string;
  label: string;
  team: string;
  jobType: JobNodeData["jobType"];
  status: JobNodeData["status"];
  durationMs?: number;
  startedAt?: string;
  /** Origem do HOLD (schemaV14): undefined = não está em hold; "folder" =
   *  segurado por uma pausa de folder (cadeado da folder, sem release individual);
   *  "self" = hold individual do operador (cadeado próprio, liberável 1-a-1). */
  holdScope?: "folder" | "self";
  /** ODAT de origem (YYYY-MM-DD) se a instance foi CARREGADA pela virada da
   *  daily (carry-over); ausente = fresca do dia. Guia o sub-agrupamento por
   *  data dentro da folder (modo local) e o chip ↩ (modo windowed). */
  carriedFrom?: string;
}

type StatusFilter = "ALL" | "RUNNING" | "FAILED" | "SUCCESS" | "WAITING";

const STATUS_DOT: Record<JobNodeData["status"], string> = {
  SUCCESS: "var(--v2-status-ok)",
  RUNNING: "var(--v2-status-running)",
  FAILED: "var(--v2-status-failed)",
  WAITING: "var(--v2-status-waiting)",
  INACTIVE: "var(--v2-text-muted)",
};

const ROW_H = 30;
const HEADER_H = 26;
const PAGE = 200;            // linhas por página windowed (300 no ScaleMonitor; aqui a row é mais rica)
const OVERSCAN_PX = 240;     // margem de pré-render acima/abaixo do viewport
const MAX_DISPLAY_H = 12_000_000; // teto físico da altura do scroller (ver header)

const SERVER_STATUS_TO_UI: Record<string, JobNodeData["status"]> = {
  OK: "SUCCESS",
  NOTOK: "FAILED",
  RUNNING: "RUNNING",
  WAITING: "WAITING",
  HOLD: "INACTIVE",
  HELD: "INACTIVE",
  CANCELLED: "INACTIVE",
};
const FILTER_TO_SERVER: Record<Exclude<StatusFilter, "ALL">, string> = {
  RUNNING: "RUNNING",
  FAILED: "NOTOK",
  SUCCESS: "OK",
  WAITING: "WAITING",
};

function formatDuration(ms?: number): string {
  if (!ms) return "—";
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
  return `${Math.floor(ms / 60000)}m${Math.floor((ms % 60000) / 1000)}s`;
}

function fmtInt(n: number): string {
  return n.toLocaleString("pt-BR");
}

// Cadeado do hold (schemaV14): AMBAR = segurado pela folder inteira (pausa D-2,
// sem release individual); VIOLETA = hold individual do operador (liberável 1-a-1).
// Mesma convenção no card do canvas (JobNodeV2) pra leitura consistente.
const HOLD_FOLDER_COLOR = "#f59e0b";
const HOLD_SELF_COLOR = "#c4b5fd";

// D-2 — botões ⏸/▶ do header da folder (discretos; idempotentes no server).
const folderActionBtn: CSSProperties = {
  background: "transparent",
  border: "1px solid var(--v2-border-subtle)",
  color: "var(--v2-text-muted)",
  borderRadius: 2,
  fontSize: 8,
  width: 18,
  height: 16,
  lineHeight: "12px",
  padding: 0,
  cursor: "pointer",
};

/* ── Windowed data (summary + páginas por folder, mesmo padrão do ScaleMonitor) ── */

interface DaySummary {
  date: string;
  total: number;
  byStatus: Record<string, number>;
  byFolder: Record<string, number>;
  /** Folders com ≥1 job segurado por pausa de folder (D-2/schemaV14) — cadeado
   *  da folder + estado do botão pausar/retomar no modo windowed. */
  pausedFolders?: string[];
}
interface PageInstance {
  id: string;
  definitionId: string;
  team?: string;
  status: string;
  startedAt?: string;
  finishedAt?: string;
  holdScope?: string;
  carriedFrom?: string;
  // M1 (schemaV18): label/jobType CONGELADOS na ordem — vêm na /page também.
  label?: string;
  jobType?: string;
}

function pageToJob(p: PageInstance, folder: string, labelFor?: (defId: string) => string | undefined): MonitoringJob {
  const started = p.startedAt ? Date.parse(p.startedAt) : NaN;
  const finished = p.finishedAt ? Date.parse(p.finishedAt) : NaN;
  const durationMs = Number.isFinite(started)
    ? (Number.isFinite(finished) ? finished - started : Date.now() - started)
    : undefined;
  const upper = p.status?.toUpperCase();
  const held = upper === "HELD" || upper === "HOLD";
  return {
    id: p.id,
    // M1: label CONGELADO na ordem (imutável); labelFor (def viva) e o id são
    // só fallback de instance LEGADA sem backfill.
    label: p.label || labelFor?.(p.definitionId) || p.definitionId,
    team: p.team || folder,
    jobType: (p.jobType ?? "") as JobNodeData["jobType"],
    status: SERVER_STATUS_TO_UI[upper] ?? "WAITING",
    durationMs,
    startedAt: Number.isFinite(started)
      ? new Date(started).toLocaleTimeString("en-GB", { hour12: false }).slice(0, 5)
      : undefined,
    holdScope: held ? (p.holdScope === "folder" ? "folder" : "self") : undefined,
    carriedFrom: p.carriedFrom || undefined,
  };
}

// useDayWindow — espelho windowed do dia: summary (estrutura + totais reais) e
// cache de páginas por folder, carregadas sob demanda pelo scroll. `active`
// desliga tudo (modo local não paga nenhum fetch extra).
function useDayWindow(active: boolean, filter: StatusFilter, search: string, labelFor?: (defId: string) => string | undefined) {
  const [summary, setSummary] = useState<DaySummary | null>(null);
  const [viewSummary, setViewSummary] = useState<DaySummary | null>(null);
  const [version, setVersion] = useState(0);
  const pages = useRef(new Map<string, Map<number, MonitoringJob[]>>());
  const rowIndex = useRef(new Map<string, { folder: string; page: number; off: number }>());
  const inflight = useRef(new Set<string>());
  const bump = useCallback(() => setVersion((v) => v + 1), []);

  const serverStatus = filter !== "ALL" ? FILTER_TO_SERVER[filter] : "";
  const scoped = !!serverStatus || !!search;

  const clearPages = useCallback(() => {
    pages.current.clear();
    rowIndex.current.clear();
    inflight.current.clear();
  }, []);

  const loadSummary = useCallback(async () => {
    if (!active) return;
    const date = todayOrderDate();
    try {
      const s = await api<DaySummary>(`/api/instances/summary?date=${encodeURIComponent(date)}`);
      setSummary(s);
      if (scoped) {
        const qs = new URLSearchParams({ date });
        if (serverStatus) qs.set("status", serverStatus);
        if (search) qs.set("q", search);
        setViewSummary(await api<DaySummary>(`/api/instances/summary?${qs.toString()}`));
      } else {
        setViewSummary(null);
      }
    } catch (err) {
      console.warn("[sidebar] summary failed", err);
    }
  }, [active, scoped, serverStatus, search]);

  // Filtro/busca mudou → o working set é outro: zera páginas e reescopa.
  useEffect(() => {
    clearPages();
    bump();
    void loadSummary();
  }, [loadSummary, clearPages, bump]);

  // Live: mutações atualizam a linha em cache NA HORA (status visível) e o
  // summary com throttle (rajada de 1k eventos ≠ 1k GETs de summary).
  const throttleTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    if (!active) return;
    const off = onServerEvent((ev) => {
      if (ev.event === "daily.started" || ev.event === "_connected") {
        clearPages();
        void loadSummary();
        bump();
        return;
      }
      if (ev.event !== "instance.changed") return;
      const p = ev.payload as { id?: string; status?: string } | undefined;
      if (p?.id && p.status) {
        const hit = rowIndex.current.get(p.id);
        if (hit) {
          const row = pages.current.get(hit.folder)?.get(hit.page)?.[hit.off];
          if (row) {
            row.status = SERVER_STATUS_TO_UI[p.status.toUpperCase()] ?? row.status;
            bump();
          }
        }
      }
      if (!throttleTimer.current) {
        throttleTimer.current = setTimeout(() => {
          throttleTimer.current = null;
          void loadSummary();
        }, 2500);
      }
    });
    return () => {
      off();
      if (throttleTimer.current) {
        clearTimeout(throttleTimer.current);
        throttleTimer.current = null;
      }
    };
  }, [active, loadSummary, clearPages, bump]);

  const ensureRange = useCallback((folder: string, fromIdx: number, toIdx: number) => {
    if (!active) return;
    const first = Math.max(0, Math.floor(fromIdx / PAGE));
    const last = Math.max(first, Math.floor(toIdx / PAGE));
    for (let pg = first; pg <= last; pg++) {
      const key = `${folder} ${pg}`;
      if (pages.current.get(folder)?.has(pg) || inflight.current.has(key)) continue;
      inflight.current.add(key);
      const qs = new URLSearchParams({
        date: todayOrderDate(),
        folder,
        limit: String(PAGE),
        offset: String(pg * PAGE),
      });
      if (serverStatus) qs.set("status", serverStatus);
      if (search) qs.set("q", search);
      api<{ items: PageInstance[] }>(`/api/instances/page?${qs.toString()}`)
        .then((resp) => {
          const rows = (resp.items ?? []).map((it) => pageToJob(it, folder, labelFor));
          if (!pages.current.has(folder)) pages.current.set(folder, new Map());
          pages.current.get(folder)!.set(pg, rows);
          rows.forEach((r, off) => rowIndex.current.set(r.id, { folder, page: pg, off }));
          bump();
        })
        .catch((err) => console.warn("[sidebar] page failed", folder, pg, err))
        .finally(() => inflight.current.delete(key));
    }
  }, [active, serverStatus, search, labelFor, bump]);

  const getRow = useCallback(
    (folder: string, idx: number): MonitoringJob | undefined =>
      pages.current.get(folder)?.get(Math.floor(idx / PAGE))?.[idx % PAGE],
    // version na dep: cache mudou → identidade nova → memos re-derivam.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [version],
  );

  return { summary, viewSummary, scoped, getRow, ensureRange, version };
}

/* ── Componente ── */

export default function MonitoringSidebarV2({
  jobs,
  selectedId,
  onSelect,
  onPauseFolder,
  onResumeFolder,
  onOpenViewPoint,
  visibleFolders,
}: {
  jobs: MonitoringJob[];
  selectedId?: string | null;
  onSelect?: (id: string) => void;
  /** D-2 — pause/resume de workflow: segura/libera os WAITING da folder em massa. */
  onPauseFolder?: (name: string) => void;
  onResumeFolder?: (name: string) => void;
  /** UI-1 — atalho pro ViewPoint quando o grafo está truncado no cap. */
  onOpenViewPoint?: () => void;
  /** Filtro de visibilidade do monitor (eye do FolderManager). null = todas.
      No modo local o V2Preview já filtra `jobs`; no windowed filtramos os
      grupos do summary aqui, pra manter a MESMA semântica. */
  visibleFolders?: Set<string> | null;
}) {
  const [filter, setFilter] = useState<StatusFilter>("ALL");
  const [query, setQuery] = useState("");
  const [internalSelected, setInternalSelected] = useState<string | null>(null);
  const selected = selectedId !== undefined ? selectedId : internalSelected;
  const handleSelect = (id: string) => {
    if (onSelect) onSelect(id);
    else setInternalSelected(id);
  };

  // Busca windowed vai pro server (q) — debounce pra não metralhar a API.
  const [debouncedQuery, setDebouncedQuery] = useState("");
  useEffect(() => {
    const t = setTimeout(() => setDebouncedQuery(query.trim()), 300);
    return () => clearTimeout(t);
  }, [query]);

  // Modo windowed: server mode E o dia REAL passa do cap do grafo. Decisão pelo
  // summary (fonte da verdade), não pelo tamanho do espelho local (que é capado).
  const cap = legacyCap();
  const server = isServerMode();
  const [probeTotal, setProbeTotal] = useState(0);
  const windowed = server && probeTotal > cap;
  const win = useDayWindow(windowed, filter, debouncedQuery);

  // Sonda o total do dia (é o MESMO summary do hook quando windowed liga; aqui
  // só decide o modo — e mantém o total real do header mesmo no modo local).
  useEffect(() => {
    if (!server) return;
    let dead = false;
    const probe = () =>
      api<DaySummary>(`/api/instances/summary?date=${encodeURIComponent(todayOrderDate())}`)
        .then((s) => { if (!dead) setProbeTotal(s.total); })
        .catch(() => {});
    void probe();
    const off = onServerEvent((ev) => {
      if (ev.event === "daily.started" || ev.event === "_connected") void probe();
      // Sem summary em cada instance.changed: o hook windowed já cobre; aqui só
      // o cruzamento do cap importa, e forçar jobs cruza via daily/força (raro).
      if (ev.event === "instance.changed" && !windowed) void probe();
    });
    return () => { dead = true; off(); };
  }, [server, windowed]);

  /* ── Dados locais (modo ≤ cap): filtro/busca client-side, como sempre ── */
  const counts = useMemo(() => {
    if (windowed && win.summary) {
      const bs = win.summary.byStatus;
      return {
        ALL: win.summary.total,
        RUNNING: bs.RUNNING ?? 0,
        FAILED: bs.NOTOK ?? 0,
        SUCCESS: bs.OK ?? 0,
        WAITING: bs.WAITING ?? 0,
      };
    }
    const c = { ALL: jobs.length, RUNNING: 0, FAILED: 0, SUCCESS: 0, WAITING: 0 };
    for (const j of jobs) {
      if (j.status === "RUNNING") c.RUNNING++;
      else if (j.status === "FAILED") c.FAILED++;
      else if (j.status === "SUCCESS") c.SUCCESS++;
      else if (j.status === "WAITING") c.WAITING++;
    }
    return c;
  }, [jobs, windowed, win.summary]);

  const filtered = useMemo(() => {
    if (windowed) return [];
    return jobs.filter((j) => {
      if (filter !== "ALL" && j.status !== filter) return false;
      if (query && !j.label.toLowerCase().includes(query.toLowerCase()) && !j.team.toLowerCase().includes(query.toLowerCase())) {
        return false;
      }
      return true;
    });
  }, [jobs, filter, query, windowed]);

  /* ── Grupos (folders) unificados: local = rows (jobs + sub-headers de
     carry-over); windowed = counts + getRow ──
     No modo LOCAL, dentro da folder os jobs do DIA CORRENTE vêm primeiro (sem
     sub-grupo) e os CARREGADOS pela virada (carry-over) ganham um sub-header
     por data de origem (ODAT) — pedido do usuário (2026-07-16): dias antigos
     agrupados, o dia de hoje limpo. `count` = nº de LINHAS (altura virtual);
     `jobs` = nº de JOBS (badge do header). */
  type LocalRow =
    | { kind: "job"; job: MonitoringJob }
    | { kind: "sub"; date: string; n: number };
  interface Group { name: string; count: number; jobs: number; rows?: LocalRow[] }
  const groups: Group[] = useMemo(() => {
    if (windowed) {
      const src = (win.scoped ? win.viewSummary : win.summary)?.byFolder ?? {};
      return Object.entries(src)
        .filter(([, n]) => n > 0)
        .map(([name, n]) => ({ name: (name || "—").trim() || "—", count: n, jobs: n }))
        .filter((g) => !visibleFolders || visibleFolders.has(g.name))
        .sort((a, b) => a.name.localeCompare(b.name));
    }
    const m = new Map<string, MonitoringJob[]>();
    for (const j of filtered) {
      const f = (j.team || "—").trim() || "—";
      if (!m.has(f)) m.set(f, []);
      m.get(f)!.push(j);
    }
    return [...m.entries()]
      .sort((a, b) => a[0].localeCompare(b[0]))
      .map(([name, arr]) => {
        const fresh = arr.filter((j) => !j.carriedFrom);
        const carried = arr.filter((j) => j.carriedFrom);
        const rows: LocalRow[] = fresh.map((job) => ({ kind: "job" as const, job }));
        if (carried.length) {
          const byDate = new Map<string, MonitoringJob[]>();
          for (const j of carried) {
            const d = j.carriedFrom!;
            if (!byDate.has(d)) byDate.set(d, []);
            byDate.get(d)!.push(j);
          }
          for (const [date, jobs] of [...byDate.entries()].sort((a, b) => a[0].localeCompare(b[0]))) {
            rows.push({ kind: "sub", date, n: jobs.length });
            for (const job of jobs) rows.push({ kind: "job", job });
          }
        }
        return { name, count: rows.length, jobs: arr.length, rows };
      });
  }, [windowed, win.scoped, win.viewSummary, win.summary, filtered, visibleFolders]);

  // Folders em PAUSA DE FOLDER (D-2/schemaV14): têm ≥1 job segurado pela folder.
  // Windowed → vem do summary (pausedFolders); local → deriva do holdScope das
  // linhas do grupo. Guia o cadeado da folder e o estado do botão pausar/retomar.
  const pausedFolders = useMemo(() => {
    const s = new Set<string>();
    if (windowed) {
      for (const f of win.summary?.pausedFolders ?? []) s.add((f || "—").trim() || "—");
    } else {
      for (const g of groups) {
        if (g.rows?.some((r) => r.kind === "job" && r.job.holdScope === "folder")) s.add(g.name);
      }
    }
    return s;
  }, [windowed, win.summary, groups]);

  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const toggleFolder = (name: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name); else next.add(name);
      return next;
    });
  };

  /* ── Virtualização ── */
  const scrollerRef = useRef<HTMLDivElement>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const [viewH, setViewH] = useState(600);
  useEffect(() => {
    const el = scrollerRef.current;
    if (!el) return;
    setViewH(el.clientHeight);
    const ro = new ResizeObserver(() => setViewH(el.clientHeight));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  // Offsets dos grupos no espaço VIRTUAL (px teóricos do dia inteiro).
  const { groupTops, virtualH } = useMemo(() => {
    let y = 0;
    const tops = groups.map((g) => {
      const t = y;
      y += HEADER_H + (collapsed.has(g.name) ? 0 : g.count * ROW_H);
      return t;
    });
    return { groupTops: tops, virtualH: y };
  }, [groups, collapsed]);

  // Compressão de escala quando o dia inteiro não cabe na altura máxima de
  // elemento do browser: a scrollbar representa o TOTAL, o mapeamento é linear.
  const displayH = Math.min(virtualH, MAX_DISPLAY_H);
  const scale = virtualH > displayH ? (virtualH - viewH) / Math.max(1, displayH - viewH) : 1;
  const vTop = scrollTop * scale;

  // Janela visível → páginas necessárias (windowed). Efeito, não render:
  // dispara fetches (side effect) e depende de scroll/tamanho/cache.
  useEffect(() => {
    if (!windowed) return;
    const winTop = vTop - OVERSCAN_PX;
    const winBot = vTop + viewH + OVERSCAN_PX;
    groups.forEach((g, gi) => {
      if (collapsed.has(g.name)) return;
      const jobsTop = groupTops[gi] + HEADER_H;
      const jobsBot = jobsTop + g.count * ROW_H;
      if (jobsBot < winTop || jobsTop > winBot) return;
      const first = Math.max(0, Math.floor((winTop - jobsTop) / ROW_H));
      const last = Math.min(g.count - 1, Math.ceil((winBot - jobsTop) / ROW_H));
      if (last >= first) win.ensureRange(g.name, first, last);
    });
  }, [windowed, vTop, viewH, groups, groupTops, collapsed, win.ensureRange, win.version]); // eslint-disable-line react-hooks/exhaustive-deps

  /* ── Rows visíveis ── */
  const visibleRows: ReactNode[] = [];
  {
    const winTop = vTop - OVERSCAN_PX;
    const winBot = vTop + viewH + OVERSCAN_PX;
    groups.forEach((g, gi) => {
      const gTop = groupTops[gi];
      const isCollapsed = collapsed.has(g.name);
      const paused = pausedFolders.has(g.name); // folder em PAUSA DE FOLDER (cadeado)
      const gH = HEADER_H + (isCollapsed ? 0 : g.count * ROW_H);
      if (gTop + gH < winTop || gTop > winBot) return;

      if (gTop >= winTop - HEADER_H) {
        visibleRows.push(
          <div
            key={`h-${g.name}`}
            onClick={() => toggleFolder(g.name)}
            style={{
              position: "absolute", top: gTop - vTop, left: 0, right: 0, height: HEADER_H,
              padding: "0 10px",
              display: "flex", alignItems: "center", gap: 6,
              background: "var(--v2-bg-elevated)",
              borderTop: "1px solid var(--v2-border-subtle)",
              borderBottom: "1px solid var(--v2-border-subtle)",
              // Folder pausada: faixa âmbar à esquerda pra sinalizar o hold da folder
              // inteira (mesma tinta do cadeado dos jobs segurados por ela).
              borderLeft: paused ? `2px solid ${HOLD_FOLDER_COLOR}` : "2px solid transparent",
              cursor: "pointer", userSelect: "none",
              boxSizing: "border-box",
            }}
            onMouseEnter={(e) => (e.currentTarget.style.background = "var(--v2-bg-hover)")}
            onMouseLeave={(e) => (e.currentTarget.style.background = "var(--v2-bg-elevated)")}
          >
            <span
              style={{
                width: 10, fontSize: 9, color: "var(--v2-text-muted)",
                fontFamily: "var(--v2-font-mono)",
                transition: "transform 80ms linear",
                transform: isCollapsed ? "rotate(-90deg)" : "none",
                display: "inline-block",
              }}
            >▾</span>
            {paused && (
              <span
                title={`Folder em HOLD — pausada (${g.name}). Os jobs segurados por ela só liberam pelo ▶ Retomar da folder inteira, não individualmente.`}
                style={{ display: "inline-flex", flexShrink: 0 }}
              >
                <Lock size={11} strokeWidth={2.5} color={HOLD_FOLDER_COLOR} />
              </span>
            )}
            <span
              style={{
                flex: 1, fontSize: 10, fontFamily: "var(--v2-font-mono)",
                letterSpacing: "0.08em",
                color: paused ? HOLD_FOLDER_COLOR : "var(--v2-text-primary)",
                textTransform: "uppercase", fontWeight: 600,
                whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis",
              }}
            >
              {g.name}
            </span>
            {onPauseFolder && onResumeFolder && (
              <span style={{ display: "inline-flex", gap: 2 }} onClick={(e) => e.stopPropagation()}>
                <button
                  title={paused
                    ? `Folder ${g.name} já está pausada — pausar de novo segura também o que entrou depois (jobs novos/carry-over)`
                    : `Pausar folder: segura TODOS os jobs de ${g.name} em hold — qualquer status (menos RUNNING), carry-over incluso; cada um congela o status atual e o Retomar restaura`}
                  onClick={() => onPauseFolder(g.name)}
                  style={paused
                    ? { ...folderActionBtn, color: HOLD_FOLDER_COLOR, borderColor: HOLD_FOLDER_COLOR, background: "rgba(245,158,11,0.12)" }
                    : folderActionBtn}
                >⏸</button>
                <button
                  title={paused
                    ? `Retomar folder: libera TODOS os jobs segurados pela pausa de ${g.name}, cada um de volta ao status que tinha`
                    : `Retomar folder: libera os jobs segurados pela pausa de ${g.name} (cada um volta ao status que tinha)`}
                  onClick={() => onResumeFolder(g.name)}
                  style={paused
                    ? { ...folderActionBtn, color: "var(--v2-status-ok)", borderColor: "var(--v2-status-ok)" }
                    : folderActionBtn}
                >▶</button>
              </span>
            )}
            <span
              style={{
                fontSize: 9, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)",
                padding: "1px 5px", border: "1px solid var(--v2-border-subtle)", borderRadius: 2,
              }}
            >
              {fmtInt(g.jobs)}
            </span>
          </div>,
        );
      }

      if (isCollapsed) return;
      const jobsTop = gTop + HEADER_H;
      const first = Math.max(0, Math.floor((winTop - jobsTop) / ROW_H));
      const last = Math.min(g.count - 1, Math.ceil((winBot - jobsTop) / ROW_H));
      for (let i = first; i <= last; i++) {
        const top = jobsTop + i * ROW_H - vTop;
        const localRow = g.rows?.[i];
        // Sub-header de carry-over (modo local): agrupa os jobs CARREGADOS por
        // data de origem (ODAT) dentro da folder — o dia corrente fica limpo.
        if (localRow?.kind === "sub") {
          visibleRows.push(
            <div
              key={`sub-${g.name}-${localRow.date}`}
              title={`Jobs carregados da diária de ${localRow.date} (carry-over) ainda ativos hoje`}
              style={{
                position: "absolute", top, left: 0, right: 0, height: ROW_H,
                padding: "0 12px 0 22px",
                display: "flex", alignItems: "center", gap: 6,
                borderBottom: "1px solid var(--v2-border-subtle)",
                background: "var(--v2-bg-elevated)",
                boxSizing: "border-box",
                fontSize: 9, fontFamily: "var(--v2-font-mono)",
                color: "var(--v2-text-muted)", letterSpacing: "0.06em",
                textTransform: "uppercase",
              }}
            >
              <span style={{ color: "var(--v2-accent-brand)" }}>↩</span>
              <span style={{ flex: 1 }}>{localRow.date} · carry-over</span>
              <span
                style={{
                  padding: "0 4px", border: "1px solid var(--v2-border-subtle)",
                  borderRadius: 2,
                }}
              >
                {fmtInt(localRow.n)}
              </span>
            </div>,
          );
          continue;
        }
        const j = localRow ? localRow.job : (g.rows ? undefined : win.getRow(g.name, i));
        if (!j) {
          visibleRows.push(
            <div
              key={`p-${g.name}-${i}`}
              style={{
                position: "absolute", top, left: 0, right: 0, height: ROW_H,
                padding: "0 12px 0 22px",
                display: "flex", alignItems: "center", gap: 8,
                borderBottom: "1px solid var(--v2-border-subtle)",
                boxSizing: "border-box",
                fontSize: 11, color: "var(--v2-text-muted)",
              }}
            >
              <span style={{ width: 6, height: 6, borderRadius: "50%", background: "var(--v2-border-medium)", flexShrink: 0 }} />
              <span style={{ opacity: 0.5, fontFamily: "var(--v2-font-mono)", letterSpacing: "0.2em" }}>···</span>
            </div>,
          );
          continue;
        }
        visibleRows.push(
          <div
            key={j.id}
            onClick={() => handleSelect(j.id)}
            style={{
              position: "absolute", top, left: 0, right: 0, height: ROW_H,
              padding: "0 12px 0 22px",
              display: "flex", alignItems: "center", gap: 8,
              borderBottom: "1px solid var(--v2-border-subtle)",
              background: selected === j.id ? "var(--v2-accent-deep)" : "transparent",
              borderLeft: selected === j.id ? "2px solid var(--v2-accent-brand)" : "2px solid transparent",
              boxShadow: selected === j.id ? "inset 0 0 12px var(--v2-accent-glow)" : "none",
              cursor: "pointer", fontSize: 11,
              transition: "background 80ms linear, box-shadow 120ms linear",
              boxSizing: "border-box",
            }}
            onMouseEnter={(e) => {
              if (selected !== j.id) e.currentTarget.style.background = "var(--v2-bg-hover)";
            }}
            onMouseLeave={(e) => {
              if (selected !== j.id) e.currentTarget.style.background = "transparent";
            }}
          >
            <span
              style={{
                width: 6, height: 6, borderRadius: "50%",
                background: STATUS_DOT[j.status], flexShrink: 0,
                animation: j.status === "RUNNING" ? "v2-dot-pulse 1.2s ease-in-out infinite" : "none",
              }}
            />
            {j.holdScope && (
              <span
                title={j.holdScope === "folder"
                  ? `Em HOLD pela pausa da folder ${j.team} — só libera pelo ▶ Retomar da folder inteira`
                  : "Em HOLD individual — botão direito no card → Release"}
                style={{ display: "inline-flex", flexShrink: 0, marginLeft: -2 }}
              >
                <Lock
                  size={11}
                  strokeWidth={2.5}
                  color={j.holdScope === "folder" ? HOLD_FOLDER_COLOR : HOLD_SELF_COLOR}
                />
              </span>
            )}
            <span
              style={{
                flex: 1, color: "var(--v2-text-primary)",
                whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis",
                fontWeight: selected === j.id ? 600 : 400,
              }}
            >
              {j.label}
            </span>
            {/* Windowed não tem sub-headers (páginas remotas): o chip ↩ diz a
                origem do carry-over na própria linha. No local o sub-header
                por data já comunica. */}
            {!g.rows && j.carriedFrom && (
              <span
                title={`Carregado da diária de ${j.carriedFrom} (carry-over)`}
                style={{
                  fontFamily: "var(--v2-font-mono)", fontSize: 8.5,
                  color: "var(--v2-accent-brand)",
                  border: "1px solid var(--v2-border-subtle)", borderRadius: 2,
                  padding: "0 3px", flexShrink: 0,
                }}
              >
                ↩ {j.carriedFrom.slice(5)}
              </span>
            )}
            <span
              style={{
                fontFamily: "var(--v2-font-mono)", fontSize: 10, color: "var(--v2-text-muted)",
                width: 48, textAlign: "right", flexShrink: 0,
              }}
            >
              {formatDuration(j.durationMs)}
            </span>
          </div>,
        );
      }
    });
  }

  const { width, onMouseDown, reset } = useResizablePanel({
    storageKey: "regente.panel.monitoring.w", defaultWidth: 320, min: 240, max: 640, edge: "right",
  });

  // Contador honesto do header: nunca um número truncado disfarçado de total.
  const viewTotal = windowed
    ? (win.scoped ? win.viewSummary?.total ?? 0 : win.summary?.total ?? 0)
    : filtered.length;
  const dayTotal = windowed ? win.summary?.total ?? 0 : jobs.length;
  const emptyList = windowed ? groups.length === 0 : filtered.length === 0;

  return (
    <aside
      data-canvas-inset="left"
      style={{
        position: "absolute",
        top: 10,
        left: 10,
        bottom: 10,
        width,
        background: "var(--v2-bg-surface)",
        border: "1px solid var(--v2-border-medium)",
        borderRadius: 16,
        boxShadow: "0 10px 30px rgba(0,0,0,0.35)",
        display: "flex",
        flexDirection: "column",
        fontFamily: "var(--v2-font-sans)",
        zIndex: 5,
        overflow: "hidden",
      }}
    >
      <ResizeHandle edge="right" onMouseDown={onMouseDown} onReset={reset} />
      {/* Header */}
      <div
        style={{
          padding: "10px 12px",
          borderBottom: "1px solid var(--v2-border-subtle)",
          display: "flex",
          alignItems: "center",
          gap: 8,
        }}
      >
        <span style={{ fontSize: 11, fontWeight: 600, letterSpacing: "0.04em", color: "var(--v2-text-primary)" }}>
          ACTIVE JOBS
        </span>
        <span
          title={windowed ? "visão atual / total real do dia (summary)" : "filtrados / carregados"}
          style={{
            marginLeft: "auto",
            fontSize: 10,
            fontFamily: "var(--v2-font-mono)",
            color: "var(--v2-text-muted)",
            padding: "1px 5px",
            border: "1px solid var(--v2-border-subtle)",
            borderRadius: 2,
            whiteSpace: "nowrap",
          }}
        >
          {windowed ? `${fmtInt(viewTotal)} de ${fmtInt(dayTotal)}` : `${viewTotal}/${dayTotal}`}
        </span>
      </div>

      {/* UI-1 — dia maior que o grafo desenha: a LISTA mostra tudo; avisa do cap. */}
      {windowed && (
        <div
          style={{
            padding: "5px 12px",
            borderBottom: "1px solid var(--v2-border-subtle)",
            display: "flex", alignItems: "center", gap: 6,
            fontSize: 9, fontFamily: "var(--v2-font-mono)",
            color: "var(--v2-text-muted)", letterSpacing: "0.03em",
          }}
        >
          <span style={{ flex: 1 }}>
            lista: dia inteiro · grafo: primeiros {fmtInt(cap)}
          </span>
          {onOpenViewPoint && (
            <button
              onClick={onOpenViewPoint}
              title="Abrir o ViewPoint (dashboard server-driven do dia inteiro)"
              style={{
                background: "transparent",
                border: "1px solid var(--v2-border-subtle)",
                color: "var(--v2-accent-brand)",
                borderRadius: 2, fontSize: 9, padding: "1px 6px",
                cursor: "pointer", fontFamily: "inherit", letterSpacing: "inherit",
              }}
            >ViewPoint</button>
          )}
        </div>
      )}

      {/* Search */}
      <div style={{ padding: "8px 12px", borderBottom: "1px solid var(--v2-border-subtle)" }}>
        <input
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={windowed ? "Filtrar por id/nome (no server)…" : "Filtrar por nome ou team…"}
          style={{
            width: "100%",
            background: "var(--v2-bg-canvas)",
            border: "1px solid var(--v2-border-subtle)",
            color: "var(--v2-text-primary)",
            padding: "5px 8px",
            fontSize: 11,
            fontFamily: "var(--v2-font-sans)",
            borderRadius: 3,
            outline: "none",
            boxSizing: "border-box",
          }}
          onFocus={(e) => (e.currentTarget.style.borderColor = "var(--v2-accent-dark)")}
          onBlur={(e) => (e.currentTarget.style.borderColor = "var(--v2-border-subtle)")}
        />
      </div>

      {/* Filter pills */}
      <div
        style={{
          padding: "8px 12px",
          display: "flex",
          gap: 4,
          borderBottom: "1px solid var(--v2-border-subtle)",
          fontSize: 10,
          fontFamily: "var(--v2-font-mono)",
          letterSpacing: "0.04em",
        }}
      >
        {(
          [
            { key: "ALL", label: "ALL", color: "var(--v2-text-secondary)" },
            { key: "RUNNING", label: "RUN", color: "var(--v2-status-running)" },
            { key: "FAILED", label: "FAIL", color: "var(--v2-status-failed)" },
            { key: "SUCCESS", label: "OK", color: "var(--v2-status-ok)" },
            { key: "WAITING", label: "WAIT", color: "var(--v2-status-waiting)" },
          ] as const
        ).map((p) => (
          <button
            key={p.key}
            onClick={() => setFilter(p.key as StatusFilter)}
            style={{
              flex: 1,
              background: filter === p.key ? "var(--v2-bg-hover)" : "transparent",
              border: `1px solid ${filter === p.key ? "var(--v2-border-strong)" : "var(--v2-border-subtle)"}`,
              color: filter === p.key ? "var(--v2-text-primary)" : p.color,
              padding: "4px 0",
              borderRadius: 3,
              cursor: "pointer",
              fontFamily: "inherit",
              fontSize: "inherit",
              letterSpacing: "inherit",
              fontWeight: filter === p.key ? 600 : 500,
              minWidth: 0,
              overflow: "hidden",
            }}
          >
            {p.label}
            <span style={{ marginLeft: 4, opacity: 0.6 }}>
              {fmtInt(counts[p.key as keyof typeof counts])}
            </span>
          </button>
        ))}
      </div>

      {/* Lista virtualizada — headers de folder + jobs, só o visível no DOM */}
      <div
        ref={scrollerRef}
        onScroll={(e) => setScrollTop(e.currentTarget.scrollTop)}
        style={{ flex: 1, overflowY: "auto", overflowX: "hidden", position: "relative" }}
      >
        {emptyList ? (
          <div style={{ padding: 20, fontSize: 11, color: "var(--v2-text-muted)", textAlign: "center" }}>
            nenhum job com esse filtro
          </div>
        ) : (
          <>
            {/* layer sticky: rows posicionadas relativas ao viewport (imune ao
                limite de altura/precisão — os tops são sempre pequenos) */}
            <div style={{ position: "sticky", top: 0, height: 0, overflow: "visible", zIndex: 1 }}>
              <div style={{ position: "relative", width: "100%" }}>{visibleRows}</div>
            </div>
            {/* spacer: dá a scrollbar do dia INTEIRO (comprimida se > teto) */}
            <div style={{ height: displayH }} />
          </>
        )}
      </div>

      {/* Footer summary */}
      <div
        style={{
          borderTop: "1px solid var(--v2-border-subtle)",
          padding: "6px 12px",
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          fontSize: 10,
          fontFamily: "var(--v2-font-mono)",
          color: "var(--v2-text-muted)",
          letterSpacing: "0.04em",
        }}
      >
        <span>{windowed ? "windowed · server-driven" : ""}</span>
        <span style={{ color: "var(--v2-accent-brand)" }}>● live</span>
      </div>
    </aside>
  );
}
