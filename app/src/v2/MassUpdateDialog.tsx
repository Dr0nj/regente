/**
 * MassUpdateDialog — CTM-3: Find & Update RICO no Design (2026-07-06).
 *
 * Critério (folder/jobType/regex em campo/campo vazio, e/ou seleção do canvas)
 * → operação (set-field/find-replace/add-action/add-upstream/variável/condition)
 * → PREVIEW (diff antes→depois por job) → Aplicar (transacional por item)
 * → Desfazer (undo por session, pilha no server).
 */
import { useCallback, useMemo, useState } from "react";
import {
  massUpdateSession, massUpdateUndo,
  type MassCriteria, type MassOperation, type MassUpdateResult,
} from "@/lib/design-session-api";
import type { JobDefinition } from "@/lib/orchestrator-model";
import { toast } from "./Toast";

const FIELD_OPTIONS = [
  { v: "label", t: "Label" },
  { v: "description", t: "Descrição" },
  { v: "schedule.runAt", t: "Run At (HH:MM)" },
  { v: "schedule.windowFrom", t: "Janela de (HH:MM)" },
  { v: "schedule.windowTo", t: "Janela até (HH:MM)" },
  { v: "schedule.enabled", t: "Habilitado (true/false)" },
  { v: "retries", t: "Retries (int)" },
  { v: "timeout", t: "Timeout s (int)" },
  { v: "agentId", t: "Agent ID" },
  { v: "calendar", t: "Calendar" },
  { v: "environment", t: "Environment" },
  { v: "dryRun", t: "Dry Run (true/false)" },
  { v: "confirm", t: "Confirm (true/false)" },
];

const CRIT_FIELDS = [
  { v: "id", t: "ID" },
  { v: "label", t: "Label" },
  { v: "jobType", t: "Job Type" },
  { v: "description", t: "Descrição" },
  { v: "schedule.runAt", t: "Run At" },
  { v: "agentId", t: "Agent ID" },
  { v: "calendar", t: "Calendar" },
  { v: "environment", t: "Environment" },
];

const inputStyle: React.CSSProperties = {
  background: "var(--v2-bg-elevated)",
  border: "1px solid var(--v2-border-medium)",
  color: "var(--v2-text-primary)",
  borderRadius: 4, padding: "5px 8px", fontSize: 11,
  fontFamily: "var(--v2-font-mono)", minWidth: 0,
};

const labelStyle: React.CSSProperties = {
  fontSize: 9, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-muted)",
  letterSpacing: "0.08em", textTransform: "uppercase", marginBottom: 3, display: "block",
};

function Field({ label, children, grow }: { label: string; children: React.ReactNode; grow?: boolean }) {
  return (
    <div style={{ flex: grow ? 1 : undefined, minWidth: 0 }}>
      <span style={labelStyle}>{label}</span>
      {children}
    </div>
  );
}

