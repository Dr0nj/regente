// Package api — HTTP + WebSocket endpoints do regente-server.
package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Dr0nj/regente-server/internal/audit"
	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/bus"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/execution"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/oidc"
	"github.com/Dr0nj/regente-server/internal/runtimeprofile"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Config struct {
	ExecutionLab  bool // I08: opt-in development; recusado em production.
	RuntimePolicy runtimeprofile.Config
	Store         *storage.FileStore
	DB            *db.DB
	Hub           *hub.Hub
	Scheduler     *scheduler.Scheduler
	Token         string
	Events        interface{ BroadcastWeb(string, interface{}) } // fan-out configurado; nil usa Hub
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
	OIDC          *oidc.Provider
	AppURL        string // destino do redirect pós-login OIDC (ex.: http://localhost:5173)
	AuthMode      string
	EmergencyUser string
	// Segurança — exportação de auditoria p/ SIEM (login, writes). nil = no-op.
	Audit *audit.Sink
	// Hosting single-origin: se != "", serve o SPA buildado deste diretório (UI+API+WS
	// na mesma porta, sem CORS). Rotas não-API caem no NotFound → asset direto ou
	// index.html (fallback do roteamento do SPA). Vazio = só API (comportamento legado).
	SPADir string
	// ADV-7 — site de docs (cmd/docsite): se != "", serve o site estático em /docs
	// na mesma porta (single-origin, self-contained). Vazio = sem docs.
	DocsDir string
	// Version — versão do build (injetada por -ldflags na release; "dev" do fonte).
	// Servida em GET /api/version, que é o que o rodapé da UI mostra. Fica ATRÁS da
	// autenticação de propósito: entregar a versão exata a um anônimo é entregar a
	// lista de CVEs aplicáveis junto, e o /health (público) não precisa disso.
	Version string
	// TrustedProxies — de quem o servidor aceita X-Forwarded-For/X-Real-IP.
	// Vazio = só loopback (a topologia do deploy/vps: nginx em 127.0.0.1). Ver
	// realip.go: header vindo de peer fora desta lista é IGNORADO, senão
	// qualquer um forja a origem no audit/SIEM.
	TrustedProxies []netip.Prefix
}

// RemotePresence — agents conectados em OUTROS nós (bus R5). Implementado por
// *bus.Distributed; nil no modo local. Mantido como interface pra não acoplar o
// handler ao transporte e pra ser mockável nos testes.
type RemotePresence interface {
	RemoteAgents() []bus.RemoteAgent
}

// D-5 — o chi só roteia métodos que conhece: registra o verbo QUERY (draft
// IETF httpbis) uma vez, no load do pacote, antes de qualquer r.Method("QUERY").
func init() { chi.RegisterMethod("QUERY") }

type server struct {
	attempts    *execution.Engine
	cfg         Config
	agentBroker *agentBroker  // Fase 2 — transporte HTTP long-poll (nil se sem hub)
	pings       *pingRegistry // ping ativo de agentes (round-trip ping/pong)
}

