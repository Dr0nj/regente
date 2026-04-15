/**
 * Notification Settings — Phase 12
 *
 * Persistence layer for notification channel configurations.
 * localStorage synchronous for UI, async write to Supabase when available.
 */

import { localLoad, localSave } from "@/lib/persistence";
import type { ChannelConfig } from "@/lib/notification-channels";

const CHANNELS_KEY = "regente:notification-channels";
const MAX_CHANNELS = 20;

let idCounter = 0;

export function generateChannelId(): string {
  return `ch-${Date.now()}-${++idCounter}`;
}

/** Load all configured notification channels */
export function getNotificationChannels(): ChannelConfig[] {
  return localLoad<ChannelConfig>(CHANNELS_KEY);
}

/** Save all notification channels */
export function saveNotificationChannels(channels: ChannelConfig[]): void {
  localSave(CHANNELS_KEY, channels, MAX_CHANNELS);
}

/** Add a new channel configuration */
export function addNotificationChannel(channel: ChannelConfig): void {
  const channels = getNotificationChannels();
  channels.push(channel);
  saveNotificationChannels(channels);
}

/** Update an existing channel configuration */
export function updateNotificationChannel(
  channelId: string,
  updates: Partial<ChannelConfig>,
): void {
  const channels = getNotificationChannels();
  const index = channels.findIndex((c) => c.id === channelId);
  if (index === -1) return;
  channels[index] = { ...channels[index], ...updates };
  saveNotificationChannels(channels);
}

/** Remove a channel configuration */
export function removeNotificationChannel(channelId: string): void {
  const channels = getNotificationChannels().filter((c) => c.id !== channelId);
  saveNotificationChannels(channels);
}

/** Toggle channel enabled/disabled */
export function toggleNotificationChannel(channelId: string): void {
  const channels = getNotificationChannels();
  const ch = channels.find((c) => c.id === channelId);
  if (ch) {
    ch.enabled = !ch.enabled;
    saveNotificationChannels(channels);
  }
}
