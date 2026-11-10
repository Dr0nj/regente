package audit

import (
	"context"
	"encoding/json"
	"github.com/Dr0nj/regente-server/internal/db"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBatchLostACKRestartAndPartialPrefix(t *testing.T) {
	for _, dialect := range []db.Dialect{db.SQLite, db.Postgres} {
		t.Run(string(dialect), func(t *testing.T) { testBatchLostACKRestartAndPartialPrefix(t, dialect) })
	}
}
func testBatchLostACKRestartAndPartialPrefix(t *testing.T, dialect db.Dialect) {
	d, key := durableTestDB(t, dialect)
	for i := range 8 {
		if err := d.AuditObservation("test", string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	public, _ := os.ReadFile(key + ".pub")
	path := filepath.Join(t.TempDir(), "journal")
	token := strings.Repeat("t", 32)
	c, err := OpenCollector(path, string(public), token)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { c.Close() }()
	mode := "lost"
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if mode == "partial" {
			var b Batch
			if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
				t.Error(err)
				return
			}
			raw, _ := json.Marshal(Batch{Records: b.Records[:2]})
			r.Body = http.NoBody
			r = httptest.NewRequest("POST", "/", strings.NewReader(string(raw)))
			r.Header.Set("Content-Type", BatchContentType)
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if mode == "lost" {
			c.ServeHTTP(httptest.NewRecorder(), r)
			w.WriteHeader(503)
			return
		}
		c.ServeHTTP(w, r)
	}))
	defer srv.Close()
	e := Exporter{DB: d, URL: srv.URL, Token: token, BatchSize: 4}
	if e.Step(context.Background()) == nil || c.Checkpoint().Seq != 4 {
		t.Fatal("ACK perdido não persistiu lote")
	}
	c.Close()
	c, err = OpenCollector(path, string(public), token)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = d.Exec("UPDATE audit_delivery SET next_at=0")
	mode = "online"
	if err = e.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	mode = "partial"
	if err = e.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = d.QueryRow("SELECT COUNT(*) FROM audit_delivery WHERE acknowledged=1").Scan(&n)
	if n != 6 {
		t.Fatal("prefixo inválido", n)
	}
	mode = "online"
	if err = e.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if calls != 4 || strings.Count(string(data), "\n") != 8 {
		t.Fatal(calls, string(data))
	}
}

func TestBatchRejectsGapWithoutPublishingPrefix(t *testing.T) {
	d, key := durableTestDB(t, db.SQLite)
	for range 3 {
		_ = d.AuditObservation("test", "ok")
	}
	records, _ := d.AuditRecords(0, 3)
	public, _ := os.ReadFile(key + ".pub")
	token := strings.Repeat("t", 32)
	c, err := OpenCollector(filepath.Join(t.TempDir(), "journal"), string(public), token)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	raw, _ := json.Marshal(Batch{Records: []db.AuditRecord{records[0], records[2]}})
	r := httptest.NewRequest("POST", "/", strings.NewReader(string(raw)))
	r.Header.Set("Content-Type", BatchContentType)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	c.ServeHTTP(w, r)
	if w.Code != 409 || c.Checkpoint().Seq != 0 {
		t.Fatal(w.Code, c.Checkpoint())
	}
}

func TestBatchRejectsUnsentOrMalformedACK(t *testing.T) {
	for _, mode := range []string{"beyond_batch", "wrong_hash", "wrong_stream", "trailing_json"} {
		t.Run(mode, func(t *testing.T) {
			d, _ := durableTestDB(t, db.SQLite)
			for range 4 {
				if err := d.AuditObservation("test", "bounded ack"); err != nil {
					t.Fatal(err)
				}
			}
			records, err := d.AuditRecords(0, 4)
			if err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				record := records[1]
				if mode == "beyond_batch" {
					record = records[3]
				}
				ack := ACK{record.Stream, record.Seq, record.Hash}
				if mode == "wrong_hash" {
					ack.Hash = "wrong"
				}
				if mode == "wrong_stream" {
					ack.Stream = "wrong"
				}
				if err := json.NewEncoder(w).Encode(ack); err != nil {
					t.Error(err)
				}
				if mode == "trailing_json" {
					_, _ = w.Write([]byte("{}"))
				}
			}))
			defer srv.Close()
			e := Exporter{DB: d, URL: srv.URL, BatchSize: 2}
			if err = e.Step(context.Background()); err == nil {
				t.Fatal("ACK inválido aceito")
			}
			var n int
			if err = d.QueryRow("SELECT COUNT(*) FROM audit_delivery WHERE acknowledged=1").Scan(&n); err != nil || n != 0 {
				t.Fatal("prefixo avançou", n, err)
			}
		})
	}
}
