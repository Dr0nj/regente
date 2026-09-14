package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
)

func TestOIDCVerification(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "test"))
	if err != nil {
		t.Fatal(err)
	}
	var base string
	var claims map[string]any
	verifier := oauth2.GenerateVerifier()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token", "jwks_uri": base + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			if r.FormValue("code_verifier") != verifier {
				http.Error(w, "invalid PKCE", 400)
				return
			}
			payload, _ := json.Marshal(claims)
			signed, e := signer.Sign(payload)
			if e != nil {
				t.Error(e)
				return
			}
			raw, e := signed.CompactSerialize()
			if e != nil {
				t.Error(e)
				return
			}
			if claims["tamper"] == true {
				parts := strings.Split(raw, ".")
				if parts[2][0] == 'A' {
					parts[2] = "B" + parts[2][1:]
				} else {
					parts[2] = "A" + parts[2][1:]
				}
				raw = strings.Join(parts, ".")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "access", "token_type": "Bearer", "id_token": raw})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	base = srv.URL
	p, err := Discover(context.Background(), Config{Issuer: base, ClientID: "client", RedirectURL: base + "/callback"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(p.AuthCodeURL("state", "nonce", verifier))
	q := u.Query()
	if q.Get("code_challenge") != oauth2.S256ChallengeFromVerifier(verifier) || q.Get("code_challenge_method") != "S256" || q.Get("nonce") != "nonce" || q.Get("state") != "state" || q.Get("prompt") != "login" {
		t.Fatal("autorização sem state/nonce/PKCE/reauth")
	}
	for _, test := range []struct {
		name, key string
		value     any
		want      bool
	}{
		{"valid", "sub", "subject", true}, {"issuer", "iss", "https://wrong", false}, {"audience", "aud", "wrong", false}, {"expired", "exp", time.Now().Add(-time.Hour).Unix(), false}, {"nonce", "nonce", "wrong", false}, {"subject", "sub", "", false}, {"access_hash", "at_hash", "wrong", false},
		{"signature", "tamper", true, false}, {"authorized_party", "azp", "another-client", false}, {"multiple_audiences_no_party", "aud", []string{"client", "another"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims = map[string]any{"iss": base, "sub": "subject", "aud": "client", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": "nonce", "preferred_username": "admin"}
			claims[test.key] = test.value
			id, e := p.Exchange(context.Background(), "code", "nonce", verifier)
			if (e == nil) != test.want {
				t.Fatalf("validação inesperada: %v", e)
			}
			if test.want && (id.Subject != "subject" || id.Issuer != base) {
				t.Fatal("vínculo incorreto")
			}
		})
	}
	if _, err = p.Exchange(context.Background(), "code", "nonce", "invalid"); err == nil {
		t.Fatal("PKCE inválido aceito")
	}
}
