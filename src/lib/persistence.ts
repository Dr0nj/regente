/**
 * Persistence Layer — localStorage only.
 *
 * v1 tinha um modo dual Supabase/localStorage. O Supabase foi removido
 * (server-backed agora é a fonte da verdade); esta camada é só localStorage,
 * usada pelo runtime browser legado (metrics/audit/alerting) que ainda existe
 * por trás do runtime-bridge no modo sem servidor.
 */

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

/** Mantido por compat; sempre false (sem backend Supabase). */
export function useSupabase(): boolean {
  return false;
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
  const entries = localLoad<LocalJobMetric>(JOB_METRICS_KEY);
  entries.push(entry);
  localSave(JOB_METRICS_KEY, entries, METRICS_MAX);
}

export async function insertWorkflowMetric(entry: LocalWorkflowMetric): Promise<void> {
  const entries = localLoad<LocalWorkflowMetric>(WORKFLOW_METRICS_KEY);
  entries.push(entry);
  localSave(WORKFLOW_METRICS_KEY, entries, METRICS_MAX);
}

export async function loadJobMetrics(nodeId?: string): Promise<LocalJobMetric[]> {
  const entries = localLoad<LocalJobMetric>(JOB_METRICS_KEY);
  return nodeId ? entries.filter((e) => e.nodeId === nodeId) : entries;
}

export async function loadWorkflowMetrics(workflowId?: string): Promise<LocalWorkflowMetric[]> {
  const entries = localLoad<LocalWorkflowMetric>(WORKFLOW_METRICS_KEY);
  return workflowId ? entries.filter((e) => e.workflowId === workflowId) : entries;
}

export async function clearMetrics(): Promise<void> {
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
  let entries = localLoad<LocalAuditEntry>(AUDIT_KEY);
  if (options?.action) entries = entries.filter((e) => e.action === options.action);
  if (options?.target) entries = entries.filter((e) => e.target === options.target);
  if (options?.actor) entries = entries.filter((e) => e.actor === options.actor);
  if (options?.since) entries = entries.filter((e) => e.timestamp >= options.since!);
  if (options?.limit) entries = entries.slice(-options.limit);
  return entries;
}

export async function clearAuditStore(): Promise<void> {
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
  return localLoad<LocalAlertRule>(RULES_KEY);
}

export async function saveAlertRulesStore(rules: LocalAlertRule[]): Promise<void> {
  localSave(RULES_KEY, rules, 100);
}

export async function insertAlertEvents(events: LocalAlertEvent[]): Promise<void> {
  if (events.length === 0) return;
  const existing = localLoad<LocalAlertEvent>(EVENTS_KEY);
  localSave(EVENTS_KEY, [...existing, ...events], ALERT_MAX);
}

export async function loadAlertEvents(options?: {
  severity?: string;
  acknowledged?: boolean;
  limit?: number;
}): Promise<LocalAlertEvent[]> {
  let events = localLoad<LocalAlertEvent>(EVENTS_KEY);
  if (options?.severity) events = events.filter((e) => e.severity === options.severity);
  if (options?.acknowledged !== undefined) events = events.filter((e) => e.acknowledged === options.acknowledged);
  if (options?.limit) events = events.slice(-options.limit);
  return events;
}

export async function acknowledgeAlertEvent(eventId: string): Promise<void> {
  const events = localLoad<LocalAlertEvent>(EVENTS_KEY);
  const ev = events.find((e) => e.id === eventId);
  if (ev) {
    ev.acknowledged = true;
    localSave(EVENTS_KEY, events, ALERT_MAX);
  }
}

export async function acknowledgeAllAlertEvents(): Promise<void> {
  const events = localLoad<LocalAlertEvent>(EVENTS_KEY);
  for (const e of events) e.acknowledged = true;
  localSave(EVENTS_KEY, events, ALERT_MAX);
}

export { COOLDOWN_KEY };
