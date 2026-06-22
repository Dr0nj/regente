package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunK8sJob_Succeeds(t *testing.T) {
	created := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/jobs") {
			created = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"metadata":{"name":"j"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":{"succeeded":1}}`))
	}))
	defer srv.Close()

	code, out := runK8sJob(map[string]interface{}{
		"image": "busybox", "command": "echo hi", "apiServer": srv.URL, "name": "j",
	}, 5, nil)
	if !created {
		t.Fatal("Job não foi criado (POST não chegou ao mock)")
	}
	if code != 0 {
		t.Fatalf("esperava exit 0 (succeeded), veio %d — %s", code, out)
	}
}

func TestRunK8sJob_Fails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":{"failed":1}}`))
	}))
	defer srv.Close()
	if code, _ := runK8sJob(map[string]interface{}{"image": "x", "apiServer": srv.URL, "name": "j"}, 5, nil); code != 1 {
		t.Fatalf("esperava exit 1 (failed), veio %d", code)
	}
}

func TestRunK8sJob_MissingImage(t *testing.T) {
	if code, _ := runK8sJob(map[string]interface{}{}, 5, nil); code != -1 {
		t.Fatalf("sem image deveria ser -1, veio %d", code)
	}
}
