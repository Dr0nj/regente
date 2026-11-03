package scheduler

import (
	"encoding/json"
	"testing"

	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
)

// Production uses durable operations. The legacy SetOK tests cannot protect
// its transaction or the notification that refreshes Monitoring's condition pool.
func TestDurableSetOKConditionsAndMonitoring(t *testing.T) {
	f := durableTest(t,
		domain.JobDefinition{ID: "producer", Team: "test", JobType: "COMMAND", Params: map[string]interface{}{"command": "unused"}, ConditionsOutAdd: []string{"done", "persistent@stat"}, ConditionsOutRemove: []string{"input"}},
		domain.JobDefinition{ID: "consumer", Team: "test", JobType: "COMMAND", Params: map[string]interface{}{"command": "unused"}, ConditionsIn: []string{"done"}, Confirm: true},
	)
	if err := f.s.conditions.Set("input", "2026-09-30", "fixture"); err != nil {
		t.Fatal(err)
	}
	client := &hub.Client{ID: "monitoring", Kind: hub.ClientWeb, Send: make(chan []byte, 16)}
	h := hub.New()
	h.Register(client)
	f.s.hub = h
	dropFault := durableFault(t, f.d, "conditions", "set_ok_condition_fault")
	if _, err := f.s.DurableAction("admin", "producer-2026-09-30", "set-ok"); err == nil {
		t.Fatal("condition storage failure accepted Set OK")
	}
	if instanceState(t, f, "producer-2026-09-30") != "WAITING" || len(client.Send) != 0 || f.s.conditions.Has("done", "2026-09-30") {
		t.Fatal("failed Set OK leaked a status, condition or notification")
	}
	dropFault()
	if _, err := f.s.DurableAction("admin", "producer-2026-09-30", "set-ok"); err != nil {
		t.Fatal(err)
	}
	if !f.s.conditions.Has("done", "2026-09-30") || !f.s.conditions.Has("persistent", "") || f.s.conditions.Has("input", "2026-09-30") {
		t.Fatal("Set OK did not atomically apply output conditions")
	}
	notified := false
	for len(client.Send) > 0 {
		var event struct {
			Event string `json:"event"`
		}
		if err := json.Unmarshal(<-client.Send, &event); err != nil {
			t.Fatal(err)
		}
		notified = notified || event.Event == "condition.changed"
	}
	if !notified {
		t.Fatal("Monitoring condition cache is stale: durable Set OK did not emit condition.changed")
	}
	f.reopen(t)
	if !f.s.conditions.Has("done", "2026-09-30") {
		t.Fatal("condition lost after restart")
	}
	explanation, err := f.s.Explain("consumer-2026-09-30")
	if err != nil {
		t.Fatal(err)
	}
	for _, blocker := range explanation.Blockers {
		if blocker.Kind == GateCondition {
			t.Fatalf("successor still blocked by condition: %+v", blocker)
		}
	}
}
