// Package oidc — H1: SSO via OpenID Connect (Authorization Code flow).
//
// Implementado só com a stdlib (sem dependência nova): discovery do issuer,
// troca de code por access_token e leitura do userinfo. O resultado é uma
// Identity que o handler mapeia para um usuário local via auth.LoginFederated.
//
// Está 100% gated por -auth-mode=oidc: sem configuração, o login local
// (admin/senha) segue sendo o único caminho. Validação real exige um IdP
// (Keycloak, Cognito, Okta, Entra ID, Google) — ver README, seção H1.
//
// Hardening pendente para produção (documentado, não bloqueia o stub): PKCE,
// verificação da assinatura do id_token via JWKS (hoje confiamos no userinfo
// sobre TLS), e nonce. O state é validado contra cookie.
package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config — parâmetros do IdP (vêm de flags/env).
type Config struct {
	Issuer       string // ex.: https://login.example.com/realms/regente
	ClientID     string
	ClientSecret string
	RedirectURL  string   // ex.: https://regente.example.com/api/auth/oidc/callback
	Scopes       []string // default: openid email profile
	DefaultRole  string   // role no 1º acesso (default viewer)
}

// Enabled indica se o SSO está configurado.
func (c Config) Enabled() bool {
	return c.Issuer != "" && c.ClientID != "" && c.RedirectURL != ""
}

type discovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
}

// Provider é o IdP resolvido (após discovery).
type Provider struct {
	cfg  Config
	disc discovery
	http *http.Client
}

// Identity — quem o IdP autenticou.
type Identity struct {
	Subject           string `json:"sub"`
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
}

// Username escolhe o identificador local: preferred_username > email > sub.
func (id Identity) Username() string {
	switch {
	case id.PreferredUsername != "":
		return id.PreferredUsername
	case id.Email != "":
		return id.Email
	default:
		return id.Subject
	}
}

// Discover busca o /.well-known/openid-configuration do issuer.
func Discover(ctx context.Context, cfg Config) (*Provider, error) {
	if !cfg.Enabled() {
		return nil, fmt.Errorf("oidc: configuração incompleta (issuer/client-id/redirect-url)")
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "email", "profile"}
	}
	wellKnown := strings.TrimRight(cfg.Issuer, "/") + "/.well-known/openid-configuration"
	hc := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, wellKnown, nil)
	res, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc discovery: status %d", res.StatusCode)
	}
	var d discovery
	if err := json.NewDecoder(res.Body).Decode(&d); err != nil {
		return nil, fmt.Errorf("oidc discovery decode: %w", err)
	}
	if d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" || d.UserinfoEndpoint == "" {
		return nil, fmt.Errorf("oidc discovery: endpoints ausentes")
	}
	return &Provider{cfg: cfg, disc: d, http: hc}, nil
}

func (p *Provider) Config() Config { return p.cfg }

// AuthCodeURL monta a URL de redirect para o IdP.
func (p *Provider) AuthCodeURL(state string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", p.cfg.ClientID)
	q.Set("redirect_uri", p.cfg.RedirectURL)
	q.Set("scope", strings.Join(p.cfg.Scopes, " "))
	q.Set("state", state)
	return p.disc.AuthorizationEndpoint + "?" + q.Encode()
}

// Exchange troca o code por access_token e lê o userinfo → Identity.
func (p *Provider) Exchange(ctx context.Context, code string) (*Identity, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", p.cfg.RedirectURL)
	form.Set("client_id", p.cfg.ClientID)
	if p.cfg.ClientSecret != "" {
		form.Set("client_secret", p.cfg.ClientSecret)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.disc.TokenEndpoint, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc token: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc token: status %d: %s", res.StatusCode, string(body))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("oidc token decode: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("oidc token: access_token vazio")
	}
	return p.userinfo(ctx, tok.AccessToken)
}

func (p *Provider) userinfo(ctx context.Context, accessToken string) (*Identity, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.disc.UserinfoEndpoint, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	res, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc userinfo: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc userinfo: status %d", res.StatusCode)
	}
	var id Identity
	if err := json.NewDecoder(res.Body).Decode(&id); err != nil {
		return nil, fmt.Errorf("oidc userinfo decode: %w", err)
	}
	if id.Username() == "" {
		return nil, fmt.Errorf("oidc userinfo: sem identificador (sub/email/preferred_username)")
	}
	return &id, nil
}
