// Package api — F13 GitOps endpoints + write helpers.
//
// Endpoints (admin-only):
//
//	GET  /api/git/status   → {configured,branch,sha,...}
//	POST /api/git/sync     → fetch+reset; reload defs; broadcast.
//	POST /api/git/webhook  → GitHub webhook (HMAC-validated).
package api

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/storage"
)

// actorFromCtx pega username do contexto auth (fallback "regente").
func actorFromCtx(r *http.Request) string {
	if u, ok := auth.FromContext(r.Context()); ok && u != nil && u.Username != "" {
		return u.Username
	}
	return "regente"
}

// applyWrite encapsula a decisão direct-vs-PR para writes de definição/folder.
//
// Em modo "direct": só chama mutate (escreve em disco; commit local opcional via FileStore).
// Em modo "pr-required": orquestra branch+commit+push+PR via PRWriter.
//
// Retorna (PRResult|nil, err). Em direct mode o PRResult tem Mode="direct".
func (s *server) applyWrite(actor, branchSuffix, commitMsg, prTitle, prBody string, mutate func() error) (*storage.PRResult, error) {
	if s.cfg.WriteMode != storage.WriteModePRRequired || s.cfg.PRWriter == nil {
		// direct: comportamento legado + push se gitOps ativo.
		if err := mutate(); err != nil {
			return nil, err
		}
		// Se git configurado em modo direct, commit+push direto pro main.
		if s.cfg.Git != nil {
			if err := s.cfg.Git.DirectPush(commitMsg, actor); err != nil {
				// log mas não bloqueia — disco já foi salvo.
				log.Printf("[git] direct push failed (disk saved): %v", err)
			}
		}
		return &storage.PRResult{Mode: string(storage.WriteModeDirect)}, nil
	}
	return s.cfg.PRWriter.Apply(actor, branchSuffix, commitMsg, prTitle, prBody, mutate)
}

// === Git endpoints ===

// directPush is a fire-and-forget push used by folder ops that don't go through applyWrite.
func (s *server) directPush(commitMsg, actor string) {
	if s.cfg.Git != nil && s.cfg.WriteMode == storage.WriteModeDirect {
		if err := s.cfg.Git.DirectPush(commitMsg, actor); err != nil {
			log.Printf("[git] direct push failed (disk saved): %v", err)
		}
	}
}

func (s *server) gitStatus(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Git == nil {
		writeJSON(w, 200, map[string]any{"configured": false})
		return
	}
	st := s.cfg.Git.Status()
	// Enriquece com estado de auth para a UI saber se push/PR estão habilitados.
	writeJSON(w, 200, map[string]any{
		"configured": st.Configured, "source": st.Source, "webUrl": st.WebURL,
		"branch": st.Branch, "sha": st.SHA, "shortSha": st.ShortSHA,
		"lastSync": st.LastSync, "drift": st.Drift, "remoteSha": st.RemoteSHA,
		"driftErr": st.DriftErr,
		"hasToken": s.cfg.Git.HasAuth(), "authMode": s.authMode(),
		"webhookConfigured": s.cfg.GitHub != nil && s.cfg.GitHub.HasWebhookSecret(),
	})
}

