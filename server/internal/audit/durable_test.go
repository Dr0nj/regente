package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Dr0nj/regente-server/internal/db"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func durableTestDB(t *testing.T, dialect db.Dialect) (*db.DB, string) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "state.db")
	if dialect == db.Postgres {
		dsn = os.Getenv("REGENTE_TEST_PG_DSN")
		if dsn == "" {
			if os.Getenv("REGENTE_REQUIRE_INTEGRATION") == "1" {
				t.Fatal("Postgres obrigatório")
			}
			t.Skip("Postgres não configurado")
		}
		admin, err := db.Open(dialect, dsn)
		if err != nil {
			t.Fatal(err)
		}
		schema := fmt.Sprintf("i14_audit_%d", time.Now().UnixNano())
		if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = admin.Exec("DROP SCHEMA " + schema + " CASCADE"); admin.Close() })
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		dsn = u.String()
	}
	d, err := db.Open(dialect, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err = db.Migrate(d); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "audit.key")
	if err = d.EnableAudit(key, 10); err != nil {
		t.Fatal(err)
	}
	return d, key
}
func TestI14DurableExport(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		t.Run(string(dialect), func(t *testing.T) {
			d, key := durableTestDB(t, dialect)
			for _, name := range []string{"one", "two", "three"} {
				if _, err := d.Exec("INSERT INTO variables(name,value) VALUES(?,?)", name, "never-export-this-secret"); err != nil {
					t.Fatal(err)
				}
			}
			public, err := os.ReadFile(key + ".pub")
			if err != nil {
				t.Fatal(err)
			}
			token := strings.Repeat("t", 32)
			path := filepath.Join(t.TempDir(), "independent.jsonl")
			collector, err := OpenCollector(path, string(public), token)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { collector.Close() }()
			mode := "offline"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "offline" {
					w.WriteHeader(503)
					return
				}
				if mode == "lost_ack" {
					recorder := httptest.NewRecorder()
					collector.ServeHTTP(recorder, r)
					w.WriteHeader(503)
					return
				}
				if mode == "invalid_ack" {
					w.Write([]byte(`{"seq":999}`))
					return
				}
				collector.ServeHTTP(w, r)
			}))
			defer server.Close()
			exporter := &Exporter{DB: d, URL: server.URL, Token: token}
			ctx := context.Background()
			if exporter.Step(ctx) == nil {
				t.Fatal("destino offline aceito")
			}
			var n int
			if err = d.QueryRow("SELECT COUNT(*) FROM audit_delivery WHERE acknowledged=1").Scan(&n); err != nil || n != 0 {
				t.Fatal(n, err)
			}
			_, _ = d.Exec("UPDATE audit_delivery SET next_at=0")
			mode = "lost_ack"
			if exporter.Step(ctx) == nil {
				t.Fatal("ACK perdido aceito")
			}
			if collector.Checkpoint().Seq != 1 {
				t.Fatal("coletor não persistiu antes do ACK")
			}
			collector.Close()
			collector, err = OpenCollector(path, string(public), token)
			if err != nil {
				t.Fatal(err)
			}
			if collector.Checkpoint().Seq != 1 {
				t.Fatal("restart perdeu checkpoint")
			}
			mode = "online"
			_, _ = d.Exec("UPDATE audit_delivery SET next_at=0")
			if err = exporter.Step(ctx); err != nil {
				t.Fatal(err)
			}
			if collector.Checkpoint().Seq != 1 {
				t.Fatal("retry duplicou evento")
			}
			mode = "invalid_ack"
			if exporter.Step(ctx) == nil {
				t.Fatal("ACK sem identidade aceito")
			}
			var dead int
			if err = d.QueryRow("SELECT dead_letter FROM audit_delivery WHERE seq=2").Scan(&dead); err != nil || dead != 1 {
				t.Fatal(dead, err)
			}
			mode = "online"
			if exporter.Step(ctx) == nil {
				t.Fatal("pulou dead-letter")
			}
			if collector.Checkpoint().Seq != 1 {
				t.Fatal("lacuna exportada")
			}
			_, _ = d.Exec("UPDATE audit_delivery SET dead_letter=0,attempts=0,next_at=0 WHERE seq=2")
			for range 2 {
				if err = exporter.Step(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if collector.Checkpoint().Seq != 3 {
				t.Fatal(collector.Checkpoint())
			}
			if err = d.QueryRow("SELECT COUNT(*) FROM audit_delivery WHERE acknowledged=1").Scan(&n); err != nil || n != 3 {
				t.Fatal(n, err)
			}
			journal, err := os.ReadFile(path)
			if err != nil || strings.Count(string(journal), "\n") != 3 || strings.Contains(string(journal), "never-export-this-secret") {
				t.Fatal(string(journal), err)
			}
			records, err := d.AuditRecords(0, 3)
			if err != nil {
				t.Fatal(err)
			}
			tampered := records[1]
			tampered.Payload = "tampered"
			b, _ := json.Marshal(tampered)
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(string(b)))
			req.Header.Set("Authorization", "Bearer "+token)
			response := httptest.NewRecorder()
			collector.ServeHTTP(response, req)
			if response.Code != 409 {
				t.Fatal("adulteração aceita", response.Code)
			}
			// Restore do destino independente a partir da cópia verificada e comparação com origem.
			restoredPath := filepath.Join(t.TempDir(), "restored.jsonl")
			if err = os.WriteFile(restoredPath, journal, 0600); err != nil {
				t.Fatal(err)
			}
			restored, err := OpenCollector(restoredPath, string(public), token)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			if restored.Checkpoint() != collector.Checkpoint() {
				t.Fatal("restore perdeu checkpoint")
			}
			if err = d.VerifyAuditCheckpoint(restored.Checkpoint().Stream, restored.Checkpoint().Seq, restored.Checkpoint().Hash); err != nil {
				t.Fatal(err)
			}
			if _, err = d.Raw().Exec("DELETE FROM audit_delivery WHERE seq=3"); err != nil {
				t.Fatal(err)
			}
			if _, err = d.Raw().Exec("DELETE FROM security_audit WHERE seq=3"); err != nil {
				t.Fatal(err)
			}
			if d.VerifyAuditCheckpoint(restored.Checkpoint().Stream, 3, restored.Checkpoint().Hash) == nil {
				t.Fatal("rollback abaixo do checkpoint aceito")
			}
		})
	}
}
