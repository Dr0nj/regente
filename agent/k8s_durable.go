package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Dr0nj/regente-agent/journal"
	"github.com/Dr0nj/regente-agent/security"
)

type kubeBinding struct {
	Name, Namespace, Server, Fingerprint, UID, PodUID string
	Deadline                                          int64
	Completed                                         bool
	Cleaned                                           bool
	Retention                                         int
}

type kubeStore struct{ j *journal.Journal }

func newKubeStore(j *journal.Journal) (*kubeStore, error) {
	_, err := j.DB.Exec(`CREATE TABLE IF NOT EXISTS kubernetes_bindings(execution_id TEXT PRIMARY KEY REFERENCES journal_entries(execution_id),binding TEXT NOT NULL)`)
	return &kubeStore{j}, err
}
func (s *kubeStore) get(id string) (kubeBinding, error) {
	var raw string
	var b kubeBinding
	err := s.j.DB.QueryRow("SELECT binding FROM kubernetes_bindings WHERE execution_id=?", id).Scan(&raw)
	if err != nil {
		return b, err
	}
	err = json.Unmarshal([]byte(raw), &b)
	return b, err
}
func (s *kubeStore) claim(id string, b kubeBinding) (bool, error) {
	raw, err := json.Marshal(b)
	if err != nil {
		return false, err
	}
	r, err := s.j.DB.Exec("INSERT OR IGNORE INTO kubernetes_bindings(execution_id,binding) VALUES(?,?)", id, string(raw))
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}
func (s *kubeStore) save(id string, b kubeBinding) error {
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	_, err = s.j.DB.Exec("UPDATE kubernetes_bindings SET binding=? WHERE execution_id=?", string(raw), id)
	return err
}
func (s *kubeStore) resumable(e journal.Entry) bool {
	_, err := s.get(e.Envelope.ExecutionID)
	return err == nil
}

