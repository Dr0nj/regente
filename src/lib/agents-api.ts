// Fase B — agentes executores. GET /api/agents lista os agentes conectados
// (cada agent disca pro server via WS e anuncia capabilities).
import { api, isServerMode } from "./server-client";

export interface AgentInfo {
  id: string;
  capabilities: string[];
}

export async function listAgents(): Promise<AgentInfo[]> {
  if (!isServerMode()) return [];
  return api<AgentInfo[]>("/api/agents");
}
