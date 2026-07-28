import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ExternalLink, X, Trash2, ArrowRight, ArrowLeft, HelpCircle, Plus } from "lucide-react";
import { addToggleStyle } from "./add-toggle";
import type { JobDefinition, CalendarRef, ActionRule, DepDateRef, ConditionLogic, CondGroup, CondBoolOp } from "@/lib/orchestrator-model";
import { COND_TIME_TOKEN } from "@/lib/orchestrator-model";
import { splitCondSuffix, withCondSuffix, producesBase } from "@/lib/conditions-model";
import type { JobType } from "@/lib/job-config";
import JobActionConfigEditor from "./JobActionConfigEditor";
import OnDoEditor from "./OnDoEditor";
import ScheduleEditor from "./ScheduleEditor";
import { DefinitionAuditPanel } from "./DefinitionAuditPanel";
import { ErrorDialog } from "./ErrorDialog";
import { getGitInfo, definitionFileUrl } from "@/lib/git-info";
import { listCalendars, type Calendar, listResources, type ResourceState } from "@/lib/bloco2-api";
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
  /** Autosave (troca de job / fechar o drawer): salva SEM fechar o drawer. */
  onAutoSave?: (def: JobDefinition) => void | Promise<void>;
  onDelete: (id: string) => void | Promise<void>;
  onClose: () => void;
}

const JOB_TYPES: JobType[] = ["COMMAND", "SCRIPT", "SSH", "HTTP", "FILE_WATCH", "FILE_TRANSFER", "DATABASE", "LAMBDA", "BATCH", "GLUE", "STEP_FUNCTION"];
type Tab = "general" | "schedule" | "action" | "ondo" | "deps";
const TABS: Array<{ id: Tab; label: string }> = [
  { id: "general", label: "General" },
  { id: "schedule", label: "Schedule" },
  { id: "action", label: "Action" },
  { id: "ondo", label: "On/Do" },
  { id: "deps", label: "Conditions" },
];

interface Props {
  definition: JobDefinition;
  isNew: boolean;
  availableFolders: string[];
  /** Todas as defs do escopo — para a aba Dependencies (depende de / dispara). */
  allDefs?: JobDefinition[];
  handlers: JobConfigHandlers;
}

/* ── ID derivado do Job Name (Opção A, 2026-07-17) ── O `id` é o JOBNAME de
   verdade (nome do YAML, referência do upstream e das condições X-TO-Y da
   setinha): nasce como slug do nome digitado — minúsculo, sem acento — em vez
   do `jobtype-<timestamp>` ilegível, e fica IMUTÁVEL depois de criado (toda
   referência do sistema aponta pra ele; renomear o label não o altera). */
