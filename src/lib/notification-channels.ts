/**
 * Notification Channels — Phase 12
 *
 * Dispatches alert events to external services (Slack, Discord, generic webhook).
 * Template engine with variable substitution for message formatting.
 * Rate limiting per channel to prevent spam.
 */

import type { AlertEvent, AlertSeverity } from "@/lib/alerting";

/* ── Types ── */

export type ChannelType = "slack" | "discord" | "webhook";

export interface ChannelConfig {
  id: string;
  type: ChannelType;
  name: string;
  enabled: boolean;
  webhookUrl: string;
  /** Optional custom message template (Mustache-style {{var}}) */
  messageTemplate?: string;
  /** Rate limit: minimum ms between dispatches per channel */
  rateLimitMs: number;
}

export interface DispatchResult {
  channelId: string;
  channelName: string;
  success: boolean;
  statusCode?: number;
  error?: string;
}

/* ── Template Engine ── */

const DEFAULT_TEMPLATE =
  "[{{severity}}] {{ruleName}}: {{message}} (workflow: {{workflowName}})";

interface TemplateVars {
  severity: string;
  ruleName: string;
  message: string;
  workflowId: string;
  workflowName: string;
  timestamp: string;
  ruleId: string;
}

function renderTemplate(template: string, vars: TemplateVars): string {
  return template.replace(/\{\{(\w+)\}\}/g, (_, key: string) => {
    return (vars as unknown as Record<string, string>)[key] ?? `{{${key}}}`;
  });
}

function eventToVars(event: AlertEvent): TemplateVars {
  return {
    severity: event.severity.toUpperCase(),
    ruleName: event.ruleName,
    message: event.message,
    workflowId: event.workflowId,
    workflowName: event.workflowName,
    timestamp: new Date(event.timestamp).toISOString(),
    ruleId: event.ruleId,
  };
}

/* ── Severity → color mapping for embeds ── */

const SEVERITY_COLORS: Record<AlertSeverity, number> = {
  info: 0x22d3ee, // cyan
  warning: 0xfbbf24, // amber
  critical: 0xef4444, // red
};

const SEVERITY_EMOJI: Record<AlertSeverity, string> = {
  info: "ℹ️",
  warning: "⚠️",
  critical: "🚨",
};

/* ── Rate limiting ── */

const lastDispatch: Record<string, number> = {};

function isRateLimited(channelId: string, rateLimitMs: number): boolean {
  const last = lastDispatch[channelId];
  if (!last) return false;
  return Date.now() - last < rateLimitMs;
}

function markDispatched(channelId: string): void {
  lastDispatch[channelId] = Date.now();
}

/* ── Channel dispatchers ── */

