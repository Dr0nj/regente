package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGlueTerminalAndQueuedStates(t *testing.T) {
	for _, state := range []string{"SUCCEEDED", "FAILED", "ERROR", "TIMEOUT", "STOPPED", "EXPIRED"} {
		t.Run(state, func(t *testing.T) {
			fastPoll(t)
			var starts, polls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Header.Get("X-Amz-Target") {
				case "AWSGlue.StartJobRun":
					starts.Add(1)
					fmt.Fprint(w, `{"JobRunId":"jr_synthetic"}`)
				case "AWSGlue.GetJobRun":
					st := "WAITING"
					if polls.Add(1) > 1 {
						st = state
					}
					fmt.Fprintf(w, `{"JobRun":{"JobRunState":%q}}`, st)
				default:
					t.Errorf("unexpected request %s", r.Header.Get("X-Amz-Target"))
					w.WriteHeader(500)
				}
			}))
			defer srv.Close()
			params := awsCreds(srv.URL)
			params["jobName"] = "synthetic"
			code, out := runGlueJob(params, 5, nil)
			want := 1
			if state == "SUCCEEDED" {
				want = 0
			}
			if code != want || !strings.Contains(out, state) || starts.Load() != 1 || polls.Load() != 2 {
				t.Fatalf("code=%d out=%s starts=%d polls=%d", code, out, starts.Load(), polls.Load())
			}
		})
	}
}

func TestGlueStopAcknowledgement(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"SuccessfulSubmissions":[{"JobName":"demo","JobRunId":"jr_1"}]}`, "submission accepted"},
		{`{"Errors":[{"JobRunId":"jr_1"}]}`, "not acknowledged"},
		{`{"SuccessfulSubmissions":[{"JobName":"other","JobRunId":"jr_1"}]}`, "not acknowledged"},
		{`{}`, "not acknowledged"}, {`not-json`, "not acknowledged"},
	} {
		if got := glueStopResult(200, []byte(tc.body), nil, "demo", "jr_1"); !strings.Contains(got, tc.want) {
			t.Fatal(got)
		}
	}
	if got := glueStopResult(503, nil, nil, "demo", "jr_1"); !strings.Contains(got, "failed") {
		t.Fatal(got)
	}
	if got := glueStopResult(0, nil, fmt.Errorf("network"), "demo", "jr_1"); !strings.Contains(got, "failed") {
		t.Fatal(got)
	}
}

func TestGlueDoesNotResubmitAmbiguousStart(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, `{}`) }))
	defer srv.Close()
	params := awsCreds(srv.URL)
	params["jobName"] = "synthetic"
	code, _ := runGlueJob(params, 5, nil)
	if code == 0 || calls.Load() != 1 {
		t.Fatalf("ambiguous start retried: %d calls, code %d", calls.Load(), code)
	}
}

func TestGlueTimeoutReportsUnconfirmedStop(t *testing.T) {
	fastPoll(t)
	var starts, stops atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Amz-Target") {
		case "AWSGlue.StartJobRun":
			starts.Add(1)
			fmt.Fprint(w, `{"JobRunId":"jr_synthetic"}`)
		case "AWSGlue.GetJobRun":
			fmt.Fprint(w, `{"JobRun":{"JobRunState":"WAITING"}}`)
		case "AWSGlue.BatchStopJobRun":
			stops.Add(1)
			fmt.Fprint(w, `{"Errors":[{"JobRunId":"jr_synthetic"}]}`)
		default:
			t.Error("unexpected request")
		}
	}))
	defer srv.Close()
	params := awsCreds(srv.URL)
	params["jobName"] = "synthetic"
	code, out := runGlueJob(params, 1, nil)
	if code != -1 || starts.Load() != 1 || stops.Load() != 1 || !strings.Contains(out, "not acknowledged") || !strings.Contains(out, "remote completion unconfirmed") {
		t.Fatalf("%d: %s", code, out)
	}
}
