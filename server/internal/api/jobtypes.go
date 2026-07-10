package api

import (
	"net/http"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// jobTypeCatalog — ADV-1: o catálogo de jobTypes com o schema DEDICADO de cada
// um (campos, obrigatórios, kinds, enums, aliases), direto do registry que o
// validador usa (domain/typeschema.go). Read-only; serve UI, integradores e o
// futuro SDK/docs (ADV-6/7).
func (s *server) jobTypeCatalog(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]interface{}{"jobTypes": domain.JobTypeCatalog()})
}
