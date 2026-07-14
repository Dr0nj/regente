import { useEffect, useMemo, useState } from "react";
import { ExternalLink, X, Trash2, ArrowRight, ArrowLeft } from "lucide-react";
import type { JobDefinition, CalendarRef, EdgeCondition, ActionRule } from "@/lib/orchestrator-model";
import type { JobType } from "@/lib/job-config";
import JobActionConfigEditor from "./JobActionConfigEditor";
import OnDoEditor from "./OnDoEditor";
import ScheduleEditor from "./ScheduleEditor";
import { DefinitionAuditPanel } from "./DefinitionAuditPanel";
import { ErrorDialog } from "./ErrorDialog";
import { getGitInfo, definitionFileUrl } from "@/lib/git-info";
import { listCalendars, type Calendar } from "@/lib/bloco2-api";
import { listAgents, type AgentInfo } from "@/lib/agents-api";
import { jobTypeFieldIndex, pruneConfigForType } from "@/lib/jobtypes-api";
import { saveTemplate } from "@/lib/differentials-api";
import { isServerMode } from "@/lib/server-client";
import { toast } from "./Toast";
import { useResizablePanel, ResizeHandle } from "./resizable";

/* ──────────────────────────────────────────────────────────────
   JobConfigDrawer — painel direito (Design). ABAS:
   General · Schedule · Action · On/Do · Dependencies.
   (Calendários foram fundidos na aba Schedule — trabalham junto com
   as regras como include/exclude; ver ScheduleEditor.)
   "Action" = config de EXECUÇÃO do job (command/url/script).
   "On/Do"  = regras REATIVAS ao ciclo (Control-M On-Do); ver OnDoEditor.
   ────────────────────────────────────────────────────────────── */

export interface JobConfigHandlers {
  onSave: (def: JobDefinition) => void | Promise<void>;
  onDelete: (id: string) => void | Promise<void>;
  onClose: () => void;
}

const JOB_TYPES: JobType[] = ["COMMAND", "SCRIPT", "SSH", "HTTP", "FILE_WATCH", "FILE_TRANSFER", "DATABASE", "LAMBDA", "BATCH", "GLUE", "STEP_FUNCTION", "CHOICE", "PARALLEL", "WAIT"];
type Tab = "general" | "schedule" | "action" | "ondo" | "deps";
const TABS: Array<{ id: Tab; label: string }> = [
  { id: "general", label: "Geral" },
  { id: "schedule", label: "Schedule" },
  { id: "action", label: "Action" },
  { id: "ondo", label: "On/Do" },
  { id: "deps", label: "Dependências" },
];

interface Props {
  definition: JobDefinition;
  isNew: boolean;
  availableFolders: string[];
  /** Todas as defs do escopo — para a aba Dependencies (depende de / dispara). */
  allDefs?: JobDefinition[];
  handlers: JobConfigHandlers;
}

