import type { JobType } from "@/lib/job-config";

/* ──────────────────────────────────────────────────────────────
   F12 — Per-jobType editor for `actionConfig` (Record<string,unknown>)
   ──────────────────────────────────────────────────────────────
   Cada jobType decide quais campos exibir. Schemas mantidos
   simples (form-based, sem Zod) para v1.
   ────────────────────────────────────────────────────────────── */

interface Props {
  jobType: JobType;
  config: Record<string, unknown>;
  onChange: (next: Record<string, unknown>) => void;
}

export default function JobActionConfigEditor({ jobType, config, onChange }: Props) {
  const set = (k: string, v: unknown) => onChange({ ...config, [k]: v });
  const str = (k: string) => (typeof config[k] === "string" ? (config[k] as string) : "");
  const num = (k: string) => (typeof config[k] === "number" ? (config[k] as number) : 0);
  const get = <T,>(k: string, def: T): T => (config[k] === undefined ? def : (config[k] as T));

  switch (jobType) {
    case "COMMAND":
      return (
        <Section label="Comando no agente">
          <Row label="Comando (sh -c no Linux / powershell no Windows)">
            <TextArea mono rows={3} value={str("command")} placeholder="echo hello && python /app/run.py --date {DATE}" onChange={v => set("command", v)} />
          </Row>
          <Row label="Working dir (cwd, opcional)">
            <Input mono value={str("cwd")} placeholder="/opt/app   ou   C:\\app" onChange={v => set("cwd", v)} />
          </Row>
        </Section>
      );

    case "SCRIPT":
      return (
        <Section label="Script no agente">
          <Row label="Caminho do script (.sh / .bat / .ps1 no agente)">
            <Input mono value={str("scriptPath")} placeholder="/opt/jobs/load.sh   ou   C:\\jobs\\load.ps1" onChange={v => set("scriptPath", v)} />
          </Row>
          <Row label="Argumentos (opcional)">
            <Input mono value={str("args")} placeholder="--date {DATE} --full" onChange={v => set("args", v)} />
          </Row>
          <Row label="Working dir (cwd, opcional)">
            <Input mono value={str("cwd")} placeholder="/opt/jobs" onChange={v => set("cwd", v)} />
          </Row>
        </Section>
      );

    case "SSH":
      return (
        <Section label="SSH remoto (agentless)">
          <Row label="Host">
            <Input mono value={str("host")} placeholder="10.0.0.5 / ec2-...compute.amazonaws.com" onChange={v => set("host", v)} />
          </Row>
          <Row label="Usuário">
            <Input mono value={str("user")} placeholder="ubuntu / ec2-user" onChange={v => set("user", v)} />
          </Row>
          <Row label="Porta (default 22)">
            <Input mono value={str("port")} placeholder="22" onChange={v => set("port", v)} />
          </Row>
          <Row label="Comando">
            <TextArea mono rows={3} value={str("command")} placeholder="systemctl restart app && echo ok" onChange={v => set("command", v)} />
          </Row>
          <Row label="Chave privada (keyPath, opcional)">
            <Input mono value={str("keyPath")} placeholder="/home/user/.ssh/id_rsa" onChange={v => set("keyPath", v)} />
          </Row>
        </Section>
      );

    case "FILE_WATCH":
      return (
        <Section label="File Watch (espera arquivo no host do agente)">
          <Row label="Caminho do arquivo (no host do agente)">
            <Input mono value={str("path")} placeholder="/data/in/carga.csv   ou   C:\\entrada\\arquivo.dat" onChange={v => set("path", v)} />
          </Row>
          <Row label="Intervalo do poll em segundos (default 5)">
            <Input mono value={num("intervalSec") ? String(num("intervalSec")) : ""} placeholder="5" onChange={v => set("intervalSec", Number(v) || 0)} />
          </Row>
          <Row label="Estabilidade em segundos (0 = basta existir; >0 = tamanho parado por N s)">
            <Input mono value={num("stableSec") ? String(num("stableSec")) : ""} placeholder="0" onChange={v => set("stableSec", Number(v) || 0)} />
          </Row>
        </Section>
      );

    case "FILE_TRANSFER":
      return (
        <Section label="File Transfer (MFT pelo agente)">
          <Row label="Origem (local, sftp://user:pass@host/caminho ou s3://bucket/chave; glob em local/sftp)">
            <Input mono value={str("src")} placeholder="/data/out/fech_*.csv   ou   s3://bucket/entrada/carga.csv" onChange={v => set("src", v)} />
          </Row>
          <Row label="Destino (termine com / para diretório/prefixo)">
            <Input mono value={str("dst")} placeholder="sftp://svc@10.0.0.5:22/entrada/" onChange={v => set("dst", v)} />
          </Row>
          <Row label="Verificação SHA-256 (relê o destino)">
            <BoolSelect value={get("checksum", false)} onChange={v => set("checksum", v)} />
          </Row>
          <Row label="Remover origem após transferir (move)">
            <BoolSelect value={get("deleteSource", false)} onChange={v => set("deleteSource", v)} />
          </Row>
          <Row label="Sobrescrever destino existente">
            <BoolSelect value={get("overwrite", true)} onChange={v => set("overwrite", v)} />
          </Row>
          <Row label="Chave privada sftp (keyPath, opcional)">
            <Input mono value={str("keyPath")} placeholder="/home/agent/.ssh/id_rsa" onChange={v => set("keyPath", v)} />
          </Row>
        </Section>
      );

    case "DATABASE":
      return (
        <Section label="Database (SQL pelo agente)">
          <Row label="Driver">
            <select value={str("driver") || "postgres"} onChange={e => set("driver", e.target.value)} style={selectStyle}>
              {["postgres", "mysql", "sqlite"].map(d => <option key={d} value={d}>{d}</option>)}
            </select>
          </Row>
          <Row label="DSN / conexão">
            <Input mono value={str("dsn")} placeholder="postgres://user:pw@host:5432/db?sslmode=disable" onChange={v => set("dsn", v)} />
          </Row>
          <Row label="SQL (SELECT renderiza linhas; DML mostra rows affected)">
            <TextArea mono rows={5} value={str("sql")} placeholder="SELECT count(*) FROM cargas WHERE dt = '%%ORDERDATE'" onChange={v => set("sql", v)} />
          </Row>
          <Row label="Máx. de linhas no output (SELECT, default 100)">
            <Input mono value={num("maxRows") ? String(num("maxRows")) : ""} placeholder="100" onChange={v => set("maxRows", Number(v) || 0)} />
          </Row>
        </Section>
      );

    case "HTTP":
      return (
        <Section label="HTTP request">
          <Row label="Method">
            <select value={str("method") || "GET"} onChange={e => set("method", e.target.value)} style={selectStyle}>
              {["GET", "POST", "PUT", "PATCH", "DELETE"].map(m => <option key={m} value={m}>{m}</option>)}
            </select>
          </Row>
          <Row label="URL">
            <Input mono value={str("url")} placeholder="https://api.example.com/path" onChange={v => set("url", v)} />
          </Row>
          <Row label="Headers (JSON)">
            <TextArea mono rows={3} value={jsonStr(get("headers", {}))} placeholder='{"X-Token":"..."}' onChange={v => set("headers", parseJson(v) ?? {})} />
          </Row>
          <Row label="Body">
            <TextArea mono rows={4} value={str("body")} onChange={v => set("body", v)} />
          </Row>
          <Row label="Expected status (default 2xx)">
            <Input mono value={str("expectStatus")} placeholder="200,204" onChange={v => set("expectStatus", v)} />
          </Row>
        </Section>
      );

    case "LAMBDA":
      return (
        <Section label="Lambda invoke">
          <Row label="Function name / ARN">
            <Input mono value={str("functionName")} onChange={v => set("functionName", v)} />
          </Row>
          <Row label="Region">
            <Input mono value={str("region") || "us-east-1"} onChange={v => set("region", v)} />
          </Row>
          <Row label="Payload (JSON)">
            <TextArea mono rows={5} value={jsonStr(get("payload", {}))} onChange={v => set("payload", parseJson(v) ?? {})} />
          </Row>
          <Row label="Invocation type">
            <select value={str("invocationType") || "RequestResponse"} onChange={e => set("invocationType", e.target.value)} style={selectStyle}>
              <option>RequestResponse</option>
              <option>Event</option>
            </select>
          </Row>
        </Section>
      );

    case "BATCH":
      return (
        <Section label="Batch / Container">
          <Row label="Job queue">
            <Input mono value={str("jobQueue")} onChange={v => set("jobQueue", v)} />
          </Row>
          <Row label="Job definition">
            <Input mono value={str("jobDefinition")} onChange={v => set("jobDefinition", v)} />
          </Row>
          <Row label="Command (espacos = args)">
            <Input mono value={str("command")} onChange={v => set("command", v)} placeholder="python /app/run.py --date {DATE}" />
          </Row>
          <Row label="Environment (JSON)">
            <TextArea mono rows={3} value={jsonStr(get("env", {}))} onChange={v => set("env", parseJson(v) ?? {})} />
          </Row>
          <Row label="Region (default env do agente)">
            <Input mono value={str("region")} placeholder="sa-east-1" onChange={v => set("region", v)} />
          </Row>
        </Section>
      );

    case "GLUE":
      return (
        <Section label="Glue ETL">
          <Row label="Job name">
            <Input mono value={str("jobName")} onChange={v => set("jobName", v)} />
          </Row>
          <Row label="Arguments (JSON)">
            <TextArea mono rows={4} value={jsonStr(get("arguments", {}))} placeholder='{"--source":"s3://..."}' onChange={v => set("arguments", parseJson(v) ?? {})} />
          </Row>
          <Row label="Worker type">
            <select value={str("workerType") || "G.1X"} onChange={e => set("workerType", e.target.value)} style={selectStyle}>
              {["G.1X", "G.2X", "G.4X", "Standard"].map(w => <option key={w}>{w}</option>)}
            </select>
          </Row>
          <Row label="# workers">
            <Input mono value={String(num("numberOfWorkers") || 2)} onChange={v => set("numberOfWorkers", Number(v) || 0)} />
          </Row>
          <Row label="Region (default env do agente)">
            <Input mono value={str("region")} placeholder="sa-east-1" onChange={v => set("region", v)} />
          </Row>
        </Section>
      );

    case "STEP_FUNCTION":
      return (
        <Section label="Step Function">
          <Row label="State machine ARN">
            <Input mono value={str("stateMachineArn")} onChange={v => set("stateMachineArn", v)} />
          </Row>
          <Row label="Input (JSON)">
            <TextArea mono rows={5} value={jsonStr(get("input", {}))} onChange={v => set("input", parseJson(v) ?? {})} />
          </Row>
          <Row label="Region (default env do agente)">
            <Input mono value={str("region")} placeholder="sa-east-1" onChange={v => set("region", v)} />
          </Row>
        </Section>
      );

    default:
      return (
        <Section label="Action config (raw JSON)">
          <TextArea mono rows={6} value={jsonStr(config)} onChange={v => onChange((parseJson(v) as Record<string, unknown> | null) ?? {})} />
        </Section>
      );
  }
}

