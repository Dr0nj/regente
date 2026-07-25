// Package scheduler — Phase 8 Alerting engine (server-side).
//
// Avalia regras configuráveis após cada execução terminal (OK/NOTOK, retries
// esgotados) e materializa eventos em `alert_events`. Os eventos são servidos
// via /api/alerts e empurrados em tempo real pelo hub ("alert.fired"), de modo
// que o sino/badge da UI e o toast funcionem em server mode.
//
// Espelha a semântica do engine do frontend (app/src/lib/alerting.ts): mesmas
// regras default, mesmas condições e mensagens, para que os dois modos
// (browser-only e server) se comportem igual.
package scheduler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/quickaction"
)

// AlertEngine avalia regras e persiste eventos disparados.
type AlertEngine struct {
	db  *db.DB
	hub *hub.Hub

	mu        sync.Mutex
	cooldowns map[string]time.Time // chave ruleID×workflowID -> último disparo (ephemeral)
}

func NewAlertEngine(database *db.DB, h *hub.Hub) *AlertEngine {
	return &AlertEngine{db: database, hub: h, cooldowns: map[string]time.Time{}}
}

// alertCondition espelha o discriminated-union AlertCondition do frontend.
type alertCondition struct {
	Type        string  `json:"type"`
	ThresholdMs int64   `json:"thresholdMs,omitempty"`
	MaxRetries  int     `json:"maxRetries,omitempty"`
	Rate        float64 `json:"rate,omitempty"`
	WindowSize  int     `json:"windowSize,omitempty"`
	Count       int     `json:"count,omitempty"`
	// PercentOver — folga sobre a média histórica (slow_vs_average): o job é
	// "lento" quando duração > média×(1+PercentOver/100). Default 50.
	PercentOver float64 `json:"percentOver,omitempty"`
}

// AlertRule — regra carregada do DB.
type AlertRule struct {
	ID              string
	Name            string
	Enabled         bool
	WorkflowPattern string
	ConditionJSON   string
	Severity        string
	Channels        string // CSV
	CooldownMs      int64
}

// AlertContext — entrada da avaliação (construída a partir da instance finalizada).
type AlertContext struct {
	WorkflowID          string
	WorkflowName        string
	InstanceID          string // D-15: alvo dos quick-action links ("" = alerta sem instance, ex.: FireSystem)
	Status              string // domain.StatusOK | domain.StatusNotOK
	DurationMs          int64
	MaxJobRetries       int
	RecentSuccessRate   float64
	ConsecutiveFailures int
	// Slow Execution (2026-07-17) — a régua é a MÉDIA histórica do próprio job:
	// AvgDurationMs/HistoryRuns vêm das execuções OK ANTERIORES (a run corrente
	// fica de fora). HistoryRuns=0 = primeira execução ⇒ a regra não se aplica.
	AvgDurationMs int64
	HistoryRuns   int
	// Running — o contexto é de uma instance AINDA em execução (varredura do
	// tick; DurationMs = decorrido). Muda só o verbo da mensagem.
	Running bool
	// SlowFired — esta run JÁ alertou lentidão durante a execução; o caminho
	// terminal não repete o disparo.
	SlowFired bool
}

// defaultRules — idênticas às do frontend (mesmos ids/cooldowns).
var defaultRules = []AlertRule{
	{ID: "rule-failure", Name: "Workflow Failure", Enabled: true, WorkflowPattern: "*",
		ConditionJSON: `{"type":"failure"}`, Severity: "critical", Channels: "toast", CooldownMs: 0},
	{ID: "rule-slow", Name: "Slow Execution", Enabled: true, WorkflowPattern: "*",
		ConditionJSON: slowDefaultJSON, Severity: "warning", Channels: "toast", CooldownMs: 300000},
	{ID: "rule-retries", Name: "Excessive Retries", Enabled: true, WorkflowPattern: "*",
		ConditionJSON: `{"type":"retry_exceeded","maxRetries":3}`, Severity: "warning", Channels: "toast", CooldownMs: 120000},
}

// slowDefaultJSON — condição default da rule-slow: lento = passou da própria
// média histórica em 50% (jobs sem histórico nunca alertam).
const slowDefaultJSON = `{"type":"slow_vs_average","percentOver":50}`

