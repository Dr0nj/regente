package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/Dr0nj/regente-agent/security"
	"github.com/Dr0nj/regente-server/internal/businessclock"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/execution"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/runtimeprofile"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/sectls"
	"github.com/Dr0nj/regente-server/internal/storage"
	"github.com/gorilla/websocket"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type i13PKI struct {
	ca, serverCert, serverKey string
	roots                     *x509.CertPool
	cert                      *x509.Certificate
	caKey                     *ecdsa.PrivateKey
}

func i13Write(t *testing.T, path string, b []byte) {
	t.Helper()
	if e := os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func i13Certificate(t *testing.T, p i13PKI, dir, name string, server bool) (string, string, tls.Certificate) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	c := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: name}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	if server {
		c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		c.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	der, e := x509.CreateCertificate(rand.Reader, c, p.cert, &key.PublicKey, p.caKey)
	if e != nil {
		t.Fatal(e)
	}
	raw := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	k, _ := x509.MarshalECPrivateKey(key)
	kr := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: k})
	cp, kp := filepath.Join(dir, name+".crt"), filepath.Join(dir, name+".key")
	i13Write(t, cp, raw)
	i13Write(t, kp, kr)
	pair, e := tls.X509KeyPair(raw, kr)
	if e != nil {
		t.Fatal(e)
	}
	return cp, kp, pair
}
func i13NewPKI(t *testing.T, dir string) i13PKI {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	c := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), IsCA: true, BasicConstraintsValid: true, Subject: pkix.Name{CommonName: "I13 test CA"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageCertSign}
	der, e := x509.CreateCertificate(rand.Reader, c, c, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	c, _ = x509.ParseCertificate(der)
	raw := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	p := i13PKI{ca: filepath.Join(dir, "ca.crt"), cert: c, caKey: key, roots: x509.NewCertPool()}
	p.roots.AppendCertsFromPEM(raw)
	i13Write(t, p.ca, raw)
	p.serverCert, p.serverKey, _ = i13Certificate(t, p, dir, "server", true)
	return p
}
func i13Fingerprint(t *testing.T, c tls.Certificate) string {
	t.Helper()
	leaf, e := x509.ParseCertificate(c.Certificate[0])
	if e != nil {
		t.Fatal(e)
	}
	return sectls.Fingerprint(leaf)
}
func i13Server(t *testing.T, p i13PKI, cfg Config) *httptest.Server {
	t.Helper()
	tlsCfg, _, e := sectls.ServerTLS(p.serverCert, p.serverKey, p.ca)
	if e != nil {
		t.Fatal(e)
	}
	cfg.RuntimePolicy.TLSClientCA = p.ca
	s := httptest.NewUnstartedServer(NewRouter(cfg))
	s.TLS = tlsCfg
	s.StartTLS()
	t.Cleanup(s.Close)
	s.Client().Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: p.roots}, DisableKeepAlives: true}
	return s
}
func TestI13CertificateBindingAndActiveRevocation(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d := machineTestDB(t, dialect)
			dir := t.TempDir()
			p := i13NewPKI(t, dir)
			_, _, old := i13Certificate(t, p, dir, "old", false)
			_, _, next := i13Certificate(t, p, dir, "next", false)
			h := hub.New()
			srv := i13Server(t, p, Config{DB: d, Hub: h, Token: "test-token", RuntimePolicy: runtimeprofile.Config{Profile: "development"}})
			machineRequest(t, srv, "GET", "/health", "", nil, 200)
			body := tokenRequest{AgentID: "secure-worker", Capabilities: []string{"COMMAND"}, ExpiresAt: time.Now().Add(time.Hour), CertificateSHA256: i13Fingerprint(t, old)}
			var credential issuedMachine
			json.Unmarshal(machineRequest(t, srv, "POST", "/api/agents/tokens", "test-token", body, 200), &credential)
			dial := func(cert *tls.Certificate, token string) (*websocket.Conn, *http.Response, error) {
				cfg := &tls.Config{RootCAs: p.roots}
				if cert != nil {
					cfg.Certificates = []tls.Certificate{*cert}
				}
				return (&websocket.Dialer{TLSClientConfig: cfg}).Dial("wss"+strings.TrimPrefix(srv.URL, "https")+"/ws/agent?id=secure-worker&caps=COMMAND", http.Header{"Authorization": []string{"Bearer " + token}})
			}
			for _, cert := range []*tls.Certificate{nil, &next} {
				conn, res, e := dial(cert, credential.Token)
				if conn != nil {
					conn.Close()
				}
				if e == nil || res == nil || res.StatusCode != 401 {
					t.Fatal("cert ausente/outro leaf aceito")
				}
				res.Body.Close()
			}
			conn, _, e := dial(&old, credential.Token)
			if e != nil {
				t.Fatal(e)
			}
			defer conn.Close()
			var replacement issuedMachine
			json.Unmarshal(machineRequest(t, srv, "POST", fmt.Sprintf("/api/agents/tokens/%d/rotate", credential.ID), "test-token", map[string]any{"expiresAt": time.Now().Add(time.Hour), "graceSeconds": 0, "certificateSHA256": i13Fingerprint(t, next)}, 200), &replacement)
			conn.SetReadDeadline(time.Now().Add(4 * time.Second))
			if _, _, e = conn.ReadMessage(); e == nil {
				t.Fatal("canal antigo continuou ativo")
			}
			if ne, ok := e.(net.Error); ok && ne.Timeout() {
				t.Fatal("canal antigo só atingiu timeout, sem revogação")
			}
			conn2, _, e := dial(&next, replacement.Token)
			if e != nil {
				t.Fatal(e)
			}
			defer conn2.Close()
			// Mesmo canal precisa perder autorização se a CA atual ficar indisponível.
			i13Write(t, p.ca, []byte("invalid CA"))
			conn2.SetReadDeadline(time.Now().Add(4 * time.Second))
			if _, _, e = conn2.ReadMessage(); e == nil {
				t.Fatal("CA inválida não cortou stream")
			}
			if ne, ok := e.(net.Error); ok && ne.Timeout() {
				t.Fatal("canal CA inválida só atingiu timeout")
			}
			// Restore da CA não reativa token antigo; CA inválida falha handshake novo.
		})
	}
}

