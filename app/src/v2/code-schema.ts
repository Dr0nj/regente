/**
 * code-schema.ts — GUIA COMPLETA do dialeto YAML do workspace (modo código).
 *
 * Fonte da verdade espelhada daqui: `server/internal/domain/model.go`
 * (JobDefinition/Schedule/ActionRule/SLASpec/SubWorkflowRef/CalendarRef),
 * `domain/validate.go` (obrigatórios por jobType), `scheduler/conditions.go`,
 * `scheduler/calendars.go` (advancedRule) e os executores do agente.
 * Se um campo novo entrar no modelo Go, ELE ENTRA AQUI — a guia do editor
 * promete cobrir TODAS as tags, sem exceção.
 *
 * Texto voltado ao usuário: INGLÊS (a UI inteira é em inglês).
 */

export interface GuideForm {
  /** A forma/valor aceitável (ex.: `"weekly"`, `-1`, `"on-failure"`). */
  form: string;
  desc: string;
}

export interface GuideEntry {
  /** Nome da tag como aparece no YAML. */
  tag: string;
  /** Tipo/natureza (string, bool, lista…) + obrigatoriedade. */
  kind: string;
  /** Uma frase: o que a tag faz. */
  summary: string;
  /** Explicação das formas de preencher / semântica de runtime. */
  detail?: string;
  /** Valores/formatos aceitáveis, um a um. */
  forms?: GuideForm[];
  /** Exemplo YAML pronto pra colar. */
  example?: string;
  /** Sub-tags (ex.: schedule.*) ou variações (ex.: um doc por jobType). */
  children?: GuideEntry[];
}

/* ── params por jobType (espelha domain/validate.go + executores do agente) ── */