type kubeMetadata struct {
	Name            string            `json:"name"`
	UID             string            `json:"uid"`
	Annotations     map[string]string `json:"annotations"`
	OwnerReferences []struct {
		UID        string `json:"uid"`
		Controller bool   `json:"controller"`
	} `json:"ownerReferences"`
}
type kubeJob struct {
	Spec     map[string]interface{} `json:"spec"`
	Metadata kubeMetadata           `json:"metadata"`
	Status   struct {
		Conditions []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"conditions"`
	} `json:"status"`
}
type kubePod struct {
	Metadata kubeMetadata `json:"metadata"`
	Status   struct {
		Phase             string `json:"phase"`
		Reason            string `json:"reason"`
		ContainerStatuses []struct {
			Name         string `json:"name"`
			RestartCount int    `json:"restartCount"`
			State        struct {
				Terminated *struct {
					ExitCode int    `json:"exitCode"`
					Reason   string `json:"reason"`
				} `json:"terminated"`
			} `json:"state"`
		} `json:"containerStatuses"`
	} `json:"status"`
}

type kubeAPI struct {
	client *http.Client
	rule   security.KubernetesRule
}

func newKubeAPI(ctx context.Context, r security.KubernetesRule) (*kubeAPI, error) {
	ca, err := os.ReadFile(r.CAFile)
	if err != nil {
		return nil, errors.New("kubernetes CA unavailable")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("invalid Kubernetes CA")
	}
	c := security.HTTPClient(ctx, 10*time.Second)
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		return nil, security.ErrPolicy
	}
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	return &kubeAPI{c, r}, nil
}
func (a *kubeAPI) request(ctx context.Context, method, path string, body any) (int, []byte, error) {
	// Projected ServiceAccount tokens rotate; never cache bearer material.
	token, err := os.ReadFile(a.rule.TokenFile)
	if err != nil {
		return 0, nil, errors.New("kubernetes credential unavailable")
	}
	if len(token) == 0 || len(token) > 65536 {
		return 0, nil, errors.New("invalid Kubernetes credential")
	}
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = strings.NewReader(string(raw))
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(a.rule.APIServer, "/")+path, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	if method == "PATCH" {
		req.Header.Set("Content-Type", "application/json-patch+json")
	} else {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := a.client.Do(req)
	if err != nil {
		return 0, nil, errors.New("kubernetes transport unavailable")
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	return res.StatusCode, data, err
}
func kubeHash(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func kubePaths(b kubeBinding) (string, string) {
	return "/apis/batch/v1/namespaces/" + b.Namespace + "/jobs/" + b.Name, "/api/v1/namespaces/" + b.Namespace + "/pods"
}
func kubeManifest(e journal.Envelope, r security.KubernetesRule, p map[string]interface{}, seconds int) (kubeBinding, map[string]interface{}) {
	name := "rg-" + kubeHash([]byte(e.ExecutionID + fmt.Sprint(":", e.Fence)))[:40]
	policyBytes, _ := json.Marshal(r)
	fingerprint := kubeHash(append(policyBytes, append(append([]byte{}, e.Definition...), []byte(fmt.Sprintf("%s/%s/%s/%d", e.AgentID, e.ExecutionID, r.Namespace, e.Fence))...)...))
	annotations := map[string]string{"regente.io/execution": e.ExecutionID, "regente.io/fingerprint": fingerprint}
	caps := map[string]interface{}{"drop": []string{"ALL"}}
	resources := map[string]interface{}{"requests": map[string]string{"cpu": r.CPU, "memory": r.Memory}, "limits": map[string]string{"cpu": r.CPU, "memory": r.Memory}}
	firewall := map[string]interface{}{"name": "egress-firewall", "image": r.FirewallImage, "imagePullPolicy": "IfNotPresent", "command": []string{"/bin/sh", "-ec", "iptables -P OUTPUT DROP; iptables -A OUTPUT -o lo -j ACCEPT; ip6tables -P OUTPUT DROP; ip6tables -A OUTPUT -o lo -j ACCEPT"}, "resources": resources, "securityContext": map[string]interface{}{"runAsUser": 0, "runAsNonRoot": false, "readOnlyRootFilesystem": true, "allowPrivilegeEscalation": false, "capabilities": map[string]interface{}{"drop": []string{"ALL"}, "add": []string{"NET_ADMIN", "NET_RAW"}}}}
	main := map[string]interface{}{"name": "main", "image": p["image"], "imagePullPolicy": "IfNotPresent", "command": []string{"/bin/sh", "-c", p["command"].(string)}, "resources": resources, "securityContext": map[string]interface{}{"runAsUser": 10001, "runAsGroup": 10001, "runAsNonRoot": true, "readOnlyRootFilesystem": true, "allowPrivilegeEscalation": false, "capabilities": caps}, "volumeMounts": []map[string]interface{}{{"name": "scratch", "mountPath": "/tmp"}}}
	manifest := map[string]interface{}{"apiVersion": "batch/v1", "kind": "Job", "metadata": map[string]interface{}{"name": name, "namespace": r.Namespace, "annotations": annotations}, "spec": map[string]interface{}{"parallelism": 1, "completions": 1, "backoffLimit": 0, "podReplacementPolicy": "Failed", "activeDeadlineSeconds": seconds + 30, "suspend": false, "template": map[string]interface{}{"metadata": map[string]interface{}{"annotations": annotations}, "spec": map[string]interface{}{"activeDeadlineSeconds": seconds, "nodeSelector": r.NodeSelector, "restartPolicy": "Never", "serviceAccountName": r.ServiceAccount, "automountServiceAccountToken": false, "enableServiceLinks": false, "terminationGracePeriodSeconds": 5, "securityContext": map[string]interface{}{"fsGroup": 10001, "seccompProfile": map[string]string{"type": "RuntimeDefault"}}, "initContainers": []any{firewall}, "containers": []any{main}, "volumes": []map[string]interface{}{{"name": "scratch", "emptyDir": map[string]interface{}{"medium": "Memory", "sizeLimit": "16Mi"}}}}}}}
	if len(r.NodeSelector) == 0 {
		delete(manifest["spec"].(map[string]interface{})["template"].(map[string]interface{})["spec"].(map[string]interface{}), "nodeSelector")
	}
	return kubeBinding{Name: name, Namespace: r.Namespace, Server: r.APIServer, Fingerprint: fingerprint, Deadline: time.Now().Add(time.Duration(seconds+60) * time.Second).Unix(), Retention: r.RetentionSeconds}, manifest
}
func kubeSpecContains(actual, expected any) bool {
	switch want := expected.(type) {
	case map[string]interface{}:
		got, ok := actual.(map[string]interface{})
		if !ok {
			return false
		}
		for k, v := range want {
			if !kubeSpecContains(got[k], v) {
				return false
			}
		}
		return true
	case []interface{}:
		got, ok := actual.([]interface{})
		if !ok || len(got) != len(want) {
			return false
		}
		for i := range want {
			if !kubeSpecContains(got[i], want[i]) {
				return false
			}
		}
		return true
	default:
		a, _ := json.Marshal(actual)
		b, _ := json.Marshal(expected)
		return string(a) == string(b)
	}
}

func kubeValid(obj kubeJob, b kubeBinding, e journal.Envelope) bool {
	return obj.Metadata.Name == b.Name && obj.Metadata.UID != "" && (b.UID == "" || b.UID == obj.Metadata.UID) && obj.Metadata.Annotations["regente.io/fingerprint"] == b.Fingerprint && obj.Metadata.Annotations["regente.io/execution"] == e.ExecutionID
}
func kubePods(ctx context.Context, a *kubeAPI, b kubeBinding) ([]kubePod, error) {
	_, path := kubePaths(b)
	code, raw, err := a.request(ctx, "GET", path+"?labelSelector="+url.QueryEscape("batch.kubernetes.io/controller-uid="+b.UID), nil)
	if err != nil || code != 200 {
		return nil, errors.New("kubernetes pod observation unavailable")
	}
	var list struct {
		Items []kubePod `json:"items"`
	}
	if json.Unmarshal(raw, &list) != nil {
		return nil, errors.New("invalid Kubernetes pod list")
	}
	for _, p := range list.Items {
		owned := false
		for _, o := range p.Metadata.OwnerReferences {
			if o.Controller && o.UID == b.UID {
				owned = true
			}
		}
		if !owned {
			return nil, errors.New("kubernetes pod ownership conflict")
		}
	}
	return list.Items, nil
}
func kubeTerminal(p kubePod) bool { return p.Status.Phase == "Succeeded" || p.Status.Phase == "Failed" }
func kubeProcessStopped(p kubePod) bool {
	if !kubeTerminal(p) || p.Status.Reason == "NodeLost" {
		return false
	}
	for _, c := range p.Status.ContainerStatuses {
		if c.Name == "main" && c.State.Terminated != nil && c.RestartCount == 0 && c.State.Terminated.Reason != "ContainerStatusUnknown" && c.State.Terminated.Reason != "NodeLost" {
			return true
		}
	}
	return false
}
func runDurableKube(ctx context.Context, s *kubeStore, e journal.Envelope, p map[string]interface{}, seconds int, resume bool, emit func(string)) (int, string) {
	unknown := func(msg string) (int, string) { return journal.RemoteUnknownExitCode, msg }
	r, ok := security.Kubernetes(ctx)
	if !ok {
		return unknown("Kubernetes requires explicit local execution policy")
	}
	if seconds < 1 || seconds > r.MaxSeconds {
		return unknown("Kubernetes timeout exceeds local policy")
	}
	a, err := newKubeAPI(ctx, r)
	if err != nil {
		return unknown(err.Error())
	}
	desired, manifest := kubeManifest(e, r, p, seconds)
	manifestJSON, _ := json.Marshal(manifest)
	var normalized map[string]interface{}
	_ = json.Unmarshal(manifestJSON, &normalized)
	expectedSpec := normalized["spec"].(map[string]interface{})
	// Cancellation can shorten this mutable deadline; security fields remain immutable.
	delete(expectedSpec, "activeDeadlineSeconds")
	b, err := s.get(e.ExecutionID)
	fresh := false
	if errors.Is(err, sql.ErrNoRows) && !resume {
		fresh, err = s.claim(e.ExecutionID, desired)
		if err != nil {
			return unknown("Kubernetes binding could not be persisted")
		}
		b, err = s.get(e.ExecutionID)
	}
	if err != nil || b.Server != r.APIServer || b.Namespace != r.Namespace || b.Fingerprint != desired.Fingerprint || b.Cleaned {
		return unknown("Kubernetes binding unavailable or changed; no submission allowed")
	}
	path, _ := kubePaths(b)
	if fresh {
		// Durable intent precedes the ONLY POST. Lost receipt is reconciled with GET.
		code, _, postErr := a.request(ctx, "POST", strings.TrimSuffix(path, "/"+b.Name), manifest)
		if postErr == nil && code >= 400 && code < 500 && code != 409 {
			return -1, fmt.Sprintf("Kubernetes submission rejected HTTP %d", code)
		}
		if emit != nil {
			emit("Kubernetes execution bound to " + b.Namespace + "/" + b.Name + "\n")
		}
	}
	for {
		if ctx.Err() != nil {
			entry, readErr := s.j.Entry(e.ExecutionID)
			if readErr != nil {
				return unknown("journal unavailable during cancellation")
			}
			if !entry.CancelRequested {
				return journal.DeferredObservationExitCode, "controller stopped; external observation deferred"
			}
			return cancelDurableKube(ctx, a, s, e, b)
		}
		code, raw, err := a.request(ctx, "GET", path, nil)
		if err == nil && code == 200 {
			var obj kubeJob
			if json.Unmarshal(raw, &obj) != nil || !kubeValid(obj, b, e) || !kubeSpecContains(obj.Spec, expectedSpec) {
				return unknown("Kubernetes Job identity conflict")
			}
			if b.UID == "" {
				b.UID = obj.Metadata.UID
				if s.save(e.ExecutionID, b) != nil {
					return unknown("Kubernetes UID could not be persisted")
				}
			}
			pods, err := kubePods(ctx, a, b)
			if err == nil {
				if len(pods) > 1 {
					return unknown("Kubernetes created multiple pods; inspect effects before resolution")
				}
				if len(pods) == 1 {
					pod := pods[0]
					if pod.Metadata.UID == "" || (b.PodUID != "" && b.PodUID != pod.Metadata.UID) {
						return unknown("Kubernetes pod identity changed; possible repeated effect")
					}
					if b.PodUID == "" {
						b.PodUID = pod.Metadata.UID
						if s.save(e.ExecutionID, b) != nil {
							return unknown("Kubernetes pod UID could not be persisted")
						}
					}
					terminal := false
					for _, c := range obj.Status.Conditions {
						if (c.Type == "Complete" || c.Type == "Failed") && c.Status == "True" {
							terminal = true
						}
					}
					if terminal && kubeTerminal(pod) {
						if !kubeProcessStopped(pod) {
							return unknown("Kubernetes terminal condition lacks a unique process receipt")
						}
						for _, c := range pod.Status.ContainerStatuses {
							if c.Name == "main" && c.State.Terminated != nil && c.RestartCount == 0 {
								_, pp := kubePaths(b)
								lc, logs, le := a.request(ctx, "GET", pp+"/"+pod.Metadata.Name+"/log?container=main&limitBytes=1048576", nil)
								if le != nil || lc != 200 {
									return unknown("Kubernetes terminated but logs are unavailable; evidence retained")
								}
								b.Completed = true
								if s.save(e.ExecutionID, b) != nil {
									return unknown("Kubernetes completion could not be persisted")
								}
								return c.State.Terminated.ExitCode, string(logs)
							}
						}
						return unknown("Kubernetes terminal condition lacks a unique process receipt")
					}
				} else if b.PodUID != "" {
					return unknown("Kubernetes pod disappeared; no recreation allowed")
				}
			}
		} else if err == nil && code == 404 {
			return unknown("Kubernetes Job absent after durable submission intent; no recreation allowed")
		}
		if time.Now().Unix() > b.Deadline {
			return unknown("Kubernetes observation deadline expired; inspect Job and node state")
		}
		select {
		case <-ctx.Done():
		case <-time.After(500 * time.Millisecond):
		}
	}
}
func cancelDurableKube(parent context.Context, a *kubeAPI, s *kubeStore, e journal.Envelope, b kubeBinding) (int, string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 20*time.Second)
	defer cancel()
	unknown := func() (int, string) {
		return journal.RemoteUnknownExitCode, "Kubernetes cancellation has no confirmed terminal receipt"
	}
	path, _ := kubePaths(b)
	code, raw, err := a.request(ctx, "GET", path, nil)
	var job kubeJob
	if err != nil || code != 200 || json.Unmarshal(raw, &job) != nil || !kubeValid(job, b, e) {
		return unknown()
	}
	b.UID = job.Metadata.UID
	if s.save(e.ExecutionID, b) != nil {
		return unknown()
	}
	pods, err := kubePods(ctx, a, b)
	if err != nil || len(pods) != 1 || b.PodUID == "" || pods[0].Metadata.UID != b.PodUID {
		return unknown()
	}
	_, podPath := kubePaths(b)
	patch := []map[string]interface{}{{"op": "test", "path": "/metadata/uid", "value": b.PodUID}, {"op": "add", "path": "/spec/activeDeadlineSeconds", "value": 1}}
	code, _, err = a.request(ctx, "PATCH", podPath+"/"+pods[0].Metadata.Name, patch)
	if err != nil || code != 200 {
		return unknown()
	}
	for ctx.Err() == nil {
		pods, err := kubePods(ctx, a, b)
		if err == nil && len(pods) == 1 && kubeProcessStopped(pods[0]) && pods[0].Metadata.UID == b.PodUID {
			b.Completed = true
			if s.save(e.ExecutionID, b) != nil {
				return unknown()
			}
			return -1, "Kubernetes pod termination confirmed after deadline reduction; external effects are not rolled back"
		}
		select {
		case <-ctx.Done():
		case <-time.After(500 * time.Millisecond):
		}
	}
	return unknown()
}

// Cleanup uses retained definitions and ACK timestamps; no TTL deletes evidence
// while Regente is unavailable. Tombstones and external UIDs are never removed.
func cleanupKube(ctx context.Context, s *kubeStore) error {
	rows, err := s.j.DB.Query("SELECT k.execution_id,e.acked_at FROM kubernetes_bindings k JOIN journal_entries e ON e.execution_id=k.execution_id WHERE e.state='acked'")
	if err != nil {
		return err
	}
	type item struct {
		id  string
		ack int64
	}
	var items []item
	for rows.Next() {
		var i item
		if err = rows.Scan(&i.id, &i.ack); err != nil {
			rows.Close()
			return err
		}
		items = append(items, i)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, i := range items {
		b, err := s.get(i.id)
		if err != nil {
			return err
		}
		if !b.Completed || b.Cleaned || i.ack == 0 || time.Now().UnixMilli()-i.ack < int64(b.Retention)*1000 {
			continue
		}
		entry, err := s.j.Entry(i.id)
		if err != nil {
			return err
		}
		var d struct {
			ID, Environment, JobType string
			Params                   map[string]interface{} `json:"actionConfig"`
		}
		if json.Unmarshal(entry.Envelope.Definition, &d) != nil {
			// Compaction may have removed an old definition during a long outage.
			// Preserve its external evidence without blocking unrelated executions.
			continue
		}
		prepared, _, _, err := security.Prepare(ctx, *executionPolicy, *jobSecrets, d.Environment, d.ID, d.JobType, d.Params)
		if err != nil {
			continue
		}
		r, ok := security.Kubernetes(prepared)
		if !ok || r.APIServer != b.Server || r.Namespace != b.Namespace {
			continue
		}
		a, err := newKubeAPI(prepared, r)
		if err != nil {
			continue
		}
		path, _ := kubePaths(b)
		code, raw, err := a.request(ctx, "GET", path, nil)
		if err != nil {
			continue
		}
		if code != 404 {
			var job kubeJob
			if code != 200 || json.Unmarshal(raw, &job) != nil || !kubeValid(job, b, entry.Envelope) {
				continue
			}
			code, _, err = a.request(ctx, "DELETE", path, map[string]interface{}{"apiVersion": "v1", "kind": "DeleteOptions", "propagationPolicy": "Foreground", "preconditions": map[string]string{"uid": b.UID}})
			if err != nil || (code != 200 && code != 202) {
				continue
			}
			continue // Observe absence on a later pass, never assume asynchronous deletion.
		}
		b.Cleaned = true
		if err = s.save(i.id, b); err != nil {
			return err
		}
	}
	return nil
}
