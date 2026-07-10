package main

// ADV-8 — cobertura dos executores AWS extras por fakes httptest, no mesmo
// padrão do fake S3 do MFT: cada fake valida a ASSINATURA SigV4 (escopo do
// serviço certo), o protocolo (path REST-JSON do Batch; X-Amz-Target +
// content-type aws-json do Glue/Step Functions) e dirige a máquina de estados
// do poll (N rounds não-terminais antes do terminal).

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fastPoll(t *testing.T) {
	t.Helper()
	old := awsPollEvery
	awsPollEvery = 5 * time.Millisecond
	t.Cleanup(func() { awsPollEvery = old })
}

func awsCreds(endpoint string) map[string]interface{} {
	return map[string]interface{}{
		"region": "us-east-1", "accessKeyId": "AKIA", "secretAccessKey": "secret",
		"endpoint": endpoint,
	}
}

// ─── Batch ───────────────────────────────────────────────────────────────────

func TestRunBatchJob_Succeeds(t *testing.T) {
	fastPoll(t)
	var submitBody, gotAuth string
	var describes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/v1/submitjob":
			gotAuth = r.Header.Get("Authorization")
			submitBody = string(b)
			_, _ = w.Write([]byte(`{"jobId":"j-123","jobName":"n"}`))
		case "/v1/describejobs":
			if !strings.Contains(string(b), `"j-123"`) {
				t.Errorf("describejobs sem o jobId: %s", b)
			}
			st := "RUNNING"
			if describes.Add(1) >= 2 {
				st = "SUCCEEDED"
			}
			_, _ = w.Write([]byte(`{"jobs":[{"status":"` + st + `"}]}`))
		default:
			t.Errorf("path inesperado %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	params := awsCreds(srv.URL)
	params["jobQueue"] = "fila"
	params["jobDefinition"] = "jd"
	params["command"] = "python run.py --full"
	params["env"] = map[string]interface{}{"B": "2", "A": 1}

	var log strings.Builder
	code, out := runBatchJob(params, 10, func(s string) { log.WriteString(s) })
	if code != 0 {
		t.Fatalf("esperava exit 0, veio %d — %s", code, out)
	}
	if !strings.Contains(gotAuth, "/batch/aws4_request") {
		t.Fatalf("assinatura sem escopo batch: %q", gotAuth)
	}
	var sub map[string]interface{}
	if err := json.Unmarshal([]byte(submitBody), &sub); err != nil {
		t.Fatalf("submit body inválido: %v", err)
	}
	ov, _ := sub["containerOverrides"].(map[string]interface{})
	if ov == nil {
		t.Fatalf("submit sem containerOverrides: %s", submitBody)
	}
	if cmd, _ := ov["command"].([]interface{}); len(cmd) != 3 || cmd[0] != "python" {
		t.Fatalf("command não foi splitado: %v", ov["command"])
	}
	// env ordenado por nome (A antes de B), valores coagidos pra string.
	if env, _ := json.Marshal(ov["environment"]); string(env) != `[{"name":"A","value":"1"},{"name":"B","value":"2"}]` {
		t.Fatalf("environment errado: %s", env)
	}
	if !strings.Contains(log.String(), "RUNNING") || !strings.Contains(log.String(), "SUCCEEDED") {
		t.Fatalf("transições não emitidas: %q", log.String())
	}
}

func TestRunBatchJob_FailedNeverOK(t *testing.T) {
	fastPoll(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/submitjob" {
			_, _ = w.Write([]byte(`{"jobId":"j-9"}`))
			return
		}
		// FAILED com exitCode 0 (container morto por fora) — não pode virar OK.
		_, _ = w.Write([]byte(`{"jobs":[{"status":"FAILED","statusReason":"Host EC2 terminated","container":{"exitCode":0}}]}`))
	}))
	defer srv.Close()

	params := awsCreds(srv.URL)
	params["jobQueue"] = "q"
	params["jobDefinition"] = "jd"
	code, out := runBatchJob(params, 10, nil)
	if code != 1 {
		t.Fatalf("FAILED deveria ser exit 1, veio %d — %s", code, out)
	}
	if !strings.Contains(out, "Host EC2 terminated") {
		t.Fatalf("statusReason não veio no output: %q", out)
	}
}

