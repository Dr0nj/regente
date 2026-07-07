// Package api — HTTP + WebSocket endpoints do regente-server.
package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path"
	"path/filepath"

	"github.com/Dr0nj/regente-server/internal/audit"
	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/bus"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/oidc"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Config struct {
	Store     *storage.FileStore
	DB        *db.DB
	Hub       *hub.Hub
	Scheduler *scheduler.Scheduler
	Token     string
	// R5 — presença cross-nó de agents (bus distribuído). nil = single-node/local:
	// a frota mostra só os agents deste nó. Com o bus NATS, reflete o cluster inteiro.
	Presence RemotePresence
	NodeID   string // ID deste nó (rótulo da frota); "" em single-node
	// F13 GitOps (todos opcionais; se nil = modo legado direct sem git remote)
	Git       *storage.GitOps
	GitHub    *storage.GitHubClient
	PRWriter  *storage.PRWriter
	WriteMode storage.WriteMode
	// Etapa 3+4+5 (2026-04-26) — Design sessions efêmeras
	Sessions *storage.SessionManager
	// H1 (2026-06-14) — SSO/OIDC. nil = desabilitado (login local segue ativo).
	OIDC   *oidc.Provider
	AppURL string // destino do redirect pós-login OIDC (ex.: http://localhost:5173)
	// Segurança — exportação de auditoria p/ SIEM (login, writes). nil = no-op.
	Audit *audit.Sink
	// Hosting single-origin: se != "", serve o SPA buildado deste diretório (UI+API+WS
	// na mesma porta, sem CORS). Rotas não-API caem no NotFound → asset direto ou
	// index.html (fallback do roteamento do SPA). Vazio = só API (comportamento legado).
	SPADir string
}

// RemotePresence — agents conectados em OUTROS nós (bus R5). Implementado por
// *bus.Distributed; nil no modo local. Mantido como interface pra não acoplar o
// handler ao transporte e pra ser mockável nos testes.
type RemotePresence interface {
	RemoteAgents() []bus.RemoteAgent
}

type server struct {
	cfg         Config
	agentBroker *agentBroker  // Fase 2 — transporte HTTP long-poll (nil se sem hub)
	pings       *pingRegistry // ping ativo de agentes (round-trip ping/pong)
}

