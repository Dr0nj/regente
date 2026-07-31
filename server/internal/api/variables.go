// F18 \u2014 Variables REST CRUD (global scope).
package api

import (
	"encoding/json"
	"net/http"

	"github.com/Dr0nj/regente-server/internal/auth"
)

// listVariables \u2014 GET /api/variables \u2192 array ordenado por nome.
//
// Seguran\u00e7a: o VALOR s\u00f3 sai para admin. O PUT/DELETE sempre exigiram admin, mas
// o GET n\u00e3o exigia nada e devolvia tudo em claro \u2014 qualquer usu\u00e1rio logado,
// viewer inclu\u00eddo, lia o conte\u00fado de uma vari\u00e1vel que porventura guardasse
// credencial. Assimetria era descuido, n\u00e3o decis\u00e3o: `listAgentTokens` j\u00e1 exp\u00f5e
// s\u00f3 os 8 primeiros caracteres do token, e `settings` mascara as chaves
// secretas. Aqui o n\u00e3o-admin continua vendo NOME/quando/quem (que \u00e9 o que ele
// precisa para escrever `%%NOME` num job), com o valor trocado por retic\u00eancias.
func (s *server) listVariables(w http.ResponseWriter, r *http.Request) {
	vs := s.cfg.Scheduler.Variables()
	if vs == nil {
		writeJSON(w, 200, []any{})
		return
	}
	list := vs.List()
	if u, ok := auth.FromContext(r.Context()); !ok || u == nil || !u.Role.CanAdmin() {
		for i := range list {
			if list[i].Value != "" {
				list[i].Value = maskedVariableValue
			}
		}
	}
	writeJSON(w, 200, list)
}

// maskedVariableValue \u2014 o que um n\u00e3o-admin recebe no lugar do valor. Marcador
// FIXO (n\u00e3o derivado do valor real): comprimento vari\u00e1vel j\u00e1 vaza o tamanho do
// segredo, que \u00e9 dica suficiente para encurtar um ataque de for\u00e7a bruta.
const maskedVariableValue = "\u2022\u2022\u2022\u2022\u2022\u2022"

// putVariable \u2014 PUT /api/variables/{name}  body: {"value": "..."} (admin).
func (s *server) putVariable(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	vs := s.cfg.Scheduler.Variables()
	if vs == nil {
		http.Error(w, "variables not configured", http.StatusServiceUnavailable)
		return
	}
	name := urlName(r, "name")
	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	actor := ""
	if u, ok := auth.FromContext(r.Context()); ok && u != nil {
		actor = u.Username
	}
	v, err := vs.Set(name, body.Value, actor)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if s.cfg.Hub != nil {
		s.cfg.Hub.BroadcastWeb("variables.changed", map[string]any{"name": name, "action": "set"})
	}
	writeJSON(w, 200, v)
}

// deleteVariable \u2014 DELETE /api/variables/{name} (admin, idempotente).
func (s *server) deleteVariable(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	vs := s.cfg.Scheduler.Variables()
	if vs == nil {
		http.Error(w, "variables not configured", http.StatusServiceUnavailable)
		return
	}
	name := urlName(r, "name")
	if err := vs.Delete(name); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if s.cfg.Hub != nil {
		s.cfg.Hub.BroadcastWeb("variables.changed", map[string]any{"name": name, "action": "delete"})
	}
	w.WriteHeader(204)
}
