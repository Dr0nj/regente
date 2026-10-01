package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Dr0nj/regente-server/internal/domain"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func awaitDurableState(t *testing.T, f *durableFixture, id, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if instanceState(t, f, id) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	rows, _ := f.d.Query("SELECT x.chunk FROM execution_output x JOIN execution_attempts a ON a.execution_id=x.execution_id JOIN runtime_orders r ON r.order_id=a.order_id WHERE r.instance_id=? ORDER BY x.seq", id)
	if rows != nil {
		for rows.Next() {
			var text string
			rows.Scan(&text)
			t.Log(text)
		}
		rows.Close()
	}
	var code int
	var state, reason, out string
	f.d.QueryRow("SELECT a.state,COALESCE(a.exit_code,0),a.reason,a.result_output FROM execution_attempts a JOIN runtime_orders r ON r.order_id=a.order_id WHERE r.instance_id=? ORDER BY a.attempt DESC LIMIT 1", id).Scan(&state, &code, &reason, &out)
	t.Log("attempt", state, code, reason, out)
	t.Fatal("state", instanceState(t, f, id), "expected", want)
}
func TestI10InternalHTTPUsesJournalAndUnknownNeverRetries(t *testing.T) {
	var effects atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		effects.Add(1)
		if r.URL.Path == "/lost" {
			c, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			c.Close()
			return
		}
		fmt.Fprint(w, "effect confirmed")
	}))
	defer target.Close()
	f := durableTest(t, domain.JobDefinition{ID: "known", Team: "test", JobType: "HTTP", AgentID: "SERVER-AGENT", Params: map[string]interface{}{"method": "POST", "url": target.URL}, ConditionsOutAdd: []string{"confirmed"}}, domain.JobDefinition{ID: "unknown", Team: "test", JobType: "HTTP", AgentID: "SERVER-AGENT", Retries: 3, Params: map[string]interface{}{"method": "POST", "url": target.URL + "/lost"}})
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := f.s.StartDurableInternal(ctx, nil, dir, "node", "test", true, false); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"known", "unknown"} {
		var raw string
		f.d.QueryRow("SELECT definition_snapshot FROM instances WHERE id=?", id+"-2026-09-30").Scan(&raw)
		var def domain.JobDefinition
		if err := json.Unmarshal([]byte(raw), &def); err != nil {
			t.Fatal(err)
		}
		f.s.startInstance(id+"-2026-09-30", def)
	}
	awaitDurableState(t, f, "known-2026-09-30", "OK")
	awaitDurableState(t, f, "unknown-2026-09-30", "UNCERTAIN")
	if effects.Load() != 2 || scalar(t, f.d, "SELECT COUNT(*) FROM conditions WHERE name='confirmed'") != 1 {
		t.Fatal(effects.Load())
	}
	f.reopen(t)
	if err := f.s.StartDurableInternal(ctx, nil, dir, "node", "test", true, false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	if effects.Load() != 2 || instanceState(t, f, "unknown-2026-09-30") != "UNCERTAIN" || scalar(t, f.d, "SELECT COUNT(*) FROM execution_attempts") != 2 {
		t.Fatal("internal effect replayed")
	}
}
