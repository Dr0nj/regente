/**
 * V2Preview.tsx — Regente v2 root (F11 + F11.5 + F11.6).
 *
 * F11: Folders-as-cards lado-a-lado com scroll horizontal (substitui swimlanes).
 * F11.5: Context menu + rerun inline + output modal.
 * F11.6: Folder lifecycle (create/rename/delete/archive) + load seletivo.
 *
 * ReactFlow foi removido neste nível. Edges entre definitions ainda são
 * visíveis como "← upstream" nos cards. Integrações de DAG visual voltam
 * em fase posterior (subflow / side-panel dedicado).
 */
import { useCallback, useEffect, useMemo, useState } from "react";
import FolderCardsView, { type FolderCardsHandlers, type Mode } from "./FolderCardsView";
import MonitoringSidebarV2, { type MonitoringJob } from "./MonitoringSidebarV2";
import DesignSidebarV2 from "./DesignSidebarV2";
import InstanceDetailsDrawer from "./InstanceDetailsDrawer";
import JobConfigDrawer from "./JobConfigDrawer";
import OutputModal from "./OutputModal";
import FolderManagerDialog from "./FolderManagerDialog";
import type { JobNodeData } from "@/lib/job-config";
import type { JobInstance, JobDefinition } from "@/lib/orchestrator-model";
import { todayOrderDate } from "@/lib/orchestrator-model";
import {
  getTodayInstances,
  onInstanceChange,
  holdInstance,
  releaseInstance,
  cancelInstance,
  rerunInstance,
  skipInstance,
  bypassInstance,
  forceInstance,
} from "@/lib/runtime-bridge";
import {
  loadDefinitions,
  onDefinitionsChange,
  getDefinitions,
  saveDefinition,
  deleteDefinition,
  reloadDefinitions,
} from "@/lib/definition-store";
import {
  runDaily,
  startScheduler,
  stopScheduler,
  updateSchedulerDefs,
  getLastDailyRun,
} from "@/lib/runtime-bridge";
import { container } from "@/lib/container";
import { onServerEvent, isServerMode } from "@/lib/server-client";
import { listFolders, type FolderInfo } from "@/lib/folder-api";

import "@/index.css";
import "./tokens.css";

const INSTANCE_TO_UI_STATUS: Record<JobInstance["status"], JobNodeData["status"]> = {
  OK: "SUCCESS",
  NOTOK: "FAILED",
  RUNNING: "RUNNING",
  WAITING: "WAITING",
  HOLD: "INACTIVE",
  CANCELLED: "INACTIVE",
};

function instanceToMonitoring(inst: JobInstance): MonitoringJob {
  return {
    id: inst.id,
    label: inst.label,
    team: inst.team ?? "—",
    jobType: inst.jobType as JobNodeData["jobType"],
    status: INSTANCE_TO_UI_STATUS[inst.status],
    durationMs: inst.durationMs ?? (inst.startedAt ? Date.now() - inst.startedAt : undefined),
    startedAt: inst.startedAt ? fmtHm(inst.startedAt) : undefined,
  };
}

function fmtHm(ms: number): string {
  return new Date(ms).toLocaleTimeString("en-GB", { hour12: false }).slice(0, 5);
}

