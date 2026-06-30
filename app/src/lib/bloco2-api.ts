// Bloco 2 — Control-M parity client API.
// F14 Calendars, F15 Resources, F16 Conditions, F19 SLA, F21 Forecast, F22 Analytics.
import { api, isServerMode } from "./server-client";
import type { JobSchedule, CalendarRef } from "./orchestrator-model";

// === Schedule preview (calendário real: dias que o job rodaria) ===
// Reusa a MESMA regra da daily no backend (IsScheduledOn) — dia presente em
// `dates` = roda sem falta, ausente = não roda. Em browser-mode devolve vazio.
export interface SchedulePreviewResult { from: string; to: string; dates: string[]; }
export async function schedulePreview(
  def: { schedule: JobSchedule; calendars?: CalendarRef[]; calendar?: string },
  from: string,
  to: string,
): Promise<SchedulePreviewResult> {
  if (!isServerMode()) return { from, to, dates: [] };
  return api<SchedulePreviewResult>("/api/schedule/preview", {
    method: "POST",
    body: JSON.stringify({ definition: def, from, to }),
    headers: { "Content-Type": "application/json" },
  });
}

// === F14 Calendars ===
export interface Calendar {
  name: string;
  businessDays?: string[]; // ["Mon","Tue",...]
  holidays?: string[];     // ["2026-12-25", ...]
  includeDates?: string[];
  excludeDates?: string[];
  timezone?: string;
}

export async function listCalendars(): Promise<Calendar[]> {
  if (!isServerMode()) return [];
  return api<Calendar[]>("/api/calendars");
}
export async function saveCalendar(cal: Calendar): Promise<Calendar> {
  return api<Calendar>(`/api/calendars/${encodeURIComponent(cal.name)}`, {
    method: "PUT", body: JSON.stringify(cal), headers: { "Content-Type": "application/json" },
  });
}
export async function deleteCalendar(name: string): Promise<void> {
  await api(`/api/calendars/${encodeURIComponent(name)}`, { method: "DELETE" });
}

// === F15 Resources ===
export interface ResourceState { name: string; capacity: number; used: number; }
export async function listResources(): Promise<ResourceState[]> {
  if (!isServerMode()) return [];
  return api<ResourceState[]>("/api/resources");
}
export async function setResourceCapacity(name: string, capacity: number): Promise<void> {
  await api(`/api/resources/${encodeURIComponent(name)}`, {
    method: "PUT", body: JSON.stringify({ capacity }), headers: { "Content-Type": "application/json" },
  });
}
export async function deleteResource(name: string): Promise<void> {
  await api(`/api/resources/${encodeURIComponent(name)}`, { method: "DELETE" });
}

// === F18 Variables (global scope) ===
export interface Variable {
  name: string;
  value: string;
  updatedAt: string;
  updatedBy?: string;
}
export async function listVariables(): Promise<Variable[]> {
  if (!isServerMode()) return [];
  return api<Variable[]>("/api/variables");
}
export async function putVariable(name: string, value: string): Promise<Variable> {
  return api<Variable>(`/api/variables/${encodeURIComponent(name)}`, {
    method: "PUT", body: JSON.stringify({ value }), headers: { "Content-Type": "application/json" },
  });
}
export async function deleteVariable(name: string): Promise<void> {
  await api(`/api/variables/${encodeURIComponent(name)}`, { method: "DELETE" });
}

// === F16 Conditions ===
export interface Condition {
  name: string; scopeDate: string; setAt: string; setBy: string;
}
export async function listConditions(scope = "*"): Promise<Condition[]> {
  if (!isServerMode()) return [];
  return api<Condition[]>(`/api/conditions?scope=${encodeURIComponent(scope)}`);
}
export async function setCondition(name: string, scope: string): Promise<void> {
  await api(`/api/conditions/${encodeURIComponent(name)}/set?scope=${encodeURIComponent(scope)}`, { method: "POST" });
}
export async function unsetCondition(name: string, scope: string): Promise<void> {
  await api(`/api/conditions/${encodeURIComponent(name)}/unset?scope=${encodeURIComponent(scope)}`, { method: "POST" });
}

// === F19 SLA ===
export interface SLABreach {
  id: number; instanceId: string; defId: string; kind: string;
  severity: string; message: string; detectedAt: string; notified: boolean;
}
export async function listSLABreaches(limit = 100): Promise<SLABreach[]> {
  if (!isServerMode()) return [];
  return api<SLABreach[]>(`/api/sla/breaches?limit=${limit}`);
}

// === F21 Forecast ===
export interface ForecastJob {
  defId: string; label: string; team: string;
  startAt: string; endAt: string;
  eligible: boolean; reason?: string; wave: number; wouldBreachSla: boolean;
}
export interface ForecastReport {
  orderDate: string; jobs: ForecastJob[]; peakResourceUsage?: Record<string, number>;
}
export async function getForecast(date?: string): Promise<ForecastReport> {
  const q = date ? `?date=${date}` : "";
  return api<ForecastReport>(`/api/forecast${q}`);
}

// === F22 Analytics ===
export interface AnalyticsSummary { since: string; counts: Record<string, number>; }
export interface TopFailingItem { defId: string; fails: number; }
export interface MTTRItem { defId: string; avgSec: number; runs: number; }

export async function fetchSummary(since?: string): Promise<AnalyticsSummary> {
  const q = since ? `?since=${since}` : "";
  return api<AnalyticsSummary>(`/api/analytics/summary${q}`);
}
export async function fetchTopFailing(since?: string): Promise<{ since: string; topFailing: TopFailingItem[] }> {
  const q = since ? `?since=${since}` : "";
  return api(`/api/analytics/top-failing${q}`);
}
export async function fetchMTTR(since?: string): Promise<{ since: string; mttr: MTTRItem[] }> {
  const q = since ? `?since=${since}` : "";
  return api(`/api/analytics/mttr${q}`);
}