function slugifyJobName(v: string): string {
  return v
    .normalize("NFD").replace(/[\u0300-\u036f]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

// Colisão com id existente → sufixo numérico (extract, extract-2, extract-3…).
function uniqueJobId(label: string, taken: ReadonlySet<string>): string {
  const base = slugifyJobName(label);
  if (!base || !taken.has(base)) return base;
  for (let n = 2; ; n++) {
    const cand = `${base}-${n}`;
    if (!taken.has(cand)) return cand;
  }
}

export default function JobConfigDrawer({ definition, isNew, availableFolders, allDefs = [], handlers }: Props) {
  const [tab, setTab] = useState<Tab>("general");
  const [label, setLabel] = useState(definition.label);
  const [id, setId] = useState(definition.id);
  // idTouched — o usuário editou o ID à mão? Enquanto não editar, um job NOVO
  // tem o id SEGUINDO o Job Name digitado (slug); depois do primeiro toque o
  // campo vira manual. Job existente: id read-only (imutável por design).
  const [idTouched, setIdTouched] = useState(false);
  const [jobType, setJobType] = useState<JobType>(definition.jobType as JobType);
  const initialTeam = definition.team ?? (availableFolders.length === 1 ? availableFolders[0] : "");
  const [team, setTeam] = useState(initialTeam);
  const [schedule, setSchedule] = useState(definition.schedule);
  // Ausente = 0 (o Go serializa `retries` com omitempty, então retries=0 volta
  // do server sem o campo — o fallback TEM que ser 0, senão o drawer ressuscita
  // um 2 que ninguém pediu).
  const [retries, setRetries] = useState(definition.retries ?? 0);
  const [timeout, setTimeoutS] = useState(definition.timeout ?? 300);
  const [dryRun, setDryRun] = useState(definition.dryRun ?? false);
  const [confirmReq, setConfirmReq] = useState(definition.confirm ?? false);
  const [actionConfig, setActionConfig] = useState<Record<string, unknown>>(definition.actionConfig ?? {});
  const [calendars, setCalendars] = useState<CalendarRef[]>(definition.calendars ?? []);
  const [actions, setActions] = useState<ActionRule[]>(definition.actions ?? []);
  // CONDIÇÕES — o modelo ÚNICO de dependência: entrada (espera no pool),
  // saída＋ (cria no OK/Set OK), saída− (deleta no OK/Set OK = consumo).
  // A setinha do canvas escreve AQUI (out+ no pai; in + out− neste job) —
  // ligar pela caixinha ou digitar à mão dá no mesmo lugar (o pool).
  const [conditionsIn, setConditionsIn] = useState<string[]>(definition.conditionsIn ?? []);
  const [conditionsOutAdd, setConditionsOutAdd] = useState<string[]>(definition.conditionsOutAdd ?? []);
  const [conditionsOutRemove, setConditionsOutRemove] = useState<string[]>(definition.conditionsOutRemove ?? []);
  // CL — lógica booleana de ENTRADA (AND/OR). undefined = AND implícito de
  // conditionsIn (retrocompat). Quando presente, conditionsIn é mantido como a
  // UNIÃO dos membros (topologia/linhas/snapshot leem conditionsIn).
  const [conditionLogic, setConditionLogic] = useState<ConditionLogic | undefined>(definition.conditionLogic);
  // F15 — RECURSOS/QUOTAS consumidos pelo job (nome → quantidade). Vive na mesma
  // aba Condições, mas é um eixo DIFERENTE: condição = pré-requisito lógico (pool
  // do ambiente); recurso = limite de concorrência (semáforo). O gate é WAIT
  // RESOURCE (fila), não WAIT COND. Editado aqui, separado visualmente.
  const [resources, setResources] = useState<Record<string, number>>(definition.resources ?? {});
  const [availableResources, setAvailableResources] = useState<ResourceState[]>([]);
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

  /* ── Autosave (2026-07-17) ── O Design é DRAFT (o commit real é o Publish):
     trocar de job no canvas ou fechar o drawer não perde mais o que foi mexido
     — antes, clicar em outro job sem Save descartava tudo (report do usuário).
     Toda edição marca dirty; o snapshot do formulário vive em liveRef
     (atualizado no render) e é salvo na troca de job e no unmount. Job NOVO
     fica de fora: criar continua decisão explícita do Save (fechar um NEW
     descarta, como sempre). */
  const dirtyRef = useRef(false);
  // editedIdRef = de QUEM são os states na tela. No frame da troca de job o
  // prop `definition` já é o próximo e os states ainda são do anterior — o
  // guard evita um snapshot frankenstein (metade de cada job).
  const editedIdRef = useRef(definition.id);
  const liveRef = useRef<{ def: JobDefinition; dirty: boolean; isNew: boolean } | null>(null);
  const autoSaveRef = useRef(handlers.onAutoSave);
  // leitor é o flush (pós-commit: troca de job @reset effect e unmount) → efeito é
  // equivalente ao write em render; declarado ANTES do efeito de reset p/ o ref já
  // estar fresco quando o flush roda. Ver roadmap §RH.
  useEffect(() => { autoSaveRef.current = handlers.onAutoSave; });
  const flushAutosave = useCallback((snap: { def: JobDefinition; dirty: boolean; isNew: boolean } | null) => {
    if (!snap || !snap.dirty || snap.isNew || !autoSaveRef.current) return;
    const d = snap.def;
    if (!d.id.trim() || !d.label.trim() || !(d.team ?? "").trim()) return; // incompleta — não persiste sozinho
    Promise.resolve(autoSaveRef.current(d)).catch((e: unknown) => {
      toast.error(`Autosave of ${d.id} failed`, { detail: e instanceof Error ? e.message : String(e) });
    });
  }, []);
  /** Envolve um setter marcando o formulário como sujo (candidato a autosave). */
  function touch<T>(setter: (v: T) => void): (v: T) => void {
    return (v) => { dirtyRef.current = true; setter(v); };
  }

  useEffect(() => {
    // Troca de job com o drawer aberto: AUTOSAVE do anterior antes de resetar
    // (liveRef ainda guarda o snapshot do job velho — o guard do render pulou
    // o frame da troca). Depois o formulário vira o job novo, limpo.
    if (editedIdRef.current !== definition.id) flushAutosave(liveRef.current);
    editedIdRef.current = definition.id;
    dirtyRef.current = false;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- reset de formulário na troca de job (definition.id); key-remount perderia largura/scroll do drawer; invariante 4; ver roadmap §RH
    setTab("general");
    setLabel(definition.label);
    // Job NOVO: id nasce do Job Name (Opção A) — o `jobtype-<timestamp>` que o
    // V2Preview sugere é só fallback enquanto o slug do nome ficar vazio.
    if (isNew) {
      const taken = new Set(allDefs.map((d) => d.id));
      setId(uniqueJobId(definition.label, taken) || definition.id);
    } else {
      setId(definition.id);
    }
    setIdTouched(false);
    setJobType(definition.jobType as JobType);
    setTeam(definition.team ?? (availableFolders.length === 1 ? availableFolders[0] : ""));
    setSchedule(definition.schedule);
    setRetries(definition.retries ?? 0); setTimeoutS(definition.timeout ?? 300);
    setDryRun(definition.dryRun ?? false); setConfirmReq(definition.confirm ?? false); setActionConfig(definition.actionConfig ?? {});
    setCalendars(definition.calendars ?? []); setActions(definition.actions ?? []);
    setConditionsIn(definition.conditionsIn ?? []); setConditionsOutAdd(definition.conditionsOutAdd ?? []); setConditionsOutRemove(definition.conditionsOutRemove ?? []);
    setConditionLogic(definition.conditionLogic);
    setResources(definition.resources ?? {});
    setAgentId(typeof definition.actionConfig?._agentId === "string" ? (definition.actionConfig._agentId as string) : "");
    setAgentTouched(false);
    setErr(null); setValidationErr(null);
  }, [definition.id]); // eslint-disable-line react-hooks/exhaustive-deps

  // Fechar o drawer (X / trocar de modo) também persiste o que estava sujo.
  useEffect(() => () => { flushAutosave(liveRef.current); }, [flushAutosave]);

  // O prop `definition` é a def VIVA (V2Preview): a setinha do canvas grava
  // condições nela COM o drawer aberto. Diffamos o que mudou POR FORA e
  // aplicamos no estado local — sem perder o que o usuário digitou aqui e sem
  // o Save/autosave daqui sobrescrever a ligação recém-criada (era a raiz do
  // "liguei pela setinha e a dependência sumiu").
  const extCondsRef = useRef({
    in: definition.conditionsIn ?? [],
    add: definition.conditionsOutAdd ?? [],
    rm: definition.conditionsOutRemove ?? [],
  });
  const condSyncIdRef = useRef(definition.id);
  useEffect(() => {
    const ext = {
      in: definition.conditionsIn ?? [],
      add: definition.conditionsOutAdd ?? [],
      rm: definition.conditionsOutRemove ?? [],
    };
    const before = extCondsRef.current;
    extCondsRef.current = ext;
    if (condSyncIdRef.current !== definition.id) { condSyncIdRef.current = definition.id; return; } // troca de job: o reset já repôs tudo
    const merge = (cur: string[], now: string[], prev: string[]) => {
      const added = now.filter((n) => !prev.includes(n) && !cur.includes(n));
      const removed = prev.filter((n) => !now.includes(n) && cur.includes(n));
      if (added.length === 0 && removed.length === 0) return cur;
      return [...cur.filter((n) => !removed.includes(n)), ...added];
    };
    setConditionsIn((c) => merge(c, ext.in, before.in));
    setConditionsOutAdd((c) => merge(c, ext.add, before.add));
    setConditionsOutRemove((c) => merge(c, ext.rm, before.rm));
  }, [definition.id, definition.conditionsIn, definition.conditionsOutAdd, definition.conditionsOutRemove]);

  // Pin default na CRIAÇÃO: job novo nasce marcado no primeiro agente online
  // capaz do jobType (re-decide se o jobType mudar antes do usuário tocar no
  // seletor). Job existente e escolha manual nunca são sobrescritos.
  useEffect(() => {
    if (!isNew || agentTouched || agents.length === 0) return;
    const cap = String(jobType || "").toUpperCase();
    // eslint-disable-next-line react-hooks/set-state-in-effect -- auto-sugestão de agente em job novo; roda em resposta a dados assíncronos de agents (não é reset mecânico); ver roadmap §RH
    if (cap === "SSH") { setAgentId(""); return; } // agentless — roda no server
    const current = agents.find((a) => a.id === agentId);
    const currentServes = current?.capabilities?.some((c) => c.toUpperCase() === cap);
    if (agentId && currentServes) return;
    const match = agents.find((a) => a.online && a.capabilities?.some((c) => c.toUpperCase() === cap));
    if (match) setAgentId(match.id);
  }, [isNew, agentTouched, agents, jobType]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- reset de githubUrl quando muda team/id/isNew (link do arquivo no GitHub); reset-on-dep-change; ver roadmap §RH
    if (isNew || !definition.team || !definition.id) { setGithubUrl(null); return; }
    let cancel = false;
    void getGitInfo().then((st) => { if (!cancel) setGithubUrl(definitionFileUrl(st, definition.team!, definition.id)); });
    return () => { cancel = true; };
  }, [isNew, definition.team, definition.id]);

  useEffect(() => {
    let cancel = false;
    void listCalendars().then((cs) => { if (!cancel) setCalendarDefs(cs); }).catch(() => {});
    void listAgents().then((a) => { if (!cancel) setAgents(a); }).catch(() => {});
    void listResources().then((r) => { if (!cancel) setAvailableResources(r); }).catch(() => {});
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
      // `upstream` é VISÃO derivada das condições (nunca editada/persistida
      // aqui) — o save reescreve a def SEM ela; a topologia renasce do
      // name-matching na normalização (conditions-model / FileStore.List).
      upstream: undefined,
      conditionsIn: conditionsIn.length ? conditionsIn : undefined,
      conditionsOutAdd: conditionsOutAdd.length ? conditionsOutAdd : undefined,
      conditionsOutRemove: conditionsOutRemove.length ? conditionsOutRemove : undefined,
      // CL — poda grupos vazios ao salvar (espelha o NormalizeConditions do
      // backend) pra o working-set local não reter grupos fantasma; sem grupo = nil.
      conditionLogic: pruneConditionLogic(conditionLogic),
      resources: Object.keys(resources).length ? resources : undefined,
    };
  }

  async function handleSave() {
    if (!label.trim()) { setValidationErr("label is required"); setTab("general"); return; }
    if (!team.trim()) { setValidationErr("folder is required"); setTab("general"); return; }
    if (!id.trim()) { setValidationErr("id is required"); setTab("general"); return; }
    // Job novo: normaliza o id final (slug) e barra colisão ANTES do save —
    // o save do server é upsert, então id repetido SOBRESCREVERIA o job antigo.
    const finalId = isNew ? slugifyJobName(id) : id.trim();
    if (isNew) {
      if (!finalId) { setValidationErr("invalid id — use letters and digits"); setTab("general"); return; }
      if (allDefs.some((d) => d.id === finalId)) {
        setValidationErr(`id "${finalId}" already exists — pick another one`); setTab("general"); return;
      }
      if (finalId !== id) setId(finalId);
    }
    setValidationErr(null);
    const next = { ...buildDef(), id: finalId };
    setSaving(true); setErr(null);
    try {
      await handlers.onSave(next);
      // salvo — nada pendente pro autosave do unmount (o drawer fecha em seguida)
      dirtyRef.current = false;
      if (liveRef.current) liveRef.current.dirty = false;
    } catch (e) { setErr(e); } finally { setSaving(false); }
  }

  // D-13 — salva a FORMA deste job como template reutilizável (o server
  // descarta id/team/upstream: identidade e vínculos não viajam no molde).
  async function handleSaveTemplate() {
    const name = window.prompt("Template name:", id.trim() || label.trim());
    if (!name?.trim()) return;
    try {
      await saveTemplate(name.trim(), label.trim(), buildDef());
      toast.success(`Template "${name.trim()}" saved`, { detail: "available in the Templates tab of the palette" });
    } catch (e) {
      toast.error("Failed to save template", { detail: e instanceof Error ? e.message : String(e) });
    }
  }

  async function handleDelete() {
    if (isNew) { handlers.onClose(); return; }
    if (!confirm(`Delete definition "${definition.label}"? This removes the YAML from the repo.`)) return;
    // Deletou → NUNCA autosave no unmount (recriaria o job apagado).
    dirtyRef.current = false;
    if (liveRef.current) liveRef.current.dirty = false;
    setSaving(true);
    try { await handlers.onDelete(definition.id); } catch (e) { setErr(e); } finally { setSaving(false); }
  }

  // Jobs que DEPENDEM deste → "dispara": consumidores de alguma condição que
  // ESTE job (no estado atual do formulário) cria — name-matching, a mesma
  // regra da topologia derivada.
  const selfDraft: JobDefinition = { ...definition, conditionsOutAdd, actions };
  const triggers = allDefs.filter((d) =>
    d.id !== definition.id &&
    (d.conditionsIn ?? []).some((n) => producesBase(selfDraft, splitCondSuffix(n).base)));

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
  const condCount = conditionsIn.length + conditionsOutAdd.length + conditionsOutRemove.length + Object.keys(resources).length;

  const { width, onMouseDown, reset } = useResizablePanel({
    storageKey: "regente.panel.jobConfig.w", defaultWidth: 360, min: 280, max: 720, edge: "left",
  });

  // Snapshot vivo do formulário pro autosave — só enquanto os states na tela
  // pertencem a ESTE `definition` (ver editedIdRef; o frame da troca é pulado).
  // Snapshot vivo do formulário pro autosave — num useEffect SEM deps (roda a cada
  // commit) declarado DEPOIS do efeito de reset (@168): o reset lê o liveRef escrito no
  // COMMIT ANTERIOR (o snapshot do job anterior) antes deste sobrescrever; o guard
  // editedIdRef mantém o snapshot coerente por job (invariante 4). Sem acessar refs no
  // corpo do render. Validado ao vivo (bateria de autosave §RH). Ver roadmap §RH.
  useEffect(() => {
    if (editedIdRef.current === definition.id) {
      liveRef.current = { def: buildDef(), dirty: dirtyRef.current, isNew };
    }
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
          <a href={githubUrl} target="_blank" rel="noreferrer" title="View the YAML on GitHub"
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
            {t.id === "deps" && (triggers.length + condCount > 0) ? ` (${triggers.length + condCount})` : ""}
            {t.id === "ondo" && actions.length > 0 ? ` (${actions.length})` : ""}
          </button>
        ))}
      </div>

      <div style={{ flex: 1, overflowY: "auto", padding: "12px", display: "flex", flexDirection: "column", gap: 12 }}>
        {tab === "general" && (
          <>
            <Field label="Job Name"><Input value={label} onChange={(v) => {
              dirtyRef.current = true;
              setLabel(v);
              // Auto-follow: em job novo o id acompanha o nome até o usuário
              // editar o campo ID à mão (aí a escolha manual vence).
              if (isNew && !idTouched) {
                const taken = new Set(allDefs.map((d) => d.id));
                setId(uniqueJobId(v, taken));
              }
            }} /></Field>
            <Field label={isNew ? "ID (unique — becomes the YAML file name and the X-TO-Y condition names)" : "ID (immutable — YAML file name and X-TO-Y condition names)"}>
              <Input value={id} mono disabled={!isNew} placeholder="derived from Job Name"
                onChange={(v) => {
                  dirtyRef.current = true; setIdTouched(true);
                  // Sanitização leve ao digitar (minúsculo, sem acento, sem chars
                  // inválidos); o aparo de hífens das pontas fica pro Save, senão
                  // digitar "meu-job" perde o hífen no meio do caminho.
                  setId(v.normalize("NFD").replace(/[\u0300-\u036f]/g, "").toLowerCase().replace(/[^a-z0-9-]+/g, "-"));
                }} />
            </Field>
            <Field label="Job Type">
              <select value={jobType} onChange={(e) => { dirtyRef.current = true; setJobType(e.target.value as JobType); }} style={selectStyle}>
                {JOB_TYPES.map((t) => <option key={t} value={t}>{t}</option>)}
              </select>
            </Field>
            <Field label="Folder">
              <select value={team} onChange={(e) => { dirtyRef.current = true; setTeam(e.target.value); }} style={selectStyle}>
                <option value="">— select folder —</option>
                {availableFolders.map((t) => <option key={t} value={t}>{t}</option>)}
              </select>
            </Field>
            <Field label="Agent (where it runs)">
              <select value={agentId} onChange={(e) => { dirtyRef.current = true; setAgentTouched(true); setAgentId(e.target.value); }} style={selectStyle}>
                <option value="">Automatic (by capability)</option>
                {agents.map((a) => (
                  // BUG-8 — o SERVER-AGENT embutido não ganha sufixo de restrição
                  // depois do nome; agentes externos seguem mostrando as caps.
                  <option key={a.id} value={a.id}>
                    {a.id === "SERVER-AGENT" ? a.id : `${a.id} — ${a.capabilities.join("/") || "no caps"}`}
                  </option>
                ))}
                {agentId && !agents.some((a) => a.id === agentId) && (
                  <option value={agentId}>{agentId} (offline)</option>
                )}
              </select>
              <div style={{ fontSize: 10, color: "var(--v2-text-muted)", marginTop: 4, lineHeight: 1.4 }}>
                {agents.length === 0
                  ? "No agent online. Run regente-agent on the target machine (HTTP jobs run on the built-in SERVER-AGENT)."
                  : "Pinned = runs ONLY on that agent (if it goes down, the job waits in WAIT AGENT — it never migrates). Empty = the server picks any agent with the capability."}
              </div>
            </Field>
            <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 11, color: "var(--v2-text-secondary)" }}>
              <input type="checkbox" checked={schedule.enabled} onChange={(e) => { dirtyRef.current = true; setSchedule({ ...schedule, enabled: e.target.checked }); }} />
              Schedule enabled
            </label>
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
              <Field label="Retries"><Input value={String(retries)} onChange={(v) => { dirtyRef.current = true; setRetries(Number(v) || 0); }} mono /></Field>
              <Field label="Timeout (s)"><Input value={String(timeout)} onChange={(v) => { dirtyRef.current = true; setTimeoutS(Number(v) || 0); }} mono /></Field>
            </div>
            <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 11, color: "var(--v2-text-secondary)" }}>
              <input type="checkbox" checked={dryRun} onChange={(e) => { dirtyRef.current = true; setDryRun(e.target.checked); }} />
              Dry run (log only, runs nothing)
            </label>
            <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 11, color: "var(--v2-text-secondary)" }}>
              <input type="checkbox" checked={confirmReq} onChange={(e) => { dirtyRef.current = true; setConfirmReq(e.target.checked); }} />
              Require confirmation (only runs after an operator confirms)
            </label>
          </>
        )}

        {tab === "schedule" && (
          <ScheduleEditor
            value={schedule}
            onChange={touch(setSchedule)}
            calendars={calendars}
            onCalendarsChange={touch(setCalendars)}
            availableCalendars={calendarDefs}
          />
        )}

        {tab === "action" && (
          <JobActionConfigEditor jobType={jobType} config={actionConfig} onChange={touch(setActionConfig)} />
        )}

        {tab === "ondo" && (
          <OnDoEditor value={actions} onChange={touch(setActions)} allDefs={allDefs} selfId={definition.id} />
        )}

        {tab === "deps" && (
          <DepsTab
            self={definition.id}
            triggers={triggers}
            allDefs={allDefs}
            conditionsIn={conditionsIn}
            conditionsOutAdd={conditionsOutAdd}
            conditionsOutRemove={conditionsOutRemove}
            conditionLogic={conditionLogic}
            windowFrom={schedule.windowFrom}
            onOpenSchedule={() => setTab("schedule")}
            onChangeConditionsIn={touch(setConditionsIn)}
            onChangeConditionLogic={touch(setConditionLogic)}
            onChangeConditionsOutAdd={touch(setConditionsOutAdd)}
            onChangeConditionsOutRemove={touch(setConditionsOutRemove)}
            knownConditions={knownConditions}
            resources={resources}
            onChangeResources={touch(setResources)}
            availableResources={availableResources}
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
          <button onClick={handleSaveTemplate} disabled={saving} title="Save this job's shape as a reusable template (D-13)"
            style={btnStyle}>☆ Template</button>
        )}
        <button onClick={handleSave} disabled={saving} style={{ ...btnStyle, borderColor: "var(--v2-accent-brand)", color: "var(--v2-accent-brand)", fontWeight: 600 }}>{saving ? "…" : "Save"}</button>
      </div>
    </aside>
  );
}

