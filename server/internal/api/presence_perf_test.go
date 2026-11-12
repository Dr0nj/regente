package api

import (
	"fmt"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/execution"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/runtimeprofile"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPresenceCoalescesWritesWithoutCachingAuthorization(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d := machineTestDB(t, dialect)
			token := newMachineToken(t, d, "presence-worker")
			if _, err := d.Exec("UPDATE machine_principals SET capabilities='COMMAND,EXECUTION_V2' WHERE agent_id='presence-worker'"); err != nil {
				t.Fatal(err)
			}
			sched := scheduler.New(storage.NewFileStore(t.TempDir(), false), d, hub.New(), time.Hour)
			defer sched.Stop()
			s := server{cfg: Config{DB: d, Scheduler: sched, RuntimePolicy: runtimeprofile.Config{Profile: "production"}}, attempts: execution.New(d, time.Now)}
			poll := func(extra string, want int) {
				t.Helper()
				r := httptest.NewRequest("GET", "/api/agent/v2/poll?id=presence-worker&caps=COMMAND,EXECUTION_V2&protocol=2&journal=1&os=linux&arch=amd64&host=test&"+extra, nil)
				r.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				s.executionPoll(w, r)
				if w.Code != want {
					t.Fatalf("poll %d, esperado %d: %s", w.Code, want, w.Body.String())
				}
			}
			poll("ver=v1&slots=4&pendingLimit=16&available=1", 204)
			exec := func(q string) {
				t.Helper()
				if _, err := d.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			exec("CREATE TABLE perf_presence_writes(name TEXT PRIMARY KEY,n INTEGER NOT NULL)")
			if dialect == db.Postgres {
				exec(`CREATE FUNCTION perf_presence_count() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN UPDATE perf_presence_writes SET n=n+1 WHERE name=TG_TABLE_NAME; RETURN NEW; END $$`)
			}
			for _, table := range []string{"agent_tokens", "agents", "execution_agent_capacity"} {
				exec(fmt.Sprintf("INSERT INTO perf_presence_writes VALUES('%s',0)", table))
				if dialect == db.Postgres {
					exec(fmt.Sprintf("CREATE TRIGGER perf_presence_update AFTER UPDATE ON %s FOR EACH ROW EXECUTE FUNCTION perf_presence_count()", table))
				} else {
					exec(fmt.Sprintf("CREATE TRIGGER perf_presence_%s AFTER UPDATE ON %s BEGIN UPDATE perf_presence_writes SET n=n+1 WHERE name='%s'; END", table, table, table))
				}
			}
			count := func(table string) int {
				t.Helper()
				var n int
				if err := d.QueryRow("SELECT n FROM perf_presence_writes WHERE name=?", table).Scan(&n); err != nil {
					t.Fatal(err)
				}
				return n
			}
			poll("ver=v1&slots=4&pendingLimit=16&available=1", 204)
			for _, table := range []string{"agent_tokens", "agents", "execution_agent_capacity"} {
				if n := count(table); n != 0 {
					t.Fatalf("presença repetida reescrita em %s: %d", table, n)
				}
			}
			poll("ver=v2&slots=1&pendingLimit=2&available=0", 204)
			var slots, pending, available int
			if err := d.QueryRow("SELECT slots,pending_limit,available FROM execution_agent_capacity WHERE agent_id='presence-worker'").Scan(&slots, &pending, &available); err != nil {
				t.Fatal(err)
			}
			if slots != 1 || pending != 2 || available != 0 || count("agents") != 1 || count("execution_agent_capacity") != 1 {
				t.Fatal("mudança de metadata/capacidade foi adiada")
			}
			exec("UPDATE agent_tokens SET last_used_at='2000-01-01 00:00:00'")
			exec("UPDATE agents SET last_seen_at='2000-01-01 00:00:00'")
			exec("UPDATE execution_agent_capacity SET last_seen=0")
			exec("UPDATE perf_presence_writes SET n=0")
			poll("ver=v2&slots=1&pendingLimit=2&available=0", 204)
			for _, table := range []string{"agent_tokens", "agents", "execution_agent_capacity"} {
				if count(table) != 1 {
					t.Fatalf("presença vencida não renovada: %s", table)
				}
			}
			exec("UPDATE agent_tokens SET revoked_at=1")
			poll("ver=v2&slots=1&pendingLimit=2&available=0", 401)
		})
	}
}
