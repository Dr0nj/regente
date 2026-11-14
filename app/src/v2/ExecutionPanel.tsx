import { useEffect, useState } from "react";
import { api, isServerMode } from "@/lib/server-client";

type Attempt = { executionId: string; attempt: number; fence: number; agentId: string; state: string; acceptedAt: number; startedAt: number; lastContact: number; reason: string; resolvedAt: number; resolvedBy: string };
type Effect = { id: string; executionId: string; kind: string; state: string; reason: string; generation: number; resolvedAt: number; resolvedBy: string };
type Details = { attempts: Attempt[]; effects: Effect[] };
const when = (n: number) => n ? new Date(n).toLocaleString() : "—";

// O formulário de resolução nasceu sem estilo: a caixa de texto não tinha borda
// e o botão parecia texto solto — o operador não achava onde digitar nem onde
// clicar (relato de 14/11/2026, 4 UNCERTAIN presas na VPS). Tudo que é campo
// aqui tem borda, fundo e foco visíveis; o botão parece botão.
const FIELD: React.CSSProperties = { display: "block", width: "100%", boxSizing: "border-box", marginTop: 4, padding: "6px 8px", fontSize: 11, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-primary)", background: "var(--v2-bg-canvas)", border: "1px solid var(--v2-border-medium)", borderRadius: 4 };
const LABEL: React.CSSProperties = { display: "block", marginTop: 10, fontSize: 10.5, fontWeight: 600, color: "var(--v2-text-secondary)" };
const CHECK: React.CSSProperties = { display: "flex", alignItems: "flex-start", gap: 8, marginTop: 10, fontSize: 11, color: "var(--v2-text-primary)", cursor: "pointer" };
const DL: React.CSSProperties = { display: "grid", gridTemplateColumns: "max-content 1fr", gap: "4px 12px", margin: "6px 0", overflowWrap: "anywhere" };
const DT: React.CSSProperties = { color: "var(--v2-text-muted)", fontSize: 10.5 };
const DD: React.CSSProperties = { margin: 0, fontFamily: "var(--v2-font-mono)", color: "var(--v2-text-primary)" };
const H: React.CSSProperties = { margin: "0 0 6px", fontSize: 12, fontWeight: 700, color: "var(--v2-text-primary)" };