const JOB_TYPE_CHILDREN: GuideEntry[] = [
  {
    tag: "COMMAND",
    kind: "jobType",
    summary: "Shell command on the agent (Windows runs it through powershell).",
    detail: "params: `command` (the command itself; essential) · `cwd` (working directory). The agent must advertise the COMMAND capability.",
    example: `jobType: COMMAND
params:
  command: "echo ODATE=%%ODATE"
  cwd: /tmp`,
  },
  {
    tag: "SCRIPT",
    kind: "jobType",
    summary: "A .sh/.bat/.ps1 script on the agent host.",
    detail: "params: `scriptPath` (path to the script) · `args` (arguments) · `cwd`.",
    example: `jobType: SCRIPT
params:
  scriptPath: /opt/jobs/backup.sh
  args: "--full"`,
  },
  {
    tag: "SSH",
    kind: "jobType",
    summary: "Remote command over SSH — AGENTLESS (no agent capability required).",
    detail: "params: `host` · `user` · `port` · `command` · `keyPath` (private key).",
    example: `jobType: SSH
params:
  host: 10.0.0.7
  user: batch
  command: "systemctl restart etl"`,
  },
  {
    tag: "HTTP",
    kind: "jobType · alias REST",
    summary: "REST call.",
    detail: "params: `url` (REQUIRED) · `method` GET|POST|PUT|PATCH|DELETE · `headers` (map) · `body` · `expectStatus` — accepts `200`, `[200, 204]` or `\"200,204\"`; any status outside them = NOTOK.",
    example: `jobType: HTTP
params:
  method: POST
  url: https://api.internal/reconcile
  headers: { Authorization: "Bearer \${var.TOKEN}" }
  expectStatus: 200`,
  },
  {
    tag: "DATABASE",
    kind: "jobType · alias DB",
    summary: "SQL against Postgres/MySQL/SQLite from the agent (pure-Go drivers).",
    detail: "REQUIRED params: `driver` (postgres|mysql|sqlite — aliases pg/pgx/postgresql/mariadb/sqlite3 accepted) · `dsn` · `sql`. Optional: `maxRows` (rows rendered from a SELECT). SELECT prints rows; DML prints rows-affected.",
    example: `jobType: DATABASE
params:
  driver: postgres
  dsn: "postgres://user:pass@db:5432/fin"
  sql: "DELETE FROM staging WHERE dt < '%%ODATE'"`,
  },
  {
    tag: "FILE_WATCH",
    kind: "jobType · alias FILEWATCH",
    summary: "Waits for a file to land on the agent host.",
    detail: "params: `path` (REQUIRED — path on the agent host) · `intervalSec` (poll, default 5) · `stableSec` (size must stay stable for N s before OK). The job `timeout` ends the wait with NOTOK.",
    example: `jobType: FILE_WATCH
timeout: 3600
params:
  path: /data/in/cadoc_%%ODATE.txt
  stableSec: 10`,
  },
  {
    tag: "FILE_TRANSFER",
    kind: "jobType · alias MFT",
    summary: "Native MFT: transfers files between local, SFTP and S3 from the agent.",
    detail: "REQUIRED params: `src` · `dst` — a local path, `sftp://user:pass@host:22/path` or `s3://bucket/key`; glob (`*.csv`) allowed on local/sftp sources; a destination ending in `/` is a directory/prefix (required with glob). Optional: `checksum` (re-reads the destination and compares SHA-256) · `deleteSource` (move) · `overwrite` (false = fail if the destination exists) · `mkdirs` (default true) · sftp: `keyPath`/`password`/`hostKeyFingerprint` (SHA256 pin) · S3: `region`/`accessKeyId`/`secretAccessKey`/`sessionToken` (defaults to the agent's AWS_* envs) and `s3Endpoint` (MinIO/tests). Atomic write (.part+rename): a FILE_WATCH on the other side never sees a partial file.",
    example: `jobType: FILE_TRANSFER
timeout: 1800
params:
  src: /data/out/close_%%ODATE_*.csv
  dst: sftp://svc@mainframe:22/inbound/
  checksum: true
  deleteSource: true`,
  },
  {
    tag: "LAMBDA",
    kind: "jobType · alias AWS_LAMBDA",
    summary: "Invokes an AWS Lambda function (SigV4 from the agent, no SDK).",
    detail: "params: `function` (REQUIRED — `functionName` accepted as an alias) · `region` (defaults to the agent's AWS_REGION env) · `payload` (JSON string or object) · `accessKeyId`/`secretAccessKey`/`sessionToken` (default to the AWS_* envs) · `endpoint` (tests) · `invocationType` (reserved; the current invocation is synchronous).",
    example: `jobType: LAMBDA
params:
  function: fin-reconcile
  region: sa-east-1`,
  },
  {
    tag: "BATCH",
    kind: "jobType · alias AWS_BATCH",
    summary: "Container/batch job (AWS Batch) — SubmitJob + polling from the agent (SigV4, no SDK).",
    detail: "REQUIRED params: `jobQueue` · `jobDefinition`. Optional: `jobName` · `command` (split on spaces) · `env` (map) · `parameters` (map) · `region` (defaults to the agent's AWS_REGION env) · `accessKeyId`/`secretAccessKey`/`sessionToken` (default to the AWS_* envs) · `endpoint` (tests). The agent follows it until SUCCEEDED/FAILED; on job timeout it sends TerminateJob.",
    example: `jobType: BATCH
timeout: 3600
params:
  jobQueue: fin-fargate
  jobDefinition: bacen-validator
  env: { REF_DATE: "%%ODATE" }`,
  },
  {
    tag: "GLUE",
    kind: "jobType · alias AWS_GLUE",
    summary: "ETL job (AWS Glue) — StartJobRun + polling from the agent (SigV4, no SDK).",
    detail: "params: `jobName` (REQUIRED) · `arguments` (map) · `workerType` · `numberOfWorkers` · `region` (defaults to the agent's AWS_REGION env) · `accessKeyId`/`secretAccessKey`/`sessionToken` (default to the AWS_* envs) · `endpoint` (tests). The agent follows it until SUCCEEDED/FAILED/TIMEOUT/STOPPED; on job timeout it sends BatchStopJobRun.",
    example: `jobType: GLUE
params:
  jobName: cadoc-3040
  arguments: { "--REF_DATE": "%%ODATE" }`,
  },
  {
    tag: "STEP_FUNCTION",
    kind: "jobType · alias STEP_FUNCTIONS",
    summary: "Fires a state machine (AWS Step Functions) — StartExecution + polling from the agent (SigV4, no SDK).",
    detail: "params: `stateMachineArn` (REQUIRED) · `input` (JSON string or object) · `name` (default: AWS generates one) · `region` (defaults to the agent's AWS_REGION env) · `accessKeyId`/`secretAccessKey`/`sessionToken` (default to the AWS_* envs) · `endpoint` (tests). The agent follows it until SUCCEEDED/FAILED/TIMED_OUT/ABORTED; on job timeout it sends StopExecution.",
    example: `jobType: STEP_FUNCTION
params:
  stateMachineArn: arn:aws:states:sa-east-1:123:stateMachine/etl`,
  },
  {
    tag: "WASM",
    kind: "jobType",
    summary: "WASI WebAssembly module on the agent (wazero — sandboxed by construction).",
    detail: "params: `wasmPath` OR `wasmUrl` (one of the two is REQUIRED) · `args` (string) · `stdin` (string). The module must be a WASI command (exports _start).",
    example: `jobType: WASM
params:
  wasmUrl: https://repo.internal/mod.wasm
  args: "--full"`,
  },
  {
    tag: "K8S",
    kind: "jobType · alias K8S_JOB",
    summary: "Kubernetes Job from the agent (talks to the API server directly, no kubectl).",
    detail: "params: `image` (REQUIRED) · `command` · `namespace` (default default) · `name` · `apiServer`/`token` (empty = in-cluster) · `insecureTLS` (bool).",
    example: `jobType: K8S
params:
  image: alpine:3
  command: "echo hi"`,
  },
  {
    tag: "GCP_RUN",
    kind: "jobType · alias CLOUD_RUN_JOB",
    summary: "Fires a Cloud Run Job (Run Admin API v2) from the agent.",
    detail: "REQUIRED params: `project` · `region` · `job`. Optional: `token` (defaults to the GOOGLE_OAUTH_TOKEN env or the metadata server) · `endpoint` (tests).",
    example: `jobType: GCP_RUN
params:
  project: fin-prod
  region: us-central1
  job: reconcile`,
  },
  {
    tag: "— per-type schema (ADV-1)",
    kind: "validation",
    summary: "Every known jobType has a DEDICATED schema on the server (GET /api/jobtypes).",
    detail: "Lint/validation checks params AGAINST the type schema: an unknown field (a typo such as `comand:`) or a wrong value type is an error RIGHT AWAY; REQUIRED params (command/url/…) are enforced at PUBLISH (an incomplete draft can still be saved). A jobType the server does not know is accepted with free-form params — but it only runs if some agent advertises the matching capability; otherwise it sits in WAIT AGENT.",
  },
];

