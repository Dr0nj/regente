// Package storage — GitHub API client minimalista (F13.2/F13.3).
//
// Sem dependência externa: usa net/http puro. Cobre só o necessário:
//   - Validar HMAC de webhook GitHub (X-Hub-Signature-256).
//   - Criar PR (POST /repos/{owner}/{repo}/pulls).
//   - Buscar PR (GET) — opcional para audit.
package storage

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// GitHubClient encapsula chamadas à API do GitHub.
type GitHubClient struct {
	token         string // PAT
	owner         string
	repo          string
	webhookSecret string
	http          *http.Client
}

// ParseRepoFlag aceita "owner/repo" ou URL completa e retorna (owner, repo).
func ParseRepoFlag(s string) (owner, repo string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", fmt.Errorf("empty repo")
	}
	// strip .git
	s = strings.TrimSuffix(s, ".git")
	// strip protocol
	if idx := strings.Index(s, "github.com"); idx >= 0 {
		s = s[idx+len("github.com"):]
		s = strings.TrimLeft(s, "/:")
	}
	parts := strings.Split(s, "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid repo %q (expected owner/repo)", s)
	}
	return parts[0], parts[1], nil
}

func NewGitHubClient(token, repo, webhookSecret string) (*GitHubClient, error) {
	owner, name, err := ParseRepoFlag(repo)
	if err != nil {
		return nil, err
	}
	return &GitHubClient{
		token:         token,
		owner:         owner,
		repo:          name,
		webhookSecret: webhookSecret,
		http:          &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *GitHubClient) Owner() string { return c.owner }
func (c *GitHubClient) Repo() string  { return c.repo }
func (c *GitHubClient) HasToken() bool {
	if c == nil {
		return false
	}
	return c.token != ""
}

// SetToken atualiza o PAT em runtime (token via UI). Sem persistência aqui —
// o caller decide gravar em settings.
func (c *GitHubClient) SetToken(token string) {
	if c == nil {
		return
	}
	c.token = strings.TrimSpace(token)
}

// SetWebhookSecret atualiza o secret HMAC do webhook em runtime (P13).
func (c *GitHubClient) SetWebhookSecret(secret string) {
	if c == nil {
		return
	}
	c.webhookSecret = strings.TrimSpace(secret)
}

// HasWebhookSecret reporta se o webhook está configurado (validação HMAC ativa).
func (c *GitHubClient) HasWebhookSecret() bool {
	return c != nil && c.webhookSecret != ""
}

// VerifyWebhookHMAC valida X-Hub-Signature-256 usando webhookSecret.
func (c *GitHubClient) VerifyWebhookHMAC(signature string, body []byte) bool {
	if c == nil || c.webhookSecret == "" {
		return false
	}
	const prefix = "sha256="
	if !strings.HasPrefix(signature, prefix) {
		return false
	}
	want, err := hex.DecodeString(signature[len(prefix):])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(c.webhookSecret))
	mac.Write(body)
	got := mac.Sum(nil)
	return hmac.Equal(want, got)
}

// PullRequest representa uma PR criada.
type PullRequest struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	Title   string `json:"title"`
}

// CreatePR cria PR via GitHub API.
func (c *GitHubClient) CreatePR(head, base, title, body string) (*PullRequest, error) {
	if c == nil || c.token == "" {
		return nil, fmt.Errorf("github token not configured")
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls", c.owner, c.repo)
	payload := map[string]string{
		"title": title,
		"head":  head,
		"base":  base,
		"body":  body,
	}
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", url, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github create PR: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var pr PullRequest
	if err := json.Unmarshal(respBody, &pr); err != nil {
		return nil, fmt.Errorf("decode PR: %w", err)
	}
	return &pr, nil
}