export default function ExecutionPanel({ instanceId }: { instanceId: string }) {
 const [snapshot, setSnapshot] = useState<{ id: string; details: Details } | null>(null);
 const [error, setError] = useState("");
 const [revision, setRevision] = useState(0);
 useEffect(() => {
  if (!isServerMode()) return;
  let alive = true;
  const load = async () => { try { const details = await api<Details>("/api/instances/" + encodeURIComponent(instanceId) + "/executions"); if (alive) { setSnapshot({ id: instanceId, details }); setError(""); } } catch (e) { if (alive) setError(e instanceof Error ? e.message : String(e)); } };
  void load(); const timer = window.setInterval(() => { void load(); }, 2000);
  return () => { alive = false; window.clearInterval(timer); };
 }, [instanceId, revision]);
 if (!isServerMode()) return <p>Durable execution details are available when connected to a server.</p>;
 const details = snapshot?.id === instanceId ? snapshot.details : null;
 return <section aria-label="Durable execution">
  {error && <p role="alert">{error}</p>}
  {!details && !error && <p>Loading execution details…</p>}
  {details?.attempts.length === 0 && <p>No durable attempt has been created.</p>}
  {details?.attempts.map(a => <article key={a.executionId} style={{ borderBottom: "1px solid var(--v2-border-subtle)", paddingBottom: 12, marginBottom: 12 }}>
   <h3 style={H}>Attempt {a.attempt} · {a.state}</h3>
   <dl style={DL}><dt style={DT}>Execution ID</dt><dd style={DD}>{a.executionId}</dd><dt style={DT}>Fence</dt><dd style={DD}>{a.fence}</dd><dt style={DT}>Agent</dt><dd style={DD}>{a.agentId}</dd><dt style={DT}>Accepted</dt><dd style={DD}>{when(a.acceptedAt)}</dd><dt style={DT}>Started</dt><dd style={DD}>{when(a.startedAt)}</dd><dt style={DT}>Last contact</dt><dd style={DD}>{when(a.lastContact)}</dd><dt style={DT}>Reason</dt><dd style={DD}>{a.reason || "—"}</dd></dl>
   {!!a.resolvedAt && <p>Resolved by {a.resolvedBy} at {when(a.resolvedAt)}</p>}
   {(a.state === "uncertain" || a.state === "cancel_requested") && <ResolutionForm id={a.executionId} fence={a.fence} onDone={() => setRevision(n => n + 1)} />}
  </article>)}
  {!!details?.effects.length && <h3 style={H}>Post-actions</h3>}
  {details?.effects.map(e => <article key={e.id} style={{ borderBottom: "1px solid var(--v2-border-subtle)", paddingBottom: 12, marginBottom: 12, overflowWrap: "anywhere" }}>
   <h4>{e.kind} · {e.state}</h4><p>{e.id}</p><p>{e.reason}</p>
   {!!e.resolvedAt && <p>Resolved by {e.resolvedBy} at {when(e.resolvedAt)}</p>}
   {(e.state === "uncertain" || e.state === "pending") && <ResolutionForm key={e.id + ":" + e.generation} id={e.id} generation={e.generation} effect onDone={() => setRevision(n => n + 1)} />}
  </article>)}
 </section>;
}
// Rascunho do formulário FORA do React, por execução. Relato de 14/11/2026 na VPS
// (diária de 28,8 mil jobs, eventos chegando o tempo todo): a caixa "verified
// stopped" desmarcava sozinha. Não reproduziu em laboratório parado, então a
// remontagem que zera o estado não foi localizada; o rascunho aqui sobrevive a
// QUALQUER remontagem do painel e só some quando a resolução é gravada.
type Draft = { decision: string; reason: string; stopped: boolean; classification: string; risk: boolean };
const drafts = new Map<string, Draft>();
function ResolutionForm({ id, fence, generation, effect = false, onDone }: { id: string; fence?: number; generation?: number; effect?: boolean; onDone: () => void }) {
 const draftKey = (effect ? "effect:" : "exec:") + id + ":" + (generation ?? fence ?? "");
 const saved = drafts.get(draftKey);
 const [decision, setDecision] = useState(saved?.decision ?? (effect ? "done" : "failed"));
 const [reason, setReason] = useState(saved?.reason ?? ""); const [stopped, setStopped] = useState(saved?.stopped ?? false);
 const [classification, setClassification] = useState(saved?.classification ?? "verified-absent"); const [risk, setRisk] = useState(saved?.risk ?? false);
 useEffect(() => { drafts.set(draftKey, { decision, reason, stopped, classification, risk }); }, [draftKey, decision, reason, stopped, classification, risk]);
 const [key, setKey] = useState(() => crypto.randomUUID()); const [saving, setSaving] = useState(false); const [error, setError] = useState("");
 const changed = () => { setKey(crypto.randomUUID()); setError(""); };
 const submit = async (event: React.FormEvent) => {
  event.preventDefault(); setSaving(true); setError("");
  try { await api("/api/" + (effect ? "execution-effects" : "executions") + "/" + encodeURIComponent(id) + "/resolve", { method: "POST", body: JSON.stringify({ idempotencyKey: key, decision, reason, effectStopped: stopped, ...(effect ? { generation, classification, duplicateRiskAccepted: risk } : { fence }) }) }); drafts.delete(draftKey); onDone(); }
  catch (e) { setError(e instanceof Error ? e.message : String(e)); } finally { setSaving(false); }
 };
 return <form onSubmit={e => { void submit(e); }} aria-label={effect ? "Resolve post-action" : "Resolve execution"} style={{ marginTop: 10, padding: 12, border: "1px solid var(--v2-border-medium)", borderRadius: 6, background: "var(--v2-bg-elevated)" }}>
  <p style={{ margin: 0, color: "var(--v2-text-secondary)" }}>The outcome is unconfirmed. Verify the external operation before recording a decision. This decision is audited.</p>
  <label style={LABEL}>Verified outcome <select style={FIELD} value={decision} disabled={saving} onChange={e => { setDecision(e.target.value); changed(); }}>{(effect ? ["done", "cancelled", "retry"] : ["failed", "succeeded", "cancelled"]).map(v => <option key={v} value={v}>{v}</option>)}</select></label>
  <label style={LABEL}>Evidence and reason <textarea style={{ ...FIELD, minHeight: 64, resize: "vertical" }} placeholder="What you checked and what you found" required maxLength={2000} value={reason} disabled={saving} onChange={e => { setReason(e.target.value); changed(); }} /></label>
  <label style={CHECK}><input type="checkbox" style={{ marginTop: 2 }} checked={stopped} disabled={saving} onChange={e => { setStopped(e.target.checked); changed(); }} />I verified that the external operation has stopped.</label>
  {effect && decision === "retry" && <>
   <label style={LABEL}>Repeat classification <select style={FIELD} value={classification} disabled={saving} onChange={e => { setClassification(e.target.value); changed(); }}><option value="verified-absent">External effect verified absent</option><option value="idempotent">Destination guarantees idempotency</option><option value="duplicate-risk-accepted">Duplicate risk accepted</option></select></label>
   <label style={CHECK}><input type="checkbox" style={{ marginTop: 2 }} checked={risk} disabled={saving} onChange={e => { setRisk(e.target.checked); changed(); }} />I accept the risk of duplicate external effects.</label>
  </>}
  {error && <p role="alert" style={{ color: "var(--v2-status-failed)", margin: "8px 0 0" }}>{error}</p>}
  <button className="v2-btn v2-btn-primary" style={{ marginTop: 12 }} disabled={saving || !reason.trim() || !stopped || (effect && decision === "retry" && !risk)} type="submit">{saving ? "Recording…" : "Record verified resolution"}</button>
 </form>;
}
