/**
 * code-schema.ts — GUIA COMPLETA do dialeto YAML do workspace (modo código).
 *
 * Fonte da verdade espelhada daqui: `server/internal/domain/model.go`
 * (JobDefinition/Schedule/ActionRule/SLASpec/SubWorkflowRef/CalendarRef),
 * `domain/validate.go` (obrigatórios por jobType), `scheduler/conditions.go`,
 * `scheduler/calendars.go` (advancedRule) e os executores do agente.
 * Se um campo novo entrar no modelo Go, ELE ENTRA AQUI — a guia do editor
 * promete cobrir TODAS as tags, sem exceção.
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
    summary: "Comando shell no agente (Windows executa via powershell).",
    detail: "params: `command` (o comando; essencial) · `cwd` (diretório de trabalho). O agente precisa anunciar a capability COMMAND.",
    example: `jobType: COMMAND
params:
  command: "echo ODATE=%%ODATE"
  cwd: /tmp`,
  },
  {
    tag: "SCRIPT",
    kind: "jobType",
    summary: "Script .sh/.bat/.ps1 no host do agente.",
    detail: "params: `scriptPath` (caminho do script) · `args` (argumentos) · `cwd`.",
    example: `jobType: SCRIPT
params:
  scriptPath: /opt/jobs/backup.sh
  args: "--full"`,
  },
  {
    tag: "SSH",
    kind: "jobType",
    summary: "Comando remoto via SSH — AGENTLESS (não exige agente com a capability).",
    detail: "params: `host` · `user` · `port` · `command` · `keyPath` (chave privada).",
    example: `jobType: SSH
params:
  host: 10.0.0.7
  user: batch
  command: "systemctl restart etl"`,
  },
  {
    tag: "HTTP",
    kind: "jobType · alias REST",
    summary: "Chamada REST.",
    detail: "params: `url` (OBRIGATÓRIO) · `method` GET|POST|PUT|PATCH|DELETE · `headers` (mapa) · `body` · `expectStatus` — aceita `200`, `[200, 204]` ou `\"200,204\"`; status fora deles = NOTOK.",
    example: `jobType: HTTP
params:
  method: POST
  url: https://api.interna/reconcile
  headers: { Authorization: "Bearer \${var.TOKEN}" }
  expectStatus: 200`,
  },
  {
    tag: "DATABASE",
    kind: "jobType · alias DB",
    summary: "SQL em Postgres/MySQL/SQLite pelo agente (drivers pure-Go).",
    detail: "params OBRIGATÓRIOS: `driver` (postgres|mysql|sqlite — aceita aliases pg/pgx/postgresql/mariadb/sqlite3) · `dsn` · `sql`. Opcional: `maxRows` (linhas renderizadas de um SELECT). SELECT mostra linhas; DML mostra rows-affected.",
    example: `jobType: DATABASE
params:
  driver: postgres
  dsn: "postgres://user:pass@db:5432/fin"
  sql: "DELETE FROM staging WHERE dt < '%%ODATE'"`,
  },
  {
    tag: "FILE_WATCH",
    kind: "jobType · alias FILEWATCH",
    summary: "Espera um arquivo chegar no host do agente.",
    detail: "params: `path` (OBRIGATÓRIO — caminho no host do agente) · `intervalSec` (poll, default 5) · `stableSec` (tamanho estável por N s antes de OK). O `timeout` do job encerra a espera com NOTOK.",
    example: `jobType: FILE_WATCH
timeout: 3600
params:
  path: /data/in/cadoc_%%ODATE.txt
  stableSec: 10`,
  },
  {
    tag: "LAMBDA",
    kind: "jobType · alias AWS_LAMBDA",
    summary: "Invoca uma função AWS Lambda (SigV4 pelo agente, sem SDK).",
    detail: "params: `function` (OBRIGATÓRIO — `functionName` aceito como alias) · `region` (default env AWS_REGION do agente) · `payload` (JSON string ou objeto) · `accessKeyId`/`secretAccessKey`/`sessionToken` (default envs AWS_*) · `endpoint` (testes) · `invocationType` (reservado; a invocação atual é síncrona).",
    example: `jobType: LAMBDA
params:
  function: fin-reconcile
  region: sa-east-1`,
  },
  {
    tag: "BATCH",
    kind: "jobType",
    summary: "Job em container/lote (AWS Batch).",
    detail: "params OBRIGATÓRIOS: `jobQueue` · `jobDefinition`. Opcionais: `command` · `env` (mapa).",
    example: `jobType: BATCH
params:
  jobQueue: fin-fargate
  jobDefinition: validador-bacen`,
  },
  {
    tag: "GLUE",
    kind: "jobType",
    summary: "Job de ETL (AWS Glue).",
    detail: "params: `jobName` (OBRIGATÓRIO) · `arguments` (mapa) · `workerType` · `numberOfWorkers`.",
    example: `jobType: GLUE
params:
  jobName: cadoc-3040
  arguments: { "--DATA_REF": "%%ODATE" }`,
  },
  {
    tag: "STEP_FUNCTION",
    kind: "jobType",
    summary: "Dispara uma state machine (AWS Step Functions).",
    detail: "params: `stateMachineArn` (OBRIGATÓRIO) · `input` (JSON).",
    example: `jobType: STEP_FUNCTION
params:
  stateMachineArn: arn:aws:states:sa-east-1:123:stateMachine/etl`,
  },
  {
    tag: "WAIT",
    kind: "jobType",
    summary: "Delay/timer.",
    detail: "params: `seconds` OU `until` (\"HH:MM\"). Sem params = no-op imediato.",
    example: `jobType: WAIT
params:
  seconds: 300`,
  },
  {
    tag: "CHOICE",
    kind: "jobType",
    summary: "Desvio condicional.",
    detail: "params: `expression` (OBRIGATÓRIA) · `branches` (array).",
    example: `jobType: CHOICE
params:
  expression: "output.total > 0"`,
  },
  {
    tag: "PARALLEL",
    kind: "jobType",
    summary: "Execução concorrente de branches.",
    detail: "params: `branches` (array NÃO-vazio; OBRIGATÓRIO) · `maxConcurrency`.",
    example: `jobType: PARALLEL
params:
  branches: ["ramo-a", "ramo-b"]
  maxConcurrency: 2`,
  },
  {
    tag: "WASM",
    kind: "jobType",
    summary: "Módulo WebAssembly WASI no agente (wazero — sandbox por construção).",
    detail: "params: `wasmPath` OU `wasmUrl` (um dos dois é OBRIGATÓRIO) · `args` (string) · `stdin` (string). O módulo deve ser um command WASI (exporta _start).",
    example: `jobType: WASM
params:
  wasmUrl: https://repo.interna/mod.wasm
  args: "--full"`,
  },
  {
    tag: "K8S",
    kind: "jobType · alias K8S_JOB",
    summary: "Job Kubernetes pelo agente (API server direto, sem kubectl).",
    detail: "params: `image` (OBRIGATÓRIO) · `command` · `namespace` (default default) · `name` · `apiServer`/`token` (vazios = in-cluster) · `insecureTLS` (bool).",
    example: `jobType: K8S
params:
  image: alpine:3
  command: "echo oi"`,
  },
  {
    tag: "GCP_RUN",
    kind: "jobType · alias CLOUD_RUN_JOB",
    summary: "Dispara um Cloud Run Job (Run Admin API v2) pelo agente.",
    detail: "params OBRIGATÓRIOS: `project` · `region` · `job`. Opcionais: `token` (default env GOOGLE_OAUTH_TOKEN ou metadata server) · `endpoint` (testes).",
    example: `jobType: GCP_RUN
params:
  project: fin-prod
  region: us-central1
  job: reconcile`,
  },
  {
    tag: "— schema por tipo (ADV-1)",
    kind: "validação",
    summary: "Cada jobType conhecido tem schema DEDICADO no server (GET /api/jobtypes).",
    detail: "O lint/validação checa os params CONTRA o schema do tipo: campo desconhecido (typo tipo `comand:`) e tipo de valor errado = erro NA HORA; params OBRIGATÓRIOS (command/url/…) são cobrados no PUBLISH (rascunho incompleto pode ser salvo). Um jobType DESCONHECIDO pelo server é aceito com params livres — mas só roda se algum agente anunciar a capability correspondente; senão fica em WAIT AGENT.",
  },
];

/* ── schedule.* ── */

