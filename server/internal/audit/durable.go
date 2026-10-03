package audit

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
)

type ACK struct {
	Stream string `json:"stream"`
	Seq    int64  `json:"seq"`
	Hash   string `json:"hash"`
}
type Exporter struct {
	DB         *db.DB
	URL, Token string
	Client     *http.Client
}

// ACK somente após validação do recibo. Nenhum 2xx genérico permite avançar.
func (e *Exporter) Step(ctx context.Context) error {
	var seq int64
	var attempts int
	var next int64
	var dead int
	err := e.DB.QueryRow("SELECT seq,attempts,next_at,dead_letter FROM audit_delivery WHERE acknowledged=0 ORDER BY seq LIMIT 1").Scan(&seq, &attempts, &next, &dead)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if dead != 0 {
		return fmt.Errorf("audit dead letter at sequence %d", seq)
	}
	if next > time.Now().UnixMilli() {
		return nil
	}
	records, err := e.DB.AuditRecords(seq-1, 1)
	if err != nil {
		return err
	}
	if len(records) != 1 || records[0].Seq != seq {
		return fmt.Errorf("audit outbox gap")
	}
	record := records[0]
	// Exportar uma trilha adulterada é proibido mesmo que o destino esteja indisponível.
	var public string
	if err = e.DB.QueryRow("SELECT public_key FROM audit_head WHERE id=1").Scan(&public); err != nil {
		return err
	}
	key, err := hex.DecodeString(public)
	if err != nil {
		return err
	}
	if err = db.VerifyAuditRecord(record, key, record.Previous, seq); err != nil {
		return err
	}
	b, _ := json.Marshal(record)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.URL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.Token)
	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	code := "transport"
	valid := false
	permanent := false
	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var ack ACK
			err = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&ack)
			valid = err == nil && ack.Stream == record.Stream && ack.Seq == seq && ack.Hash == record.Hash
			code = "invalid_ack"
			permanent = !valid
		} else {
			code = fmt.Sprintf("http_%d", resp.StatusCode)
			permanent = resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 429
		}
	}
	if valid {
		_, err = e.DB.Exec("UPDATE audit_delivery SET acknowledged=1,error_code='' WHERE seq=? AND acknowledged=0", seq)
		return err
	}
	attempts++
	delay := time.Second << min(attempts, 8)
	dead = 0
	if permanent || attempts >= 20 {
		dead = 1
	}
	_, saveErr := e.DB.Exec("UPDATE audit_delivery SET attempts=?,next_at=?,dead_letter=?,error_code=? WHERE seq=? AND acknowledged=0", attempts, time.Now().Add(delay).UnixMilli(), dead, code, seq)
	if saveErr != nil {
		return saveErr
	}
	return fmt.Errorf("audit export sequence %d: %s", seq, code)
}
func (e *Exporter) Run(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := e.Step(ctx); err != nil {
				log.Printf("[audit] %v", err)
			}
		}
	}
}
func ValidateDestination(raw, token string, production bool) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || token == "" {
		return fmt.Errorf("audit destination requires an endpoint and separate token")
	}
	if u.Scheme != "https" && (production || u.Scheme != "http") {
		return fmt.Errorf("audit destination requires HTTPS")
	}
	return nil
}

// Coletor independente: possui apenas chave pública e arquivo próprio. ACK implica Sync.
type Collector struct {
	mu       sync.Mutex
	file     *os.File
	public   ed25519.PublicKey
	token    string
	last     ACK
	receipts map[int64]ACK
	broken   bool
}

func OpenCollector(path, publicHex, token string) (*Collector, error) {
	key, err := hex.DecodeString(strings.TrimSpace(publicHex))
	if err != nil || len(key) != ed25519.PublicKeySize || len(token) < 32 {
		return nil, fmt.Errorf("collector requires a public key and a token of at least 32 characters")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		f.Close()
		return nil, fmt.Errorf("collector journal requires an owner-only regular file")
	}
	c := &Collector{file: f, public: key, token: token, receipts: map[int64]ACK{}}
	dec := json.NewDecoder(f)
	for {
		var r db.AuditRecord
		err = dec.Decode(&r)
		if err == io.EOF {
			break
		}
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("collector journal is incomplete; restore a verified copy")
		}
		if err = c.accept(r, false); err != nil {
			f.Close()
			return nil, err
		}
	}
	return c, nil
}
func (c *Collector) Close() error { c.mu.Lock(); defer c.mu.Unlock(); return c.file.Close() }
func (c *Collector) accept(r db.AuditRecord, persist bool) error {
	if r.Stream == "" || (c.last.Stream != "" && r.Stream != c.last.Stream) {
		return fmt.Errorf("collector stream mismatch")
	}
	if err := db.VerifyAuditRecord(r, c.public, c.last.Hash, c.last.Seq+1); err != nil {
		return err
	}
	if persist {
		b, _ := json.Marshal(r)
		b = append(b, '\n')
		if _, err := c.file.Write(b); err != nil {
			c.broken = true
			return err
		}
		if err := c.file.Sync(); err != nil {
			c.broken = true
			return err
		}
	}
	c.last = ACK{Stream: r.Stream, Seq: r.Seq, Hash: r.Hash}
	c.receipts[r.Seq] = c.last
	return nil
}
func (c *Collector) Checkpoint() ACK { c.mu.Lock(); defer c.mu.Unlock(); return c.last }
func (c *Collector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+c.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var record db.AuditRecord
	if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
		http.Error(w, "invalid record", http.StatusBadRequest)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.broken {
		http.Error(w, "journal requires recovery", http.StatusServiceUnavailable)
		return
	}
	ack, duplicate := c.receipts[record.Seq]
	if duplicate {
		if ack.Stream != record.Stream || ack.Hash != record.Hash || db.VerifyAuditRecord(record, c.public, record.Previous, record.Seq) != nil {
			http.Error(w, "conflicting duplicate", http.StatusConflict)
			return
		}
	} else {
		if err := c.accept(record, true); err != nil {
			http.Error(w, "journal write or integrity validation failed", http.StatusConflict)
			return
		}
		ack = c.last
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(ack)
}