/* ── schedule.* ── */

const SCHEDULE_CHILDREN: GuideEntry[] = [
  {
    tag: "enabled",
    kind: "bool · required",
    summary: "false = the job does NOT enter the daily (shows as INACTIVE in Design).",
    forms: [
      { form: "true", desc: "scheduled — the daily materializes the instance on eligible days" },
      { form: "false", desc: "off — only runs through Force Order" },
    ],
  },
  {
    tag: "description",
    kind: "string",
    summary: "Free text shown in the UI (General tab of the drawer).",
  },
  {
    tag: "frequency",
    kind: "string",
    summary: "WHICH DAYS the job runs on (evaluated by the daily).",
    forms: [
      { form: '"" | "daily"', desc: "every day (default)" },
      { form: '"weekly"', desc: "on the days listed in `daysOfWeek`" },
      { form: '"monthly"', desc: "on the days listed in `daysOfMonth`" },
      { form: '"businessday"', desc: "on the Nth BUSINESS day of the month (`nthBusinessDays`)" },
      { form: '"advanced"', desc: "named rule in `advancedRule`" },
    ],
  },
  {
    tag: "daysOfWeek",
    kind: "list of string · frequency weekly",
    summary: "Days of the week it runs on.",
    forms: [{ form: '"mon" "tue" "wed" "thu" "fri" "sat" "sun"', desc: "any combination" }],
    example: `schedule:
  enabled: true
  frequency: weekly
  daysOfWeek: [mon, wed, fri]`,
  },
  {
    tag: "daysOfMonth",
    kind: "list of int · frequency monthly",
    summary: "Days of the month it runs on.",
    forms: [
      { form: "1..31", desc: "fixed day of the month" },
      { form: "-1", desc: "LAST day of the month" },
    ],
    example: `schedule:
  enabled: true
  frequency: monthly
  daysOfMonth: [1, 15, -1]`,
  },
  {
    tag: "nthBusinessDays",
    kind: "list of int · frequency businessday",
    summary: "Nth BUSINESS day of the month (holidays come from the job's calendar).",
    forms: [
      { form: "5", desc: "5th business day of the month" },
      { form: "-1", desc: "LAST business day of the month" },
    ],
    example: `schedule:
  enabled: true
  frequency: businessday
  nthBusinessDays: [1, -1]`,
  },
  {
    tag: "monthsOfYear",
    kind: "list of int (1..12)",
    summary: "Filters MONTHS on any frequency (empty = all of them).",
    example: `schedule:
  enabled: true
  frequency: monthly
  daysOfMonth: [-1]
  monthsOfYear: [3, 6, 9, 12]   # quarter-end only`,
  },
  {
    tag: "advancedRule",
    kind: "string · frequency advanced",
    summary: "Named business-day rules (calendar-aware).",
    forms: [
      { form: '"first-businessday"', desc: "1st business day of the month" },
      { form: '"last-businessday"', desc: "last business day of the month" },
      { form: '"first-businessday-not-monday"', desc: "1st business day that is NOT a Monday" },
      { form: '"penultimate-businessday"', desc: "second-to-last business day of the month" },
    ],
  },
  {
    tag: "shift",
    kind: "string",
    summary: "What to do when the NOMINAL day is not eligible (holiday/exclude; with no calendar, the weekend). Control-M \"roll\".",
    forms: [
      { form: '"" | "none"', desc: "skips that cycle (classic)" },
      { form: '"next-businessday"', desc: "rolls to the NEXT eligible day" },
      { form: '"prev-businessday"', desc: "PULLS IT FORWARD to the previous eligible day" },
    ],
  },
  {
    tag: "runAt",
    kind: 'string "HH:MM"',
    summary: "Time at which the instance becomes ELIGIBLE after the daily (before that: WAIT).",
  },
  {
    tag: "windowFrom / windowTo",
    kind: 'string "HH:MM"',
    summary: "Allowed execution window. Past windowTo, a WAITING instance moves to the WINDOW_CLOSED gate (no more submissions today).",
    example: `schedule:
  enabled: true
  windowFrom: "22:00"
  windowTo: "05:00"`,
  },
  {
    tag: "cyclic + intervalMin",
    kind: "bool + int (minutes)",
    summary: "Repeating job: once it ends OK, the SAME instance re-arms for +intervalMin.",
    detail: "NOTOK does not cycle (it waits for an operator). The cycle respects windowTo and dies at the daily rollover. `cycleRuns` counts the laps on the instance.",
    example: `schedule:
  enabled: true
  cyclic: true
  intervalMin: 15
  windowTo: "18:00"`,
  },
  {
    tag: "cyclicMaxRuns",
    kind: "int",
    summary: "Cap on cycle laps (0 = no cap — the window/rollover decides).",
  },
  {
    tag: "keepActive",
    kind: "int (dailies)",
    summary: "Carry-over: how many EXTRA dailies the job survives without ending OK.",
    detail: "0 = default (an untreated NOTOK survives +1 daily; a WAITING that never ran does NOT survive). >0 = survives N dailies. RUNNING and HELD always survive. Pairs with `confirm`/`retryDelayMin` for human-in-the-loop workflows that span days.",
  },
  {
    tag: "cronExpression",
    kind: "string · LEGACY",
    summary: "5-field cron. Only used as the source when `frequency` is empty. Prefer the structured frequency.",
  },
];

