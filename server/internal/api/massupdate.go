// Package api — CTM-3: Mass Update / Find & Update RICO no Design (2026-07-06).
//
//	POST /api/design/sessions/{sid}/massupdate       → seleção por critério
//	     (folders/jobType/regex em campo/campo vazio, e/ou ids explícitos) +
//	     operação (set-field/find-replace/add-action/add-upstream/...).
//	     apply=false = PREVIEW: devolve o diff por job SEM tocar em nada.
//	     apply=true  = executa transacional POR ITEM e empilha snapshot p/ undo.
//	POST /api/design/sessions/{sid}/massupdate/undo  → desfaz a última aplicação
//	     (re-salva as defs como eram antes). Pilha in-memory por session (cap 10;
//	     não sobrevive a restart do server — a session sim).
//
// Tudo opera no clone DA SESSION (write-staging): nada vai pro Git até Publish.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/domain"
)

// ── Undo stack (in-memory, por session) ─────────────────────────────────────

type massUndoEntry struct {
	At     time.Time              `json:"at"`
	Label  string                 `json:"label"`
	Before []domain.JobDefinition `json:"-"`
	IDs    []string               `json:"ids"`
}

type massUndoStore struct {
	mu    sync.Mutex
	stack map[string][]massUndoEntry // sid → pilha (topo = último)
}

var massUndo = &massUndoStore{stack: map[string][]massUndoEntry{}}

const massUndoCap = 10

func (m *massUndoStore) push(sid string, e massUndoEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := append(m.stack[sid], e)
	if len(st) > massUndoCap {
		st = st[len(st)-massUndoCap:]
	}
	m.stack[sid] = st
}

func (m *massUndoStore) pop(sid string) (massUndoEntry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stack[sid]
	if len(st) == 0 {
		return massUndoEntry{}, false
	}
	e := st[len(st)-1]
	m.stack[sid] = st[:len(st)-1]
	return e, true
}

func (m *massUndoStore) depth(sid string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.stack[sid])
}

func (m *massUndoStore) clear(sid string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.stack, sid)
}

// ── Acesso a campos por nome ────────────────────────────────────────────────
//
// Conjunto FIXO de campos endereçáveis (não reflexão): previsível, validável e
// com mensagens de erro claras. "description" é alias de "schedule.description".

func normalizeField(f string) string {
	if f == "description" {
		return "schedule.description"
	}
	return f
}

// getStringField — leitura de campos string (p/ critério regex e find-replace).
func getStringField(d *domain.JobDefinition, field string) (string, bool) {
	switch normalizeField(field) {
	case "id":
		return d.ID, true
	case "label":
		return d.Label, true
	case "team":
		return d.Team, true
	case "jobType":
		return d.JobType, true
	case "agentId":
		return d.AgentID, true
	case "calendar":
		return d.Calendar, true
	case "environment":
		return d.Environment, true
	case "schedule.description":
		return d.Schedule.Description, true
	case "schedule.runAt":
		return d.Schedule.RunAt, true
	case "schedule.windowFrom":
		return d.Schedule.WindowFrom, true
	case "schedule.windowTo":
		return d.Schedule.WindowTo, true
	}
	return "", false
}

// setField — escrita p/ set-field. value chega como JSON (string/número/bool).
func setField(d *domain.JobDefinition, field string, value any) error {
	asString := func() (string, error) {
		s, ok := value.(string)
		if !ok {
			return "", fmt.Errorf("campo %s espera string", field)
		}
		return s, nil
	}
	asInt := func() (int, error) {
		n, ok := value.(float64) // JSON number
		if !ok || n != float64(int(n)) {
			return 0, fmt.Errorf("campo %s espera inteiro", field)
		}
		return int(n), nil
	}
	asBool := func() (bool, error) {
		b, ok := value.(bool)
		if !ok {
			return false, fmt.Errorf("campo %s espera booleano", field)
		}
		return b, nil
	}
	var err error
	switch normalizeField(field) {
	case "label":
		d.Label, err = asString()
	case "jobType":
		d.JobType, err = asString()
	case "agentId":
		d.AgentID, err = asString()
	case "calendar":
		d.Calendar, err = asString()
	case "environment":
		d.Environment, err = asString()
	case "schedule.description":
		d.Schedule.Description, err = asString()
	case "schedule.runAt":
		d.Schedule.RunAt, err = asString()
	case "schedule.windowFrom":
		d.Schedule.WindowFrom, err = asString()
	case "schedule.windowTo":
		d.Schedule.WindowTo, err = asString()
	case "retries":
		d.Retries, err = asInt()
	case "timeout":
		d.Timeout, err = asInt()
	case "schedule.enabled":
		d.Schedule.Enabled, err = asBool()
	case "dryRun":
		d.DryRun, err = asBool()
	case "confirm":
		d.Confirm, err = asBool()
	default:
		return fmt.Errorf("campo não endereçável: %s", field)
	}
	return err
}

