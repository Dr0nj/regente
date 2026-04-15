/**
 * Alerting Engine — Phase 8 + Phase 11 (dual-mode persistence)
 *
 * Rule-based alerting system that evaluates conditions after each
 * workflow execution and fires notifications.
 *
 * MVP: fires toast notifications. Extensible to Slack/email/PagerDuty.
 * Uses Supabase when configured, localStorage as fallback.
 */

import {
  localLoad,
  localSave,
  insertAlertEvents,
  acknowledgeAlertEvent,
  acknowledgeAllAlertEvents,
  saveAlertRulesStore,
  COOLDOWN_KEY,
} from "@/lib/persistence";

/* ── Types ── */

export type AlertSeverity = "info" | "warning" | "critical";
export type AlertChannel = "toast" | "slack" | "email" | "pagerduty";

export interface AlertRule {
  id: string;
  name: string;
  enabled: boolean;
  /** Workflow ID to match (* = all) */
  workflowPattern: string;
  condition: AlertCondition;
  severity: AlertSeverity;
  channels: AlertChannel[];
  cooldownMs: number; // minimum interval between fires
}

export type AlertCondition =
  | { type: "failure" }
  | { type: "duration_exceeded"; thresholdMs: number }
  | { type: "retry_exceeded"; maxRetries: number }
  | { type: "success_rate_below"; rate: number; windowSize: number }
  | { type: "consecutive_failures"; count: number };

export interface AlertEvent {
  id: string;
  ruleId: string;
  ruleName: string;
  severity: AlertSeverity;
  timestamp: number;
  workflowId: string;
  workflowName: string;
  message: string;
  acknowledged: boolean;
}

/* ── Storage keys ── */

const RULES_KEY = "regente:alert-rules";
const EVENTS_KEY = "regente:alert-events";
const MAX_EVENTS = 200;

let eventCounter = 0;

/* ── Default rules ── */

const DEFAULT_RULES: AlertRule[] = [
  {
    id: "rule-failure",
    name: "Workflow Failure",
    enabled: true,
    workflowPattern: "*",
    condition: { type: "failure" },
    severity: "critical",
    channels: ["toast"],
    cooldownMs: 60_000,
  },
  {
    id: "rule-slow",
    name: "Slow Execution",
    enabled: true,
    workflowPattern: "*",
    condition: { type: "duration_exceeded", thresholdMs: 30_000 },
    severity: "warning",
    channels: ["toast"],
    cooldownMs: 300_000,
  },
  {
    id: "rule-retries",
    name: "Excessive Retries",
    enabled: true,
    workflowPattern: "*",
    condition: { type: "retry_exceeded", maxRetries: 3 },
    severity: "warning",
    channels: ["toast"],
    cooldownMs: 120_000,
  },
];

/* ── Rule persistence ── */

export function getAlertRules(): AlertRule[] {
  const rules = localLoad<AlertRule>(RULES_KEY);
  if (rules.length > 0) return rules;
  // Seed defaults
  localSave(RULES_KEY, DEFAULT_RULES, 100);
  // Also persist to Supabase (fire-and-forget)
  saveAlertRulesStore(DEFAULT_RULES);
  return [...DEFAULT_RULES];
}

export function saveAlertRules(rules: AlertRule[]): void {
  localSave(RULES_KEY, rules, 100);
  // Also persist to Supabase (fire-and-forget)
  saveAlertRulesStore(rules);
}

export function toggleAlertRule(ruleId: string): void {
  const rules = getAlertRules();
  const rule = rules.find((r) => r.id === ruleId);
  if (rule) {
    rule.enabled = !rule.enabled;
    saveAlertRules(rules);
  }
}

/* ── Event persistence ── */

export function getAlertEvents(options?: {
  severity?: AlertSeverity;
  acknowledged?: boolean;
  limit?: number;
}): AlertEvent[] {
  // Sync read from localStorage for fast UI rendering
  let events = localLoad<AlertEvent>(EVENTS_KEY);
  if (options?.severity) events = events.filter((e) => e.severity === options.severity);
  if (options?.acknowledged !== undefined) events = events.filter((e) => e.acknowledged === options.acknowledged);
  if (options?.limit) events = events.slice(-options.limit);
  return events;
}

function saveAlertEventsLocal(events: AlertEvent[]): void {
  localSave(EVENTS_KEY, events, MAX_EVENTS);
}

export function acknowledgeAlert(eventId: string): void {
  const events = localLoad<AlertEvent>(EVENTS_KEY);
  const ev = events.find((e) => e.id === eventId);
  if (ev) {
    ev.acknowledged = true;
    saveAlertEventsLocal(events);
    // Also persist to Supabase (fire-and-forget)
    acknowledgeAlertEvent(eventId);
  }
}

export function acknowledgeAll(): void {
  const events = localLoad<AlertEvent>(EVENTS_KEY);
  for (const e of events) e.acknowledged = true;
  saveAlertEventsLocal(events);
  // Also persist to Supabase (fire-and-forget)
  acknowledgeAllAlertEvents();
}

