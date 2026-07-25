// Seeder de demo/carga — popula a tabela instances com N jobs espalhados em F
// folders, para exercitar o ViewPoint server-driven (P3) e os benchmarks de
// escala. Usa o pacote db (migra o schema, inclui a coluna team da v4).
//
//	go run ./tools/seed -db /tmp/demo/regente.db -n 1000000 -folders 200 -date 2026-06-23
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
)

func main() {
	dbPath := flag.String("db", "./demo.db", "path to the SQLite file")
	n := flag.Int("n", 100000, "number of instances")
	folders := flag.Int("folders", 100, "number of folders")
	date := flag.String("date", time.Now().Format("2006-01-02"), "order_date")
	flag.Parse()

	d, err := db.Open(db.SQLite, *dbPath)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer d.Close()
	if err := db.Migrate(d); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	// Limpa instances dessa data (re-seed idempotente).
	if _, err := d.Exec("DELETE FROM instances WHERE order_date=?", *date); err != nil {
		log.Fatalf("clean: %v", err)
	}

	statuses := []string{"WAITING", "WAITING", "RUNNING", "OK", "OK", "OK", "NOTOK", "HELD"}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if t, err := time.Parse("2006-01-02", *date); err == nil {
		base = t
	}

	const chunk = 5000
	start := time.Now()
	inserted := 0
	for off := 0; off < *n; off += chunk {
		tx, err := d.Begin()
		if err != nil {
			log.Fatalf("begin: %v", err)
		}
		st, err := tx.Prepare(`INSERT INTO instances(id, definition_id, team, order_date, status, scheduled_at) VALUES(?,?,?,?,?,?)`)
		if err != nil {
			log.Fatalf("prepare: %v", err)
		}
		end := off + chunk
		if end > *n {
			end = *n
		}
		for i := off; i < end; i++ {
			id := fmt.Sprintf("job-%07d-%s", i, *date)
			team := fmt.Sprintf("APP_%03d", i%*folders)
			status := statuses[i%len(statuses)]
			sched := base.Add(time.Duration(i) * time.Second)
			if _, err := st.Exec(id, id, team, *date, status, sched); err != nil {
				log.Fatalf("insert %d: %v", i, err)
			}
			inserted++
		}
		st.Close()
		if err := tx.Commit(); err != nil {
			log.Fatalf("commit: %v", err)
		}
		if off%100000 == 0 {
			fmt.Printf("  …%d\n", off)
		}
	}
	// Marca a daily como rodada (pra UI não mostrar "ambiente vazio").
	_, _ = d.Exec("INSERT OR REPLACE INTO daily_runs(order_date, started_at) VALUES(?, CURRENT_TIMESTAMP)", *date)
	fmt.Printf("seed: %d instances em %d folders, date=%s, %v\n",
		inserted, *folders, *date, time.Since(start).Round(time.Millisecond))
}
