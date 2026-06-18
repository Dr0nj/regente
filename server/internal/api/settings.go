// F20 — server settings (key/value persisted in SQLite).
package api

import (
	"encoding/json"
	"net/http"
)

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
		if k == "github_token" {
			if v != "" {
				m["github_token_set"] = "true"
			}
			continue
		}
		if k == "webhook_secret" {
			if v != "" {
				m["webhook_secret_set"] = "true"
			}
			continue
		}
		// Webhooks de alerta são credenciais (quem tem a URL posta no canal):
		// nunca devolver em claro, só sinalizar se estão setados.
		if k == "alert_slack_webhook" || k == "alert_webhook_url" {
			if v != "" {
				m[k+"_set"] = "true"
			}
			continue
		}
		m[k] = v
	}
	writeJSON(w, 200, m)
}

// putSettings merges provided keys into the settings table (admin-only).
func (s *server) putSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	tx, err := s.cfg.DB.Begin()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	for k, v := range body {
		if k == "github_token" || k == "github_token_set" ||
			k == "webhook_secret" || k == "webhook_secret_set" {
			// Segredos: só via endpoints dedicados (propagam em runtime).
			continue
		}
		if _, err := tx.Exec(`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v); err != nil {
			tx.Rollback()
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	// broadcast so UI can react
	if s.cfg.Hub != nil {
		s.cfg.Hub.BroadcastWeb("settings.changed", body)
	}
	s.getSettings(w, r)
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
