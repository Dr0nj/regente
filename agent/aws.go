package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

// runLambdaJob — adapter de nuvem por capability (LAMBDA). Invoca uma função AWS Lambda
// pela Invoke API, assinando a requisição com SigV4 (stdlib só — sem aws-sdk-go). Mesmo
// seam dos demais executores: um agente com `-caps LAMBDA` recebe jobType=LAMBDA; o core
// do server não muda.
//
// Params (actionConfig):
//
//	function        (obrigatório) — nome ou ARN da função
//	region          (default env AWS_REGION)
//	payload         (opcional)    — JSON enviado como event à função (default {})
//	accessKeyId     (default env AWS_ACCESS_KEY_ID)
//	secretAccessKey (default env AWS_SECRET_ACCESS_KEY)
//	sessionToken    (opcional; default env AWS_SESSION_TOKEN)
//	endpoint        (opcional)    — override da URL base (testes); default lambda.<region>.amazonaws.com
func runLambdaJob(params map[string]interface{}, timeoutSec int, emit func(string)) (int, string) {
	fn, _ := params["function"].(string)
	if fn == "" {
		return -1, "missing 'function' param"
	}
	region := strFromParamsOrEnv(params, "region", "AWS_REGION")
	if region == "" {
		return -1, "missing 'region' (param ou AWS_REGION)"
	}
	ak := strFromParamsOrEnv(params, "accessKeyId", "AWS_ACCESS_KEY_ID")
	sk := strFromParamsOrEnv(params, "secretAccessKey", "AWS_SECRET_ACCESS_KEY")
	if ak == "" || sk == "" {
		return -1, "missing AWS credentials (accessKeyId/secretAccessKey ou env)"
	}
	sessTok := strFromParamsOrEnv(params, "sessionToken", "AWS_SESSION_TOKEN")
	payload, _ := params["payload"].(string)
	if payload == "" {
		payload = "{}"
	}

	base, _ := params["endpoint"].(string)
	if base == "" {
		base = "https://lambda." + region + ".amazonaws.com"
	}
	invokeURL := strings.TrimRight(base, "/") + "/2015-03-31/functions/" + fn + "/invocations"

	body := []byte(payload)
	hdrs, err := sigv4Headers(http.MethodPost, invokeURL, region, "lambda", body, ak, sk, sessTok, time.Now())
	if err != nil {
		return -1, "sigv4: " + err.Error()
	}
	req, err := http.NewRequest(http.MethodPost, invokeURL, bytes.NewReader(body))
	if err != nil {
		return -1, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdrs {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: time.Duration(max(timeoutSec, 1)) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return -1, "invoke: " + err.Error()
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if emit != nil {
		emit(fmt.Sprintf("lambda %s invoked (HTTP %d)\n", fn, resp.StatusCode))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return -1, fmt.Sprintf("lambda invoke HTTP %d: %s", resp.StatusCode, trunc(out, 400))
	}
	// Erro de FUNÇÃO (a invocação rodou mas a função lançou) vem no header dedicado.
	if fe := resp.Header.Get("X-Amz-Function-Error"); fe != "" {
		return 1, fmt.Sprintf("lambda function error (%s): %s", fe, trunc(out, 400))
	}
	return 0, fmt.Sprintf("lambda %s OK: %s", fn, trunc(out, 400))
}

// sigv4Headers assina (AWS Signature V4) e devolve os headers a setar. Implementação
// mínima e hermética: stdlib (hmac/sha256), sem dependência da AWS SDK.
func sigv4Headers(method, rawURL, region, service string, payload []byte, ak, sk, token string, now time.Time) (map[string]string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	t := now.UTC()
	amzDate := t.Format("20060102T150405Z")
	dateStamp := t.Format("20060102")
	payloadHash := sha256hex(payload)

	// Headers assinados, em ordem alfabética (host < x-amz-date < x-amz-security-token).
	signed := []string{"host", "x-amz-date"}
	vals := map[string]string{"host": u.Host, "x-amz-date": amzDate}
	if token != "" {
		signed = append(signed, "x-amz-security-token")
		vals["x-amz-security-token"] = token
	}
	sort.Strings(signed)
	var ch strings.Builder
	for _, h := range signed {
		ch.WriteString(h + ":" + vals[h] + "\n")
	}
	signedHeaders := strings.Join(signed, ";")

	uri := u.EscapedPath()
	if uri == "" {
		uri = "/"
	}
	canonicalReq := method + "\n" + uri + "\n" + u.RawQuery + "\n" + ch.String() + "\n" + signedHeaders + "\n" + payloadHash
	scope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256hex([]byte(canonicalReq))

	kDate := hmacSHA256([]byte("AWS4"+sk), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	kSigning := hmacSHA256(kService, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(kSigning, stringToSign))

	auth := "AWS4-HMAC-SHA256 Credential=" + ak + "/" + scope +
		", SignedHeaders=" + signedHeaders + ", Signature=" + signature
	h := map[string]string{"Authorization": auth, "X-Amz-Date": amzDate}
	if token != "" {
		h["X-Amz-Security-Token"] = token
	}
	return h, nil
}

func sha256hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// strFromParamsOrEnv lê params[key]; se vazio, cai pra os.Getenv(env).
func strFromParamsOrEnv(params map[string]interface{}, key, env string) string {
	if v, _ := params[key].(string); v != "" {
		return v
	}
	return os.Getenv(env)
}

func trunc(b []byte, n int) string {
	if s := string(b); len(s) > n {
		return s[:n] + "…"
	} else {
		return s
	}
}