func TestI13RealAgentSecretsMTLSAndEgress(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "agent.exe")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = "../../../agent"
	if b, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build agente: %v %s", e, b)
	}
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d := machineTestDB(t, dialect)
			dir := t.TempDir()
			p := i13NewPKI(t, dir)
			certFile, keyFile, old := i13Certificate(t, p, dir, "client", false)
			nextFile, nextKey, next := i13Certificate(t, p, dir, "replacement", false)
			var effects atomic.Int32
			var observed atomic.Value
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				effects.Add(1)
				observed.Store(r.Header.Get("Authorization"))
				fmt.Fprint(w, r.Header.Get("Authorization"))
			}))
			defer target.Close()
			policyFile, secretsFile, tokenFile := filepath.Join(dir, "policy.json"), filepath.Join(dir, "secrets.json"), filepath.Join(dir, "token.txt")
			policy := security.Policy{Environment: "prod", Jobs: map[string]security.Job{"secure-job": {Types: []string{"HTTP"}, Secrets: []string{"auth"}, Destinations: []security.Destination{{Host: "127.0.0.1", Port: strings.TrimPrefix(target.URL, "http://127.0.0.1:"), Networks: []string{"127.0.0.1/32"}}}}}}
			writeJSONFile := func(path string, v any) {
				t.Helper()
				b, e := json.Marshal(v)
				if e != nil {
					t.Fatal(e)
				}
				i13Write(t, path, b)
			}
			writeJSONFile(policyFile, policy)
			original, rotated := "Bearer i13-original-canary", "Bearer i13-rotated-canary"
			writeJSONFile(secretsFile, security.Secrets{Values: map[string]string{"auth": original}})
			store := storage.NewFileStore(t.TempDir(), false)
			def := domain.JobDefinition{ID: "secure-job", Label: "Secure job", Team: "test", Environment: "prod", JobType: "HTTP", Confirm: true, Params: map[string]interface{}{"url": target.URL, "headers": map[string]interface{}{"Authorization": map[string]interface{}{"secretRef": "auth"}}}, Schedule: domain.Schedule{Enabled: true}}
			if e := store.Save(def); e != nil {
				t.Fatal(e)
			}
			h := hub.New()
			s := scheduler.New(store, d, h, time.Hour)
			defer s.Stop()
			// Data de negócio fixa, mas leases/retry precisam de tempo que avança.
			clockStarted := time.Now()
			s.SetClock(businessclock.Func(func() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC).Add(time.Since(clockStarted)) }))
			s.RuntimePolicy = runtimeprofile.Config{Profile: "development", ExecutionMode: "durable", TLSClientCA: p.ca}
			s.AttachDurable(execution.New(d, s.Now))
			s.ReloadDefs()
			s.RunDaily("2026-10-02")
			srv := i13Server(t, p, Config{DB: d, Hub: h, Scheduler: s, Store: store, Token: "test-token", RuntimePolicy: s.RuntimePolicy})
			var credential issuedMachine
			json.Unmarshal(machineRequest(t, srv, "POST", "/api/agents/tokens", "test-token", tokenRequest{AgentID: "secure-worker", Environment: "prod", Capabilities: []string{"HTTP", "EXECUTION_V2"}, ExpiresAt: time.Now().Add(time.Hour), CertificateSHA256: i13Fingerprint(t, old)}, 200), &credential)
			i13Write(t, tokenFile, []byte(credential.Token))
			if _, e := d.Exec("UPDATE instances SET confirmed=1 WHERE id='secure-job-2026-10-02'"); e != nil {
				t.Fatal(e)
			}
			order, e := s.DurableEngine().CreateRuntimeOrder("secure-job-2026-10-02")
			if e != nil {
				t.Fatal(e)
			}
			journalFile := filepath.Join(dir, "journal.db")
			logFile := filepath.Join(dir, "agent.log")
			logs, e := os.Create(logFile)
			if e != nil {
				t.Fatal(e)
			}
			agent := exec.Command(bin, "-transport", "v2", "-server", srv.URL, "-token-file", tokenFile, "-tls-cert", certFile, "-tls-key", keyFile, "-tls-ca", p.ca, "-env", "prod", "-id", "secure-worker", "-caps", "HTTP", "-journal", journalFile, "-execution-policy", policyFile, "-job-secrets-file", secretsFile)
			agent.Stdout = logs
			agent.Stderr = logs
			if e = agent.Start(); e != nil {
				logs.Close()
				t.Fatal(e)
			}
			defer func() { agent.Process.Kill(); agent.Wait(); logs.Close() }()
			run := func(key, want string) execution.Attempt {
				t.Helper()
				if e := s.ReportAgentCapacity("secure-worker", 1, 1000, true); e != nil {
					t.Fatal(e)
				}
				if key != "first" {
					if _, e := s.DurableAction("operator", "secure-job-2026-10-02", "rerun"); e != nil {
						t.Fatal(e)
					}
				}
				a, e := s.DurableEngine().Start(order.ID, "secure-worker", key, map[bool]string{true: "start", false: "rerun"}[key == "first"])
				if e != nil {
					t.Fatal(e)
				}
				// A revogação pode ocorrer entre claim e resposta: só o lease permite redelivery.
				deadline := time.Now().Add(s.DurableEngine().DeliveryLease + 15*time.Second)
				for time.Now().Before(deadline) {
					got, e := s.DurableEngine().Attempt(a.ExecutionID)
					if e != nil {
						t.Fatal(e)
					}
					if got.State == want {
						return got
					}
					time.Sleep(20 * time.Millisecond)
				}
				b, _ := os.ReadFile(logFile)
				t.Fatalf("execução não chegou a %s; log=%s", want, string(b))
				return a
			}
			first := run("first", "succeeded")
			if effects.Load() != 1 || observed.Load() != original {
				t.Fatal("primeira execução/autorização")
			}
			// Troca certificado e credencial em disco; processo não é reiniciado.
			var replacement issuedMachine
			json.Unmarshal(machineRequest(t, srv, "POST", fmt.Sprintf("/api/agents/tokens/%d/rotate", credential.ID), "test-token", map[string]any{"expiresAt": time.Now().Add(time.Hour), "graceSeconds": 0, "certificateSHA256": i13Fingerprint(t, next)}, 200), &replacement)
			raw, _ := os.ReadFile(nextFile)
			i13Write(t, certFile, raw)
			raw, _ = os.ReadFile(nextKey)
			i13Write(t, keyFile, raw)
			i13Write(t, tokenFile, []byte(replacement.Token))
			writeJSONFile(secretsFile, security.Secrets{Values: map[string]string{"auth": rotated}})
			second := run("rotated", "succeeded")
			if effects.Load() != 2 || observed.Load() != rotated {
				t.Fatal("rotação não aplicada")
			}
			if e = os.Remove(secretsFile); e != nil {
				t.Fatal(e)
			}
			run("unavailable", "failed")
			if effects.Load() != 2 {
				t.Fatal("secret indisponível causou efeito")
			}
			writeJSONFile(secretsFile, security.Secrets{Values: map[string]string{"auth": rotated}})
			rule := policy.Jobs["secure-job"]
			rule.Secrets = nil
			policy.Jobs["secure-job"] = rule
			writeJSONFile(policyFile, policy)
			run("unauthorized-reference", "failed")
			if effects.Load() != 2 {
				t.Fatal("secret não autorizado causou efeito")
			}
			rule.Secrets = []string{"auth"}
			rule.Destinations = nil
			policy.Jobs["secure-job"] = rule
			writeJSONFile(policyFile, policy)
			run("forbidden-destination", "failed")
			if effects.Load() != 2 {
				t.Fatal("destino proibido causou efeito")
			}
			// Valores brutos não aparecem na definição, journal/output nem log do processo.
			j, e := db.Open(db.SQLite, journalFile)
			if e != nil {
				t.Fatal(e)
			}
			defer j.Close()
			rows, e := j.Query("SELECT envelope,output,reason FROM journal_entries")
			if e != nil {
				t.Fatal(e)
			}
			defer rows.Close()
			for rows.Next() {
				var envelope, out, reason string
				if e := rows.Scan(&envelope, &out, &reason); e != nil {
					t.Fatal(e)
				}
				for _, secret := range []string{original, rotated} {
					if strings.Contains(envelope+out+reason, secret) {
						t.Fatal("secret vazou no journal")
					}
				}
			}
			if e = rows.Err(); e != nil {
				t.Fatal(e)
			}
			for _, a := range []execution.Attempt{first, second} {
				var output string
				if e = d.QueryRow("SELECT result_output FROM execution_attempts WHERE execution_id=?", a.ExecutionID).Scan(&output); e != nil {
					t.Fatal(e)
				}
				if !strings.Contains(output, "[REDACTED]") {
					t.Fatal("canário não redigido no output")
				}
			}
			b, _ := json.Marshal(order)
			logBytes, _ := os.ReadFile(logFile)
			for _, secret := range []string{original, rotated} {
				if strings.Contains(string(b)+string(logBytes), secret) {
					t.Fatal("secret vazou em snapshot/log")
				}
			}
		})
	}
}
