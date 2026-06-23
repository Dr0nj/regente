package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunCloudRunJob_SucceedsImmediate(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"projects/p/operations/op1","done":true}`))
	}))
	defer srv.Close()

	code, out := runCloudRunJob(map[string]interface{}{
		"project": "p", "region": "us-central1", "job": "myjob",
		"token": "ya29.tok", "endpoint": srv.URL,
	}, 5, nil)

	if code != 0 {
		t.Fatalf("esperava exit 0, veio %d — %s", code, out)
	}
	if gotAuth != "Bearer ya29.tok" {
		t.Fatalf("Authorization Bearer errado: %q", gotAuth)
	}
	if !strings.HasSuffix(gotPath, "/jobs/myjob:run") {
		t.Fatalf("path :run errado: %q", gotPath)
	}
}

func TestRunCloudRunJob_PollsThenSucceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodPost { // :run → operação ainda não concluída
			_, _ = w.Write([]byte(`{"name":"projects/p/operations/op1","done":false}`))
			return
		}
		_, _ = w.Write([]byte(`{"name":"projects/p/operations/op1","done":true}`)) // poll → done
	}))
	defer srv.Close()

	code, out := runCloudRunJob(map[string]interface{}{
		"project": "p", "region": "us-central1", "job": "j", "token": "t", "endpoint": srv.URL,
	}, 5, nil)
	if code != 0 {
		t.Fatalf("esperava exit 0 após poll, veio %d — %s", code, out)
	}
}

func TestRunCloudRunJob_Fails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"op","done":true,"error":{"message":"container exited 1"}}`))
	}))
	defer srv.Close()

	if code, _ := runCloudRunJob(map[string]interface{}{
		"project": "p", "region": "r", "job": "j", "token": "t", "endpoint": srv.URL,
	}, 5, nil); code != 1 {
		t.Fatalf("operação com erro deveria ser exit 1, veio %d", code)
	}
}

func TestRunCloudRunJob_MissingParams(t *testing.T) {
	if code, _ := runCloudRunJob(map[string]interface{}{"project": "p"}, 5, nil); code != -1 {
		t.Fatalf("faltando params deveria ser -1, veio %d", code)
	}
}

func TestRunCloudRunJob_MissingToken(t *testing.T) {
	// endpoint não-GCP → metadata não é tentado; sem token → -1.
	if code, _ := runCloudRunJob(map[string]interface{}{
		"project": "p", "region": "r", "job": "j", "endpoint": "http://127.0.0.1:1",
	}, 5, nil); code != -1 {
		t.Fatalf("sem token deveria ser -1, veio %d", code)
	}
}
