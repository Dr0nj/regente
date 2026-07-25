// Package api — D-10 Policy as Code: endpoint read-only + gate de publish.
//
//	GET /api/policy → política ativa (policies.yaml do workspace) + violações
//	                  do estado VIVO (o que já está publicado e fora da regra).
//
// O enforcement de verdade acontece no PUBLISH da session de Design e no
// apply do modo CODE (ver sessions.go/code.go): as defs do CLONE DA SESSION
// são validadas contra o policies.yaml DO PRÓPRIO CLONE — política e jobs
// promovem juntos, atômicos no mesmo commit.
package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Dr0nj/regente-server/internal/policy"
	"github.com/Dr0nj/regente-server/internal/storage"
)

// getPolicy — GET /api/policy: a política + violações do workspace publicado.
func (s *server) getPolicy(w http.ResponseWriter, r *http.Request) {
	pol, err := policy.Load(s.cfg.Store.Root())
	if err != nil {
		writeJSON(w, 200, map[string]any{"configured": false, "error": err.Error()})
		return
	}
	if pol == nil {
		writeJSON(w, 200, map[string]any{"configured": false})
		return
	}
	defs, err := s.cfg.Store.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, 200, map[string]any{
		"configured": true,
		"policy":     pol,
		"violations": pol.Validate(defs),
	})
}

// checkSessionPolicy — valida o working set de uma session contra o
// policies.yaml DO CLONE dela. Retorna (violations, blocking, err).
// Sem política / enforcement off → (nil, false, nil): publish segue livre.
func checkSessionPolicy(store *storage.FileStore) ([]policy.Violation, bool, error) {
	pol, err := policy.Load(store.Root())
	if err != nil {
		// Política QUEBRADA (YAML inválido/regex ruim) bloqueia: o autor pediu
		// governança e ela não está avaliável — falhar aberto seria burlável.
		return nil, true, err
	}
	if !pol.Active() {
		return nil, false, nil
	}
	defs, err := store.List()
	if err != nil {
		return nil, true, err
	}
	return pol.Validate(defs), pol.Blocking(), nil
}

// policyViolationsSummary — mensagem compacta pra erro HTTP (lista capada).
func policyViolationsSummary(vs []policy.Violation) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("policy: %d violation(s)", len(vs)))
	for i, v := range vs {
		if i >= 10 {
			b.WriteString(fmt.Sprintf("; … (+%d)", len(vs)-10))
			break
		}
		b.WriteString(fmt.Sprintf("; [%s] %s/%s: %s", v.Rule, v.Folder, v.DefID, v.Message))
	}
	return b.String()
}
