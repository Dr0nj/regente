// Package api — NL-query (substrato de consulta estruturada / linguagem natural).
//
// POST /api/query {"q":"o que falhou hoje na folder PIX"} → resolve a intenção
// para uma CONSULTA ESTRUTURADA determinística (sem LLM, sem alucinação), executa
// contra o estado do dia (respeitando RBAC) e devolve a resposta JUNTO com a
// interpretação (`interpreted`) — o operador vê exatamente o que foi consultado.
//
// É o "transporte QUERY": a camada agent-native (MCP/LLM) manda texto e recebe
// uma resposta determinística; o parser é conservador (o que não entende vira
// intent "unknown" com sugestões, nunca um palpite). Read-only, reusa os mesmos
// filtros/RBAC de /api/instances.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// QueryIntent — a interpretação estruturada de um texto.
type QueryIntent struct {
	Kind   string `json:"kind"`             // summary | list | count | explain | unknown
	Status string `json:"status,omitempty"` // filtro de status (list/count)
	Folder string `json:"folder,omitempty"` // folder/team extraído
	JobRef string `json:"jobRef,omitempty"` // referência de job (explain)
	Raw    string `json:"raw"`              // texto original
}

var reFolder = regexp.MustCompile(`(?i)\b(?:folder|pasta|time|team)\s+([A-Za-z0-9_.\-]+)`)

// parseQueryIntent — parser determinístico PT/EN. FUNÇÃO PURA (testável sem DB).
// Ordem importa: "por que <job>" é checado antes dos filtros de status genéricos.
func parseQueryIntent(text string) QueryIntent {
	raw := strings.TrimSpace(text)
	low := strings.ToLower(raw)
	intent := QueryIntent{Raw: raw, Kind: "unknown"}

	if m := reFolder.FindStringSubmatch(raw); m != nil {
		intent.Folder = m[1]
	}

	// "por que o job X (não rodou)?" → explain, se houver um token que PAREÇA um id
	// de job (contém _/-/. ou dígito) — evita capturar o verbo final ("rodou").
	if strings.Contains(low, "por que") || strings.Contains(low, "porque") ||
		strings.HasPrefix(low, "pq ") || strings.Contains(low, "why") {
		if ref := extractJobRef(raw, intent.Folder); ref != "" {
			intent.Kind = "explain"
			intent.JobRef = ref
			return intent
		}
	}

	// status alvo, se mencionado.
	status := ""
	switch {
	case containsAny(low, "falhou", "falharam", "falha", "falho", "notok", "not ok", "erro", "failed", "fail"):
		status = "NOTOK"
	case containsAny(low, "bloquead", "travad", "esperand", "aguardand", "waiting", "blocked", "parad"):
		status = "WAITING"
	case containsAny(low, "rodando", "executand", "em execução", "running", "rodar agora"):
		status = "RUNNING"
	case containsAny(low, "ok", "sucesso", "concluíd", "concluid", "success", "terminaram", "prontos"):
		status = "OK"
	}
	intent.Status = status

	switch {
	case containsAny(low, "quantos", "quantas", "how many", "número de", "numero de", "contagem"):
		intent.Kind = "count"
	case containsAny(low, "resumo", "panorama", "como está", "como esta", "status do dia", "summary", "overview", "situação", "situacao"):
		intent.Kind = "summary"
	case status != "" || containsAny(low, "jobs", "listar", "liste", "quais", "list", "mostrar", "mostre"):
		intent.Kind = "list"
	}

	return intent
}