/* ── Aba CONDIÇÕES — o modelo único de dependência ── */
function DepsTab({ self, triggers, allDefs, conditionsIn, conditionsOutAdd, conditionsOutRemove, conditionLogic, windowFrom, onOpenSchedule, onChangeConditionsIn, onChangeConditionLogic, onChangeConditionsOutAdd, onChangeConditionsOutRemove, knownConditions, resources, onChangeResources, availableResources }: {
  self: string;
  triggers: JobDefinition[];
  allDefs: JobDefinition[];
  conditionsIn: string[];
  conditionsOutAdd: string[];
  conditionsOutRemove: string[];
  conditionLogic?: ConditionLogic;
  windowFrom?: string;
  onOpenSchedule: () => void;
  onChangeConditionsIn: (v: string[]) => void;
  onChangeConditionLogic: (v: ConditionLogic | undefined) => void;
  onChangeConditionsOutAdd: (v: string[]) => void;
  onChangeConditionsOutRemove: (v: string[]) => void;
  knownConditions: string[];
  resources: Record<string, number>;
  onChangeResources: (v: Record<string, number>) => void;
  availableResources: ResourceState[];
}) {
  // Referência cruzada por NOME-BASE: quem produz (conditionsOutAdd ou ação
  // set-condition) e quem consome (conditionsIn) cada condição no escopo.
  // O sufixo de data (@odat/@prev/@stat) é referência RELATIVA de cada lado —
  // "X@prev" na entrada espera o X que alguém cria sem sufixo na diária
  // anterior — então o vínculo visual compara os nomes SEM o sufixo.
  const sameCond = (a: string, b: string) => splitCondSuffix(a).base === splitCondSuffix(b).base;
  const producersOf = (name: string) =>
    allDefs.filter((d) => d.id !== self && producesBase(d, splitCondSuffix(name).base)).map((d) => d.label);
  const consumersOf = (name: string) =>
    allDefs.filter((d) => d.id !== self && d.conditionsIn?.some((c) => sameCond(c, name))).map((d) => d.label);

  // "Depende de" (leitura): os produtores de cada condição de ENTRADA — a
  // mesma topologia das linhas do canvas (name-matching).
  const dependsOn = [...new Set(conditionsIn.flatMap((n) => producersOf(n)))];

  // O manual completo do modelo mora atrás do "?" — quem já sabe usar a aba
  // não precisa ler o textão toda vez (report do usuário: poluía a tela).
  const [showHelp, setShowHelp] = useState(false);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      {/* ── RECURSOS / QUOTAS ── Eixo SEPARADO das condições: limite de
          concorrência (semáforo), não pré-requisito lógico. Bloco próprio com
          moldura âmbar pra deixar a fronteira visual explícita. */}
      <ResourcesEditor resources={resources} onChange={onChangeResources} available={availableResources} />

      {/* Divisória forte entre os dois eixos (recurso × condição). */}
      <div style={{ borderTop: "1px dashed var(--v2-border-strong)", margin: "2px 0 0" }} />

      {/* ── CONDIÇÕES ── (position:relative isola o popover "?" deste bloco) */}
      <div style={{ display: "flex", flexDirection: "column", gap: 14, position: "relative" }}>
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <span style={{ fontSize: 9, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--v2-text-muted)" }}>
          Single model — condition pool
        </span>
        <button
          onClick={() => setShowHelp((v) => !v)}
          title="How conditions work"
          style={{
            marginLeft: "auto", width: 20, height: 20, borderRadius: "50%",
            display: "inline-flex", alignItems: "center", justifyContent: "center",
            background: showHelp ? "var(--v2-accent-deep)" : "transparent",
            border: `1px solid ${showHelp ? "var(--v2-accent-brand)" : "var(--v2-border-medium)"}`,
            color: showHelp ? "var(--v2-accent-brand)" : "var(--v2-text-muted)",
            cursor: "pointer", padding: 0, flexShrink: 0,
          }}
        >
          <HelpCircle size={13} />
        </button>
      </div>
      {showHelp && (
        <div style={{
          position: "absolute", top: 26, left: 0, right: 0, zIndex: 10,
          background: "var(--v2-bg-elevated)",
          border: "1px solid var(--v2-accent-brand)", borderRadius: 8,
          boxShadow: "0 10px 30px rgba(0,0,0,0.45)", padding: "12px 14px",
        }}>
          <div style={{ display: "flex", alignItems: "center", marginBottom: 6 }}>
            <span style={{ fontSize: 10, fontWeight: 700, letterSpacing: "0.06em", textTransform: "uppercase", color: "var(--v2-accent-brand)" }}>
              How it works
            </span>
            <button onClick={() => setShowHelp(false)} title="Close" style={{ ...iconBtn, marginLeft: "auto" }}><X size={12} /></button>
          </div>
          <div style={{ fontSize: 10.5, color: "var(--v2-text-secondary)", lineHeight: 1.5, display: "flex", flexDirection: "column", gap: 10, maxHeight: 340, overflowY: "auto" }}>
            <HelpTopic title="Model — a single pool">
              <b>Every dependency is a condition in a single environment-wide pool</b> (the
              “Conditions” button in Monitoring). Linking A→B with the canvas <b>arrow</b>
              creates the <code>A-TO-B</code> condition automatically: out＋ on the parent,
              in + out− here. Doing it <b>by hand</b> lands in the same place: the same name
              in the parent's out＋ and in <b>this job's in AND out−</b>.
            </HelpTopic>
            <HelpTopic title="In — depends on">
              The job sits in <b>WAIT COND</b> until <b>ALL</b> inbound conditions exist in
              the pool. <b>Set OK + rerun</b> goes back to waiting if out− already deleted
              the condition.
            </HelpTopic>
            <HelpTopic title="AND/OR — inbound logic">
              By default the inbound side is an <b>AND</b> (all required). Click
              <b> AND/OR</b> next to “In” to group them: each <b>group</b> has its own
              operator (<b>AND</b>/<b>OR</b>) and the groups combine through a <b>top</b>
              operator. E.g. <code>(C1 AND C2) OR C3</code> runs on whichever branch closes
              first. A condition added by the <b>arrow</b> on a job with logic becomes an
              <b> AND requirement</b> (mandatory).
            </HelpTopic>
            <HelpTopic title="OR time — temporal fallback">
              With <b>one</b> condition the <b>OR run at the scheduled time</b> shortcut
              shows up: the job runs when the condition arrives <b>OR</b> when the time is
              reached — whichever comes first. The time stops being a floor and becomes the
              <b> ⏱ time</b> member of the expression. It needs the <b>“from”</b> time
              (Schedule tab) — without it, the shortcut takes you there. Inside <b>groups</b>,
              the <b>＋ time</b> button adds the same member. The <b>“to”</b> time (windowTo)
              always stays a hard ceiling.
            </HelpTopic>
            <HelpTopic title="Out ＋ — added when it ends OK">
              When it ends OK (or on Set OK), the job <b>ADDS</b> these conditions to the
              pool — that is what releases whoever depends on them.
            </HelpTopic>
            <HelpTopic title="Out − — deleted when it ends OK (consumption)">
              When it ends OK (or on Set OK), the job <b>DELETES</b> these conditions from
              the pool — that is the <b>consumption</b> of its inbound side. Without out−,
              the condition survives the OK and a rerun goes straight through.
            </HelpTopic>
            <HelpTopic title="Dates — the selector on each row">
              <b>Odate</b> (default) = the job's origin daily · <b>Prev</b> = previous
              daily · <b>Stat</b> = static, no date.
            </HelpTopic>
          </div>
        </div>
      )}

      <EntryConditions
        conditionsIn={conditionsIn}
        conditionLogic={conditionLogic}
        windowFrom={windowFrom}
        onOpenSchedule={onOpenSchedule}
        onChangeConditionsIn={onChangeConditionsIn}
        onChangeConditionLogic={onChangeConditionLogic}
        known={knownConditions}
        crossRef={(n) => { const p = producersOf(n); return p.length ? `created by: ${p.join(", ")}` : "nobody in this scope creates this condition (operator/Conditions panel?)"; }}
      />
      <CondChipsEditor
        title="Out ＋ — added when it ends OK"
        value={conditionsOutAdd}
        onChange={onChangeConditionsOutAdd}
        known={knownConditions}
        listId="cond-known-add"
        crossRef={(n) => { const c = consumersOf(n); return c.length ? `consumed by: ${c.join(", ")}` : "no job waits for this condition yet"; }}
      />
      <CondChipsEditor
        title="Out − — deleted when it ends OK (consumption)"
        value={conditionsOutRemove}
        onChange={onChangeConditionsOutRemove}
        known={knownConditions}
        listId="cond-known-rm"
        crossRef={(n) => { const c = consumersOf(n); return c.length ? `blocks again: ${c.join(", ")}` : undefined; }}
      />

      {/* Vista dos dois lados (leitura, derivada por name-matching) */}
      <div style={{ borderTop: "1px solid var(--v2-border-subtle)", paddingTop: 12 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 9, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--v2-text-muted)", marginBottom: 6 }}>
          <ArrowLeft size={11} /> Depends on
        </div>
        {dependsOn.length === 0 && <Hint>No job creates this one's inbound conditions (or there are none).</Hint>}
        {dependsOn.map((l) => (
          <div key={l} style={depRow}>
            <span style={{ flex: 1, fontSize: 12, fontFamily: "var(--v2-font-mono)" }}>{l}</span>
          </div>
        ))}
        <div style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 9, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--v2-text-muted)", margin: "10px 0 6px" }}>
          <ArrowRight size={11} /> Triggers
        </div>
        {triggers.length === 0 && <Hint>No job waits for the conditions this one creates.</Hint>}
        {triggers.map((d) => (
          <div key={d.id} style={depRow}>
            <span style={{ flex: 1, fontSize: 12, fontFamily: "var(--v2-font-mono)" }}>{d.label}</span>
          </div>
        ))}
      </div>
      </div>
    </div>
  );
}

/* ── Editor de RECURSOS / quotas (F15) ── Eixo DISTINTO das condições: não é
   pré-requisito lógico (pool do ambiente) e sim limite de concorrência
   (semáforo Control-M). Nome → quantidade consumida; sem unidade livre o job
   fica em WAIT RESOURCE (fila). Pedido multi-recurso é tudo-ou-nada. A moldura
   âmbar marca a fronteira visual pedida. A CAPACIDADE de cada recurso é gerida
   no Monitoring (painel Recursos) — aqui só se ESCOLHE quais o job consome. */
const RES_ACCENT = "#f59e0b";
function ResourcesEditor({ resources, onChange, available }: {
  resources: Record<string, number>;
  onChange: (v: Record<string, number>) => void;
  available: ResourceState[];
}) {
  const [draftName, setDraftName] = useState("");
  const [draftQty, setDraftQty] = useState("1");
  const [showHelp, setShowHelp] = useState(false);
  const [adding, setAdding] = useState(false); // campo escondido até clicar no ＋ (padrão do produto)
  const entries = Object.entries(resources);
  const capOf = (name: string) => available.find((r) => r.name === name);
  const add = () => {
    const name = draftName.trim();
    const qty = Math.max(1, parseInt(draftQty || "1", 10) || 1);
    if (!name || name in resources) { setDraftName(""); return; }
    onChange({ ...resources, [name]: qty });
    setDraftName(""); setDraftQty("1");
  };
  const setQty = (name: string, qty: number) => onChange({ ...resources, [name]: Math.max(1, qty || 1) });
  const remove = (name: string) => { const next = { ...resources }; delete next[name]; onChange(next); };
  const suggestions = available.filter((r) => !(r.name in resources));
  const numInput: React.CSSProperties = {
    width: 54, background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)",
    color: "var(--v2-text-primary)", padding: "3px 6px", fontSize: 11,
    fontFamily: "var(--v2-font-mono)", borderRadius: 3, boxSizing: "border-box",
  };
  return (
    <div style={{
      border: `1px solid ${RES_ACCENT}55`, borderRadius: 8, padding: "10px 12px",
      background: "rgba(245,158,11,0.05)", display: "flex", flexDirection: "column", gap: 8, position: "relative",
    }}>
      <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
        <span style={{ width: 6, height: 6, borderRadius: "50%", background: RES_ACCENT }} />
        <span style={{ fontSize: 9, letterSpacing: "0.08em", textTransform: "uppercase", color: RES_ACCENT, fontWeight: 700 }}>
          Resources / Quotas
        </span>
        <button onClick={() => setShowHelp((v) => !v)} title="How resources work" style={{ ...addToggleStyle(showHelp, RES_ACCENT), marginLeft: "auto" }}>
          <HelpCircle size={13} />
        </button>
        <button onClick={() => setAdding((v) => !v)} title={adding ? "Close" : "Add resource"} style={addToggleStyle(adding, RES_ACCENT)}>
          {adding ? <X size={12} /> : <Plus size={12} />}
        </button>
      </div>
      {showHelp && (
        <div style={{ position: "absolute", top: 30, left: 8, right: 8, zIndex: 10, background: "var(--v2-bg-elevated)", border: `1px solid ${RES_ACCENT}`, borderRadius: 8, boxShadow: "0 10px 30px rgba(0,0,0,0.45)", padding: "12px 14px", display: "flex", flexDirection: "column", gap: 10 }}>
          <div style={{ display: "flex", alignItems: "center" }}>
            <span style={{ fontSize: 10, fontWeight: 700, letterSpacing: "0.06em", textTransform: "uppercase", color: RES_ACCENT }}>How resources work</span>
            <button onClick={() => setShowHelp(false)} title="Close" style={{ ...iconBtn, marginLeft: "auto" }}><X size={12} /></button>
          </div>
          <div style={{ fontSize: 10.5, color: "var(--v2-text-secondary)", lineHeight: 1.5 }}>
            A resource is a <b>concurrency semaphore</b> (quota), not a logical prerequisite.
            With no free unit in the pool, the job waits in <b style={{ color: RES_ACCENT }}>WAIT RESOURCE</b> (queue).
            Several resources on one job = <b>all-or-nothing</b> (it only starts if it fits in all of them at once).
          </div>
          <div style={{ fontSize: 10.5, color: "var(--v2-text-secondary)", lineHeight: 1.5 }}>
            Here you only CHOOSE which ones the job consumes and how much. Each resource's <b>capacity</b> is
            managed in Monitoring (the <b>Resources</b> panel) and survives a restart. A new resource is born with
            capacity 1 (exclusive lock) until you adjust it there.
          </div>
        </div>
      )}
      {entries.map(([name, qty]) => {
        const cap = capOf(name);
        return (
          <div key={name} style={{ ...depRow, borderColor: `${RES_ACCENT}44` }}>
            <span style={{ flex: 1, fontSize: 12, fontFamily: "var(--v2-font-mono)" }}>{name}</span>
            <span style={{ fontSize: 9, color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)" }}>
              {cap ? `cap ${cap.capacity} · used ${cap.used}` : "new · cap 1 on create"}
            </span>
            <input type="number" min={1} value={qty} title="quantity consumed by this job"
              onChange={(e) => setQty(name, parseInt(e.target.value, 10))} style={numInput} />
            <button onClick={() => remove(name)} style={iconBtn} title="Remove resource"><Trash2 size={12} /></button>
          </div>
        );
      })}
      {adding && (
        <div style={{ display: "flex", gap: 6 }}>
          <input
            autoFocus
            value={draftName}
            list="job-known-resources"
            placeholder="resource… (e.g. SAP)"
            onChange={(e) => setDraftName(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); add(); } else if (e.key === "Escape") { setDraftName(""); setAdding(false); } }}
            onFocus={(e) => { e.currentTarget.style.borderColor = RES_ACCENT; }}
            onBlur={(e) => { e.currentTarget.style.borderColor = "var(--v2-border-medium)"; }}
            style={{ flex: 1, background: "var(--v2-bg-elevated)", border: "1px dashed var(--v2-border-medium)", color: "var(--v2-text-primary)", padding: "5px 8px", fontSize: 11, fontFamily: "var(--v2-font-mono)", borderRadius: 3, outline: "none", boxSizing: "border-box" }}
          />
          <datalist id="job-known-resources">
            {suggestions.map((r) => <option key={r.name} value={r.name}>{`cap ${r.capacity}`}</option>)}
          </datalist>
          <input type="number" min={1} value={draftQty} title="quantity"
            onChange={(e) => setDraftQty(e.target.value.replace(/\D/g, ""))}
            style={{ ...numInput, background: "var(--v2-bg-elevated)", border: "1px dashed var(--v2-border-medium)" }} />
          <button onClick={add} disabled={!draftName.trim()} title="Add" style={{ ...btnStyle, borderColor: draftName.trim() ? RES_ACCENT : "var(--v2-border-medium)", color: draftName.trim() ? RES_ACCENT : "var(--v2-text-muted)" }}>＋</button>
        </div>
      )}
    </div>
  );
}

