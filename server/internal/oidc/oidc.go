// Package oidc implementa Authorization Code com validação OIDC e PKCE S256.
package oidc

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Config struct {
	Issuer, ClientID, ClientSecret, RedirectURL string
	Scopes                                      []string
	DefaultRole                                 string
}

func (c Config) Enabled() bool { return c.Issuer != "" && c.ClientID != "" && c.RedirectURL != "" }

type Provider struct {
	cfg      Config
	oauth    oauth2.Config
	verifier *coreoidc.IDTokenVerifier
	http     *http.Client
}

type Identity struct {
	Issuer            string
	Subject           string `json:"sub"`
	Email             string `json:"email"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
	ExpiresAt         time.Time
}

func (id Identity) Username() string {
	if id.PreferredUsername != "" {
		return id.PreferredUsername
	}
	if id.Name != "" {
		return id.Name
	}
	return "SSO user"
}

// HTTP local explícito permite desenvolvimento; endpoints remotos exigem TLS.
func validURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && u.User == nil && u.Fragment == "" &&
		(u.Scheme == "https" || u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))
}

func Discover(ctx context.Context, cfg Config) (*Provider, error) {
	if !cfg.Enabled() || !validURL(cfg.Issuer) || !validURL(cfg.RedirectURL) {
		return nil, errors.New("OIDC requires complete HTTPS configuration (HTTP loopback is allowed)")
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{coreoidc.ScopeOpenID, "email", "profile"}
	}
	hc := &http.Client{Timeout: 10 * time.Second}
	provider, err := coreoidc.NewProvider(coreoidc.ClientContext(ctx, hc), cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery: %w", err)
	}
	ep := provider.Endpoint()
	if !validURL(ep.AuthURL) || !validURL(ep.TokenURL) {
		return nil, errors.New("OIDC endpoints require HTTPS")
	}
	var meta struct {
		JWKS string `json:"jwks_uri"`
	}
	if err := provider.Claims(&meta); err != nil || !validURL(meta.JWKS) {
		return nil, errors.New("OIDC JWKS endpoint requires HTTPS")
	}
	return &Provider{cfg: cfg, http: hc, oauth: oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURL: cfg.RedirectURL, Scopes: cfg.Scopes, Endpoint: ep}, verifier: provider.Verifier(&coreoidc.Config{ClientID: cfg.ClientID})}, nil
}

func (p *Provider) Config() Config { return p.cfg }

func (p *Provider) AuthCodeURL(state, nonce, verifier string) string {
	return p.oauth.AuthCodeURL(state, coreoidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("prompt", "login"))
}

func (p *Provider) Exchange(ctx context.Context, code, nonce, verifier string) (*Identity, error) {
	tok, err := p.oauth.Exchange(coreoidc.ClientContext(ctx, p.http), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, errors.New("OIDC code exchange failed")
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok {
		return nil, errors.New("OIDC ID token is missing")
	}
	idToken, err := p.verifier.Verify(coreoidc.ClientContext(ctx, p.http), raw)
	if err != nil {
		return nil, errors.New("OIDC ID token verification failed")
	}
	if nonce == "" || subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 || idToken.Subject == "" {
		return nil, errors.New("OIDC nonce or subject is invalid")
	}
	if idToken.AccessTokenHash != "" {
		if err := idToken.VerifyAccessToken(tok.AccessToken); err != nil {
			return nil, errors.New("OIDC access token binding is invalid")
		}
	}
	var id Identity
	var party struct {
		AuthorizedParty string `json:"azp"`
	}
	if err := idToken.Claims(&party); err != nil || (len(idToken.Audience) > 1 || party.AuthorizedParty != "") && party.AuthorizedParty != p.cfg.ClientID {
		return nil, errors.New("OIDC authorized party is invalid")
	}
	if err := idToken.Claims(&id); err != nil {
		return nil, errors.New("OIDC claims are invalid")
	}
	id.Issuer, id.Subject, id.ExpiresAt = idToken.Issuer, idToken.Subject, idToken.Expiry
	return &id, nil
}