export default function V2Preview() {
  const [mode, setMode] = useState<Mode>("monitoring");
  const [instances, setInstances] = useState<JobInstance[]>([]);
  const [defs, setDefs] = useState<JobDefinition[]>([]);
  const [folders, setFolders] = useState<FolderInfo[]>([]);
  const [visibleFolders, setVisibleFolders] = useState<Set<string>>(new Set());
  const [selectedInstanceId, setSelectedInstanceId] = useState<string | null>(null);
  const [editingDef, setEditingDef] = useState<{ def: JobDefinition; isNew: boolean } | null>(null);
  const [outputFor, setOutputFor] = useState<JobInstance | null>(null);
  const [showFolderManager, setShowFolderManager] = useState(false);
  const [lastDaily, setLastDaily] = useState<string | null>(getLastDailyRun());

  const reloadFolders = useCallback(async () => {
    if (!isServerMode()) {
      const unique = new Set<string>();
      for (const d of getDefinitions()) if (d.team) unique.add(d.team);
      const list: FolderInfo[] = [...unique].map((name) => ({
        name,
        jobCount: getDefinitions().filter((d) => d.team === name).length,
      }));
      setFolders(list);
      return;
    }
    try {
      const list = await listFolders();
      setFolders(list);
    } catch (e) {
      console.error("[folders] list failed", e);
    }
  }, []);

  useEffect(() => {
    const serverMode = container.storageBackend === "server";

    if (!serverMode && typeof window !== "undefined") {
      const oldSeedFlag = window.localStorage.getItem("regente:v2-seeded:v1");
      if (oldSeedFlag) {
        window.localStorage.removeItem("regente:instances");
        window.localStorage.removeItem("regente:v2-seeded:v1");
        window.localStorage.removeItem("regente:daily-run-at");
      }
    }

    void loadDefinitions().then((list) => {
      setDefs(list);
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
          } catch {
            /* ignore */
          }
        }
      }
      setInstances(getTodayInstances());
      void reloadFolders();
    });
    const unsubDefs = onDefinitionsChange((list) => {
      setDefs([...list]);
      updateSchedulerDefs([...list]);
      void reloadFolders();
    });
    setInstances(getTodayInstances());
    const unsubInst = onInstanceChange(() => {
      setInstances(getTodayInstances().filter((i) => i.orderDate === todayOrderDate()));
    });
    startScheduler(2000);

    let unsubWs: (() => void) | null = null;
    if (isServerMode()) {
      unsubWs = onServerEvent((ev) => {
        if (ev.event === "definition.changed" || ev.event === "definition.deleted") {
          void reloadDefinitions().then((list) => setDefs([...list]));
          void reloadFolders();
        } else if (ev.event === "folder.changed") {
          void reloadFolders();
        }
      });
    }

    return () => {
      unsubDefs();
      unsubInst();
      if (unsubWs) unsubWs();
      stopScheduler();
    };
  }, [reloadFolders]);

  useEffect(() => {
    updateSchedulerDefs(defs);
  }, [defs]);

  const monitoringJobs = useMemo(() => instances.map(instanceToMonitoring), [instances]);
  const selectedInstance = selectedInstanceId
    ? instances.find((i) => i.id === selectedInstanceId) ?? null
    : null;

  const statusCounts = useMemo(() => {
    const c = { ok: 0, running: 0, failed: 0, waiting: 0, hold: 0 };
    for (const i of instances) {
      if (i.status === "OK") c.ok++;
      else if (i.status === "RUNNING") c.running++;
      else if (i.status === "NOTOK") c.failed++;
      else if (i.status === "WAITING") c.waiting++;
      else if (i.status === "HOLD") c.hold++;
    }
    return c;
  }, [instances]);

  const knownFolderNames = useMemo(
    () => folders.filter((f) => !f.archived).map((f) => f.name),
    [folders],
  );

  const handleSidebarSelect = useCallback((instId: string) => {
    setSelectedInstanceId(instId);
  }, []);

  const handleSaveDef = useCallback(async (def: JobDefinition) => {
    await saveDefinition(def);
    setEditingDef(null);
  }, []);

  const handleDeleteDef = useCallback(async (id: string) => {
    await deleteDefinition(id);
    for (const d of getDefinitions()) {
      if (d.upstream?.some((u) => u.from === id)) {
        await saveDefinition({ ...d, upstream: d.upstream.filter((u) => u.from !== id) });
      }
    }
    setEditingDef(null);
  }, []);

  const handleRunDaily = useCallback(() => {
    const created = runDaily(defs);
    setLastDaily(new Date().toISOString());
    if (container.storageBackend === "server") return;
    if (created.length > 0) setInstances(getTodayInstances());
    else alert("Nenhuma definition elegível (sem cron habilitado ou já materializadas hoje).");
  }, [defs]);

  const handleRerunInstance = useCallback((id: string) => {
    Promise.resolve(rerunInstance(id)).then((fresh) => {
      if (fresh) setSelectedInstanceId(fresh.id);
    });
  }, []);

  const handleForce = useCallback((def: JobDefinition) => {
    Promise.resolve(forceInstance(def))
      .then((fresh) => {
        if (fresh) setSelectedInstanceId(fresh.id);
      })
      .catch((err) => {
        console.error("[force] failed", err);
        alert(`Force falhou: ${err?.message ?? err}`);
      });
  }, []);

  const handleDuplicate = useCallback((def: JobDefinition) => {
    const suffix = Date.now().toString(36).slice(-4);
    const dup: JobDefinition = {
      ...def,
      id: `${def.id}-copy-${suffix}`,
      label: `${def.label} (copy)`,
      upstream: def.upstream ? [...def.upstream] : undefined,
    };
    setEditingDef({ def: dup, isNew: true });
  }, []);

  const handleAddJob = useCallback(() => {
    const id = `job-${Date.now().toString(36).slice(-5)}`;
    const draft: JobDefinition = {
      id,
      label: id,
      jobType: "COMMAND",
      team: folders[0]?.name ?? "default",
      schedule: { cronExpression: "0 3 * * *", enabled: true, description: "daily 03:00" },
      retries: 2,
      timeout: 300,
    };
    setEditingDef({ def: draft, isNew: true });
  }, [folders]);

  const cardHandlers: FolderCardsHandlers = useMemo(
    () => ({
      onInstanceClick: (inst) => setSelectedInstanceId(inst.id),
      onRerun: handleRerunInstance,
      onHold: (id) => Promise.resolve(holdInstance(id)),
      onRelease: (id) => Promise.resolve(releaseInstance(id)),
      onCancel: (id) => Promise.resolve(cancelInstance(id)),
      onSkip: (id) => Promise.resolve(skipInstance(id)),
      onBypass: (id) => Promise.resolve(bypassInstance(id)),
      onViewOutput: (inst) => setOutputFor(inst),
      onCopyId: (id) => void navigator.clipboard.writeText(id).catch(() => {}),
      onDefinitionClick: (def) => setEditingDef({ def, isNew: false }),
      onForce: handleForce,
      onDuplicate: handleDuplicate,
      onDelete: (def) => {
        if (confirm(`Deletar definition ${def.label}?`)) void handleDeleteDef(def.id);
      },
    }),
    [handleRerunInstance, handleForce, handleDuplicate, handleDeleteDef],
  );

  const hasDefs = defs.length > 0;
  const hasInstances = instances.length > 0;

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
      <header
        style={{
          height: 44,
          padding: "0 16px",
          borderBottom: "1px solid var(--v2-border-subtle)",
          background: "var(--v2-bg-surface)",
          display: "flex",
          alignItems: "center",
          gap: 16,
          flexShrink: 0,
        }}
      >
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <div
            style={{
              width: 18,
              height: 18,
              background: "var(--v2-accent-brand)",
              borderRadius: 3,
              display: "inline-flex",
              alignItems: "center",
              justifyContent: "center",
              color: "#000",
              fontWeight: 700,
              fontSize: 11,
            }}
          >
            R
          </div>
          <span style={{ fontSize: 13, fontWeight: 600, letterSpacing: "0.02em" }}>Regente</span>
          <span
            style={{
              fontSize: 9,
              fontFamily: "var(--v2-font-mono)",
              color: "var(--v2-text-muted)",
              letterSpacing: "0.06em",
              marginLeft: 4,
            }}
          >
            {container.storageBackend}
          </span>
        </div>

        <div
          style={{
            display: "flex",
            background: "var(--v2-bg-elevated)",
            border: "1px solid var(--v2-border-medium)",
            borderRadius: 4,
            overflow: "hidden",
          }}
        >
          {(["design", "monitoring"] as const).map((m) => (
            <button
              key={m}
              onClick={() => {
                setMode(m);
                setSelectedInstanceId(null);
                setEditingDef(null);
              }}
              style={{
                padding: "5px 14px",
                background: mode === m ? "var(--v2-accent-deep)" : "transparent",
                border: "none",
                borderRight: m === "design" ? "1px solid var(--v2-border-medium)" : "none",
                color: mode === m ? "var(--v2-accent-brand)" : "var(--v2-text-secondary)",
                fontSize: 11,
                fontFamily: "var(--v2-font-mono)",
                letterSpacing: "0.06em",
                cursor: "pointer",
                fontWeight: mode === m ? 600 : 500,
                textTransform: "uppercase",
              }}
            >
              {m}
            </button>
          ))}
        </div>

        <button onClick={() => setShowFolderManager(true)} style={topBtn()} title="Manage folders">
          Folders
        </button>

        {mode === "design" && (
          <button onClick={handleAddJob} style={topBtn(true)} title="Add new job">
            + Job
          </button>
        )}

        {mode === "monitoring" && (
          <button
            onClick={handleRunDaily}
            disabled={!hasDefs}
            title={
              hasDefs
                ? "Materializa instances de hoje a partir das definitions"
                : "Crie definitions no Design primeiro"
            }
            style={topBtn(true, !hasDefs)}
          >
            ▶ Run Daily
          </button>
        )}

        <div style={{ flex: 1 }} />

        <div
          style={{
            display: "flex",
            gap: 14,
            fontSize: 10,
            fontFamily: "var(--v2-font-mono)",
            color: "var(--v2-text-secondary)",
            letterSpacing: "0.04em",
          }}
        >
          <span>
            <span style={{ color: "var(--v2-status-ok)" }}>●</span> {statusCounts.ok}
          </span>
          <span>
            <span style={{ color: "var(--v2-status-running)" }}>●</span> {statusCounts.running}
          </span>
          <span>
            <span style={{ color: "var(--v2-status-failed)" }}>●</span> {statusCounts.failed}
          </span>
          <span>
            <span style={{ color: "var(--v2-status-waiting)" }}>●</span> {statusCounts.waiting}
          </span>
          {statusCounts.hold > 0 && (
            <span>
              <span style={{ color: "var(--v2-status-hold)" }}>●</span> {statusCounts.hold}
            </span>
          )}
        </div>
      </header>

      <main style={{ flex: 1, position: "relative", minHeight: 0, display: "flex" }}>
        <FolderCardsView
          mode={mode}
          instances={instances}
          definitions={defs}
          knownFolders={knownFolderNames}
          visibleFolders={visibleFolders}
          handlers={cardHandlers}
          selectedInstanceId={selectedInstanceId}
        />

        {mode === "monitoring" && !hasInstances && (
          <EmptyState
            title={hasDefs ? "Nenhuma instance hoje" : "Ambiente vazio"}
            hint={
              hasDefs
                ? "Clique em Run Daily na topbar para materializar os jobs do dia."
                : "Vá para Design mode e crie jobs com + Job."
            }
          />
        )}
        {mode === "design" && !hasDefs && (
          <EmptyState title="Nenhuma definition" hint="Clique em + Job na topbar para criar o primeiro." />
        )}

        {mode === "monitoring" ? (
          <MonitoringSidebarV2
            jobs={monitoringJobs}
            selectedId={selectedInstanceId}
            onSelect={handleSidebarSelect}
          />
        ) : (
          <DesignSidebarV2 definitions={defs} />
        )}

        {mode === "monitoring" && selectedInstance && (
          <InstanceDetailsDrawer
            instance={selectedInstance}
            handlers={{
              onHold: holdInstance,
              onRelease: releaseInstance,
              onCancel: cancelInstance,
              onSkip: skipInstance,
              onBypass: bypassInstance,
              onRerun: handleRerunInstance,
              onClose: () => setSelectedInstanceId(null),
            }}
          />
        )}

        {mode === "design" && editingDef && (
          <JobConfigDrawer
            definition={editingDef.def}
            isNew={editingDef.isNew}
            handlers={{
              onSave: handleSaveDef,
              onDelete: handleDeleteDef,
              onClose: () => setEditingDef(null),
            }}
          />
        )}

        {outputFor && <OutputModal instance={outputFor} onClose={() => setOutputFor(null)} />}

        {showFolderManager && (
          <FolderManagerDialog
            folders={folders}
            visibleFolders={visibleFolders}
            onVisibleChange={setVisibleFolders}
            onFoldersChanged={() => {
              void reloadFolders();
              void reloadDefinitions().then((list) => setDefs([...list]));
            }}
            onClose={() => setShowFolderManager(false)}
          />
        )}
      </main>

      <footer
        style={{
          height: 24,
          padding: "0 16px",
          borderTop: "1px solid var(--v2-border-subtle)",
          background: "var(--v2-bg-surface)",
          display: "flex",
          alignItems: "center",
          gap: 20,
          fontSize: 10,
          fontFamily: "var(--v2-font-mono)",
          color: "var(--v2-text-muted)",
          letterSpacing: "0.04em",
          flexShrink: 0,
        }}
      >
        <span>
          {defs.length} definitions · {instances.length} instances · {folders.length} folders ·{" "}
          {todayOrderDate()}
        </span>
        {lastDaily && (
          <span>daily: {new Date(lastDaily).toLocaleTimeString("en-GB", { hour12: false })}</span>
        )}
        <span style={{ marginLeft: "auto" }}>{mode}</span>
      </footer>
    </div>
  );
}

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
        <div
          style={{
            fontSize: 13,
            fontWeight: 600,
            color: "var(--v2-text-primary)",
            marginBottom: 6,
          }}
        >
          {title}
        </div>
        <div style={{ fontSize: 11, color: "var(--v2-text-secondary)", lineHeight: 1.5 }}>{hint}</div>
      </div>
    </div>
  );
}

function topBtn(primary = false, disabled = false): React.CSSProperties {
  return {
    padding: "5px 10px",
    background: "transparent",
    border: `1px solid ${
      disabled ? "var(--v2-border-medium)" : primary ? "var(--v2-accent-brand)" : "var(--v2-border-medium)"
    }`,
    color: disabled
      ? "var(--v2-text-muted)"
      : primary
      ? "var(--v2-accent-brand)"
      : "var(--v2-text-primary)",
    borderRadius: 3,
    fontSize: 10,
    fontFamily: "var(--v2-font-mono)",
    letterSpacing: "0.06em",
    textTransform: "uppercase",
    cursor: disabled ? "not-allowed" : "pointer",
    fontWeight: 600,
  };
}
