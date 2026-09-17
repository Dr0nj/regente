package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/domain"
)

// Política imutável da subscription. Recarregada antes de cada envio e pelo
// watchdog; alteração fecha o canal, inclusive se houver mensagens na fila.
// ACLs seguem exatamente o REST: nomes exatos, zero linhas = leitura global.
type webAccess struct {
	User *auth.User
	ACLs []auth.FolderACL
}

func (s *server) webAccess(digest string) (*webAccess, error) {
	if s.cfg.Token != "" && digest == auth.Digest(s.cfg.Token) {
		if s.mode() != "local" && s.mode() != "hybrid" {
			return nil, auth.ErrInvalidToken
		}
		return &webAccess{User: &auth.User{Role: auth.RoleAdmin}}, nil
	}
	u, err := auth.ResolveDigest(s.cfg.DB, digest)
	if err != nil {
		return nil, err
	}
	if !s.sessionAllowed(u) || !u.Role.Valid() {
		return nil, auth.ErrInvalidToken
	}
	acls, err := auth.ListUserACLs(s.cfg.DB, u.ID)
	if err != nil {
		return nil, err
	}
	return &webAccess{User: u, ACLs: acls}, nil
}

func (a *webAccess) global() bool { return a.User.Role == auth.RoleAdmin || len(a.ACLs) == 0 }
func (a *webAccess) folder(name string) bool {
	if a.global() {
		return true
	}
	for _, acl := range a.ACLs {
		if acl.FolderName == name && strings.Contains(acl.Perms, "r") {
			return true
		}
	}
	return false
}
func (a *webAccess) fingerprint() string {
	raw, _ := json.Marshal(a)
	return string(raw)
}

// O filtro de ambiente apenas estreita a subscription; não concede ACL humana.
// nil = todos; ponteiro para "" = apenas jobs sem label.
type webScope struct {
	Folder      string `json:"folder"`
	Environment string `json:"environment"`
}

func (a *webAccess) scope(scope webScope, environment *string) bool {
	return a.folder(scope.Folder) && (environment == nil || *environment == scope.Environment)
}
func (s *server) instanceWebScope(id string) (webScope, error) {
	var scope webScope
	err := s.cfg.DB.QueryRow("SELECT COALESCE(team,''),COALESCE(environment,'') FROM instances WHERE id=?", id).Scan(&scope.Folder, &scope.Environment)
	// Mesma compatibilidade pré-v4 do read path; erro nunca vira folder livre.
	if err == nil && scope.Folder == "" {
		scope.Folder, err = s.instanceFolder(id)
	}
	return scope, err
}

// Tombstone emitido apenas pelo servidor, antes de perder o contexto no DELETE.
// _scope é metadado interno do barramento: nunca atravessa o writer web.
func (s *server) broadcastWeb(event string, payload any) {
	if s.cfg.Events != nil {
		s.cfg.Events.BroadcastWeb(event, payload)
	} else if s.cfg.Hub != nil {
		s.cfg.Hub.BroadcastWeb(event, payload)
	}
}

func stringField(p map[string]json.RawMessage, key string) string {
	var value string
	_ = json.Unmarshal(p[key], &value)
	return value
}
func projectFields(p map[string]json.RawMessage, keys ...string) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for _, key := range keys {
		if v, ok := p[key]; ok {
			out[key] = v
		}
	}
	return out
}