// getFieldDisplay — valor exibível de qualquer campo endereçável (preview).
func getFieldDisplay(d *domain.JobDefinition, field string) string {
	if v, ok := getStringField(d, field); ok {
		return v
	}
	switch normalizeField(field) {
	case "retries":
		return fmt.Sprintf("%d", d.Retries)
	case "timeout":
		return fmt.Sprintf("%d", d.Timeout)
	case "schedule.enabled":
		return fmt.Sprintf("%v", d.Schedule.Enabled)
	case "dryRun":
		return fmt.Sprintf("%v", d.DryRun)
	case "confirm":
		return fmt.Sprintf("%v", d.Confirm)
	}
	return ""
}

// deepCopyDef — cópia PROFUNDA da definition (round-trip JSON): as operações
// mutam slices/maps internos e o snapshot de undo precisa do original intacto.
func deepCopyDef(d domain.JobDefinition) (domain.JobDefinition, error) {
	raw, err := json.Marshal(d)
	if err != nil {
		return domain.JobDefinition{}, err
	}
	var out domain.JobDefinition
	if err := json.Unmarshal(raw, &out); err != nil {
		return domain.JobDefinition{}, err
	}
	// Params viaja como "actionConfig" no JSON — o round-trip preserva (mesma tag).
	return out, nil
}

// replaceInField — find-replace em UM campo string, ou em TODOS os valores
// string de params (field=="params"). Retorna se algo mudou.
func replaceInField(d *domain.JobDefinition, field string, re *regexp.Regexp, repl string) (bool, error) {
	if field == "params" {
		changed := false
		var walk func(v any) any
		walk = func(v any) any {
			switch vv := v.(type) {
			case string:
				out := re.ReplaceAllString(vv, repl)
				if out != vv {
					changed = true
				}
				return out
			case map[string]any:
				for k, x := range vv {
					vv[k] = walk(x)
				}
				return vv
			case []any:
				for i, x := range vv {
					vv[i] = walk(x)
				}
				return vv
			default:
				return v
			}
		}
		if d.Params != nil {
			for k, v := range d.Params {
				d.Params[k] = walk(v)
			}
		}
		return changed, nil
	}
	cur, ok := getStringField(d, field)
	if !ok {
		return false, fmt.Errorf("campo não endereçável para find-replace: %s", field)
	}
	next := re.ReplaceAllString(cur, repl)
	if next == cur {
		return false, nil
	}
	// find-replace não reescreve chaves de identidade.
	nf := normalizeField(field)
	if nf == "id" || nf == "team" {
		return false, fmt.Errorf("find-replace em %s não é permitido (use rename/move)", nf)
	}
	return true, setField(d, field, next)
}

// ── Critério de seleção ─────────────────────────────────────────────────────

type massCriteria struct {
	IDs        []string `json:"ids,omitempty"`        // restringe aos ids (seleção do canvas)
	Folders    []string `json:"folders,omitempty"`    // restringe às folders
	JobType    string   `json:"jobType,omitempty"`    // igualdade exata
	Field      string   `json:"field,omitempty"`      // campo do regex
	Regex      string   `json:"regex,omitempty"`      // regex sobre Field
	FieldEmpty string   `json:"fieldEmpty,omitempty"` // campo string vazio
}

