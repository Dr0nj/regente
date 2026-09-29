package scheduler

import (
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
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
	h := hub.New()
	s.hub = h
	c := &hub.Client{ID: "worker", Kind: hub.ClientAgent, Environment: "prod", StrictIdentity: true, Capabilities: []string{"COMMAND"}, Send: make(chan []byte, 4)}
	h.Register(c)
	defer h.Unregister(c)
	def := domain.JobDefinition{ID: "job", Environment: "prod", JobType: "COMMAND"}
	seedWaitingEx(t, s, "instance", time.Now().Add(-time.Hour), def)
	s.startInstance("instance", def)
	select {
	case <-c.Send:
	case <-time.After(3 * time.Second):
		t.Fatal("dispatch autorizado não ocorreu")
	}
	var status string
	if err := s.db.QueryRow("SELECT status FROM instances WHERE id='instance'").Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "RUNNING" {
		t.Fatal(status)
	}
}
