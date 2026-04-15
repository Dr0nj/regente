/**
 * Persistence Layer — Phase 11
 *
 * Dual-mode storage abstraction: Supabase when configured, localStorage as fallback.
 * Provides a unified interface for metrics, audit, and alerting data.
 */

import { supabase, isSupabaseConfigured } from "@/lib/supabase";

/* ── Generic helpers ── */

/** Load an array from localStorage by key */
export function localLoad<T>(key: string): T[] {
  try {
    const raw = localStorage.getItem(key);
    return raw ? JSON.parse(raw) : [];
  } catch {
    return [];
  }
}

/** Save an array to localStorage, trimming to max entries */
export function localSave<T>(key: string, entries: T[], max: number): void {
  const trimmed = entries.slice(-max);
  localStorage.setItem(key, JSON.stringify(trimmed));
}

/** Whether to route to Supabase */
export function useSupabase(): boolean {
  return isSupabaseConfigured;
}

/* ── Metrics persistence ── */

const JOB_METRICS_KEY = "regente:metrics:jobs";
const WORKFLOW_METRICS_KEY = "regente:metrics:workflows";
const METRICS_MAX = 500;

export interface JobMetricRow {
  node_id: string;
  node_name: string;
  workflow_id: string;
  duration_ms: number;
  attempts: number;
  status: string;
  created_at?: string;
}

export interface WorkflowMetricRow {
  workflow_id: string;
  workflow_name: string;
  duration_ms: number;
  status: string;
  jobs_total: number;
  jobs_succeeded: number;
  jobs_failed: number;
  created_at?: string;
}

interface LocalJobMetric {
  nodeId: string;
  nodeName: string;
  workflowId: string;
  timestamp: number;
  durationMs: number;
  attempts: number;
  status: string;
}

interface LocalWorkflowMetric {
  workflowId: string;
  workflowName: string;
  timestamp: number;
  durationMs: number;
  status: string;
  jobsTotal: number;
  jobsSucceeded: number;
  jobsFailed: number;
}

export async function insertJobMetric(entry: LocalJobMetric): Promise<void> {
  if (useSupabase()) {
    await (supabase.from as Function)("execution_metrics_jobs").insert({
      node_id: entry.nodeId,
      node_name: entry.nodeName,
      workflow_id: entry.workflowId,
      duration_ms: entry.durationMs,
      attempts: entry.attempts,
      status: entry.status,
    });
    return;
  }
  const entries = localLoad<LocalJobMetric>(JOB_METRICS_KEY);
  entries.push(entry);
  localSave(JOB_METRICS_KEY, entries, METRICS_MAX);
}

export async function insertWorkflowMetric(entry: LocalWorkflowMetric): Promise<void> {
  if (useSupabase()) {
    await (supabase.from as Function)("execution_metrics_workflows").insert({
      workflow_id: entry.workflowId,
      workflow_name: entry.workflowName,
      duration_ms: entry.durationMs,
      status: entry.status,
      jobs_total: entry.jobsTotal,
      jobs_succeeded: entry.jobsSucceeded,
      jobs_failed: entry.jobsFailed,
    });
    return;
  }
  const entries = localLoad<LocalWorkflowMetric>(WORKFLOW_METRICS_KEY);
  entries.push(entry);
  localSave(WORKFLOW_METRICS_KEY, entries, METRICS_MAX);
}

export async function loadJobMetrics(nodeId?: string): Promise<LocalJobMetric[]> {
  if (useSupabase()) {
    let query = (supabase.from as Function)("execution_metrics_jobs")
      .select("*")
      .order("created_at", { ascending: true })
      .limit(METRICS_MAX);
    if (nodeId) query = query.eq("node_id", nodeId);
    const { data } = await query;
    return (data ?? []).map((r: JobMetricRow) => ({
      nodeId: r.node_id,
      nodeName: r.node_name,
      workflowId: r.workflow_id,
      timestamp: new Date(r.created_at!).getTime(),
      durationMs: r.duration_ms,
      attempts: r.attempts,
      status: r.status,
    }));
  }
  const entries = localLoad<LocalJobMetric>(JOB_METRICS_KEY);
  return nodeId ? entries.filter((e) => e.nodeId === nodeId) : entries;
}

