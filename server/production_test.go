package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Executa o binário real: validação deve anteceder banco, workspace e listener.
func TestProductionBoot(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "server")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	env := []string{}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "REGENTE_") {
			env = append(env, e)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	base := "http://" + addr
	common := []string{"-addr", addr, "-workspace", filepath.Join(dir, "workspace"), "-role", "api", "-selfmon=false"}
	production := []string{"-profile", "production", "-environment", "prod", "-network-boundary", "loopback", "-app-url", base}
	invoke := func(extra []string, moreEnv ...string) ([]byte, error) {
		cmd := exec.Command(bin, append(append([]string{}, common...), extra...)...)
		cmd.Env = append(append([]string{}, env...), moreEnv...)
		return cmd.CombinedOutput()
	}
	checkDB := filepath.Join(dir, "check.db")
	out, err := invoke(append(append([]string{}, production...), "-db", checkDB, "-check-config"))
	if err != nil {
		t.Fatalf("check: %v %s", err, out)
	}
	if _, err = os.Stat(checkDB); !os.IsNotExist(err) {
		t.Fatal("check-config criou banco")
	}
	for _, bad := range [][]string{{"-api-token", "synthetic-secret-never-log"}, {"-demo-mode"}, {"-server-agent"}, {"-addr", ":8080"}, {"-environment", ""}, {"-db-driver", "invalid", "-check-config"}, {"-migration-timeout", "0", "-check-config"}, {"-network-boundary", "tls", "-app-url", "https://example.test", "-tls-cert", "missing-cert", "-tls-key", "missing-key"}} {
		args := append(append(append([]string{}, production...), "-db", checkDB), bad...)
		out, err = invoke(args)
		if err == nil || strings.Contains(string(out), "synthetic-secret-never-log") {
			t.Fatalf("boot inseguro ou segredo no diagnóstico: %s", out)
		}
		if _, err = os.Stat(checkDB); !os.IsNotExist(err) {
			t.Fatal("boot inválido abriu banco")
		}
	}
	client := &http.Client{Timeout: time.Second}
	request := func(method, path, token string, body any, want int) map[string]any {
		t.Helper()
		data, _ := json.Marshal(body)
		req, _ := http.NewRequest(method, base+path, bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, e := client.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s: %d esperado %d: %s", path, resp.StatusCode, want, raw)
		}
		v := map[string]any{}
		_ = json.Unmarshal(raw, &v)
		return v
	}
	start := func(args []string, moreEnv ...string) func() {
		t.Helper()
		cmd := exec.Command(bin, append(append([]string{}, common...), args...)...)
		cmd.Env = append(append([]string{}, env...), moreEnv...)
		var logs bytes.Buffer
		cmd.Stdout = &logs
		cmd.Stderr = &logs
		if e := cmd.Start(); e != nil {
			t.Fatal(e)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		stopped := false
		stop := func() {
			if !stopped {
				_ = cmd.Process.Kill()
				<-done
				stopped = true
			}
		}
		t.Cleanup(stop)
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case e := <-done:
				stopped = true
				t.Fatalf("boot falhou: %v %s", e, logs.String())
			default:
			}
			resp, e := client.Get(base + "/health")
			if e == nil {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					return stop
				}
			}
			time.Sleep(40 * time.Millisecond)
		}
		stop()
		t.Fatalf("sem health: %s", logs.String())
		return nil
	}
	const password = "production-fixture-password"
	// Instalação limpa: recusa segredo ausente, bootstrap explícito e restart sem segredo.
	fresh := append(append([]string{}, production...), "-db", filepath.Join(dir, "fresh.db"))
	if out, err = invoke(fresh); err == nil || !strings.Contains(string(out), "REGENTE_BOOTSTRAP_PASSWORD") {
		t.Fatalf("bootstrap ausente não recusado: %s", out)
	}
	stop := start(fresh, "REGENTE_BOOTSTRAP_PASSWORD="+password)
	login := request("POST", "/api/auth/login", "", map[string]string{"username": "admin", "password": password}, 200)
	token := fmt.Sprint(login["token"])
	request("GET", "/api/users", token, nil, 200)
	request("GET", "/api/users", "dev-token", nil, 401)
	stop()
	stop = start(fresh)
	request("POST", "/api/auth/login", "", map[string]string{"username": "admin", "password": password}, 200)
	stop()
	// Conversão: senha padrão bloqueia, rotação permite, sessão antiga é revogada.
	legacyDB := filepath.Join(dir, "legacy.db")
	legacy := []string{"-db", legacyDB, "-server-agent=false"}
	stop = start(legacy)
	login = request("POST", "/api/auth/login", "", map[string]string{"username": "admin", "password": "admin"}, 200)
	token = fmt.Sprint(login["token"])
	stop()
	upgrade := append(append([]string{}, production...), "-db", legacyDB)
	if out, err = invoke(upgrade); err == nil || !strings.Contains(string(out), "development password") {
		t.Fatalf("default aceito: %s", out)
	}
	stop = start(legacy)
	login = request("POST", "/api/auth/login", "", map[string]string{"username": "admin", "password": "admin"}, 200)
	token = fmt.Sprint(login["token"])
	request("POST", "/api/auth/change-password", token, map[string]string{"current": "admin", "next": password}, 204)
	login = request("POST", "/api/auth/login", "", map[string]string{"username": "admin", "password": password}, 200)
	token = fmt.Sprint(login["token"])
	stop()
	stop = start(upgrade)
	request("GET", "/api/users", token, nil, 401)
	request("POST", "/api/auth/login", "", map[string]string{"username": "admin", "password": password}, 200)
	stop()
	if out, err = invoke(legacy); err == nil || !strings.Contains(string(out), "database is bound") {
		t.Fatalf("downgrade silencioso: %s", out)
	}
	stop = start(upgrade)
	request("POST", "/api/auth/login", "", map[string]string{"username": "admin", "password": password}, 200)
	stop()
}
