/**
 * Audit Trail — Phase 8
 *
 * Records user/system actions for compliance and debugging.
 * Stored in localStorage (MVP), Supabase-ready interface.
 */

/* ── Types ── */

export type AuditAction =
  | "workflow.created"
  | "workflow.saved"
  | "workflow.deleted"
  | "workflow.imported"
  | "workflow.exported"
  | "workflow.executed"
  | "workflow.aborted"
  | "workflow.completed"
  | "workflow.scheduled"
  | "workflow.unscheduled"
  | "node.created"
  | "node.deleted"
  | "node.updated"
  | "node.duplicated"
  | "version.created"
  | "version.restored"
  | "template.applied"
  | "user.login"
  | "user.logout";

export interface AuditEntry {
  id: string;
  timestamp: number;
  action: AuditAction;
  actor: string; // userId or "system"
  target: string; // workflowId, nodeId, etc.
  targetName?: string;
  details?: Record<string, unknown>;
}

/* ── Storage ── */

const AUDIT_KEY = "regente:audit";
const MAX_ENTRIES = 1000;

let counter = 0;

function generateId(): string {
  return `audit-${Date.now()}-${++counter}`;
}

/* ── Public API ── */

export function recordAudit(
  action: AuditAction,
  target: string,
  options?: {
    actor?: string;
    targetName?: string;
    details?: Record<string, unknown>;
  },
): AuditEntry {
  const entry: AuditEntry = {
    id: generateId(),
    timestamp: Date.now(),
    action,
    actor: options?.actor ?? "user",
    target,
    targetName: options?.targetName,
    details: options?.details,
  };

  const entries = getAuditEntries();
  entries.push(entry);

  // Trim to max
  const trimmed = entries.slice(-MAX_ENTRIES);
  localStorage.setItem(AUDIT_KEY, JSON.stringify(trimmed));

  return entry;
}

export function getAuditEntries(options?: {
  action?: AuditAction;
  target?: string;
  actor?: string;
  since?: number;
  limit?: number;
}): AuditEntry[] {
  let entries: AuditEntry[];
  try {
    const raw = localStorage.getItem(AUDIT_KEY);
    entries = raw ? JSON.parse(raw) : [];
  } catch {
    entries = [];
  }

  if (options?.action) entries = entries.filter((e) => e.action === options.action);
  if (options?.target) entries = entries.filter((e) => e.target === options.target);
  if (options?.actor) entries = entries.filter((e) => e.actor === options.actor);
  if (options?.since) entries = entries.filter((e) => e.timestamp >= options.since!);
  if (options?.limit) entries = entries.slice(-options.limit);

  return entries;
}

export function getRecentAudit(limit = 50): AuditEntry[] {
  return getAuditEntries({ limit }).reverse();
}

export function clearAudit(): void {
  localStorage.removeItem(AUDIT_KEY);
}

/** Human-readable action label */
export function actionLabel(action: AuditAction): string {
  const labels: Record<AuditAction, string> = {
    "workflow.created": "Workflow Created",
    "workflow.saved": "Workflow Saved",
    "workflow.deleted": "Workflow Deleted",
    "workflow.imported": "Workflow Imported",
    "workflow.exported": "Workflow Exported",
    "workflow.executed": "Execution Started",
    "workflow.aborted": "Execution Aborted",
    "workflow.completed": "Execution Completed",
    "workflow.scheduled": "Schedule Added",
    "workflow.unscheduled": "Schedule Removed",
    "node.created": "Node Created",
    "node.deleted": "Node Deleted",
    "node.updated": "Node Updated",
    "node.duplicated": "Node Duplicated",
    "version.created": "Version Created",
    "version.restored": "Version Restored",
    "template.applied": "Template Applied",
    "user.login": "User Login",
    "user.logout": "User Logout",
  };
  return labels[action] ?? action;
}

/** Color class for action badge */
export function actionColor(action: AuditAction): string {
  if (action.startsWith("workflow.executed") || action.startsWith("workflow.completed"))
    return "text-cyan-400 bg-cyan-500/10 ring-cyan-500/20";
  if (action.includes("deleted") || action.includes("aborted"))
    return "text-red-400 bg-red-500/10 ring-red-500/20";
  if (action.includes("created") || action.includes("saved"))
    return "text-emerald-400 bg-emerald-500/10 ring-emerald-500/20";
  if (action.includes("scheduled"))
    return "text-amber-400 bg-amber-500/10 ring-amber-500/20";
  return "text-slate-400 bg-slate-500/10 ring-slate-500/20";
}
