// capacity-seed — apenas dataset descartável do laboratório I16, pelo DB auditado.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Dr0nj/regente-server/internal/businessclock"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"log"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	driver := flag.String("driver", "postgres", "Lab driver")
	dsn := flag.String("db", "", "Disposable lab DSN")
	key := flag.String("audit-key", "", "Lab audit key")
	count := flag.Int("count", 10000, "Retained synthetic terminal rows")
	flag.Parse()
	u, err := url.Parse(*dsn)
	if os.Getenv("REGENTE_DISPOSABLE_DB") != "1" || *driver != "postgres" || err != nil || u.Hostname() != "127.0.0.1" || !strings.HasPrefix(u.Path, "/regente_capacity_") || *count < 1 || *count > 1000000 {
		log.Fatal("Only an explicitly disposable loopback capacity database is accepted")
	}
	d, err := db.Open(db.Postgres, *dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()
	if err = db.Migrate(d); err != nil {
		log.Fatal(err)
	}
	if err = d.EnableAudit(*key, 100000); err != nil {
		log.Fatal(err)
	}
	// Prepara um banco corrente pelas mesmas migrações de negócio do servidor,
	// antes dos fixtures; não confundir uma escrita legada por linha com a carga.
	s := scheduler.New(nil, d, nil, 2*time.Second)
	s.MigrateMonitoringSnapshot()
	s.MigrateResourcesSnapshot()
	s.MigrateCondLogicSnapshot()
	s.Stop()
	var flags int
	if err = d.QueryRow("SELECT COUNT(*) FROM meta_flags WHERE name IN ('monitoring-snapshot-v18','monitoring-resources-v19','monitoring-condlogic-v21')").Scan(&flags); err != nil || flags != 3 {
		log.Fatal("Current business migrations were not initialized")
	}
	now := time.Now().UTC()
	calendar := businessclock.Calendar{Timezone: "UTC", DailyAt: now.Add(12 * time.Hour).Format("15:04")}
	for key, value := range map[string]string{"daily_timezone": calendar.Timezone, "daily_at": calendar.DailyAt} {
		if _, err = d.Exec("INSERT OR REPLACE INTO settings(key,value) VALUES(?,?)", key, value); err != nil {
			log.Fatal(err)
		}
	}
	businessDate := calendar.BusinessDate(now)
	raw, _ := json.Marshal(map[string]any{"businessTime": calendar, "id": "retained", "label": "Retained fixture", "environment": "capacity", "jobType": "COMMAND", "actionConfig": map[string]string{"command": "echo retained # " + strings.Repeat("x", 2048)}, "schedule": map[string]bool{"enabled": false}})
	tx, err := d.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare("INSERT INTO instances(id,definition_id,order_date,status,scheduled_at,definition_snapshot,label,job_type,environment) VALUES(?,'retained',?,'OK',?,?,'Retained fixture','COMMAND','capacity')")
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()
	for i := range *count {
		if _, err = stmt.Exec(fmt.Sprintf("capacity-historical-%d", i), businessDate, now, string(raw)); err != nil {
			log.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Seeded %d retained terminal fixtures; no execution/effect claim; snapshot bytes=%d\n", *count, len(raw))
}
