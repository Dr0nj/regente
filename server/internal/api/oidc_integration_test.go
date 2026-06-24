package api

import (
	"context"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/oidc"
)

// e2e REAL do SSO/OIDC contra um IdP de verdade (Keycloak). O fluxo unitário é
// coberto por handlers + oidc package; ESTE prova o Authorization Code ponta-a-
// ponta: /api/auth/oidc/login → IdP (discovery + authorize) → login form →
// callback (troca code→token + userinfo) → app recebe #token, e o usuário é
// PROVISIONADO no DB local (LoginFederated). Fecha o gap "SSO e2e com IdP real".
//
// Skip por padrão. Para rodar (ver setup do Keycloak no histórico/roadmap):
//
//	REGENTE_TEST_OIDC_ISSUER=http://127.0.0.1:8088/realms/regente \
//	REGENTE_TEST_OIDC_CLIENT_ID=regente-app \
//	REGENTE_TEST_OIDC_CLIENT_SECRET=regente-secret-123 \
//	REGENTE_TEST_OIDC_USER=thiago REGENTE_TEST_OIDC_PASS=senha123 \
//	    go test ./internal/api -run TestIntegrationOIDC -v
func TestIntegrationOIDC_AuthCodeFlow(t *testing.T) {
	issuer := os.Getenv("REGENTE_TEST_OIDC_ISSUER")
	clientID := os.Getenv("REGENTE_TEST_OIDC_CLIENT_ID")
	secret := os.Getenv("REGENTE_TEST_OIDC_CLIENT_SECRET")
	user := os.Getenv("REGENTE_TEST_OIDC_USER")
	pass := os.Getenv("REGENTE_TEST_OIDC_PASS")
	if issuer == "" || clientID == "" || user == "" || pass == "" {
		t.Skip("defina REGENTE_TEST_OIDC_ISSUER/CLIENT_ID/CLIENT_SECRET/USER/PASS para o e2e de SSO")
	}

	// 1) Listener em porta fixa para conhecer a redirect_uri ANTES de montar o router.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + ln.Addr().String()
	redirectURL := base + "/api/auth/oidc/callback"

	// 2) Discovery real contra o Keycloak.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	prov, err := oidc.Discover(ctx, oidc.Config{
		Issuer: issuer, ClientID: clientID, ClientSecret: secret,
		RedirectURL: redirectURL, DefaultRole: "admin",
	})
	if err != nil {
		t.Fatalf("discovery real falhou: %v", err)
	}

	// 3) Router real com OIDC ligado + AppURL = base (pra ler o #token no final).
	d := newTestDB(t)
	defer d.Close()
	router := NewRouter(Config{DB: d, Hub: hub.New(), OIDC: prov, AppURL: base})
	srv := &http.Server{Handler: router}
	go srv.Serve(ln)
	defer srv.Close()

	// 4) Cliente com cookie jar (carrega o state do regente E as sessões do KC),
	//    SEM auto-redirect (seguimos manualmente para inspecionar cada hop).
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	// hop A: /oidc/login → 302 para o authorize do Keycloak (seta cookie de state).
	resp := mustDo(t, c, req(t, "GET", base+"/api/auth/oidc/login", nil, ""))
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("oidc/login esperava 302, veio %d", resp.StatusCode)
	}
	authorizeURL := resp.Header.Get("Location")
	if !strings.Contains(authorizeURL, "/protocol/openid-connect/auth") {
		t.Fatalf("Location do login não aponta ao authorize do IdP: %s", authorizeURL)
	}

	// hop B: GET authorize → página de login HTML do Keycloak.
	resp = mustDo(t, c, req(t, "GET", authorizeURL, nil, ""))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("página de login do IdP esperava 200, veio %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	m := regexp.MustCompile(`action="([^"]+)"`).FindSubmatch(body)
	if m == nil {
		t.Fatalf("não achei o form de login na página do Keycloak")
	}
	formAction := html.UnescapeString(string(m[1]))

	// hop C: POST credenciais no form → 302 de volta ao callback com ?code&state.
	form := url.Values{"username": {user}, "password": {pass}, "credentialId": {""}}
	post := req(t, "POST", formAction, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
	resp = mustDo(t, c, post)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("POST de login esperava 302 (sucesso), veio %d — credenciais? config do client?", resp.StatusCode)
	}
	callbackURL := resp.Header.Get("Location")
	if !strings.Contains(callbackURL, "/api/auth/oidc/callback") || !strings.Contains(callbackURL, "code=") {
		t.Fatalf("redirect do IdP não trouxe code pro callback: %s", callbackURL)
	}

	// hop D: GET callback → regente troca code→token, lê userinfo, provisiona e
	//        redireciona pro app com #token=...
	resp = mustDo(t, c, req(t, "GET", callbackURL, nil, ""))
	if resp.StatusCode != http.StatusFound {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("callback esperava 302 pro app, veio %d — %s", resp.StatusCode, string(b))
	}
	dest := resp.Header.Get("Location")
	frag := ""
	if u, err := url.Parse(dest); err == nil {
		frag = u.Fragment
	}
	if !strings.HasPrefix(frag, "token=") || len(frag) < len("token=")+10 {
		t.Fatalf("app não recebeu #token= no final do SSO: %q", dest)
	}
	tokenFromSSO := strings.TrimPrefix(frag, "token=")
	t.Logf("SSO OK — token de sessão emitido (%d chars), usuário '%s' provisionado", len(tokenFromSSO), user)

	// 5) Prova que a sessão emitida pelo SSO É VÁLIDA na API protegida (não é 401).
	//    O user é provisionado como admin (DefaultRole) → 200 em /api/users.
	authed := req(t, "GET", base+"/api/users", nil, "")
	authed.Header.Set("Authorization", "Bearer "+tokenFromSSO)
	resp = mustDo(t, c, authed)
	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatalf("token do SSO não autenticou (401) — sessão federada inválida")
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token do SSO (admin) deveria acessar /api/users (200), veio %d", resp.StatusCode)
	}
}

func req(t *testing.T, method, u string, body io.Reader, ct string) *http.Request {
	t.Helper()
	r, err := http.NewRequest(method, u, body)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	return r
}

func mustDo(t *testing.T, c *http.Client, r *http.Request) *http.Response {
	t.Helper()
	resp, err := c.Do(r)
	if err != nil {
		t.Fatalf("%s %s: %v", r.Method, r.URL, err)
	}
	return resp
}