export default function JobConfigDrawer({ definition, isNew, availableFolders, allDefs = [], handlers }: Props) {
  const [tab, setTab] = useState<Tab>("general");
  const [label, setLabel] = useState(definition.label);
  const [id, setId] = useState(definition.id);
  const [jobType, setJobType] = useState<JobType>(definition.jobType as JobType);
  const initialTeam = definition.team ?? (availableFolders.length === 1 ? availableFolders[0] : "");
  const [team, setTeam] = useState(initialTeam);
  const [schedule, setSchedule] = useState(definition.schedule);
  const [retries, setRetries] = useState(definition.retries ?? 2);
  const [timeout, setTimeoutS] = useState(definition.timeout ?? 300);
  const [dryRun, setDryRun] = useState(definition.dryRun ?? false);
  const [confirmReq, setConfirmReq] = useState(definition.confirm ?? false);
  const [actionConfig, setActionConfig] = useState<Record<string, unknown>>(definition.actionConfig ?? {});
  const [calendars, setCalendars] = useState<CalendarRef[]>(definition.calendars ?? []);
  const [actions, setActions] = useState<ActionRule[]>(definition.actions ?? []);
  const [upstream, setUpstream] = useState(definition.upstream ?? []);
  // F16 — conditions nomeadas (entrada = gate WAIT_CONDITION; saída = set/unset
  // no término OK). Editáveis aqui pra fechar o circuito com o set-condition
  // do On/Do: o nome que um job seta é o mesmo que outro declara na entrada.
  const [conditionsIn, setConditionsIn] = useState<string[]>(definition.conditionsIn ?? []);
  const [conditionsOutAdd, setConditionsOutAdd] = useState<string[]>(definition.conditionsOutAdd ?? []);
  const [conditionsOutRemove, setConditionsOutRemove] = useState<string[]>(definition.conditionsOutRemove ?? []);
  const [agentId, setAgentId] = useState<string>(
    typeof definition.actionConfig?._agentId === "string" ? (definition.actionConfig._agentId as string) : ""
  );
  // agentTouched — o usuário mexeu no seletor de agente? Enquanto não mexer,
  // um job NOVO nasce PINADO por padrão no primeiro agente online com a
  // capability do jobType (SERVER-AGENT cobre HTTP/REST). É a regra "o job
  // criado num agente fica marcado naquele agente": se ele cair amanhã, o job
  // espera em WAIT AGENT — nunca migra sozinho pra outro agente.
  const [agentTouched, setAgentTouched] = useState(false);
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [saving, setSaving] = useState(false);
  const [showAudit, setShowAudit] = useState(false);
  const [err, setErr] = useState<unknown>(null);
  const [validationErr, setValidationErr] = useState<string | null>(null);
  const [githubUrl, setGithubUrl] = useState<string | null>(null);
  const [calendarDefs, setCalendarDefs] = useState<Calendar[]>([]);
  // Schema por tipo (ADV-1) — usado pra podar params órfãos quando o tipo muda.
  const [typeFieldIdx, setTypeFieldIdx] = useState<Map<string, Set<string>> | null>(null);

  useEffect(() => {
    setTab("general");
    setLabel(definition.label); setId(definition.id); setJobType(definition.jobType as JobType);
    setTeam(definition.team ?? (availableFolders.length === 1 ? availableFolders[0] : ""));
    setSchedule(definition.schedule);
    setRetries(definition.retries ?? 2); setTimeoutS(definition.timeout ?? 300);
    setDryRun(definition.dryRun ?? false); setConfirmReq(definition.confirm ?? false); setActionConfig(definition.actionConfig ?? {});
    setCalendars(definition.calendars ?? []); setActions(definition.actions ?? []); setUpstream(definition.upstream ?? []);
    setConditionsIn(definition.conditionsIn ?? []); setConditionsOutAdd(definition.conditionsOutAdd ?? []); setConditionsOutRemove(definition.conditionsOutRemove ?? []);
    setAgentId(typeof definition.actionConfig?._agentId === "string" ? (definition.actionConfig._agentId as string) : "");
    setAgentTouched(false);
    setErr(null); setValidationErr(null);
  }, [definition.id]); // eslint-disable-line react-hooks/exhaustive-deps

  // Pin default na CRIAÇÃO: job novo nasce marcado no primeiro agente online
  // capaz do jobType (re-decide se o jobType mudar antes do usuário tocar no
  // seletor). Job existente e escolha manual nunca são sobrescritos.
  useEffect(() => {
    if (!isNew || agentTouched || agents.length === 0) return;
    const cap = String(jobType || "").toUpperCase();
    if (cap === "SSH") { setAgentId(""); return; } // agentless — roda no server
    const current = agents.find((a) => a.id === agentId);
    const currentServes = current?.capabilities?.some((c) => c.toUpperCase() === cap);
    if (agentId && currentServes) return;
    const match = agents.find((a) => a.online && a.capabilities?.some((c) => c.toUpperCase() === cap));
    if (match) setAgentId(match.id);
  }, [isNew, agentTouched, agents, jobType]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (isNew || !definition.team || !definition.id) { setGithubUrl(null); return; }
    let cancel = false;
    void getGitInfo().then((st) => { if (!cancel) setGithubUrl(definitionFileUrl(st, definition.team!, definition.id)); });
    return () => { cancel = true; };
  }, [isNew, definition.team, definition.id]);

  useEffect(() => {
    let cancel = false;
    void listCalendars().then((cs) => { if (!cancel) setCalendarDefs(cs); }).catch(() => {});
    void listAgents().then((a) => { if (!cancel) setAgents(a); }).catch(() => {});
    void jobTypeFieldIndex().then((m) => { if (!cancel) setTypeFieldIdx(m); }).catch(() => {});
    return () => { cancel = true; };
  }, []);

  // buildDef — a definition como está no formulário (usada pelo Save e pelo
  // Salvar-como-template, D-13).
  function buildDef(): JobDefinition {
    // Poda params que o jobType escolhido NÃO aceita: trocar o tipo deixava as
    // chaves do tipo antigo no actionConfig (scriptPath num COMMAND) e o server
    // rejeitava até o DRAFT ("campo desconhecido") — o job ficava preso no tipo
    // antigo. Chaves compartilhadas (cwd, command, …) e meta `_*` sobrevivem;
    // tipo desconhecido pelo catálogo segue com params livres.
    const prunedConfig = pruneConfigForType(typeFieldIdx, jobType, actionConfig);
    return {
      ...definition,
      id: id.trim(), label: label.trim(), jobType, team: team.trim(),
      schedule: { ...schedule, enabled: schedule.enabled ?? true },
      retries, timeout, dryRun, confirm: confirmReq || undefined,
      // _agentId é o canal que o ServerApiAdapter usa p/ mapear actionConfig→agentId.
      actionConfig: { ...prunedConfig, _agentId: agentId.trim() || undefined },
      calendars: calendars.length ? calendars : undefined,
      actions: actions.length ? actions : undefined,
      upstream: upstream.length ? upstream : undefined,
      conditionsIn: conditionsIn.length ? conditionsIn : undefined,
      conditionsOutAdd: conditionsOutAdd.length ? conditionsOutAdd : undefined,
      conditionsOutRemove: conditionsOutRemove.length ? conditionsOutRemove : undefined,
    };
  }

  async function handleSave() {
    if (!label.trim()) { setValidationErr("label obrigatório"); setTab("general"); return; }
    if (!team.trim()) { setValidationErr("folder obrigatória"); setTab("general"); return; }
    if (!id.trim()) { setValidationErr("id obrigatório"); setTab("general"); return; }
    setValidationErr(null);
    const next = buildDef();
    setSaving(true); setErr(null);
    try { await handlers.onSave(next); } catch (e) { setErr(e); } finally { setSaving(false); }
  }

  // D-13 — salva a FORMA deste job como template reutilizável (o server
  // descarta id/team/upstream: identidade e vínculos não viajam no molde).
  async function handleSaveTemplate() {
    const name = window.prompt("Nome do template:", id.trim() || label.trim());
    if (!name?.trim()) return;
    try {
      await saveTemplate(name.trim(), label.trim(), buildDef());
      toast.success(`Template "${name.trim()}" salvo`, { detail: "disponível na aba Templates da palette" });
    } catch (e) {
      toast.error("Falha ao salvar template", { detail: e instanceof Error ? e.message : String(e) });
    }
  }

  async function handleDelete() {
    if (isNew) { handlers.onClose(); return; }
    if (!confirm(`Delete definition "${definition.label}"? Vai remover o YAML do repo.`)) return;
    setSaving(true);
    try { await handlers.onDelete(definition.id); } catch (e) { setErr(e); } finally { setSaving(false); }
  }

  // Jobs que DEPENDEM deste (this.id ∈ outro.upstream.from) → "dispara".
  const triggers = allDefs.filter((d) => (d.upstream ?? []).some((u) => u.from === definition.id));

  // Vocabulário de conditions do escopo: todo nome já usado em qualquer def
  // (entrada, saída ou ação set-condition) vira sugestão no editor — o vínculo
  // entre produtor e consumidor é cravado pelo NOME, então digitar errado quebra.
  const knownConditions = useMemo(() => {
    const s = new Set<string>();
    for (const d of allDefs) {
      d.conditionsIn?.forEach((c) => s.add(c));
      d.conditionsOutAdd?.forEach((c) => s.add(c));
      d.conditionsOutRemove?.forEach((c) => s.add(c));
      d.actions?.forEach((a) => { if (a.do === "set-condition" && a.condition) s.add(a.condition); });
    }
    return [...s].sort();
  }, [allDefs]);
  const condCount = conditionsIn.length + conditionsOutAdd.length + conditionsOutRemove.length;

  const { width, onMouseDown, reset } = useResizablePanel({
    storageKey: "regente.panel.jobConfig.w", defaultWidth: 360, min: 280, max: 720, edge: "left",
  });

  return (
    <aside style={{
      position: "absolute", top: 10, right: 10, bottom: 10, width,
      background: "var(--v2-bg-surface)",
      border: "1px solid var(--v2-border-medium)", borderRadius: 16,
      boxShadow: "0 10px 30px rgba(0,0,0,0.35)",
      display: "flex", flexDirection: "column", fontFamily: "var(--v2-font-sans)", zIndex: 5, overflow: "hidden",
    }}>
      <ResizeHandle edge="left" onMouseDown={onMouseDown} onReset={reset} />
      {/* Header */}
      <div style={{ padding: "10px 12px", borderBottom: "1px solid var(--v2-border-subtle)", display: "flex", alignItems: "center", gap: 8 }}>
        <span style={{ width: 6, height: 6, borderRadius: "50%", background: "var(--v2-accent-brand)" }} />
        <span style={{ fontSize: 11, fontWeight: 600, letterSpacing: "0.04em" }}>{isNew ? "NEW JOB" : "EDIT JOB"}</span>
        {githubUrl && (
          <a href={githubUrl} target="_blank" rel="noreferrer" title="Ver o YAML no GitHub"
            style={{ display: "inline-flex", alignItems: "center", gap: 4, fontSize: 10, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-secondary)", textDecoration: "none", padding: "2px 7px", border: "1px solid var(--v2-border-medium)", borderRadius: 3, marginLeft: 6 }}>
            <ExternalLink size={10} /> GitHub
          </a>
        )}
        <button onClick={handlers.onClose} title="Close"
          style={{ marginLeft: "auto", background: "transparent", border: "none", color: "var(--v2-text-secondary)", cursor: "pointer", padding: 0, width: 18, height: 18, display: "inline-flex", alignItems: "center", justifyContent: "center" }}>
          <X size={14} />
        </button>
      </div>

      {/* Tabs */}
      <div style={{ display: "flex", borderBottom: "1px solid var(--v2-border-subtle)", overflowX: "auto" }}>
        {TABS.map((t) => (
          <button key={t.id} onClick={() => setTab(t.id)} style={{
            padding: "7px 11px", fontSize: 10.5, cursor: "pointer", whiteSpace: "nowrap",
            background: "transparent", border: "none",
            borderBottom: `2px solid ${tab === t.id ? "var(--v2-accent-brand)" : "transparent"}`,
            color: tab === t.id ? "var(--v2-text-primary)" : "var(--v2-text-muted)",
            fontWeight: tab === t.id ? 600 : 500, fontFamily: "var(--v2-font-mono)",
          }}>{t.label}
            {t.id === "deps" && (upstream.length + triggers.length + condCount > 0) ? ` (${upstream.length + triggers.length + condCount})` : ""}
            {t.id === "ondo" && actions.length > 0 ? ` (${actions.length})` : ""}
          </button>
        ))}
      </div>

      <div style={{ flex: 1, overflowY: "auto", padding: "12px", display: "flex", flexDirection: "column", gap: 12 }}>
        {tab === "general" && (
          <>
            <Field label="Job Name"><Input value={label} onChange={setLabel} /></Field>
            <Field label="Job Type">
              <select value={jobType} onChange={(e) => setJobType(e.target.value as JobType)} style={selectStyle}>
                {JOB_TYPES.map((t) => <option key={t} value={t}>{t}</option>)}
              </select>
            </Field>
            <Field label="Folder">
              <select value={team} onChange={(e) => setTeam(e.target.value)} style={selectStyle}>
                <option value="">— select folder —</option>
                {availableFolders.map((t) => <option key={t} value={t}>{t}</option>)}
              </select>
            </Field>
            <Field label="Agente (onde roda)">
              <select value={agentId} onChange={(e) => { setAgentTouched(true); setAgentId(e.target.value); }} style={selectStyle}>
                <option value="">Automático (por capability)</option>
                {agents.map((a) => (
                  // BUG-8 — o SERVER-AGENT embutido não ganha sufixo de restrição
                  // depois do nome; agentes externos seguem mostrando as caps.
                  <option key={a.id} value={a.id}>
                    {a.id === "SERVER-AGENT" ? a.id : `${a.id} — ${a.capabilities.join("/") || "sem caps"}`}
                  </option>
                ))}
                {agentId && !agents.some((a) => a.id === agentId) && (
                  <option value={agentId}>{agentId} (offline)</option>
                )}
              </select>
              <div style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 4, lineHeight: 1.4 }}>
                {agents.length === 0
                  ? "Nenhum agente online. Rode o regente-agent na máquina alvo (jobs HTTP rodam no SERVER-AGENT embutido)."
                  : "Pinado = roda SÓ nesse agente (se ele cair, o job espera em WAIT AGENT — nunca migra). Vazio = o server escolhe qualquer agente com a capability."}
              </div>
            </Field>
            <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 11, color: "var(--v2-text-secondary)" }}>
              <input type="checkbox" checked={schedule.enabled} onChange={(e) => setSchedule({ ...schedule, enabled: e.target.checked })} />
              Schedule habilitado
            </label>
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
              <Field label="Retries"><Input value={String(retries)} onChange={(v) => setRetries(Number(v) || 0)} mono /></Field>
              <Field label="Timeout (s)"><Input value={String(timeout)} onChange={(v) => setTimeoutS(Number(v) || 0)} mono /></Field>
            </div>
            <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 11, color: "var(--v2-text-secondary)" }}>
              <input type="checkbox" checked={dryRun} onChange={(e) => setDryRun(e.target.checked)} />
              Dry run (log only, não executa)
            </label>
            <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 11, color: "var(--v2-text-secondary)" }}>
              <input type="checkbox" checked={confirmReq} onChange={(e) => setConfirmReq(e.target.checked)} />
              Exigir confirmação (Control-M Confirm — só roda após o operador confirmar)
            </label>
          </>
        )}

        {tab === "schedule" && (
          <ScheduleEditor
            value={schedule}
            onChange={setSchedule}
            calendars={calendars}
            onCalendarsChange={setCalendars}
            availableCalendars={calendarDefs}
          />
        )}

        {tab === "action" && (
          <JobActionConfigEditor jobType={jobType} config={actionConfig} onChange={setActionConfig} />
        )}

        {tab === "ondo" && (
          <OnDoEditor value={actions} onChange={setActions} allDefs={allDefs} selfId={definition.id} />
        )}

        {tab === "deps" && (
          <DepsTab
            self={definition.id}
            upstream={upstream}
            onChangeUpstream={setUpstream}
            triggers={triggers}
            allDefs={allDefs}
            conditionsIn={conditionsIn}
            conditionsOutAdd={conditionsOutAdd}
            conditionsOutRemove={conditionsOutRemove}
            onChangeConditionsIn={setConditionsIn}
            onChangeConditionsOutAdd={setConditionsOutAdd}
            onChangeConditionsOutRemove={setConditionsOutRemove}
            knownConditions={knownConditions}
          />
        )}

        {err != null && <ErrorDialog error={err} onClose={() => setErr(null)} />}
        {validationErr && (
          <div style={{ padding: 8, background: "rgba(239,68,68,.08)", border: "1px solid rgba(239,68,68,.3)", borderRadius: 3, color: "var(--v2-status-failed)", fontSize: 11, fontFamily: "var(--v2-font-mono)" }}>{validationErr}</div>
        )}
        {tab === "general" && !isNew && team && id && (
          <Field label="History">
            <button type="button" onClick={() => setShowAudit((x) => !x)} style={{ background: "transparent", border: "1px solid #333", color: "#a3a3a3", padding: "4px 10px", borderRadius: 4, fontSize: 11, cursor: "pointer", marginBottom: showAudit ? 8 : 0 }}>
              {showAudit ? "▾ Hide audit log" : "▸ Show audit log"}
            </button>
            {showAudit && <DefinitionAuditPanel team={team} definitionId={id} />}
          </Field>
        )}
      </div>

      {/* Actions */}
      <div style={{ borderTop: "1px solid var(--v2-border-subtle)", padding: "8px 12px", display: "flex", gap: 6 }}>
        <button onClick={handleDelete} disabled={saving} style={{ ...btnStyle, borderColor: "rgba(239,68,68,.4)", color: "var(--v2-status-failed)" }}>{isNew ? "Cancel" : "Delete"}</button>
        <div style={{ flex: 1 }} />
        {isServerMode() && (
          <button onClick={handleSaveTemplate} disabled={saving} title="Salvar a forma deste job como template reutilizável (D-13)"
            style={btnStyle}>☆ Template</button>
        )}
        <button onClick={handleSave} disabled={saving} style={{ ...btnStyle, borderColor: "var(--v2-accent-brand)", color: "var(--v2-accent-brand)", fontWeight: 600 }}>{saving ? "…" : "Save"}</button>
      </div>
    </aside>
  );
}