/* ── Editor de chips de condições ── */
// A DATA de uma condição vive como sufixo no NOME (contrato do server —
// domain.SplitCondRef): sem sufixo/"@odat" = diária de ORIGEM do job;
// "@prev" = diária anterior; "@stat" = permanente (sem data). O seletor por
// linha edita o sufixo sem o usuário digitar arroba (helpers em
// lib/conditions-model — os MESMOS do canvas e da normalização).

function CondChipsEditor({ title, value, onChange, known, listId, crossRef, headerExtra }: {
  title: string;
  value: string[];
  onChange: (v: string[]) => void;
  known: string[];
  listId: string;
  crossRef?: (name: string) => string | undefined;
  headerExtra?: React.ReactNode;
}) {
  const [draft, setDraft] = useState("");
  // Padrão "+ ao lado do título" (regra do produto): o campo de digitar fica
  // ESCONDIDO até clicar no ＋ — inclusive para a PRIMEIRA condição. Some quando
  // não se está adicionando (workspace limpo). Fica aberto após adicionar pra
  // encadear várias; o ＋ vira ✕ pra fechar.
  const [adding, setAdding] = useState(false);
  const add = () => {
    const name = draft.trim();
    setDraft("");
    if (!name || value.includes(name)) return;
    onChange([...value, name]);
  };
  const suggestions = known.filter((k) => !value.includes(k));
  const setRef = (name: string, ref: DepDateRef) => {
    const next = withCondSuffix(splitCondSuffix(name).base, ref);
    if (next === name) return;
    if (value.includes(next)) { // já existe a variante com essa data — só remove a duplicata
      onChange(value.filter((v) => v !== name));
      return;
    }
    onChange(value.map((v) => (v === name ? next : v)));
  };
  return (
    <div>
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: value.length ? 4 : 0 }}>
        <span style={{ fontSize: 10, fontWeight: 600, color: "var(--v2-text-secondary)" }}>{title}</span>
        <button onClick={() => setAdding((v) => !v)} title={adding ? "Close" : "Add condition"} style={addToggleStyle(adding)}>
          {adding ? <X size={12} /> : <Plus size={12} />}
        </button>
        {headerExtra}
      </div>
      {value.map((n) => {
        const ref = crossRef?.(n);
        const { base, ref: dateRef } = splitCondSuffix(n);
        return (
          <div key={n} style={{ ...depRow, flexDirection: "column", alignItems: "stretch", gap: 2 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
              <span style={{ flex: 1, fontSize: 12, fontFamily: "var(--v2-font-mono)" }}>{base}</span>
              {/* Data da condition (mesma régua do dateRef das setas): Odate =
                  diária de origem (default) · Prev = anterior · Stat = permanente. */}
              <select
                value={dateRef}
                title="Which daily this condition refers to (relative to the job's origin date); Stat = permanent, no date"
                onChange={(e) => setRef(n, e.target.value as DepDateRef)}
                style={{ ...selectStyle, width: 72, padding: "2px 4px", fontSize: 9 }}
              >
                <option value="odat">Odate</option>
                <option value="prev">Prev</option>
                <option value="stat">Stat</option>
              </select>
              <button onClick={() => onChange(value.filter((v) => v !== n))} style={iconBtn} title="Remove"><Trash2 size={12} /></button>
            </div>
            {ref && <div style={{ fontSize: 9.5, color: "var(--v2-text-muted)" }}>{ref}</div>}
          </div>
        );
      })}
      {adding && (
        <div style={{ display: "flex", gap: 6, marginTop: value.length ? 4 : 0 }}>
          {/* Campo de condição NOVA — só aparece ao clicar no ＋ do título. Visual
              distinto das chips (fundo elevado + borda tracejada). */}
          <input
            autoFocus
            value={draft}
            list={listId}
            placeholder="new condition… (e.g. FILE-ARRIVED)"
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); add(); } else if (e.key === "Escape") { setDraft(""); setAdding(false); } }}
            onFocus={(e) => { e.currentTarget.style.borderColor = "var(--v2-accent-brand)"; }}
            onBlur={(e) => { e.currentTarget.style.borderColor = "var(--v2-border-medium)"; }}
            style={{ flex: 1, background: "var(--v2-bg-elevated)", border: "1px dashed var(--v2-border-medium)", color: "var(--v2-text-primary)", padding: "5px 8px", fontSize: 11, fontFamily: "var(--v2-font-mono)", borderRadius: 3, outline: "none", boxSizing: "border-box" }}
          />
          <datalist id={listId}>
            {suggestions.map((k) => <option key={k} value={k} />)}
          </datalist>
          <button onClick={add} disabled={!draft.trim()} title="Add" style={{ ...btnStyle, borderColor: draft.trim() ? "var(--v2-accent-brand)" : "var(--v2-border-medium)", color: draft.trim() ? "var(--v2-accent-brand)" : "var(--v2-text-muted)" }}>＋</button>
        </div>
      )}
    </div>
  );
}