async function dispatchSlack(
  config: ChannelConfig,
  event: AlertEvent,
): Promise<DispatchResult> {
  const vars = eventToVars(event);
  const text = renderTemplate(
    config.messageTemplate || DEFAULT_TEMPLATE,
    vars,
  );

  const payload = {
    text: `${SEVERITY_EMOJI[event.severity]} ${text}`,
    blocks: [
      {
        type: "section",
        text: { type: "mrkdwn", text: `*${SEVERITY_EMOJI[event.severity]} ${event.severity.toUpperCase()}* — ${event.ruleName}` },
      },
      {
        type: "section",
        fields: [
          { type: "mrkdwn", text: `*Workflow:*\n${event.workflowName}` },
          { type: "mrkdwn", text: `*Time:*\n${new Date(event.timestamp).toLocaleString()}` },
        ],
      },
      {
        type: "section",
        text: { type: "mrkdwn", text: event.message },
      },
    ],
  };

  try {
    const res = await fetch(config.webhookUrl, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    return {
      channelId: config.id,
      channelName: config.name,
      success: res.ok,
      statusCode: res.status,
      error: res.ok ? undefined : `HTTP ${res.status}`,
    };
  } catch (e) {
    return {
      channelId: config.id,
      channelName: config.name,
      success: false,
      error: e instanceof Error ? e.message : "Unknown error",
    };
  }
}

async function dispatchDiscord(
  config: ChannelConfig,
  event: AlertEvent,
): Promise<DispatchResult> {
  const vars = eventToVars(event);
  const text = renderTemplate(
    config.messageTemplate || DEFAULT_TEMPLATE,
    vars,
  );

  const payload = {
    content: `${SEVERITY_EMOJI[event.severity]} ${text}`,
    embeds: [
      {
        title: `${event.severity.toUpperCase()} — ${event.ruleName}`,
        description: event.message,
        color: SEVERITY_COLORS[event.severity],
        fields: [
          { name: "Workflow", value: event.workflowName, inline: true },
          { name: "Time", value: new Date(event.timestamp).toLocaleString(), inline: true },
        ],
        timestamp: new Date(event.timestamp).toISOString(),
      },
    ],
  };

  try {
    const res = await fetch(config.webhookUrl, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    return {
      channelId: config.id,
      channelName: config.name,
      success: res.ok,
      statusCode: res.status,
      error: res.ok ? undefined : `HTTP ${res.status}`,
    };
  } catch (e) {
    return {
      channelId: config.id,
      channelName: config.name,
      success: false,
      error: e instanceof Error ? e.message : "Unknown error",
    };
  }
}

async function dispatchWebhook(
  config: ChannelConfig,
  event: AlertEvent,
): Promise<DispatchResult> {
  const vars = eventToVars(event);
  const text = renderTemplate(
    config.messageTemplate || DEFAULT_TEMPLATE,
    vars,
  );

  const payload = {
    severity: event.severity,
    ruleName: event.ruleName,
    message: text,
    workflowId: event.workflowId,
    workflowName: event.workflowName,
    timestamp: event.timestamp,
    raw: event,
  };

  try {
    const res = await fetch(config.webhookUrl, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    return {
      channelId: config.id,
      channelName: config.name,
      success: res.ok,
      statusCode: res.status,
      error: res.ok ? undefined : `HTTP ${res.status}`,
    };
  } catch (e) {
    return {
      channelId: config.id,
      channelName: config.name,
      success: false,
      error: e instanceof Error ? e.message : "Unknown error",
    };
  }
}

/* ── Public API ── */

const DISPATCHERS: Record<ChannelType, (config: ChannelConfig, event: AlertEvent) => Promise<DispatchResult>> = {
  slack: dispatchSlack,
  discord: dispatchDiscord,
  webhook: dispatchWebhook,
};

/**
 * Dispatch an alert event to a single channel.
 * Respects rate limiting; returns null if rate-limited.
 */
export async function dispatchToChannel(
  config: ChannelConfig,
  event: AlertEvent,
): Promise<DispatchResult | null> {
  if (!config.enabled) return null;
  if (isRateLimited(config.id, config.rateLimitMs)) return null;

  const dispatcher = DISPATCHERS[config.type];
  if (!dispatcher) return null;

  markDispatched(config.id);
  return dispatcher(config, event);
}

/**
 * Dispatch an alert event to all enabled channels.
 * Returns results for channels that were dispatched (not rate-limited).
 */
export async function dispatchToAllChannels(
  channels: ChannelConfig[],
  event: AlertEvent,
): Promise<DispatchResult[]> {
  const promises = channels
    .filter((c) => c.enabled)
    .map((c) => dispatchToChannel(c, event));

  const results = await Promise.allSettled(promises);
  return results
    .map((r) => (r.status === "fulfilled" ? r.value : null))
    .filter((r): r is DispatchResult => r !== null);
}

/** Create a test event for testing channel configuration */
export function createTestEvent(): AlertEvent {
  return {
    id: `test-${Date.now()}`,
    ruleId: "test",
    ruleName: "Test Notification",
    severity: "info",
    timestamp: Date.now(),
    workflowId: "test-workflow",
    workflowName: "Test Workflow",
    message: "This is a test notification from Regente to verify your channel configuration.",
    acknowledged: false,
  };
}

/** Channel type metadata for UI */
export const CHANNEL_TYPE_META: Record<ChannelType, { label: string; placeholder: string; color: string }> = {
  slack: {
    label: "Slack",
    placeholder: "https://hooks.slack.com/services/...",
    color: "text-[#E01E5A]",
  },
  discord: {
    label: "Discord",
    placeholder: "https://discord.com/api/webhooks/...",
    color: "text-[#5865F2]",
  },
  webhook: {
    label: "Webhook",
    placeholder: "https://your-service.com/webhook",
    color: "text-emerald-400",
  },
};