func TestRunBatchJob_TimeoutTerminates(t *testing.T) {
	fastPoll(t)
	var terminated atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/submitjob":
			_, _ = w.Write([]byte(`{"jobId":"j-slow"}`))
		case "/v1/terminatejob":
			terminated.Store(true)
			_, _ = w.Write([]byte(`{}`))
		default:
			_, _ = w.Write([]byte(`{"jobs":[{"status":"RUNNING"}]}`))
		}
	}))
	defer srv.Close()

	params := awsCreds(srv.URL)
	params["jobQueue"] = "q"
	params["jobDefinition"] = "jd"
	code, out := runBatchJob(params, 1, nil)
	if code != -1 || !strings.Contains(out, "timeout") {
		t.Fatalf("esperava timeout -1, veio %d — %s", code, out)
	}
	if !terminated.Load() {
		t.Fatal("timeout deveria mandar TerminateJob best-effort")
	}
}

func TestRunBatchJob_MissingParams(t *testing.T) {
	if code, _ := runBatchJob(map[string]interface{}{"jobQueue": "q"}, 5, nil); code != -1 {
		t.Fatalf("sem jobDefinition deveria ser -1, veio %d", code)
	}
}

// ─── Glue ────────────────────────────────────────────────────────────────────

func TestRunGlueJob_Succeeds(t *testing.T) {
	fastPoll(t)
	var startBody, gotAuth, gotCT string
	var polls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		switch r.Header.Get("X-Amz-Target") {
		case "AWSGlue.StartJobRun":
			gotAuth = r.Header.Get("Authorization")
			gotCT = r.Header.Get("Content-Type")
			startBody = string(b)
			_, _ = w.Write([]byte(`{"JobRunId":"jr_1"}`))
		case "AWSGlue.GetJobRun":
			if !strings.Contains(string(b), `"jr_1"`) {
				t.Errorf("GetJobRun sem RunId: %s", b)
			}
			st := "RUNNING"
			if polls.Add(1) >= 2 {
				st = "SUCCEEDED"
			}
			_, _ = w.Write([]byte(`{"JobRun":{"JobRunState":"` + st + `"}}`))
		default:
			t.Errorf("target inesperado %q", r.Header.Get("X-Amz-Target"))
		}
	}))
	defer srv.Close()

	params := awsCreds(srv.URL)
	params["jobName"] = "cadoc-3040"
	params["arguments"] = map[string]interface{}{"--DATA_REF": "2026-07-10"}
	params["workerType"] = "G.1X"
	params["numberOfWorkers"] = 4

	code, out := runGlueJob(params, 10, nil)
	if code != 0 {
		t.Fatalf("esperava exit 0, veio %d — %s", code, out)
	}
	if !strings.Contains(gotAuth, "/glue/aws4_request") {
		t.Fatalf("assinatura sem escopo glue: %q", gotAuth)
	}
	if gotCT != "application/x-amz-json-1.1" {
		t.Fatalf("content-type aws-json 1.1 esperado, veio %q", gotCT)
	}
	for _, want := range []string{`"JobName":"cadoc-3040"`, `"--DATA_REF":"2026-07-10"`, `"WorkerType":"G.1X"`, `"NumberOfWorkers":4`} {
		if !strings.Contains(startBody, want) {
			t.Fatalf("StartJobRun sem %s: %s", want, startBody)
		}
	}
}

func TestRunGlueJob_Failed(t *testing.T) {
	fastPoll(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Amz-Target") == "AWSGlue.StartJobRun" {
			_, _ = w.Write([]byte(`{"JobRunId":"jr_2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"JobRun":{"JobRunState":"FAILED","ErrorMessage":"OOM no driver"}}`))
	}))
	defer srv.Close()

	params := awsCreds(srv.URL)
	params["jobName"] = "etl"
	code, out := runGlueJob(params, 10, nil)
	if code != 1 || !strings.Contains(out, "OOM no driver") {
		t.Fatalf("esperava exit 1 com ErrorMessage, veio %d — %s", code, out)
	}
}

func TestRunGlueJob_MissingJobName(t *testing.T) {
	if code, _ := runGlueJob(awsCreds("http://x"), 5, nil); code != -1 {
		t.Fatal("sem jobName deveria ser -1")
	}
}

// ─── Step Functions ──────────────────────────────────────────────────────────