export default function MassUpdateDialog({
  sessionId,
  folders,
  defs,
  presetIds,
  onClose,
  onChanged,
}: {
  sessionId: string;
  folders: string[];
  defs: JobDefinition[];
  /** Seleção do canvas (opcional): restringe o critério a esses ids. */
  presetIds?: string[];
  onClose: () => void;
  onChanged: () => Promise<void> | void;
}) {
  // Critério
  const [useSelection, setUseSelection] = useState<boolean>((presetIds?.length ?? 0) > 0);
  const [critFolder, setCritFolder] = useState("");
  const [critJobType, setCritJobType] = useState("");
  const [critField, setCritField] = useState("id");
  const [critRegex, setCritRegex] = useState("");
  const [critEmpty, setCritEmpty] = useState("");
  // Operação
  const [op, setOp] = useState<MassOperation["op"]>("set-field");
  const [opField, setOpField] = useState("description");
  const [opValue, setOpValue] = useState("");
  const [onlyIfEmpty, setOnlyIfEmpty] = useState(true);
  const [find, setFind] = useState("");
  const [replace, setReplace] = useState("");
  const [upFrom, setUpFrom] = useState("");
  const [upCond, setUpCond] = useState("on-success");
  const [varKey, setVarKey] = useState("");
  const [varVal, setVarVal] = useState("");
  const [actOn, setActOn] = useState("result");
  const [actStatus, setActStatus] = useState("NOTOK");
  const [actDo, setActDo] = useState("notify");
  const [actMsg, setActMsg] = useState("");
  const [actSeverity, setActSeverity] = useState("warning");
  const [actTarget, setActTarget] = useState("");
  const [actCondition, setActCondition] = useState("");
  // Resultado
  const [preview, setPreview] = useState<MassUpdateResult | null>(null);
  const [busy, setBusy] = useState(false);
  const [undoDepth, setUndoDepth] = useState(0);

  const jobTypes = useMemo(
    () => [...new Set(defs.map((d) => d.jobType))].sort(),
    [defs],
  );

  const criteria = useMemo<MassCriteria>(() => {
    const c: MassCriteria = {};
    if (useSelection && presetIds && presetIds.length > 0) c.ids = presetIds;
    if (critFolder) c.folders = [critFolder];
    if (critJobType) c.jobType = critJobType;
    if (critRegex) { c.field = critField; c.regex = critRegex; }
    if (critEmpty) c.fieldEmpty = critEmpty;
    return c;
  }, [useSelection, presetIds, critFolder, critJobType, critField, critRegex, critEmpty]);

  const operation = useMemo<MassOperation>(() => {
    switch (op) {
      case "set-field": {
        // Coerção de tipo pelo campo: int/bool viajam tipados no JSON.
        let value: unknown = opValue;
        if (opField === "retries" || opField === "timeout") value = Number(opValue);
        if (opField === "schedule.enabled" || opField === "dryRun" || opField === "confirm") value = opValue.trim().toLowerCase() === "true";
        return { op, field: opField, value, onlyIfEmpty };
      }
      case "find-replace":
        return { op, field: opField, find, replace };
      case "add-action":
        return {
          op,
          action: {
            on: actOn,
            ...(actOn === "result" ? { status: actStatus } : {}),
            do: actDo,
            ...(actDo === "notify" ? { message: actMsg, severity: actSeverity } : {}),
            ...(actDo === "run-job" ? { targetJob: actTarget } : {}),
            ...(actDo === "set-condition" ? { condition: actCondition } : {}),
          },
        };
      case "remove-action":
        return { op, actionMatch: { ...(actDo ? { do: actDo } : {}) } };
      case "add-upstream":
        return { op, upstream: { from: upFrom, condition: upCond } };
      case "remove-upstream":
        return { op, upstream: { from: upFrom } };
      case "set-variable":
        return { op, key: varKey, val: varVal };
      case "remove-variable":
        return { op, key: varKey };
      case "add-condition-in":
      case "remove-condition-in":
        return { op, key: varKey };
    }
  }, [op, opField, opValue, onlyIfEmpty, find, replace, upFrom, upCond, varKey, varVal,
      actOn, actStatus, actDo, actMsg, actSeverity, actTarget, actCondition]);

  const run = useCallback(async (apply: boolean) => {
    setBusy(true);
    try {
      const res = await massUpdateSession(sessionId, criteria, operation, apply);
      setPreview(res);
      setUndoDepth(res.undoDepth);
      if (apply) {
        const okCount = res.items.filter((i) => i.ok).length;
        const failCount = res.items.filter((i) => !i.ok).length;
        if (failCount > 0) {
          toast.error(`Mass update: ${failCount} de ${okCount + failCount} falharam`, {
            detail: res.items.find((i) => i.error)?.error,
          });
        } else if (okCount === 0) {
          toast.info("Nenhum job precisou mudar", { detail: `${res.matched} casaram o critério, todos já estavam como pedido.` });
        } else {
          toast.success(`Mass update aplicado em ${okCount} job(s)`, { detail: "Desfazer disponível no diálogo." });
        }
        await onChanged();
      }
    } catch (e) {
      toast.error(apply ? "Mass update falhou" : "Preview falhou", { detail: e instanceof Error ? e.message : String(e) });
    } finally {
      setBusy(false);
    }
  }, [sessionId, criteria, operation, onChanged]);

  const undo = useCallback(async () => {
    setBusy(true);
    try {
      const res = await massUpdateUndo(sessionId);
      setUndoDepth(res.undoDepth);
      setPreview(null);
      toast.success(`Desfeito: ${res.label} (${res.ok} job(s) restaurados)`);
      await onChanged();
    } catch (e) {
      toast.error("Undo falhou", { detail: e instanceof Error ? e.message : String(e) });
    } finally {
      setBusy(false);
    }
  }, [sessionId, onChanged]);

  return (
    <div style={{
      position: "fixed", inset: 0, zIndex: 90, background: "rgba(0,0,0,0.6)",
      display: "flex", alignItems: "center", justifyContent: "center",
    }} onMouseDown={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div className="v2-grain-card v2-neon-card" style={{
        width: "min(880px, 94vw)", maxHeight: "88vh", display: "flex", flexDirection: "column",
        background: "var(--v2-bg-surface)", overflow: "hidden",
      }}>
        {/* Header */}
        <div style={{
          display: "flex", alignItems: "center", padding: "12px 16px",
          borderBottom: "1px solid var(--v2-border-subtle)",
        }}>
          <div>
            <div style={{ fontSize: 13, fontWeight: 700, color: "var(--v2-text-primary)" }}>Find &amp; Update — atualização em massa</div>
            <div style={{ fontSize: 10, color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)" }}>
              critério → preview → aplicar (por item) → desfazer
            </div>
          </div>
          <div style={{ flex: 1 }} />
          {undoDepth > 0 && (
            <button className="v2-btn" disabled={busy} onClick={() => void undo()} style={{ marginRight: 8, color: "#f5b50a", borderColor: "#5a4a1a" }}>
              ↩ Desfazer última ({undoDepth})
            </button>
          )}
          <button className="v2-dialog-x" onClick={onClose}>✕</button>
        </div>

        <div style={{ padding: "12px 16px", overflowY: "auto", display: "flex", flexDirection: "column", gap: 14 }}>
          {/* ── Critério ── */}
          <div>
            <div style={{ ...labelStyle, color: "var(--v2-accent-brand)" }}>1 · Quais jobs (critério)</div>
            <div style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end" }}>
              {(presetIds?.length ?? 0) > 0 && (
                <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 11, color: "var(--v2-text-secondary)" }}>
                  <input type="checkbox" checked={useSelection} onChange={(e) => setUseSelection(e.target.checked)} />
                  só a seleção do canvas ({presetIds!.length})
                </label>
              )}
              <Field label="Folder">
                <select style={inputStyle} value={critFolder} onChange={(e) => setCritFolder(e.target.value)}>
                  <option value="">todas abertas</option>
                  {folders.map((f) => <option key={f} value={f}>{f}</option>)}
                </select>
              </Field>
              <Field label="Job Type">
                <select style={inputStyle} value={critJobType} onChange={(e) => setCritJobType(e.target.value)}>
                  <option value="">qualquer</option>
                  {jobTypes.map((t) => <option key={t} value={t}>{t}</option>)}
                </select>
              </Field>
              <Field label="Campo p/ regex">
                <select style={inputStyle} value={critField} onChange={(e) => setCritField(e.target.value)}>
                  {CRIT_FIELDS.map((f) => <option key={f.v} value={f.v}>{f.t}</option>)}
                </select>
              </Field>
              <Field label="Regex (vazio = ignora)" grow>
                <input style={{ ...inputStyle, width: "100%" }} placeholder="^etl-.*-prod$" value={critRegex} onChange={(e) => setCritRegex(e.target.value)} />
              </Field>
              <Field label="Campo VAZIO">
                <select style={inputStyle} value={critEmpty} onChange={(e) => setCritEmpty(e.target.value)}>
                  <option value="">—</option>
                  {CRIT_FIELDS.map((f) => <option key={f.v} value={f.v}>{f.t}</option>)}
                </select>
              </Field>
            </div>
          </div>

          {/* ── Operação ── */}
          <div>
            <div style={{ ...labelStyle, color: "var(--v2-accent-brand)" }}>2 · O que fazer (operação)</div>
            <div style={{ display: "flex", gap: 10, flexWrap: "wrap", alignItems: "flex-end" }}>
              <Field label="Operação">
                <select style={inputStyle} value={op} onChange={(e) => { setOp(e.target.value as MassOperation["op"]); setPreview(null); }}>
                  <option value="set-field">Setar campo</option>
                  <option value="find-replace">Find &amp; Replace (regex)</option>
                  <option value="add-action">Adicionar action (On/Do)</option>
                  <option value="remove-action">Remover actions (por Do)</option>
                  <option value="add-upstream">Adicionar dependência</option>
                  <option value="remove-upstream">Remover dependência</option>
                  <option value="set-variable">Setar variável do job</option>
                  <option value="remove-variable">Remover variável do job</option>
                  <option value="add-condition-in">Adicionar condition IN</option>
                  <option value="remove-condition-in">Remover condition IN</option>
                </select>
              </Field>

              {op === "set-field" && (
                <>
                  <Field label="Campo">
                    <select style={inputStyle} value={opField} onChange={(e) => setOpField(e.target.value)}>
                      {FIELD_OPTIONS.map((f) => <option key={f.v} value={f.v}>{f.t}</option>)}
                    </select>
                  </Field>
                  <Field label="Valor" grow>
                    <input style={{ ...inputStyle, width: "100%" }} value={opValue} onChange={(e) => setOpValue(e.target.value)} />
                  </Field>
                  <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 11, color: "var(--v2-text-secondary)" }}>
                    <input type="checkbox" checked={onlyIfEmpty} onChange={(e) => setOnlyIfEmpty(e.target.checked)} />
                    só se vazio
                  </label>
                </>
              )}

              {op === "find-replace" && (
                <>
                  <Field label="Campo">
                    <select style={inputStyle} value={opField} onChange={(e) => setOpField(e.target.value)}>
                      {FIELD_OPTIONS.filter((f) => !["retries", "timeout", "schedule.enabled", "dryRun", "confirm"].includes(f.v)).map((f) => (
                        <option key={f.v} value={f.v}>{f.t}</option>
                      ))}
                      <option value="params">Params (todos os valores string)</option>
                    </select>
                  </Field>
                  <Field label="Find (regex)" grow>
                    <input style={{ ...inputStyle, width: "100%" }} placeholder="legacy-host" value={find} onChange={(e) => setFind(e.target.value)} />
                  </Field>
                  <Field label="Replace ($1 p/ grupos)" grow>
                    <input style={{ ...inputStyle, width: "100%" }} placeholder="new-host" value={replace} onChange={(e) => setReplace(e.target.value)} />
                  </Field>
                </>
              )}

              {op === "add-action" && (
                <>
                  <Field label="On">
                    <select style={inputStyle} value={actOn} onChange={(e) => setActOn(e.target.value)}>
                      <option value="result">result</option>
                      <option value="attempt">attempt</option>
                      <option value="runtime">runtime</option>
                    </select>
                  </Field>
                  {actOn === "result" && (
                    <Field label="Status">
                      <select style={inputStyle} value={actStatus} onChange={(e) => setActStatus(e.target.value)}>
                        <option value="NOTOK">NOTOK</option>
                        <option value="OK">OK</option>
                      </select>
                    </Field>
                  )}
                  <Field label="Do">
                    <select style={inputStyle} value={actDo} onChange={(e) => setActDo(e.target.value)}>
                      <option value="notify">notify</option>
                      <option value="set-condition">set-condition</option>
                      <option value="run-job">run-job</option>
                      <option value="set-ok">set-ok</option>
                    </select>
                  </Field>
                  {actDo === "notify" && (
                    <>
                      <Field label="Mensagem" grow>
                        <input style={{ ...inputStyle, width: "100%" }} value={actMsg} onChange={(e) => setActMsg(e.target.value)} />
                      </Field>
                      <Field label="Severidade">
                        <select style={inputStyle} value={actSeverity} onChange={(e) => setActSeverity(e.target.value)}>
                          <option value="info">info</option>
                          <option value="warning">warning</option>
                          <option value="critical">critical</option>
                        </select>
                      </Field>
                    </>
                  )}
                  {actDo === "run-job" && (
                    <Field label="Target job" grow>
                      <input style={{ ...inputStyle, width: "100%" }} value={actTarget} onChange={(e) => setActTarget(e.target.value)} />
                    </Field>
                  )}
                  {actDo === "set-condition" && (
                    <Field label="Condition" grow>
                      <input style={{ ...inputStyle, width: "100%" }} value={actCondition} onChange={(e) => setActCondition(e.target.value)} />
                    </Field>
                  )}
                </>
              )}

              {op === "remove-action" && (
                <Field label="Do (das actions a remover)">
                  <select style={inputStyle} value={actDo} onChange={(e) => setActDo(e.target.value)}>
                    <option value="notify">notify</option>
                    <option value="set-condition">set-condition</option>
                    <option value="run-job">run-job</option>
                    <option value="set-ok">set-ok</option>
                  </select>
                </Field>
              )}

              {(op === "add-upstream" || op === "remove-upstream") && (
                <>
                  <Field label="Upstream (job pai)" grow>
                    <select style={{ ...inputStyle, width: "100%" }} value={upFrom} onChange={(e) => setUpFrom(e.target.value)}>
                      <option value="">— escolha —</option>
                      {defs.map((d) => <option key={d.id} value={d.id}>{d.label} ({d.id})</option>)}
                    </select>
                  </Field>
                  {op === "add-upstream" && (
                    <Field label="Condição">
                      <select style={inputStyle} value={upCond} onChange={(e) => setUpCond(e.target.value)}>
                        <option value="on-success">on-success</option>
                        <option value="on-failure">on-failure</option>
                        <option value="on-complete">on-complete</option>
                      </select>
                    </Field>
                  )}
                </>
              )}

              {(op === "set-variable" || op === "remove-variable" || op === "add-condition-in" || op === "remove-condition-in") && (
                <>
                  <Field label={op.includes("condition") ? "Condition" : "Variável"} grow>
                    <input style={{ ...inputStyle, width: "100%" }} placeholder={op.includes("condition") ? "carga-fin-ok" : "ENV"} value={varKey} onChange={(e) => setVarKey(e.target.value)} />
                  </Field>
                  {op === "set-variable" && (
                    <Field label="Valor" grow>
                      <input style={{ ...inputStyle, width: "100%" }} value={varVal} onChange={(e) => setVarVal(e.target.value)} />
                    </Field>
                  )}
                </>
              )}
            </div>
          </div>

          {/* ── Preview ── */}
          {preview && (
            <div>
              <div style={{ ...labelStyle, color: "var(--v2-accent-brand)" }}>
                3 · {preview.applied ? "Resultado" : "Preview"} — {preview.matched} casaram o critério, {preview.items.length} mudariam
              </div>
              {preview.items.length === 0 ? (
                <div style={{ fontSize: 11, color: "var(--v2-text-muted)", padding: "8px 0" }}>
                  Nenhuma mudança: ou nada casou o critério, ou todos já estão como pedido.
                </div>
              ) : (
                <div style={{ border: "1px solid var(--v2-border-subtle)", borderRadius: 4, maxHeight: 240, overflowY: "auto" }}>
                  {preview.items.map((it) => (
                    <div key={it.id} style={{
                      display: "flex", gap: 10, padding: "6px 10px", alignItems: "baseline",
                      borderBottom: "1px solid var(--v2-border-subtle)", fontSize: 11,
                    }}>
                      <span style={{ fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-primary)", minWidth: 160, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }} title={it.id}>
                        {it.label || it.id}
                      </span>
                      <span style={{ fontSize: 9, color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)" }}>{it.team}</span>
                      <div style={{ flex: 1, minWidth: 0 }}>
                        {it.error ? (
                          <span style={{ color: "var(--v2-status-failed)" }}>✗ {it.error}</span>
                        ) : (
                          (it.changes ?? []).map((c, i) => (
                            <div key={i} style={{ fontFamily: "var(--v2-font-mono)", fontSize: 10, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }} title={`${c.field}: ${c.before} → ${c.after}`}>
                              <span style={{ color: "var(--v2-text-muted)" }}>{c.field}: </span>
                              <span style={{ color: "var(--v2-status-failed)", textDecoration: "line-through" }}>{c.before || "∅"}</span>
                              <span style={{ color: "var(--v2-text-muted)" }}> → </span>
                              <span style={{ color: "var(--v2-status-ok)" }}>{c.after || "∅"}</span>
                            </div>
                          ))
                        )}
                      </div>
                      {preview.applied && it.ok && <span style={{ color: "var(--v2-status-ok)", fontSize: 10 }}>✓</span>}
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </div>

        {/* Footer */}
        <div style={{
          display: "flex", gap: 8, padding: "10px 16px",
          borderTop: "1px solid var(--v2-border-subtle)", alignItems: "center",
        }}>
          <span style={{ fontSize: 10, color: "var(--v2-text-muted)", fontFamily: "var(--v2-font-mono)" }}>
            transacional por item · undo por session (até 10 níveis, até publicar)
          </span>
          <div style={{ flex: 1 }} />
          <button className="v2-btn" disabled={busy} onClick={() => void run(false)}>Preview</button>
          <button
            className="v2-btn v2-btn-primary"
            disabled={busy || !preview || preview.applied || preview.items.filter((i) => !i.error).length === 0}
            title={!preview ? "Rode o Preview primeiro" : undefined}
            onClick={() => void run(true)}
          >
            Aplicar {preview && !preview.applied ? `em ${preview.items.filter((i) => !i.error).length}` : ""}
          </button>
        </div>
      </div>
    </div>
  );
}
