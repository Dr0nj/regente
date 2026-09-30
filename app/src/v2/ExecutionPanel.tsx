import { useEffect, useState } from "react";
import { api, isServerMode } from "@/lib/server-client";

type Attempt = { executionId: string; attempt: number; fence: number; agentId: string; state: string; acceptedAt: number; startedAt: number; lastContact: number; reason: string; resolvedAt: number; resolvedBy: string };
type Effect = { id: string; executionId: string; kind: string; state: string; reason: string; generation: number; resolvedAt: number; resolvedBy: string };
type Details = { attempts: Attempt[]; effects: Effect[] };
const when = (n: number) => n ? new Date(n).toLocaleString() : "—";

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
   <h3>Attempt {a.attempt} · {a.state}</h3>
   <dl style={{ overflowWrap: "anywhere" }}><dt>Execution ID</dt><dd>{a.executionId}</dd><dt>Fence</dt><dd>{a.fence}</dd><dt>Agent</dt><dd>{a.agentId}</dd><dt>Accepted</dt><dd>{when(a.acceptedAt)}</dd><dt>Started</dt><dd>{when(a.startedAt)}</dd><dt>Last contact</dt><dd>{when(a.lastContact)}</dd><dt>Reason</dt><dd>{a.reason || "—"}</dd></dl>
   {!!a.resolvedAt && <p>Resolved by {a.resolvedBy} at {when(a.resolvedAt)}</p>}
   {(a.state === "uncertain" || a.state === "cancel_requested") && <ResolutionForm id={a.executionId} fence={a.fence} onDone={() => setRevision(n => n + 1)} />}
  </article>)}
  {!!details?.effects.length && <h3>Post-actions</h3>}
  {details?.effects.map(e => <article key={e.id} style={{ borderBottom: "1px solid var(--v2-border-subtle)", paddingBottom: 12, marginBottom: 12, overflowWrap: "anywhere" }}>
   <h4>{e.kind} · {e.state}</h4><p>{e.id}</p><p>{e.reason}</p>
   {!!e.resolvedAt && <p>Resolved by {e.resolvedBy} at {when(e.resolvedAt)}</p>}
   {(e.state === "uncertain" || e.state === "pending") && <ResolutionForm key={e.id + ":" + e.generation} id={e.id} generation={e.generation} effect onDone={() => setRevision(n => n + 1)} />}
  </article>)}
 </section>;
}
function ResolutionForm({ id, fence, generation, effect = false, onDone }: { id: string; fence?: number; generation?: number; effect?: boolean; onDone: () => void }) {
 const [decision, setDecision] = useState(effect ? "done" : "failed");
 const [reason, setReason] = useState(""); const [stopped, setStopped] = useState(false);
 const [classification, setClassification] = useState("verified-absent"); const [risk, setRisk] = useState(false);
 const [key, setKey] = useState(() => crypto.randomUUID()); const [saving, setSaving] = useState(false); const [error, setError] = useState("");
 const changed = () => { setKey(crypto.randomUUID()); setError(""); };
 const submit = async (event: React.FormEvent) => {
  event.preventDefault(); setSaving(true); setError("");
  try { await api("/api/" + (effect ? "execution-effects" : "executions") + "/" + encodeURIComponent(id) + "/resolve", { method: "POST", body: JSON.stringify({ idempotencyKey: key, decision, reason, effectStopped: stopped, ...(effect ? { generation, classification, duplicateRiskAccepted: risk } : { fence }) }) }); onDone(); }
  catch (e) { setError(e instanceof Error ? e.message : String(e)); } finally { setSaving(false); }
 };
 return <form onSubmit={e => { void submit(e); }} aria-label={effect ? "Resolve post-action" : "Resolve execution"}>
  <p>The outcome is unconfirmed. Verify the external operation before recording a decision. This decision is audited.</p>
  <label>Verified outcome <select value={decision} disabled={saving} onChange={e => { setDecision(e.target.value); changed(); }}>{(effect ? ["done", "cancelled", "retry"] : ["failed", "succeeded", "cancelled"]).map(v => <option key={v} value={v}>{v}</option>)}</select></label>
  <label style={{ display: "block" }}>Evidence and reason <textarea required maxLength={2000} value={reason} disabled={saving} onChange={e => { setReason(e.target.value); changed(); }} /></label>
  <label style={{ display: "block" }}><input type="checkbox" checked={stopped} disabled={saving} onChange={e => { setStopped(e.target.checked); changed(); }} />I verified that the external operation has stopped.</label>
  {effect && decision === "retry" && <>
   <label>Repeat classification <select value={classification} disabled={saving} onChange={e => { setClassification(e.target.value); changed(); }}><option value="verified-absent">External effect verified absent</option><option value="idempotent">Destination guarantees idempotency</option><option value="duplicate-risk-accepted">Duplicate risk accepted</option></select></label>
   <label style={{ display: "block" }}><input type="checkbox" checked={risk} disabled={saving} onChange={e => { setRisk(e.target.checked); changed(); }} />I accept the risk of duplicate external effects.</label>
  </>}
  {error && <p role="alert">{error}</p>}
  <button disabled={saving || !reason.trim() || !stopped || (effect && decision === "retry" && !risk)} type="submit">{saving ? "Recording…" : "Record verified resolution"}</button>
 </form>;
}
