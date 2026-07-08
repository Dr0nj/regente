package quickaction

import (
	"strings"
	"testing"
	"time"
)

// REGRA: o token só vale intacto — mudar payload OU assinatura invalida.
func TestSignVerify_RoundtripAndTamper(t *testing.T) {
	secret, _ := NewSecret()
	tok, err := Sign(secret, "job-2026-07-07", "rerun", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	id, action, err := Verify(secret, tok)
	if err != nil || id != "job-2026-07-07" || action != "rerun" {
		t.Fatalf("roundtrip falhou: %v %s %s", err, id, action)
	}

	// assinatura de outro segredo não cola
	otherSecret, _ := NewSecret()
	if _, _, err := Verify(otherSecret, tok); err == nil {
		t.Fatal("token de outro segredo deveria falhar")
	}
	// payload adulterado (troca a ação no primeiro segmento) não cola
	parts := strings.Split(tok, ".")
	forged, _ := Sign(secret, "job-2026-07-07", "set-ok", time.Now().Add(time.Hour))
	forgedPayload := strings.Split(forged, ".")[0]
	if _, _, err := Verify(secret, forgedPayload+"."+parts[1]); err == nil {
		t.Fatal("payload trocado com assinatura antiga deveria falhar")
	}
}

// REGRA: expirado = ErrExpired (o handler traduz pra 410, não 403).
func TestVerify_Expired(t *testing.T) {
	secret, _ := NewSecret()
	tok, _ := Sign(secret, "i", "set-ok", time.Now().Add(-time.Minute))
	if _, _, err := Verify(secret, tok); err != ErrExpired {
		t.Fatalf("esperava ErrExpired, veio %v", err)
	}
}

// REGRA: só ações da allowlist são assináveis — cancel (destrutivo) fica fora.
func TestSign_ActionAllowlist(t *testing.T) {
	secret, _ := NewSecret()
	if _, err := Sign(secret, "i", "cancel", time.Now().Add(time.Hour)); err == nil {
		t.Fatal("cancel não pode virar quick-action")
	}
}
