// Fase B — agentes executores. GET /api/agents lista os agentes conectados
// (cada agent disca pro server via WS e anuncia capabilities).
import { api, isServerMode } from "./server-client";

export interface AgentInfo {
  id: string;
  capabilities: string[];
  environment?: string; // ADV-2 — label de ambiente/site (flag -env); só quando online
  online: boolean;
  node?: string;   // R5 — nó do cluster em que o agente está conectado
  local?: boolean; // conectado NESTE nó (pingável); false = online em outro nó
  os?: string;
  arch?: string;
  host?: string;
  version?: string;
  startedAt?: string;   // início do processo do agente (uptime)
  connectedAt?: string; // conectou neste servidor (sessão)
  firstSeen?: string;
  lastSeen?: string;
}

export async function listAgents(): Promise<AgentInfo[]> {
  if (!isServerMode()) return [];
  return api<AgentInfo[]>("/api/agents");
}

// Ping ativo — round-trip pelo /ws/agent (latência), não só presença da conexão.
export interface PingResult {
  id: string;
  online: boolean;
  ok: boolean;
  latencyMs?: number;
  error?: string;
}

export async function pingAgent(id: string): Promise<PingResult> {
  return api<PingResult>(`/api/agents/${encodeURIComponent(id)}/ping`, { method: "POST" });
}

// B5 — tokens por agente (admin).
export interface AgentToken {
	 agentId: string;
	 environment: string;
	 capabilities: string[];
	 expiresAt: string;
	 status: "active" | "expired" | "revoked" | "requires_reissue";
  id: number;
  label: string;
  tokenPrefix: string;
  createdAt: string;
  lastUsedAt: string;
}

export async function listAgentTokens(): Promise<AgentToken[]> {
  if (!isServerMode()) return [];
  return api<AgentToken[]>("/api/agents/tokens");
}

/** Cria um token de agente. O `token` cru só volta aqui, uma vez. */
export async function createAgentToken(body: { label: string; agentId: string; environment: string; capabilities: string[]; expiresAt: string }): Promise<{ id: number; token: string }> {
  return api("/api/agents/tokens", { method: "POST", body: JSON.stringify(body) });
}

export async function rotateAgentToken(id: number, validityDays: number, graceSeconds: number): Promise<{ id: number; token: string }> {
  const expiresAt = new Date(Date.now() + validityDays * 86400000).toISOString();
  return api(`/api/agents/tokens/${id}/rotate`, { method: "POST", body: JSON.stringify({ expiresAt, graceSeconds }) });
}

export async function revokeAgentToken(id: number): Promise<void> {
  await api(`/api/agents/tokens/${id}`, { method: "DELETE" });
}