function Section({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{
      border: "1px solid var(--v2-border-subtle)", borderRadius: 4, padding: 8,
      display: "grid", gap: 8, background: "var(--v2-bg-canvas)",
    }}>
      <div style={{ fontSize: 9, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--v2-accent-brand)" }}>
        {label}
      </div>
      {children}
    </div>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div style={{ fontSize: 9, letterSpacing: "0.06em", textTransform: "uppercase", color: "var(--v2-text-muted)", marginBottom: 3 }}>{label}</div>
      {children}
    </div>
  );
}

function BoolSelect({ value, onChange }: { value: boolean; onChange: (v: boolean) => void }) {
  return (
    <select value={value ? "true" : "false"} onChange={e => onChange(e.target.value === "true")} style={selectStyle}>
      <option value="false">não</option>
      <option value="true">sim</option>
    </select>
  );
}

function Input({ value, onChange, placeholder, mono }: { value: string; onChange: (v: string) => void; placeholder?: string; mono?: boolean }) {
  return (
    <input value={value} placeholder={placeholder} onChange={e => onChange(e.target.value)}
      style={{ width: "100%", background: "var(--v2-bg-surface)", border: "1px solid var(--v2-border-subtle)",
        color: "var(--v2-text-primary)", padding: "5px 8px", fontSize: 11,
        fontFamily: mono ? "var(--v2-font-mono)" : "var(--v2-font-sans)", borderRadius: 3, boxSizing: "border-box" }} />
  );
}