// oldSlowDefaultJSON — o default ANTIGO (teto fixo de 30s), reconhecido pela
// migração do SeedDefaults: só quem nunca customizou a regra é migrado.
const oldSlowDefaultJSON = `{"type":"duration_exceeded","thresholdMs":30000}`

// SeedDefaults insere as regras default uma vez (idempotente: pula se a tabela
// já tiver regras). Chamado no boot. Também migra a rule-slow default antiga
// (30s fixos) para a régua por média histórica — condition_json customizado
// pelo usuário nunca é tocado.
func (e *AlertEngine) SeedDefaults() {
	if _, err := e.db.Exec(
		`UPDATE alert_rules SET condition_json=? WHERE id='rule-slow' AND condition_json=?`,
		slowDefaultJSON, oldSlowDefaultJSON,
	); err != nil {
		log.Printf("[alerts] migrate rule-slow: %v", err)
	}
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM alert_rules`).Scan(&n); err != nil {
		log.Printf("[alerts] seed check: %v", err)
		return
	}
	if n > 0 {
		return
	}
	for _, r := range defaultRules {
		_, err := e.db.Exec(
			`INSERT INTO alert_rules(id, name, enabled, workflow_pattern, condition_json, severity, channels, cooldown_ms)
			 VALUES(?,?,?,?,?,?,?,?)`,
			r.ID, r.Name, boolToInt(r.Enabled), r.WorkflowPattern, r.ConditionJSON, r.Severity, r.Channels, r.CooldownMs,
		)
		if err != nil {
			log.Printf("[alerts] seed rule %s: %v", r.ID, err)
		}
	}
	log.Printf("[alerts] seeded %d default rules", len(defaultRules))
}

// Evaluate roda todas as regras habilitadas contra o contexto e dispara as que
// satisfazem (respeitando cooldown). Best-effort: nunca propaga erro.
func (e *AlertEngine) Evaluate(ctx AlertContext) {
	rules, err := e.ListRules()
	if err != nil {
		return
	}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if !matchesWorkflow(r.WorkflowPattern, ctx.WorkflowID) {
			continue
		}
		if e.onCooldown(cooldownKey(r.ID, ctx.WorkflowID), r.CooldownMs) {
			continue
		}
		var cond alertCondition
		if json.Unmarshal([]byte(r.ConditionJSON), &cond) != nil {
			continue
		}
		if !evalCondition(cond, ctx) {
			continue
		}
		e.fire(r, cond, ctx)
	}
}

// SlowRules — regras HABILITADAS do tipo slow_vs_average (uma query; o tick
// chama 1× por ciclo antes de varrer as RUNNING).
func (e *AlertEngine) SlowRules() []AlertRule {
	rules, err := e.ListRules()
	if err != nil {
		return nil
	}
	var out []AlertRule
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		var cond alertCondition
		if json.Unmarshal([]byte(r.ConditionJSON), &cond) != nil || cond.Type != "slow_vs_average" {
			continue
		}
		out = append(out, r)
	}
	return out
}

// FireSlowRules — avalia as regras de lentidão (já filtradas por SlowRules)
// contra uma instance RUNNING (ctx.Running=true; DurationMs = decorrido). É o
// caminho do tick: o alerta tem que sair ENQUANTO o job estoura a média, não
// só quando terminar. Retorna true se disparou (o scheduler marca a run para
// não re-alertar).
func (e *AlertEngine) FireSlowRules(rules []AlertRule, ctx AlertContext) bool {
	fired := false
	for _, r := range rules {
		if !matchesWorkflow(r.WorkflowPattern, ctx.WorkflowID) {
			continue
		}
		if e.onCooldown(cooldownKey(r.ID, ctx.WorkflowID), r.CooldownMs) {
			continue
		}
		var cond alertCondition
		if json.Unmarshal([]byte(r.ConditionJSON), &cond) != nil {
			continue
		}
		if !evalCondition(cond, ctx) {
			continue
		}
		e.fire(r, cond, ctx)
		fired = true
	}
	return fired
}

func (e *AlertEngine) fire(r AlertRule, cond alertCondition, ctx AlertContext) {
	msg := buildMessage(cond, ctx)
	tsMs := time.Now().UnixMilli()
	id, err := e.db.InsertID(
		`INSERT INTO alert_events(rule_id, rule_name, severity, workflow_id, workflow_name, message, acknowledged, ts_ms)
		 VALUES(?,?,?,?,?,?,0,?)`,
		r.ID, r.Name, r.Severity, ctx.WorkflowID, ctx.WorkflowName, msg, tsMs,
	)
	if err != nil {
		log.Printf("[alerts] insert event: %v", err)
		return
	}
	e.setCooldown(cooldownKey(r.ID, ctx.WorkflowID))
	if e.hub != nil {
		e.hub.BroadcastWeb("alert.fired", map[string]any{
			"id":           fmt.Sprintf("%d", id),
			"ruleId":       r.ID,
			"ruleName":     r.Name,
			"severity":     r.Severity,
			"timestamp":    tsMs,
			"workflowId":   ctx.WorkflowID,
			"workflowName": ctx.WorkflowName,
			"message":      msg,
			"acknowledged": false,
		})
	}
	// Routing externo (best-effort, async) — Slack / webhook genérico. Só spawna
	// se há sink configurado (ver hasExternalSink): sem canal, seria no-op.
	if e.hasExternalSink() {
		go e.route(r, ctx, msg, fmt.Sprintf("%d", id), tsMs)
	}
}

// FireSystem emite um alerta do CONTROL PLANE (R7) — auto-monitoramento do próprio
// servidor (tick parado, DB inacessível, leader flapping, frota de agentes). Reusa a
// MESMA persistência (alert_events) + broadcast + routing externo das regras, então cai
// nos canais já configurados (Slack/webhook/e-mail/PagerDuty). signalKey identifica o
// sinal (ex.: "tick-stalled") e tem cooldown próprio p/ não virar storm. Best-effort.
func (e *AlertEngine) FireSystem(signalKey, name, severity, message string, cooldownMs int64) {
	if e.onCooldown("sys:"+signalKey, cooldownMs) {
		return
	}
	tsMs := time.Now().UnixMilli()
	id, err := e.db.InsertID(
		`INSERT INTO alert_events(rule_id, rule_name, severity, workflow_id, workflow_name, message, acknowledged, ts_ms)
		 VALUES(?,?,?,?,?,?,0,?)`,
		"rule-control-plane", name, severity, "control-plane", "Control Plane", message, tsMs,
	)
	if err != nil {
		log.Printf("[alerts] insert system event: %v", err)
		return
	}
	e.setCooldown("sys:" + signalKey)
	if e.hub != nil {
		e.hub.BroadcastWeb("alert.fired", map[string]any{
			"id": fmt.Sprintf("%d", id), "ruleId": "rule-control-plane", "ruleName": name,
			"severity": severity, "timestamp": tsMs, "workflowId": "control-plane",
			"workflowName": "Control Plane", "message": message, "acknowledged": false,
		})
	}
	// Regra sintética SEM canais → channelWanted roteia p/ todos os sinks configurados.
	r := AlertRule{ID: "rule-control-plane", Name: name, Severity: severity, Channels: ""}
	ctx := AlertContext{WorkflowID: "control-plane", WorkflowName: "Control Plane"}
	if e.hasExternalSink() {
		go e.route(r, ctx, message, fmt.Sprintf("%d", id), tsMs)
	}
}

// FireAction emite um alerta vindo de uma regra Actions/On-Do de um job (ação
// "notify"). Reusa a MESMA persistência (alert_events) + broadcast + routing
// externo das regras de alerta, então o sino/toast da UI e os canais
// (Slack/webhook/e-mail/PagerDuty) funcionam igual. channels vazio → roteia para
// TODOS os sinks configurados (igual a uma regra sem canal externo). Sem cooldown:
// a regra On/Do já dispara no máximo uma vez por instance (ledger action_fires).
// Best-effort.
func (e *AlertEngine) FireAction(workflowID, workflowName, name, severity, message string, channels []string) {
	if severity == "" {
		severity = "warning"
	}
	tsMs := time.Now().UnixMilli()
	id, err := e.db.InsertID(
		`INSERT INTO alert_events(rule_id, rule_name, severity, workflow_id, workflow_name, message, acknowledged, ts_ms)
		 VALUES(?,?,?,?,?,?,0,?)`,
		"rule-action", name, severity, workflowID, workflowName, message, tsMs,
	)
	if err != nil {
		log.Printf("[alerts] insert action event: %v", err)
		return
	}
	if e.hub != nil {
		e.hub.BroadcastWeb("alert.fired", map[string]any{
			"id": fmt.Sprintf("%d", id), "ruleId": "rule-action", "ruleName": name,
			"severity": severity, "timestamp": tsMs, "workflowId": workflowID,
			"workflowName": workflowName, "message": message, "acknowledged": false,
		})
	}
	r := AlertRule{ID: "rule-action", Name: name, Severity: severity, Channels: strings.Join(channels, ",")}
	ctx := AlertContext{WorkflowID: workflowID, WorkflowName: workflowName}
	if e.hasExternalSink() {
		go e.route(r, ctx, message, fmt.Sprintf("%d", id), tsMs)
	}
}

/* ── Alert routing — sinks externos (Slack / webhook) ── */

// hasExternalSink diz se ALGUM sink externo (Slack/webhook/SMTP/PagerDuty) está
// configurado. Chamado ANTES de spawnar a goroutine de routing: sem sink, o
// routing é no-op, então nem criamos a goroutine — poupa trabalho no caso comum
// (nenhum canal externo, o default) e, crucialmente, elimina a goroutine que lia
// o DB DEPOIS do teste terminar, correndo com o RemoveAll do t.TempDir() (flake
// "directory not empty" da CI em TestAlertEngine_*). A leitura aqui é síncrona,
// no caminho de quem dispara o alerta.
func (e *AlertEngine) hasExternalSink() bool {
	return e.setting("alert_slack_webhook") != "" ||
		e.setting("alert_webhook_url") != "" ||
		e.setting("alert_smtp_host") != "" ||
		e.setting("alert_pagerduty_routing_key") != ""
}

// route entrega o alerta nos sinks externos configurados em settings, conforme
// os canais escolhidos POR REGRA:
//
//	slack      → alert_slack_webhook        (Slack incoming webhook)
//	webhook    → alert_webhook_url          (webhook genérico; Teams/Discord/custom)
//	email      → alert_smtp_*               (SMTP)
//	pagerduty  → alert_pagerduty_routing_key (PagerDuty Events API v2)
//
// Routing por-regra: cada regra define em `channels` para onde roteia. Por
// compatibilidade, regra SEM nenhum canal externo selecionado (ex.: só "toast")
// roteia para TODOS os sinks configurados — preserva o comportamento "global".
// É best-effort e nunca afeta o fluxo de disparo.
func (e *AlertEngine) route(r AlertRule, ctx AlertContext, msg, id string, tsMs int64) {
	// D-15 — quick actions: links assinados de um toque (rerun/set-ok/…) anexados
	// ao alerta. Exigem settings.public_url (a URL que o celular alcança); sem
	// ela, alerta sai como sempre. Melhor-esforço: falha de assinatura ≠ sem alerta.
	qa := e.quickActionLinks(ctx)
	slackMsg := msg
	if len(qa) > 0 {
		var parts []string
		for _, l := range qa {
			parts = append(parts, fmt.Sprintf("<%s|%s>", l.URL, l.Label))
		}
		slackMsg = msg + "\nQuick actions: " + strings.Join(parts, " · ")
	}
	if channelWanted(r.Channels, "slack") {
		if slack := e.setting("alert_slack_webhook"); slack != "" {
			postJSON(slack, map[string]any{"text": slackText(r.Severity, r.Name, slackMsg, ctx.WorkflowName)})
		}
	}
	if channelWanted(r.Channels, "webhook") {
		if hook := e.setting("alert_webhook_url"); hook != "" {
			payload := map[string]any{
				"event":        "alert.fired",
				"id":           id,
				"ruleId":       r.ID,
				"ruleName":     r.Name,
				"severity":     r.Severity,
				"timestamp":    tsMs,
				"workflowId":   ctx.WorkflowID,
				"workflowName": ctx.WorkflowName,
				"message":      msg,
			}
			if len(qa) > 0 {
				payload["quickActions"] = qa
			}
			postJSON(hook, payload)
		}
	}
	if channelWanted(r.Channels, "email") {
		e.sendEmail(r, ctx, msg)
	}
	if channelWanted(r.Channels, "pagerduty") {
		e.sendPagerDuty(r, ctx, msg)
	}
}

// QuickActionLink — um link de ação de um toque num alerta.
type QuickActionLink struct {
	Action string `json:"action"`
	Label  string `json:"label"`
	URL    string `json:"url"`
}

const quickActionTTL = 24 * time.Hour

// quickActionLinks — monta os links assinados para a instance do alerta.
// Ações oferecidas dependem do estado do alerta: falha → rerun + set-ok.
func (e *AlertEngine) quickActionLinks(ctx AlertContext) []QuickActionLink {
	if ctx.InstanceID == "" || ctx.Status != string(domain.StatusNotOK) {
		return nil
	}
	base := strings.TrimRight(e.setting("public_url"), "/")
	if base == "" {
		return nil
	}
	secret := e.quickActionSecret()
	if secret == "" {
		return nil
	}
	exp := time.Now().Add(quickActionTTL)
	var out []QuickActionLink
	for _, a := range []struct{ action, label string }{
		{"rerun", "▶ Rerun"}, {"set-ok", "✔ Set OK"},
	} {
		tok, err := quickaction.Sign(secret, ctx.InstanceID, a.action, exp)
		if err != nil {
			continue
		}
		out = append(out, QuickActionLink{Action: a.action, Label: a.label, URL: base + "/qa/" + tok})
	}
	return out
}

// quickActionSecret — lê settings.quickaction_secret; gera e persiste na 1ª vez.
func (e *AlertEngine) quickActionSecret() string {
	if s := e.setting("quickaction_secret"); s != "" {
		return s
	}
	s, err := quickaction.NewSecret()
	if err != nil {
		return ""
	}
	if _, err := e.db.Exec(
		`INSERT INTO settings(key, value) VALUES('quickaction_secret', ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`, s); err != nil {
		return ""
	}
	return s
}

// parseChannelSet — CSV "toast,slack" → set normalizado (lowercase).
func parseChannelSet(csv string) map[string]bool {
	set := map[string]bool{}
	for _, c := range strings.Split(csv, ",") {
		if t := strings.TrimSpace(strings.ToLower(c)); t != "" {
			set[t] = true
		}
	}
	return set
}

// channelWanted decide se um sink externo deve disparar dada a lista de canais
// da regra. Regra sem canal externo escolhido → roteia para todos (back-compat).
func channelWanted(channelsCSV, ch string) bool {
	set := parseChannelSet(channelsCSV)
	hasExternal := set["slack"] || set["webhook"] || set["email"] || set["pagerduty"]
	return !hasExternal || set[ch]
}

// sendEmail entrega o alerta por SMTP. Settings:
//
//	alert_smtp_host · alert_smtp_port (default 587) · alert_smtp_username ·
//	alert_smtp_password · alert_smtp_from · alert_smtp_to (CSV de destinatários)
//
// Sem host ou sem destinatário → no-op. Auth PLAIN só se houver username.
func (e *AlertEngine) sendEmail(r AlertRule, ctx AlertContext, msg string) {
	host := e.setting("alert_smtp_host")
	to := e.setting("alert_smtp_to")
	if host == "" || to == "" {
		return
	}
	recipients := []string{}
	for _, a := range strings.Split(to, ",") {
		if t := strings.TrimSpace(a); t != "" {
			recipients = append(recipients, t)
		}
	}
	if len(recipients) == 0 {
		return
	}
	from := e.setting("alert_smtp_from")
	if from == "" {
		from = "regente@localhost"
	}
	port := e.setting("alert_smtp_port")
	if port == "" {
		port = "587"
	}
	user := e.setting("alert_smtp_username")
	pass := e.setting("alert_smtp_password")

	subject := fmt.Sprintf("[Regente][%s] %s", strings.ToUpper(r.Severity), r.Name)
	body := fmt.Sprintf("To: %s\r\nFrom: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n\r\nworkflow: %s\r\n",
		strings.Join(recipients, ", "), from, subject, msg, ctx.WorkflowName)

	var auth smtp.Auth
	if user != "" {
		auth = smtp.PlainAuth("", user, pass, host)
	}
	if err := smtp.SendMail(host+":"+port, auth, from, recipients, []byte(body)); err != nil {
		log.Printf("[alerts] route email failed: %v", err)
	}
}

// pagerDutyURL — endpoint da Events API v2 (var p/ permitir override em teste).
var pagerDutyURL = "https://events.pagerduty.com/v2/enqueue"

// sendPagerDuty dispara um evento na PagerDuty Events API v2. Setting:
//
//	alert_pagerduty_routing_key — integration/routing key do serviço.
//
// Sem key → no-op. dedup_key estável por regra+workflow agrupa re-disparos.
func (e *AlertEngine) sendPagerDuty(r AlertRule, ctx AlertContext, msg string) {
	key := e.setting("alert_pagerduty_routing_key")
	if key == "" {
		return
	}
	source := ctx.WorkflowName
	if source == "" {
		source = ctx.WorkflowID
	}
	postJSON(pagerDutyURL, map[string]any{
		"routing_key":  key,
		"event_action": "trigger",
		"dedup_key":    fmt.Sprintf("regente-%s-%s", r.ID, ctx.WorkflowID),
		"payload": map[string]any{
			"summary":   fmt.Sprintf("%s — %s", r.Name, msg),
			"severity":  pagerDutySeverity(r.Severity),
			"source":    source,
			"component": "regente",
			"group":     ctx.WorkflowID,
		},
	})
}

// pagerDutySeverity mapeia a severidade Regente para a aceita pela PagerDuty
// (critical|error|warning|info).
func pagerDutySeverity(sev string) string {
	switch sev {
	case "critical":
		return "critical"
	case "warning":
		return "warning"
	default:
		return "info"
	}
}

func (e *AlertEngine) setting(key string) string {
	var v string
	_ = e.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	return v
}

func slackText(severity, ruleName, msg, workflow string) string {
	icon := ":information_source:"
	switch severity {
	case "critical":
		icon = ":rotating_light:"
	case "warning":
		icon = ":warning:"
	}
	return fmt.Sprintf("%s *%s* — %s\n%s\nworkflow: `%s`", icon, strings.ToUpper(severity), ruleName, msg, workflow)
}

func postJSON(url string, payload map[string]any) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[alerts] route webhook failed: %v", err)
		return
	}
	resp.Body.Close()
}

/* ── Cooldown (ephemeral, in-memory) ──
   A chave é ruleID×workflowID — NÃO só ruleID. O cooldown agrupa apenas
   re-disparos do MESMO job na mesma regra (anti-spam de job flapando). Jobs
   DIFERENTES nunca se suprimem: numa rajada (ex.: agente cai e N jobs falham
   juntos), cada job gera o seu alerta. Garante que nenhum erro distinto seja
   perdido na tela de alertas — o operador vê todos os jobs a tratar. */

// cooldownKey isola o cooldown por (regra, workflow). \x00 evita colisão de ids.
func cooldownKey(ruleID, workflowID string) string {
	return ruleID + "\x00" + workflowID
}

func (e *AlertEngine) onCooldown(key string, cooldownMs int64) bool {
	if cooldownMs <= 0 {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	last, ok := e.cooldowns[key]
	if !ok {
		return false
	}
	return time.Since(last) < time.Duration(cooldownMs)*time.Millisecond
}

func (e *AlertEngine) setCooldown(key string) {
	e.mu.Lock()
	e.cooldowns[key] = time.Now()
	e.mu.Unlock()
}

/* ── Condition evaluation (mirror do frontend) ── */

func matchesWorkflow(pattern, workflowID string) bool {
	return pattern == "*" || pattern == workflowID
}

func evalCondition(c alertCondition, ctx AlertContext) bool {
	switch c.Type {
	case "failure":
		return ctx.Status == string(domain.StatusNotOK)
	case "duration_exceeded":
		return ctx.DurationMs > c.ThresholdMs
	case "slow_vs_average":
		// Lento = passou da própria média histórica + folga. Primeira execução
		// (sem histórico) NUNCA alerta; SlowFired deduplica o disparo terminal
		// de quem já alertou durante a run.
		if ctx.SlowFired || ctx.HistoryRuns <= 0 || ctx.AvgDurationMs <= 0 {
			return false
		}
		pct := c.PercentOver
		if pct <= 0 {
			pct = 50
		}
		return float64(ctx.DurationMs) > float64(ctx.AvgDurationMs)*(1+pct/100)
	case "retry_exceeded":
		return ctx.MaxJobRetries > c.MaxRetries
	case "success_rate_below":
		return ctx.RecentSuccessRate < c.Rate
	case "consecutive_failures":
		return ctx.ConsecutiveFailures >= c.Count
	}
	return false
}

func buildMessage(c alertCondition, ctx AlertContext) string {
	wf := ctx.WorkflowName
	if wf == "" {
		wf = ctx.WorkflowID
	}
	switch c.Type {
	case "failure":
		return fmt.Sprintf("Workflow %q failed after %.1fs", wf, float64(ctx.DurationMs)/1000)
	case "duration_exceeded":
		return fmt.Sprintf("Workflow %q exceeded %ds threshold (took %.1fs)", wf, c.ThresholdMs/1000, float64(ctx.DurationMs)/1000)
	case "slow_vs_average":
		over := 0.0
		if ctx.AvgDurationMs > 0 {
			over = (float64(ctx.DurationMs)/float64(ctx.AvgDurationMs) - 1) * 100
		}
		verb := "took"
		if ctx.Running {
			verb = "is running for"
		}
		return fmt.Sprintf("Workflow %q %s %.1fs — %.0f%% over its %.1fs average (%d runs)",
			wf, verb, float64(ctx.DurationMs)/1000, over, float64(ctx.AvgDurationMs)/1000, ctx.HistoryRuns)
	case "retry_exceeded":
		return fmt.Sprintf("Workflow %q had %d retries (limit: %d)", wf, ctx.MaxJobRetries, c.MaxRetries)
	case "success_rate_below":
		return fmt.Sprintf("Workflow %q success rate dropped to %.0f%%", wf, ctx.RecentSuccessRate*100)
	case "consecutive_failures":
		return fmt.Sprintf("Workflow %q has %d consecutive failures", wf, ctx.ConsecutiveFailures)
	}
	return fmt.Sprintf("Alert triggered for %q", wf)
}

/* ── Persistence queries (servidas pela API) ── */

// AlertEventRow — linha de evento serializável para a API.
type AlertEventRow struct {
	ID           string `json:"id"`
	RuleID       string `json:"ruleId"`
	RuleName     string `json:"ruleName"`
	Severity     string `json:"severity"`
	Timestamp    int64  `json:"timestamp"`
	WorkflowID   string `json:"workflowId"`
	WorkflowName string `json:"workflowName"`
	Message      string `json:"message"`
	Acknowledged bool   `json:"acknowledged"`
	// Resolution — ciclo de vida: '' novo · 'ack' reconhecido · 'rerun' · 'set_ok'.
	Resolution string `json:"resolution"`
}

func (e *AlertEngine) ListEvents(limit int) ([]AlertEventRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := e.db.Query(
		`SELECT id, rule_id, rule_name, severity, workflow_id, workflow_name, message,
		        COALESCE(acknowledged,0), COALESCE(ts_ms,0), COALESCE(resolution,'')
		 FROM alert_events ORDER BY ts_ms DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertEventRow{}
	for rows.Next() {
		var ev AlertEventRow
		var idNum int64
		var ackInt int
		if err := rows.Scan(&idNum, &ev.RuleID, &ev.RuleName, &ev.Severity,
			&ev.WorkflowID, &ev.WorkflowName, &ev.Message, &ackInt, &ev.Timestamp, &ev.Resolution); err != nil {
			continue
		}
		ev.ID = fmt.Sprintf("%d", idNum)
		ev.Acknowledged = ackInt == 1
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (e *AlertEngine) UnacknowledgedCount() (int, error) {
	var n int
	err := e.db.QueryRow(`SELECT COUNT(*) FROM alert_events WHERE COALESCE(acknowledged,0)=0`).Scan(&n)
	return n, err
}

func (e *AlertEngine) Acknowledge(id string) error {
	// reconhecimento manual = resolução 'ack' (não sobrescreve rerun/set_ok já tratados).
	_, err := e.db.Exec(
		`UPDATE alert_events SET acknowledged=1,
		        resolution=CASE WHEN COALESCE(resolution,'')='' THEN 'ack' ELSE resolution END
		 WHERE id=?`, id)
	return err
}

func (e *AlertEngine) AcknowledgeAll() error {
	_, err := e.db.Exec(
		`UPDATE alert_events SET acknowledged=1,
		        resolution=CASE WHEN COALESCE(resolution,'')='' THEN 'ack' ELSE resolution END
		 WHERE COALESCE(acknowledged,0)=0`)
	return err
}

// MarkHandledByWorkflow marca como tratados os alertas AINDA NÃO tratados
// (resolution=”) de um workflow, com a resolução dada ('rerun'|'set_ok').
// Alertas já tratados não são sobrescritos — preserva o histórico de ações:
// um alerta tratado com rerun continua 'rerun' mesmo depois de um set_ok posterior
// (que marca apenas o alerta NOVO gerado pela re-execução). Retorna nº afetado.
func (e *AlertEngine) MarkHandledByWorkflow(workflowID, resolution string) (int, error) {
	res, err := e.db.Exec(
		`UPDATE alert_events SET acknowledged=1, resolution=?
		 WHERE workflow_id=? AND COALESCE(resolution,'')=''`,
		resolution, workflowID,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// AlertRuleRow — linha de regra serializável (condition emitido como objeto).
type AlertRuleRow struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Enabled         bool            `json:"enabled"`
	WorkflowPattern string          `json:"workflowPattern"`
	Condition       json.RawMessage `json:"condition"`
	Severity        string          `json:"severity"`
	Channels        []string        `json:"channels"`
	CooldownMs      int64           `json:"cooldownMs"`
}

func (e *AlertEngine) ListRules() ([]AlertRule, error) {
	rows, err := e.db.Query(
		`SELECT id, name, COALESCE(enabled,1), workflow_pattern, condition_json, severity, channels, COALESCE(cooldown_ms,60000)
		 FROM alert_rules ORDER BY id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AlertRule{}
	for rows.Next() {
		var r AlertRule
		var enInt int
		if err := rows.Scan(&r.ID, &r.Name, &enInt, &r.WorkflowPattern, &r.ConditionJSON, &r.Severity, &r.Channels, &r.CooldownMs); err != nil {
			continue
		}
		r.Enabled = enInt == 1
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (e *AlertEngine) ToggleRule(id string) error {
	// flip enabled — portável entre SQLite/PG (sem boolean NOT).
	var enInt int
	if err := e.db.QueryRow(`SELECT COALESCE(enabled,1) FROM alert_rules WHERE id=?`, id).Scan(&enInt); err != nil {
		return err
	}
	next := 0
	if enInt == 0 {
		next = 1
	}
	_, err := e.db.Exec(`UPDATE alert_rules SET enabled=? WHERE id=?`, next, id)
	return err
}

// validChannels — canais aceitos para routing por regra.
var validChannels = map[string]bool{
	"toast": true, "slack": true, "webhook": true, "email": true, "pagerduty": true,
}

// UpdateRuleChannels persiste os canais de routing de uma regra (CSV ordenado).
// Filtra tokens desconhecidos/duplicados e garante "toast" sempre presente (o
// alerta sempre aparece na UI; os demais canais controlam o routing externo).
func (e *AlertEngine) UpdateRuleChannels(id string, channels []string) error {
	seen := map[string]bool{}
	clean := []string{"toast"}
	seen["toast"] = true
	for _, c := range channels {
		t := strings.ToLower(strings.TrimSpace(c))
		if t == "" || !validChannels[t] || seen[t] {
			continue
		}
		seen[t] = true
		clean = append(clean, t)
	}
	_, err := e.db.Exec(`UPDATE alert_rules SET channels=? WHERE id=?`, strings.Join(clean, ","), id)
	return err
}

// UpdateRuleCooldown define o cooldown (ms) de uma regra. 0 = sem cooldown
// (toda ocorrência vira alerta — o tratamento é feito pelo ciclo de vida do alerta).
func (e *AlertEngine) UpdateRuleCooldown(id string, cooldownMs int64) error {
	if cooldownMs < 0 {
		cooldownMs = 0
	}
	_, err := e.db.Exec(`UPDATE alert_rules SET cooldown_ms=? WHERE id=?`, cooldownMs, id)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