/* ── Entrada com lógica AND/OR (CL) ── Progressivo: por padrão a entrada é uma
   lista plana (AND implícito, igual antes). O toggle "AND/OR" revela o editor de
   GRUPOS — cada grupo com seu operador (AND/OR) + um operador de TOPO entre grupos
   (forma DNF, espelha domain.ConditionLogic). A UI mantém `conditionsIn` = UNIÃO
   dos membros (topologia/linhas/snapshot leem conditionsIn); `conditionLogic` só
   existe no modo avançado. Uma condição vinda de fora (setinha) que não está em
   nenhum grupo é preservada e o backend a trata como requisito AND avulso. */
const AND_OR_ACCENT = "var(--v2-accent-brand)";
// pruneConditionLogic — remove grupos vazios; sem grupo restante vira undefined
// (AND implícito). Espelha o NormalizeConditions do backend pra o save do front
// não reter grupos fantasma. Usado no buildDef (momento do save, não da edição:
// durante a edição um grupo recém-criado fica vazio de propósito).
function pruneConditionLogic(l?: ConditionLogic): ConditionLogic | undefined {
  if (!l) return undefined;
  const groups = l.groups.filter((g) => g.members.length > 0);
  return groups.length ? { op: l.op, groups } : undefined;
}
function EntryConditions({ conditionsIn, conditionLogic, windowFrom, onOpenSchedule, onChangeConditionsIn, onChangeConditionLogic, known, crossRef }: {
  conditionsIn: string[];
  conditionLogic?: ConditionLogic;
  windowFrom?: string;
  onOpenSchedule: () => void;
  onChangeConditionsIn: (v: string[]) => void;
  onChangeConditionLogic: (v: ConditionLogic | undefined) => void;
  known: string[];
  crossRef?: (name: string) => string | undefined;
}) {
  const advanced = !!conditionLogic;
  // Ligar o avançado: semeia um grupo AND com as condições atuais (ou vazio).
  const enable = () => onChangeConditionLogic({ op: "AND", groups: [{ op: "AND", members: [...conditionsIn] }] });
  // Desligar: some a lógica; conditionsIn permanece como lista plana (AND).
  const disable = () => onChangeConditionLogic(undefined);
  // editLogic — recomputa conditionsIn = avulsos (fora da lógica anterior) ∪ novos
  // membros; nunca perde uma condição adicionada por fora (setinha). O token
  // $TIME NÃO é condição do pool → nunca entra em conditionsIn.
  const editLogic = (next: ConditionLogic) => {
    const oldMembers = new Set((conditionLogic?.groups ?? []).flatMap((g) => g.members));
    const loose = conditionsIn.filter((n) => !oldMembers.has(n));
    const newMembers = next.groups.flatMap((g) => g.members).filter((m) => m !== COND_TIME_TOKEN);
    onChangeConditionsIn([...new Set([...loose, ...newMembers])]);
    onChangeConditionLogic(next);
  };
  // CL-2 — fallback temporal: com UMA condição e "a partir de" preenchido, roda
  // pela condição OU pelo horário (o que vier primeiro). Cria a DNF (C1) OU ($TIME).
  const enableTimeFallback = () => onChangeConditionLogic({
    op: "OR", groups: [{ op: "AND", members: [conditionsIn[0]] }, { op: "AND", members: [COND_TIME_TOKEN] }],
  });
  const advToggle = (
    <button onClick={advanced ? disable : enable}
      title={advanced ? "Back to the simple list (AND)" : "Group with AND/OR logic"}
      style={{ marginLeft: "auto", fontSize: 8.5, letterSpacing: "0.06em", fontWeight: 700, padding: "2px 7px", borderRadius: 10, cursor: "pointer",
        background: advanced ? "var(--v2-accent-deep)" : "transparent",
        border: `1px solid ${advanced ? AND_OR_ACCENT : "var(--v2-border-medium)"}`,
        color: advanced ? AND_OR_ACCENT : "var(--v2-text-muted)" }}>
      AND/OR
    </button>
  );
  if (!advanced) {
    return (
      <div>
        <CondChipsEditor title="In — depends on" value={conditionsIn} onChange={onChangeConditionsIn}
          known={known} listId="cond-known-in" crossRef={crossRef} headerExtra={advToggle} />
        {/* Atalho CL-2: sempre visível com UMA condição — o fallback temporal não
            pode ficar invisível (report: "não achei formas de fazer OR com o
            horário"). Sem windowFrom o clique leva à aba Schedule: $TIME sem
            "From" seria satisfeito imediatamente e anularia a condição. */}
        {conditionsIn.length === 1 && (windowFrom ? (
          <button onClick={enableTimeFallback}
            title={`Runs when the condition arrives OR at ${windowFrom} — whichever comes first`}
            style={{ marginTop: 6, display: "flex", alignItems: "center", gap: 6, width: "100%", textAlign: "left",
              background: "transparent", border: "1px dashed var(--v2-border-medium)", borderRadius: 4, padding: "5px 8px",
              color: "var(--v2-text-muted)", cursor: "pointer", fontSize: 10 }}>
            <Plus size={11} /> <span><b>OR</b> run at the scheduled time (from <b>{windowFrom}</b>) — whichever comes first</span>
          </button>
        ) : (
          <button onClick={onOpenSchedule}
            title={'The "condition OR time" fallback needs a window start — click to set "From" in the Schedule tab'}
            style={{ marginTop: 6, display: "flex", alignItems: "center", gap: 6, width: "100%", textAlign: "left",
              background: "transparent", border: "1px dashed var(--v2-border-subtle)", borderRadius: 4, padding: "5px 8px",
              color: "var(--v2-text-muted)", opacity: 0.75, cursor: "pointer", fontSize: 10 }}>
            <Plus size={11} /> <span><b>OR</b> run at the scheduled time — needs <b>“From”</b> (click to open the Schedule tab)</span>
          </button>
        ))}
      </div>
    );
  }
  return <GroupedLogicEditor logic={conditionLogic!} onChange={editLogic} onDisable={disable} known={known} crossRef={crossRef} windowFrom={windowFrom} onOpenSchedule={onOpenSchedule} />;
}

