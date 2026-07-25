package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// runK8sJob — adapter de nuvem por capability (K8S). Cria um Job no Kubernetes via a
// API REST (sem client-go: leve e hermético de testar), aguarda concluir e reporta o
// resultado. O seam é o roteamento por capability: um agente que anuncia `-caps K8S`
// recebe os jobType=K8S_JOB; o core do server não muda. Mesma forma dos executores
// COMMAND/SCRIPT/HTTP/WASM.
//
// Params (actionConfig):
//
//	image       (obrigatório) — imagem do container
//	command     (opcional)    — roda como ["/bin/sh","-c", command]
//	namespace   (default "default")
//	name        (opcional)    — nome do Job (default regente-<ms>)
//	apiServer   (opcional)    — URL da API k8s; vazio = in-cluster (KUBERNETES_SERVICE_HOST)
//	token       (opcional)    — Bearer da service account; vazio = token in-cluster
//	insecureTLS (opcional)    — "true" pula verificação TLS (clusters self-signed)
func runK8sJob(params map[string]interface{}, timeoutSec int, emit func(string)) (int, string) {
	image, _ := params["image"].(string)
	if image == "" {
		return -1, "missing 'image' param"
	}
	command, _ := params["command"].(string)
	ns, _ := params["namespace"].(string)
	if ns == "" {
		ns = "default"
	}
	name, _ := params["name"].(string)
	if name == "" {
		name = fmt.Sprintf("regente-%d", time.Now().UnixNano()/1e6)
	}

	apiServer, _ := params["apiServer"].(string)
	token, _ := params["token"].(string)
	if apiServer == "" {
		host := os.Getenv("KUBERNETES_SERVICE_HOST")
		if host == "" {
			return -1, "no 'apiServer' and no KUBERNETES_SERVICE_HOST (running outside a cluster)"
		}
		port := os.Getenv("KUBERNETES_SERVICE_PORT")
		if port == "" {
			port = "443"
		}
		apiServer = "https://" + host + ":" + port
		if token == "" {
			if b, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token"); err == nil {
				token = strings.TrimSpace(string(b))
			}
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	// insecureTLS — aceita bool YAML/JSON e "true" string (schema ADV-1: bool).
	insecure := false
	switch v := params["insecureTLS"].(type) {
	case bool:
		insecure = v
	case string:
		insecure = strings.EqualFold(v, "true")
	}
	if insecure {
		client.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	do := func(method, url string, body []byte) (int, []byte, error) {
		var r io.Reader
		if body != nil {
			r = bytes.NewReader(body)
		}
		req, err := http.NewRequest(method, url, r)
		if err != nil {
			return 0, nil, err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, out, nil
	}

	container := map[string]interface{}{"name": "main", "image": image}
	if command != "" {
		container["command"] = []string{"/bin/sh", "-c", command}
	}
	manifest := map[string]interface{}{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata":   map[string]interface{}{"name": name, "namespace": ns},
		"spec": map[string]interface{}{
			"backoffLimit": 0,
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"restartPolicy": "Never",
					"containers":    []interface{}{container},
				},
			},
		},
	}
	body, _ := json.Marshal(manifest)
	base := strings.TrimRight(apiServer, "/")
	jobsURL := fmt.Sprintf("%s/apis/batch/v1/namespaces/%s/jobs", base, ns)

	if code, out, err := do(http.MethodPost, jobsURL, body); err != nil {
		return -1, "create Job: " + err.Error()
	} else if code < 200 || code >= 300 {
		return -1, fmt.Sprintf("create Job HTTP %d: %s", code, k8sTrunc(out, 400))
	}
	if emit != nil {
		emit(fmt.Sprintf("k8s Job %s/%s created\n", ns, name))
	}

	// Poll até succeeded/failed/timeout.
	statusURL := jobsURL + "/" + name
	deadline := time.Now().Add(time.Duration(max(timeoutSec, 1)) * time.Second)
	for {
		sc, sb, err := do(http.MethodGet, statusURL, nil)
		if err == nil && sc >= 200 && sc < 300 {
			var j struct {
				Status struct {
					Succeeded int `json:"succeeded"`
					Failed    int `json:"failed"`
				} `json:"status"`
			}
			if json.Unmarshal(sb, &j) == nil {
				if j.Status.Succeeded > 0 {
					return 0, fmt.Sprintf("k8s Job %s/%s succeeded", ns, name)
				}
				if j.Status.Failed > 0 {
					return 1, fmt.Sprintf("k8s Job %s/%s failed", ns, name)
				}
			}
		}
		if time.Now().After(deadline) {
			return -1, fmt.Sprintf("k8s Job %s/%s timed out after %ds", ns, name, timeoutSec)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func k8sTrunc(b []byte, n int) string {
	if s := string(b); len(s) > n {
		return s[:n] + "…"
	} else {
		return s
	}
}
