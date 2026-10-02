// Package security aplica a política local antes de resolver secrets ou iniciar efeitos.
package security

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

var ErrPolicy = errors.New("execution denied by local policy")
var ErrSecrets = errors.New("authorized secret unavailable")

type Destination struct {
	Host     string   `json:"host"`
	Port     string   `json:"port"`
	Networks []string `json:"networks"`
}
type Job struct {
	Types        []string      `json:"types"`
	Secrets      []string      `json:"secrets"`
	Destinations []Destination `json:"destinations"`
}
type Policy struct {
	Environment string         `json:"environment"`
	Jobs        map[string]Job `json:"jobs"`
}
type policyKey struct{}
type Secrets struct {
	Values map[string]string `json:"values"`
}

func read(path string, out any) error {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 1<<20 {
		return ErrPolicy
	}
	if runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0 {
		return ErrPolicy
	}
	f, err := os.Open(path)
	if err != nil {
		return ErrPolicy
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, 1<<20))
	dec.DisallowUnknownFields()
	if dec.Decode(out) != nil {
		return ErrPolicy
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return ErrPolicy
	}
	return nil
}
func Load(path string) (Policy, error) {
	var p Policy
	if read(path, &p) != nil || p.Environment == "" || len(p.Jobs) == 0 {
		return p, ErrPolicy
	}
	for id, j := range p.Jobs {
		if id == "" || len(j.Types) == 0 {
			return p, ErrPolicy
		}
		for _, d := range j.Destinations {
			if d.Host == "" || (strings.ContainsAny(d.Host, "/@ \t\r\n") || (strings.Contains(d.Host, ":") && net.ParseIP(d.Host) == nil)) || d.Port == "" || len(d.Networks) == 0 {
				return p, ErrPolicy
			}
			if port, err := strconv.Atoi(d.Port); err != nil || port < 1 || port > 65535 {
				return p, ErrPolicy
			}
			for _, n := range d.Networks {
				if _, err := netip.ParsePrefix(n); err != nil {
					return p, ErrPolicy
				}
			}
		}
	}
	return p, nil
}
func HasRefs(v any) bool {
	switch x := v.(type) {
	case map[string]interface{}:
		if _, ok := x["secretRef"]; ok {
			return true
		}
		for _, a := range x {
			if HasRefs(a) {
				return true
			}
		}
	case []interface{}:
		for _, a := range x {
			if HasRefs(a) {
				return true
			}
		}
	}
	return false
}