// OpToggle — chave AND/OR (pill de dois botões), usada no topo e por grupo.
// Rótulos em inglês de propósito (pedido do usuário): AND/OR é o vocabulário
// canônico do operador — o mesmo do YAML (`op: AND|OR`) e do toggle do header.
function OpToggle({ value, onChange, title }: { value: CondBoolOp; onChange: (op: CondBoolOp) => void; title?: string }) {
  return (
    <div title={title} style={{ display: "inline-flex", border: "1px solid var(--v2-border-medium)", borderRadius: 10, overflow: "hidden" }}>
      {(["AND", "OR"] as CondBoolOp[]).map((op) => (
        <button key={op} onClick={() => onChange(op)}
          style={{ fontSize: 8.5, fontWeight: 700, letterSpacing: "0.06em", padding: "2px 8px", cursor: "pointer", border: "none",
            background: value === op ? "var(--v2-accent-deep)" : "transparent",
            color: value === op ? AND_OR_ACCENT : "var(--v2-text-muted)" }}>
          {op}
        </button>
      ))}
    </div>
  );
}

function GroupedLogicEditor({ logic, onChange, onDisable, known, crossRef, windowFrom, onOpenSchedule }: {
  logic: ConditionLogic;
  onChange: (v: ConditionLogic) => void;
  onDisable: () => void;
  known: string[];
  crossRef?: (name: string) => string | undefined;
  windowFrom?: string;
  onOpenSchedule: () => void;
}) {
  const setGroups = (groups: CondGroup[]) => onChange({ ...logic, groups });
  const patchGroup = (gi: number, patch: Partial<CondGroup>) => setGroups(logic.groups.map((g, i) => (i === gi ? { ...g, ...patch } : g)));
  const addGroup = () => setGroups([...logic.groups, { op: "AND", members: [] }]);
  const removeGroup = (gi: number) => setGroups(logic.groups.filter((_, i) => i !== gi));
  const addMember = (gi: number, name: string) => {
    const g = logic.groups[gi];
    if (!name || g.members.includes(name)) return;
    patchGroup(gi, { members: [...g.members, name] });
  };
  const removeMember = (gi: number, name: string) => patchGroup(gi, { members: logic.groups[gi].members.filter((m) => m !== name) });
  const setMemberRef = (gi: number, name: string, ref: DepDateRef) => {
    const next = withCondSuffix(splitCondSuffix(name).base, ref);
    const g = logic.groups[gi];
    if (next === name) return;
    patchGroup(gi, { members: g.members.includes(next) ? g.members.filter((m) => m !== name) : g.members.map((m) => (m === name ? next : m)) });
  };
  return (
    <div>
      <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 6 }}>
        <span style={{ fontSize: 10, fontWeight: 600, color: "var(--v2-text-secondary)" }}>In — AND/OR logic</span>
        {logic.groups.length > 1 && <OpToggle value={logic.op} onChange={(op) => onChange({ ...logic, op })} title="Operator BETWEEN the groups" />}
        <button onClick={onDisable} title="Back to the simple list (AND)"
          style={{ marginLeft: "auto", fontSize: 8.5, letterSpacing: "0.06em", fontWeight: 700, padding: "2px 7px", borderRadius: 10, cursor: "pointer",
            background: "var(--v2-accent-deep)", border: `1px solid ${AND_OR_ACCENT}`, color: AND_OR_ACCENT }}>
          AND/OR
        </button>
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
        {logic.groups.map((g, gi) => (
          <div key={gi}>
            {gi > 0 && (
              <div style={{ textAlign: "center", fontSize: 9, fontWeight: 700, letterSpacing: "0.08em", color: AND_OR_ACCENT, margin: "2px 0" }}>
                {logic.op}
              </div>
            )}
            <GroupBox index={gi} group={g} canRemove={logic.groups.length > 1} windowFrom={windowFrom} onOpenSchedule={onOpenSchedule}
              onSetOp={(op) => patchGroup(gi, { op })} onRemove={() => removeGroup(gi)}
              onAddMember={(n) => addMember(gi, n)} onRemoveMember={(n) => removeMember(gi, n)}
              onSetMemberRef={(n, ref) => setMemberRef(gi, n, ref)} known={known} crossRef={crossRef} />
          </div>
        ))}
      </div>
      <button onClick={addGroup} style={{ ...btnStyle, marginTop: 8, borderColor: AND_OR_ACCENT, color: AND_OR_ACCENT }}>＋ group</button>
    </div>
  );
}