function TextArea({ value, onChange, rows, placeholder, mono }: { value: string; onChange: (v: string) => void; rows?: number; placeholder?: string; mono?: boolean }) {
  return (
    <textarea value={value} placeholder={placeholder} rows={rows ?? 3} onChange={e => onChange(e.target.value)}
      style={{ width: "100%", background: "var(--v2-bg-surface)", border: "1px solid var(--v2-border-subtle)",
        color: "var(--v2-text-primary)", padding: "5px 8px", fontSize: 11,
        fontFamily: mono ? "var(--v2-font-mono)" : "var(--v2-font-sans)", borderRadius: 3, resize: "vertical", boxSizing: "border-box" }} />
  );
}

const selectStyle: React.CSSProperties = {
  width: "100%", background: "var(--v2-bg-surface)", border: "1px solid var(--v2-border-subtle)",
  color: "var(--v2-text-primary)", padding: "5px 8px", fontSize: 11, fontFamily: "var(--v2-font-mono)", borderRadius: 3, boxSizing: "border-box",
};

function jsonStr(v: unknown): string {
  try { return JSON.stringify(v ?? {}, null, 2); } catch { return ""; }
}
function parseJson(s: string): unknown | null {
  if (!s.trim()) return null;
  try { return JSON.parse(s); } catch { return null; }
}
