package scheduler

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Dr0nj/regente-server/internal/db"
)

// Regressão de produção (2026-11-13): as consultas que rodam a cada tick
// varriam o histórico inteiro de execuções. Este teste trava o PLANO: nenhuma
// delas pode fazer SCAN de tabela que cresce com os dias (runtime_orders,
// lab_orders, execution_attempts, instances) — só de índice parcial do que
// está vivo. Um teste de resultado não pegaria a regressão: a consulta lenta
// devolvia as linhas certas.
func TestTickQueriesNeverScanHistory(t *testing.T) {
	d := newTestDB(t)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec("INSERT INTO machine_principals(agent_id,environment,capabilities) VALUES('a','test','COMMAND')")
	// Histórico grande e terminado + poucas linhas vivas, como num banco de dias.
	for i := 0; i < 400; i++ {
		id := fmt.Sprintf("h%04d", i)
		state := "succeeded"
		status := "OK"
		attempts := 1
		switch {
		case i == 7:
			state, status = "running", "RUNNING"
		case i == 9:
			state, status = "uncertain", "RUNNING"
		case i == 11:
			state, status, attempts = "failed", "WAITING", 2
		case i%2 == 0:
			// Diária grande ainda aguardando (28,8 mil na VPS): o ramo de retry não
			// pode percorrer todas elas via idx_instances_status.
			status = "WAITING"
		}
		exec("INSERT INTO instances(id,definition_id,order_date,status,scheduled_at,attempts) VALUES(?,?,'2026-11-13',?,CURRENT_TIMESTAMP,?)", id, id, status, attempts)
		exec("INSERT INTO lab_orders(id,source_instance_id,request_key,snapshot,snapshot_checksum,environment,state,current_execution,created_at) VALUES(?,?,?,'{}',?,'test',?,?,1)", id, id, id, digest("{}"), state, id)
		exec("INSERT INTO execution_attempts(execution_id,order_id,attempt,fence,agent_id,request_key,intent,state,started_at,created_at) VALUES(?,?,1,1,'a',?,'start',?,1,1)", id, id, id, state)
		exec("INSERT INTO runtime_orders(instance_id,order_id) VALUES(?,?)", id, id)
	}
	records, _, err := readResourceOrders(d)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range records {
		got[r.id] = true
	}
	if len(got) != 3 || !got["h0007"] || !got["h0009"] || !got["h0011"] {
		t.Fatalf("ordens vivas = %v, esperava h0007 (running), h0009 (uncertain) e h0011 (retry em espera)", got)
	}

	queries := map[string]string{
		"reservas":    activeResourceOrdersQuery(d.Dialect()),
		"lease":       "SELECT execution_id FROM execution_attempts WHERE " + db.ActiveAttemptPredicate("execution_attempts") + " AND state IN ('accepted','running','cancel_requested') AND lease_until>0 AND lease_until<=1",
		"em-execução": "SELECT r.instance_id FROM execution_attempts a JOIN lab_orders o ON o.id=a.order_id AND o.current_execution=a.execution_id JOIN runtime_orders r ON r.order_id=o.id JOIN instances i ON i.id=r.instance_id WHERE " + db.ActiveAttemptPredicate("a") + " AND a.state='running' AND a.started_at>0",
	}
	// Sem estatísticas (banco nunca analisado, caso da VPS) e com elas.
	for _, stats := range []bool{false, true} {
		if stats {
			exec("ANALYZE")
		}
		for name, q := range queries {
			rows, err := d.Query("EXPLAIN QUERY PLAN " + q)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			plan := []string{}
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				plan = append(plan, detail)
			}
			rows.Close()
			for _, step := range plan {
				if strings.HasPrefix(step, "SCAN") && !strings.Contains(step, "execution_attempts_active") && !strings.Contains(step, "instances_retry_wait") {
					t.Errorf("%s: varredura de histórico no plano: %q\nplano completo: %v", name, step, plan)
				}
				if strings.Contains(step, "idx_instances_status") {
					t.Errorf("%s: plano percorre toda instance de um status: %q\nplano completo: %v", name, step, plan)
				}
			}
		}
	}
}
