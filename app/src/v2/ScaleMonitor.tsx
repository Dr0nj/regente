/**
 * ScaleMonitor — Monitoring por ViewPoint server-driven (P3/escala).
 *
 * O canvas legado (ReactFlow) baixa o dia inteiro e renderiza todos os nós — não
 * passa de alguns milhares. Este painel é o modelo do Control-M para volume real
 * (100k–1M jobs/dia): NUNCA baixa o dia inteiro.
 *   - dashboard: /api/instances/summary  (contadores agregados, GROUP BY → ~50ms @100k)
 *   - folders:   summary.byFolder         (a "ViewPoint" — escolha o que olhar)
 *   - jobs:      /api/instances/page      (cursor, só o working set da folder aberta)
 *   - render:    lista VIRTUALIZADA       (só as linhas no viewport vão pro DOM)
 *
 * Mostra centenas na tela, guarda milhões no servidor.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, onServerEvent } from "@/lib/server-client";
import { todayOrderDate } from "@/lib/orchestrator-model";

interface Summary {
  date: string;
  total: number;
  byStatus: Record<string, number>;
  byFolder: Record<string, number>;
}
interface PageInstance {
  id: string;
  definitionId: string;
  team?: string;
  status: string;
  scheduledAt?: string;
  startedAt?: string;
  finishedAt?: string;
  carriedFrom?: string;
}
interface PageResp {
  items: PageInstance[];
  nextCursor: string;
}

const PAGE_LIMIT = 300;
const ROW_H = 30;
const STATUS_COLOR: Record<string, string> = {
  OK: "var(--v2-status-ok)",
  RUNNING: "var(--v2-status-running)",
  NOTOK: "var(--v2-status-failed)",
  WAITING: "var(--v2-status-waiting)",
  HELD: "var(--v2-text-muted)",
  HOLD: "var(--v2-text-muted)",
  CANCELLED: "var(--v2-text-muted)",
};

function fmtInt(n: number): string {
  return n.toLocaleString("pt-BR");
}

// Dashboards prontos — presets fixos da UI sobre o MESMO shape do /summary (que já
// honra ?status=). "Visão geral" = sem filtro; cada estado é um dashboard clicável
// que reescopa o total, a lista de folders (só as que têm jobs nesse estado) e os
// jobs da folder aberta. Zero backend novo: é o /summary com um filtro.
const PRESETS: { key: string; label: string; status: string }[] = [
  { key: "RUNNING", label: "Rodando", status: "RUNNING" },
  { key: "WAITING", label: "Aguardando", status: "WAITING" },
  { key: "OK", label: "Concluídos", status: "OK" },
  { key: "NOTOK", label: "Falhas", status: "NOTOK" },
  { key: "HELD", label: "Em espera", status: "HELD" },
];

export default function ScaleMonitor({ onClose }: { onClose?: () => void }) {
  const date = todayOrderDate();
  const [summary, setSummary] = useState<Summary | null>(null);
  // Dashboard pronto ativo: "" = visão geral; senão a chave de um PRESET.
  const [presetKey, setPresetKey] = useState<string>("");
  // Summary reescopado pelo preset (só quando há preset) — dá total + folders do estado.
  const [viewSummary, setViewSummary] = useState<Summary | null>(null);
  const [folderQuery, setFolderQuery] = useState("");
  const [folder, setFolder] = useState<string | null>(null);
  const [items, setItems] = useState<PageInstance[]>([]);
  const [cursor, setCursor] = useState<string>("");
  const [loading, setLoading] = useState(false);
  const [loadMs, setLoadMs] = useState<number | null>(null);

  const activeStatus = useMemo(
    () => (presetKey ? PRESETS.find((p) => p.key === presetKey)?.status ?? "" : ""),
    [presetKey],
  );

  const loadSummary = useCallback(async () => {
    try {
      // Global (sem filtro): alimenta SEMPRE os cards de estado (a visão do dia inteiro).
      const s = await api<Summary>(`/api/instances/summary?date=${encodeURIComponent(date)}`);
      setSummary(s);
      // Preset ativo: 2º /summary com ?status= → total + folders reescopados ao estado.
      if (activeStatus) {
        const v = await api<Summary>(
          `/api/instances/summary?date=${encodeURIComponent(date)}&status=${encodeURIComponent(activeStatus)}`,
        );
        setViewSummary(v);
      } else {
        setViewSummary(null);
      }
    } catch (e) {
      console.error("[scale] summary", e);
    }
  }, [date, activeStatus]);

  // Carrega a 1ª página de uma folder (reseta o working set).
  const openFolder = useCallback(
    async (f: string, status: string) => {
      setFolder(f);
      setLoading(true);
      const t0 = performance.now();
      try {
        const qs = new URLSearchParams({ date, folder: f, limit: String(PAGE_LIMIT) });
        if (status) qs.set("status", status);
        const p = await api<PageResp>(`/api/instances/page?${qs.toString()}`);
        setItems(p.items ?? []);
        setCursor(p.nextCursor ?? "");
        setLoadMs(Math.round(performance.now() - t0));
      } catch (e) {
        console.error("[scale] page", e);
        setItems([]);
        setCursor("");
      } finally {
        setLoading(false);
      }
    },
    [date],
  );

  // Próxima página (append) via cursor.
  const loadMore = useCallback(async () => {
    if (!folder || !cursor || loading) return;
    setLoading(true);
    try {
      const qs = new URLSearchParams({ date, folder, limit: String(PAGE_LIMIT), cursor });
      if (activeStatus) qs.set("status", activeStatus);
      const p = await api<PageResp>(`/api/instances/page?${qs.toString()}`);
      setItems((prev) => [...prev, ...(p.items ?? [])]);
      setCursor(p.nextCursor ?? "");
    } catch (e) {
      console.error("[scale] loadMore", e);
    } finally {
      setLoading(false);
    }
  }, [date, folder, cursor, activeStatus, loading]);

  useEffect(() => {
    // loadSummary não tem setState síncrono (tudo pós-await); escopo async aninhado
    // p/ a regra não pegar a chamada no corpo do efeito. Ver roadmap §RH.
    void (async () => { await loadSummary(); })();
    const off = onServerEvent((ev) => {
      if (ev.event === "daily.started" || ev.event === "instance.changed") void loadSummary();
    });
    return off;
  }, [loadSummary]);

  // Trocar de dashboard reescopa a folder já aberta (jobs batem com o preset).
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- reescope da folder aberta quando o preset muda; openFolder seta folder/loading de propósito (feedback da troca), não é reset de mount; ver roadmap §RH
    if (folder) void openFolder(folder, activeStatus);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeStatus]);

  // A "view" ativa é o summary do preset (reescopado) ou o global (visão geral).
  const activeView = presetKey ? viewSummary : summary;

  const folders = useMemo(() => {
    if (!activeView) return [];
    return Object.entries(activeView.byFolder)
      .map(([name, count]) => ({ name: name || "—", count }))
      .filter((f) => !folderQuery || f.name.toLowerCase().includes(folderQuery.toLowerCase()))
      .sort((a, b) => b.count - a.count);
  }, [activeView, folderQuery]);

  return (
    <div
      style={{
        position: "absolute",
        inset: 0,
        zIndex: 8,
        background: "var(--v2-bg-canvas)",
        display: "flex",
        flexDirection: "column",
        fontFamily: "var(--v2-font-sans)",
      }}
    >
      {/* Dashboard (summary agregado — independe do volume) */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 18,
          padding: "12px 16px",
          borderBottom: "1px solid var(--v2-border-medium)",
          background: "var(--v2-bg-surface)",
        }}
      >
        <div style={{ display: "flex", flexDirection: "column" }}>
          <span style={{ fontSize: 10, letterSpacing: "0.08em", color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)" }}>
            VIEWPOINT · {date}
            {presetKey && <span style={{ color: "var(--v2-accent-brand)" }}> · {PRESETS.find((p) => p.key === presetKey)?.label}</span>}
          </span>
          <span style={{ fontSize: 26, fontWeight: 700, color: "var(--v2-text-primary)", lineHeight: 1.1 }}>
            {activeView ? fmtInt(activeView.total) : "…"}
            <span style={{ fontSize: 12, fontWeight: 500, color: "var(--v2-text-muted)", marginLeft: 6 }}>
              {presetKey ? `de ${summary ? fmtInt(summary.total) : "…"} no dia` : "jobs no dia"}
            </span>
          </span>
        </div>
        {/* Dashboards prontos — cada card de estado é um preset clicável do /summary. */}
        <div style={{ display: "flex", gap: 8, marginLeft: 8, flexWrap: "wrap", alignItems: "center" }}>
          <PresetChip
            active={!presetKey}
            label="Visão geral"
            onClick={() => setPresetKey("")}
          />
          {PRESETS.map((p) => {
            const n = summary?.byStatus[p.status] ?? 0;
            const active = presetKey === p.key;
            return (
              <button
                key={p.key}
                onClick={() => setPresetKey(active ? "" : p.key)}
                title={`Dashboard: ${p.label}`}
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 6,
                  padding: "5px 10px",
                  borderRadius: 6,
                  border: active ? "1px solid var(--v2-accent-brand)" : "1px solid var(--v2-border-subtle)",
                  background: active ? "var(--v2-accent-deep)" : "var(--v2-bg-elevated)",
                  boxShadow: active ? "0 0 0 1px var(--v2-accent-brand) inset" : "none",
                  cursor: "pointer",
                  fontFamily: "inherit",
                }}
              >
                <span style={{ width: 8, height: 8, borderRadius: "50%", background: STATUS_COLOR[p.status] ?? "var(--v2-text-muted)" }} />
                <span style={{ fontSize: 10, fontFamily: "var(--v2-font-mono)", color: active ? "var(--v2-text-primary)" : "var(--v2-text-secondary)", letterSpacing: "0.04em" }}>{p.status}</span>
                <span style={{ fontSize: 13, fontWeight: 600, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-primary)" }}>{fmtInt(n)}</span>
              </button>
            );
          })}
        </div>
        <div style={{ marginLeft: "auto", display: "flex", gap: 8, alignItems: "center" }}>
          <span style={{ fontSize: 10, color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)" }}>
            {activeView ? `${fmtInt(folders.length)} folders` : ""}
          </span>
          {onClose && (
            <button
              onClick={onClose}
              style={{
                padding: "5px 10px",
                background: "transparent",
                border: "1px solid var(--v2-border-medium)",
                color: "var(--v2-text-primary)",
                borderRadius: 4,
                fontSize: 10,
                fontFamily: "var(--v2-font-mono)",
                letterSpacing: "0.06em",
                textTransform: "uppercase",
                cursor: "pointer",
                fontWeight: 600,
              }}
            >
              ✕ Canvas
            </button>
          )}
        </div>
      </div>

      {/* Corpo: folders à esquerda + jobs virtualizados à direita */}
      <div style={{ flex: 1, display: "flex", minHeight: 0 }}>
        {/* Folder list (ViewPoint picker) */}
        <div
          style={{
            width: 260,
            borderRight: "1px solid var(--v2-border-medium)",
            display: "flex",
            flexDirection: "column",
            background: "var(--v2-bg-surface)",
          }}
        >
          <div style={{ padding: "8px 10px", borderBottom: "1px solid var(--v2-border-subtle)" }}>
            <input
              value={folderQuery}
              onChange={(e) => setFolderQuery(e.target.value)}
              placeholder="Filtrar folders…"
              style={{
                width: "100%",
                background: "var(--v2-bg-canvas)",
                border: "1px solid var(--v2-border-subtle)",
                color: "var(--v2-text-primary)",
                padding: "5px 8px",
                fontSize: 11,
                borderRadius: 3,
                outline: "none",
              }}
            />
          </div>
          <div style={{ flex: 1, overflowY: "auto" }}>
            {folders.length === 0 && presetKey && (
              <div style={{ padding: "12px 14px", fontSize: 11, color: "var(--v2-text-muted)", lineHeight: 1.5 }}>
                Nenhuma folder com jobs em <strong style={{ color: "var(--v2-text-secondary)" }}>{PRESETS.find((p) => p.key === presetKey)?.label}</strong>.
              </div>
            )}
            {folders.map((f) => (
              <div
                key={f.name}
                onClick={() => void openFolder(f.name, activeStatus)}
                style={{
                  height: 30,
                  padding: "0 12px",
                  display: "flex",
                  alignItems: "center",
                  gap: 8,
                  cursor: "pointer",
                  borderBottom: "1px solid var(--v2-border-subtle)",
                  background: folder === f.name ? "var(--v2-accent-deep)" : "transparent",
                  borderLeft: folder === f.name ? "2px solid var(--v2-accent-brand)" : "2px solid transparent",
                }}
                onMouseEnter={(e) => { if (folder !== f.name) e.currentTarget.style.background = "var(--v2-bg-hover)"; }}
                onMouseLeave={(e) => { if (folder !== f.name) e.currentTarget.style.background = "transparent"; }}
              >
                <span style={{ flex: 1, fontSize: 11, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-primary)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                  {f.name}
                </span>
                <span style={{ fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)", padding: "1px 6px", border: "1px solid var(--v2-border-subtle)", borderRadius: 2 }}>
                  {fmtInt(f.count)}
                </span>
              </div>
            ))}
          </div>
        </div>

        {/* Jobs virtualizados da folder aberta */}
        <div style={{ flex: 1, display: "flex", flexDirection: "column", minWidth: 0 }}>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 10,
              padding: "8px 14px",
              borderBottom: "1px solid var(--v2-border-subtle)",
              fontSize: 11,
              color: "var(--v2-text-secondary)",
            }}
          >
            {folder ? (
              <>
                <strong style={{ color: "var(--v2-text-primary)", fontFamily: "var(--v2-font-mono)", letterSpacing: "0.04em" }}>{folder}</strong>
                <span style={{ color: "var(--v2-text-muted)" }}>
                  · {fmtInt(activeView?.byFolder[folder] ?? 0)} jobs{presetKey ? ` em ${PRESETS.find((p) => p.key === presetKey)?.label}` : ""}
                </span>
                <span style={{ color: "var(--v2-text-muted)" }}>· {fmtInt(items.length)} carregados</span>
                {loadMs !== null && <span style={{ color: "var(--v2-accent-brand)", fontFamily: "var(--v2-font-mono)" }}>· 1ª página em {loadMs}ms</span>}
              </>
            ) : (
              <span style={{ color: "var(--v2-text-muted)" }}>Escolha uma folder à esquerda para carregar os jobs (só o working set vem do servidor).</span>
            )}
          </div>
          <VirtualJobList items={items} hasMore={!!cursor} loading={loading} onReachEnd={loadMore} />
        </div>
      </div>
    </div>
  );
}