// workspaceView mede somente a visão autorizada. Um git sync de outro folder
// não pode anunciar SHA, nomes, contagens nem invalidar a UI deste usuário.
func (s *server) workspaceView(a *webAccess, environment *string) (string, error) {
	if s.cfg.Store == nil {
		return "", errors.New("workspace unavailable")
	}
	defs, err := s.cfg.Store.List()
	if err != nil {
		return "", err
	}
	folders, err := s.cfg.Store.ListFolders()
	if err != nil {
		return "", err
	}
	visible := []any{}
	for _, def := range defs {
		if a.scope(webScope{def.Team, def.Environment}, environment) {
			visible = append(visible, def)
		}
	}
	// Folder não tem ambiente próprio: inclui metadados só na subscription global.
	if environment == nil {
		for _, folder := range folders {
			if a.folder(folder.Name) {
				visible = append(visible, folder)
			}
		}
	}
	raw, err := json.Marshal(visible)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// Lista fechada de eventos e projeções. Payload desconhecido/sem prova de escopo
// falha fechado, inclusive para admin; novos eventos exigem política explícita.
func (s *server) filterWebEvent(a *webAccess, environment *string, view *string, raw []byte) []byte {
	var ev struct {
		Event   string                     `json:"event"`
		Payload map[string]json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(raw, &ev) != nil || ev.Payload == nil {
		return nil
	}
	p := ev.Payload
	var out any = map[string]any{}
	instanceAllowed := func(id string) bool {
		if id == "" {
			return false
		}
		scope, err := s.instanceWebScope(id)
		return err == nil && a.scope(scope, environment)
	}
	switch ev.Event {
	case "instance.changed":
		if !instanceAllowed(stringField(p, "id")) {
			return nil
		}
		out = projectFields(p, "id", "status", "startedAt", "finishedAt", "exitCode", "holdScope", "heldFromStatus", "confirmed", "forced", "cyclic", "setOk", "killed")
	case "instance.deleted":
		var scope webScope
		if stringField(p, "id") == "" || p["_scope"] == nil || json.Unmarshal(p["_scope"], &scope) != nil || !a.scope(scope, environment) {
			return nil
		}
		out = projectFields(p, "id")
	case "instance.bulk":
		// Lotes emitidos por folder ou por itens: só invalidação, nunca totais/atores.
		var scopes []webScope
		if json.Unmarshal(p["_scopes"], &scopes) != nil {
			if folder := stringField(p, "folder"); folder != "" && a.folder(folder) {
				if environment == nil {
					break
				}
				var n int
				if s.cfg.DB.QueryRow("SELECT COUNT(*) FROM instances WHERE team=? AND environment=?", folder, *environment).Scan(&n) == nil && n > 0 {
					break
				}
			}
			if !a.global() || environment != nil {
				return nil
			}
			break
		}
		allowed := false
		for _, scope := range scopes {
			if a.scope(scope, environment) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil
		}
	case "definition.changed", "definition.deleted", "folder.changed":
		// Refresh só se a visão publicada que este usuário pode ler mudou. Não
		// depende de payload confiável para autorizar conteúdo de uma definition.
		current, err := s.workspaceView(a, environment)
		if err != nil || current == *view {
			return nil
		}
		*view = current
		ev.Event = "_resync"
	case "alert.fired", "alert.changed":
		if id := stringField(p, "instanceId"); id != "" {
			if !instanceAllowed(id) {
				return nil
			}
		} else if !a.global() || environment != nil {
			return nil
		}
		if ev.Event == "alert.fired" {
			out = projectFields(p, "id", "instanceId", "ruleId", "ruleName", "severity", "timestamp", "workflowId", "workflowName", "message", "acknowledged")
		}
	case "sla.breach":
		if !instanceAllowed(stringField(p, "instanceId")) {
			return nil
		}
		out = projectFields(p, "id", "instanceId", "kind", "severity")
	case "daily.started":
		// Data de negócio é global e pública aos usuários; totais/SHA não são.
		out = projectFields(p, "orderDate")
	case "settings.changed":
		// Nunca publicar valores: podem conter tokens, SMTP e secrets de webhook.
	case "agent.changed":
		if !a.global() {
			return nil
		}
		if environment != nil {
			return nil
		} // desconexão antiga não traz ambiente
	case "condition.changed", "condition.set", "condition.unset":
		if !s.readableCondition(a, environment, stringField(p, "name")) {
			return nil
		}
	case "variables.changed", "git.drift":
		if !a.global() || environment != nil {
			return nil
		}
		// Apenas invalidação. Nomes/atores/SHAs não são necessários ao consumidor.
	default:
		return nil
	}
	encoded, err := json.Marshal(map[string]any{"event": ev.Event, "payload": out})
	if err != nil {
		return nil
	}
	return encoded
}

// A invalidação de condição só interessa se uma ordem visível referencia esse
// nome. Consulta candidatos no DB; valida o JSON/sufixo para não usar substring
// como prova de permissão. Nenhum nome/ator atravessa o evento.
func (s *server) readableCondition(a *webAccess, environment *string, name string) bool {
	if name == "" {
		return false
	}
	if a.global() && environment == nil {
		return true
	}
	pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(name) + "%"
	query := "SELECT COALESCE(team,''),COALESCE(environment,''),COALESCE(conds_in,''),COALESCE(conds_out_add,'') FROM instances WHERE (conds_in LIKE ? ESCAPE '!' OR conds_out_add LIKE ? ESCAPE '!')"
	args := []any{pattern, pattern}
	if environment != nil {
		query += " AND environment=?"
		args = append(args, *environment)
	}
	rows, err := s.cfg.DB.Query(query, args...)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var scope webScope
		var in, out string
		if rows.Scan(&scope.Folder, &scope.Environment, &in, &out) != nil {
			return false
		}
		if !a.scope(scope, environment) {
			continue
		}
		for _, ref := range append(decodeConds(in), decodeConds(out)...) {
			base, _ := domain.SplitCondRef(ref)
			if base == name {
				return true
			}
		}
	}
	return false
}