func (c massCriteria) selectDefs(defs []domain.JobDefinition) ([]domain.JobDefinition, error) {
	var re *regexp.Regexp
	if c.Regex != "" {
		if c.Field == "" {
			return nil, fmt.Errorf("regex requer field")
		}
		var err error
		re, err = regexp.Compile(c.Regex)
		if err != nil {
			return nil, fmt.Errorf("regex inválido: %v", err)
		}
	}
	idSet := map[string]bool{}
	for _, id := range c.IDs {
		idSet[id] = true
	}
	folderSet := map[string]bool{}
	for _, f := range c.Folders {
		folderSet[f] = true
	}
	out := []domain.JobDefinition{}
	for _, d := range defs {
		if len(idSet) > 0 && !idSet[d.ID] {
			continue
		}
		if len(folderSet) > 0 && !folderSet[d.Team] {
			continue
		}
		if c.JobType != "" && !strings.EqualFold(d.JobType, c.JobType) {
			continue
		}
		if re != nil {
			v, ok := getStringField(&d, c.Field)
			if !ok || !re.MatchString(v) {
				continue
			}
		}
		if c.FieldEmpty != "" {
			v, ok := getStringField(&d, c.FieldEmpty)
			if !ok || strings.TrimSpace(v) != "" {
				continue
			}
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ── Operação ────────────────────────────────────────────────────────────────

type massOperation struct {
	Op string `json:"op"`

	// set-field / find-replace
	Field       string `json:"field,omitempty"`
	Value       any    `json:"value,omitempty"`
	OnlyIfEmpty bool   `json:"onlyIfEmpty,omitempty"`
	Find        string `json:"find,omitempty"`
	Replace     string `json:"replace,omitempty"`

	// add-action / remove-action
	Action      *domain.ActionRule `json:"action,omitempty"`
	ActionMatch *domain.ActionRule `json:"actionMatch,omitempty"`

	// add-upstream / remove-upstream
	Upstream *domain.Upstream `json:"upstream,omitempty"`

	// set-variable / remove-variable / add-condition-in / remove-condition-in
	Key string `json:"key,omitempty"`
	Val string `json:"val,omitempty"`
}

type massChange struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}

func compactJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// applyOperation — muta a def in-place; devolve as mudanças (vazio = no-op).
func applyOperation(d *domain.JobDefinition, op massOperation) ([]massChange, error) {
	switch op.Op {
	case "set-field":
		if op.Field == "" {
			return nil, fmt.Errorf("set-field requer field")
		}
		if op.OnlyIfEmpty {
			if cur, ok := getStringField(d, op.Field); !ok {
				return nil, fmt.Errorf("onlyIfEmpty só se aplica a campos string")
			} else if strings.TrimSpace(cur) != "" {
				return nil, nil // já preenchido — pula
			}
		}
		before := getFieldDisplay(d, op.Field)
		if err := setField(d, op.Field, op.Value); err != nil {
			return nil, err
		}
		after := getFieldDisplay(d, op.Field)
		if before == after {
			return nil, nil
		}
		return []massChange{{Field: normalizeField(op.Field), Before: before, After: after}}, nil

	case "find-replace":
		if op.Field == "" || op.Find == "" {
			return nil, fmt.Errorf("find-replace requer field e find")
		}
		re, err := regexp.Compile(op.Find)
		if err != nil {
			return nil, fmt.Errorf("find inválido: %v", err)
		}
		before, _ := getStringField(d, op.Field)
		changed, err := replaceInField(d, op.Field, re, op.Replace)
		if err != nil {
			return nil, err
		}
		if !changed {
			return nil, nil
		}
		after, ok := getStringField(d, op.Field)
		if !ok { // params: diff simbólico
			before, after = "…"+op.Find+"…", "…"+op.Replace+"…"
		}
		return []massChange{{Field: normalizeField(op.Field), Before: before, After: after}}, nil

	case "add-action":
		if op.Action == nil {
			return nil, fmt.Errorf("add-action requer action")
		}
		want := compactJSON(op.Action)
		for _, a := range d.Actions {
			if compactJSON(a) == want { // ActionRule tem slice — igualdade via JSON
				return nil, nil // idêntica já existe — idempotente
			}
		}
		d.Actions = append(d.Actions, *op.Action)
		return []massChange{{Field: "actions", Before: "", After: compactJSON(op.Action)}}, nil

	case "remove-action":
		if op.ActionMatch == nil {
			return nil, fmt.Errorf("remove-action requer actionMatch")
		}
		m := *op.ActionMatch
		kept := d.Actions[:0]
		var removed []string
		for _, a := range d.Actions {
			match := (m.On == "" || a.On == m.On) &&
				(m.Do == "" || a.Do == m.Do) &&
				(m.Status == "" || a.Status == m.Status) &&
				(m.Condition == "" || a.Condition == m.Condition) &&
				(m.TargetJob == "" || a.TargetJob == m.TargetJob)
			if match {
				removed = append(removed, compactJSON(a))
				continue
			}
			kept = append(kept, a)
		}
		if len(removed) == 0 {
			return nil, nil
		}
		d.Actions = kept
		return []massChange{{Field: "actions", Before: strings.Join(removed, " · "), After: ""}}, nil

	case "add-upstream":
		if op.Upstream == nil || op.Upstream.From == "" {
			return nil, fmt.Errorf("add-upstream requer upstream.from")
		}
		if op.Upstream.From == d.ID {
			return nil, nil // não cria dependência de si mesmo
		}
		cond := op.Upstream.Condition
		if cond == "" {
			cond = domain.CondOnSuccess
		}
		for i, u := range d.Upstream {
			if u.From == op.Upstream.From {
				if u.Condition == cond {
					return nil, nil
				}
				before := compactJSON(u)
				d.Upstream[i].Condition = cond
				return []massChange{{Field: "upstream", Before: before, After: compactJSON(d.Upstream[i])}}, nil
			}
		}
		u := domain.Upstream{From: op.Upstream.From, Condition: cond}
		d.Upstream = append(d.Upstream, u)
		return []massChange{{Field: "upstream", Before: "", After: compactJSON(u)}}, nil

	case "remove-upstream":
		if op.Upstream == nil || op.Upstream.From == "" {
			return nil, fmt.Errorf("remove-upstream requer upstream.from")
		}
		kept := d.Upstream[:0]
		var removed string
		for _, u := range d.Upstream {
			if u.From == op.Upstream.From {
				removed = compactJSON(u)
				continue
			}
			kept = append(kept, u)
		}
		if removed == "" {
			return nil, nil
		}
		d.Upstream = kept
		return []massChange{{Field: "upstream", Before: removed, After: ""}}, nil

	case "set-variable":
		if op.Key == "" {
			return nil, fmt.Errorf("set-variable requer key")
		}
		before := ""
		if d.Variables != nil {
			before = d.Variables[op.Key]
		}
		if before == op.Val {
			return nil, nil
		}
		if d.Variables == nil {
			d.Variables = map[string]string{}
		}
		d.Variables[op.Key] = op.Val
		return []massChange{{Field: "variables." + op.Key, Before: before, After: op.Val}}, nil

	case "remove-variable":
		if op.Key == "" {
			return nil, fmt.Errorf("remove-variable requer key")
		}
		if d.Variables == nil {
			return nil, nil
		}
		before, found := d.Variables[op.Key]
		if !found {
			return nil, nil
		}
		delete(d.Variables, op.Key)
		return []massChange{{Field: "variables." + op.Key, Before: before, After: ""}}, nil

	case "add-condition-in":
		if op.Key == "" {
			return nil, fmt.Errorf("add-condition-in requer key")
		}
		for _, c := range d.ConditionsIn {
			if c == op.Key {
				return nil, nil
			}
		}
		d.ConditionsIn = append(d.ConditionsIn, op.Key)
		return []massChange{{Field: "conditionsIn", Before: "", After: op.Key}}, nil

	case "remove-condition-in":
		if op.Key == "" {
			return nil, fmt.Errorf("remove-condition-in requer key")
		}
		kept := d.ConditionsIn[:0]
		found := false
		for _, c := range d.ConditionsIn {
			if c == op.Key {
				found = true
				continue
			}
			kept = append(kept, c)
		}
		if !found {
			return nil, nil
		}
		d.ConditionsIn = kept
		return []massChange{{Field: "conditionsIn", Before: op.Key, After: ""}}, nil
	}
	return nil, fmt.Errorf("operação desconhecida: %s", op.Op)
}

// ── Handlers ────────────────────────────────────────────────────────────────

type massItemPreview struct {
	ID      string       `json:"id"`
	Team    string       `json:"team"`
	Label   string       `json:"label"`
	Changes []massChange `json:"changes"`
	Error   string       `json:"error,omitempty"`
	OK      bool         `json:"ok"`
}

// POST /api/design/sessions/{sid}/massupdate
// body: {"criteria": {...}, "operation": {...}, "apply": false}
func (s *server) massUpdateSession(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromURL(w, r)
	if !ok {
		return
	}
	var req struct {
		Criteria  massCriteria  `json:"criteria"`
		Operation massOperation `json:"operation"`
		Apply     bool          `json:"apply"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Operation.Op == "" {
		http.Error(w, "operation.op required", http.StatusBadRequest)
		return
	}
	defs, err := sess.Store.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Escopo de segurança: só definitions das folders da session.
	scope := map[string]bool{}
	for _, f := range append(append([]string{}, sess.Folders...), sess.NewFolders...) {
		scope[f] = true
	}
	inScope := defs[:0]
	for _, d := range defs {
		if scope[d.Team] {
			inScope = append(inScope, d)
		}
	}
	selected, err := req.Criteria.selectDefs(inScope)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(selected) > 500 {
		http.Error(w, fmt.Sprintf("critério casa %d jobs (max 500) — restrinja a seleção", len(selected)), http.StatusBadRequest)
		return
	}

	u, _ := auth.FromContext(r.Context())
	items := make([]massItemPreview, 0, len(selected))
	var before []domain.JobDefinition // snapshot p/ undo (só os efetivamente mudados)
	var changedIDs []string
	applied := 0

	for _, orig := range selected {
		item := massItemPreview{ID: orig.ID, Team: orig.Team, Label: orig.Label}
		// Cópia PROFUNDA: as operações mutam slices/maps internos e o snapshot
		// de undo (orig) precisa permanecer intacto.
		mod, err := deepCopyDef(orig)
		if err != nil {
			item.Error = err.Error()
			items = append(items, item)
			continue
		}
		changes, err := applyOperation(&mod, req.Operation)
		if err != nil {
			item.Error = err.Error()
			items = append(items, item)
			continue
		}
		if len(changes) == 0 {
			continue // no-op p/ este job — fica fora do preview/aplicação
		}
		item.Changes = changes
		if u != nil {
			if can, _ := auth.CanWriteFolder(s.cfg.DB, u, mod.Team); !can {
				item.Error = "sem permissão de escrita na folder " + mod.Team
				items = append(items, item)
				continue
			}
		}
		if req.Apply {
			// escreve na SESSION (draft) — obrigatórios ficam pro publish
			if err := domain.ValidateDefinitionDraft(mod); err != nil {
				item.Error = err.Error()
				items = append(items, item)
				continue
			}
			if err := sess.Store.Save(mod); err != nil {
				item.Error = err.Error()
				items = append(items, item)
				continue
			}
			before = append(before, orig)
			changedIDs = append(changedIDs, orig.ID)
			applied++
		}
		item.OK = true
		items = append(items, item)
	}

	if req.Apply && applied > 0 {
		massUndo.push(sess.ID, massUndoEntry{
			At:     time.Now(),
			Label:  req.Operation.Op,
			Before: before,
			IDs:    changedIDs,
		})
		s.cfg.Sessions.PersistSession(sess)
	}
	writeJSON(w, 200, map[string]any{
		"session":   sess.ID,
		"matched":   len(selected),
		"changed":   len(items),
		"applied":   req.Apply,
		"items":     items,
		"undoDepth": massUndo.depth(sess.ID),
	})
}

// POST /api/design/sessions/{sid}/massupdate/undo — desfaz a ÚLTIMA aplicação.
func (s *server) massUpdateUndo(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromURL(w, r)
	if !ok {
		return
	}
	entry, found := massUndo.pop(sess.ID)
	if !found {
		http.Error(w, "nada para desfazer nesta session", http.StatusNotFound)
		return
	}
	u, _ := auth.FromContext(r.Context())
	results := make([]bulkItemResult, 0, len(entry.Before))
	for _, def := range entry.Before {
		if u != nil {
			if can, _ := auth.CanWriteFolder(s.cfg.DB, u, def.Team); !can {
				results = append(results, bulkItemResult{ID: def.ID, Error: "sem permissão de escrita na folder " + def.Team})
				continue
			}
		}
		if err := sess.Store.Save(def); err != nil {
			results = append(results, bulkItemResult{ID: def.ID, Error: err.Error()})
			continue
		}
		results = append(results, bulkItemResult{ID: def.ID, OK: true, Status: "restored"})
	}
	s.cfg.Sessions.PersistSession(sess)
	resp := newBulkResponse("massupdate-undo", results)
	writeJSON(w, 200, map[string]any{
		"session":   sess.ID,
		"label":     entry.Label,
		"at":        entry.At,
		"total":     resp.Total,
		"ok":        resp.Ok,
		"failed":    resp.Failed,
		"results":   resp.Results,
		"undoDepth": massUndo.depth(sess.ID),
	})
}
