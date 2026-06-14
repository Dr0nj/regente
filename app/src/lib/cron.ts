/**
 * Cron utilities — Phase 7
 *
 * Parses standard 5-field cron expressions and calculates next run times.
 * Fields: minute hour day-of-month month day-of-week
 *
 * Supports: numbers, ranges (1-5), steps (star/5), lists (1,3,5), wildcards (star)
 */

interface CronField {
  values: Set<number>;
  min: number;
  max: number;
}

function parseField(field: string, min: number, max: number): CronField {
  const values = new Set<number>();

  for (const part of field.split(",")) {
    if (part === "*") {
      for (let i = min; i <= max; i++) values.add(i);
    } else if (part.includes("/")) {
      const [range, stepStr] = part.split("/");
      const step = parseInt(stepStr, 10);
      if (isNaN(step) || step <= 0) continue;
      const start = range === "*" ? min : parseInt(range, 10);
      for (let i = start; i <= max; i += step) values.add(i);
    } else if (part.includes("-")) {
      const [lo, hi] = part.split("-").map(Number);
      for (let i = lo; i <= hi; i++) values.add(i);
    } else {
      const n = parseInt(part, 10);
      if (!isNaN(n) && n >= min && n <= max) values.add(n);
    }
  }

  return { values, min, max };
}

export interface ParsedCron {
  minute: CronField;
  hour: CronField;
  dayOfMonth: CronField;
  month: CronField;
  dayOfWeek: CronField;
  raw: string;
}

/**
 * Parse a 5-field cron expression. Returns null if invalid.
 */
export function parseCron(expression: string): ParsedCron | null {
  const parts = expression.trim().split(/\s+/);
  if (parts.length < 5 || parts.length > 6) return null;

  // If 6 fields, ignore seconds (first field) — shift
  const fields = parts.length === 6 ? parts.slice(1) : parts;

  try {
    return {
      minute: parseField(fields[0], 0, 59),
      hour: parseField(fields[1], 0, 23),
      dayOfMonth: parseField(fields[2], 1, 31),
      month: parseField(fields[3], 1, 12),
      dayOfWeek: parseField(fields[4], 0, 6),
      raw: expression,
    };
  } catch {
    return null;
  }
}

/**
 * Calculate the next execution time after `from`.
 * Returns null if no valid time found within 1 year.
 */
export function nextRun(cron: ParsedCron, from: Date = new Date()): Date | null {
  const d = new Date(from.getTime());
  d.setSeconds(0, 0);
  d.setMinutes(d.getMinutes() + 1); // at least 1 minute ahead

  const maxIterations = 525960; // ~1 year in minutes
  for (let i = 0; i < maxIterations; i++) {
    if (
      cron.month.values.has(d.getMonth() + 1) &&
      cron.dayOfMonth.values.has(d.getDate()) &&
      cron.dayOfWeek.values.has(d.getDay()) &&
      cron.hour.values.has(d.getHours()) &&
      cron.minute.values.has(d.getMinutes())
    ) {
      return d;
    }
    d.setMinutes(d.getMinutes() + 1);
  }
  return null;
}

/**
 * Human-readable description of a cron expression.
 */
export function describeCron(expression: string): string {
  const cron = parseCron(expression);
  if (!cron) return "Invalid cron expression";

  const parts: string[] = [];
  const minSize = cron.minute.values.size;
  const hourSize = cron.hour.values.size;

  if (minSize === 60 && hourSize === 24) {
    parts.push("Every minute");
  } else if (minSize === 1 && hourSize === 1) {
    const m = [...cron.minute.values][0];
    const h = [...cron.hour.values][0];
    parts.push(`Daily at ${String(h).padStart(2, "0")}:${String(m).padStart(2, "0")}`);
  } else if (hourSize === 24 && minSize === 1) {
    const m = [...cron.minute.values][0];
    if (m === 0) parts.push("Every hour");
    else parts.push(`Hourly at :${String(m).padStart(2, "0")}`);
  } else {
    // Check for step patterns
    const raw = expression.trim().split(/\s+/);
    const minuteField = raw.length === 6 ? raw[1] : raw[0];
    if (minuteField.startsWith("*/")) {
      parts.push(`Every ${minuteField.slice(2)} minutes`);
    } else {
      parts.push(`At minute ${[...cron.minute.values].sort((a, b) => a - b).join(",")}`);
      if (hourSize < 24) {
        parts.push(`hour ${[...cron.hour.values].sort((a, b) => a - b).join(",")}`);
      }
    }
  }

  // Day-of-week
  const dowSize = cron.dayOfWeek.values.size;
  if (dowSize < 7) {
    const dayNames = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
    const days = [...cron.dayOfWeek.values].sort((a, b) => a - b).map((d) => dayNames[d]);
    parts.push(`on ${days.join(", ")}`);
  }

  return parts.join(" ");
}

/**
 * Validate a cron expression string. Returns error message or null.
 */
export function validateCron(expression: string): string | null {
  if (!expression || !expression.trim()) return null; // empty is OK (no schedule)
  const cron = parseCron(expression);
  if (!cron) return "Invalid cron: expected 5 or 6 space-separated fields";
  if (cron.minute.values.size === 0) return "Invalid minute field";
  if (cron.hour.values.size === 0) return "Invalid hour field";
  if (cron.dayOfMonth.values.size === 0) return "Invalid day-of-month field";
  if (cron.month.values.size === 0) return "Invalid month field";
  if (cron.dayOfWeek.values.size === 0) return "Invalid day-of-week field";
  return null;
}
