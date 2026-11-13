// Fonte semântica única para cards, densidade, legenda e tooltips do minimap.
export type VisualStatus = "failed" | "uncertain" | "running" | "hold" | "confirm" | "agent" | "slot" | "resource" | "condition" | "waiting" | "success" | "inactive";
export interface StatusInput {
  status?: string; held?: boolean; waitConfirm?: boolean; waitAgent?: boolean;
  waitResource?: boolean; waitEvent?: boolean; waitSlot?: boolean;
}
export const STATUS_VISUALS: Record<VisualStatus, { label: string; token: string; priority: number }> = {
  failed: { label: "FAIL", token: "--job-failed", priority: 100 },
  uncertain: { label: "UNCERTAIN", token: "--job-uncertain", priority: 90 },
  running: { label: "RUNNING", token: "--job-running", priority: 80 },
  hold: { label: "HOLD", token: "--job-hold", priority: 70 },
  confirm: { label: "CONFIRM", token: "--job-confirm", priority: 60 },
  agent: { label: "WAIT AGENT", token: "--job-agent", priority: 50 },
  slot: { label: "WAIT SLOT", token: "--job-slot", priority: 50 },
  resource: { label: "WAIT RESOURCE", token: "--job-resource", priority: 50 },
  condition: { label: "WAIT COND", token: "--job-waiting", priority: 40 },
  waiting: { label: "WAIT", token: "--job-waiting", priority: 30 },
  success: { label: "OK", token: "--job-success", priority: 20 },
  inactive: { label: "IDLE", token: "--job-inactive", priority: 10 },
};
export function jobVisualStatus(data: StatusInput): VisualStatus {
  if (data.status === "FAILED" || data.status === "NOTOK") return "failed";
  if (data.status === "UNCERTAIN") return "uncertain";
  if (data.status === "RUNNING") return "running";
  if (data.held || data.status === "HELD" || data.status === "HOLD") return "hold";
  if (data.status === "WAITING") {
    if (data.waitConfirm) return "confirm";
    if (data.waitAgent) return "agent";
    if (data.waitSlot) return "slot";
    if (data.waitResource) return "resource";
    if (data.waitEvent) return "condition";
    return "waiting";
  }
  if (data.status === "SUCCESS" || data.status === "OK") return "success";
  return "inactive";
}
export function jobStatusVisual(data: StatusInput) {
  const status = jobVisualStatus(data);
  return { status, ...STATUS_VISUALS[status], color: `var(${STATUS_VISUALS[status].token})` };
}
export type StatusCounts = Partial<Record<VisualStatus, number>>;
export function priorityStatus(counts: StatusCounts): VisualStatus {
  let winner: VisualStatus = "inactive";
  for (const status of Object.keys(counts) as VisualStatus[]) {
    if ((counts[status] ?? 0) > 0 && STATUS_VISUALS[status].priority > STATUS_VISUALS[winner].priority) winner = status;
  }
  return winner;
}
export function dominantStatus(counts: StatusCounts): VisualStatus {
  let winner: VisualStatus = "inactive", max = 0;
  for (const status of Object.keys(counts) as VisualStatus[]) {
    const count = counts[status] ?? 0;
    if (count > max || (count === max && STATUS_VISUALS[status].priority > STATUS_VISUALS[winner].priority)) { winner = status; max = count; }
  }
  return winner;
}
