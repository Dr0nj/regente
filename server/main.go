// regente-server — orquestrador daemon.
//
// Arquitetura (F10):
//   - HTTP/REST API (chi) para web e operações administrativas
//   - WebSocket hub (gorilla) para web (eventos live) e agents (dispatch)
//   - Scheduler core (goroutine) que roda daily, avalia deps e promove instances
//   - Storage de definitions em disco (YAML em ./workspace/definitions/<folder>/<id>.yaml)
//     com commit opcional via git
//   - SQLite como state store de instances/agents/runs (pure-Go, sem CGO)
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	// E1 — base IANA de timezones EMBUTIDA no binário: settings.daily_timezone
	// usa time.LoadLocation, e Windows/containers scratch não têm zoneinfo do
	// sistema. Custa ~450KB e torna o binário auto-suficiente.
	_ "time/tzdata"

	"github.com/Dr0nj/regente-server/internal/api"
	"github.com/Dr0nj/regente-server/internal/audit"
	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/bus"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/leader"
	"github.com/Dr0nj/regente-server/internal/oidc"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/secrets"
	"github.com/Dr0nj/regente-server/internal/sectls"
	"github.com/Dr0nj/regente-server/internal/serveragent"
	"github.com/Dr0nj/regente-server/internal/storage"
	"github.com/Dr0nj/regente-server/internal/telemetry"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// version — versão do build, injetada pela release:
//
//	go build -ldflags "-X main.version=v1.2.3"
//
// "dev" é o valor de quem builda do código-fonte, e é honesto: não existe
// release correspondente. Exposta no boot, em `regente-server -version` e em
// GET /api/version (que é o que o rodapé da UI mostra) — a pergunta "atualizei
// mesmo?" tem de ser respondível SEM olhar o disco.
var version = "dev"