/* Chip de preset (ex.: "Visão geral") — o reset da barra de dashboards prontos. */
function PresetChip({ active, label, onClick }: { active: boolean; label: string; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      style={{
        padding: "5px 12px",
        borderRadius: 6,
        border: active ? "1px solid var(--v2-accent-brand)" : "1px solid var(--v2-border-subtle)",
        background: active ? "var(--v2-accent-deep)" : "var(--v2-bg-elevated)",
        boxShadow: active ? "0 0 0 1px var(--v2-accent-brand) inset" : "none",
        color: active ? "var(--v2-text-primary)" : "var(--v2-text-secondary)",
        fontSize: 11,
        fontWeight: 600,
        cursor: "pointer",
        fontFamily: "inherit",
        whiteSpace: "nowrap",
      }}
    >
      {label}
    </button>
  );
}

/* Lista virtualizada: só renderiza as linhas visíveis no viewport. */
function VirtualJobList({
  items,
  hasMore,
  loading,
  onReachEnd,
}: {
  items: PageInstance[];
  hasMore: boolean;
  loading: boolean;
  onReachEnd: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const [viewH, setViewH] = useState(600);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    setViewH(el.clientHeight);
  }, []);

  const total = items.length;
  const overscan = 8;
  const start = Math.max(0, Math.floor(scrollTop / ROW_H) - overscan);
  const visible = Math.ceil(viewH / ROW_H) + overscan * 2;
  const end = Math.min(total, start + visible);
  const slice = items.slice(start, end);

  const onScroll = (e: React.UIEvent<HTMLDivElement>) => {
    const el = e.currentTarget;
    setScrollTop(el.scrollTop);
    if (hasMore && !loading && el.scrollHeight - el.scrollTop - el.clientHeight < ROW_H * 12) {
      onReachEnd();
    }
  };

  return (
    <div ref={ref} onScroll={onScroll} style={{ flex: 1, overflowY: "auto", position: "relative" }}>
      <div style={{ height: total * ROW_H, position: "relative" }}>
        {slice.map((j, i) => {
          const idx = start + i;
          return (
            <div
              key={j.id}
              style={{
                position: "absolute",
                top: idx * ROW_H,
                left: 0,
                right: 0,
                height: ROW_H,
                display: "flex",
                alignItems: "center",
                gap: 10,
                padding: "0 14px",
                borderBottom: "1px solid var(--v2-border-subtle)",
                fontSize: 11,
                background: idx % 2 ? "transparent" : "var(--v2-bg-elevated)",
              }}
            >
              <span style={{ width: 7, height: 7, borderRadius: "50%", background: STATUS_COLOR[j.status?.toUpperCase()] ?? "var(--v2-text-muted)", flexShrink: 0 }} />
              <span style={{ width: 70, fontSize: 9, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)", letterSpacing: "0.04em", flexShrink: 0 }}>
                {j.status}
              </span>
              <span style={{ flex: 1, color: "var(--v2-text-primary)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontFamily: "var(--v2-font-mono)" }}>
                {j.definitionId}
                {j.carriedFrom && (
                  <span
                    title={`Carregado da diária de ${j.carriedFrom} (carry-over)`}
                    style={{ marginLeft: 7, fontSize: 9, color: "var(--v2-status-waiting)", fontFamily: "var(--v2-font-mono)" }}
                  >
                    ↩ {j.carriedFrom}
                  </span>
                )}
              </span>
              <span style={{ fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)", flexShrink: 0 }}>
                {j.scheduledAt ? new Date(j.scheduledAt).toLocaleTimeString("pt-BR", { hour: "2-digit", minute: "2-digit" }) : ""}
              </span>
            </div>
          );
        })}
      </div>
      {loading && (
        <div style={{ position: "sticky", bottom: 0, padding: "6px 14px", fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)", background: "var(--v2-bg-surface)", borderTop: "1px solid var(--v2-border-subtle)" }}>
          carregando…
        </div>
      )}
    </div>
  );
}