// Prepare reabre arquivos por execução; TTL=0, sem servir cache stale após falha.
// Retorna cópia de params: a definição congelada/journal nunca recebe valores.
func Prepare(ctx context.Context, policyFile, secretsFile, env, id, kind string, params map[string]interface{}) (context.Context, map[string]interface{}, func(string) string, error) {
	redact := func(s string) string { return s }
	if policyFile == "" {
		if HasRefs(params) {
			return ctx, nil, redact, ErrPolicy
		}
		return ctx, params, redact, nil
	}
	p, err := Load(policyFile)
	if err != nil || p.Environment != env {
		return ctx, nil, redact, ErrPolicy
	}
	j, ok := p.Jobs[id]
	if !ok || !slices.Contains(j.Types, strings.ToUpper(kind)) {
		return ctx, nil, redact, ErrPolicy
	}
	// Adapters sem dialer controlável não são homologados neste perfil.
	switch strings.ToUpper(kind) {
	case "HTTP", "REST", "SSH", "COMMAND", "SCRIPT":
	default:
		return ctx, nil, redact, ErrPolicy
	}
	var secrets Secrets
	if HasRefs(params) {
		// Secrets só em executores sem comandos arbitrários neste domínio de confiança.
		if kind != "HTTP" && kind != "REST" {
			return ctx, nil, redact, ErrPolicy
		}
		if read(secretsFile, &secrets) != nil {
			return ctx, nil, redact, ErrSecrets
		}
	}
	values := []string{}
	var resolve func(any) (any, error)
	resolve = func(v any) (any, error) {
		switch x := v.(type) {
		case map[string]interface{}:
			if ref, exists := x["secretRef"]; exists {
				key, ok := ref.(string)
				if !ok || len(x) != 1 || !slices.Contains(j.Secrets, key) {
					return nil, ErrPolicy
				}
				value, ok := secrets.Values[key]
				if !ok || len(value) < 8 || len(value) > 4096 {
					return nil, ErrSecrets
				}
				values = append(values, value)
				return value, nil
			}
			out := map[string]interface{}{}
			for k, a := range x {
				v, e := resolve(a)
				if e != nil {
					return nil, e
				}
				out[k] = v
			}
			return out, nil
		case []interface{}:
			out := make([]interface{}, len(x))
			for i, a := range x {
				v, e := resolve(a)
				if e != nil {
					return nil, e
				}
				out[i] = v
			}
			return out, nil
		default:
			return v, nil
		}
	}
	if params == nil {
		return context.WithValue(ctx, policyKey{}, j), nil, redact, nil
	}
	v, err := resolve(params)
	if err != nil {
		return ctx, nil, redact, err
	}
	slices.SortFunc(values, func(a, b string) int { return len(b) - len(a) })
	redact = func(s string) string {
		for _, v := range values {
			s = strings.ReplaceAll(s, v, "[REDACTED]")
		}
		return s
	}
	ctx = context.WithValue(ctx, policyKey{}, j)
	return ctx, v.(map[string]interface{}), redact, nil
}

// PinnedAddress valida todas as respostas DNS e conecta ao IP aprovado,
// eliminando segunda resolução e rebinding entre autorização e dial.
func PinnedAddress(ctx context.Context, host, port string) (string, error) {
	j, restricted := ctx.Value(policyKey{}).(Job)
	if !restricted {
		return net.JoinHostPort(host, port), nil
	}
	var d *Destination
	for i := range j.Destinations {
		if strings.EqualFold(j.Destinations[i].Host, host) && j.Destinations[i].Port == port {
			d = &j.Destinations[i]
			break
		}
	}
	if d == nil {
		return "", ErrPolicy
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return "", ErrPolicy
	}
	for _, ip := range ips {
		ip = ip.Unmap()
		if ip == netip.MustParseAddr("100.100.100.200") || ip == netip.MustParseAddr("fd00:ec2::254") {
			return "", ErrPolicy
		}
		if !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return "", ErrPolicy
		}
		allowed := false
		for _, n := range d.Networks {
			prefix, _ := netip.ParsePrefix(n)
			if prefix.Contains(ip) {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", ErrPolicy
		}
	}
	return net.JoinHostPort(ips[0].Unmap().String(), port), nil
}
func Restricted(ctx context.Context) bool { _, ok := ctx.Value(policyKey{}).(Job); return ok }
func HTTPClient(ctx context.Context, timeout time.Duration) *http.Client {
	c := &http.Client{Timeout: timeout}
	if !Restricted(ctx) {
		return c
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	t.DisableKeepAlives = true
	t.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, ErrPolicy
		}
		pinned, err := PinnedAddress(ctx, host, port)
		if err != nil {
			return nil, err
		}
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, pinned)
	}
	c.Transport = t
	// Nenhum redirect transfere cabeçalhos/segredos ou dispara efeito fora da origem.
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return ErrPolicy }
	return c
}

// CleanEnvironment impede herança de credenciais em executores de processos.
func CleanEnvironment() []string {
	out := []string{}
	for _, entry := range os.Environ() {
		k, _, _ := strings.Cut(entry, "=")
		u := strings.ToUpper(k)
		switch u {
		case "PATH", "SYSTEMROOT", "WINDIR", "COMSPEC", "TEMP", "TMP", "LANG", "TZ":
			out = append(out, entry)
		default:
			if strings.HasPrefix(u, "LC_") {
				out = append(out, entry)
			}
		}
	}
	return out
}
