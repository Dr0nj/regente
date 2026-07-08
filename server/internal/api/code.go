// Package api — Job-as-code: o modo código do Design (2026-07-06).
//
// O canvas e o código são DUAS VISÕES do mesmo working set da design session:
//
//	GET  /api/design/sessions/{sid}/code  → definitions das folders abertas como
//	                                        UM documento YAML multi-doc (mesmo
//	                                        dialeto dos arquivos do workspace Git)
//	POST /api/design/sessions/{sid}/code  → parseia o YAML de volta, calcula o
//	                                        PLANO (creates/updates/deletes) e,
//	                                        com apply=true, executa item a item
//
// Semântica WYSIWYG: o documento É o working set das folders no escopo — job
// ausente no código = delete (gated por allowDelete, o front pede confirmação).
// apply=false é o dry-run: valida e devolve o plano sem tocar em nada.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/policy"
	"gopkg.in/yaml.v3"
)

// codeHeader — preâmbulo comentado do documento gerado (guia rápido pro dev).
const codeHeader = `# Regente — jobs as code (YAML)
# Um documento por job, separados por "---". Mesmo formato dos arquivos do
# workspace Git (definitions/<team>/<id>.yaml). Job removido daqui = deletado
# ao aplicar (com confirmação). Campos principais:
#   id, label, team, jobType, schedule{enabled,runAt,frequency,...},
#   retries, timeout, params{...}, upstream[{from,condition}], actions[...]
`

// sessionCodeScope — resolve o conjunto de folders do documento: a interseção
// pedida via ?folders=/body, ou todas as folders da session.
func sessionCodeScope(sess sessionFolders, requested []string) (map[string]bool, error) {
	all := map[string]bool{}
	for _, f := range sess.allFolders() {
		all[f] = true
	}
	if len(requested) == 0 {
		return all, nil
	}
	scope := map[string]bool{}
	for _, f := range requested {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !all[f] {
			return nil, fmt.Errorf("folder %q fora do escopo da session", f)
		}
		scope[f] = true
	}
	if len(scope) == 0 {
		return all, nil
	}
	return scope, nil
}

// sessionFolders — visão mínima da session usada pelo escopo (testável sem Git).
type sessionFolders interface{ allFolders() []string }

type sessFoldersView struct{ folders, newFolders []string }

func (v sessFoldersView) allFolders() []string {
	out := append([]string{}, v.folders...)
	out = append(out, v.newFolders...)
	return out
}

// GET /api/design/sessions/{sid}/code?folders=A,B
func (s *server) getSessionCode(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromURL(w, r)
	if !ok {
		return
	}
	var requested []string
	if q := strings.TrimSpace(r.URL.Query().Get("folders")); q != "" {
		requested = strings.Split(q, ",")
	}
	scope, err := sessionCodeScope(sessFoldersView{sess.Folders, sess.NewFolders}, requested)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defs, err := sess.Store.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	code, count := marshalDefsAsCode(defs, scope)
	folders := make([]string, 0, len(scope))
	for f := range scope {
		folders = append(folders, f)
	}
	sort.Strings(folders)
	writeJSON(w, 200, map[string]any{
		"session": sess.ID,
		"folders": folders,
		"count":   count,
		"code":    code,
	})
}

// marshalDefsAsCode — serializa as defs no escopo como YAML multi-doc estável
// (ordenado por team,id) com um comentário de caminho por doc.
func marshalDefsAsCode(defs []domain.JobDefinition, scope map[string]bool) (string, int) {
	inScope := make([]domain.JobDefinition, 0, len(defs))
	for _, d := range defs {
		if scope[d.Team] {
			inScope = append(inScope, d)
		}
	}
	sort.Slice(inScope, func(i, j int) bool {
		if inScope[i].Team != inScope[j].Team {
			return inScope[i].Team < inScope[j].Team
		}
		return inScope[i].ID < inScope[j].ID
	})
	var b strings.Builder
	b.WriteString(codeHeader)
	for _, d := range inScope {
		raw, err := yaml.Marshal(&d)
		if err != nil {
			continue
		}
		b.WriteString("---\n")
		fmt.Fprintf(&b, "# definitions/%s/%s.yaml\n", d.Team, d.ID)
		b.Write(raw)
	}
	return b.String(), len(inScope)
}