export async function loadWorkflowMetrics(workflowId?: string): Promise<LocalWorkflowMetric[]> {
  if (useSupabase()) {
    let query = (supabase.from as Function)("execution_metrics_workflows")
      .select("*")
      .order("created_at", { ascending: true })
      .limit(METRICS_MAX);
    if (workflowId) query = query.eq("workflow_id", workflowId);
    const { data } = await query;
    return (data ?? []).map((r: WorkflowMetricRow) => ({
      workflowId: r.workflow_id,
      workflowName: r.workflow_name,
      timestamp: new Date(r.created_at!).getTime(),
      durationMs: r.duration_ms,
      status: r.status,
      jobsTotal: r.jobs_total,
      jobsSucceeded: r.jobs_succeeded,
      jobsFailed: r.jobs_failed,
    }));
  }
  const entries = localLoad<LocalWorkflowMetric>(WORKFLOW_METRICS_KEY);
  return workflowId ? entries.filter((e) => e.workflowId === workflowId) : entries;
}

export async function clearMetrics(): Promise<void> {
  if (useSupabase()) {
    await (supabase.from as Function)("execution_metrics_jobs").delete().neq("node_id", "");
    await (supabase.from as Function)("execution_metrics_workflows").delete().neq("workflow_id", "");
    return;
  }
  localStorage.removeItem(JOB_METRICS_KEY);
  localStorage.removeItem(WORKFLOW_METRICS_KEY);
}

/* ── Audit persistence ── */

const AUDIT_KEY = "regente:audit";
const AUDIT_MAX = 1000;

interface LocalAuditEntry {
  id: string;
  timestamp: number;
  action: string;
  actor: string;
  target: string;
  targetName?: string;
  details?: Record<string, unknown>;
}

export interface AuditRow {
  id?: string;
  action: string;
  actor: string;
  target: string;
  target_name?: string;
  details?: Record<string, unknown>;
  created_at?: string;
}

export async function insertAudit(entry: LocalAuditEntry): Promise<void> {
  if (useSupabase()) {
    await (supabase.from as Function)("audit_entries").insert({
      action: entry.action,
      actor: entry.actor,
      target: entry.target,
      target_name: entry.targetName,
      details: entry.details,
    });
    return;
  }
  const entries = localLoad<LocalAuditEntry>(AUDIT_KEY);
  entries.push(entry);
  localSave(AUDIT_KEY, entries, AUDIT_MAX);
}

export async function loadAuditEntries(options?: {
  action?: string;
  target?: string;
  actor?: string;
  since?: number;
  limit?: number;
}): Promise<LocalAuditEntry[]> {
  if (useSupabase()) {
    let query = supabase
      .from("audit_entries")
      .select("*")
      .order("created_at", { ascending: true })
      .limit(options?.limit ?? AUDIT_MAX);
    if (options?.action) query = query.eq("action", options.action);
    if (options?.target) query = query.eq("target", options.target);
    if (options?.actor) query = query.eq("actor", options.actor);
    if (options?.since) query = query.gte("created_at", new Date(options.since).toISOString());
    const { data } = await query;
    return (data ?? []).map((r: AuditRow) => ({
      id: r.id!,
      timestamp: new Date(r.created_at!).getTime(),
      action: r.action,
      actor: r.actor,
      target: r.target,
      targetName: r.target_name,
      details: r.details,
    }));
  }
  let entries = localLoad<LocalAuditEntry>(AUDIT_KEY);
  if (options?.action) entries = entries.filter((e) => e.action === options.action);
  if (options?.target) entries = entries.filter((e) => e.target === options.target);
  if (options?.actor) entries = entries.filter((e) => e.actor === options.actor);
  if (options?.since) entries = entries.filter((e) => e.timestamp >= options.since!);
  if (options?.limit) entries = entries.slice(-options.limit);
  return entries;
}

export async function clearAuditStore(): Promise<void> {
  if (useSupabase()) {
    await supabase.from("audit_entries").delete().neq("action", "");
    return;
  }
  localStorage.removeItem(AUDIT_KEY);
}

/* ── Alerting persistence ── */

const RULES_KEY = "regente:alert-rules";
const EVENTS_KEY = "regente:alert-events";
const COOLDOWN_KEY = "regente:alert-cooldowns";
const ALERT_MAX = 200;

interface LocalAlertRule {
  id: string;
  name: string;
  enabled: boolean;
  workflowPattern: string;
  condition: unknown;
  severity: string;
  channels: string[];
  cooldownMs: number;
}

interface LocalAlertEvent {
  id: string;
  ruleId: string;
  ruleName: string;
  severity: string;
  timestamp: number;
  workflowId: string;
  workflowName: string;
  message: string;
  acknowledged: boolean;
}

