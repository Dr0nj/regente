package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Dr0nj/regente-agent/security"
	"github.com/Dr0nj/regente-server/internal/domain"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestI13InternalHTTPPolicyAndSSH(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var effects atomic.Int32
			canary := "Bearer internal-i13-canary"
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				effects.Add(1)
				fmt.Fprint(w, r.Header.Get("Authorization"))
			}))
			defer target.Close()
			f := durableTest(t, domain.JobDefinition{ID: "known", Team: "test", Environment: "prod", JobType: "HTTP", AgentID: "SERVER-AGENT", Params: map[string]interface{}{"url": target.URL, "headers": map[string]interface{}{"Authorization": map[string]interface{}{"secretRef": "auth"}}}},
				domain.JobDefinition{ID: "denied", Team: "test", Environment: "prod", JobType: "HTTP", AgentID: "SERVER-AGENT", Params: map[string]interface{}{"url": target.URL}})
			f.s.RuntimePolicy.Environment = "prod"
			dir := t.TempDir()
			policyFile, secretsFile := filepath.Join(dir, "policy.json"), filepath.Join(dir, "secrets.json")
			p := security.Policy{Environment: "prod", Jobs: map[string]security.Job{"known": {Types: []string{"HTTP"}, Secrets: []string{"auth"}, Destinations: []security.Destination{{Host: "127.0.0.1", Port: strings.TrimPrefix(target.URL, "http://127.0.0.1:"), Networks: []string{"127.0.0.1/32"}}}}, "denied": {Types: []string{"HTTP"}}}}
			b, _ := json.Marshal(p)
			if e := os.WriteFile(policyFile, b, 0600); e != nil {
				t.Fatal(e)
			}
			b, _ = json.Marshal(security.Secrets{Values: map[string]string{"auth": canary}})
			if e := os.WriteFile(secretsFile, b, 0600); e != nil {
				t.Fatal(e)
			}
			f.s.ExecutionPolicyPath = policyFile
			f.s.JobSecretsFile = secretsFile
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if e := f.s.StartDurableInternal(ctx, nil, filepath.Join(dir, "runtime"), "node", "test", true, false); e != nil {
				t.Fatal(e)
			}
			for _, id := range []string{"known", "denied"} {
				var raw string
				if e := f.d.QueryRow("SELECT definition_snapshot FROM instances WHERE id=?", id+"-2026-09-30").Scan(&raw); e != nil {
					t.Fatal(e)
				}
				var def domain.JobDefinition
				if e := json.Unmarshal([]byte(raw), &def); e != nil {
					t.Fatal(e)
				}
				f.s.startInstance(id+"-2026-09-30", def)
			}
			awaitDurableState(t, f, "known-2026-09-30", "OK")
			awaitDurableState(t, f, "denied-2026-09-30", "NOTOK")
			if effects.Load() != 1 {
				t.Fatal("efeito fora da política")
			}
			var out string
			if e := f.d.QueryRow("SELECT result_output FROM execution_attempts a JOIN runtime_orders r ON a.order_id=r.order_id WHERE r.instance_id=?", "known-2026-09-30").Scan(&out); e != nil {
				t.Fatal(e)
			}
			if strings.Contains(out, canary) || !strings.Contains(out, "[REDACTED]") {
				t.Fatal("canário interno não redigido")
			}
			// SSH deve negar antes de iniciar subprocesso/conexão para destino não autorizado.
			sshCtx, _, _, e := security.Prepare(context.Background(), policyFile, secretsFile, "prod", "known", "HTTP", map[string]interface{}{})
			if e != nil {
				t.Fatal(e)
			}
			code, out := executeDurableSSH(sshCtx, domain.JobDefinition{Params: map[string]interface{}{"host": "127.0.0.1", "port": "22", "command": "effect", "strictHostKey": "yes", "knownHostsPath": "/not-used"}}, nil)
			if code != -1 || out != "SSH destination denied by execution policy" {
				t.Fatal("SSH fora da política não negou antes do efeito")
			}
		})
	}
}
