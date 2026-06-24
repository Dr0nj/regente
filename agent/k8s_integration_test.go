package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Integração REAL do adapter K8S contra um apiserver de verdade (kind / Docker
// Desktop / EKS / GKE). Os testes unitários (k8s_test.go) cobrem o protocolo
// contra httptest; ESTE prova que runK8sJob cria um Job real, o kubelet roda o
// container e o status é lido de volta — fechando o gap "validar em cluster REAL".
//
// Skip por padrão. Para rodar:
//
//	kind create cluster --name regente
//	API=$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')
//	kubectl create serviceaccount regente-runner
//	kubectl create clusterrolebinding regente-runner --clusterrole=cluster-admin \
//	    --serviceaccount=default:regente-runner
//	TOKEN=$(kubectl create token regente-runner --duration=3600s)
//	REGENTE_TEST_K8S_API="$API" REGENTE_TEST_K8S_TOKEN="$TOKEN" \
//	    go test ./agent -run TestIntegrationK8s -v
func k8sIntegrationParams(t *testing.T) (api, token string) {
	t.Helper()
	api = os.Getenv("REGENTE_TEST_K8S_API")
	token = os.Getenv("REGENTE_TEST_K8S_TOKEN")
	if api == "" || token == "" {
		t.Skip("set REGENTE_TEST_K8S_API e REGENTE_TEST_K8S_TOKEN para rodar a integração k8s real")
	}
	return api, token
}

// Cria um Job real (busybox echo) e exige succeeded (exit 0).
func TestIntegrationK8sJob_Succeeds(t *testing.T) {
	api, token := k8sIntegrationParams(t)
	name := fmt.Sprintf("regente-it-ok-%d", time.Now().UnixNano()/1e6)
	var log strings.Builder
	emit := func(s string) { log.WriteString(s) }

	code, out := runK8sJob(map[string]interface{}{
		"image":       "busybox:1.36",
		"command":     "echo hello-from-regente && sleep 1",
		"apiServer":   api,
		"token":       token,
		"name":        name,
		"insecureTLS": "true", // kind/minikube usam apiserver self-signed
	}, 90, emit)

	t.Logf("emit: %q | resultado: %s", log.String(), out)
	if code != 0 {
		t.Fatalf("Job real deveria suceder (exit 0), veio %d — %s", code, out)
	}
	if !strings.Contains(out, "succeeded") {
		t.Fatalf("esperava 'succeeded' no resultado, veio: %s", out)
	}
}

// Cria um Job real que termina com erro (exit 7) e exige failed (exit 1).
func TestIntegrationK8sJob_Fails(t *testing.T) {
	api, token := k8sIntegrationParams(t)
	name := fmt.Sprintf("regente-it-fail-%d", time.Now().UnixNano()/1e6)

	code, out := runK8sJob(map[string]interface{}{
		"image":       "busybox:1.36",
		"command":     "echo vou-falhar && exit 7",
		"apiServer":   api,
		"token":       token,
		"name":        name,
		"insecureTLS": "true",
	}, 90, nil)

	if code != 1 {
		t.Fatalf("Job que sai com 7 deveria reportar failed (exit 1), veio %d — %s", code, out)
	}
	if !strings.Contains(out, "failed") {
		t.Fatalf("esperava 'failed' no resultado, veio: %s", out)
	}
}

// Credencial inválida contra apiserver real → erro de criação (não trava).
func TestIntegrationK8sJob_BadToken(t *testing.T) {
	api, _ := k8sIntegrationParams(t)
	code, out := runK8sJob(map[string]interface{}{
		"image":       "busybox:1.36",
		"apiServer":   api,
		"token":       "token-invalido",
		"name":        "regente-it-bad",
		"insecureTLS": "true",
	}, 20, nil)
	if code != -1 {
		t.Fatalf("token inválido deveria falhar na criação (-1), veio %d — %s", code, out)
	}
}