const SCHEDULE_CHILDREN: GuideEntry[] = [
  {
    tag: "enabled",
    kind: "bool · obrigatório",
    summary: "false = o job NÃO entra na daily (aparece INACTIVE no Design).",
    forms: [
      { form: "true", desc: "agendado — a daily materializa a instance nos dias elegíveis" },
      { form: "false", desc: "desligado — só roda por Force Order" },
    ],
  },
  {
    tag: "description",
    kind: "string",
    summary: "Texto livre exibido na UI (aba General do drawer).",
  },
  {
    tag: "frequency",
    kind: "string",
    summary: "EM QUAIS DIAS o job roda (avaliado pela daily).",
    forms: [
      { form: '"" | "daily"', desc: "todo dia (default)" },
      { form: '"weekly"', desc: "nos dias de `daysOfWeek`" },
      { form: '"monthly"', desc: "nos dias de `daysOfMonth`" },
      { form: '"businessday"', desc: "no N-ésimo dia ÚTIL do mês (`nthBusinessDays`)" },
      { form: '"advanced"', desc: "regra nomeada em `advancedRule`" },
    ],
  },
  {
    tag: "daysOfWeek",
    kind: "lista de string · frequency weekly",
    summary: "Dias da semana em que roda.",
    forms: [{ form: '"mon" "tue" "wed" "thu" "fri" "sat" "sun"', desc: "qualquer combinação" }],
    example: `schedule:
  enabled: true
  frequency: weekly
  daysOfWeek: [mon, wed, fri]`,
  },
  {
    tag: "daysOfMonth",
    kind: "lista de int · frequency monthly",
    summary: "Dias do mês em que roda.",
    forms: [
      { form: "1..31", desc: "dia fixo do mês" },
      { form: "-1", desc: "ÚLTIMO dia do mês" },
    ],
    example: `schedule:
  enabled: true
  frequency: monthly
  daysOfMonth: [1, 15, -1]`,
  },
  {
    tag: "nthBusinessDays",
    kind: "lista de int · frequency businessday",
    summary: "N-ésimo dia ÚTIL do mês (feriados via calendar do job).",
    forms: [
      { form: "5", desc: "5º dia útil do mês" },
      { form: "-1", desc: "ÚLTIMO dia útil do mês" },
    ],
    example: `schedule:
  enabled: true
  frequency: businessday
  nthBusinessDays: [1, -1]`,
  },
  {
    tag: "monthsOfYear",
    kind: "lista de int (1..12)",
    summary: "Filtra MESES em qualquer frequency (vazio = todos).",
    example: `schedule:
  enabled: true
  frequency: monthly
  daysOfMonth: [-1]
  monthsOfYear: [3, 6, 9, 12]   # só fim de trimestre`,
  },
  {
    tag: "advancedRule",
    kind: "string · frequency advanced",
    summary: "Regras nomeadas de dia útil (cientes do calendar).",
    forms: [
      { form: '"first-businessday"', desc: "1º dia útil do mês" },
      { form: '"last-businessday"', desc: "último dia útil do mês" },
      { form: '"first-businessday-not-monday"', desc: "1º dia útil que NÃO é segunda" },
      { form: '"penultimate-businessday"', desc: "penúltimo dia útil do mês" },
    ],
  },
  {
    tag: "shift",
    kind: "string",
    summary: "O que fazer quando o dia NOMINAL cai em dia não-elegível (feriado/exclude; sem calendar, fim de semana). Control-M \"roll\".",
    forms: [
      { form: '"" | "none"', desc: "não roda naquele ciclo (clássico)" },
      { form: '"next-businessday"', desc: "rola pro PRÓXIMO dia elegível" },
      { form: '"prev-businessday"', desc: "ANTECIPA pro dia elegível anterior" },
    ],
  },
  {
    tag: "runAt",
    kind: 'string "HH:MM"',
    summary: "Hora em que a instance fica ELEGÍVEL depois da daily (antes disso: WAIT).",
  },
  {
    tag: "windowFrom / windowTo",
    kind: 'string "HH:MM"',
    summary: "Janela de execução permitida. Depois de windowTo, WAITING vira gate WINDOW_CLOSED (não submete mais hoje).",
    example: `schedule:
  enabled: true
  windowFrom: "22:00"
  windowTo: "05:00"`,
  },
  {
    tag: "cyclic + intervalMin",
    kind: "bool + int (minutos)",
    summary: "Job repetitivo: ao terminar OK, a MESMA instance re-arma para +intervalMin.",
    detail: "NOTOK não cicla (espera operador). O ciclo respeita windowTo e morre na virada da daily. `cycleRuns` conta as voltas na instance.",
    example: `schedule:
  enabled: true
  cyclic: true
  intervalMin: 15
  windowTo: "18:00"`,
  },
  {
    tag: "cyclicMaxRuns",
    kind: "int",
    summary: "Teto de voltas do ciclo (0 = sem teto — vale a janela/virada).",
  },
  {
    tag: "keepActive",
    kind: "int (diárias)",
    summary: "Carry-over: quantas diárias EXTRA o job sobrevive sem terminar OK.",
    detail: "0 = default (NOTOK não-tratado persiste +1 diária; WAITING que nunca rodou NÃO persiste). >0 = sobrevive N diárias. RUNNING e HELD persistem sempre. Combina com `confirm`/`retryDelayMin` para workflows human-in-the-loop de dias.",
  },
  {
    tag: "cronExpression",
    kind: "string · LEGADO",
    summary: "Cron de 5 campos. Só é a fonte quando `frequency` está vazio. Prefira frequency estruturada.",
  },
];