export function getUnacknowledgedCount(): number {
  return getAlertEvents({ acknowledged: false }).length;
}

/* ── Cooldown tracking (always localStorage — ephemeral) ── */

function getCooldowns(): Record<string, number> {
  try {
    const raw = localStorage.getItem(COOLDOWN_KEY);
    return raw ? JSON.parse(raw) : {};
  } catch {
    return {};
  }
}

function setCooldown(ruleId: string, timestamp: number): void {
  const cooldowns = getCooldowns();
  cooldowns[ruleId] = timestamp;
  localStorage.setItem(COOLDOWN_KEY, JSON.stringify(cooldowns));
}

function isOnCooldown(ruleId: string, cooldownMs: number): boolean {
  const cooldowns = getCooldowns();
  const last = cooldowns[ruleId];
  if (!last) return false;
  return Date.now() - last < cooldownMs;
}

/* ── Evaluation ── */

export interface EvaluationContext {
  workflowId: string;
  workflowName: string;
  status: "SUCCESS" | "FAILED" | "ABORTED";
  durationMs: number;
  maxJobRetries: number;
  /** Historical success rate (0-1) from metrics */
  recentSuccessRate: number;
  /** Recent consecutive failure count */
  consecutiveFailures: number;
}

function matchesWorkflow(pattern: string, workflowId: string): boolean {
  if (pattern === "*") return true;
  return pattern === workflowId;
}

function evaluateCondition(condition: AlertCondition, ctx: EvaluationContext): boolean {
  switch (condition.type) {
    case "failure":
      return ctx.status === "FAILED";
    case "duration_exceeded":
      return ctx.durationMs > condition.thresholdMs;
    case "retry_exceeded":
      return ctx.maxJobRetries > condition.maxRetries;
    case "success_rate_below":
      return ctx.recentSuccessRate < condition.rate;
    case "consecutive_failures":
      return ctx.consecutiveFailures >= condition.count;
    default:
      return false;
  }
}

function buildMessage(rule: AlertRule, ctx: EvaluationContext): string {
  const wf = ctx.workflowName || ctx.workflowId;
  switch (rule.condition.type) {
    case "failure":
      return `Workflow "${wf}" failed after ${(ctx.durationMs / 1000).toFixed(1)}s`;
    case "duration_exceeded":
      return `Workflow "${wf}" exceeded ${(rule.condition.thresholdMs / 1000).toFixed(0)}s threshold (took ${(ctx.durationMs / 1000).toFixed(1)}s)`;
    case "retry_exceeded":
      return `Workflow "${wf}" had ${ctx.maxJobRetries} retries (limit: ${rule.condition.maxRetries})`;
    case "success_rate_below":
      return `Workflow "${wf}" success rate dropped to ${(ctx.recentSuccessRate * 100).toFixed(0)}%`;
    case "consecutive_failures":
      return `Workflow "${wf}" has ${ctx.consecutiveFailures} consecutive failures`;
    default:
      return `Alert triggered for "${wf}"`;
  }
}

export type AlertNotifier = (event: AlertEvent) => void;

/**
 * Evaluate all enabled alert rules against an execution context.
 * Returns fired alert events and calls the notifier for each.
 */
export function evaluateAlerts(
  ctx: EvaluationContext,
  notifier?: AlertNotifier,
): AlertEvent[] {
  const rules = getAlertRules().filter((r) => r.enabled);
  const fired: AlertEvent[] = [];

  for (const rule of rules) {
    if (!matchesWorkflow(rule.workflowPattern, ctx.workflowId)) continue;
    if (isOnCooldown(rule.id, rule.cooldownMs)) continue;
    if (!evaluateCondition(rule.condition, ctx)) continue;

    const event: AlertEvent = {
      id: `alert-${Date.now()}-${++eventCounter}`,
      ruleId: rule.id,
      ruleName: rule.name,
      severity: rule.severity,
      timestamp: Date.now(),
      workflowId: ctx.workflowId,
      workflowName: ctx.workflowName,
      message: buildMessage(rule, ctx),
      acknowledged: false,
    };

    fired.push(event);
    setCooldown(rule.id, Date.now());
    notifier?.(event);
  }

  // Persist fired events
  if (fired.length > 0) {
    const existing = localLoad<AlertEvent>(EVENTS_KEY);
    saveAlertEventsLocal([...existing, ...fired]);
    // Also persist to Supabase (fire-and-forget)
    insertAlertEvents(fired);
  }

  return fired;
}

/** Severity config for UI */
export const SEVERITY_CONFIG: Record<AlertSeverity, { label: string; color: string; dot: string }> = {
  info: { label: "Info", color: "text-cyan-400", dot: "bg-cyan-400" },
  warning: { label: "Warning", color: "text-amber-400", dot: "bg-amber-400" },
  critical: { label: "Critical", color: "text-red-400", dot: "bg-red-400" },
};
