// Package sectls — TLS/mTLS opcional do regente-server (segurança enterprise).
//
// Sem `-tls-cert` o servidor segue em HTTP plano (comportamento atual). Com cert/key
// liga HTTPS; com `-tls-client-ca` adicional liga **mTLS** (exige e verifica o
// certificado de cliente contra a CA), autenticando agentes/web por certificado além
// do token. Stdlib só (crypto/tls, crypto/x509).
package sectls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// ServerTLS monta o *tls.Config do servidor a partir dos caminhos de arquivo.
//
//   - certFile == ""            → TLS desligado: retorna (nil, false, nil) → HTTP plano.
//   - cert/key                  → HTTPS (TLS 1.2+).
//   - + clientCAFile            → mTLS: ClientAuth=RequireAndVerifyClientCert contra a CA.
//
// O segundo retorno indica se mTLS ficou ativo (para log).
func ServerTLS(certFile, keyFile, clientCAFile string) (*tls.Config, bool, error) {
	if certFile == "" {
		return nil, false, nil // TLS off
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, false, fmt.Errorf("tls cert/key: %w", err)
	}
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	if clientCAFile == "" {
		return cfg, false, nil // HTTPS sem mTLS
	}
	pem, err := os.ReadFile(clientCAFile)
	if err != nil {
		return nil, false, fmt.Errorf("client CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, false, fmt.Errorf("client CA: nenhum certificado PEM válido em %s", clientCAFile)
	}
	cfg.ClientCAs = pool
	cfg.ClientAuth = tls.RequireAndVerifyClientCert
	return cfg, true, nil
}
