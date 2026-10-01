package runtimeprofile

import (
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"path/filepath"
	"strings"
	"testing"
)

func valid() Config {
	return Config{Profile: "production", Environment: "prod", Network: "loopback", ControlPlane: "deny", Addr: "127.0.0.1:8080", AppURL: "http://127.0.0.1:8080", AuthMode: "local", Role: "all", Scheduler: "internal", Bus: "hub"}
}
func TestConfigurationMatrix(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Config)
		ok     bool
	}{
		{"local", func(c *Config) {}, true},
		{"proxy", func(c *Config) {
			c.Network = "proxy"
			c.AppURL = "https://example.test"
			c.TrustedProxies = "127.0.0.1/32"
		}, true},
		{"tls", func(c *Config) {
			c.Network = "tls"
			c.TLSCert = "cert"
			c.TLSKey = "key"
			c.AppURL = "https://example.test"
			c.Addr = "0.0.0.0:443"
		}, true},
		{"http", func(c *Config) { c.ControlPlane = "http"; c.ServerAgent = true }, true},
		{"ssh", func(c *Config) { c.ControlPlane = "http-ssh" }, true},
		{"profile", func(c *Config) { c.Profile = "prod" }, false},
		{"environment", func(c *Config) { c.Environment = "" }, false},
		{"environment-wildcard", func(c *Config) { c.Environment = "*" }, false},
		{"legacy-token", func(c *Config) { c.Token = "do-not-print-secret" }, false},
		{"demo", func(c *Config) { c.Demo = true }, false},
		{"implicit-server-agent", func(c *Config) { c.ServerAgent = true }, false},
		{"implicit-network", func(c *Config) { c.Network = "" }, false},
		{"wildcard-listen", func(c *Config) { c.Addr = ":8080" }, false},
		{"public-http", func(c *Config) { c.Addr = "0.0.0.0:8080" }, false},
		{"port", func(c *Config) { c.Addr = "127.0.0.1:0" }, false},
		{"app-secret", func(c *Config) { c.AppURL = "http://do-not-print-secret@localhost" }, false},
		{"role", func(c *Config) { c.Role = "typo" }, false},
		{"scheduler", func(c *Config) { c.Scheduler = "typo" }, false},
		{"bus", func(c *Config) { c.Bus = "typo" }, false},
		{"partial-tls", func(c *Config) { c.TLSCert = "cert" }, false},
		{"proxy-all", func(c *Config) { c.TrustedProxies = "0.0.0.0/0" }, false},
		{"local-oidc-leftover", func(c *Config) { c.OIDCIssuer = "https://idp.test" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := valid()
			tc.change(&c)
			err := c.Validate()
			if (err == nil) != tc.ok {
				t.Fatalf("resultado incorreto: %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "do-not-print-secret") {
				t.Fatal("segredo no diagnóstico")
			}
		})
	}
	for _, mode := range []string{"hybrid", "oidc"} {
		t.Run(mode, func(t *testing.T) {
			c := valid()
			c.AuthMode = mode
			c.OIDCIssuer = "https://idp.test"
			c.OIDCClientID = "regente"
			c.OIDCRole = "viewer"
			c.OIDCRedirect = c.AppURL + "/api/auth/oidc/callback"
			if err := c.Validate(); err != nil {
				t.Fatal(err)
			}
			for _, mutate := range []func(*Config){func(c *Config) { c.OIDCRedirect = "https://wrong.test/api/auth/oidc/callback" }, func(c *Config) { c.OIDCIssuer = "http://public.test" }, func(c *Config) { c.OIDCRole = "invalid" }, func(c *Config) { c.OIDCClientID = "" }} {
				bad := c
				mutate(&bad)
				if bad.Validate() == nil {
					t.Fatal("OIDC inválido aceito")
				}
			}
		})
	}
	if err := (Config{Profile: "development"}).Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestDatabaseBinding(t *testing.T) {
	d, err := db.Open(db.SQLite, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err = db.Migrate(d); err != nil {
		t.Fatal(err)
	}
	if err = Bind(d, Config{Profile: "development"}); err != nil {
		t.Fatal(err)
	}
	c := valid()
	if err = Bind(d, c); err != nil {
		t.Fatal(err)
	}
	if err = Bind(d, c); err != nil {
		t.Fatal(err)
	}
	if Bind(d, Config{Profile: "development"}) == nil {
		t.Fatal("downgrade aceito")
	}
	c.Environment = "staging"
	if Bind(d, c) == nil {
		t.Fatal("outro ambiente aceito")
	}
}
func TestExecutionPolicy(t *testing.T) {
	c := valid()
	d := domain.JobDefinition{Environment: "prod", JobType: "COMMAND"}
	if c.ExecutionError(d) != "" {
		t.Fatal("agente externo bloqueado")
	}
	for _, env := range []string{"", "staging"} {
		d.Environment = env
		if c.ExecutionError(d) == "" {
			t.Fatal("ambiente inválido aceito")
		}
	}
	d.Environment = "prod"
	d.JobType = "ssh"
	if c.ExecutionError(d) == "" {
		t.Fatal("SSH aceito")
	}
	c.ControlPlane = "http-ssh"
	if c.ExecutionError(d) != "" {
		t.Fatal("SSH explícito bloqueado")
	}
	d.JobType = "HTTP"
	d.AgentID = "SERVER-AGENT"
	if c.ExecutionError(d) == "" {
		t.Fatal("SERVER-AGENT desligado aceito")
	}
	c.ServerAgent = true
	if c.ExecutionError(d) != "" {
		t.Fatal("HTTP explícito bloqueado")
	}
}

func TestProductionConversionRequiresDrain(t *testing.T) {
	d, err := db.Open(db.SQLite, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err = db.Migrate(d); err != nil {
		t.Fatal(err)
	}
	if _, err = d.Exec("INSERT INTO instances(id,definition_id,order_date,status,scheduled_at) VALUES('run','job','2026-09-29','RUNNING',CURRENT_TIMESTAMP)"); err != nil {
		t.Fatal(err)
	}
	if Bind(d, valid()) == nil {
		t.Fatal("conversão com execução ativa aceita")
	}
	var count int
	if err = d.QueryRow("SELECT COUNT(*) FROM settings WHERE key='_runtime_environment'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("falha deixou marca persistente")
	}
	if _, err = d.Exec("UPDATE instances SET status='OK' WHERE id='run'"); err != nil {
		t.Fatal(err)
	}
	if err = Bind(d, valid()); err != nil {
		t.Fatal(err)
	}
}
