package main

// ADV-8 — Executores AWS extras: BATCH (AWS Batch), GLUE (AWS Glue) e
// STEP_FUNCTION (AWS Step Functions). Mesmo seam do LAMBDA (aws.go): SigV4 da
// stdlib, sem aws-sdk-go; um agente com `-caps BATCH,GLUE,STEP_FUNCTION`
// recebe esses jobTypes e o core do server não muda. Os três seguem o mesmo
// contrato submit→poll: disparam o trabalho, acompanham o status na cadência
// awsPollEvery até terminal ou deadline do job, e no timeout mandam um stop
// best-effort (TerminateJob / BatchStopJobRun / StopExecution) pra não deixar
// trabalho órfão rodando na nuvem.
//
// Validação em conta AWS paga está fora de escopo por decisão (roadmap ADV-8);
// a cobertura é por fakes httptest que verificam assinatura, protocolo
// (REST-JSON do Batch, aws-json 1.1 do Glue, aws-json 1.0 do Step Functions)
// e a máquina de estados do poll.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// awsPollEvery — cadência do poll de status (var pra teste encurtar).
var awsPollEvery = 3 * time.Second

// awsConn — endpoint + credenciais resolvidos de params/env, comum aos três.
type awsConn struct {
	base    string // URL base (endpoint override ou default do serviço)
	region  string
	service string // escopo SigV4 (batch | glue | states)
	ak, sk  string
	token   string
}

// awsConnFrom resolve region/credenciais (params > env, igual ao LAMBDA) e o
// endpoint do serviço. errMsg != "" quando falta algo obrigatório.
func awsConnFrom(params map[string]interface{}, service, defaultHostPrefix string) (awsConn, string) {
	region := strFromParamsOrEnv(params, "region", "AWS_REGION")
	if region == "" {
		return awsConn{}, "missing 'region' (param ou AWS_REGION)"
	}
	ak := strFromParamsOrEnv(params, "accessKeyId", "AWS_ACCESS_KEY_ID")
	sk := strFromParamsOrEnv(params, "secretAccessKey", "AWS_SECRET_ACCESS_KEY")
	if ak == "" || sk == "" {
		return awsConn{}, "missing AWS credentials (accessKeyId/secretAccessKey ou env)"
	}
	base, _ := params["endpoint"].(string)
	if base == "" {
		base = "https://" + defaultHostPrefix + "." + region + ".amazonaws.com"
	}
	return awsConn{
		base: strings.TrimRight(base, "/"), region: region, service: service,
		ak: ak, sk: sk, token: strFromParamsOrEnv(params, "sessionToken", "AWS_SESSION_TOKEN"),
	}, ""
}

