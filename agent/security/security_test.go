package security

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func secureFile(t *testing.T, path string, v any) {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestI13SecretsRotationAuthorizationOutage(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "policy.json")
	f := filepath.Join(dir, "secrets.json")
	secureFile(t, p, Policy{Environment: "prod", Jobs: map[string]Job{"job": {Types: []string{"HTTP"}, Secrets: []string{"auth"}}}})
	secureFile(t, f, Secrets{Values: map[string]string{"auth": "Bearer original-canary"}})
	input := map[string]interface{}{"headers": map[string]interface{}{"Authorization": map[string]interface{}{"secretRef": "auth"}}}
	before, _ := json.Marshal(input)
	for _, bad := range []struct{ env, id, kind string }{{"dev", "job", "HTTP"}, {"prod", "other", "HTTP"}, {"prod", "job", "COMMAND"}} {
		if _, _, _, e := Prepare(context.Background(), p, f, bad.env, bad.id, bad.kind, input); e == nil {
			t.Fatal("autorização fora do escopo")
		}
	}
	_, params, redact, e := Prepare(context.Background(), p, f, "prod", "job", "HTTP", input)
	if e != nil {
		t.Fatal(e)
	}
	if params["headers"].(map[string]interface{})["Authorization"] != "Bearer original-canary" {
		t.Fatal("secret não resolvido")
	}
	if strings.Contains(redact("echo Bearer original-canary"), "original-canary") {
		t.Fatal("vazamento no output")
	}
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("snapshot foi mutado")
	}
	secureFile(t, f, Secrets{Values: map[string]string{"auth": "Bearer rotated-canary"}})
	_, params, _, e = Prepare(context.Background(), p, f, "prod", "job", "HTTP", input)
	if e != nil || params["headers"].(map[string]interface{})["Authorization"] != "Bearer rotated-canary" {
		t.Fatal("rotação cacheada")
	}
	if e = os.Remove(f); e != nil {
		t.Fatal(e)
	}
	if _, _, _, e = Prepare(context.Background(), p, f, "prod", "job", "HTTP", input); e != ErrSecrets {
		t.Fatal("indisponível serviu valor stale")
	}
	if _, _, _, e = Prepare(context.Background(), "", f, "prod", "job", "HTTP", input); e != ErrPolicy {
		t.Fatal("refs sem política")
	}
}
func TestI13EgressRedirectAndSensitiveDestination(t *testing.T) {
	effects := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { effects++; w.Write([]byte("unauthorized")) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer source.Close()
	host := "127.0.0.1"
	port := strings.TrimPrefix(source.URL, "http://"+host+":")
	j := Job{Destinations: []Destination{{Host: host, Port: port, Networks: []string{"127.0.0.1/32"}}}}
	ctx := context.WithValue(context.Background(), policyKey{}, j)
	req, _ := http.NewRequestWithContext(ctx, "GET", source.URL, nil)
	resp, e := HTTPClient(ctx, time.Second).Do(req)
	if resp != nil {
		resp.Body.Close()
	}
	if e == nil || effects != 0 {
		t.Fatal("redirect alcançou destino proibido")
	}
	if _, e = PinnedAddress(ctx, "127.0.0.1", strings.TrimPrefix(target.URL, "http://127.0.0.1:")); e == nil {
		t.Fatal("porta fora da política")
	}
	if _, e = PinnedAddress(ctx, "localhost", port); e == nil {
		t.Fatal("host alias fora da política")
	}
	for _, ip := range []string{"169.254.169.254", "0.0.0.0", "224.0.0.1", "fe80::1"} {
		c := context.WithValue(context.Background(), policyKey{}, Job{Destinations: []Destination{{Host: ip, Port: "80", Networks: []string{"0.0.0.0/0", "::/0"}}}})
		if _, e = PinnedAddress(c, ip, "80"); e == nil {
			t.Fatalf("destino sensível permitido: %s", ip)
		}
	}
	// IP privado não é proibido arbitrariamente; precisa de CIDR explícito e porta exata.
	if address, e := PinnedAddress(ctx, host, port); e != nil || address != fmt.Sprintf("%s:%s", host, port) {
		t.Fatal("IP autorizado negado")
	}
	_ = netip.MustParseAddr(host)
}