/* ── Aba Dependencies (2 lados) ── */
function DepsTab({ self, upstream, onChangeUpstream, triggers, allDefs, conditionsIn, conditionsOutAdd, conditionsOutRemove, onChangeConditionsIn, onChangeConditionsOutAdd, onChangeConditionsOutRemove, knownConditions }: {
  self: string;
  upstream: Array<{ from: string; condition: EdgeCondition }>;
  onChangeUpstream: (u: Array<{ from: string; condition: EdgeCondition }>) => void;
  triggers: JobDefinition[];
  allDefs: JobDefinition[];
  conditionsIn: string[];
  conditionsOutAdd: string[];
  conditionsOutRemove: string[];
  onChangeConditionsIn: (v: string[]) => void;
  onChangeConditionsOutAdd: (v: string[]) => void;
  onChangeConditionsOutRemove: (v: string[]) => void;
  knownConditions: string[];
}) {
  const labelOf = (id: string) => allDefs.find((d) => d.id === id)?.label ?? id;
  const remove = (from: string) => onChangeUpstream(upstream.filter((u) => u.from !== from));

  // Referência cruzada por NOME: quem produz (conditionsOutAdd ou ação
  // set-condition) e quem consome (conditionsIn) cada condition no escopo.
  const producersOf = (name: string) =>
    allDefs.filter((d) => d.id !== self && (
      d.conditionsOutAdd?.includes(name) ||
      d.actions?.some((a) => a.do === "set-condition" && a.condition === name)
    )).map((d) => d.label);
  const consumersOf = (name: string) =>
    allDefs.filter((d) => d.id !== self && d.conditionsIn?.includes(name)).map((d) => d.label);
  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      {/* Depende de (upstream do próprio job) */}
      <div>
        <div style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 9, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--v2-text-muted)", marginBottom: 6 }}>
          <ArrowLeft size={11} /> Depende de
        </div>
        {upstream.length === 0 && <Hint>Este job não espera nenhum outro. Conecte um job → este no canvas para criar.</Hint>}
        {upstream.map((u) => (
          <div key={u.from} style={depRow}>
            <span style={{ flex: 1, fontSize: 12, fontFamily: "var(--v2-font-mono)" }}>{labelOf(u.from)}</span>
            <span style={{ fontSize: 9, color: "var(--v2-accent-brand)", fontFamily: "var(--v2-font-mono)" }}>{u.condition}</span>
            <button onClick={() => remove(u.from)} style={iconBtn} title="Remover"><Trash2 size={12} /></button>
          </div>
        ))}
      </div>
      {/* Dispara (jobs cujo upstream aponta para este) */}
      <div>
        <div style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 9, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--v2-text-muted)", marginBottom: 6 }}>
          <ArrowRight size={11} /> Dispara
        </div>
        {triggers.length === 0 && <Hint>Nenhum job depende deste.</Hint>}
        {triggers.map((d) => {
          const cond = (d.upstream ?? []).find((u) => u.from === self)?.condition;
          return (
            <div key={d.id} style={depRow}>
              <span style={{ flex: 1, fontSize: 12, fontFamily: "var(--v2-font-mono)" }}>{d.label}</span>
              <span style={{ fontSize: 9, color: "var(--v2-accent-brand)", fontFamily: "var(--v2-font-mono)" }}>{cond}</span>
            </div>
          );
        })}
        <Hint>Editado no job de destino (ou conectando no canvas). Mostrado aqui para você ver a relação dos dois lados.</Hint>
      </div>

      {/* Conditions nomeadas (F16) — o outro tipo de dependência: por NOME,
          não por aresta do grafo. Fecha o circuito com o set-condition do
          On/Do e com eventos externos (POST /events/ingest). */}
      <div style={{ borderTop: "1px solid var(--v2-border-subtle)", paddingTop: 12 }}>
        <div style={{ fontSize: 9, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--v2-text-muted)", marginBottom: 6 }}>
          Conditions (por nome)
        </div>
        <Hint>
          Além das setas do grafo, um job pode esperar/emitir <b>conditions nomeadas</b> do dia.
          Quem cria: a saída (＋) de outro job, uma ação On/Do <b>set-condition</b>, um evento
          externo ou o operador. O vínculo é o <b>nome exato</b>.
        </Hint>
        <div style={{ display: "flex", flexDirection: "column", gap: 10, marginTop: 10 }}>
          <CondChipsEditor
            title="Entrada — espera por"
            hint="O job fica em WAIT CONDITION até TODAS existirem no dia."
            value={conditionsIn}
            onChange={onChangeConditionsIn}
            known={knownConditions}
            listId="cond-known-in"
            crossRef={(n) => { const p = producersOf(n); return p.length ? `criada por: ${p.join(", ")}` : "ninguém cria esta condition no escopo (evento externo/operador?)"; }}
          />
          <CondChipsEditor
            title="Saída ＋ — cria ao terminar OK"
            hint="Ao terminar OK (ou Set OK), o job CRIA estas conditions."
            value={conditionsOutAdd}
            onChange={onChangeConditionsOutAdd}
            known={knownConditions}
            listId="cond-known-add"
            crossRef={(n) => { const c = consumersOf(n); return c.length ? `consumida por: ${c.join(", ")}` : "nenhum job espera por esta condition ainda"; }}
          />
          <CondChipsEditor
            title="Saída − — remove ao terminar OK"
            hint="Ao terminar OK, o job APAGA estas conditions (limpa o gate de quem depende delas)."
            value={conditionsOutRemove}
            onChange={onChangeConditionsOutRemove}
            known={knownConditions}
            listId="cond-known-rm"
            crossRef={(n) => { const c = consumersOf(n); return c.length ? `trava de volta: ${c.join(", ")}` : undefined; }}
          />
        </div>
      </div>
    </div>
  );
}

