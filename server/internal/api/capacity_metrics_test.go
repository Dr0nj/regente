package api

import (
	"github.com/Dr0nj/regente-server/internal/db"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestI16CapacityMetrics(t *testing.T) {
	d, err := db.Open(db.SQLite, filepath.Join(t.TempDir(), "metrics.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err = db.Migrate(d); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-10 * time.Second)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO instances(id,definition_id,order_date,status,scheduled_at) VALUES('i','d','2026-10-03','OK',?)", now)
	exec("INSERT INTO machine_principals(agent_id,environment,capabilities) VALUES('a','','COMMAND,EXECUTION_V2')")
	exec("INSERT INTO lab_orders(id,source_instance_id,request_key,snapshot,snapshot_checksum,environment,state,created_at,runtime) VALUES('o','i','key','{}','sum','','succeeded',?,1)", now.UnixMilli())
	exec("INSERT INTO runtime_orders(instance_id,order_id) VALUES('i','o')")
	exec("INSERT INTO execution_attempts(execution_id,order_id,attempt,fence,agent_id,request_key,intent,state,created_at,accepted_at,started_at,finished_at) VALUES('e','o',1,1,'a','key','start','succeeded',?,?,?,?)", now.Add(time.Second).UnixMilli(), now.Add(2*time.Second).UnixMilli(), now.Add(3*time.Second).UnixMilli(), now.Add(4*time.Second).UnixMilli())
	exec("INSERT INTO instance_events(instance_id,kind,message,ts) VALUES('i','eligible',?,?)", "Business gates passed at "+now.UTC().Format(time.RFC3339Nano), now)
	s := server{cfg: Config{DB: d}}
	w := httptest.NewRecorder()
	s.capacityMetrics(w)
	for _, want := range []string{"regente_capacity_metrics_available 1", "regente_execution_latency_sampled_attempts 1", `stage="ready_to_planned",window="1h_last_10000",quantile="0.99"} 1.000000`, `stage="ready_to_started",window="1h_last_10000",quantile="0.99"} 3.000000`} {
		if !strings.Contains(w.Body.String(), want) {
			t.Fatal(want, w.Body.String())
		}
	}
}