/* ── actions.* (On/Do) ── */

const ACTIONS_CHILDREN: GuideEntry[] = [
  {
    tag: "on",
    kind: "string · obrigatório na regra",
    summary: "O GATILHO da regra.",
    forms: [
      { form: '"result"', desc: "status TERMINAL do job (após esgotar retries) — qualifica com `status`" },
      { form: '"attempt"', desc: "a N-ésima tentativa FALHOU (escada de rerun) — qualifica com `attempt`" },
      { form: '"runtime"', desc: "job RUNNING há mais de N minutos (shout) — qualifica com `afterMin`" },
    ],
  },
  { tag: "status", kind: 'string · on: "result"', summary: 'Qual resultado dispara: "OK" ou "NOTOK".' },
  { tag: "attempt", kind: 'int · on: "attempt"', summary: "Número da tentativa (1-based) que, ao falhar, dispara." },
  { tag: "afterMin", kind: 'int · on: "runtime"', summary: "Minutos de RUNNING que disparam a regra." },
  {
    tag: "do",
    kind: "string · obrigatório na regra",
    summary: "A AÇÃO disparada (cada regra dispara NO MÁXIMO 1× por instance).",
    forms: [
      { form: '"notify"', desc: "alerta nos canais (usa message/severity/channels)" },
      { form: '"set-condition"', desc: "seta a condition global do order_date (destrava sucessores) — usa `condition`" },
      { form: '"run-job"', desc: "Force Order de `targetJob` (roda outro job, ignorando deps)" },
      { form: '"set-ok"', desc: "flipa o PRÓPRIO job NOTOK→OK (só faz sentido com on result NOTOK)" },
    ],
  },
  { tag: "message", kind: "string · do notify", summary: "Texto do alerta (interpola %%VARS)." },
  {
    tag: "severity",
    kind: "string · do notify",
    summary: "Severidade do alerta.",
    forms: [{ form: '"info" | "warning" | "critical"', desc: "critical/warning viram toast de erro na UI" }],
  },
  {
    tag: "channels",
    kind: "lista de string · do notify",
    summary: "Canais de saída; VAZIO = todos os configurados em Settings.",
    forms: [{ form: '"slack" "webhook" "email" "pagerduty"', desc: "qualquer combinação" }],
  },
  { tag: "condition", kind: "string · do set-condition", summary: "Nome da condition global a setar." },
  { tag: "targetJob", kind: "string · do run-job", summary: "id da definition a forçar." },
];