/* ── Editor de chips de conditions (F16) ── */
function CondChipsEditor({ title, hint, value, onChange, known, listId, crossRef }: {
  title: string;
  hint: string;
  value: string[];
  onChange: (v: string[]) => void;
  known: string[];
  listId: string;
  crossRef?: (name: string) => string | undefined;
}) {
  const [draft, setDraft] = useState("");
  const add = () => {
    const name = draft.trim();
    setDraft("");
    if (!name || value.includes(name)) return;
    onChange([...value, name]);
  };
  const suggestions = known.filter((k) => !value.includes(k));
  return (
    <div>
      <div style={{ fontSize: 10, fontWeight: 600, color: "var(--v2-text-secondary)", marginBottom: 4 }}>{title}</div>
      {value.map((n) => {
        const ref = crossRef?.(n);
        return (
          <div key={n} style={{ ...depRow, flexDirection: "column", alignItems: "stretch", gap: 2 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
              <span style={{ flex: 1, fontSize: 12, fontFamily: "var(--v2-font-mono)" }}>{n}</span>
              <button onClick={() => onChange(value.filter((v) => v !== n))} style={iconBtn} title="Remover"><Trash2 size={12} /></button>
            </div>
            {ref && <div style={{ fontSize: 9.5, color: "var(--v2-text-muted)" }}>{ref}</div>}
          </div>
        );
      })}
      <div style={{ display: "flex", gap: 6 }}>
        <input
          value={draft}
          list={listId}
          placeholder="ex.: ARQUIVO-CHEGOU"
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); add(); } }}
          style={{ flex: 1, background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)", color: "var(--v2-text-primary)", padding: "5px 8px", fontSize: 11, fontFamily: "var(--v2-font-mono)", borderRadius: 3, outline: "none", boxSizing: "border-box" }}
        />
        <datalist id={listId}>
          {suggestions.map((k) => <option key={k} value={k} />)}
        </datalist>
        <button onClick={add} disabled={!draft.trim()} style={{ ...btnStyle, borderColor: draft.trim() ? "var(--v2-accent-brand)" : "var(--v2-border-medium)", color: draft.trim() ? "var(--v2-accent-brand)" : "var(--v2-text-muted)" }}>＋</button>
      </div>
      <div style={{ fontSize: 9.5, color: "var(--v2-text-muted)", marginTop: 3, lineHeight: 1.4 }}>{hint}</div>
    </div>
  );
}