function GroupBox({ index, group, canRemove, windowFrom, onOpenSchedule, onSetOp, onRemove, onAddMember, onRemoveMember, onSetMemberRef, known, crossRef }: {
  index: number;
  group: CondGroup;
  canRemove: boolean;
  windowFrom?: string;
  onOpenSchedule: () => void;
  onSetOp: (op: CondBoolOp) => void;
  onRemove: () => void;
  onAddMember: (name: string) => void;
  onRemoveMember: (name: string) => void;
  onSetMemberRef: (name: string, ref: DepDateRef) => void;
  known: string[];
  crossRef?: (name: string) => string | undefined;
}) {
  const [adding, setAdding] = useState(false);
  const [draft, setDraft] = useState("");
  const listId = `cond-grp-${index}`;
  const add = () => { const n = draft.trim(); setDraft(""); if (n) onAddMember(n); };
  const suggestions = known.filter((k) => !group.members.includes(k));
  const hasTime = group.members.includes(COND_TIME_TOKEN);
  return (
    <div style={{ border: "1px solid var(--v2-border-subtle)", borderRadius: 6, padding: "8px 10px", background: "var(--v2-bg-canvas)", display: "flex", flexDirection: "column", gap: 6 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <span style={{ fontSize: 9, letterSpacing: "0.06em", textTransform: "uppercase", color: "var(--v2-text-muted)", fontWeight: 700 }}>Group {index + 1}</span>
        {group.members.length > 1 && <OpToggle value={group.op} onChange={onSetOp} title="Operator INSIDE the group" />}
        {/* CL-2 — adicionar o token de horário ($TIME) ao grupo. Sempre visível
            (descobribilidade); sem windowFrom o clique abre a aba Horário, pois
            $TIME sem "A partir de" é satisfeito imediatamente. */}
        {!hasTime && (
          <button onClick={() => (windowFrom ? onAddMember(COND_TIME_TOKEN) : onOpenSchedule())}
            title={windowFrom
              ? `Add the scheduled time (from ${windowFrom}) as a member`
              : 'Needs "From" — click to open the Schedule tab'}
            style={{ fontSize: 8.5, fontWeight: 700, letterSpacing: "0.04em", padding: "2px 6px", borderRadius: 8, cursor: "pointer",
              background: "transparent", border: `1px ${windowFrom ? "solid" : "dashed"} var(--v2-border-medium)`,
              color: "var(--v2-text-muted)", opacity: windowFrom ? 1 : 0.65 }}>
            ＋ time
          </button>
        )}
        <button onClick={() => setAdding((v) => !v)} title={adding ? "Close" : "Add condition to the group"} style={{ ...addToggleStyle(adding), marginLeft: !hasTime ? 0 : "auto" }}>
          {adding ? <X size={12} /> : <Plus size={12} />}
        </button>
        {canRemove && <button onClick={onRemove} title="Remove group" style={iconBtn}><Trash2 size={12} /></button>}
      </div>
      {group.members.length === 0 && !adding && <Hint>Empty group — click ＋ to add.</Hint>}
      {group.members.map((n) => {
        // Token de horário ($TIME): membro especial, sem seletor de data. Sem
        // windowFrom (apagado depois) o token é satisfeito imediatamente — a
        // linha fica ÂMBAR avisando, senão o OR anula a condição em silêncio.
        if (n === COND_TIME_TOKEN) {
          const noWindow = !windowFrom;
          return (
            <div key={n} style={{ ...depRow, marginBottom: 0, borderColor: noWindow ? "#f59e0b" : `${AND_OR_ACCENT}` }}>
              <span style={{ flex: 1, fontSize: 12, color: noWindow ? "#f59e0b" : AND_OR_ACCENT, fontWeight: 600 }}>
                ⏱ time{noWindow ? " — no “From”: satisfied immediately" : ` (from ${windowFrom})`}
              </span>
              <button onClick={() => onRemoveMember(n)} style={iconBtn} title="Remove"><Trash2 size={12} /></button>
            </div>
          );
        }
        const { base, ref } = splitCondSuffix(n);
        const xref = crossRef?.(n);
        return (
          <div key={n} style={{ ...depRow, flexDirection: "column", alignItems: "stretch", gap: 2, marginBottom: 0 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
              <span style={{ flex: 1, fontSize: 12, fontFamily: "var(--v2-font-mono)" }}>{base}</span>
              <select value={ref} onChange={(e) => onSetMemberRef(n, e.target.value as DepDateRef)} title="Referenced daily (Stat = permanent)" style={{ ...selectStyle, width: 72, padding: "2px 4px", fontSize: 9 }}>
                <option value="odat">Odate</option>
                <option value="prev">Prev</option>
                <option value="stat">Stat</option>
              </select>
              <button onClick={() => onRemoveMember(n)} style={iconBtn} title="Remove"><Trash2 size={12} /></button>
            </div>
            {xref && <div style={{ fontSize: 9.5, color: "var(--v2-text-muted)" }}>{xref}</div>}
          </div>
        );
      })}
      {adding && (
        <div style={{ display: "flex", gap: 6 }}>
          <input autoFocus value={draft} list={listId} placeholder="condition… (e.g. FILE-ARRIVED)"
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); add(); } else if (e.key === "Escape") { setDraft(""); setAdding(false); } }}
            style={{ flex: 1, background: "var(--v2-bg-elevated)", border: "1px dashed var(--v2-border-medium)", color: "var(--v2-text-primary)", padding: "5px 8px", fontSize: 11, fontFamily: "var(--v2-font-mono)", borderRadius: 3, outline: "none", boxSizing: "border-box" }} />
          <datalist id={listId}>{suggestions.map((k) => <option key={k} value={k} />)}</datalist>
          <button onClick={add} disabled={!draft.trim()} style={{ ...btnStyle, borderColor: draft.trim() ? AND_OR_ACCENT : "var(--v2-border-medium)", color: draft.trim() ? AND_OR_ACCENT : "var(--v2-text-muted)" }}>＋</button>
        </div>
      )}
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
// HelpTopic — uma seção temática dentro de um popover "?" (título + corpo). Toda
// explicação sai do workspace e vem morar aqui, separada por tema.
function HelpTopic({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <div style={{ fontSize: 9.5, fontWeight: 700, letterSpacing: "0.04em", textTransform: "uppercase", color: "var(--v2-accent-brand)", marginBottom: 3 }}>{title}</div>
      <div>{children}</div>
    </div>
  );
}
function Input({ value, onChange, disabled, mono, placeholder }: { value: string; onChange: (v: string) => void; disabled?: boolean; mono?: boolean; placeholder?: string }) {
  return <input value={value} onChange={(e) => onChange(e.target.value)} disabled={disabled} placeholder={placeholder}
    style={{ width: "100%", background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)", color: disabled ? "var(--v2-text-muted)" : "var(--v2-text-primary)", padding: "5px 8px", fontSize: 11, fontFamily: mono ? "var(--v2-font-mono)" : "var(--v2-font-sans)", borderRadius: 3, outline: "none", boxSizing: "border-box" }} />;
}
const selectStyle: React.CSSProperties = { width: "100%", background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)", color: "var(--v2-text-primary)", padding: "5px 8px", fontSize: 11, fontFamily: "var(--v2-font-mono)", borderRadius: 3, outline: "none", boxSizing: "border-box" };
const btnStyle: React.CSSProperties = { padding: "4px 10px", background: "transparent", border: "1px solid var(--v2-border-medium)", borderRadius: 3, fontSize: 10, fontFamily: "var(--v2-font-mono)", letterSpacing: "0.06em", textTransform: "uppercase", cursor: "pointer" };
const iconBtn: React.CSSProperties = { background: "transparent", border: "none", color: "var(--v2-text-muted)", cursor: "pointer", padding: 2, display: "inline-flex" };
const depRow: React.CSSProperties = { display: "flex", alignItems: "center", gap: 8, padding: "6px 8px", background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-subtle)", borderRadius: 3, marginBottom: 4 };