// NewRouter monta o router principal (REST + WS).
func NewRouter(cfg Config) http.Handler {
	s := &server{cfg: cfg, pings: newPingRegistry()}
	if cfg.Hub != nil {
		s.agentBroker = newAgentBroker(cfg.Hub)
	}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(cors)

	r.Get("/health", s.health)
	// R2 — liveness (público): 200 enquanto o processo serve; reporta idade do tick.
	r.Get("/livez", s.livez)
	// R3 — readiness (público): 200 se o nó pode servir respostas corretas (DB alcançável);
	// reporta líder + idade do tick + último daily. Aponte o readinessProbe do k8s aqui.
	r.Get("/readyz", s.readyz)

	// P17 — métricas Prometheus (público, sem auth — scraper-friendly)
	r.Get("/metrics", s.metrics)

	// F20 — env label (public, no auth)
	r.Get("/api/env", s.envLabel)

	// Auth public endpoint (no session required)
	r.Post("/api/auth/login", s.authLogin)

	// H1 — SSO/OIDC (público; o flow de login). Inertes se OIDC == nil.
	r.Get("/api/auth/oidc/login", s.oidcLogin)
	r.Get("/api/auth/oidc/callback", s.oidcCallback)

	r.Route("/api", func(r chi.Router) {
		r.Use(s.authMiddleware)

		// Auth (post-login)
		r.Post("/auth/logout", s.authLogout)
		r.Get("/auth/me", s.authMe)
		r.Post("/auth/change-password", s.authChangePassword)

		// Users (admin-only enforced em handler)
		r.Get("/users", s.listUsers)
		r.Post("/users", s.createUser)
		r.Patch("/users/{id}/role", s.updateUserRole)
		r.Patch("/users/{id}/password", s.resetUserPassword)
		r.Delete("/users/{id}", s.deleteUser)

		// F11.10b — per-folder ACL (admin-only)
		r.Get("/users/{id}/acls", s.listUserACLs)
		r.Put("/users/{id}/acls", s.replaceUserACLs)
		r.Patch("/users/{id}/acls/{folder}", s.setUserACL)

		// Definitions (source of truth em YAML)
		r.Get("/definitions", s.listDefinitions)
		r.With(s.requireWriterMW).Post("/definitions", s.saveDefinition)
		r.With(s.requireWriterMW).Delete("/definitions/{team}/{id}", s.deleteDefinition)
		// F13.5 — audit history per definition
		r.Get("/definitions/{team}/{id}/audit", s.listDefinitionAudit)

		// Folders (subdirs de definitions/) — F11.6 folder lifecycle
		r.Get("/folders", s.listFolders)
		r.With(s.requireWriterMW).Post("/folders", s.createFolder)
		r.With(s.requireWriterMW).Patch("/folders/{name}", s.renameFolder)
		r.With(s.requireWriterMW).Delete("/folders/{name}", s.deleteFolder)
		r.With(s.requireWriterMW).Post("/folders/{name}/archive", s.archiveFolder)

		// Instances (runtime)
		r.Get("/instances", s.listInstances)
		r.Get("/instances/page", s.pageInstances)       // P2/escala: paginação por cursor
		r.Get("/instances/summary", s.summaryInstances) // P2/escala: contadores agregados
		r.With(s.requireWriterMW).Post("/instances/{id}/hold", s.holdInstance)
		r.With(s.requireWriterMW).Post("/instances/{id}/release", s.releaseInstance)
		r.With(s.requireWriterMW).Post("/instances/{id}/cancel", s.cancelInstance)
		r.With(s.requireWriterMW).Post("/instances/{id}/rerun", s.rerunInstance)
		r.With(s.requireWriterMW).Post("/instances/{id}/set-ok", s.setOKInstance)
		r.With(s.requireWriterMW).Post("/instances/{id}/confirm", s.confirmInstance) // Control-M Confirm (confirm:true)
		r.Get("/instances/{id}/events", s.listInstanceEvents)
		r.Get("/instances/{id}/explain", s.explainInstance)   // diferencial: "por que não rodou?"
		r.Get("/instances/{id}/blast-radius", s.blastRadius)  // diferencial: impacto de cancelar/segurar
		r.Get("/instances/{id}/neighborhood", s.neighborhood) // diferencial: grafo local (up/downstream)
		r.Get("/instances/{id}/rca", s.rca)                   // diferencial: causa raiz da falha/bloqueio

		r.Get("/events", s.listEventLog)               // diferencial: event log CQRS-lite (feed do dia)
		r.Get("/audit/export", s.auditExport)          // E2: export JSONL unificado (admin-only, cursor after_id)
		r.Post("/query", s.runQuery)                   // diferencial: NL-query (texto → consulta estruturada)
		r.Get("/daily/diff", s.diffDaily)              // diferencial: o que mudou entre duas diárias
		r.Get("/daily/dryrun", s.dryRunDaily)          // diferencial: simular uma daily futura sem materializar
		r.Get("/daily/status", s.dailyStatus)          // última daily (relógio do server) + horário configurado
		r.Post("/schedule/preview", s.schedulePreview) // calendário-preview: dias que o schedule rodaria (read-only)

		// Daily + Force (Control-M parity)
		r.With(s.requireWriterMW).Post("/daily/run", s.runDaily)
		r.With(s.requireWriterMW).Post("/definitions/{id}/force", s.forceOrder)
		// Fase 1 (serverless) — tick sob demanda para cron externo (scheduler=external)
		r.With(s.requireWriterMW).Post("/scheduler/tick", s.schedulerTick)

		// F11.8 — Find & Update / Mass Update (bulk, transacional por item)
		r.With(s.requireWriterMW).Post("/bulk/instances", s.bulkInstances)

		// Agents
		r.Get("/agents", s.listAgents)
		r.Post("/agents/{id}/ping", s.pingAgent) // ping ativo (round-trip latência)
		// B5 — tokens por agente (admin-only enforced no handler)
		r.Get("/agents/tokens", s.listAgentTokens)
		r.Post("/agents/tokens", s.createAgentToken)
		r.Delete("/agents/tokens/{id}", s.revokeAgentToken)

		// F13 GitOps
		r.Get("/git/status", s.gitStatus)
		r.Get("/git/drift", s.gitDrift)
		r.With(s.requireWriterMW).Post("/git/sync", s.gitSync)
		// Token via UI (admin-only) + cleanup da DB poluída no repo
		r.Post("/git/token", s.setGitToken)
		r.Delete("/git/token", s.clearGitToken)
		r.Post("/git/cleanup-db", s.cleanupDB)
		// P13 — secret do webhook GitHub (HMAC) configurável em runtime
		r.Post("/git/webhook-secret", s.setWebhookSecret)

		// === Design sessions (Etapa 3+4+5 do realinhamento, 2026-04-26) ===
		r.Get("/design/sessions", s.listDesignSessions)
		r.With(s.requireWriterMW).Post("/design/sessions", s.createDesignSession)
		r.Get("/design/sessions/{sid}", s.getDesignSession)
		r.Get("/design/sessions/{sid}/status", s.getDesignSessionStatus)
		r.With(s.requireWriterMW).Delete("/design/sessions/{sid}", s.deleteDesignSession)
		r.Get("/design/sessions/{sid}/folders", s.listSessionFolders)
		r.Get("/design/sessions/{sid}/definitions", s.listSessionDefinitions)
		r.With(s.requireWriterMW).Post("/design/sessions/{sid}/definitions", s.saveSessionDefinition)
		r.With(s.requireWriterMW).Delete("/design/sessions/{sid}/definitions/{team}/{id}", s.deleteSessionDefinition)
		r.With(s.requireWriterMW).Post("/design/sessions/{sid}/folders", s.createSessionFolder)
		r.With(s.requireWriterMW).Post("/design/sessions/{sid}/folders/open", s.openSessionFolder)
		r.With(s.requireWriterMW).Post("/design/sessions/{sid}/publish", s.publishDesignSession)
		// F11.8 — bulk em definitions DA SESSION (move-folder/patch/delete)
		r.With(s.requireWriterMW).Post("/design/sessions/{sid}/bulk", s.bulkSessionDefinitions)
		// Job-as-code — o working set como YAML multi-doc (modo código do Design)
		r.Get("/design/sessions/{sid}/code", s.getSessionCode)
		r.With(s.requireWriterMW).Post("/design/sessions/{sid}/code", s.applySessionCode)
		// CTM-3 — Mass Update rico (critério/regex → preview → apply → undo)
		r.With(s.requireWriterMW).Post("/design/sessions/{sid}/massupdate", s.massUpdateSession)
		r.With(s.requireWriterMW).Post("/design/sessions/{sid}/massupdate/undo", s.massUpdateUndo)

		// === Bloco 2 — Control-M parity ===
		// F14 Calendars
		r.Get("/calendars", s.listCalendars)
		r.Get("/calendars/{name}", s.getCalendar)
		r.With(s.requireWriterMW).Put("/calendars/{name}", s.saveCalendar)
		r.With(s.requireWriterMW).Delete("/calendars/{name}", s.deleteCalendar)
		// F15 Resources
		r.Get("/resources", s.listResources)
		r.With(s.requireWriterMW).Put("/resources/{name}", s.setResourceCapacity)
		r.With(s.requireWriterMW).Delete("/resources/{name}", s.deleteResource)

		// F18 Variables (globals)
		r.Get("/variables", s.listVariables)
		r.Put("/variables/{name}", s.putVariable)
		r.Delete("/variables/{name}", s.deleteVariable)
		// F16 Conditions
		r.Get("/conditions", s.listConditions)
		r.With(s.requireWriterMW).Post("/conditions/{name}/set", s.setCondition)
		r.With(s.requireWriterMW).Post("/conditions/{name}/unset", s.unsetCondition)
		// F19 SLA
		r.Get("/sla/breaches", s.listSLABreaches)
		// Phase 8 — Alerting
		r.Get("/alerts", s.listAlertEvents)
		r.With(s.requireWriterMW).Post("/alerts/ack-all", s.ackAllAlertEvents)
		r.With(s.requireWriterMW).Post("/alerts/{id}/ack", s.ackAlertEvent)
		r.Get("/alerts/rules", s.listAlertRules)
		r.With(s.requireWriterMW).Post("/alerts/rules/{id}/toggle", s.toggleAlertRule)
		r.With(s.requireWriterMW).Put("/alerts/rules/{id}/channels", s.setAlertRuleChannels)
		r.With(s.requireWriterMW).Put("/alerts/rules/{id}/cooldown", s.setAlertRuleCooldown)
		// ViewPoints salvos (filtros nomeados do Monitoring; base dos dashboards)
		r.Get("/viewpoints", s.listViewpoints)
		r.Post("/viewpoints", s.createViewpoint)
		r.Delete("/viewpoints/{id}", s.deleteViewpoint)

		// F21 Forecast
		r.Get("/forecast", s.getForecast)
		// F22 Analytics
		r.Get("/analytics/summary", s.analyticsSummary)
		r.Get("/analytics/top-failing", s.analyticsTopFailing)
		r.Get("/analytics/mttr", s.analyticsMTTR)

		// F20 Settings (admin-only for writes)
		r.Get("/settings", s.getSettings)
		r.Put("/settings", s.putSettings)
	})

	// WebSockets (auth via query ?token=...)
	r.Get("/ws/web", s.wsWeb)
	r.Get("/ws/agent", s.wsAgent)

	// Fase 2 — transporte HTTP long-poll p/ agentes (auth própria por agent token).
	// Fora do grupo /api (que exige sessão/legacy), como o /ws/agent.
	r.Get("/api/agent/poll", s.agentPoll)
	r.Post("/api/agent/result", s.agentResult)
	r.Post("/api/agent/output", s.agentOutput)

	// F13.3 — webhook GitHub (público; auth via HMAC do payload)
	r.Post("/api/git/webhook", s.gitWebhook)

	// Hosting single-origin: rotas não registradas acima (/, /design, /assets/*, etc.)
	// caem aqui e servem o SPA. As rotas de API/WS/metrics já foram casadas antes, então
	// nunca chegam no NotFound. Vazio = mantém o 404 padrão (só API).
	if cfg.SPADir != "" {
		r.NotFound(serveSPA(cfg.SPADir))
	}

	return r
}

