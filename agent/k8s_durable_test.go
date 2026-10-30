package main

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"github.com/Dr0nj/regente-agent/journal"
	"github.com/Dr0nj/regente-agent/security"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type kubeFixture struct {
	mu                                               sync.Mutex
	job                                              map[string]interface{}
	posts, deletes, patches                          int
	missing, lost, duplicate, changedPod, changedJob bool
	running                                          bool
	tokens                                           []string
	manifest                                         map[string]interface{}
}

func (f *kubeFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens = append(f.tokens, r.Header.Get("Authorization"))
	w.Header().Set("Content-Type", "application/json")
	if r.Method == "POST" {
		f.posts++
		if json.NewDecoder(r.Body).Decode(&f.manifest) != nil {
			panic("manifest")
		}
		meta := f.manifest["metadata"].(map[string]interface{})
		meta["uid"] = "job-uid"
		f.job = map[string]interface{}{"metadata": meta, "spec": f.manifest["spec"], "status": map[string]interface{}{"conditions": []any{map[string]string{"type": "Complete", "status": "True"}}}}
		if f.lost {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.Close()
			}
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(f.job)
		return
	}
	if r.Method == "PATCH" {
		var patch []map[string]interface{}
		if json.NewDecoder(r.Body).Decode(&patch) != nil || len(patch) != 2 || patch[0]["value"] != "pod-uid" || !strings.HasSuffix(r.URL.Path, "/pods/pod-name") {
			w.WriteHeader(409)
			return
		}
		f.patches++
		f.running = false
		json.NewEncoder(w).Encode(f.job)
		return
	}
	if r.Method == "DELETE" {
		f.deletes++
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["preconditions"].(map[string]interface{})["uid"] != "job-uid" {
			panic("UID precondition missing")
		}
		f.missing = true
		w.WriteHeader(202)
		w.Write([]byte(`{}`))
		return
	}
	if strings.HasSuffix(r.URL.Path, "/log") {
		w.Write([]byte("synthetic-pod-output\n"))
		return
	}
	if strings.HasSuffix(r.URL.Path, "/pods") {
		uid := "pod-uid"
		if f.changedPod {
			uid = "replacement-uid"
		}
		status := map[string]interface{}{"phase": "Succeeded", "containerStatuses": []any{map[string]interface{}{"name": "main", "restartCount": 0, "state": map[string]interface{}{"terminated": map[string]int{"exitCode": 0}}}}}
		if f.running {
			status = map[string]interface{}{"phase": "Running"}
		}
		pod := map[string]interface{}{"metadata": map[string]interface{}{"name": "pod-name", "uid": uid, "ownerReferences": []any{map[string]interface{}{"uid": "job-uid", "controller": true}}}, "status": status}
		items := []any{pod}
		if f.duplicate {
			items = append(items, pod)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"items": items})
		return
	}
	if f.job == nil || f.missing {
		w.WriteHeader(404)
		w.Write([]byte(`{}`))
		return
	}
	if f.changedJob {
		f.job["metadata"].(map[string]interface{})["uid"] = "foreign-uid"
	}
	if f.running {
		f.job["status"] = map[string]interface{}{}
	} else {
		f.job["status"] = map[string]interface{}{"conditions": []any{map[string]string{"type": "Complete", "status": "True"}}}
	}
	json.NewEncoder(w).Encode(f.job)
}

type kubeTest struct {
	ctx          context.Context
	s            *kubeStore
	e            journal.Envelope
	p            map[string]interface{}
	r            security.KubernetesRule
	f            *kubeFixture
	path, policy string
}

