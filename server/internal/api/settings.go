// F20 — server settings (key/value persisted in SQLite).
package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/Dr0nj/regente-server/internal/audit"
)

// secretSettingKeys — chaves cujo VALOR nunca sai do server: o GET devolve só
// "<chave>_set":"true" e a auditoria de mudanças (E2) registra a alteração sem
// o conteúdo. Fonte única — getSettings e putSettings usam a MESMA lista.
var secretSettingKeys = map[string]bool{
	"github_token":                true,
	"webhook_secret":              true,
	"alert_slack_webhook":         true, // URL de webhook Slack é credencial
	"alert_webhook_url":           true,
	"alert_smtp_password":         true,
	"alert_pagerduty_routing_key": true,
}

// getSettings returns all settings as a flat JSON object.
func (s *server) getSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.cfg.DB.Query(`SELECT key, value FROM settings`)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		// Nunca devolver segredos em claro; só sinaliza se estão setados.
		if secretSettingKeys[k] {
			if v != "" {
				m[k+"_set"] = "true"
			}
			continue
		}
		m[k] = v
	}
	if !rowsOK(w, rows) {
		return
	}
	writeJSON(w, 200, m)
}

// putSettings merges provided keys into the settings table (admin-only).
// E2 — cada PUT gera um evento de auditoria (settings.write) com a LISTA de
// chaves efetivamente alteradas e os valores de→para — exceto segredos, que
// aparecem só como "(alterado)".
func (s *server) putSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	// Snapshot dos valores atuais ANTES do write — é o "de" do diff de auditoria
	// e o filtro de no-op (chave re-enviada com o mesmo valor não vira evento).
	// `old` parcial só degrada o diff de auditoria (chave "nova" que já existia);
	// não bloqueia o write — erro de iteração é 500 mesmo assim por honestidade.
	old := map[string]string{}
	if rows, err := s.cfg.DB.Query(`SELECT key, value FROM settings`); err == nil {
		for rows.Next() {
			var k, v string
			if rows.Scan(&k, &v) == nil {
				old[k] = v
			}
		}
		if !rowsOK(w, rows) {
			rows.Close()
			return
		}
		rows.Close()
	}
	tx, err := s.cfg.DB.Begin()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	var changed []string
	for k, v := range body {
		if k == "github_token" || k == "webhook_secret" {
			// Segredos com endpoint dedicado (propagam em runtime).
			continue
		}
		if strings.HasSuffix(k, "_set") && secretSettingKeys[strings.TrimSuffix(k, "_set")] {
			// Flags sintéticas do GET ("alert_smtp_password_set": "true"): a UI
			// devolve o objeto inteiro no PUT — sem este skip virariam LINHA real
			// na tabela e entrada falsa na auditoria.
			continue
		}
		if _, err := tx.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v); err != nil {
			tx.Rollback()
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		// Auditoria só de MUDANÇA real; chave ausente == "" (todos os leitores
		// tratam assim), então gravar "" onde não havia nada não é evento.
		if old[k] != v {
			changed = append(changed, settingChange(k, old[k], v))
		}
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if len(changed) > 0 {
		sort.Strings(changed) // ordem determinística (map de entrada não tem ordem)
		s.audit(audit.Event{
			Type: "settings.write", Actor: actorFromCtx(r), Action: "update",
			Target: "settings", Outcome: "success", IP: clientIP(r),
			Detail: strings.Join(changed, "; "),
		})
	}
	// broadcast so UI can react
	if s.cfg.Hub != nil {
		s.cfg.Hub.BroadcastWeb("settings.changed", body)
	}
	s.getSettings(w, r)
}

// settingChange formata "chave: de → para" mascarando segredos.
// Valores citados: "" vira aspas vazias no diff, não some.
func settingChange(k, from, to string) string {
	if secretSettingKeys[k] {
		return k + ": (changed)"
	}
	return k + `: "` + from + `" → "` + to + `"`
}

// envLabel — F20: retorna label do ambiente (público, sem auth).
// Reads from settings table; empty if not set.
func (s *server) envLabel(w http.ResponseWriter, r *http.Request) {
	var label string
	_ = s.cfg.DB.QueryRow(`SELECT value FROM settings WHERE key='env_label'`).Scan(&label)
	// H1 — informa ao SPA se há SSO ativo (mostra o botão "Entrar com SSO").
	out := map[string]any{"label": label, "oidcEnabled": s.cfg.OIDC != nil}
	if s.cfg.OIDC != nil {
		out["oidcLoginUrl"] = "/api/auth/oidc/login"
	}
	writeJSON(w, 200, out)
}
