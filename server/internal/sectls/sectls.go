// Package sectls valida TLS e identidades de máquina sem bloquear navegador/probes.
package sectls

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"os"
)

func roots(path string) (*x509.CertPool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("client CA unavailable")
	}
	p := x509.NewCertPool()
	if !p.AppendCertsFromPEM(b) {
		return nil, errors.New("invalid client CA")
	}
	return p, nil
}
func config(certFile, keyFile, caFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, errors.New("TLS certificate unavailable")
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12, SessionTicketsDisabled: true}
	if caFile != "" {
		p, err := roots(caFile)
		if err != nil {
			return nil, err
		}
		cfg.ClientCAs = p
		cfg.ClientAuth = tls.VerifyClientCertIfGiven
	}
	return cfg, nil
}

// A CA opt-in protege rotas de máquina na API; TLS permite web/probes sem certificado.
// Arquivos são recarregados por handshake; sessão TLS não reaproveita confiança antiga.
func ServerTLS(certFile, keyFile, caFile string) (*tls.Config, bool, error) {
	if certFile == "" {
		if keyFile != "" || caFile != "" {
			return nil, false, errors.New("incomplete TLS configuration")
		}
		return nil, false, nil
	}
	cfg, err := config(certFile, keyFile, caFile)
	if err != nil {
		return nil, false, err
	}
	cfg.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) { return config(certFile, keyFile, caFile) }
	return cfg, caFile != "", nil
}
func Fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// Revalida a cadeia na CA ATUAL e horário atual, inclusive em keep-alive/streams.
func VerifyClient(certs []*x509.Certificate, caFile string) bool {
	if caFile == "" || len(certs) == 0 {
		return false
	}
	p, err := roots(caFile)
	if err != nil {
		return false
	}
	intermediates := x509.NewCertPool()
	for _, c := range certs[1:] {
		intermediates.AddCert(c)
	}
	_, err = certs[0].Verify(x509.VerifyOptions{Roots: p, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	return err == nil
}
