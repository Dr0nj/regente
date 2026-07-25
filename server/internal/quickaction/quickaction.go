// Package quickaction — D-15 Mobile-friendly alerts: tokens assinados de ação
// rápida (rerun / set-ok / confirm / hold / release) embutidos nos alertas.
//
// O link é a credencial: um token HMAC-SHA256 com escopo de UMA ação numa
// instance específica e validade curta — quem recebeu o alerta no celular age
// em dois toques, sem fazer login. Modelo de segurança:
//
//   - o segredo mora no servidor (settings.quickaction_secret, auto-gerado);
//   - o payload assinado é instanceId|action|exp — trocar QUALQUER parte
//     invalida a assinatura (constant-time compare);
//   - GET nunca executa (mostra a página de confirmação); a ação é POST —
//     preview de link de chat/crawler não dispara rerun;
//   - expirado = 410. Sem revogação individual (é um token de conveniência
//     operacional com TTL de horas, não uma credencial de longa duração).
package quickaction

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Actions permitidas via quick-link (subset deliberado das ações operacionais:
// nada de cancel — destrutivo demais para um toque no celular).
var Allowed = map[string]bool{"rerun": true, "set-ok": true, "confirm": true, "hold": true, "release": true}

var (
	ErrBadToken = errors.New("invalid token")
	ErrExpired  = errors.New("expired token")
)

// NewSecret — 32 bytes aleatórios em hex (gerado uma vez e persistido em settings).
func NewSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Sign — token urlsafe: base64url(instanceId|action|expUnix) + "." + base64url(hmac).
func Sign(secret, instanceID, action string, exp time.Time) (string, error) {
	if !Allowed[action] {
		return "", fmt.Errorf("action %q not allowed as a quick-action", action)
	}
	if strings.ContainsAny(instanceID, "|") {
		return "", errors.New("invalid instanceId")
	}
	payload := fmt.Sprintf("%s|%s|%d", instanceID, action, exp.Unix())
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// Verify — decodifica e valida assinatura + expiração.
func Verify(secret, token string) (instanceID, action string, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", "", ErrBadToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", "", ErrBadToken
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", ErrBadToken
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return "", "", ErrBadToken
	}
	fields := strings.Split(string(payload), "|")
	if len(fields) != 3 || !Allowed[fields[1]] {
		return "", "", ErrBadToken
	}
	exp, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return "", "", ErrBadToken
	}
	if time.Now().Unix() > exp {
		return "", "", ErrExpired
	}
	return fields[0], fields[1], nil
}
