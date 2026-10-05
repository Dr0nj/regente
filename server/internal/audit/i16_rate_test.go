package audit

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
)

// Mesmo exporter e coletor com journal Sync; não é uma simulação de ACK.
func TestI16AuditRate(t *testing.T) {
	d, key := durableTestDB(t, db.SQLite)
	for i := range 8 {
		if _, err := d.Exec("INSERT INTO variables(name,value) VALUES(?,?)", fmt.Sprint(i), "fixture"); err != nil {
			t.Fatal(err)
		}
	}
	public, err := os.ReadFile(key + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("i", 32)
	collector, err := OpenCollector(filepath.Join(t.TempDir(), "independent.jsonl"), string(public), token)
	if err != nil {
		t.Fatal(err)
	}
	defer collector.Close()
	server := httptest.NewServer(collector)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	begin := time.Now()
	go func() { defer close(done); (&Exporter{DB: d, URL: server.URL, Token: token}).Run(ctx) }()
	deadline := begin.Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err = d.QueryRow("SELECT COUNT(*) FROM audit_delivery WHERE acknowledged=1").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 8 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	elapsed := time.Since(begin)
	cancel()
	<-done
	if collector.Checkpoint().Seq != 8 {
		t.Fatal("export did not finish", collector.Checkpoint())
	}
	var acknowledged int
	if err = d.QueryRow("SELECT COUNT(*) FROM audit_delivery WHERE acknowledged=1").Scan(&acknowledged); err != nil || acknowledged != 8 {
		t.Fatal(acknowledged, err)
	}
	if err = d.VerifyAuditCheckpoint(collector.Checkpoint().Stream, 8, collector.Checkpoint().Hash); err != nil {
		t.Fatal(err)
	}
	t.Logf("I16 export: records=8 seconds=%.3f rate=%.3f records/s; valid sequence and durable ACK", elapsed.Seconds(), 8/elapsed.Seconds())
}
