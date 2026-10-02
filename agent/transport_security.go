package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"github.com/gorilla/websocket"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var controlTransport http.RoundTripper = http.DefaultTransport
var controlDialer = websocket.DefaultDialer

type secureControlTransport struct{ cert, key, ca, tokenFile string }

func clientTLS(cert, key, ca string) (*tls.Config, error) {
	if (cert == "") != (key == "") {
		return nil, errors.New("both client certificate and key are required")
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if ca != "" {
		b, err := os.ReadFile(ca)
		if err != nil {
			return nil, errors.New("server CA unavailable")
		}
		p := x509.NewCertPool()
		if !p.AppendCertsFromPEM(b) {
			return nil, errors.New("invalid server CA")
		}
		cfg.RootCAs = p
	}
	if cert != "" {
		c, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			return nil, errors.New("client certificate unavailable")
		}
		cfg.Certificates = []tls.Certificate{c}
	}
	return cfg, nil
}
func (t secureControlTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	cfg, err := clientTLS(t.cert, t.key, t.ca)
	if err != nil {
		return nil, err
	}
	copy := r.Clone(r.Context())
	if t.tokenFile != "" {
		b, err := os.ReadFile(t.tokenFile)
		if err != nil || len(b) > 16384 || len(strings.TrimSpace(string(b))) == 0 {
			return nil, errors.New("machine credential unavailable")
		}
		copy.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(b)))
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = cfg
	transport.Proxy = nil
	// Sem conexões antigas: cada operação revalida os arquivos de CA/cert/credencial.
	transport.DisableKeepAlives = true
	return transport.RoundTrip(copy)
}
func configureControl(server, cert, key, ca, tokenFile string) error {
	u, err := url.Parse(server)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("server URL must have no credentials, query or fragment")
	}
	if cert != "" || key != "" || ca != "" {
		if u.Scheme != "https" && u.Scheme != "wss" {
			return errors.New("TLS files require an HTTPS or WSS server")
		}
	}
	cfg, err := clientTLS(cert, key, ca)
	if err != nil {
		return err
	}
	controlTransport = secureControlTransport{cert, key, ca, tokenFile}
	dialer := *websocket.DefaultDialer
	dialer.Proxy = nil
	dialer.TLSClientConfig = cfg
	if cert != "" {
		dialer.TLSClientConfig.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			c, err := tls.LoadX509KeyPair(cert, key)
			if err != nil {
				return nil, errors.New("client certificate unavailable")
			}
			return &c, nil
		}
	}
	controlDialer = &dialer
	return nil
}
func controlClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: controlTransport, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("control plane redirects are forbidden") }}
}