/* ── primitivos ── */
function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (<div><div style={{ fontSize: 9, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--v2-text-muted)", marginBottom: 4 }}>{label}</div>{children}</div>);
}
function Hint({ children }: { children: React.ReactNode }) {
  return <div style={{ fontSize: 10.5, color: "var(--v2-text-muted)", lineHeight: 1.45 }}>{children}</div>;
}
function Input({ value, onChange, disabled, mono, placeholder }: { value: string; onChange: (v: string) => void; disabled?: boolean; mono?: boolean; placeholder?: string }) {
  return <input value={value} onChange={(e) => onChange(e.target.value)} disabled={disabled} placeholder={placeholder}
    style={{ width: "100%", background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)", color: disabled ? "var(--v2-text-muted)" : "var(--v2-text-primary)", padding: "5px 8px", fontSize: 11, fontFamily: mono ? "var(--v2-font-mono)" : "var(--v2-font-sans)", borderRadius: 3, outline: "none", boxSizing: "border-box" }} />;
}
const selectStyle: React.CSSProperties = { width: "100%", background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)", color: "var(--v2-text-primary)", padding: "5px 8px", fontSize: 11, fontFamily: "var(--v2-font-mono)", borderRadius: 3, outline: "none", boxSizing: "border-box" };
const btnStyle: React.CSSProperties = { padding: "4px 10px", background: "transparent", border: "1px solid var(--v2-border-medium)", borderRadius: 3, fontSize: 10, fontFamily: "var(--v2-font-mono)", letterSpacing: "0.06em", textTransform: "uppercase", cursor: "pointer" };
const iconBtn: React.CSSProperties = { background: "transparent", border: "none", color: "var(--v2-text-muted)", cursor: "pointer", padding: 2, display: "inline-flex" };
const depRow: React.CSSProperties = { display: "flex", alignItems: "center", gap: 8, padding: "6px 8px", background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)", borderRadius: 3, marginBottom: 4 };