/* ── A GUIA (toda tag top-level do YAML) ── */

export const YAML_GUIDE: GuideEntry[] = [
  {
    tag: "— o documento",
    kind: "regras gerais",
    summary: "Como o editor entende o YAML (dialeto EXATO dos arquivos do workspace Git).",
    detail:
      "Vários jobs num só texto: separe com `---` (multi-doc), UM job por documento. O comentário `# definitions/<folder>/<id>.yaml` que o servidor gera é informativo. O parse é ESTRITO: campo desconhecido/typo (ex.: `retires:`) = erro na validação, nada é ignorado em silêncio. Job presente no working set e AUSENTE do texto = marcado para DELETE (o Aplicar pede confirmação listando os ids). `team` pode ser omitido quando há UMA folder aberta (vira o default do escopo).",
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
    kind: "string · OBRIGATÓRIO",
    summary: "Identificador ÚNICO do job no workspace — vira o arquivo definitions/<team>/<id>.yaml.",
    detail:
      "Formas aceitáveis: slug estável, minúsculo, sem espaço (letras/números/traço — ex.: `extract-fin`, `cadoc-3040`). É a referência usada em `upstream.from`, `actions.targetJob` e na API. Renomear id = o plano vira CRIAR o novo + DELETAR o antigo (o histórico da instance não migra).",
    example: `id: extract-fin`,
  },
  {
    tag: "label",
    kind: "string · OBRIGATÓRIO",
    summary: "Nome de exibição nos cards, listas e drawers.",
    example: `label: Extract FIN (diário)`,
  },
  {
    tag: "team",
    kind: "string · OBRIGATÓRIO (inferível)",
    summary: "A folder do job (o subdiretório de definitions/).",
    detail: "No modo código com UMA folder aberta pode ser omitido (assume a folder do escopo). Com várias folders abertas, é obrigatório em cada doc. Mover de folder = Save na nova + Delete na antiga (o plano mostra).",
    example: `team: fin`,
  },
  {
    tag: "jobType",
    kind: "string · OBRIGATÓRIO",
    summary: "O QUE o job executa. Expanda para ver todos os tipos JÁ IMPLEMENTADOS e os params de cada um.",
    detail: "O agente que vai executar precisa anunciar a capability do tipo (exceto SSH, que é agentless). Sem agente capaz online, a instance fica no gate WAIT AGENT (card azul claro) — nem é reivindicada.",
    children: JOB_TYPE_CHILDREN,
  },
  {
    tag: "schedule",
    kind: "objeto · OBRIGATÓRIO",
    summary: "QUANDO o job roda: em quais dias (frequency/calendars) e a que horas (runAt/janela/ciclo).",
    detail: "A elegibilidade final = frequency + monthsOfYear + shift + calendars (include/exclude). O painel Forecast e o Dry Run usam EXATAMENTE a mesma decisão da daily (IsScheduledOn).",
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
    kind: "mapa · por jobType",
    summary: "Parâmetros específicos do jobType (no YAML é `params:`; na API JSON o mesmo campo chama `actionConfig`).",
    detail: "Os campos e obrigatórios de cada tipo estão dentro de `jobType` (expanda lá). Extra: `_agentId` pina o job num agente específico — com o pin, só aquele agente executa (offline = WAIT AGENT). Strings de params são interpoláveis com %%VARS/${var.} (ver `variables`).",
    example: `params:
  command: "gera-cadoc --data %%ODATE"
  _agentId: agente-fin-01`,
  },
  {
    tag: "upstream",
    kind: "lista de {from, condition}",
    summary: "Dependências: este job só roda depois dos pais, conforme a condition.",
    detail: "Pai NOTOK/CANCELLED bloqueia o filho (aresta vermelha, gate BLOCKED_DEP) — \"Set OK\" no pai libera. Force Order ignora deps de propósito.",
    forms: [
      { form: "condition omitida / \"\"", desc: "= on-success (default seguro)" },
      { form: '"on-success"', desc: "roda se o pai terminou OK" },
      { form: '"on-failure"', desc: "roda se o pai terminou NOTOK (jobs de contingência)" },
      { form: '"on-complete"', desc: "roda quando o pai TERMINA (OK ou NOTOK)" },
      { form: '"always"', desc: "como on-complete" },
    ],
    example: `upstream:
  - from: extract-fin
  - from: valida-fin
    condition: on-failure`,
  },
  {
    tag: "retries",
    kind: "int",
    summary: "Tentativas EXTRAS após uma falha (0 = roda 1 vez só).",
    example: `retries: 2`,
  },
  {
    tag: "retryDelayMin",
    kind: "int (minutos)",
    summary: "Espaçamento entre tentativas de retry.",
    detail: "0 = backoff curto clássico (segundos). >0 = a próxima tentativa é AGENDADA via scheduled_at — DURÁVEL: sobrevive a restart do server e à virada da daily (um retry de dias atravessa diárias). Combine com confirm/keepActive para aprovações humanas + retry após dias.",
    example: `retries: 3
retryDelayMin: 60   # re-tenta a cada 1h`,
  },
  {
    tag: "timeout",
    kind: "int (segundos)",
    summary: "Tempo máximo de execução; estourou = NOTOK (e encerra FILE_WATCH/WAIT).",
    example: `timeout: 1800`,
  },
  {
    tag: "dryRun",
    kind: "bool",
    summary: "Job 👻GHOST: entra na daily e \"roda\" sem executar nada (log only).",
    detail: "O selo no Monitoring vem CONGELADO na instance (snapshot da ordem) — mudar dryRun no Design só afeta a PRÓXIMA ordem.",
  },
  {
    tag: "confirm",
    kind: "bool",
    summary: "Control-M \"Wait for confirmation\": a instance NÃO roda até um operador confirmar.",
    detail: "Gate WAIT_CONFIRM (card violeta ✋CONFIRM). Nem Force Order bypassa; rerun exige confirmar de novo. Confirmação: botão no card/drawer ou POST /instances/{id}/confirm.",
  },
  {
    tag: "selfService",
    kind: "bool",
    summary: "Expõe o job no portal /portal para usuários de negócio dispararem sem acesso ao Design/Monitoring.",
  },
  {
    tag: "environment",
    kind: "string",
    summary: "Ambiente/site do job — ROTEIA a execução (ADV-2).",
    detail: "Job com environment só despacha pra agente do MESMO env (flag `-env` do agente) ou pra agente SEM label (generalista). Sem agente casando, fica em WAIT AGENT com o motivo no Explain. Vale cross-nó (presença R5). Case-insensitive.",
    forms: [{ form: '"dev" | "staging" | "prod" | "dc-sp"…', desc: "texto livre; case-insensitive" }],
    example: `environment: prod`,
  },
  {
    tag: "agentId",
    kind: "string",
    summary: "Pin de agente no MODELO (campo do YAML). A UI usa `params._agentId` para o mesmo fim — prefira o pin em params, que é o que o gate WAIT AGENT lê.",
  },
  {
    tag: "calendars",
    kind: "lista de {name, mode}",
    summary: "Vincula calendars nomeados (workspace calendars/*.yaml) ao job.",
    detail: "include = só roda em dias elegíveis do calendar · exclude = NÃO roda nos dias elegíveis (negação). Vários calendars compõem. O campo `calendar` (string, sem lista) é o LEGADO e vale como include.",
    forms: [
      { form: "mode: include", desc: "restringe aos dias do calendar (feriados fora, etc.)" },
      { form: "mode: exclude", desc: "bloqueia os dias do calendar" },
    ],
    example: `calendars:
  - { name: uteis-br, mode: include }
  - { name: freeze-eoy, mode: exclude }`,
  },
  {
    tag: "resources",
    kind: "mapa nome → quantidade",
    summary: "Recursos quantitativos consumidos (Control-M resources): sem unidades livres, o job espera.",
    detail: "Gate WAIT_RESOURCE com FILA; pedido multi-recurso é ALL-OR-NOTHING (não reserva parcial). Capacidade de cada recurso é gerida na UI (recurso desconhecido nasce com capacidade 1 = lock exclusivo).",
    example: `resources:
  db-fin: 1
  slots-etl: 2`,
  },
  {
    tag: "conditionsIn",
    kind: "lista de string",
    summary: "Conditions GLOBAIS exigidas: o job só fica pronto quando TODAS existem no escopo do seu order_date (ou permanentes).",
    detail: "Gate de RUNTIME (WAIT_CONDITION) — a daily ainda materializa a instance. Quem seta: `conditionsOutAdd` de outro job, ação On/Do set-condition, POST /events/ingest (evento externo) ou a UI.",
    example: `conditionsIn: [ARQUIVO-CHEGOU, FECHAMENTO-LIBERADO]`,
  },
  {
    tag: "conditionsOutAdd",
    kind: "lista de string",
    summary: "Conditions CRIADAS quando este job termina OK (destrava quem as exige via conditionsIn).",
    example: `conditionsOutAdd: [EXTRACT-DONE]`,
  },
  {
    tag: "conditionsOutRemove",
    kind: "lista de string",
    summary: "Conditions APAGADAS quando este job termina OK (nega/limpa condição do dia).",
  },
  {
    tag: "variables",
    kind: "mapa NOME → valor",
    summary: "Variáveis do job (escopo definition), interpoláveis nos params via %%NOME ou ${var.NOME}.",
    detail:
      "Precedência na interpolação: Runtime > Local (da instance) > Definition (estas) > Global. " +
      "TOKENS NATIVOS de runtime (sempre disponíveis, MAIÚSCULOS): %%ODATE (YYYYMMDD da ordem — correto em rerun/carry), %%ORDERDATE, %%RUNDATE, %%TIME, %%JOBNAME, %%JOBLABEL, %%FOLDER, %%INSTANCEID. " +
      "TOKENS DE DATA cientes do calendar do job: %%EOM/%%BOM (último/primeiro dia do mês), %%EOY/%%BOY (do ano), %%NEXTBD/%%PREVBD (próximo/anterior dia útil), %%FIRSTBD/%%LASTBD (1º/último dia útil do mês). " +
      "OFFSETS compõem com qualquer token: ±N = dias corridos, ±NB = dias ÚTEIS (ex.: %%ODATE+2B, %%EOM-1, %%LASTBD-1B). " +
      "EM RUNTIME o job pode gravar variáveis imprimindo no output: `%%SET NOME=VALOR` (GLOBAL, terminal-only) e `%%SETLOCAL NOME=VALOR` (LOCAL da instance, aplicado a cada término de tentativa ANTES do retry — passa estado entre tentativas; nunca vaza pra outro job). Nome de %%var = letra + letras/números/underscore; nomes com ponto só via ${var.}.",
    example: `variables:
  REGIAO: sudeste
params:
  command: "processa --dt %%ODATE-1B --reg %%REGIAO"`,
  },
  {
    tag: "sla",
    kind: "objeto",
    summary: "SLA do job: duração esperada e/ou deadline — breach gera alerta (e aba SLA no drawer).",
    forms: [
      { form: "expectedDurationMin: N", desc: "alerta se rodar além de N minutos" },
      { form: 'deadlineHM: "HH:MM"', desc: "alerta se não terminar OK até a hora" },
      { form: 'severity: "warning" | "critical"', desc: "severidade do breach" },
      { form: "webhookUrl: https://…", desc: "canal extra do alerta de SLA" },
    ],
    example: `sla:
  expectedDurationMin: 30
  deadlineHM: "07:00"
  severity: critical`,
  },
  {
    tag: "subWorkflow",
    kind: "objeto {folder, variables}",
    summary: "Este job espera OUTRA folder inteira terminar OK (sub-workflow como dependência).",
    example: `subWorkflow:
  folder: pre-processamento
  variables: { MODO: full }`,
  },
  {
    tag: "actions",
    kind: "lista de regras On/Do",
    summary: "Automação Control-M \"On/Do\": On <gatilho> Do <ação>. Expanda para todos os gatilhos e ações.",
    detail: "Estrutura PLANA por regra: os campos de gatilho (on + qualificador) e de ação (do + parâmetros) convivem; só os relevantes são lidos. Cada regra dispara no máximo 1× por instance.",
    children: ACTIONS_CHILDREN,
    example: `actions:
  - on: result
    status: NOTOK
    do: notify
    severity: critical
    channels: [slack, pagerduty]
    message: "%%JOBNAME falhou no %%ODATE"
  - on: result
    status: OK
    do: set-condition
    condition: FIN-DONE
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
