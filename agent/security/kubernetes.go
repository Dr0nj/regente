package security

import (
	"context"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// KubernetesRule is trusted cell configuration, never accepted from a job.
// No arbitrary mounts, raw manifests or workload secrets are supported.
type KubernetesRule struct {
	NodeSelector     map[string]string `json:"nodeSelector,omitempty"`
	APIServer        string            `json:"apiServer"`
	CAFile           string            `json:"caFile"`
	TokenFile        string            `json:"tokenFile"`
	Namespace        string            `json:"namespace"`
	ServiceAccount   string            `json:"serviceAccount"`
	Images           []string          `json:"images"`
	Commands         []string          `json:"commands"`
	FirewallImage    string            `json:"firewallImage"`
	CPU              string            `json:"cpu"`
	Memory           string            `json:"memory"`
	RetentionSeconds int               `json:"retentionSeconds"`
	MaxSeconds       int               `json:"maxSeconds"`
}

var kubeName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
var digestImage = regexp.MustCompile(`^[a-zA-Z0-9./:_-]+@sha256:[a-f0-9]{64}$`)
var cpuQuantity = regexp.MustCompile(`^[1-9][0-9]{0,3}m$`)
var memoryQuantity = regexp.MustCompile(`^[1-9][0-9]{0,4}Mi$`)

func validateKubernetes(r *KubernetesRule, params map[string]interface{}) error {
	if r == nil {
		return ErrPolicy
	}
	u, err := url.Parse(r.APIServer)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return ErrPolicy
	}
	if !filepath.IsAbs(r.CAFile) || !filepath.IsAbs(r.TokenFile) || !kubeName.MatchString(r.Namespace) || !kubeName.MatchString(r.ServiceAccount) || !digestImage.MatchString(r.FirewallImage) || !cpuQuantity.MatchString(r.CPU) || !memoryQuantity.MatchString(r.Memory) || r.RetentionSeconds < 3600 || r.RetentionSeconds > 6*86400 || r.MaxSeconds < 1 || r.MaxSeconds > 86400 {
		return ErrPolicy
	}
	for k := range params {
		if k != "image" && k != "command" && k != "namespace" {
			return ErrPolicy
		}
	}
	image, _ := params["image"].(string)
	command, _ := params["command"].(string)
	if !digestImage.MatchString(image) || !slices.Contains(r.Images, image) || command == "" || !slices.Contains(r.Commands, command) || params["namespace"] != r.Namespace {
		return ErrPolicy
	}
	return nil
}

func Kubernetes(ctx context.Context) (KubernetesRule, bool) {
	j, ok := ctx.Value(policyKey{}).(Job)
	if !ok || j.Kubernetes == nil {
		return KubernetesRule{}, false
	}
	return *j.Kubernetes, true
}

func IsKubernetes(kind string) bool {
	return strings.EqualFold(kind, "K8S") || strings.EqualFold(kind, "K8S_JOB")
}
