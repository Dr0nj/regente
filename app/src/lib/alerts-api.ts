/**
 * alerts-api.ts — facade dual-mode para alertas (Phase 8).
 *
 * Em server mode roteia para /api/alerts*; caso contrário delega para o engine
 * browser (localStorage) em @/lib/alerting. A UI consome só este módulo, então
 * a troca entre os modos é transparente.
 */
import { api, isServerMode } from "./server-client";
import {
  getAlertEvents,
  getAlertRules,
  acknowledgeAlert,
  acknowledgeAll,
  toggleAlertRule,
  setAlertRuleChannels,
  setAlertRuleCooldown,
  getUnacknowledgedCount,
  type AlertChannel,
  type AlertEvent,
  type AlertRule,
} from "./alerting";

export async function fetchAlertEvents(limit = 200): Promise<AlertEvent[]> {
  if (isServerMode()) {
    return api<AlertEvent[]>(`/api/alerts?limit=${limit}`);
  }
  return getAlertEvents({ limit }).sort((a, b) => b.timestamp - a.timestamp);
}

export async function fetchAlertRules(): Promise<AlertRule[]> {
  if (isServerMode()) {
    return api<AlertRule[]>("/api/alerts/rules");
  }
  return getAlertRules();
}

export async function ackAlert(id: string): Promise<void> {
  if (isServerMode()) {
    await api(`/api/alerts/${encodeURIComponent(id)}/ack`, { method: "POST" });
    return;
  }
  acknowledgeAlert(id);
}

export async function ackAllAlerts(): Promise<void> {
  if (isServerMode()) {
    await api("/api/alerts/ack-all", { method: "POST" });
    return;
  }
  acknowledgeAll();
}

export async function toggleRule(id: string): Promise<void> {
  if (isServerMode()) {
    await api(`/api/alerts/rules/${encodeURIComponent(id)}/toggle`, { method: "POST" });
    return;
  }
  toggleAlertRule(id);
}

export async function updateRuleChannels(id: string, channels: AlertChannel[]): Promise<void> {
  if (isServerMode()) {
    await api(`/api/alerts/rules/${encodeURIComponent(id)}/channels`, {
      method: "PUT",
      body: JSON.stringify({ channels }),
    });
    return;
  }
  setAlertRuleChannels(id, channels);
}

export async function updateRuleCooldown(id: string, cooldownMs: number): Promise<void> {
  if (isServerMode()) {
    await api(`/api/alerts/rules/${encodeURIComponent(id)}/cooldown`, {
      method: "PUT",
      body: JSON.stringify({ cooldownMs }),
    });
    return;
  }
  setAlertRuleCooldown(id, cooldownMs);
}

export async function fetchUnacknowledgedCount(): Promise<number> {
  if (isServerMode()) {
    const events = await fetchAlertEvents(500);
    return events.filter((e) => !e.acknowledged).length;
  }
  return getUnacknowledgedCount();
}
