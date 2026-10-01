package db

import (
	"context"
	"testing"
)

func TestI11MigrationPreservesHolds(t *testing.T) {
	for _, dialect := range []Dialect{SQLite, Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d, _ := migrationTestDB(t, dialect)
			migs := sqliteMigrations
			if dialect == Postgres {
				migs = pgMigrations
			}
			if err := migrate(context.Background(), d, migs[:28], 28, 28); err != nil {
				t.Fatal(err)
			}
			queries := []string{
				"INSERT INTO machine_principals(agent_id,environment,capabilities) VALUES('worker','','COMMAND,EXECUTION_V2')",
				"INSERT INTO instances(id,definition_id,order_date,status,scheduled_at) VALUES('source','job','2026-09-30','UNCERTAIN',CURRENT_TIMESTAMP)",
				"INSERT INTO lab_orders(id,source_instance_id,request_key,snapshot,snapshot_checksum,environment,state,current_execution,created_at,runtime) VALUES('order','source','runtime','{}','fixture','','uncertain','attempt',0,1)",
				"INSERT INTO runtime_orders(instance_id,order_id) VALUES('source','order')",
				"INSERT INTO execution_attempts(execution_id,order_id,attempt,fence,agent_id,request_key,intent,state,created_at) VALUES('attempt','order',1,1,'worker','start','start','uncertain',0)",
				"INSERT INTO execution_resource_holds(instance_id,name,quantity) VALUES('source','pool',2)",
				"INSERT INTO execution_resource_holds(instance_id,name,quantity) VALUES('unlinked','pool',3)"}
			for _, q := range queries {
				if _, err := d.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			if err := Migrate(d); err != nil {
				t.Fatal(err)
			}
			var owner string
			var quantity int
			if err := d.QueryRow("SELECT execution_id,quantity FROM execution_resource_holds WHERE instance_id='source'").Scan(&owner, &quantity); err != nil || owner != "attempt" || quantity != 2 {
				t.Fatal(owner, quantity, err)
			}
			if err := d.QueryRow("SELECT execution_id,quantity FROM execution_resource_holds WHERE instance_id='unlinked'").Scan(&owner, &quantity); err != nil || owner != "" || quantity != 3 {
				t.Fatal(owner, quantity, err)
			}
			assertCount(t, d, "SELECT SUM(quantity) FROM execution_resource_holds", 5)
			if err := Migrate(d); err != nil {
				t.Fatal(err)
			}
		})
	}
}
