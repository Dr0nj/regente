package sectls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- helpers de geração de certificados (in-memory) ---

func genCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "regente-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := x509.ParseCertificate(der)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return caCert, key, caPEM
}

func genLeaf(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, cn string, server bool) (certPEM, keyPEM []byte) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	if server {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		tmpl.DNSNames = []string{"localhost"}
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	} else {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, _ := x509.MarshalECPrivateKey(key)
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

func writeFile(t *testing.T, dir, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// --- testes ---

// TLS desligado quando não há cert.
func TestServerTLS_Off(t *testing.T) {
	cfg, mtls, err := ServerTLS("", "", "")
	if err != nil || cfg != nil || mtls {
		t.Fatalf("sem cert deveria ser TLS off (nil,false,nil); veio cfg=%v mtls=%v err=%v", cfg, mtls, err)
	}
}

// HTTPS sem mTLS quando há cert/key mas não há client-CA.
func TestServerTLS_HTTPSOnly(t *testing.T) {
	dir := t.TempDir()
	ca, caKey, _ := genCA(t)
	scert, skey := genLeaf(t, ca, caKey, "server", true)
	cfg, mtls, err := ServerTLS(writeFile(t, dir, "s.crt", scert), writeFile(t, dir, "s.key", skey), "")
	if err != nil || cfg == nil || mtls {
		t.Fatalf("esperava HTTPS sem mTLS; veio mtls=%v err=%v", mtls, err)
	}
	if cfg.ClientAuth != tls.NoClientCert {
		t.Fatalf("sem client-CA não deveria exigir cert de cliente")
	}
}

// mTLS e2e: cliente COM cert da CA é aceito; SEM cert é rejeitado.
func TestServerTLS_MutualAuth(t *testing.T) {
	dir := t.TempDir()
	ca, caKey, caPEM := genCA(t)
	scert, skey := genLeaf(t, ca, caKey, "server", true)
	ccert, ckey := genLeaf(t, ca, caKey, "agent-1", false)

	cfg, mtls, err := ServerTLS(
		writeFile(t, dir, "s.crt", scert), writeFile(t, dir, "s.key", skey),
		writeFile(t, dir, "ca.crt", caPEM),
	)
	if err != nil || !mtls {
		t.Fatalf("esperava mTLS ativo; veio mtls=%v err=%v", mtls, err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	srv.TLS = cfg
	srv.StartTLS()
	defer srv.Close()

	// Pool de confiança no cliente (confia na CA p/ validar o cert do servidor).
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(caPEM)

	// 1) SEM cert de cliente → rejeitado (handshake falha).
	noCert := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}}
	if resp, err := noCert.Get(srv.URL); err == nil {
		resp.Body.Close()
		t.Fatal("cliente SEM certificado deveria ser rejeitado pelo mTLS")
	}

	// 2) COM cert assinado pela CA → aceito.
	clientCert, err := tls.X509KeyPair(ccert, ckey)
	if err != nil {
		t.Fatal(err)
	}
	withCert := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs:      roots,
		Certificates: []tls.Certificate{clientCert},
	}}}
	resp, err := withCert.Get(srv.URL)
	if err != nil {
		t.Fatalf("cliente COM certificado válido deveria ser aceito: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("resposta inesperada: %q", body)
	}
}

// CA inválida → erro claro.
func TestServerTLS_BadClientCA(t *testing.T) {
	dir := t.TempDir()
	ca, caKey, _ := genCA(t)
	scert, skey := genLeaf(t, ca, caKey, "server", true)
	_, _, err := ServerTLS(
		writeFile(t, dir, "s.crt", scert), writeFile(t, dir, "s.key", skey),
		writeFile(t, dir, "bad-ca.crt", []byte("not a pem")),
	)
	if err == nil {
		t.Fatal("client-CA inválida deveria retornar erro")
	}
}