var reWord = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9_.\-]*`)
var reHasDigit = regexp.MustCompile(`\d`)

// Padrões fortes de referência a job (determinísticos, PT/EN):
//   - "<job> (não) rodou/falhou/executou"  → o token ANTES do verbo é o job.
//   - "(o )?job <job>" / "the job <job>"    → o token DEPOIS de "job" é o job.
var reJobBeforeVerb = regexp.MustCompile(`(?i)\b([A-Za-z0-9][A-Za-z0-9_.\-]{2,})\s+(?:n[ãa]o\s+)?(?:rodou|rodar|falhou|falha|executou|quebrou|failed|fail|fails|ran|broke|break)\b`)
var reJobAfterKw = regexp.MustCompile(`(?i)\bjob\s+([A-Za-z0-9][A-Za-z0-9_.\-]{2,})`)

// extractJobRef — resolve a referência de job de uma pergunta "por que…".
// Estratégia em camadas (primeira que casar vence), sempre pulando stopwords e o
// nome da folder: (1) token antes de um verbo de falha; (2) token após "job";
// (3) token que PARECE id (contém _/-/. ou dígito). Sem nenhum, devolve "" — não
// vira explain no chute (o fallback "unknown" orienta o usuário).
func extractJobRef(text, folder string) string {
	ok := func(tok string) bool {
		return len(tok) >= 3 && !isStopWord(tok) && !strings.EqualFold(tok, folder)
	}
	if m := reJobBeforeVerb.FindStringSubmatch(text); m != nil && ok(m[1]) {
		return m[1]
	}
	if m := reJobAfterKw.FindStringSubmatch(text); m != nil && ok(m[1]) {
		return m[1]
	}
	for _, tok := range reWord.FindAllString(text, -1) {
		if ok(tok) && (strings.ContainsAny(tok, "_-.") || reHasDigit.MatchString(tok)) {
			return tok
		}
	}
	return ""
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// isStopWord evita capturar palavras comuns como se fossem nome de job.
func isStopWord(w string) bool {
	switch strings.ToLower(w) {
	case "hoje", "rodou", "rodar", "job", "run", "ran", "today", "não", "nao",
		"falhou", "está", "esta", "que", "por", "porque", "pq", "why", "para",
		"pra", "com", "dos", "das", "uma", "num", "the", "and", "executou",
		"quebrou", "failed", "broke", "qual", "quais", "quantos", "quantas":
		return true
	}
	return false
}

// runQuery — POST /api/query. Executa a intenção contra o dia atual.
func (s *server) runQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Q string `json:"q"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	intent := parseQueryIntent(req.Q)
	date := time.Now().Format("2006-01-02")
	allowed, restrict := s.allowedTeams(r, date)

	// query estruturada base (dia atual + folder da intenção).
	iq := instanceQuery{date: date, folder: intent.Folder}
	if intent.Status != "" {
		iq.statuses = []string{intent.Status}
	}

	resp := map[string]any{"interpreted": intent}

	switch intent.Kind {
	case "summary":
		where, args := iq.where(allowed, restrict)
		byStatus, total := s.countByStatus(where, args)
		resp["answer"] = map[string]any{
			"text":     summaryText(date, total, byStatus, intent.Folder),
			"total":    total,
			"byStatus": byStatus,
		}

	case "count":
		where, args := iq.where(allowed, restrict)
		byStatus, total := s.countByStatus(where, args)
		n := total
		if intent.Status != "" {
			n = byStatus[intent.Status]
		}
		resp["answer"] = map[string]any{"text": countText(n, intent), "count": n}

	case "list":
		where, args := iq.where(allowed, restrict)
		sqlStr := `SELECT ` + instanceCols + ` FROM instances WHERE ` + where + ` ORDER BY scheduled_at LIMIT 200`
		rows, err := s.cfg.DB.Query(sqlStr, args...)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		items, scanErr := scanInstances(rows)
		rows.Close()
		if scanErr != nil {
			http.Error(w, scanErr.Error(), http.StatusInternalServerError)
			return
		}
		resp["answer"] = map[string]any{"text": listText(items, intent), "items": items, "count": len(items)}

	case "explain":
		id := s.resolveJobRef(intent.JobRef, date, allowed, restrict)
		if id == "" {
			resp["answer"] = map[string]any{"text": "Não achei um job '" + intent.JobRef + "' ordenado hoje. Confira o nome/ID."}
			break
		}
		ex, err := s.cfg.Scheduler.Explain(id)
		if err != nil {
			resp["answer"] = map[string]any{"text": "Não consegui explicar '" + id + "'."}
			break
		}
		resp["answer"] = map[string]any{"text": ex.Summary, "explain": ex, "instanceId": id}

	default:
		resp["answer"] = map[string]any{
			"text": "Não entendi a pergunta. Tente: \"resumo do dia\", \"o que falhou hoje\", " +
				"\"jobs bloqueados na folder X\", \"quantos rodando\" ou \"por que o job Y não rodou\".",
		}
	}

	writeJSON(w, 200, resp)
}

