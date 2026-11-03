package scheduler

import (
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/runtimeprofile"
	"testing"
)

func TestDurableRoutingByIdentityWithoutEnvironment(t *testing.T) {
	for _, env := range []string{"", "local", "prod", "qa"} {
		t.Run(env, func(t *testing.T) {
			def := domain.JobDefinition{ID: "job", Team: "test", AgentID: "worker", Environment: env, JobType: "COMMAND"}
			f := durableTest(t, def)
			f.s.RuntimePolicy = runtimeprofile.Config{Profile: "production", ControlPlane: "deny", ExecutionMode: "durable"}
			if _, err := f.d.Exec("UPDATE machine_principals SET environment='old-agent-label' WHERE agent_id='worker'"); err != nil {
				t.Fatal(err)
			}
			if id, err := f.s.durableAgent(def); err != nil || id != "worker" {
				t.Fatalf("routing: %q %v", id, err)
			}
			f.s.Tick()
			if instanceState(t, f, "job-2026-09-30") != "RUNNING" {
				t.Fatal("legacy environment blocked admission")
			}
			def.AgentID = "different-worker"
			if _, err := f.s.durableAgent(def); err == nil {
				t.Fatal("pinned identity bypassed")
			}
		})
	}
}