// parseCodeDocs — decodifica o YAML multi-doc em defs, estrito (campo
// desconhecido = erro, pega typo) e com defaults amigáveis (team único no
// escopo preenche team ausente).
func parseCodeDocs(code string, scope map[string]bool) ([]domain.JobDefinition, []string) {
	var out []domain.JobDefinition
	var errs []string
	soleFolder := ""
	if len(scope) == 1 {
		for f := range scope {
			soleFolder = f
		}
	}
	dec := yaml.NewDecoder(strings.NewReader(code))
	dec.KnownFields(true)
	seen := map[string]bool{}
	for docN := 1; ; docN++ {
		var def domain.JobDefinition
		err := dec.Decode(&def)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("doc #%d: %v", docN, err))
			break // o decoder do yaml.v3 não recupera posição após erro
		}
		// Doc vazio (só comentários/---) — ignora silenciosamente.
		if def.ID == "" && def.Label == "" && def.JobType == "" {
			continue
		}
		if def.Team == "" && soleFolder != "" {
			def.Team = soleFolder
		}
		if def.ID == "" {
			errs = append(errs, fmt.Sprintf("doc #%d: id obrigatório", docN))
			continue
		}
		if seen[def.ID] {
			errs = append(errs, fmt.Sprintf("doc #%d: id duplicado %q", docN, def.ID))
			continue
		}
		seen[def.ID] = true
		if !scope[def.Team] {
			errs = append(errs, fmt.Sprintf("%s: team %q fora do escopo do documento", def.ID, def.Team))
			continue
		}
		if err := domain.ValidateDefinition(def); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", def.ID, err))
			continue
		}
		out = append(out, def)
	}
	return out, errs
}

// codePlan — o diff código→working set.
type codePlan struct {
	Creates   []string `json:"creates"`
	Updates   []string `json:"updates"`
	Deletes   []string `json:"deletes"`
	Unchanged int      `json:"unchanged"`
}

// buildCodePlan — compara o desejado (código) com o existente (working set no
// escopo). Igualdade por YAML canônico (mesma serialização = sem mudança).
func buildCodePlan(existing, desired []domain.JobDefinition, scope map[string]bool) (codePlan, map[string]domain.JobDefinition) {
	plan := codePlan{Creates: []string{}, Updates: []string{}, Deletes: []string{}}
	prev := map[string]domain.JobDefinition{}
	for _, d := range existing {
		if scope[d.Team] {
			prev[d.ID] = d
		}
	}
	seen := map[string]bool{}
	for _, d := range desired {
		seen[d.ID] = true
		old, found := prev[d.ID]
		if !found {
			plan.Creates = append(plan.Creates, d.ID)
			continue
		}
		a, _ := yaml.Marshal(&old)
		b, _ := yaml.Marshal(&d)
		if string(a) == string(b) {
			plan.Unchanged++
		} else {
			plan.Updates = append(plan.Updates, d.ID)
		}
	}
	for id := range prev {
		if !seen[id] {
			plan.Deletes = append(plan.Deletes, id)
		}
	}
	sort.Strings(plan.Creates)
	sort.Strings(plan.Updates)
	sort.Strings(plan.Deletes)
	return plan, prev
}

