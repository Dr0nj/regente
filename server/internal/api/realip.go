package api

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// realIP — substituto do `middleware.RealIP` do chi, que foi DEPRECADO por ser
// vulnerável a spoofing (GHSA-3fxj-6jh8-hvhx · GHSA-rjr7-jggh-pgcp ·
// GHSA-9g5q-2w5x-hmxf). Dois defeitos, ambos explorados com um header:
//
//   - confiava em X-Forwarded-For / X-Real-IP / True-Client-IP viessem de ONDE
//     viessem — inclusive de um cliente falando direto com o server;
//   - pegava a entrada MAIS À ESQUERDA do X-Forwarded-For, que é justamente a
//     que o cliente escreve. Os saltos confiáveis ficam à DIREITA.
//
// Como o `clientIP()` (audit.go) lia o RemoteAddr que esse middleware reescrevia,
// qualquer um forjava a origem das tentativas de login no audit/SIEM mandando
// `X-Forwarded-For: 1.2.3.4`. Não dava acesso a nada — RemoteAddr não é
// consumido por autorização nem por rate limit —, mas corrompia a trilha.
//
// Aqui a regra é a correta: o header só vale se o PEER TCP for um proxy
// confiável (a topologia do deploy: nginx em loopback, com o server escutando
// em REGENTE_ADDR=127.0.0.1:8080), e a varredura do X-Forwarded-For é da
// DIREITA pra ESQUERDA, parando no primeiro salto que não é confiável — esse é
// o cliente real. Peer não-confiável = headers ignorados por completo.
//
// E, diferente do chi, isto NÃO mexe no RemoteAddr: o IP resolvido vai pro
// contexto e o RemoteAddr continua sendo a verdade sobre o peer TCP. Um campo
// que mente é o que tornou o bug original difícil de enxergar.

// trustedProxiesDefault — loopback. É a topologia que o `deploy/vps/` monta
// (nginx termina o TLS e fala com o server por 127.0.0.1) e o padrão seguro
// para quem expõe o server direto: sem proxy na frente, nenhum header vale.
var trustedProxiesDefault = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("::1/128"),
}

// ParseTrustedProxies converte a lista de `-trusted-proxies` (CIDRs ou IPs
// soltos, separados por vírgula) em prefixos. Entrada vazia = o default
// (loopback). Um IP solto vira /32 ou /128.
func ParseTrustedProxies(spec string) ([]netip.Prefix, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return trustedProxiesDefault, nil
	}
	var out []netip.Prefix
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "/") {
			p, err := netip.ParsePrefix(part)
			if err != nil {
				return nil, err
			}
			out = append(out, p.Masked())
			continue
		}
		addr, err := netip.ParseAddr(part)
		if err != nil {
			return nil, err
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	if len(out) == 0 {
		return trustedProxiesDefault, nil
	}
	return out, nil
}

type clientIPCtxKey struct{}

// realIP devolve o middleware. `trusted` vazio cai no default (loopback).
func realIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	if len(trusted) == 0 {
		trusted = trustedProxiesDefault
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ip, ok := resolveClientIP(r, trusted); ok {
				ctx := context.WithValue(r.Context(), clientIPCtxKey{}, ip)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// resolveClientIP aplica a regra descrita no topo do arquivo. O bool é false
// quando não há nada melhor que o RemoteAddr (peer não-confiável, ou confiável
// mas sem header) — aí o clientIP() cai no fallback.
func resolveClientIP(r *http.Request, trusted []netip.Prefix) (string, bool) {
	peer, err := addrFromHostPort(r.RemoteAddr)
	if err != nil {
		return "", false
	}
	if !isTrusted(peer, trusted) {
		// Cliente falando direto com o server: os headers são dele, não valem nada.
		return "", false
	}
	// X-Forwarded-For, da direita pra esquerda: o primeiro salto que NÃO é um
	// proxy confiável é o cliente. Se todos forem confiáveis, o mais à esquerda
	// (a ponta da cadeia) é o melhor palpite.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		hops := strings.Split(xff, ",")
		for i := len(hops) - 1; i >= 0; i-- {
			addr, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
			if err != nil {
				continue
			}
			if !isTrusted(addr, trusted) {
				return addr.Unmap().String(), true
			}
		}
	}
	// Sem XFF utilizável: X-Real-IP, que o nginx do `deploy/vps/` seta. Só chega
	// aqui com peer confiável, então é o proxy falando, não o cliente.
	if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" {
		if addr, err := netip.ParseAddr(xr); err == nil {
			return addr.Unmap().String(), true
		}
	}
	return "", false
}

func isTrusted(addr netip.Addr, trusted []netip.Prefix) bool {
	addr = addr.Unmap()
	for _, p := range trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func addrFromHostPort(remoteAddr string) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	// IPv6 com zona ("fe80::1%eth0") não parseia como Addr puro.
	if i := strings.IndexByte(host, '%'); i >= 0 {
		host = host[:i]
	}
	return netip.ParseAddr(host)
}