func main() {
	var (
		addr      = flag.String("addr", envOr("REGENTE_ADDR", ":8080"), "HTTP listen address")
		spaDir    = flag.String("spa-dir", envOr("REGENTE_SPA_DIR", ""), "Single-origin hosting: serve the built SPA from this directory (UI+API+WS on the same port). Empty = API only")
		docsDir   = flag.String("docs-dir", envOr("REGENTE_DOCS_DIR", ""), "ADV-7: serve the docs site (cmd/docsite) at /docs on the same port. Empty = no docs")
		workspace = flag.String("workspace", envOr("REGENTE_WORKSPACE", "./workspace"), "Path to regente workspace (contains definitions/)")
		dbPath    = flag.String("db", envOr("REGENTE_DB", "./regente.db"), "DB DSN: path to the SQLite file, or a Postgres connection string when -db-driver=postgres")
		dbDriver  = flag.String("db-driver", envOr("REGENTE_DB_DRIVER", "sqlite"), "State store backend: sqlite | postgres")
		backupTo  = flag.String("backup", "", "R6/DR: writes an online backup of the state store to this path and exits (SQLite: VACUUM INTO; Postgres: use pg_dump — see docs/dr-backup.md)")
		// Segurança — TLS/mTLS opcional (vazio = HTTP plano, comportamento atual).
		tlsCert      = flag.String("tls-cert", envOr("REGENTE_TLS_CERT", ""), "Security: server TLS cert (empty = plain HTTP)")
		tlsKey       = flag.String("tls-key", envOr("REGENTE_TLS_KEY", ""), "Security: server TLS key")
		tlsClientCA  = flag.String("tls-client-ca", envOr("REGENTE_TLS_CLIENT_CA", ""), "Security: CA to require+verify a client cert (enables mTLS)")
		auditSIEMURL = flag.String("audit-siem-url", envOr("REGENTE_AUDIT_SIEM_URL", ""), "Security: SIEM HTTP endpoint to POST audit events to (empty = JSON to stderr only)")
		secretsFile  = flag.String("secrets-file", envOr("REGENTE_SECRETS_FILE", ""), "H3: JSON secrets file {\"github_token\":...}; the REGENTE_SECRET_<KEY> env takes priority")
		tickMs       = flag.Int("tick-ms", 2000, "Scheduler tick interval (ms) — used in internal mode")
		// Fase 1/2 (serverless) — papel do processo + origem do tick. Ver docs/architecture-future.md.
		role          = flag.String("role", envOr("REGENTE_ROLE", "all"), "Process role: all | api | scheduler")
		schedulerMode = flag.String("scheduler", envOr("REGENTE_SCHEDULER", "internal"), "Scheduler trigger: internal (goroutine ticker) | external (cron via POST /api/scheduler/tick)")
		// R5 — bus distribuído (opt-in). hub (default) = comportamento local de sempre.
		busMode = flag.String("bus", envOr("REGENTE_BUS", "hub"), "Hub transport: hub (local) | nats (distributed)")
		natsURL = flag.String("nats-url", envOr("REGENTE_NATS_URL", "nats://localhost:4222"), "NATS URL (when -bus=nats)")
		nodeID  = flag.String("node-id", envOr("REGENTE_NODE_ID", defaultNodeID()), "Unique id of this node in the cluster (presence/routing R5)")
		// I1 — observabilidade: tracing OTLP opt-in (vazio = off, no-op).
		// R7 — auto-SLO do control plane (auto-monitoramento: tick parado/DB/leader flapping/agentes).
		selfmon          = flag.Bool("selfmon", true, "R7: control-plane auto-SLO (alerts on stalled tick/DB/leader flapping/agent fleet). -selfmon=false turns it off")
		selfmonInterval  = flag.Int("selfmon-interval-sec", 30, "R7: auto-monitoring cadence (s)")
		selfmonTickStale = flag.Int("selfmon-tick-stale-sec", 90, "R7: last-tick age above which it alerts (s)")
		otelEndpoint     = flag.String("otel-endpoint", envOr("OTEL_EXPORTER_OTLP_ENDPOINT", ""), "OTLP/HTTP endpoint for distributed tracing (empty = off)")
		otelService      = flag.String("otel-service", envOr("OTEL_SERVICE_NAME", "regente-server"), "service.name in the traces")
		gitEnable        = flag.Bool("git-commit", false, "Commit saves via git (workspace must be a git repo)")
		apiToken         = flag.String("api-token", envOr("REGENTE_TOKEN", "dev-token"), "Bearer token for API + WS")
		demoMode         = flag.Bool("demo-mode", envOr("REGENTE_DEMO_MODE", "") == "1", "Demo/playground: with no agent online, jobs are mock-finalized OK. Default OFF (production): with no agent, the instance stays WAITING and retries when an agent connects")
		serverAgent      = flag.Bool("server-agent", envOr("REGENTE_SERVER_AGENT", "1") != "0", "Registers the built-in SERVER-AGENT (runs HTTP/REST jobs on the server itself, no external agent). Turn it off with -server-agent=false / REGENTE_SERVER_AGENT=0")
		trustedProxyCIDR = flag.String("trusted-proxies", envOr("REGENTE_TRUSTED_PROXIES", ""), "Comma-separated CIDRs/IPs allowed to set X-Forwarded-For / X-Real-IP (empty = loopback only, which is the nginx-in-front layout of deploy/vps). Headers from any other peer are ignored, so the audit trail cannot be spoofed")

		// F13 GitOps — SEM default de repositório: cada instalação aponta pro SEU
		// workspace (o repo é diferente em cada instalação). Vazio = modo offline,
		// só disco local. Um default aqui faria toda instalação nova tentar clonar
		// o repo de outra pessoa.
		gitSource    = flag.String("git-source", envOr("REGENTE_GIT_SOURCE", ""), "Git remote URL of YOUR workspace repo (source of truth for job definitions). Empty = offline, local disk only")
		gitBranch    = flag.String("git-branch", envOr("REGENTE_GIT_BRANCH", "main"), "Git branch tracked as source-of-truth")
		gitPollSec   = flag.Int("git-poll-interval", 30, "Git polling interval in seconds (0 = disabled)")
		gitWriteMode = flag.String("git-write-mode", envOr("REGENTE_GIT_WRITE_MODE", "direct"), "Write mode: direct | pr-required")

		driftReconcileSec  = flag.Int("drift-reconcile-sec", intEnvOr("REGENTE_DRIFT_RECONCILE_SEC", 0), "Enterprise: cadence (s) of the GitOps drift reconciler; alerts (and optionally reconciles) when runtime≠Git. 0 = off")
		driftReconcileMode = flag.String("drift-reconcile-mode", envOr("REGENTE_DRIFT_RECONCILE_MODE", "alert"), "Drift reconciler: alert (only alerts through the R7 channels) | sync (automatic fetch+reset+reload)")
		ghToken            = flag.String("github-token", envOr("GITHUB_TOKEN", ""), "GitHub PAT for push/PR")
		ghRepo             = flag.String("github-repo", envOr("REGENTE_GIT_REPO", ""), "YOUR workspace repo as 'owner/name' (enables push/PR and the webhook). Empty = disabled")
		ghWebhookSec       = flag.String("github-webhook-secret", envOr("REGENTE_WEBHOOK_SECRET", ""), "Shared secret for GitHub webhook HMAC validation")

		// P3 (2026-04-26) — design session GC.
		designSessionTTLMin    = flag.Int("design-session-ttl-min", 1440, "Design session TTL in minutes (idle since lastTouch); 0 disables GC")
		designSessionGCTickMin = flag.Int("design-session-gc-tick-min", 60, "Design session GC tick interval in minutes; 0 disables GC")

		// H1 (2026-06-14) — SSO/OIDC. auth-mode=oidc ativa o flow; default local (admin/senha).
		authMode         = flag.String("auth-mode", envOr("REGENTE_AUTH_MODE", "local"), "Auth mode: local | oidc")
		oidcIssuer       = flag.String("oidc-issuer", envOr("REGENTE_OIDC_ISSUER", ""), "OIDC issuer URL (e.g. https://idp/realms/regente)")
		oidcClientID     = flag.String("oidc-client-id", envOr("REGENTE_OIDC_CLIENT_ID", ""), "OIDC client id")
		oidcClientSecret = flag.String("oidc-client-secret", envOr("REGENTE_OIDC_CLIENT_SECRET", ""), "OIDC client secret")
		oidcRedirectURL  = flag.String("oidc-redirect-url", envOr("REGENTE_OIDC_REDIRECT_URL", ""), "OIDC redirect URL (e.g. http://localhost:8080/api/auth/oidc/callback)")
		oidcDefaultRole  = flag.String("oidc-default-role", envOr("REGENTE_OIDC_DEFAULT_ROLE", "viewer"), "Role on the 1st federated login: viewer|operator|admin")
		appURL           = flag.String("app-url", envOr("REGENTE_APP_URL", ""), "SPA URL for the post-OIDC-login redirect (e.g. http://localhost:5173)")

		showVersion = flag.Bool("version", false, "print the build version and exit")
	)
	flag.Parse()

	// Antes de abrir banco, workspace ou porta: `-version` tem de responder numa
	// máquina onde o serviço já está rodando, sem disputar nada com ele.
	if *showVersion {
		fmt.Println(version)
		return
	}

	dialect, err := db.ParseDialect(*dbDriver)
	if err != nil {
		log.Fatalf("db driver: %v", err)
	}
	database, err := db.Open(dialect, *dbPath)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer database.Close()
	// R6/DR — modo backup one-shot: -backup <dest> faz um snapshot online e sai.
	if *backupTo != "" {
		if err := db.OnlineBackup(database, *backupTo); err != nil {
			log.Fatalf("[backup] %v", err)
		}
		log.Printf("[backup] snapshot online gravado em %s", *backupTo)
		return
	}
	if err := db.Migrate(database); err != nil {
		log.Fatalf("db migrate: %v", err)
	}
	log.Printf("[db] driver=%s", dialect)

	// I1 — tracing OTLP (opt-in). Sem endpoint, no-op (zero overhead).
	otelShutdown, otelErr := telemetry.Init(context.Background(), *otelEndpoint, *otelService)
	if otelErr != nil {
		log.Printf("[otel] init failed (%v) — tracing off", otelErr)
	} else if *otelEndpoint != "" {
		log.Printf("[otel] tracing OTLP -> %s (service=%s)", *otelEndpoint, *otelService)
	}
	defer func() { _ = otelShutdown(context.Background()) }()

	// F11.10 — bootstrap admin if no users exist
	if err := auth.Bootstrap(database); err != nil {
		log.Fatalf("auth bootstrap: %v", err)
	}
	_ = auth.PurgeExpiredSessions(database)

	// H3 — secrets manager. Ordem de resolução do PAT/webhook secret:
	//   1. flag/env explícito (-github-token / GITHUB_TOKEN, -github-webhook-secret)
	//   2. secrets provider (REGENTE_SECRET_<KEY> ou -secrets-file)  ← tira do DB em claro
	//   3. settings DB (persistido via UI; compat com o fluxo atual)
	secretStore := secrets.Default(*secretsFile)
	log.Printf("[secrets] provider chain: %s", secretStore.Name())

	effectiveToken := *ghToken
	if effectiveToken == "" {
		if v, ok := secretStore.Get("github_token"); ok {
			effectiveToken = v
			log.Printf("[secrets] github_token resolvido via provider")
		}
	}
	if effectiveToken == "" {
		var saved string
		if err := database.QueryRow(`SELECT value FROM settings WHERE key='github_token'`).Scan(&saved); err == nil && saved != "" {
			effectiveToken = saved
			log.Printf("[git] PAT loaded from settings (token via UI)")
		}
	}
	// P13 — webhook secret: flag/env > secrets provider > settings.
	effectiveWebhookSecret := *ghWebhookSec
	if effectiveWebhookSecret == "" {
		if v, ok := secretStore.Get("webhook_secret"); ok {
			effectiveWebhookSecret = v
			log.Printf("[secrets] webhook_secret resolvido via provider")
		}
	}
	if effectiveWebhookSecret == "" {
		var saved string
		if err := database.QueryRow(`SELECT value FROM settings WHERE key='webhook_secret'`).Scan(&saved); err == nil && saved != "" {
			effectiveWebhookSecret = saved
			log.Printf("[github] webhook secret loaded from settings")
		}
	}

	store := storage.NewFileStore(*workspace, *gitEnable)

	// === F13 GitOps boot ===
	var (
		gitOps   *storage.GitOps
		ghClient *storage.GitHubClient
		prWriter *storage.PRWriter
	)
	writeMode := storage.ParseWriteMode(*gitWriteMode)

	if *gitSource != "" {
		gitOps = storage.NewGitOps(*workspace, *gitSource, *gitBranch)
		gitOps.InjectToken(effectiveToken)
		logSource := *gitSource // safe to log (no token)
		log.Printf("[git] source=%s branch=%s — ensuring clone...", logSource, *gitBranch)
		// O boot NÃO é fatal: um repo/PAT errado derrubava o processo, e com
		// Restart=always isso vira crash-loop — o operador fica sem UI justamente
		// na tela onde arrumaria a configuração. Aqui o server sobe, o motivo
		// aparece em /api/git/status e uma goroutine tenta de novo até conseguir.
		if err := gitOps.EnsureClone(); err != nil {
			log.Printf("[git] WARNING: workspace not synced: %v", err)
			log.Printf("[git] the server is UP and the UI is reachable. Retrying in the background (a transient failure recovers on its own; a PAT saved in Settings -> GitHub applies immediately). Wrong repository? sudo regente-configure (it restarts the service). The daily is skipped until then")
			go retryGitClone(gitOps)
		} else {
			st := gitOps.Status()
			log.Printf("[git] ready: %s@%s (last sync %s)", st.Branch, st.ShortSHA, st.LastSync.Format("15:04:05"))
		}
	} else {
		log.Printf("[git] offline mode (no -git-source): definitions are read from %s only. Point it at your own workspace repo to enable GitOps", *workspace)
	}
	// Ensure definitions dir exists after clone (or when git is off)
	if err := os.MkdirAll(*workspace+"/definitions", 0o755); err != nil {
		log.Fatalf("workspace init: %v", err)
	}

	if *ghRepo != "" {
		gh, err := storage.NewGitHubClient(effectiveToken, *ghRepo, effectiveWebhookSecret)
		if err != nil {
			// Erro de digitação em REGENTE_GIT_REPO (ex.: a URL inteira em vez de
			// owner/name) não derruba o server: só desliga push/PR/webhook.
			log.Printf("[github] WARNING: invalid -github-repo/REGENTE_GIT_REPO %q: %v — push/PR and the webhook stay OFF (expected format: owner/name)", *ghRepo, err)
		} else {
			ghClient = gh
		}
	}
	if ghClient != nil {
		gh := ghClient
		// Authoritative auth status:
		//  - explicit env/flag PAT (HasToken) enables both push AND PR/API ops.
		//  - embedded creds in source URL only enable push (clone/fetch).
		//  - neither = read-only.
		authMode := "none"
		switch {
		case gh.HasToken():
			authMode = "env-token"
		case gitOps != nil && gitOps.HasEmbeddedToken():
			authMode = "embedded-in-url"
		}
		log.Printf("[github] repo=%s/%s auth=%s webhook=%v", gh.Owner(), gh.Repo(), authMode, effectiveWebhookSecret != "")
	}

	if writeMode == storage.WriteModePRRequired {
		if gitOps == nil || ghClient == nil || !ghClient.HasToken() {
			log.Fatalf("[git] write-mode=pr-required requires -git-source, -github-repo and -github-token (env GITHUB_TOKEN)")
		}
		prWriter = &storage.PRWriter{
			Git:    gitOps,
			GH:     ghClient,
			Store:  store,
			Mode:   writeMode,
			Author: "regente",
		}
		log.Printf("[git] write-mode=pr-required (every save will open a PR)")
	} else {
		log.Printf("[git] write-mode=direct (saves write to disk; PR mode disabled)")
	}

	// Etapa 3+4+5 (2026-04-26) — Design SessionManager
	// Cada session vira clone Git efêmero em ./sessions/<sid>/.
	// Sem GitOps configurado, sessions ficam desligadas (UI degrada gracefully).
	var sessionMgr *storage.SessionManager
	if gitOps != nil {
		sessionMgr = storage.NewSessionManager("./sessions", *gitSource, *gitBranch, effectiveToken, ghClient, database)
		if err := sessionMgr.Restore(); err != nil {
			log.Printf("[design] restore warning: %v", err)
		}
		log.Printf("[design] sessions enabled (root=./sessions, source=%s)", *gitSource)
		sessionMgr.StartGC(
			time.Duration(*designSessionTTLMin)*time.Minute,
			time.Duration(*designSessionGCTickMin)*time.Minute,
		)
	} else {
		log.Printf("[design] sessions disabled (no GitOps)")
	}

	h := hub.New()

	// R5 — bus: local (hub por-processo, default) ou distribuído via NATS (opt-in).
	// No modo distribuído, eventos web fazem fan-out entre nós e o dispatch é roteado
	// ao nó dono do agent — então o failover do líder não estranda agentes.
	var theBus scheduler.Bus = h
	var remotePresence api.RemotePresence // R5 — frota cross-nó na tela de Agentes; nil = single-node
	if strings.EqualFold(*busMode, "nats") {
		tr, err := bus.DialNATS(*natsURL)
		if err != nil {
			log.Fatalf("[bus] NATS connection failed: %v", err)
		}
		dbus := bus.NewDistributed(*nodeID, h, tr)
		if err := dbus.Start(); err != nil {
			log.Fatalf("[bus] distributed start: %v", err)
		}
		theBus = dbus
		remotePresence = dbus
		log.Printf("[bus] distributed via NATS url=%s node=%s", *natsURL, *nodeID)
	} else {
		log.Printf("[bus] local (per-process hub)")
	}

	sched := scheduler.New(store, database, theBus, time.Duration(*tickMs)*time.Millisecond)
	sched.DemoMode = *demoMode
	if *demoMode {
		log.Printf("[scheduler] DEMO MODE — no agent online, jobs are mock-finalized OK (do not use in production)")
	}

	// SERVER-AGENT — o agente padrão EMBUTIDO: todo server executa jobs HTTP/REST
	// sozinho (quem quer disparar uma API dispara direto do SERVER-AGENT), sem
	// regente-agent externo. Registrado no hub como agente normal: aparece na
	// tela de Agentes e é pinável no Design. Não sobe em demo-mode (lá tudo é
	// mock — um agente real executaria HTTP de verdade no playground).
	if *serverAgent && !*demoMode {
		serveragent.Start(h, database, func(id string, st domain.InstanceStatus, exit int, out string) {
			sched.FinishInstance(id, st, exit, out)
		})
		log.Printf("[agent] SERVER-AGENT embutido registrado (capabilities: HTTP/REST)")
	} else if !*serverAgent {
		log.Printf("[agent] SERVER-AGENT embutido DESLIGADO (-server-agent=false)")
	}

	// Opção B (2026-04-26) — daily lê definitions de Git fresh.
	// Sem GitOps atachado, daily cai pro modo legado (lê só do disco local).
	if gitOps != nil {
		sched.AttachGit(gitOps)
		log.Printf("[scheduler] daily-source=git (Option B — fetch+reset before each daily)")
	} else {
		log.Printf("[scheduler] daily-source=local (no GitOps)")
	}

	// === Bloco 2 — Control-M parity engines ===
	calStore := storage.NewCalendarStore(*workspace)
	sched.AttachCalendars(calStore)
	resTracker := scheduler.NewResourceTracker()
	// Registry de capacidade DURÁVEL: recarrega o que o operador configurou no
	// painel Recursos (tabela `resources`) — sem isto, restart zerava as quotas do
	// ambiente. Antes do RebuildResourcesFromRunning (que só reconstrói o USO).
	if err := resTracker.LoadFromDB(database); err != nil {
		log.Printf("[quotas] registry load failed: %v", err)
	}
	sched.AttachResources(resTracker)
	sched.AttachConditions(scheduler.NewConditionEngine(database))
	sched.AttachSLA(scheduler.NewSLAEngine(database, h))
	alertEngine := scheduler.NewAlertEngine(database, h)
	alertEngine.SeedDefaults()
	sched.AttachAlerts(alertEngine)
	if varStore, err := storage.NewVariableStore(database); err != nil {
		log.Printf("[bloco2] variables: %v (continuing without globals)", err)
	} else {
		sched.AttachVariables(varStore)
	}
	log.Printf("[bloco2] calendars/resources/conditions/sla/variables engines attached")
	log.Printf("[alerts] engine attached (Phase 8)")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// G1 — leader election. Postgres: advisory lock (vários nós, 1 líder).
	// SQLite/nó único: sempre líder.
	if dialect == db.Postgres {
		elector := leader.NewPgAdvisory(database.Raw(), leader.DefaultAdvisoryKey, 5*time.Second)
		elector.Start(ctx)
		sched.AttachLeader(elector)
		log.Printf("[ha] leader election: %s", elector.Describe())
	} else {
		sched.AttachLeader(leader.SingleNode{})
		log.Printf("[ha] leader election: %s", leader.SingleNode{}.Describe())
	}

	// R7 — auto-SLO do control plane: o servidor vigia a própria saúde e alerta pelos
	// mesmos canais (Slack/webhook/e-mail/PagerDuty). Só o líder publica; DB caído é logado.
	if *selfmon {
		mon := scheduler.NewControlPlaneMonitor(
			alertEngine, sched.LastTick, sched.IsLeader,
			func(c context.Context) error { return database.PingContext(c) },
			func() int { return len(h.OnlineAgents()) },
			time.Duration(*selfmonInterval)*time.Second,
			time.Duration(*selfmonTickStale)*time.Second,
		)
		mon.Start(ctx)
		log.Printf("[selfmon] R7 control-plane monitor ON (interval=%ds, tick-stale=%ds)", *selfmonInterval, *selfmonTickStale)
	} else {
		log.Printf("[selfmon] R7 control-plane monitor OFF (-selfmon=false)")
	}

	// Enterprise/HA — quotas (F15) são in-memory e vivem no líder. Após restart/
	// failover, reconstrói o uso a partir das instances RUNNING (estado durável)
	// para não furar a capacidade ao retomar o dispatch.
	if n, err := sched.RebuildResourcesFromRunning(); err != nil {
		log.Printf("[quotas] tracker rebuild failed: %v", err)
	} else if n > 0 {
		log.Printf("[quotas] tracker rebuilt: %d RUNNING instances re-registered", n)
	}

	// Unificação de dependências → pool de condições (2026-07-17): backfill
	// one-time para instances ordenadas antes do upgrade (pai já OK ⇒ semeia a
	// condição sintetizada; guardado por meta_flags).
	sched.MigrateConditionsUnify()

	// M1 (schemaV18): backfill one-time das colunas congeladas do Monitoring
	// (label/job_type/conds/…) a partir do definition_snapshot + congelamento
	// da união produtor nos snapshots em voo. Guardado por meta_flags.
	sched.MigrateMonitoringSnapshot()

	// F15 (schemaV19): backfill one-time da coluna `resources` congelada a partir
	// do definition_snapshot — entrada do card WAIT RESOURCE. Guardado por meta_flags.
	sched.MigrateResourcesSnapshot()

	// CL (schemaV21): backfill one-time da coluna `cond_logic` congelada — entrada
	// das linhas OR do grafo (CL-4). Guardado por meta_flags.
	sched.MigrateCondLogicSnapshot()

	// Enterprise/Operação — reconciler de drift GitOps (opt-in). O git-poll já
	// auto-sincroniza; este fecha o lado OPERACIONAL: quando runtime≠Git, alerta
	// pelos MESMOS canais do R7 (Slack/PagerDuty/…), e o modo "sync" reconcilia
	// sozinho (útil quando o git-poll está desligado). Só o líder reconcilia.
	if gitOps != nil && *driftReconcileSec > 0 {
		rec := scheduler.NewDriftReconciler(
			func() (bool, string, error) { d, err := gitOps.CheckDrift(); return d, gitOps.Status().RemoteSHA, err },
			func() error {
				if e := gitOps.SyncFromRemote(); e != nil {
					return e
				}
				sched.ReloadDefs()
				return nil
			},
			sched.IsLeader,
			func(remoteSHA, detail string) {
				alertEngine.FireSystem("git-drift", "Workspace drifted from Git", "warning",
					"Runtime diverged from the repository (remote "+remoteSHA+"). "+detail, int64(*driftReconcileSec)*1000)
			},
			*driftReconcileMode,
			time.Duration(*driftReconcileSec)*time.Second,
		)
		rec.Start(ctx)
		log.Printf("[reconciler] drift GitOps ON (interval=%ds, mode=%s)", *driftReconcileSec, *driftReconcileMode)
	}

	// Fase 1/2 (serverless) — modo de scheduler + papel do processo.
	//   -role=all|api|scheduler   (REGENTE_ROLE)   — quais responsabilidades este nó assume
	//   -scheduler=internal|external (REGENTE_SCHEDULER) — ticker em goroutine vs cron externo
	// Defaults (all+internal) = daemon clássico, comportamento idêntico ao anterior.
	runsScheduler := *role == "all" || *role == "scheduler"
	switch {
	case !runsScheduler:
		log.Printf("[scheduler] role=%s — this node does NOT run scheduling (serves the API only)", *role)
	case *schedulerMode == "external":
		// Sem ticker interno: defs carregadas no boot; cron externo bate em
		// POST /api/scheduler/tick para dirigir daily+dispatch (scale-to-zero).
		sched.ReloadDefs()
		// ARCH-3 — no serverless não há líder de longa duração segurando o
		// dispatch; dois containers podem receber tick ao mesmo tempo. Liga o
		// advisory lock POR-TICK (Postgres) pra serializar ticks sobrepostos. É
		// higiene (o claim atômico já garante corretude); no-op fora do Postgres.
		sched.EnableTickLock()
		log.Printf("[scheduler] mode=external — no internal ticker; drive it via POST /api/scheduler/tick (+/scheduler/daily)")
	default:
		// E4 — fila assíncrona de eventos: só no modo daemon (internal). No modo
		// external/serverless fica desligada de propósito — o processo pode ser
		// congelado entre requests e eventos bufferizados em memória morreriam;
		// lá o write síncrono é o correto.
		sched.StartEventQueue()
		go sched.Run(ctx)
		log.Printf("[scheduler] mode=internal — ticker every %s", time.Duration(*tickMs)*time.Millisecond)
	}

	// F13.1 — polling opcional
	if gitOps != nil && *gitPollSec > 0 {
		log.Printf("[git] polling every %ds", *gitPollSec)
		api.StartGitPolling(ctx, gitOps, *gitPollSec, func() {
			sched.ReloadDefs()
			h.BroadcastWeb("definition.changed", map[string]string{"reason": "git-poll"})
			h.BroadcastWeb("folder.changed", map[string]string{"reason": "git-poll"})
		}, h)
	}

	// H1 — SSO/OIDC discovery (só quando auth-mode=oidc). Falha de discovery
	// não derruba o server: cai pro login local e loga o motivo.
	var oidcProvider *oidc.Provider
	if strings.EqualFold(*authMode, "oidc") {
		cfg := oidc.Config{
			Issuer:       *oidcIssuer,
			ClientID:     *oidcClientID,
			ClientSecret: secretStore.GetOr("oidc_client_secret", *oidcClientSecret),
			RedirectURL:  *oidcRedirectURL,
			DefaultRole:  *oidcDefaultRole,
		}
		p, err := oidc.Discover(ctx, cfg)
		if err != nil {
			log.Printf("[auth] OIDC disabled (discovery failed): %v — using local login", err)
		} else {
			oidcProvider = p
			log.Printf("[auth] SSO/OIDC active: issuer=%s client=%s", *oidcIssuer, *oidcClientID)
		}
	} else {
		log.Printf("[auth] mode=local (admin/password)")
	}

	// Segurança — sink de auditoria p/ SIEM (sempre JSON em stderr; POST se -audit-siem-url).
	auditSink := audit.New(*auditSIEMURL)
	if *auditSIEMURL != "" {
		log.Printf("[audit] SIEM export -> %s", *auditSIEMURL)
	}

	// REGENTE_TOKEN é admin-equivalente (bypassa o login). Se ficou no valor de
	// exemplo, avisa alto e claro: é o erro de segurança mais fácil de cometer
	// numa instalação nova, e o único jeito de o operador perceber é no log.
	switch strings.TrimSpace(*apiToken) {
	case "dev-token", "change-me", "":
		log.Printf("[security] WARNING: REGENTE_TOKEN is still the example value (%q). It is ADMIN-EQUIVALENT and bypasses the login — anyone who reaches this port owns this Regente. Fix it with: sudo regente-configure", *apiToken)
	}

	// Segurança — de quem o server aceita X-Forwarded-For/X-Real-IP. Config
	// ERRADA aqui é falha de segurança (audit forjável), então não cai em
	// default silencioso: valor inválido derruba o boot.
	trustedProxies, tpErr := api.ParseTrustedProxies(*trustedProxyCIDR)
	if tpErr != nil {
		log.Fatalf("[security] -trusted-proxies inválido (%q): %v", *trustedProxyCIDR, tpErr)
	}
	if strings.TrimSpace(*trustedProxyCIDR) != "" {
		log.Printf("[security] trusted proxies: %v (X-Forwarded-For/X-Real-IP from anyone else is ignored)", trustedProxies)
	}

	router := api.NewRouter(api.Config{
		Store:     store,
		DB:        database,
		Hub:       h,
		Scheduler: sched,
		Token:     *apiToken,
		Git:       gitOps,
		GitHub:    ghClient,
		PRWriter:  prWriter,
		WriteMode: writeMode,
		Sessions:  sessionMgr,
		OIDC:      oidcProvider,
		AppURL:    *appURL,
		Audit:     auditSink,
		SPADir:    *spaDir,
		DocsDir:   *docsDir,
		Presence:  remotePresence,
		NodeID:    *nodeID,
		Version:   version,

		TrustedProxies: trustedProxies,
	})

	if *spaDir != "" {
		log.Printf("[spa] servindo o frontend de %s (single-origin: UI+API+WS na porta %s)", *spaDir, *addr)
	}
	if *docsDir != "" {
		log.Printf("[docs] servindo o site de docs de %s em /docs", *docsDir)
	}

	// Segurança — TLS/mTLS opcional. Sem -tls-cert segue em HTTP plano.
	tlsCfg, mtlsOn, tlsErr := sectls.ServerTLS(*tlsCert, *tlsKey, *tlsClientCA)
	if tlsErr != nil {
		log.Fatalf("[tls] %v", tlsErr)
	}

	// I1 — otelhttp instrumenta todas as rotas (latência/status por endpoint).
	srv := &http.Server{Addr: *addr, Handler: otelhttp.NewHandler(router, "regente-server"), ReadHeaderTimeout: 10 * time.Second}
	if tlsCfg != nil {
		srv.TLSConfig = tlsCfg
	}

	go func() {
		gitState := "off"
		if gitOps != nil {
			gitState = fmt.Sprintf("on(mode=%s)", string(writeMode))
		} else if *gitEnable {
			gitState = "local-commit"
		}
		scheme := "http"
		if tlsCfg != nil {
			scheme = "https"
			if mtlsOn {
				scheme = "https+mTLS"
			}
		}
		log.Printf("regente-server %s listening on %s [%s] (workspace=%s, db=%s, gitops=%s)", version, *addr, scheme, *workspace, *dbPath, gitState)
		var err error
		if tlsCfg != nil {
			err = srv.ListenAndServeTLS("", "") // certs já no TLSConfig
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	// E4 — flush final da fila de eventos (e teto das goroutines de fundo):
	// Stop() drena o canal e grava o resto antes de retornar.
	sched.Stop()
}

// retryGitClone — o workspace não sincronizou no boot (repo errado, PAT ausente,
// rede fora). Em vez de derrubar o processo, tentamos de novo em background com
// backoff até 5 min. O operador conserta a config pela UI/regente-configure e o
// GitOps se liga sozinho, sem reiniciar o serviço.
func retryGitClone(g *storage.GitOps) {
	delay := 15 * time.Second
	const maxDelay = 5 * time.Minute
	for {
		time.Sleep(delay)
		if err := g.EnsureClone(); err != nil {
			log.Printf("[git] still not synced (%v) — next attempt in %s", err, delay)
			if delay < maxDelay {
				delay *= 2
				if delay > maxDelay {
					delay = maxDelay
				}
			}
			continue
		}
		st := g.Status()
		log.Printf("[git] recovered: %s@%s — GitOps is live (no restart needed)", st.Branch, st.ShortSHA)
		return
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// intEnvOr — como envOr, mas para flags numéricas (valor inválido cai no default).
func intEnvOr(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// defaultNodeID — id do nó para presence/routing (R5); usa o hostname da máquina.
func defaultNodeID() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "regente-1"
}