// awsPost assina (SigV4) e executa um POST JSON. target != "" vira o header
// X-Amz-Target (protocolo aws-json 1.0/1.1 do Glue/Step Functions; o Batch é
// REST-JSON e usa path + contentType application/json).
func (c awsConn) awsPost(path, target, contentType string, body []byte) (int, []byte, error) {
	u := c.base + path
	hdrs, err := sigv4Headers(http.MethodPost, u, c.region, c.service, body, c.ak, c.sk, c.token, time.Now())
	if err != nil {
		return 0, nil, fmt.Errorf("sigv4: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", contentType)
	if target != "" {
		req.Header.Set("X-Amz-Target", target)
	}
	for k, v := range hdrs {
		req.Header.Set(k, v)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out, nil
}

// jsonStrParam lê um param JSON que a UI manda como objeto e o YAML como
// string (mesma dualidade do payload do LAMBDA). fallback quando vazio.
func jsonStrParam(params map[string]interface{}, key, fallback string) string {
	if s, _ := params[key].(string); s != "" {
		return s
	}
	if obj, ok := params[key].(map[string]interface{}); ok && len(obj) > 0 {
		if b, err := json.Marshal(obj); err == nil {
			return string(b)
		}
	}
	return fallback
}

// strMapParam coage um param mapa (map[string]interface{}) pra map[string]string.
func strMapParam(params map[string]interface{}, key string) map[string]string {
	obj, ok := params[key].(map[string]interface{})
	if !ok || len(obj) == 0 {
		return nil
	}
	out := make(map[string]string, len(obj))
	for k, v := range obj {
		out[k] = fmt.Sprint(v)
	}
	return out
}

// ─── AWS Batch ───────────────────────────────────────────────────────────────

// runBatchJob — SubmitJob + poll DescribeJobs até SUCCEEDED/FAILED/timeout.
//
// Params (actionConfig):
//
//	jobQueue        (obrigatório) — fila do AWS Batch
//	jobDefinition   (obrigatório) — job definition registrada
//	jobName         (opcional)    — nome da submissão (default regente-<unix>)
//	command         (opcional)    — override do command do container (split por espaços)
//	env             (opcional)    — mapa nome→valor (containerOverrides.environment)
//	parameters      (opcional)    — mapa de parameters da job definition
//	region/accessKeyId/secretAccessKey/sessionToken/endpoint — como no LAMBDA
func runBatchJob(params map[string]interface{}, timeoutSec int, emit func(string)) (int, string) {
	queue, _ := params["jobQueue"].(string)
	def, _ := params["jobDefinition"].(string)
	if queue == "" || def == "" {
		return -1, "missing 'jobQueue'/'jobDefinition' param"
	}
	conn, errMsg := awsConnFrom(params, "batch", "batch")
	if errMsg != "" {
		return -1, errMsg
	}

	name, _ := params["jobName"].(string)
	if name == "" {
		name = fmt.Sprintf("regente-%d", time.Now().Unix())
	}
	submit := map[string]interface{}{"jobName": name, "jobQueue": queue, "jobDefinition": def}
	overrides := map[string]interface{}{}
	if cmd, _ := params["command"].(string); cmd != "" {
		overrides["command"] = strings.Fields(cmd)
	}
	if env := strMapParam(params, "env"); env != nil {
		keys := make([]string, 0, len(env))
		for k := range env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		list := make([]map[string]string, 0, len(env))
		for _, k := range keys {
			list = append(list, map[string]string{"name": k, "value": env[k]})
		}
		overrides["environment"] = list
	}
	if len(overrides) > 0 {
		submit["containerOverrides"] = overrides
	}
	if p := strMapParam(params, "parameters"); p != nil {
		submit["parameters"] = p
	}

	body, _ := json.Marshal(submit)
	sc, out, err := conn.awsPost("/v1/submitjob", "", "application/json", body)
	if err != nil {
		return -1, "submit: " + err.Error()
	}
	if sc < 200 || sc >= 300 {
		return -1, fmt.Sprintf("batch SubmitJob HTTP %d: %s", sc, trunc(out, 400))
	}
	var sub struct {
		JobID string `json:"jobId"`
	}
	_ = json.Unmarshal(out, &sub)
	if sub.JobID == "" {
		return -1, "batch SubmitJob: resposta sem jobId: " + trunc(out, 400)
	}
	if emit != nil {
		emit(fmt.Sprintf("batch job %s submetido (id=%s, queue=%s)\n", name, sub.JobID, queue))
	}

	deadline := time.Now().Add(time.Duration(max(timeoutSec, 1)) * time.Second)
	last := ""
	for {
		if time.Now().After(deadline) {
			// best-effort: não deixa o job órfão consumindo a fila.
			tb, _ := json.Marshal(map[string]string{"jobId": sub.JobID, "reason": "regente: job timeout"})
			_, _, _ = conn.awsPost("/v1/terminatejob", "", "application/json", tb)
			return -1, fmt.Sprintf("batch job %s timeout após %ds (último status %s; TerminateJob enviado)", sub.JobID, timeoutSec, last)
		}
		db, _ := json.Marshal(map[string][]string{"jobs": {sub.JobID}})
		sc, out, err := conn.awsPost("/v1/describejobs", "", "application/json", db)
		if err == nil && sc >= 200 && sc < 300 {
			var desc struct {
				Jobs []struct {
					Status       string `json:"status"`
					StatusReason string `json:"statusReason"`
					Container    struct {
						ExitCode *int   `json:"exitCode"`
						Reason   string `json:"reason"`
					} `json:"container"`
				} `json:"jobs"`
			}
			_ = json.Unmarshal(out, &desc)
			if len(desc.Jobs) > 0 {
				j := desc.Jobs[0]
				if j.Status != last {
					last = j.Status
					if emit != nil {
						emit(fmt.Sprintf("batch job %s: %s\n", sub.JobID, j.Status))
					}
				}
				switch j.Status {
				case "SUCCEEDED":
					return 0, fmt.Sprintf("batch job %s SUCCEEDED", sub.JobID)
				case "FAILED":
					msg := j.StatusReason
					if j.Container.Reason != "" {
						msg += " — " + j.Container.Reason
					}
					// exitCode 0 com status FAILED existe (container morto por
					// fora) — nunca pode virar OK.
					if j.Container.ExitCode != nil && *j.Container.ExitCode != 0 {
						return *j.Container.ExitCode, fmt.Sprintf("batch job %s FAILED (exit %d): %s", sub.JobID, *j.Container.ExitCode, msg)
					}
					return 1, fmt.Sprintf("batch job %s FAILED: %s", sub.JobID, msg)
				}
			}
		}
		time.Sleep(awsPollEvery)
	}
}

// ─── AWS Glue ────────────────────────────────────────────────────────────────

// runGlueJob — StartJobRun + poll GetJobRun (protocolo aws-json 1.1).
//
// Params (actionConfig):
//
//	jobName         (obrigatório) — nome do Glue job
//	arguments       (opcional)    — mapa "--CHAVE" → valor
//	workerType      (opcional)    — ex.: G.1X
//	numberOfWorkers (opcional)
//	region/accessKeyId/secretAccessKey/sessionToken/endpoint — como no LAMBDA
func runGlueJob(params map[string]interface{}, timeoutSec int, emit func(string)) (int, string) {
	jobName, _ := params["jobName"].(string)
	if jobName == "" {
		return -1, "missing 'jobName' param"
	}
	conn, errMsg := awsConnFrom(params, "glue", "glue")
	if errMsg != "" {
		return -1, errMsg
	}

	start := map[string]interface{}{"JobName": jobName}
	if args := strMapParam(params, "arguments"); args != nil {
		start["Arguments"] = args
	}
	if wt, _ := params["workerType"].(string); wt != "" {
		start["WorkerType"] = wt
	}
	if n, ok := toInt(params["numberOfWorkers"]); ok && n > 0 {
		start["NumberOfWorkers"] = n
	}

	const ct = "application/x-amz-json-1.1"
	body, _ := json.Marshal(start)
	sc, out, err := conn.awsPost("/", "AWSGlue.StartJobRun", ct, body)
	if err != nil {
		return -1, "start: " + err.Error()
	}
	if sc < 200 || sc >= 300 {
		return -1, fmt.Sprintf("glue StartJobRun HTTP %d: %s", sc, trunc(out, 400))
	}
	var run struct {
		JobRunID string `json:"JobRunId"`
	}
	_ = json.Unmarshal(out, &run)
	if run.JobRunID == "" {
		return -1, "glue StartJobRun: resposta sem JobRunId: " + trunc(out, 400)
	}
	if emit != nil {
		emit(fmt.Sprintf("glue job %s: run %s disparado\n", jobName, run.JobRunID))
	}

	deadline := time.Now().Add(time.Duration(max(timeoutSec, 1)) * time.Second)
	last := ""
	for {
		if time.Now().After(deadline) {
			tb, _ := json.Marshal(map[string]interface{}{"JobName": jobName, "JobRunIds": []string{run.JobRunID}})
			_, _, _ = conn.awsPost("/", "AWSGlue.BatchStopJobRun", ct, tb)
			return -1, fmt.Sprintf("glue run %s timeout após %ds (último status %s; BatchStopJobRun enviado)", run.JobRunID, timeoutSec, last)
		}
		gb, _ := json.Marshal(map[string]string{"JobName": jobName, "RunId": run.JobRunID})
		sc, out, err := conn.awsPost("/", "AWSGlue.GetJobRun", ct, gb)
		if err == nil && sc >= 200 && sc < 300 {
			var got struct {
				JobRun struct {
					JobRunState  string `json:"JobRunState"`
					ErrorMessage string `json:"ErrorMessage"`
				} `json:"JobRun"`
			}
			_ = json.Unmarshal(out, &got)
			st := got.JobRun.JobRunState
			if st != "" && st != last {
				last = st
				if emit != nil {
					emit(fmt.Sprintf("glue run %s: %s\n", run.JobRunID, st))
				}
			}
			switch st {
			case "SUCCEEDED":
				return 0, fmt.Sprintf("glue run %s SUCCEEDED", run.JobRunID)
			case "FAILED", "ERROR", "TIMEOUT", "STOPPED":
				return 1, fmt.Sprintf("glue run %s %s: %s", run.JobRunID, st, got.JobRun.ErrorMessage)
			}
		}
		time.Sleep(awsPollEvery)
	}
}

// ─── AWS Step Functions ──────────────────────────────────────────────────────

// runStepFunctionJob — StartExecution + poll DescribeExecution (aws-json 1.0).
//
// Params (actionConfig):
//
//	stateMachineArn (obrigatório) — ARN da state machine
//	input           (opcional)    — JSON de input (string ou objeto; default {})
//	name            (opcional)    — nome da execution (default: AWS gera)
//	region/accessKeyId/secretAccessKey/sessionToken/endpoint — como no LAMBDA
func runStepFunctionJob(params map[string]interface{}, timeoutSec int, emit func(string)) (int, string) {
	arn, _ := params["stateMachineArn"].(string)
	if arn == "" {
		return -1, "missing 'stateMachineArn' param"
	}
	conn, errMsg := awsConnFrom(params, "states", "states")
	if errMsg != "" {
		return -1, errMsg
	}

	start := map[string]interface{}{"stateMachineArn": arn, "input": jsonStrParam(params, "input", "{}")}
	if name, _ := params["name"].(string); name != "" {
		start["name"] = name
	}

	const ct = "application/x-amz-json-1.0"
	body, _ := json.Marshal(start)
	sc, out, err := conn.awsPost("/", "AWSStepFunctions.StartExecution", ct, body)
	if err != nil {
		return -1, "start: " + err.Error()
	}
	if sc < 200 || sc >= 300 {
		return -1, fmt.Sprintf("step function StartExecution HTTP %d: %s", sc, trunc(out, 400))
	}
	var exec struct {
		ExecutionArn string `json:"executionArn"`
	}
	_ = json.Unmarshal(out, &exec)
	if exec.ExecutionArn == "" {
		return -1, "step function StartExecution: resposta sem executionArn: " + trunc(out, 400)
	}
	if emit != nil {
		emit(fmt.Sprintf("step function: execution %s iniciada\n", exec.ExecutionArn))
	}

	deadline := time.Now().Add(time.Duration(max(timeoutSec, 1)) * time.Second)
	for {
		if time.Now().After(deadline) {
			tb, _ := json.Marshal(map[string]string{
				"executionArn": exec.ExecutionArn, "error": "Regente.Timeout", "cause": "job timeout",
			})
			_, _, _ = conn.awsPost("/", "AWSStepFunctions.StopExecution", ct, tb)
			return -1, fmt.Sprintf("step function %s timeout após %ds (StopExecution enviado)", exec.ExecutionArn, timeoutSec)
		}
		db, _ := json.Marshal(map[string]string{"executionArn": exec.ExecutionArn})
		sc, out, err := conn.awsPost("/", "AWSStepFunctions.DescribeExecution", ct, db)
		if err == nil && sc >= 200 && sc < 300 {
			var desc struct {
				Status string `json:"status"`
				Output string `json:"output"`
				Error  string `json:"error"`
				Cause  string `json:"cause"`
			}
			_ = json.Unmarshal(out, &desc)
			switch desc.Status {
			case "SUCCEEDED":
				return 0, fmt.Sprintf("step function SUCCEEDED: %s", trunc([]byte(desc.Output), 400))
			case "FAILED", "TIMED_OUT", "ABORTED":
				msg := desc.Error
				if desc.Cause != "" {
					msg += " — " + desc.Cause
				}
				return 1, fmt.Sprintf("step function %s: %s", desc.Status, msg)
			}
		}
		time.Sleep(awsPollEvery)
	}
}