/* ── actions.* (On/Do) ── */

const ACTIONS_CHILDREN: GuideEntry[] = [
  {
    tag: "on",
    kind: "string · required on the rule",
    summary: "The rule TRIGGER.",
    forms: [
      { form: '"result"', desc: "the job's TERMINAL status (after retries are exhausted) — qualify it with `status`" },
      { form: '"exit"', desc: "the job's TERMINAL exit code (Control-M COMPSTAT) — qualify it with `exitCodes`" },
      { form: '"attempt"', desc: "the Nth attempt FAILED (rerun ladder) — qualify it with `attempt`" },
      { form: '"runtime"', desc: "job RUNNING for more than N minutes (shout) — qualify it with `afterMin`" },
    ],
  },
  { tag: "status", kind: 'string · on: "result"', summary: 'Which result fires it: "OK" or "NOTOK".' },
  {
    tag: "exitCodes",
    kind: 'string · on: "exit"',
    summary: "Exit codes that fire the rule, comma-separated (OR between the items).",
    detail: "Each item is a value (`3`), a range (`1-4`) or a comparison (`>0`, `>=8`, `<0`, `<=2`, `!=0`). Empty NEVER fires. The status comes from the code (`exit != 0` ⇒ NOTOK), so `on: exit` + `do: set-ok` is the \"treat these codes as success\" rule. Killing a RUNNING job (Cancel) records exit `-1`.",
    forms: [
      { form: '"1,2,3"', desc: "any of the three" },
      { form: '"1-4"', desc: "inclusive range" },
      { form: '">0"', desc: "comparison" },
    ],
  },
  { tag: "attempt", kind: 'int · on: "attempt"', summary: "Attempt number (1-based) whose failure fires the rule." },
  { tag: "afterMin", kind: 'int · on: "runtime"', summary: "Minutes of RUNNING that fire the rule." },
  {
    tag: "do",
    kind: "string · required on the rule",
    summary: "The ACTION fired (each rule fires AT MOST once per instance).",
    forms: [
      { form: '"notify"', desc: "alert on the channels (uses message/severity/channels)" },
      { form: '"set-condition"', desc: "sets the global condition for the order_date (unblocks successors) — uses `condition`" },
      { form: '"run-job"', desc: "Force Order on `targetJob` (runs another job, ignoring its deps)" },
      { form: '"set-ok"', desc: "flips THIS job from NOTOK to OK (only makes sense with on result NOTOK)" },
    ],
  },
  { tag: "message", kind: "string · do notify", summary: "Alert text (interpolates %%VARS)." },
  {
    tag: "severity",
    kind: "string · do notify",
    summary: "Alert severity.",
    forms: [{ form: '"info" | "warning" | "critical"', desc: "critical/warning become error toasts in the UI" }],
  },
  {
    tag: "channels",
    kind: "list of string · do notify",
    summary: "Output channels; EMPTY = every channel configured in Settings.",
    forms: [{ form: '"slack" "webhook" "email" "pagerduty"', desc: "any combination" }],
  },
  { tag: "condition", kind: "string · do set-condition", summary: "Name of the global condition to set." },
  { tag: "targetJob", kind: "string · do run-job", summary: "id of the definition to force." },
];

