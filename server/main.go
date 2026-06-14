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
	"strings"
	"syscall"
	"time"

	"github.com/Dr0nj/regente-server/internal/api"
	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/leader"
	"github.com/Dr0nj/regente-server/internal/oidc"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/secrets"
	"github.com/Dr0nj/regente-server/internal/storage"
)

func main() {
	var (
		addr        = flag.String("addr", ":8080", "HTTP listen address")
		workspace   = flag.String("workspace", "./workspace", "Path to regente workspace (contains definitions/)")
		dbPath      = flag.String("db", "./regente.db", "DB DSN: caminho do arquivo SQLite, ou connection string Postgres quando -db-driver=postgres")
		dbDriver    = flag.String("db-driver", envOr("REGENTE_DB_DRIVER", "sqlite"), "State store backend: sqlite | postgres")
		secretsFile = flag.String("secrets-file", envOr("REGENTE_SECRETS_FILE", ""), "H3: arquivo JSON de segredos {\"github_token\":...}; env REGENTE_SECRET_<KEY> tem prioridade")
		tickMs      = flag.Int("tick-ms", 2000, "Scheduler tick interval (ms)")
		gitEnable   = flag.Bool("git-commit", false, "Commit saves via git (workspace must be a git repo)")
		apiToken    = flag.String("api-token", envOr("REGENTE_TOKEN", "dev-token"), "Bearer token for API + WS")

		// F13 GitOps — defaults apontam pro regente-workspace (padrão, não configuração)
		gitSource    = flag.String("git-source", envOr("REGENTE_GIT_SOURCE", "https://github.com/Dr0nj/regente-workspace.git"), "Git remote URL for workspace source-of-truth")
		gitBranch    = flag.String("git-branch", envOr("REGENTE_GIT_BRANCH", "main"), "Git branch tracked as source-of-truth")
		gitPollSec   = flag.Int("git-poll-interval", 30, "Git polling interval in seconds (0 = disabled)")
		gitWriteMode = flag.String("git-write-mode", envOr("REGENTE_GIT_WRITE_MODE", "direct"), "Write mode: direct | pr-required")
		ghToken      = flag.String("github-token", envOr("GITHUB_TOKEN", ""), "GitHub PAT for push/PR")
		ghRepo       = flag.String("github-repo", envOr("REGENTE_GIT_REPO", "Dr0nj/regente-workspace"), "GitHub repo as 'owner/name'")
		ghWebhookSec = flag.String("github-webhook-secret", envOr("REGENTE_WEBHOOK_SECRET", ""), "Shared secret for GitHub webhook HMAC validation")

		// P3 (2026-04-26) — design session GC.
		designSessionTTLMin    = flag.Int("design-session-ttl-min", 1440, "Design session TTL in minutes (idle since lastTouch); 0 disables GC")
		designSessionGCTickMin = flag.Int("design-session-gc-tick-min", 60, "Design session GC tick interval in minutes; 0 disables GC")

		// H1 (2026-06-14) — SSO/OIDC. auth-mode=oidc ativa o flow; default local (admin/senha).
		authMode         = flag.String("auth-mode", envOr("REGENTE_AUTH_MODE", "local"), "Auth mode: local | oidc")
		oidcIssuer       = flag.String("oidc-issuer", envOr("REGENTE_OIDC_ISSUER", ""), "OIDC issuer URL (ex.: https://idp/realms/regente)")
		oidcClientID     = flag.String("oidc-client-id", envOr("REGENTE_OIDC_CLIENT_ID", ""), "OIDC client id")
		oidcClientSecret = flag.String("oidc-client-secret", envOr("REGENTE_OIDC_CLIENT_SECRET", ""), "OIDC client secret")
		oidcRedirectURL  = flag.String("oidc-redirect-url", envOr("REGENTE_OIDC_REDIRECT_URL", ""), "OIDC redirect URL (ex.: http://localhost:8080/api/auth/oidc/callback)")
		oidcDefaultRole  = flag.String("oidc-default-role", envOr("REGENTE_OIDC_DEFAULT_ROLE", "viewer"), "Role no 1º login federado: viewer|operator|admin")
		appURL           = flag.String("app-url", envOr("REGENTE_APP_URL", ""), "URL do SPA p/ redirect pós-login OIDC (ex.: http://localhost:5173)")
	)
	flag.Parse()

	dialect, err := db.ParseDialect(*dbDriver)
	if err != nil {
		log.Fatalf("db driver: %v", err)
	}
	database, err := db.Open(dialect, *dbPath)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer database.Close()
	if err := db.Migrate(database); err != nil {
		log.Fatalf("db migrate: %v", err)
	}
	log.Printf("[db] driver=%s", dialect)

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
			log.Printf("[git] PAT carregado de settings (token via UI)")
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
			log.Printf("[github] webhook secret carregado de settings")
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
		if err := gitOps.EnsureClone(); err != nil {
			log.Fatalf("[git] EnsureClone failed: %v", err)
		}
		st := gitOps.Status()
		log.Printf("[git] ready: %s@%s (last sync %s)", st.Branch, st.ShortSHA, st.LastSync.Format("15:04:05"))
	}
	// Ensure definitions dir exists after clone (or when git is off)
	if err := os.MkdirAll(*workspace+"/definitions", 0o755); err != nil {
		log.Fatalf("workspace init: %v", err)
	}

	if *ghRepo != "" {
		gh, err := storage.NewGitHubClient(effectiveToken, *ghRepo, effectiveWebhookSecret)
		if err != nil {
			log.Fatalf("[github] invalid -github-repo: %v", err)
		}
		ghClient = gh
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

	sched := scheduler.New(store, database, h, time.Duration(*tickMs)*time.Millisecond)

	// Opção B (2026-04-26) — daily lê definitions de Git fresh.
	// Sem GitOps atachado, daily cai pro modo legado (lê só do disco local).
	if gitOps != nil {
		sched.AttachGit(gitOps)
		log.Printf("[scheduler] daily-source=git (Opção B — fetch+reset antes de cada daily)")
	} else {
		log.Printf("[scheduler] daily-source=local (sem GitOps)")
	}

	// === Bloco 2 — Control-M parity engines ===
	calStore := storage.NewCalendarStore(*workspace)
	sched.AttachCalendars(calStore)
	sched.AttachResources(scheduler.NewResourceTracker())
	sched.AttachConditions(scheduler.NewConditionEngine(database))
	sched.AttachSLA(scheduler.NewSLAEngine(database, h))
	if varStore, err := storage.NewVariableStore(database); err != nil {
		log.Printf("[bloco2] variables: %v (continuing without globals)", err)
	} else {
		sched.AttachVariables(varStore)
	}
	log.Printf("[bloco2] calendars/resources/conditions/sla/variables engines attached")

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

	go sched.Run(ctx)

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
			log.Printf("[auth] OIDC desabilitado (discovery falhou): %v — usando login local", err)
		} else {
			oidcProvider = p
			log.Printf("[auth] SSO/OIDC ativo: issuer=%s client=%s", *oidcIssuer, *oidcClientID)
		}
	} else {
		log.Printf("[auth] mode=local (admin/senha)")
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
	})

	srv := &http.Server{Addr: *addr, Handler: router, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		gitState := "off"
		if gitOps != nil {
			gitState = fmt.Sprintf("on(mode=%s)", string(writeMode))
		} else if *gitEnable {
			gitState = "local-commit"
		}
		log.Printf("regente-server listening on %s (workspace=%s, db=%s, gitops=%s)", *addr, *workspace, *dbPath, gitState)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