// setWebhookSecret — POST /api/git/webhook-secret {secret}. Admin-only. Configura
// o secret HMAC do webhook GitHub em runtime e persiste em settings (P13).
// secret vazio = desabilita a validação (webhook passa a recusar tudo).
func (s *server) setWebhookSecret(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var body struct {
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	secret := strings.TrimSpace(body.Secret)
	if s.cfg.GitHub != nil {
		s.cfg.GitHub.SetWebhookSecret(secret)
	}
	if _, err := s.cfg.DB.Exec(
		`INSERT INTO settings(key,value) VALUES('webhook_secret',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, secret,
	); err != nil {
		writeJSON(w, 500, map[string]string{"error": "persist falhou: " + err.Error()})
		return
	}
	s.gitStatus(w, r)
}

// authMode reporta a origem da credencial de push/PR (para a UI).
func (s *server) authMode() string {
	if s.cfg.GitHub != nil && s.cfg.GitHub.HasToken() {
		return "token"
	}
	if s.cfg.Git != nil && s.cfg.Git.HasAuth() {
		return "token"
	}
	return "none"
}

// setGitToken — POST /api/git/token {token}. Admin-only. Injeta o PAT em
// runtime (gitOps + github client + session manager), persiste em settings
// (sobrevive a restart, fora do .git/config) e revalida o clone. O token nunca
// é devolvido pela API.
func (s *server) setGitToken(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
		return
	}
	tok := strings.TrimSpace(body.Token)
	if tok == "" {
		writeJSON(w, 400, map[string]string{"error": "token vazio"})
		return
	}
	s.applyToken(tok)
	if _, err := s.cfg.DB.Exec(
		`INSERT INTO settings(key,value) VALUES('github_token',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, tok,
	); err != nil {
		writeJSON(w, 500, map[string]string{"error": "persist falhou: " + err.Error()})
		return
	}
	// Revalida: fetch+reset com o novo header de auth (também reescreve remote limpo).
	if s.cfg.Git != nil {
		if err := s.cfg.Git.EnsureClone(); err != nil {
			writeJSON(w, 502, map[string]string{"error": "token salvo, mas validação do clone falhou: " + err.Error()})
			return
		}
	}
	s.gitStatus(w, r)
}

// clearGitToken — DELETE /api/git/token. Admin-only. Remove o PAT de runtime e
// do settings. Volta para modo read-only (push/PR desabilitados).
func (s *server) clearGitToken(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	s.applyToken("")
	if _, err := s.cfg.DB.Exec(`DELETE FROM settings WHERE key='github_token'`); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	s.gitStatus(w, r)
}

// applyToken propaga o PAT para todos os componentes que falam com o remote.
func (s *server) applyToken(tok string) {
	if s.cfg.Git != nil {
		s.cfg.Git.InjectToken(tok)
	}
	if s.cfg.GitHub != nil {
		s.cfg.GitHub.SetToken(tok)
	}
	if s.cfg.Sessions != nil {
		s.cfg.Sessions.SetToken(tok)
	}
}

// cleanupDB — POST /api/git/cleanup-db. Admin-only. Remove a SQLite DB que runs
// antigas commitaram no repo (origin) e versiona o .gitignore. Faz push (precisa
// de token). Resolve o bug do WAL: sem a DB trackeada, o reset --hard da daily
// nunca tropeça no WAL travado.
func (s *server) cleanupDB(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if s.cfg.Git == nil {
		http.Error(w, "git not configured", http.StatusBadRequest)
		return
	}
	actor := actorFromCtx(r)
	changed, err := s.cfg.Git.CleanupDBArtifacts(actor)
	if err != nil {
		writeJSON(w, 502, map[string]any{"changed": changed, "error": err.Error()})
		return
	}
	if changed {
		s.cfg.Scheduler.ReloadDefs()
		st := s.cfg.Git.Status()
		s.cfg.Hub.BroadcastWeb("definition.changed", map[string]string{"reason": "git-cleanup", "sha": st.ShortSHA})
	}
	writeJSON(w, 200, map[string]any{"changed": changed, "status": "ok"})
}

func (s *server) gitSync(w http.ResponseWriter, r *http.Request) {
	// admin-only
	u, ok := auth.FromContext(r.Context())
	if !ok || u == nil || !u.Role.CanAdmin() {
		http.Error(w, "forbidden: admin only", http.StatusForbidden)
		return
	}
	if s.cfg.Git == nil {
		http.Error(w, "git not configured", http.StatusBadRequest)
		return
	}
	if err := s.cfg.Git.SyncFromRemote(); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.cfg.Scheduler.ReloadDefs()
	s.cfg.Hub.BroadcastWeb("definition.changed", map[string]string{"reason": "git-sync"})
	s.cfg.Hub.BroadcastWeb("folder.changed", map[string]string{"reason": "git-sync"})
	writeJSON(w, 200, s.cfg.Git.Status())
}

// F13.4 — drift check endpoint (lightweight fetch+compare).
func (s *server) gitDrift(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Git == nil {
		writeJSON(w, 200, map[string]any{"drift": false, "configured": false})
		return
	}
	drifted, err := s.cfg.Git.CheckDrift()
	if err != nil {
		writeJSON(w, 200, map[string]any{"drift": false, "error": err.Error()})
		return
	}
	st := s.cfg.Git.Status()
	if drifted {
		s.cfg.Hub.BroadcastWeb("git.drift", map[string]any{
			"drift":     true,
			"localSha":  st.SHA,
			"remoteSha": st.RemoteSHA,
		})
	}
	writeJSON(w, 200, st)
}

// gitWebhook recebe payloads do GitHub e dispara fetch+reset quando relevante.
func (s *server) gitWebhook(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Git == nil || s.cfg.GitHub == nil {
		http.Error(w, "git/webhook not configured", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	sig := r.Header.Get("X-Hub-Signature-256")
	if !s.cfg.GitHub.VerifyWebhookHMAC(sig, body) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	event := r.Header.Get("X-GitHub-Event")
	// We care about: push (to main), pull_request (closed+merged).
	relevant := false
	switch event {
	case "push":
		var payload struct {
			Ref string `json:"ref"`
		}
		if err := json.Unmarshal(body, &payload); err == nil {
			// ref like "refs/heads/main"
			if strings.HasSuffix(payload.Ref, "/"+s.cfg.Git.Branch()) {
				relevant = true
			}
		}
	case "pull_request":
		var payload struct {
			Action      string `json:"action"`
			PullRequest struct {
				Merged bool `json:"merged"`
				Base   struct {
					Ref string `json:"ref"`
				} `json:"base"`
			} `json:"pull_request"`
		}
		if err := json.Unmarshal(body, &payload); err == nil {
			if payload.Action == "closed" && payload.PullRequest.Merged && payload.PullRequest.Base.Ref == s.cfg.Git.Branch() {
				relevant = true
			}
		}
	case "ping":
		writeJSON(w, 200, map[string]string{"pong": "regente"})
		return
	}
	if !relevant {
		writeJSON(w, 200, map[string]string{"status": "ignored", "event": event})
		return
	}
	if err := s.cfg.Git.SyncFromRemote(); err != nil {
		log.Printf("[webhook] sync failed: %v", err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.cfg.Scheduler.ReloadDefs()
	st := s.cfg.Git.Status()
	s.cfg.Hub.BroadcastWeb("definition.changed", map[string]string{"reason": "git-webhook", "sha": st.ShortSHA})
	s.cfg.Hub.BroadcastWeb("folder.changed", map[string]string{"reason": "git-webhook", "sha": st.ShortSHA})
	writeJSON(w, 200, map[string]any{"status": "synced", "git": st})
}

// StartGitPolling roda fetch+reset em loop (intervalo > 0).
// Also checks drift between syncs and broadcasts git.drift if remote moved.
// Cancelável via context.
func StartGitPolling(ctx context.Context, git *storage.GitOps, intervalSec int, onSync func(), hub interface{ BroadcastWeb(string, any) }) {
	if git == nil || intervalSec <= 0 {
		return
	}
	go func() {
		ticker := newPollTicker(intervalSec)
		defer ticker.Stop()
		// Drift check runs more often: half the sync interval (min 15s).
		driftInterval := intervalSec / 2
		if driftInterval < 15 {
			driftInterval = 15
		}
		driftTicker := newPollTicker(driftInterval)
		defer driftTicker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := git.SyncFromRemote(); err != nil {
					log.Printf("[git-poll] sync failed: %v", err)
					continue
				}
				if onSync != nil {
					onSync()
				}
			case <-driftTicker.C:
				drifted, err := git.CheckDrift()
				if err != nil {
					log.Printf("[git-poll] drift check failed: %v", err)
					continue
				}
				if drifted && hub != nil {
					st := git.Status()
					hub.BroadcastWeb("git.drift", map[string]any{
						"drift":     true,
						"localSha":  st.SHA,
						"remoteSha": st.RemoteSHA,
					})
				}
			}
		}
	}()
}
