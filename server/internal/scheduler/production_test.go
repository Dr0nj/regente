package scheduler

import (
	"encoding/json"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/execution"
	"github.com/Dr0nj/regente-server/internal/runtimeprofile"
	"testing"
	"time"
)

func TestProductionPolicyCannotBeForced(t *testing.T) {
	for _, tc := range []struct{ name, env, job, agent string }{{"empty", "", "COMMAND", ""}, {"wrong", "staging", "COMMAND", ""}, {"ssh", "prod", "SSH", ""}, {"embedded", "prod", "HTTP", "SERVER-AGENT"}} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestScheduler(t)
			s.DemoMode = false
			s.RuntimePolicy = runtimeprofile.Config{Profile: "production", Environment: "prod", ControlPlane: "deny"}
			s.hub = panicBus{}
			def := domain.JobDefinition{ID: "job", Environment: tc.env, JobType: tc.job, AgentID: tc.agent}
			seedWaitingEx(t, s, "instance", time.Now().Add(-time.Hour), def)
			if _, err := s.db.Exec("UPDATE instances SET forced=1 WHERE id='instance'"); err != nil {
				t.Fatal(err)
			}
			if ex := explainOf(t, s, "instance"); ex.Runnable || hasKind(ex.Blockers, GateConfiguration) == nil {
				t.Fatalf("bloqueio não explicado: %+v", ex)
			}
			s.Tick()
			s.startInstance("instance", def)
			var status string
			var snapshot string
			if err := s.db.QueryRow("SELECT status,definition_snapshot FROM instances WHERE id='instance'").Scan(&status, &snapshot); err != nil {
				t.Fatal(err)
			}
			if status != "WAITING" || snapshot == "" {
				t.Fatalf("política contornada: %s", status)
			}
		})
	}
}
func TestProductionScopedDispatch(t *testing.T) {
	s := newTestScheduler(t)
	s.DemoMode = false
	s.RuntimePolicy = runtimeprofile.Config{Profile: "production", Environment: "prod", ControlPlane: "deny"}
	s.AttachDurable(execution.New(s.db, s.Now))
	if _, err := s.db.Exec("INSERT INTO machine_principals(agent_id,environment,capabilities) VALUES('worker','prod','COMMAND,EXECUTION_V2')"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO agent_tokens(token_hash,label,agent_id,expires_at) VALUES('fixture','worker','worker',?)", time.Now().Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	def := domain.JobDefinition{ID: "job", Environment: "prod", JobType: "COMMAND"}
	seedWaitingEx(t, s, "instance", time.Now().Add(-time.Hour), def)
	var raw string
	s.db.QueryRow("SELECT definition_snapshot FROM instances WHERE id='instance'").Scan(&raw)
	if err := json.Unmarshal([]byte(raw), &def); err != nil {
		t.Fatal(err)
	}
	if err := s.ReportAgentCapacity("worker", 4, 128, true); err != nil {
		t.Fatal(err)
	}
	s.startInstance("instance", def)
	msg, err := s.durable.Claim("worker")
	if err != nil || msg == nil || msg.Protocol != 2 || msg.Definition.Environment != "prod" {
		t.Fatal(msg, err)
	}
	var status string
	if err = s.db.QueryRow("SELECT status FROM instances WHERE id='instance'").Scan(&status); err != nil || status != "RUNNING" {
		t.Fatal(status, err)
	}
	if msg.ExecutionID == "" || msg.Fence != 1 {
		t.Fatal("unfenced production dispatch")
	}
}
