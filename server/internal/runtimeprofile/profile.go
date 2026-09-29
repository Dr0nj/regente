// Package runtimeprofile valida a fronteira produtiva antes dos serviços.
package runtimeprofile

import (
	"database/sql"
	"errors"
	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type Config struct {
	Profile, Environment, Network, ControlPlane      string
	Addr, AppURL, Token, AuthMode                    string
	TLSCert, TLSKey, TLSClientCA, TrustedProxies     string
	OIDCIssuer, OIDCClientID, OIDCRedirect, OIDCRole string
	Role, Scheduler, Bus                             string
	Demo, ServerAgent                                bool
}

func (c Config) Production() bool { return c.Profile == "production" }

var environmentPattern = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$")

func (c Config) AllowsEnvironment(env string) bool {
	return !c.Production() || (env != "" && env == c.Environment)
}
func loopback(host string) bool {
	ip, err := netip.ParseAddr(host)
	return host == "localhost" || (err == nil && ip.IsLoopback())
}
func (c Config) Validate() error {
	if c.Profile != "development" && !c.Production() {
		return errors.New("profile must be development or production")
	}
	mode, err := auth.Mode(c.AuthMode)
	if err != nil {
		return errors.New("invalid auth mode")
	}
	if !c.Production() {
		if c.Environment != "" || c.Network != "" || c.ControlPlane != "" {
			return errors.New("environment, network-boundary and control-plane-execution require production profile")
		}
		return nil
	}
	if !environmentPattern.MatchString(c.Environment) {
		return errors.New("production requires an explicit environment (1-64 letters, digits, dots, underscores or hyphens)")
	}
	if c.Token != "" {
		return errors.New("production forbids the legacy API token; use user sessions and scoped machine credentials")
	}
	if c.Demo {
		return errors.New("production forbids demo mode")
	}
	if c.Role != "all" && c.Role != "api" && c.Role != "scheduler" {
		return errors.New("invalid process role")
	}
	if c.Scheduler != "internal" && c.Scheduler != "external" {
		return errors.New("invalid scheduler mode")
	}
	if c.Bus != "hub" && c.Bus != "nats" {
		return errors.New("invalid bus mode")
	}
	switch c.ControlPlane {
	case "deny":
		if c.ServerAgent {
			return errors.New("SERVER-AGENT requires an explicit execution policy")
		}
	case "http", "http-ssh":
	default:
		return errors.New("control-plane-execution must be deny, http or http-ssh")
	}
	host, port, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return errors.New("production requires an explicit numeric listen address and port")
	}
	ip, err := netip.ParseAddr(host)
	n, perr := strconv.Atoi(port)
	if err != nil || perr != nil || n < 1 || n > 65535 {
		return errors.New("production requires an explicit numeric listen address and valid port")
	}
	app, err := url.Parse(c.AppURL)
	if err != nil || app.Hostname() == "" || app.User != nil || app.RawQuery != "" || app.Fragment != "" || (app.Path != "" && app.Path != "/") {
		return errors.New("production requires app-url as a public origin without credentials, query or fragment")
	}
	tls := c.TLSCert != "" && c.TLSKey != ""
	if (c.TLSCert == "") != (c.TLSKey == "") || (c.TLSClientCA != "" && !tls) {
		return errors.New("incomplete TLS configuration")
	}
	switch c.Network {
	case "loopback":
		if !ip.IsLoopback() || tls || app.Scheme != "http" || !loopback(app.Hostname()) {
			return errors.New("loopback boundary requires local HTTP listen and app origin")
		}
	case "proxy":
		if !ip.IsLoopback() || tls || app.Scheme != "https" || strings.TrimSpace(c.TrustedProxies) == "" {
			return errors.New("proxy boundary requires loopback HTTP, HTTPS app origin and explicit trusted proxies")
		}
	case "tls":
		if !tls || app.Scheme != "https" {
			return errors.New("tls boundary requires a certificate, key and HTTPS app origin")
		}
	default:
		return errors.New("production requires network-boundary: loopback, proxy or tls")
	}
	for _, raw := range strings.Split(c.TrustedProxies, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		p, err := netip.ParsePrefix(raw)
		if err != nil {
			a, e := netip.ParseAddr(raw)
			if e != nil {
				return errors.New("invalid trusted proxy address")
			}
			p = netip.PrefixFrom(a, a.BitLen())
		}
		if p.Bits() == 0 {
			return errors.New("production forbids trusting every proxy")
		}
		if c.Network == "proxy" && (!p.Addr().IsLoopback() || (p.Addr().Is4() && p.Bits() < 8) || (p.Addr().Is6() && p.Bits() != 128)) {
			return errors.New("proxy boundary only trusts loopback proxy addresses")
		}
	}
	if mode == "local" {
		if c.OIDCIssuer != "" || c.OIDCClientID != "" || c.OIDCRedirect != "" {
			return errors.New("local mode has unexpected OIDC configuration")
		}
		return nil
	}
	issuer, err := url.Parse(c.OIDCIssuer)
	if err != nil || issuer.Hostname() == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" || (issuer.Scheme != "https" && !(c.Network == "loopback" && issuer.Scheme == "http" && loopback(issuer.Hostname()))) || c.OIDCClientID == "" || !auth.Role(c.OIDCRole).Valid() {
		return errors.New("production requires a valid OIDC issuer, client and initial role")
	}
	redirect, err := url.Parse(c.OIDCRedirect)
	if err != nil || redirect.Scheme != app.Scheme || redirect.Host != app.Host || redirect.Path != "/api/auth/oidc/callback" || redirect.User != nil || redirect.RawQuery != "" || redirect.Fragment != "" {
		return errors.New("OIDC callback must use the app origin and /api/auth/oidc/callback")
	}
	return nil
}

// Bind impede downgrade acidental e reuso do banco por outro ambiente.
func Bind(d *db.DB, c Config) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var bound string
	err = tx.QueryRow("SELECT value FROM settings WHERE key='_runtime_environment'").Scan(&bound)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if bound != "" && (!c.Production() || bound != c.Environment) {
		return errors.New("database is bound to a production environment; restore its explicit profile and environment")
	}
	if c.Production() && bound == "" {
		var running int
		if err = tx.QueryRow("SELECT COUNT(*) FROM instances WHERE status='RUNNING'").Scan(&running); err != nil {
			return err
		}
		if running != 0 {
			return errors.New("drain running instances before converting the database to production")
		}
		if _, err = tx.Exec("INSERT INTO settings(key,value) VALUES('_runtime_environment',?) ON CONFLICT(key) DO NOTHING", c.Environment); err != nil {
			return err
		}
		if err = tx.QueryRow("SELECT value FROM settings WHERE key='_runtime_environment'").Scan(&bound); err != nil {
			return err
		}
		if bound != c.Environment {
			return errors.New("database production environment conflict")
		}
		if _, err = tx.Exec("DELETE FROM sessions"); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (c Config) ExecutionError(def domain.JobDefinition) string {
	if !c.Production() {
		return ""
	}
	if !c.AllowsEnvironment(def.Environment) {
		return "Job environment does not match the production environment"
	}
	if strings.EqualFold(def.JobType, "SSH") && c.ControlPlane != "http-ssh" {
		return "SSH execution on the control plane is disabled by production policy"
	}
	if def.AgentID == "SERVER-AGENT" && (!c.ServerAgent || c.ControlPlane == "deny") {
		return "SERVER-AGENT is disabled by production policy"
	}
	return ""
}