// countByStatus — GROUP BY status sob o mesmo WHERE (reusa o filtro/RBAC).
func (s *server) countByStatus(where string, args []any) (map[string]int, int) {
	byStatus := map[string]int{}
	total := 0
	if rows, err := s.cfg.DB.Query(`SELECT status, COUNT(*) FROM instances WHERE `+where+` GROUP BY status`, args...); err == nil {
		for rows.Next() {
			var st string
			var n int
			if rows.Scan(&st, &n) == nil {
				byStatus[st] = n
				total += n
			}
		}
		rows.Close()
	}
	return byStatus, total
}

// resolveJobRef — acha o instanceId "mais determinante" de hoje cujo definition_id
// bate (exato ou por sufixo/prefixo) com a referência. RBAC por conjunto.
func (s *server) resolveJobRef(ref, date string, allowed []string, restrict bool) string {
	if ref == "" {
		return ""
	}
	iq := instanceQuery{date: date, search: ref}
	where, args := iq.where(allowed, restrict)
	rows, err := s.cfg.DB.Query(`SELECT id, status FROM instances WHERE `+where+` ORDER BY scheduled_at`, args...)
	if err != nil {
		return ""
	}
	defer rows.Close()
	bestID, bestRank := "", -1
	for rows.Next() {
		var id, st string
		if rows.Scan(&id, &st) != nil {
			continue
		}
		if rk := instanceStatusRank(st); rk > bestRank {
			bestRank, bestID = rk, id
		}
	}
	return bestID
}

// instanceStatusRank — "mais determinante" (espelha statusRank do scheduler; local
// p/ o pacote api não depender de internals do scheduler).
func instanceStatusRank(s string) int {
	switch s {
	case "NOTOK":
		return 6
	case "OK":
		return 5
	case "RUNNING":
		return 4
	case "CANCELLED":
		return 3
	case "HELD":
		return 2
	case "WAITING":
		return 1
	}
	return 0
}

func summaryText(date string, total int, byStatus map[string]int, folder string) string {
	scope := "hoje"
	if folder != "" {
		scope = "hoje na folder " + folder
	}
	if total == 0 {
		return "Nenhum job ordenado " + scope + "."
	}
	parts := []string{}
	for _, st := range []string{"RUNNING", "WAITING", "OK", "NOTOK", "HELD", "CANCELLED"} {
		if n := byStatus[st]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, st))
		}
	}
	return fmt.Sprintf("%d jobs %s: %s.", total, scope, strings.Join(parts, ", "))
}

func countText(n int, intent QueryIntent) string {
	what := "jobs"
	if intent.Status != "" {
		what = intent.Status
	}
	where := "hoje"
	if intent.Folder != "" {
		where = "na folder " + intent.Folder
	}
	return fmt.Sprintf("%d %s %s.", n, what, where)
}

func listText(items []instanceRow, intent QueryIntent) string {
	if len(items) == 0 {
		what := "jobs"
		if intent.Status != "" {
			what = intent.Status
		}
		scope := ""
		if intent.Folder != "" {
			scope = " na folder " + intent.Folder
		}
		return "Nenhum " + what + scope + " hoje."
	}
	ids := make([]string, 0, len(items))
	for i, it := range items {
		if i >= 15 {
			ids = append(ids, fmt.Sprintf("… (+%d)", len(items)-15))
			break
		}
		ids = append(ids, it.DefinitionID)
	}
	label := "jobs"
	if intent.Status != "" {
		label = intent.Status
	}
	return fmt.Sprintf("%d %s: %s.", len(items), label, strings.Join(ids, ", "))
}