// NewRouter monta o router principal (REST + WS).
func NewRouter(cfg Config) http.Handler {
	s := &server{cfg: cfg, pings: newPingRegistry()}
	if cfg.RuntimePolicy.Durable() || (cfg.ExecutionLab && !cfg.RuntimePolicy.Production()) {
		if cfg.Scheduler != nil {
			s.attempts = execution.New(cfg.DB, cfg.Scheduler.Now)
		} else {
			s.attempts = execution.New(cfg.DB, nil)
		}
	}
	if cfg.RuntimePolicy.Durable() && cfg.Scheduler != nil {
		if existing := cfg.Scheduler.DurableEngine(); existing != nil {
			s.attempts = existing
		} else {
			cfg.Scheduler.AttachDurable(s.attempts)
		}
	}
	if cfg.Hub != nil {
		s.agentBroker = newAgentBroker(cfg.Hub)
	}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	// O middleware.RealIP do chi saiu daqui: foi deprecado por ser spoofável
	// (GHSA-3fxj-6jh8-hvhx e cia). O substituto só honra X-Forwarded-For/X-Real-IP
	// vindos de um proxy confiável — ver realip.go.
	r.Use(realIP(cfg.TrustedProxies))
	r.Use(s.cors)

	r.Get("/health", s.scoped((*server).health))
	// R2 — liveness (público): 200 enquanto o processo serve; reporta idade do tick.
	r.Get("/livez", s.scoped((*server).livez))
	// R3 — readiness (público): 200 se o nó pode servir respostas corretas (DB alcançável);
	// reporta líder + idade do tick + último daily. Aponte o readinessProbe do k8s aqui.
	r.Get("/readyz", s.scoped((*server).readyz))

	// P17 — métricas Prometheus (público, sem auth — scraper-friendly)
	r.Get("/metrics", s.scoped((*server).metrics))

	// F20 — env label (public, no auth)
	r.Get("/api/env", s.scoped((*server).envLabel))

	// Auth public endpoint (no session required)
	r.Post("/api/auth/login", s.scoped((*server).authLogin))
	r.Get("/api/auth/config", s.scoped((*server).authConfig))

	// H1 — SSO/OIDC (público; o flow de login). Inertes se OIDC == nil.
	r.Get("/api/auth/oidc/login", s.scoped((*server).oidcLogin))
	r.Get("/api/auth/oidc/callback", s.scoped((*server).oidcCallback))

	r.Route("/api", func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Get("/audit/security", s.scoped((*server).securityAudit))
		r.Get("/audit/security/status", s.scoped((*server).securityAuditStatus))
		r.Post("/audit/security/retry", s.scoped((*server).retrySecurityAudit))

		// Auth (post-login)
		r.Post("/auth/logout", s.scoped((*server).authLogout))
		r.Get("/auth/me", s.scoped((*server).authMe))
		r.Post("/auth/event-ticket", s.scoped((*server).authEventTicket))
		r.Post("/auth/change-password", s.scoped((*server).authChangePassword))

		// Users (admin-only enforced em handler)
		r.Get("/users", s.scoped((*server).listUsers))
		r.Post("/users", s.scoped((*server).createUser))
		r.Patch("/users/{id}/role", s.scoped((*server).updateUserRole))
		r.Patch("/users/{id}/password", s.scoped((*server).resetUserPassword))
		r.Delete("/users/{id}", s.scoped((*server).deleteUser))
		r.Post("/users/{id}/identity", s.scoped((*server).linkIdentity))
		r.Patch("/users/{id}/access", s.scoped((*server).userAccess))

		// F11.10b — per-folder ACL (admin-only)
		r.Get("/users/{id}/acls", s.scoped((*server).listUserACLs))
		r.Put("/users/{id}/acls", s.scoped((*server).replaceUserACLs))
		r.Patch("/users/{id}/acls/{folder}", s.scoped((*server).setUserACL))

		// ADV-1 — catálogo de jobTypes com schema dedicado por tipo (read-only)
		r.Get("/jobtypes", s.scoped((*server).jobTypeCatalog))

		// Definitions (source of truth em YAML)
		r.Get("/definitions", s.scoped((*server).listDefinitions))
		r.With(s.requireWriterMW).Post("/definitions", s.scoped((*server).saveDefinition))
		r.With(s.requireWriterMW).Delete("/definitions/{team}/{id}", s.scoped((*server).deleteDefinition))
		// F13.5 — audit history per definition
		r.Get("/definitions/{team}/{id}/audit", s.scoped((*server).listDefinitionAudit))

		// Folders (subdirs de definitions/) — F11.6 folder lifecycle
		r.Get("/folders", s.scoped((*server).listFolders))
		r.With(s.requireWriterMW).Post("/folders", s.scoped((*server).createFolder))
		r.With(s.requireWriterMW).Patch("/folders/{name}", s.scoped((*server).renameFolder))
		r.With(s.requireWriterMW).Delete("/folders/{name}", s.scoped((*server).deleteFolder))
		r.With(s.requireWriterMW).Post("/folders/{name}/archive", s.scoped((*server).archiveFolder))
		r.With(s.requireWriterMW).Put("/folders/{name}/layout", s.scoped((*server).setFolderLayout)) // UI-3: override de grade por folder

		// I08: API administrativa de laboratório, isolada de instances/condições.
		r.Post("/lab/orders", s.scoped((*server).labCreateOrder))
		r.Get("/lab/orders/{id}", s.scoped((*server).labOrder))
		r.Post("/lab/orders/{id}/attempts", s.scoped((*server).labStart))
		r.Post("/lab/orders/{id}/cancel", s.scoped((*server).labCancel))
		r.Get("/lab/executions/{id}", s.scoped((*server).labAttempt))
		r.Get("/lab/executions/{id}/output", s.scoped((*server).labOutput))
		r.Post("/lab/reconcile", s.scoped((*server).labReconcile))

		// Instances (runtime)
		r.Get("/instances", s.scoped((*server).listInstances))
		r.Get("/instances/page", s.scoped((*server).pageInstances))       // P2/escala: paginação por cursor
		r.Get("/instances/summary", s.scoped((*server).summaryInstances)) // P2/escala: contadores agregados
		r.Get("/instances/{id}/executions", s.scoped((*server).instanceExecutions))
		r.With(s.requireWriterMW).Post("/executions/{id}/resolve", s.scoped((*server).resolveExecution))
		r.With(s.requireWriterMW).Post("/execution-effects/{id}/resolve", s.scoped((*server).resolveExecutionEffect))
		r.Get("/instances/{id}", s.scoped((*server).getInstance)) // detalhe: linha + action congelada da ordem (snapshot)
		// D-5 — query estruturada composta (POST baseline; QUERY = progressive
		// enhancement, o verbo IETF safe+idempotente com body — mesma handler).
		r.Post("/instances/query", s.scoped((*server).queryInstances))
		r.Method("QUERY", "/instances/query", http.HandlerFunc(s.scoped((*server).queryInstances)))
		r.With(s.requireWriterMW).Post("/instances/{id}/hold", s.scoped((*server).holdInstance))
		r.With(s.requireWriterMW).Post("/instances/{id}/release", s.scoped((*server).releaseInstance))
		r.With(s.requireWriterMW).Post("/instances/{id}/cancel", s.scoped((*server).cancelInstance))
		r.With(s.requireWriterMW).Post("/instances/{id}/rerun", s.scoped((*server).rerunInstance))
		r.With(s.requireWriterMW).Post("/instances/{id}/set-ok", s.scoped((*server).setOKInstance))
		r.With(s.requireWriterMW).Post("/instances/{id}/confirm", s.scoped((*server).confirmInstance)) // Control-M Confirm (confirm:true)
		r.With(s.requireWriterMW).Post("/instances/{id}/force", s.scoped((*server).forceRunInstance))  // Run Now: força ESTA instance (bypass gates, honra agent+Confirm)
		r.With(s.requireWriterMW).Delete("/instances/{id}", s.scoped((*server).deleteInstance))        // Delete: remove a ordem (SÓ em HOLD; RUNNING nunca)
		r.Get("/instances/{id}/events", s.scoped((*server).listInstanceEvents))
		r.Get("/instances/{id}/output", s.scoped((*server).getInstanceOutput))  // OL-2: sysout da execução (por tentativa, live-tail)
		r.Get("/instances/{id}/explain", s.scoped((*server).explainInstance))   // diferencial: "por que não rodou?"
		r.Get("/instances/{id}/blast-radius", s.scoped((*server).blastRadius))  // diferencial: impacto de cancelar/segurar
		r.Get("/instances/{id}/neighborhood", s.scoped((*server).neighborhood)) // diferencial: grafo local (up/downstream)
		r.Get("/instances/{id}/rca", s.scoped((*server).rca))                   // diferencial: causa raiz da falha/bloqueio
		// D-11 — chaos engineering: falha sintética pelo fluxo REAL de falha
		r.With(s.requireWriterMW).Post("/instances/{id}/inject-failure", s.scoped((*server).injectFailure))
		// D-2 — pause/resume de workflow (folder) com estado preservado
		r.With(s.requireWriterMW).Post("/folders/{name}/pause", s.scoped((*server).pauseFolder))
		r.With(s.requireWriterMW).Post("/folders/{name}/resume", s.scoped((*server).resumeFolder))
		// Order Folder — ordena a folder inteira na diária ATIVA (pula quem já está nela)
		r.With(s.requireWriterMW).Post("/folders/{name}/order", s.scoped((*server).orderFolder))

		r.Get("/events", s.scoped((*server).listEventLog))            // diferencial: event log CQRS-lite (feed do dia)
		r.Get("/audit/export", s.scoped((*server).auditExport))       // E2: export JSONL unificado (admin-only, cursor after_id)
		r.Get("/archive", s.scoped((*server).listArchives))           // ADV-5: dailies arquivadas pela retenção (admin-only)
		r.Get("/archive/{file}", s.scoped((*server).downloadArchive)) // ADV-5: download do NDJSON de um dia (admin-only)
		r.Post("/query", s.scoped((*server).runQuery))                // diferencial: NL-query (texto → consulta estruturada)
		// D-3 — event-driven confiável: ingestão idempotente de eventos externos
		r.With(s.requireWriterMW).Post("/events/ingest", s.scoped((*server).ingestEvent))
		r.Get("/events/external", s.scoped((*server).listExternalEvents))
		// Versão do build — o que o rodapé da UI mostra. Responde "atualizei mesmo?"
		// sem SSH na caixa: se a release publicou v0.2.7 e aqui ainda diz v0.2.6, o
		// processo antigo continua no ar.
		r.Get("/version", s.scoped((*server).getVersion))
		// D-10 — policy as code: política ativa + violações do workspace publicado
		r.Get("/policy", s.scoped((*server).getPolicy))
		r.Get("/daily/diff", s.scoped((*server).diffDaily))              // diferencial: o que mudou entre duas diárias
		r.Get("/daily/dryrun", s.scoped((*server).dryRunDaily))          // diferencial: simular uma daily futura sem materializar
		r.Get("/daily/status", s.scoped((*server).dailyStatus))          // última daily (relógio do server) + horário configurado
		r.Get("/daily/report", s.scoped((*server).dailyReport))          // E5: relatório/SLO da daily (counts/lateStart/failures/slaBreaches)
		r.Post("/schedule/preview", s.scoped((*server).schedulePreview)) // calendário-preview: dias que o schedule rodaria (read-only)

		// Daily + Force (Control-M parity)
		r.With(s.requireWriterMW).Post("/daily/run", s.scoped((*server).runDaily))
		r.With(s.requireWriterMW).Post("/daily/resume", s.scoped((*server).resumeDaily))
		r.With(s.requireWriterMW).Post("/definitions/{id}/force", s.scoped((*server).forceOrder))
		// Fase 1 (serverless) — tick sob demanda para cron externo (scheduler=external)
		r.With(s.requireWriterMW).Post("/scheduler/tick", s.scoped((*server).schedulerTick))
		// ARCH-5 — gatilho de daily DEDICADO: um cron diário separado do tick de
		// dispatch materializa a diária (idempotente, leader-guarded).
		r.With(s.requireWriterMW).Post("/scheduler/daily", s.scoped((*server).schedulerDaily))

		// F11.8 — Find & Update / Mass Update (bulk, transacional por item)
		r.With(s.requireWriterMW).Post("/bulk/instances", s.scoped((*server).bulkInstances))

		// Agents
		r.Get("/agents", s.scoped((*server).listAgents))
		r.Post("/agents/{id}/ping", s.scoped((*server).pingAgent)) // ping ativo (round-trip latência)
		// B5 — tokens por agente (admin-only enforced no handler)
		r.Get("/agents/tokens", s.scoped((*server).listAgentTokens))
		r.Post("/agents/tokens", s.scoped((*server).createAgentToken))
		r.Post("/agents/tokens/{id}/rotate", s.scoped((*server).rotateAgentToken))
		r.Delete("/agents/tokens/{id}", s.scoped((*server).revokeAgentToken))

		// F13 GitOps
		r.Get("/git/status", s.scoped((*server).gitStatus))
		r.Get("/git/drift", s.scoped((*server).gitDrift))
		r.With(s.requireWriterMW).Post("/git/sync", s.scoped((*server).gitSync))
		// Token via UI (admin-only) + cleanup da DB poluída no repo
		r.Post("/git/token", s.scoped((*server).setGitToken))
		r.Delete("/git/token", s.scoped((*server).clearGitToken))
		r.Post("/git/cleanup-db", s.scoped((*server).cleanupDB))
		// P13 — secret do webhook GitHub (HMAC) configurável em runtime
		r.Post("/git/webhook-secret", s.scoped((*server).setWebhookSecret))

		// === Design sessions (Etapa 3+4+5 do realinhamento, 2026-04-26) ===
		r.Get("/design/sessions", s.scoped((*server).listDesignSessions))
		r.With(s.requireWriterMW).Post("/design/sessions/import", s.scoped((*server).importDraft))
		r.With(s.draftMiddleware).Get("/design/sessions/{sid}/export", s.scoped((*server).exportDraft))
		r.With(s.requireWriterMW, s.draftMiddleware).Post("/design/sessions/{sid}/publication/cancel", s.scoped((*server).cancelDraftPublication))
		r.With(s.requireWriterMW).Post("/design/sessions", s.scoped((*server).createDesignSession))
		r.With(s.draftMiddleware).Get("/design/sessions/{sid}", s.scoped((*server).getDesignSession))
		r.With(s.draftMiddleware).Get("/design/sessions/{sid}/status", s.scoped((*server).getDesignSessionStatus))
		r.With(s.requireWriterMW, s.draftMiddleware).Delete("/design/sessions/{sid}", s.scoped((*server).deleteDesignSession))
		r.With(s.draftMiddleware).Get("/design/sessions/{sid}/folders", s.scoped((*server).listSessionFolders))
		r.With(s.draftMiddleware).Get("/design/sessions/{sid}/definitions", s.scoped((*server).listSessionDefinitions))
		r.With(s.requireWriterMW, s.draftMiddleware).Post("/design/sessions/{sid}/definitions", s.scoped((*server).saveSessionDefinition))
		r.With(s.requireWriterMW, s.draftMiddleware).Delete("/design/sessions/{sid}/definitions/{team}/{id}", s.scoped((*server).deleteSessionDefinition))
		r.With(s.requireWriterMW, s.draftMiddleware).Post("/design/sessions/{sid}/folders", s.scoped((*server).createSessionFolder))
		r.With(s.requireWriterMW, s.draftMiddleware).Post("/design/sessions/{sid}/folders/open", s.scoped((*server).openSessionFolder))
		r.With(s.requireWriterMW, s.draftMiddleware).Put("/design/sessions/{sid}/folders/{name}/layout", s.scoped((*server).setSessionFolderLayout))
		r.With(s.requireWriterMW, s.draftMiddleware).Post("/design/sessions/{sid}/publish", s.scoped((*server).publishDesignSession))
		// F11.8 — bulk em definitions DA SESSION (move-folder/patch/delete)
		r.With(s.requireWriterMW, s.draftMiddleware).Post("/design/sessions/{sid}/bulk", s.scoped((*server).bulkSessionDefinitions))
		// Job-as-code — o working set como YAML multi-doc (modo código do Design)
		r.With(s.draftMiddleware).Get("/design/sessions/{sid}/code", s.scoped((*server).getSessionCode))
		r.With(s.requireWriterMW, s.draftMiddleware).Post("/design/sessions/{sid}/code", s.scoped((*server).applySessionCode))
		// CTM-3 — Mass Update rico (critério/regex → preview → apply → undo)
		r.With(s.requireWriterMW, s.draftMiddleware).Post("/design/sessions/{sid}/massupdate", s.scoped((*server).massUpdateSession))
		r.With(s.requireWriterMW, s.draftMiddleware).Post("/design/sessions/{sid}/massupdate/undo", s.scoped((*server).massUpdateUndo))

		// === Bloco 2 — Control-M parity ===
		// F14 Calendars
		r.Get("/calendars", s.scoped((*server).listCalendars))
		r.Get("/calendars/{name}", s.scoped((*server).getCalendar))
		r.With(s.requireWriterMW).Put("/calendars/{name}", s.scoped((*server).saveCalendar))
		r.With(s.requireWriterMW).Delete("/calendars/{name}", s.scoped((*server).deleteCalendar))
		// F15 Resources
		r.Get("/resources", s.scoped((*server).listResources))
		r.With(s.requireWriterMW).Put("/resources/{name}", s.scoped((*server).setResourceCapacity))
		r.With(s.requireWriterMW).Delete("/resources/{name}", s.scoped((*server).deleteResource))

		// F18 Variables (globals)
		r.Get("/variables", s.scoped((*server).listVariables))
		r.Put("/variables/{name}", s.scoped((*server).putVariable))
		r.Delete("/variables/{name}", s.scoped((*server).deleteVariable))
		// F16 Conditions
		r.Get("/conditions", s.scoped((*server).listConditions))
		r.With(s.requireWriterMW).Post("/conditions/{name}/set", s.scoped((*server).setCondition))
		r.With(s.requireWriterMW).Post("/conditions/{name}/unset", s.scoped((*server).unsetCondition))
		// F19 SLA
		r.Get("/sla/breaches", s.scoped((*server).listSLABreaches))
		// Phase 8 — Alerting
		r.Get("/alerts", s.scoped((*server).listAlertEvents))
		r.With(s.requireWriterMW).Post("/alerts/ack-all", s.scoped((*server).ackAllAlertEvents))
		r.With(s.requireWriterMW).Post("/alerts/{id}/ack", s.scoped((*server).ackAlertEvent))
		r.Get("/alerts/rules", s.scoped((*server).listAlertRules))
		r.With(s.requireWriterMW).Post("/alerts/rules/{id}/toggle", s.scoped((*server).toggleAlertRule))
		r.With(s.requireWriterMW).Put("/alerts/rules/{id}/channels", s.scoped((*server).setAlertRuleChannels))
		r.With(s.requireWriterMW).Put("/alerts/rules/{id}/cooldown", s.scoped((*server).setAlertRuleCooldown))
		// ViewPoints salvos (filtros nomeados do Monitoring; base dos dashboards)
		r.Get("/viewpoints", s.scoped((*server).listViewpoints))
		r.Post("/viewpoints", s.scoped((*server).createViewpoint))
		r.Delete("/viewpoints/{id}", s.scoped((*server).deleteViewpoint))

		// F21 Forecast
		r.Get("/forecast", s.scoped((*server).getForecast))
		r.Get("/forecast/range", s.scoped((*server).getForecastRange)) // ≥1 semana à frente (CTM-6)
		// F22 Analytics
		r.Get("/analytics/summary", s.scoped((*server).analyticsSummary))
		r.Get("/analytics/top-failing", s.scoped((*server).analyticsTopFailing))
		r.Get("/analytics/mttr", s.scoped((*server).analyticsMTTR))
		// D-4 — performance forecasting por histórico (gráfico do drawer + Timeline)
		r.Get("/analytics/forecast", s.scoped((*server).perfForecast))
		r.Get("/analytics/durations", s.scoped((*server).dayDurations))
		// ADV-3 — Statistics por definition + What-If (simulação de cenário, read-only)
		r.Get("/analytics/jobstats", s.scoped((*server).jobStats))
		r.Post("/whatif", s.scoped((*server).whatIf))

		// D-13 — job templates (writer p/ mutar; leitura livre)
		r.Get("/templates", s.scoped((*server).listTemplates))
		r.With(s.requireWriterMW).Post("/templates", s.scoped((*server).saveTemplate))
		r.With(s.requireWriterMW).Delete("/templates/{name}", s.scoped((*server).deleteTemplate))

		// D-14 — self-service portal: gate PRÓPRIO (qualquer logado; opt-in por def)
		r.Get("/selfservice/jobs", s.scoped((*server).selfServiceJobs))
		r.Post("/selfservice/run/{defId}", s.scoped((*server).selfServiceRun))

		// F20 Settings (admin-only for writes)
		r.Get("/settings", s.scoped((*server).getSettings))
		r.Put("/settings", s.scoped((*server).putSettings))
	})

	// WebSockets (auth via query ?token=...)
	r.Get("/ws/web", s.scoped((*server).wsWeb))
	r.Get("/ws/agent", s.scoped((*server).wsAgent))

	// Fase 2 — transporte HTTP long-poll p/ agentes (auth própria por agent token).
	// Fora do grupo /api (que exige sessão/legacy), como o /ws/agent.
	r.Get("/api/agent/v2/poll", s.scoped((*server).executionPoll))
	r.Post("/api/agent/v2/ack", s.scoped((*server).executionAck))
	r.Post("/api/agent/v2/result", s.scoped((*server).executionResult))
	r.Post("/api/agent/v2/output", s.scoped((*server).executionOutput))
	r.Get("/api/agent/poll", s.scoped((*server).agentPoll))
	// ARCH-4 — transporte SSE: stream de dispatch por push imediato (mesmo broker;
	// resultados voltam pelos POSTs abaixo, iguais ao long-poll).
	r.Get("/api/agent/events", s.scoped((*server).agentSSE))
	r.Post("/api/agent/result", s.scoped((*server).agentResult))
	r.Post("/api/agent/output", s.scoped((*server).agentOutput))

	// F13.3 — webhook GitHub (público; auth via HMAC do payload)
	r.Post("/api/git/webhook", s.scoped((*server).gitWebhook))

	// D-15 — quick actions de alerta (públicas; a AUTH é o token HMAC assinado
	// de escopo único). GET = página de confirmação; POST = executa.
	r.Get("/qa/{token}", s.scoped((*server).quickActionPage))
	r.Post("/qa/{token}", s.scoped((*server).quickActionExec))

	// API-1 — contrato OpenAPI da superfície de integração + viewer, embutidos
	// no binário (go:embed): sempre no ar, sem flag. Público como /docs — é
	// documentação. A spec curada vive em openapi.yaml (escrita à mão; NÃO é
	// auto-doc dos handlers) e o teste apidocs_test.go trava spec×router.
	r.Get("/api-docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/api-docs/", http.StatusFound)
	})
	r.Get("/api-docs/", s.scoped((*server).apiDocsPage))
	r.Get("/api-docs/openapi.yaml", s.scoped((*server).apiDocsSpec))
	r.Get("/api-docs/openapi.json", s.scoped((*server).apiDocsSpec))

	// ADV-7 — site de docs em /docs (registrado ANTES do NotFound do SPA, senão o
	// fallback engoliria o caminho). Público como o restante do hosting estático.
	if cfg.DocsDir != "" {
		r.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/docs/", http.StatusFound)
		})
		r.Get("/docs/*", serveDocs(cfg.DocsDir))
	}

	// Hosting single-origin: rotas não registradas acima (/, /design, /assets/*, etc.)
	// caem aqui e servem o SPA. As rotas de API/WS/metrics já foram casadas antes, então
	// nunca chegam no NotFound. Vazio = mantém o 404 padrão (só API).
	if cfg.SPADir != "" {
		r.NotFound(serveSPA(cfg.SPADir))
	}

	return r
}

