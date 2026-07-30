package api

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

// O caso que motivou o fix (GHSA-3fxj-6jh8-hvhx): cliente falando DIRETO com o
// server manda X-Forwarded-For e forja a origem no audit/SIEM. Com peer
// não-confiável, o header não pode valer nada.
func TestClientIP_IgnoraHeaderDePeerNaoConfiavel(t *testing.T) {
	trusted, err := ParseTrustedProxies("")
	if err != nil {
		t.Fatalf("ParseTrustedProxies: %v", err)
	}
	for _, header := range []string{"X-Forwarded-For", "X-Real-IP"} {
		r := httptest.NewRequest("POST", "/api/auth/login", nil)
		r.RemoteAddr = "203.0.113.9:44321" // internet, não é proxy nosso
		r.Header.Set(header, "1.2.3.4")
		if got := clientIPAfterMW(r, trusted); got != "203.0.113.9" {
			t.Fatalf("%s de peer não-confiável foi respeitado: got %q, want o peer real", header, got)
		}
	}
}

// Peer confiável (o nginx em loopback do deploy/vps): aí o header é do proxy e vale.
func TestClientIP_HonraProxyConfiavel(t *testing.T) {
	trusted, err := ParseTrustedProxies("")
	if err != nil {
		t.Fatalf("ParseTrustedProxies: %v", err)
	}
	r := httptest.NewRequest("GET", "/api/definitions", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "198.51.100.7")
	if got := clientIPAfterMW(r, trusted); got != "198.51.100.7" {
		t.Fatalf("got %q, want 198.51.100.7", got)
	}
}

// O segundo defeito do chi: pegar a entrada MAIS À ESQUERDA, que é a que o
// cliente escreve. Aqui o cliente prepende um IP falso e o nginx acrescenta o
// real à direita — tem que ganhar o da direita.
func TestClientIP_XFFVarreDaDireitaParaEsquerda(t *testing.T) {
	trusted, err := ParseTrustedProxies("10.0.0.0/8")
	if err != nil {
		t.Fatalf("ParseTrustedProxies: %v", err)
	}
	r := httptest.NewRequest("GET", "/api/instances", nil)
	r.RemoteAddr = "10.0.0.5:9999" // proxy confiável
	// "1.2.3.4" é forjado pelo cliente; "198.51.100.7" foi acrescentado pelo
	// proxy da borda; "10.0.0.9" é salto interno confiável.
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 198.51.100.7, 10.0.0.9")
	if got := clientIPAfterMW(r, trusted); got != "198.51.100.7" {
		t.Fatalf("got %q, want 198.51.100.7 (o primeiro salto não-confiável da direita)", got)
	}
}

// Sem header nenhum, o peer TCP continua sendo a resposta.
func TestClientIP_FallbackNoPeer(t *testing.T) {
	trusted, _ := ParseTrustedProxies("")
	r := httptest.NewRequest("GET", "/health", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	if got := clientIPAfterMW(r, trusted); got != "127.0.0.1" {
		t.Fatalf("got %q, want 127.0.0.1", got)
	}
}

func TestParseTrustedProxies(t *testing.T) {
	if _, err := ParseTrustedProxies("nao-e-cidr"); err == nil {
		t.Fatal("entrada inválida devia falhar o boot, não virar default silencioso")
	}
	got, err := ParseTrustedProxies("10.0.0.0/8, 192.168.1.7")
	if err != nil {
		t.Fatalf("ParseTrustedProxies: %v", err)
	}
	if len(got) != 2 || got[0].String() != "10.0.0.0/8" || got[1].String() != "192.168.1.7/32" {
		t.Fatalf("got %v", got)
	}
}

// clientIPAfterMW roda o request pelo middleware e devolve o que o clientIP()
// (o consumidor real, lá no audit) enxerga do outro lado. O teste exercita a
// cadeia inteira de propósito: o bug original só existia na junção middleware
// → RemoteAddr → clientIP, não em nenhuma das peças isoladas.
func clientIPAfterMW(r *http.Request, trusted []netip.Prefix) string {
	var got string
	realIP(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		got = clientIP(req)
	})).ServeHTTP(httptest.NewRecorder(), r)
	return got
}
