/**
 * YAML codec mínimo para JobDefinition.
 *
 * Motivação: evitar dependência pesada (js-yaml ~30KB) por um schema
 * pequeno e fechado. Emite YAML legível em Git diff.
 *
 * Suporta apenas o subset usado por JobDefinition:
 *   - string/number/boolean/null
 *   - listas de objetos (variables)
 *   - objetos aninhados (schedule, actionConfig)
 *
 * Para parsing tolerante de YAML arbitrário, trocar por js-yaml
 * no futuro sem mudar a API desta camada.
 */

import type { JobDefinition } from "@/lib/orchestrator-model";

/* ── Serialize ── */

function escapeString(s: string): string {
  // Se contém caractere especial YAML, quota.
  if (/^[\s-]|[:#&*!|>%@`{}\[\],]|^\d/.test(s) || s === "" || /["'\n]/.test(s)) {
    return `"${s.replace(/\\/g, "\\\\").replace(/"/g, '\\"').replace(/\n/g, "\\n")}"`;
  }
  return s;
}

function emitValue(v: unknown, indent: number): string {
  const pad = "  ".repeat(indent);
  if (v === null || v === undefined) return "null";
  if (typeof v === "string") return escapeString(v);
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  if (Array.isArray(v)) {
    if (v.length === 0) return "[]";
    return v
      .map((item) => {
        if (item !== null && typeof item === "object" && !Array.isArray(item)) {
          const body = emitObject(item as Record<string, unknown>, indent + 1);
          // primeiro campo inline no "-"
          const lines = body.split("\n");
          const first = lines.shift() ?? "";
          const rest = lines.map((l) => pad + "  " + l.replace(/^ {2}/, "")).join("\n");
          return `${pad}- ${first.trimStart()}${rest ? "\n" + rest : ""}`;
        }
        return `${pad}- ${emitValue(item, indent + 1)}`;
      })
      .join("\n");
  }
  if (typeof v === "object") {
    const body = emitObject(v as Record<string, unknown>, indent + 1);
    return "\n" + body;
  }
  return escapeString(String(v));
}

function emitObject(obj: Record<string, unknown>, indent: number): string {
  const pad = "  ".repeat(indent);
  const lines: string[] = [];
  for (const [k, v] of Object.entries(obj)) {
    if (v === undefined) continue;
    if (v !== null && typeof v === "object" && !Array.isArray(v)) {
      lines.push(`${pad}${k}:`);
      lines.push(emitObject(v as Record<string, unknown>, indent + 1));
    } else if (Array.isArray(v)) {
      if (v.length === 0) {
        lines.push(`${pad}${k}: []`);
      } else {
        lines.push(`${pad}${k}:`);
        lines.push(emitValue(v, indent + 1));
      }
    } else {
      lines.push(`${pad}${k}: ${emitValue(v, indent)}`);
    }
  }
  return lines.join("\n");
}

export function toYaml(def: JobDefinition): string {
  const header = `# Regente JobDefinition\n# id: ${def.id}\n`;
  return header + emitObject(def as unknown as Record<string, unknown>, 0) + "\n";
}

/* ── Deserialize (parser minimal, tolera apenas o que emitimos) ── */

interface ParseState {
  lines: string[];
  i: number;
}

function peekIndent(line: string): number {
  return line.length - line.trimStart().length;
}

function unquote(v: string): string {
  const t = v.trim();
  if ((t.startsWith('"') && t.endsWith('"')) || (t.startsWith("'") && t.endsWith("'"))) {
    return t.slice(1, -1).replace(/\\n/g, "\n").replace(/\\"/g, '"').replace(/\\\\/g, "\\");
  }
  return t;
}

function coerce(raw: string): unknown {
  const t = raw.trim();
  if (t === "" || t === "null") return null;
  if (t === "true") return true;
  if (t === "false") return false;
  if (t === "[]") return [];
  if (t.startsWith('"') || t.startsWith("'")) return unquote(t);
  if (/^-?\d+(\.\d+)?$/.test(t)) return Number(t);
  return t;
}

function parseBlock(state: ParseState, baseIndent: number): unknown {
  // Retorna objeto ou array dependendo do primeiro item do bloco.
  const startLine = state.lines[state.i];
  if (startLine === undefined) return {};
  const firstContent = startLine.trim();
  if (firstContent.startsWith("- ")) {
    return parseArray(state, baseIndent);
  }
  return parseObject(state, baseIndent);
}

function parseObject(state: ParseState, baseIndent: number): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  while (state.i < state.lines.length) {
    const line = state.lines[state.i];
    if (line.trim() === "" || line.trim().startsWith("#")) {
      state.i++;
      continue;
    }
    const indent = peekIndent(line);
    if (indent < baseIndent) break;
    if (indent > baseIndent) {
      // bloco filho já foi consumido, não deveria acontecer aqui
      state.i++;
      continue;
    }
    const content = line.slice(indent);
    const colonIdx = content.indexOf(":");
    if (colonIdx < 0) break;
    const key = content.slice(0, colonIdx).trim();
    const rest = content.slice(colonIdx + 1).trim();
    state.i++;
    if (rest === "") {
      // bloco filho
      const next = state.lines[state.i];
      if (next === undefined || peekIndent(next) <= baseIndent) {
        out[key] = {};
      } else {
        out[key] = parseBlock(state, peekIndent(next));
      }
    } else {
      out[key] = coerce(rest);
    }
  }
  return out;
}

function parseArray(state: ParseState, baseIndent: number): unknown[] {
  const out: unknown[] = [];
  while (state.i < state.lines.length) {
    const line = state.lines[state.i];
    if (line.trim() === "" || line.trim().startsWith("#")) {
      state.i++;
      continue;
    }
    const indent = peekIndent(line);
    if (indent < baseIndent) break;
    const content = line.slice(indent);
    if (!content.startsWith("- ")) break;
    const rest = content.slice(2);
    state.i++;
    const colonIdx = rest.indexOf(":");
    if (colonIdx >= 0) {
      // objeto inline "- key: value"
      const key = rest.slice(0, colonIdx).trim();
      const val = rest.slice(colonIdx + 1).trim();
      const item: Record<string, unknown> = {};
      item[key] = val === "" ? parseBlock(state, baseIndent + 2) : coerce(val);
      // campos adicionais do mesmo item (indent = baseIndent + 2)
      while (state.i < state.lines.length) {
        const l2 = state.lines[state.i];
        if (l2.trim() === "") { state.i++; continue; }
        const i2 = peekIndent(l2);
        if (i2 !== baseIndent + 2) break;
        const c2 = l2.slice(i2);
        const ci2 = c2.indexOf(":");
        if (ci2 < 0) break;
        const k2 = c2.slice(0, ci2).trim();
        const v2 = c2.slice(ci2 + 1).trim();
        state.i++;
        if (v2 === "") {
          item[k2] = parseBlock(state, baseIndent + 4);
        } else {
          item[k2] = coerce(v2);
        }
      }
      out.push(item);
    } else {
      out.push(coerce(rest));
    }
  }
  return out;
}

export function fromYaml(text: string): JobDefinition {
  const raw = text.replace(/\r\n/g, "\n").split("\n");
  const state: ParseState = { lines: raw, i: 0 };
  const obj = parseObject(state, 0);
  return obj as unknown as JobDefinition;
}
