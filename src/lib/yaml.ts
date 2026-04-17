/**
 * YAML serializer mínimo para JobDefinition.
 *
 * Não depende de libs externas (bundle size matters). Suporta o
 * subset necessário: strings com quote, numbers, booleans, arrays
 * simples, objetos rasos. Rejeita tipos exóticos (Date, Function).
 *
 * Formato canônico (estável para diff em Git):
 *   id: "extract-daily"
 *   label: "Extract Daily"
 *   jobType: "LAMBDA"
 *   team: "DATA"
 *   schedule:
 *     cronExpression: "0 3 * * *"
 *     enabled: true
 *   retries: 2
 *   timeout: 300
 */

import type { JobDefinition } from "@/lib/orchestrator-model";

function quote(s: string): string {
  // Escapa apenas o que pode quebrar parser: " e \
  const needsQuote = /[:#\[\]{}&*!|>'"%@`\n]/.test(s) || s.trim() !== s || s === "";
  if (!needsQuote && !/^(true|false|null|~|-?\d)/i.test(s)) return s;
  return `"${s.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;
}

function scalar(v: unknown): string {
  if (v === null || v === undefined) return "null";
  if (typeof v === "boolean") return v ? "true" : "false";
  if (typeof v === "number") return String(v);
  if (typeof v === "string") return quote(v);
  throw new Error(`YAML: unsupported scalar: ${typeof v}`);
}

function emit(value: unknown, indent: number): string {
  const pad = "  ".repeat(indent);
  if (Array.isArray(value)) {
    if (value.length === 0) return "[]";
    return "\n" + value.map((v) => {
      if (v !== null && typeof v === "object") {
        const inner = emitObject(v as Record<string, unknown>, indent + 1);
        const lines = inner.split("\n");
        return `${pad}- ${lines[0].trim()}\n${lines.slice(1).join("\n")}`;
      }
      return `${pad}- ${scalar(v)}`;
    }).join("\n");
  }
  if (value !== null && typeof value === "object") {
    return emitObject(value as Record<string, unknown>, indent);
  }
  return scalar(value);
}

function emitObject(obj: Record<string, unknown>, indent: number): string {
  const pad = "  ".repeat(indent);
  const keys = Object.keys(obj).filter((k) => obj[k] !== undefined);
  if (keys.length === 0) return "{}";
  return keys.map((k) => {
    const v = obj[k];
    if (v === null) return `${pad}${k}: null`;
    if (Array.isArray(v) || (typeof v === "object")) {
      const rendered = emit(v, indent + 1);
      if (rendered.startsWith("\n")) return `${pad}${k}:${rendered}`;
      return `${pad}${k}:\n${rendered}`;
    }
    return `${pad}${k}: ${scalar(v)}`;
  }).join("\n");
}

/**
 * Serializa uma JobDefinition como YAML canônico.
 * Chaves em ordem fixa para estabilidade de diff.
 */
export function definitionToYaml(def: JobDefinition): string {
  const ordered: Record<string, unknown> = {
    id: def.id,
    label: def.label,
    jobType: def.jobType,
    team: def.team ?? null,
    schedule: {
      cronExpression: def.schedule.cronExpression,
      enabled: def.schedule.enabled,
      description: def.schedule.description ?? null,
      timezone: def.schedule.timezone ?? null,
    },
    retries: def.retries,
    timeout: def.timeout,
    dryRun: def.dryRun ?? false,
    actionConfig: def.actionConfig ?? null,
    variables: def.variables ?? [],
    upstream: def.upstream ?? [],
  };
  return emitObject(ordered, 0) + "\n";
}

/* ── Parser mínimo ──
   Apenas o subset que `definitionToYaml` produz. Não é parser YAML
   genérico; é um "de-serializador" do formato canônico acima.
*/

interface Line {
  indent: number;
  key?: string;
  value?: string;
  listItem?: boolean;
  raw: string;
}

function tokenize(src: string): Line[] {
  return src.split("\n").filter((l) => l.trim() !== "" && !l.trim().startsWith("#")).map((raw) => {
    const indentMatch = raw.match(/^(\s*)/);
    const indent = indentMatch ? Math.floor(indentMatch[1].length / 2) : 0;
    const trimmed = raw.trim();
    if (trimmed.startsWith("- ")) {
      const rest = trimmed.slice(2);
      const colonIdx = findColon(rest);
      if (colonIdx >= 0) {
        return { indent, key: rest.slice(0, colonIdx).trim(), value: rest.slice(colonIdx + 1).trim(), listItem: true, raw };
      }
      return { indent, value: rest, listItem: true, raw };
    }
    const colonIdx = findColon(trimmed);
    if (colonIdx < 0) return { indent, value: trimmed, raw };
    return { indent, key: trimmed.slice(0, colonIdx).trim(), value: trimmed.slice(colonIdx + 1).trim(), raw };
  });
}

function findColon(s: string): number {
  let inQuote = false;
  for (let i = 0; i < s.length; i++) {
    const c = s[i];
    if (c === '"' && s[i - 1] !== "\\") inQuote = !inQuote;
    if (c === ":" && !inQuote) return i;
  }
  return -1;
}

function parseScalar(v: string): unknown {
  if (v === "" || v === "null" || v === "~") return null;
  if (v === "true") return true;
  if (v === "false") return false;
  if (v === "[]") return [];
  if (v === "{}") return {};
  if (/^-?\d+(\.\d+)?$/.test(v)) return Number(v);
  if (v.startsWith('"') && v.endsWith('"')) {
    return v.slice(1, -1).replace(/\\"/g, '"').replace(/\\\\/g, "\\");
  }
  return v;
}

function parseBlock(lines: Line[], start: number, baseIndent: number): { value: unknown; next: number } {
  const first = lines[start];
  if (!first) return { value: null, next: start };

  // Array?
  if (first.listItem && first.indent === baseIndent) {
    const arr: unknown[] = [];
    let i = start;
    while (i < lines.length && lines[i].indent === baseIndent && lines[i].listItem) {
      const line = lines[i];
      if (line.key !== undefined) {
        // objeto inline no item: - key: val  →  começa objeto; itens subsequentes com indent > baseIndent seguem
        const obj: Record<string, unknown> = {};
        obj[line.key] = line.value !== undefined && line.value !== "" ? parseScalar(line.value) : null;
        i++;
        while (i < lines.length && lines[i].indent > baseIndent && !lines[i].listItem) {
          const sub = lines[i];
          if (sub.key !== undefined) {
            if (sub.value === "" || sub.value === undefined) {
              const { value, next } = parseBlock(lines, i + 1, sub.indent + 1);
              obj[sub.key] = value;
              i = next;
            } else {
              obj[sub.key] = parseScalar(sub.value);
              i++;
            }
          } else {
            i++;
          }
        }
        arr.push(obj);
      } else {
        arr.push(parseScalar(line.value ?? ""));
        i++;
      }
    }
    return { value: arr, next: i };
  }

  // Objeto
  const obj: Record<string, unknown> = {};
  let i = start;
  while (i < lines.length && lines[i].indent === baseIndent && !lines[i].listItem) {
    const line = lines[i];
    if (line.key === undefined) { i++; continue; }
    if (line.value === undefined || line.value === "") {
      const { value, next } = parseBlock(lines, i + 1, baseIndent + 1);
      obj[line.key] = value;
      i = next;
    } else {
      obj[line.key] = parseScalar(line.value);
      i++;
    }
  }
  return { value: obj, next: i };
}

export function yamlToDefinition(yaml: string): JobDefinition {
  const lines = tokenize(yaml);
  const { value } = parseBlock(lines, 0, 0);
  const obj = value as Record<string, unknown>;
  const sched = (obj.schedule ?? {}) as Record<string, unknown>;
  return {
    id: String(obj.id ?? ""),
    label: String(obj.label ?? ""),
    jobType: String(obj.jobType ?? ""),
    team: obj.team ? String(obj.team) : undefined,
    schedule: {
      cronExpression: String(sched.cronExpression ?? ""),
      enabled: Boolean(sched.enabled),
      description: sched.description ? String(sched.description) : undefined,
      timezone: sched.timezone ? String(sched.timezone) : undefined,
    },
    retries: Number(obj.retries ?? 0),
    timeout: Number(obj.timeout ?? 300),
    dryRun: Boolean(obj.dryRun),
    actionConfig: (obj.actionConfig as Record<string, unknown>) ?? undefined,
    variables: (obj.variables as Array<{ key: string; value: string }>) ?? [],
    upstream: (obj.upstream as Array<{ from: string; condition: import("@/lib/orchestrator-model").EdgeCondition }>) ?? [],
  };
}