// serveSPA devolve um handler que serve o build do frontend: se o caminho aponta pra
// um arquivo existente (assets com hash, logo, etc.) serve direto; senão devolve o
// index.html pra o roteamento client-side do SPA. GET/HEAD apenas.
func serveSPA(dir string) http.HandlerFunc {
	fileServer := http.FileServer(http.Dir(dir))
	index := filepath.Join(dir, "index.html")
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		// path.Clean + filepath.Join mantêm o alvo DENTRO de dir (barra traversal).
		full := filepath.Join(dir, filepath.FromSlash(path.Clean("/"+r.URL.Path)))
		if st, err := os.Stat(full); err == nil && !st.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, index)
	}
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authMiddleware aceita:
//  1. Session token (Bearer) emitido por /api/auth/login → resolve para User real.
//  2. Legacy bearer == cfg.Token → injeta um pseudo-user "system" admin (para
//     compat com agent + ferramentas curl existentes).
func (s *server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := auth.ExtractToken(r)
		if tok == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// Legacy token (env REGENTE_TOKEN / dev-token) → admin equivalente
		if s.cfg.Token != "" && tok == s.cfg.Token {
			ctx := auth.WithUser(r.Context(), &auth.User{
				ID:       0,
				Username: "system",
				Role:     auth.RoleAdmin,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		u, err := auth.Resolve(s.cfg.DB, tok)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), u)))
	})
}

// requireWriterMW gateia mutation endpoints (operator/admin).
func (s *server) requireWriterMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.requireWriter(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// wsTokenOK aceita legacy token OU session token resolvido.
func (s *server) wsTokenOK(r *http.Request) bool {
	tok := auth.ExtractToken(r)
	if tok == "" {
		return false
	}
	if s.cfg.Token != "" && tok == s.cfg.Token {
		return true
	}
	if _, err := auth.Resolve(s.cfg.DB, tok); err == nil {
		return true
	}
	return false
}

// silenced legacy helper kept for grep history; not used.

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