func TestRunStepFunctionJob_Succeeds(t *testing.T) {
	fastPoll(t)
	var startBody, gotAuth, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		switch r.Header.Get("X-Amz-Target") {
		case "AWSStepFunctions.StartExecution":
			gotAuth = r.Header.Get("Authorization")
			gotCT = r.Header.Get("Content-Type")
			startBody = string(b)
			_, _ = w.Write([]byte(`{"executionArn":"arn:aws:states:us-east-1:1:execution:sm:e1"}`))
		case "AWSStepFunctions.DescribeExecution":
			if !strings.Contains(string(b), "execution:sm:e1") {
				t.Errorf("DescribeExecution sem o arn: %s", b)
			}
			_, _ = w.Write([]byte(`{"status":"SUCCEEDED","output":"{\"ok\":true}"}`))
		default:
			t.Errorf("target inesperado %q", r.Header.Get("X-Amz-Target"))
		}
	}))
	defer srv.Close()

	params := awsCreds(srv.URL)
	params["stateMachineArn"] = "arn:aws:states:us-east-1:1:stateMachine:sm"
	// input como OBJETO (a UI manda objeto; YAML manda string) — tem que virar JSON.
	params["input"] = map[string]interface{}{"date": "%%ODATE"}

	code, out := runStepFunctionJob(params, 10, nil)
	if code != 0 {
		t.Fatalf("esperava exit 0, veio %d — %s", code, out)
	}
	if !strings.Contains(gotAuth, "/states/aws4_request") {
		t.Fatalf("assinatura sem escopo states: %q", gotAuth)
	}
	if gotCT != "application/x-amz-json-1.0" {
		t.Fatalf("content-type aws-json 1.0 esperado, veio %q", gotCT)
	}
	if !strings.Contains(startBody, `\"date\":\"%%ODATE\"`) && !strings.Contains(startBody, `{\"date\":`) {
		t.Fatalf("input objeto não foi serializado no StartExecution: %s", startBody)
	}
	if !strings.Contains(out, `{"ok":true}`) {
		t.Fatalf("output da execution não veio: %q", out)
	}
}

func TestRunStepFunctionJob_Failed(t *testing.T) {
	fastPoll(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Amz-Target") == "AWSStepFunctions.StartExecution" {
			_, _ = w.Write([]byte(`{"executionArn":"arn:e2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"FAILED","error":"States.TaskFailed","cause":"lambda quebrou"}`))
	}))
	defer srv.Close()

	params := awsCreds(srv.URL)
	params["stateMachineArn"] = "arn:sm"
	code, out := runStepFunctionJob(params, 10, nil)
	if code != 1 || !strings.Contains(out, "States.TaskFailed") || !strings.Contains(out, "lambda quebrou") {
		t.Fatalf("esperava exit 1 com error+cause, veio %d — %s", code, out)
	}
}

func TestRunStepFunctionJob_TimeoutStops(t *testing.T) {
	fastPoll(t)
	var stopped atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Amz-Target") {
		case "AWSStepFunctions.StartExecution":
			_, _ = w.Write([]byte(`{"executionArn":"arn:e3"}`))
		case "AWSStepFunctions.StopExecution":
			stopped.Store(true)
			_, _ = w.Write([]byte(`{}`))
		default:
			_, _ = w.Write([]byte(`{"status":"RUNNING"}`))
		}
	}))
	defer srv.Close()

	params := awsCreds(srv.URL)
	params["stateMachineArn"] = "arn:sm"
	code, out := runStepFunctionJob(params, 1, nil)
	if code != -1 || !strings.Contains(out, "timeout") {
		t.Fatalf("esperava timeout -1, veio %d — %s", code, out)
	}
	if !stopped.Load() {
		t.Fatal("timeout deveria mandar StopExecution best-effort")
	}
}

func TestRunStepFunctionJob_MissingArn(t *testing.T) {
	if code, _ := runStepFunctionJob(map[string]interface{}{}, 5, nil); code != -1 {
		t.Fatal("sem stateMachineArn deveria ser -1")
	}
}

func TestExecuteJob_DispatchesAWSExtras(t *testing.T) {
	// Sem params obrigatórios cada executor devolve -1 com mensagem própria —
	// o que interessa aqui é o dispatch reconhecer os jobTypes (e aliases).
	for _, jt := range []string{"BATCH", "AWS_BATCH", "GLUE", "AWS_GLUE", "STEP_FUNCTION", "STEP_FUNCTIONS"} {
		if _, out := executeJob(jt, map[string]interface{}{}, 1, nil); strings.Contains(out, "unsupported") {
			t.Fatalf("jobType %s não roteado: %s", jt, out)
		}
	}
}
