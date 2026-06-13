// F20 — Settings API client.
import { api, isServerMode } from "./server-client";

export type ServerSettings = Record<string, string>;

export async function getSettings(): Promise<ServerSettings> {
  if (!isServerMode()) return {};
  return api<ServerSettings>("/api/settings");
}

export async function putSettings(patch: ServerSettings): Promise<ServerSettings> {
  return api<ServerSettings>("/api/settings", {
    method: "PUT",
    body: JSON.stringify(patch),
  });
}