/* ── A GUIA (toda tag top-level do YAML) ── */

export const YAML_GUIDE: GuideEntry[] = [
  {
    tag: "— the document",
    kind: "general rules",
    summary: "How the editor reads the YAML (the EXACT dialect of the Git workspace files).",
    detail:
      "Several jobs in one text: separate them with `---` (multi-doc), ONE job per document. The `# definitions/<folder>/<id>.yaml` comment the server generates is informational. Parsing is STRICT: an unknown field/typo (e.g. `retires:`) is a validation error, nothing is silently ignored. A job present in the working set and MISSING from the text is marked for DELETE (Apply asks for confirmation and lists the ids). `team` can be omitted when a SINGLE folder is open (it becomes the scope default).",
    example: `# definitions/fin/extract.yaml
id: extract
label: Extract FIN
team: fin
jobType: COMMAND
schedule:
  enabled: true
params:
  command: "run-extract.sh"
---
id: load
label: Load FIN
team: fin
jobType: COMMAND
schedule:
  enabled: true
upstream:
  - from: extract
params:
  command: "run-load.sh"`,
  },
  {
    tag: "id",
    kind: "string · REQUIRED",
    summary: "UNIQUE identifier of the job in the workspace — becomes the file definitions/<team>/<id>.yaml.",
    detail:
      "Accepted form: a stable, lowercase slug with no spaces (letters/digits/dashes — e.g. `extract-fin`, `cadoc-3040`). It is the reference used in `upstream.from`, `actions.targetJob` and in the API. Renaming the id turns the plan into CREATE the new one + DELETE the old one (instance history does not migrate).",
    example: `id: extract-fin`,
  },
  {
    tag: "label",
    kind: "string · REQUIRED",
    summary: "Display name on cards, lists and drawers.",
    example: `label: Extract FIN (daily)`,
  },
  {
    tag: "team",
    kind: "string · REQUIRED (inferable)",
    summary: "The job's folder (the subdirectory under definitions/).",
    detail: "In code mode with a SINGLE folder open it can be omitted (the scope folder is assumed). With several folders open it is required in every doc. Moving between folders = Save in the new one + Delete in the old one (the plan shows it).",
    example: `team: fin`,
  },
  {
    tag: "jobType",
    kind: "string · REQUIRED",
    summary: "WHAT the job runs. Expand to see every type ALREADY IMPLEMENTED and the params of each one.",
    detail: "The agent that will run it must advertise the type's capability (except SSH, which is agentless). With no capable agent online, the instance sits at the WAIT AGENT gate (light blue card) — it is not even claimed.",
    children: JOB_TYPE_CHILDREN,
  },
  {
    tag: "schedule",
    kind: "object · REQUIRED",
    summary: "WHEN the job runs: which days (frequency/calendars) and at what time (runAt/window/cycle).",
    detail: "Final eligibility = frequency + monthsOfYear + shift + calendars (include/exclude). The Forecast panel and the Dry Run use EXACTLY the same decision as the daily (IsScheduledOn).",
    children: SCHEDULE_CHILDREN,
    example: `schedule:
  enabled: true
  frequency: businessday
  nthBusinessDays: [-1]
  runAt: "18:30"
  shift: next-businessday`,
  },
  {
    tag: "params",
    kind: "map · per jobType",
    summary: "jobType-specific parameters (in YAML it is `params:`; in the JSON API the same field is called `actionConfig`).",
    detail: "The fields and requirements of each type live inside `jobType` (expand it there). Extra: `_agentId` pins the job to a specific agent — with the pin, only that agent runs it (offline = WAIT AGENT). Param strings are interpolable with %%VARS/${var.} (see `variables`).",
    example: `params:
  command: "make-cadoc --date %%ODATE"
  _agentId: fin-agent-01`,
  },
  {
    tag: "upstream",
    kind: "list of {from, condition, dateRef} — LEGACY (reading sugar)",
    summary: "LEGACY dependency form: on load it becomes explicit CONDITIONS (single model) — A-TO-B in the parent's conditionsOutAdd and in the child's conditionsIn + conditionsOutRemove.",
    detail: "Since the unification (2026-07-17) every dependency is a CONDITION in a global pool (Conditions panel in Monitoring). `upstream:` is still accepted in YAML as a shortcut: the expansion is automatic and idempotent, and the field is never persisted back (it is only a topology view). Prefer declaring conditions directly. `dateRef` becomes the @prev/@stat suffix of the INBOUND condition, relative to this job's ODAT (origin).",
    forms: [
      { form: "condition omitted / \"\"", desc: "= on-success (safe default)" },
      { form: '"on-success"', desc: "runs if the parent ended OK" },
      { form: '"on-failure"', desc: "runs if the parent ended NOTOK (contingency jobs)" },
      { form: '"on-complete"', desc: "runs when the parent ENDS (OK or NOTOK)" },
      { form: '"always"', desc: "same as on-complete" },
      { form: "dateRef omitted / \"odat\"", desc: "parent from the SAME origin daily (default)" },
      { form: 'dateRef: "prev"', desc: "parent from the PREVIOUS daily (D-1 close)" },
      { form: 'dateRef: "stat"', desc: "static: any free completion, ignoring the date" },
    ],
    example: `upstream:
  - from: extract-fin
  - from: validate-fin
    condition: on-failure
  - from: close
    dateRef: prev`,
  },
  {
    tag: "retries",
    kind: "int",
    summary: "EXTRA attempts after a failure (0 = runs exactly once).",
    example: `retries: 2`,
  },
  {
    tag: "retryDelayMin",
    kind: "int (minutes)",
    summary: "Spacing between retry attempts.",
    detail: "0 = classic short backoff (seconds). >0 = the next attempt is SCHEDULED through scheduled_at — DURABLE: it survives a server restart and the daily rollover (a multi-day retry crosses dailies). Combine it with confirm/keepActive for human approvals + a retry days later.",
    example: `retries: 3
retryDelayMin: 60   # retry every 1h`,
  },
  {
    tag: "timeout",
    kind: "int (seconds)",
    summary: "Maximum run time; exceeded = NOTOK (and it ends FILE_WATCH/WAIT).",
    example: `timeout: 1800`,
  },
  {
    tag: "dryRun",
    kind: "bool",
    summary: "👻GHOST job: enters the daily and \"runs\" without executing anything (log only).",
    detail: "The badge in Monitoring comes FROZEN on the instance (order snapshot) — changing dryRun in Design only affects the NEXT order.",
  },
  {
    tag: "confirm",
    kind: "bool",
    summary: "Control-M \"Wait for confirmation\": the instance does NOT run until an operator confirms it.",
    detail: "WAIT_CONFIRM gate (purple ✋CONFIRM card). Not even Force Order bypasses it; a rerun requires confirming again. Confirmation: the button on the card/drawer or POST /instances/{id}/confirm.",
  },
  {
    tag: "selfService",
    kind: "bool",
    summary: "Exposes the job on the /portal page so business users can fire it without access to Design/Monitoring.",
  },
  {
    tag: "environment",
    kind: "string",
    summary: "Job environment/site — ROUTES the execution (ADV-2).",
    detail: "A job with an environment only dispatches to an agent in the SAME env (the agent's `-env` flag) or to an agent with NO label (a generalist). With no matching agent it sits in WAIT AGENT with the reason in Explain. Works cross-node (R5 presence). Case-insensitive.",
    forms: [{ form: '"dev" | "staging" | "prod" | "dc-sp"…', desc: "free text; case-insensitive" }],
    example: `environment: prod`,
  },
  {
    tag: "agentId",
    kind: "string",
    summary: "Agent pin on the MODEL (YAML field). The UI uses `params._agentId` for the same purpose — prefer the pin in params, which is what the WAIT AGENT gate reads.",
  },
  {
    tag: "calendars",
    kind: "list of {name, mode}",
    summary: "Binds named calendars (workspace calendars/*.yaml) to the job.",
    detail: "include = only runs on days the calendar marks eligible · exclude = does NOT run on the calendar's eligible days (negation). Several calendars compose. The `calendar` field (a plain string, no list) is LEGACY and counts as include.",
    forms: [
      { form: "mode: include", desc: "restricts to the calendar's days (holidays out, etc.)" },
      { form: "mode: exclude", desc: "blocks the calendar's days" },
    ],
    example: `calendars:
  - { name: business-days-br, mode: include }
  - { name: freeze-eoy, mode: exclude }`,
  },
  {
    tag: "resources",
    kind: "map name → quantity",
    summary: "Quantitative resources consumed (Control-M resources): with no free units, the job waits.",
    detail: "WAIT_RESOURCE gate with a QUEUE; a multi-resource request is ALL-OR-NOTHING (no partial reservation). Each resource's capacity is managed in the UI (an unknown resource is born with capacity 1 = exclusive lock).",
    example: `resources:
  db-fin: 1
  etl-slots: 2`,
  },
  {
    tag: "conditionsIn",
    kind: "list of string",
    summary: "GLOBAL conditions required: the job is only ready once ALL of them exist in the resolved scope (default = the job's ORIGIN daily, the ODAT).",
    detail: "RUNTIME gate (WAIT_CONDITION) — the daily still materializes the instance. Who sets them: another job's `conditionsOutAdd`, an On/Do set-condition action, POST /events/ingest (external event) or the UI. The DATE goes in the name suffix: no suffix/`@odat` = origin daily; `@prev` = previous daily; `@stat` = static (only the permanent one satisfies it).",
    example: `conditionsIn: [FILE-ARRIVED, CLOSE@prev, ENVIRONMENT-OK@stat]`,
  },
  {
    tag: "conditionsOutAdd",
    kind: "list of string",
    summary: "Conditions CREATED when this job ends OK (unblocks whoever requires them through conditionsIn).",
    detail: "Created in the scope of the producer's ORIGIN daily (ODAT) — a job carried over by the rollover creates the condition for ITS day, not for the current one. Suffixes: `@stat` creates a permanent one; `@prev` creates it in the previous daily.",
    example: `conditionsOutAdd: [EXTRACT-DONE, ENVIRONMENT-OK@stat]`,
  },
  {
    tag: "conditionsOutRemove",
    kind: "list of string",
    summary: "Conditions DELETED when this job ends OK (negates/clears the day's condition). Same date suffixes as conditionsOutAdd.",
  },
  {
    tag: "variables",
    kind: "map NAME → value",
    summary: "Job variables (definition scope), interpolable in params through %%NAME or ${var.NAME}.",
    detail:
      "Interpolation precedence: Runtime > Local (from the instance) > Definition (these) > Global. " +
      "NATIVE runtime TOKENS (always available, UPPERCASE): %%ODATE (order YYYYMMDD — correct on rerun/carry-over), %%ORDERDATE, %%RUNDATE, %%TIME, %%JOBNAME, %%JOBLABEL, %%FOLDER, %%INSTANCEID. " +
      "DATE TOKENS aware of the job's calendar: %%EOM/%%BOM (last/first day of the month), %%EOY/%%BOY (of the year), %%NEXTBD/%%PREVBD (next/previous business day), %%FIRSTBD/%%LASTBD (1st/last business day of the month). " +
      "OFFSETS compose with any token: ±N = calendar days, ±NB = BUSINESS days (e.g. %%ODATE+2B, %%EOM-1, %%LASTBD-1B). " +
      "AT RUNTIME the job can write variables by printing to its output: `%%SET NAME=VALUE` (GLOBAL, terminal-only) and `%%SETLOCAL NAME=VALUE` (LOCAL to the instance, applied at the end of every attempt BEFORE the retry — carries state between attempts; never leaks to another job). A %%var name is a letter followed by letters/digits/underscore; names with a dot only work through ${var.}.",
    example: `variables:
  REGION: southeast
params:
  command: "process --dt %%ODATE-1B --reg %%REGION"`,
  },
  {
    tag: "sla",
    kind: "object",
    summary: "Job SLA: expected duration and/or deadline — a breach raises an alert (and the SLA tab in the drawer).",
    forms: [
      { form: "expectedDurationMin: N", desc: "alerts if it runs longer than N minutes" },
      { form: 'deadlineHM: "HH:MM"', desc: "alerts if it has not ended OK by that time" },
      { form: 'severity: "warning" | "critical"', desc: "breach severity" },
      { form: "webhookUrl: https://…", desc: "extra channel for the SLA alert" },
    ],
    example: `sla:
  expectedDurationMin: 30
  deadlineHM: "07:00"
  severity: critical`,
  },
  {
    tag: "subWorkflow",
    kind: "object {folder, variables}",
    summary: "This job waits for ANOTHER whole folder to end OK (sub-workflow as a dependency).",
    example: `subWorkflow:
  folder: pre-processing
  variables: { MODE: full }`,
  },
  {
    tag: "actions",
    kind: "list of On/Do rules",
    summary: "Control-M \"On/Do\" automation: On <trigger> Do <action>. Expand for every trigger and action.",
    detail: "FLAT structure per rule: trigger fields (on + qualifier) and action fields (do + parameters) live side by side; only the relevant ones are read. Each rule fires at most once per instance.",
    children: ACTIONS_CHILDREN,
    example: `actions:
  - on: result
    status: NOTOK
    do: notify
    severity: critical
    channels: [slack, pagerduty]
    message: "%%JOBNAME failed on %%ODATE"
  - on: result
    status: OK
    do: set-condition
    condition: FIN-DONE
  - on: exit
    exitCodes: "1,2,3"
    do: set-ok
  - on: attempt
    attempt: 2
    do: run-job
    targetJob: cleanup-fin
  - on: runtime
    afterMin: 45
    do: notify
    severity: warning`,
  },
];