// serveDocs serve o site de docs gerado (cmd/docsite): arquivo direto quando
// existe, index.html na raiz, 404 no resto — SEM fallback de SPA (docs não têm
// roteamento client-side). path.Clean mantém o alvo dentro de dir.
func serveDocs(dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/docs/")
		if rel == "" {
			rel = "index.html"
		}
		full := filepath.Join(dir, filepath.FromSlash(path.Clean("/"+rel)))
		if st, err := os.Stat(full); err != nil || st.IsDir() {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, full)
	}
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

func (s *server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && s.allowedOrigin(r) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Expose-Headers", "X-CSRF-Token, ETag, X-Draft-Version")
			w.Header().Add("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-CSRF-Token, If-Match")
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
//     compat com ferramentas administrativas existentes).
func (s *server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := auth.ExtractToken(r)
		if tok == "" {
			if err := s.cfg.DB.WithAuditIdentity(db.AuditIdentity{Actor: "anonymous", Route: r.Method + " " + r.URL.Path, IP: clientIP(r)}).AuditObservation("access.denied", "401"); err != nil {
				http.Error(w, "Mandatory audit unavailable", http.StatusServiceUnavailable)
				return
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// Legacy token (env REGENTE_TOKEN / dev-token) → admin equivalente
		if !s.cfg.RuntimePolicy.Production() && s.cfg.Token != "" && tok == s.cfg.Token && s.mode() != "oidc" && s.mode() != "invalid" && r.Header.Get("Authorization") != "" {
			ctx := auth.WithUser(r.Context(), &auth.User{
				ID:       0,
				Username: "system",
				Role:     auth.RoleAdmin,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		u, err := auth.Resolve(s.cfg.DB, tok)
		if err != nil || !s.sessionAllowed(u) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !s.browserCSRF(w, r, u) {
			if err := s.cfg.DB.WithAuditIdentity(db.AuditIdentity{Actor: u.Username, Route: r.Method + " " + r.URL.Path, IP: clientIP(r)}).AuditObservation("access.denied", "csrf"); err != nil {
				http.Error(w, "Mandatory audit unavailable", http.StatusServiceUnavailable)
				return
			}
			http.Error(w, "Invalid session transport or CSRF token", http.StatusForbidden)
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

// silenced legacy helper kept for grep history; not used.

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// getVersion — GET /api/version. Versão do binário em execução (não a do disco:
// trocar o arquivo não troca o processo, e é justamente essa confusão que este
// endpoint existe pra desfazer). "dev" = build do código-fonte, sem release.
func (s *server) getVersion(w http.ResponseWriter, r *http.Request) {
	v := s.cfg.Version
	if v == "" {
		v = "dev"
	}
	writeJSON(w, 200, map[string]string{"version": v})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// rowsOK — fecha o contrato dos handlers de listagem: erro de ITERAÇÃO
// (rows.Err após o for rows.Next) vira 500 em vez de um 200 com resultado
// PARCIAL silencioso — relevante com o backend Postgres via rede, onde a
// conexão pode cair no meio do cursor. Chamar depois do loop, antes do
// writeJSON do payload. true = iteração completa, pode responder.
func rowsOK(w http.ResponseWriter, rows *sql.Rows) bool {
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "row iteration: " + err.Error()})
		return false
	}
	return true
}