func setupKube(t *testing.T) *kubeTest {
	t.Helper()
	dir := t.TempDir()
	f := &kubeFixture{}
	srv := httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	ca := filepath.Join(dir, "ca.crt")
	token := filepath.Join(dir, "token")
	policy := filepath.Join(dir, "policy.json")
	os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0600)
	os.WriteFile(token, []byte("original-synthetic-token"), 0600)
	u, _ := url.Parse(srv.URL)
	image := "example.invalid/workload@sha256:" + strings.Repeat("a", 64)
	r := security.KubernetesRule{APIServer: srv.URL, CAFile: ca, TokenFile: token, Namespace: "jobs", ServiceAccount: "executor", Images: []string{image}, Commands: []string{"echo synthetic"}, FirewallImage: "example.invalid/firewall@sha256:" + strings.Repeat("b", 64), CPU: "100m", Memory: "64Mi", RetentionSeconds: 3600, MaxSeconds: 60}
	p := map[string]interface{}{"image": image, "command": "echo synthetic", "namespace": "jobs"}
	pol := security.Policy{Environment: "lab", Jobs: map[string]security.Job{"test": {Types: []string{"K8S_JOB"}, Destinations: []security.Destination{{Host: u.Hostname(), Port: u.Port(), Networks: []string{"127.0.0.0/8"}}}, Kubernetes: &r}}}
	raw, _ := json.Marshal(pol)
	os.WriteFile(policy, raw, 0600)
	ctx, _, _, err := security.Prepare(context.Background(), policy, "", "lab", "test", "K8S_JOB", p)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "journal.db")
	j, err := journal.Open(path, "worker")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	raw, _ = json.Marshal(map[string]interface{}{"id": "test", "jobType": "K8S_JOB", "environment": "lab", "actionConfig": p, "timeout": 30})
	e := journal.Envelope{Protocol: 2, Kind: "dispatch", MessageID: "message", ExecutionID: "execution", OrderID: "order", Attempt: 1, AgentID: "worker", Fence: 1, IdempotencyKey: "execution", Definition: raw}
	if _, err = j.Accept(e); err != nil {
		t.Fatal(err)
	}
	j.AckPhase(e.ExecutionID, "accepted")
	j.BeginEffect(e.ExecutionID)
	j.AckPhase(e.ExecutionID, "started")
	j.Running(e.ExecutionID)
	s, err := newKubeStore(j)
	if err != nil {
		t.Fatal(err)
	}
	return &kubeTest{ctx, s, e, p, r, f, path, policy}
}
func TestDurableKubeLostCreateReceiptAndRestartNoPOST(t *testing.T) {
	x := setupKube(t)
	x.f.lost = true
	if code, out := runDurableKube(x.ctx, x.s, x.e, x.p, 30, false, nil); code != 0 || !strings.Contains(out, "synthetic-pod-output") {
		t.Fatal(code, out)
	}
	b, err := x.s.get(x.e.ExecutionID)
	if err != nil || b.UID != "job-uid" || b.PodUID != "pod-uid" {
		t.Fatal(b, err)
	}
	x.s.j.Close()
	j, err := journal.Open(x.path, "worker")
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	s, err := newKubeStore(j)
	if err != nil {
		t.Fatal(err)
	}
	if code, out := runDurableKube(x.ctx, s, x.e, x.p, 30, true, nil); code != 0 {
		t.Fatal(code, out)
	}
	if x.f.posts != 1 {
		t.Fatal("recreated external Job", x.f.posts)
	}
	spec := x.f.manifest["spec"].(map[string]interface{})
	if spec["backoffLimit"] != float64(0) || spec["podReplacementPolicy"] != "Failed" {
		t.Fatal(spec)
	}
	ps := spec["template"].(map[string]interface{})["spec"].(map[string]interface{})
	if ps["automountServiceAccountToken"] != false || ps["restartPolicy"] != "Never" {
		t.Fatal(ps)
	}
}
func TestDurableKubeIntentWithoutReceiptNeverRecreated(t *testing.T) {
	x := setupKube(t)
	b, _ := kubeManifest(x.e, x.r, x.p, 30)
	if _, err := x.s.claim(x.e.ExecutionID, b); err != nil {
		t.Fatal(err)
	}
	for _, resume := range []bool{false, true} {
		code, _ := runDurableKube(x.ctx, x.s, x.e, x.p, 30, resume, nil)
		if code != journal.RemoteUnknownExitCode {
			t.Fatal(code)
		}
	}
	if x.f.posts != 0 {
		t.Fatal("blind resubmission")
	}
}
func TestDurableKubeMissingOrReplacedObjectsRemainUnknown(t *testing.T) {
	for _, scenario := range []string{"missing", "duplicate", "job-uid", "pod-uid", "spec-drift"} {
		t.Run(scenario, func(t *testing.T) {
			x := setupKube(t)
			if code, out := runDurableKube(x.ctx, x.s, x.e, x.p, 30, false, nil); code != 0 {
				t.Fatal(code, out)
			}
			switch scenario {
			case "missing":
				x.f.missing = true
			case "duplicate":
				x.f.duplicate = true
			case "job-uid":
				x.f.changedJob = true
			case "pod-uid":
				x.f.changedPod = true
			case "spec-drift":
				x.f.job["spec"].(map[string]interface{})["backoffLimit"] = float64(1)
			}
			code, _ := runDurableKube(x.ctx, x.s, x.e, x.p, 30, true, nil)
			if code != journal.RemoteUnknownExitCode || x.f.posts != 1 {
				t.Fatal(code, x.f.posts)
			}
		})
	}
}
func TestDurableKubeRotatesTokenAndDeniesInsecureParams(t *testing.T) {
	x := setupKube(t)
	if code, out := runDurableKube(x.ctx, x.s, x.e, x.p, 30, false, nil); code != 0 {
		t.Fatal(code, out)
	}
	os.WriteFile(x.r.TokenFile, []byte("rotated-synthetic-token"), 0600)
	if code, out := runDurableKube(x.ctx, x.s, x.e, x.p, 30, true, nil); code != 0 {
		t.Fatal(code, out)
	}
	if x.f.tokens[len(x.f.tokens)-1] != "Bearer rotated-synthetic-token" {
		t.Fatal("stale token")
	}
	for _, field := range []string{"token", "apiServer", "insecureTLS", "name", "volumes", "serviceAccountName"} {
		p := map[string]interface{}{}
		for k, v := range x.p {
			p[k] = v
		}
		p[field] = "untrusted"
		if _, _, _, err := security.Prepare(context.Background(), x.policy, "", "lab", "test", "K8S_JOB", p); err == nil {
			t.Fatal("allowed", field)
		}
	}
	for _, field := range []string{"image", "command", "namespace"} {
		p := map[string]interface{}{}
		for k, v := range x.p {
			p[k] = v
		}
		p[field] = "unapproved"
		if _, _, _, err := security.Prepare(context.Background(), x.policy, "", "lab", "test", "K8S_JOB", p); err == nil {
			t.Fatal("allowed", field)
		}
	}
}
func TestDurableKubeCancelRequiresTerminalPodReceipt(t *testing.T) {
	x := setupKube(t)
	if code, out := runDurableKube(x.ctx, x.s, x.e, x.p, 30, false, nil); code != 0 {
		t.Fatal(code, out)
	}
	x.f.running = true
	x.s.j.DB.Exec("UPDATE journal_entries SET cancel_requested=1 WHERE execution_id=?", x.e.ExecutionID)
	ctx, cancel := context.WithCancel(x.ctx)
	cancel()
	code, out := runDurableKube(ctx, x.s, x.e, x.p, 30, true, nil)
	if code != -1 || x.f.patches != 1 || !strings.Contains(out, "termination confirmed") {
		t.Fatal(code, out, x.f.patches)
	}
}
func TestDurableKubeTerminalPhaseWithoutProcessReceiptIsNotStopped(t *testing.T) {
	for _, raw := range []string{
		`{"status":{"phase":"Failed","reason":"NodeLost"}}`,
		`{"status":{"phase":"Failed","containerStatuses":[{"name":"main","restartCount":0,"state":{"terminated":{"exitCode":137,"reason":"ContainerStatusUnknown"}}}]}}`,
		`{"status":{"phase":"Failed","containerStatuses":[{"name":"main","restartCount":1,"state":{"terminated":{"exitCode":137}}}]}}`,
		`{"status":{"phase":"Running","containerStatuses":[{"name":"main","restartCount":0,"state":{"terminated":{"exitCode":0}}}]}}`,
	} {
		var pod kubePod
		if err := json.Unmarshal([]byte(raw), &pod); err != nil {
			t.Fatal(err)
		}
		if kubeProcessStopped(pod) {
			t.Fatal("unconfirmed or repeated process accepted as stopped", raw)
		}
	}
}
func TestDurableKubeCleanupRequiresACKRetentionAndUID(t *testing.T) {
	x := setupKube(t)
	oldPolicy := *executionPolicy
	*executionPolicy = x.policy
	defer func() { *executionPolicy = oldPolicy }()
	if code, out := runDurableKube(x.ctx, x.s, x.e, x.p, 30, false, nil); code != 0 {
		t.Fatal(code, out)
	}
	if err := cleanupKube(x.ctx, x.s); err != nil {
		t.Fatal(err)
	}
	if x.f.deletes != 0 {
		t.Fatal("deleted before ACK")
	}
	x.s.j.Result(x.e.ExecutionID, 0, "done")
	x.s.j.Ack(x.e.ExecutionID)
	cleanupKube(x.ctx, x.s)
	if x.f.deletes != 0 {
		t.Fatal("deleted before retention")
	}
	x.s.j.DB.Exec("UPDATE journal_entries SET acked_at=? WHERE execution_id=?", time.Now().Add(-2*time.Hour).UnixMilli(), x.e.ExecutionID)
	if err := cleanupKube(x.ctx, x.s); err != nil {
		t.Fatal(err)
	}
	if x.f.deletes != 1 {
		t.Fatal(x.f.deletes)
	}
	if err := cleanupKube(x.ctx, x.s); err != nil {
		t.Fatal(err)
	}
	b, _ := x.s.get(x.e.ExecutionID)
	if !b.Cleaned {
		t.Fatal("tombstone not retained")
	}
	if code, _ := runDurableKube(x.ctx, x.s, x.e, x.p, 30, true, nil); code != journal.RemoteUnknownExitCode || x.f.posts != 1 {
		t.Fatal("cleaned execution recreated")
	}
}

func TestDurableKubeCompactedDefinitionPreservesEvidenceWithoutBlockingAgent(t *testing.T) {
	x := setupKube(t)
	if code, out := runDurableKube(x.ctx, x.s, x.e, x.p, 30, false, nil); code != 0 {
		t.Fatal(code, out)
	}
	if err := x.s.j.Result(x.e.ExecutionID, 0, "done"); err != nil {
		t.Fatal(err)
	}
	if err := x.s.j.Ack(x.e.ExecutionID); err != nil {
		t.Fatal(err)
	}
	if _, err := x.s.j.DB.Exec("UPDATE journal_entries SET acked_at=? WHERE execution_id=?", time.Now().Add(-8*24*time.Hour).UnixMilli(), x.e.ExecutionID); err != nil {
		t.Fatal(err)
	}
	if err := x.s.j.Compact(time.Now().Add(-7 * 24 * time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := cleanupKube(x.ctx, x.s); err != nil {
		t.Fatal(err)
	}
	if x.f.deletes != 0 {
		t.Fatal("deleted evidence without retained policy definition")
	}
}