export interface AlertRuleRow {
  id?: string;
  name: string;
  enabled: boolean;
  workflow_pattern: string;
  condition: unknown;
  severity: string;
  channels: string[];
  cooldown_ms: number;
}

export interface AlertEventRow {
  id?: string;
  rule_id: string;
  rule_name: string;
  severity: string;
  workflow_id: string;
  workflow_name: string;
  message: string;
  acknowledged: boolean;
  created_at?: string;
}

export async function loadAlertRules(): Promise<LocalAlertRule[]> {
  if (useSupabase()) {
    const { data } = await supabase.from("alert_rules").select("*");
    return (data ?? []).map((r: AlertRuleRow) => ({
      id: r.id!,
      name: r.name,
      enabled: r.enabled,
      workflowPattern: r.workflow_pattern,
      condition: r.condition,
      severity: r.severity,
      channels: r.channels,
      cooldownMs: r.cooldown_ms,
    }));
  }
  return localLoad<LocalAlertRule>(RULES_KEY);
}

export async function saveAlertRulesStore(rules: LocalAlertRule[]): Promise<void> {
  if (useSupabase()) {
    // Upsert all rules
    const rows = rules.map((r) => ({
      id: r.id,
      name: r.name,
      enabled: r.enabled,
      workflow_pattern: r.workflowPattern,
      condition: r.condition,
      severity: r.severity,
      channels: r.channels,
      cooldown_ms: r.cooldownMs,
    }));
    await (supabase.from as Function)("alert_rules").upsert(rows, { onConflict: "id" });
    return;
  }
  localSave(RULES_KEY, rules, 100);
}

export async function insertAlertEvents(events: LocalAlertEvent[]): Promise<void> {
  if (events.length === 0) return;
  if (useSupabase()) {
    const rows = events.map((e) => ({
      rule_id: e.ruleId,
      rule_name: e.ruleName,
      severity: e.severity,
      workflow_id: e.workflowId,
      workflow_name: e.workflowName,
      message: e.message,
      acknowledged: e.acknowledged,
    }));
    await (supabase.from as Function)("alert_events").insert(rows);
    return;
  }
  const existing = localLoad<LocalAlertEvent>(EVENTS_KEY);
  localSave(EVENTS_KEY, [...existing, ...events], ALERT_MAX);
}

export async function loadAlertEvents(options?: {
  severity?: string;
  acknowledged?: boolean;
  limit?: number;
}): Promise<LocalAlertEvent[]> {
  if (useSupabase()) {
    let query = supabase
      .from("alert_events")
      .select("*")
      .order("created_at", { ascending: true })
      .limit(options?.limit ?? ALERT_MAX);
    if (options?.severity) query = query.eq("severity", options.severity);
    if (options?.acknowledged !== undefined) query = query.eq("acknowledged", options.acknowledged);
    const { data } = await query;
    return (data ?? []).map((r: AlertEventRow) => ({
      id: r.id!,
      ruleId: r.rule_id,
      ruleName: r.rule_name,
      severity: r.severity,
      timestamp: new Date(r.created_at!).getTime(),
      workflowId: r.workflow_id,
      workflowName: r.workflow_name,
      message: r.message,
      acknowledged: r.acknowledged,
    }));
  }
  let events = localLoad<LocalAlertEvent>(EVENTS_KEY);
  if (options?.severity) events = events.filter((e) => e.severity === options.severity);
  if (options?.acknowledged !== undefined) events = events.filter((e) => e.acknowledged === options.acknowledged);
  if (options?.limit) events = events.slice(-options.limit);
  return events;
}

export async function acknowledgeAlertEvent(eventId: string): Promise<void> {
  if (useSupabase()) {
    await (supabase.from as Function)("alert_events").update({ acknowledged: true }).eq("id", eventId);
    return;
  }
  const events = localLoad<LocalAlertEvent>(EVENTS_KEY);
  const ev = events.find((e) => e.id === eventId);
  if (ev) {
    ev.acknowledged = true;
    localSave(EVENTS_KEY, events, ALERT_MAX);
  }
}

export async function acknowledgeAllAlertEvents(): Promise<void> {
  if (useSupabase()) {
    await (supabase.from as Function)("alert_events").update({ acknowledged: true }).eq("acknowledged", false);
    return;
  }
  const events = localLoad<LocalAlertEvent>(EVENTS_KEY);
  for (const e of events) e.acknowledged = true;
  localSave(EVENTS_KEY, events, ALERT_MAX);
}

export { COOLDOWN_KEY };
