package scheduler

import (
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/runtimeprofile"
	"testing"
)

func TestRemoteSSHRequiresBridgeCapabilityPreservesControlPlaneDeny(t *testing.T) {
	f := durableTest(t)
	def := domain.JobDefinition{JobType: "SSH_REMOTE", AgentID: "worker"}
	if _, err := f.s.durableAgent(def); err == nil {
		t.Fatal("COMMAND agent accepted remote SSH")
	}
	if _, err := f.d.Exec("UPDATE machine_principals SET capabilities='SSH_REMOTE,EXECUTION_V2' WHERE agent_id='worker'"); err != nil {
		t.Fatal(err)
	}
	id, err := f.s.durableAgent(def)
	if err != nil || id != "worker" {
		t.Fatal(id, err)
	}
	policy := runtimeprofile.Config{Profile: "production", Environment: "lab", ControlPlane: "deny", ExecutionMode: "durable"}
	def.Environment = "lab"
	if err := policy.ExecutionError(def); err != "" {
		t.Fatal(err)
	}
	def.JobType = "SSH"
	if policy.ExecutionError(def) == "" {
		t.Fatal("legacy control-plane SSH was enabled")
	}
	def.JobType = "SSH_REMOTE"
	def.AgentID = "SERVER-AGENT"
	if policy.ExecutionError(def) == "" {
		t.Fatal("bridge ran on control plane")
	}
}