// POST /api/design/sessions/{sid}/code
// body: {"code": "...", "folders": [...], "apply": bool, "allowDelete": bool}
func (s *server) applySessionCode(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.sessionFromURL(w, r)
	if !ok {
		return
	}
	var req struct {
		Code        string   `json:"code"`
		Folders     []string `json:"folders,omitempty"`
		Apply       bool     `json:"apply"`
		AllowDelete bool     `json:"allowDelete"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	scope, err := sessionCodeScope(sessFoldersView{sess.Folders, sess.NewFolders}, req.Folders)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(req.Code) > 4<<20 {
		http.Error(w, "code too large (max 4MB)", http.StatusBadRequest)
		return
	}

	desired, errs := parseCodeDocs(req.Code, scope)
	existing, err := sess.Store.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	plan, prev := buildCodePlan(existing, desired, scope)

	// ACL por folder tocada (creates/updates: destino e origem se mudou; deletes: origem).
	u, _ := auth.FromContext(r.Context())
	aclErr := func(team string) string {
		if u == nil {
			return ""
		}
		if can, _ := auth.CanWriteFolder(s.cfg.DB, u, team); !can {
			return "sem permissão de escrita na folder " + team
		}
		return ""
	}

	// D-10 — feedback antecipado de policy no editor: violações do estado
	// DESEJADO (o YAML como escrito), calculadas contra o policies.yaml do
	// clone. Só informativo aqui — o gate que bloqueia é o do publish.
	var policyViolations any
	if pol, perr := policy.Load(sess.Store.Root()); perr == nil && pol.Active() {
		if vs := pol.Validate(desired); len(vs) > 0 {
			policyViolations = vs
		}
	}

	respond := func(applied bool, results []bulkItemResult) {
		writeJSON(w, 200, map[string]any{
			"session":          sess.ID,
			"parsed":           len(desired),
			"plan":             plan,
			"errors":           errs,
			"applied":          applied,
			"results":          results,
			"policyViolations": policyViolations,
		})
	}

	if len(errs) > 0 || !req.Apply {
		respond(false, nil)
		return
	}
	if len(plan.Deletes) > 0 && !req.AllowDelete {
		errs = append(errs, fmt.Sprintf("%d job(s) ausentes no código seriam DELETADOS (%s) — confirme a remoção",
			len(plan.Deletes), strings.Join(plan.Deletes, ", ")))
		respond(false, nil)
		return
	}

	// Aplica transacional POR ITEM (mesma semântica do bulk): falha parcial é
	// reportada item a item, não aborta o lote.
	results := make([]bulkItemResult, 0, len(plan.Creates)+len(plan.Updates)+len(plan.Deletes))
	desiredByID := map[string]domain.JobDefinition{}
	for _, d := range desired {
		desiredByID[d.ID] = d
	}
	for _, id := range append(append([]string{}, plan.Creates...), plan.Updates...) {
		def := desiredByID[id]
		if msg := aclErr(def.Team); msg != "" {
			results = append(results, bulkItemResult{ID: id, Error: msg})
			continue
		}
		old, existed := prev[id]
		if existed && old.Team != def.Team {
			if msg := aclErr(old.Team); msg != "" {
				results = append(results, bulkItemResult{ID: id, Error: msg})
				continue
			}
		}
		if err := sess.Store.Save(def); err != nil {
			results = append(results, bulkItemResult{ID: id, Error: err.Error()})
			continue
		}
		// Job movido de folder no código: remove o arquivo da folder antiga.
		if existed && old.Team != def.Team {
			if err := sess.Store.Delete(old.Team, old.ID); err != nil {
				results = append(results, bulkItemResult{ID: id, Error: "salvo em " + def.Team + " mas falhou remover de " + old.Team + ": " + err.Error()})
				continue
			}
		}
		status := "updated"
		if !existed {
			status = "created"
		}
		results = append(results, bulkItemResult{ID: id, OK: true, Status: status})
	}
	for _, id := range plan.Deletes {
		old := prev[id]
		if msg := aclErr(old.Team); msg != "" {
			results = append(results, bulkItemResult{ID: id, Error: msg})
			continue
		}
		if err := sess.Store.Delete(old.Team, old.ID); err != nil {
			results = append(results, bulkItemResult{ID: id, Error: err.Error()})
			continue
		}
		results = append(results, bulkItemResult{ID: id, OK: true, Status: "deleted"})
	}

	s.cfg.Sessions.PersistSession(sess)
	respond(true, results)
}
