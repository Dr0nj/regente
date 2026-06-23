package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// runCloudRunJob — adapter de nuvem por capability (GCP_RUN). Dispara a execução de um
// Cloud Run Job pela Run Admin API v2 (`POST …:run`) e acompanha a Operation retornada
// até concluir. Auth = Bearer OAuth token. Mesmo seam dos demais executores.
//
// Params (actionConfig):
//
//	project  (obrigatório) — id do projeto GCP
//	region   (obrigatório) — location (ex.: us-central1)
//	job      (obrigatório) — nome do Cloud Run Job
//	token    (opcional)    — OAuth bearer; default env GOOGLE_OAUTH_TOKEN, ou metadata server in-GCP
//	endpoint (opcional)    — override base (testes); default https://run.googleapis.com
func runCloudRunJob(params map[string]interface{}, timeoutSec int, emit func(string)) (int, string) {
	project, _ := params["project"].(string)
	region, _ := params["region"].(string)
	job, _ := params["job"].(string)
	if project == "" || region == "" || job == "" {
		return -1, "missing 'project'/'region'/'job' param"
	}
	base, _ := params["endpoint"].(string)
	if base == "" {
		base = "https://run.googleapis.com"
	}
	base = strings.TrimRight(base, "/")

	token := strFromParamsOrEnv(params, "token", "GOOGLE_OAUTH_TOKEN")
	if token == "" {
		token = gcpMetadataToken(base) // só dentro do GCP; em teste passamos o token
	}
	if token == "" {
		return -1, "missing GCP OAuth token (param token / env GOOGLE_OAUTH_TOKEN / metadata server)"
	}

	client := &http.Client{Timeout: 30 * time.Second}
	do := func(method, u string, body []byte) (int, []byte, error) {
		var r io.Reader
		if body != nil {
			r = bytes.NewReader(body)
		}
		req, err := http.NewRequest(method, u, r)
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
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

	runURL := fmt.Sprintf("%s/v2/projects/%s/locations/%s/jobs/%s:run", base, project, region, job)
	code, out, err := do(http.MethodPost, runURL, []byte("{}"))
	if err != nil {
		return -1, "run: " + err.Error()
	}
	if code < 200 || code >= 300 {
		return -1, fmt.Sprintf("cloud run :run HTTP %d: %s", code, trunc(out, 400))
	}

	var op cloudRunOp
	_ = json.Unmarshal(out, &op)
	if emit != nil {
		emit(fmt.Sprintf("cloud run job %s: execução disparada (op=%s)\n", job, op.Name))
	}
	if op.Name == "" {
		// Sem operação retornada → aceito (fire-and-forget).
		return 0, fmt.Sprintf("cloud run job %s disparado", job)
	}

	// Poll a Operation até done/timeout.
	opURL := base + "/v2/" + strings.TrimPrefix(op.Name, "/")
	deadline := time.Now().Add(time.Duration(max(timeoutSec, 1)) * time.Second)
	for {
		if op.Done {
			if op.Error != nil && op.Error.Message != "" {
				return 1, fmt.Sprintf("cloud run job %s failed: %s", job, op.Error.Message)
			}
			return 0, fmt.Sprintf("cloud run job %s succeeded", job)
		}
		if time.Now().After(deadline) {
			return -1, fmt.Sprintf("cloud run job %s timeout após %ds", job, timeoutSec)
		}
		time.Sleep(500 * time.Millisecond)
		if sc, sb, e := do(http.MethodGet, opURL, nil); e == nil && sc >= 200 && sc < 300 {
			op = cloudRunOp{}
			_ = json.Unmarshal(sb, &op)
		}
	}
}

type cloudRunOp struct {
	Name  string `json:"name"`
	Done  bool   `json:"done"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// gcpMetadataToken pega um OAuth token do metadata server (só funciona DENTRO do GCP).
// Em teste, base != run.googleapis.com → retorna "" sem tentar (evita timeout).
func gcpMetadataToken(base string) string {
	if !strings.Contains(base, "run.googleapis.com") {
		return ""
	}
	req, err := http.NewRequest(http.MethodGet,
		"http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Metadata-Flavor", "Google")
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var t struct {
		AccessToken string `json:"access_token"`
	}
	if json.NewDecoder(resp.Body).Decode(&t) != nil {
		return ""
	}
	return t.AccessToken
}
