package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunLambdaJob_Succeeds(t *testing.T) {
	var gotAuth, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer srv.Close()

	code, out := runLambdaJob(map[string]interface{}{
		"function": "myfn", "region": "us-east-1",
		"accessKeyId": "AKIA", "secretAccessKey": "secret",
		"payload": `{"k":"v"}`, "endpoint": srv.URL,
	}, 5, nil)

	if code != 0 {
		t.Fatalf("esperava exit 0, veio %d — %s", code, out)
	}
	if !strings.HasPrefix(gotAuth, "AWS4-HMAC-SHA256 ") {
		t.Fatalf("requisição não foi assinada com SigV4: %q", gotAuth)
	}
	if !strings.HasSuffix(gotPath, "/2015-03-31/functions/myfn/invocations") {
		t.Fatalf("path da Invoke API errado: %q", gotPath)
	}
	if !strings.Contains(gotBody, `"k":"v"`) {
		t.Fatalf("payload não foi encaminhado: %q", gotBody)
	}
}

func TestRunLambdaJob_FunctionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Amz-Function-Error", "Unhandled")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"errorMessage":"boom"}`))
	}))
	defer srv.Close()

	code, _ := runLambdaJob(map[string]interface{}{
		"function": "fn", "region": "us-east-1",
		"accessKeyId": "AKIA", "secretAccessKey": "secret", "endpoint": srv.URL,
	}, 5, nil)
	if code != 1 {
		t.Fatalf("erro de função deveria ser exit 1, veio %d", code)
	}
}

func TestRunLambdaJob_MissingFunction(t *testing.T) {
	if code, _ := runLambdaJob(map[string]interface{}{"region": "us-east-1"}, 5, nil); code != -1 {
		t.Fatalf("sem function deveria ser -1, veio %d", code)
	}
}

func TestRunLambdaJob_MissingCreds(t *testing.T) {
	if code, _ := runLambdaJob(map[string]interface{}{"function": "f", "region": "us-east-1"}, 5, nil); code != -1 {
		t.Fatalf("sem credenciais deveria ser -1, veio %d", code)
	}
}
