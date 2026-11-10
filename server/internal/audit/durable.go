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
	"github.com/Dr0nj/regente-server/internal/telemetry"
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
	// BatchSize=0/1 preserva o protocolo legado; habilitar após atualizar o coletor.
	BatchSize int
}

// ACK somente após validação do recibo. Nenhum 2xx genérico permite avançar.
func (e *Exporter) Step(ctx context.Context) error {
	_, err := e.step(ctx)
	return err
}
func (e *Exporter) step(ctx context.Context) (bool, error) {
	defer telemetry.Measure(telemetry.AuditExport)()
	var seq int64
	var attempts int
	var next int64
	var dead int
	err := e.DB.QueryRow("SELECT seq,attempts,next_at,dead_letter FROM audit_delivery WHERE acknowledged=0 ORDER BY seq LIMIT 1").Scan(&seq, &attempts, &next, &dead)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	if dead != 0 {
		return false, fmt.Errorf("audit dead letter at sequence %d", seq)
	}
	if next > time.Now().UnixMilli() {
		return false, nil
	}
	records, err := e.DB.AuditRecords(seq-1, max(1, min(e.BatchSize, MaxBatchRecords)))
	if err != nil {
		return false, err
	}
	if len(records) == 0 || records[0].Seq != seq {
		return false, fmt.Errorf("audit outbox gap")
	}
	record := records[0]
	// Exportar uma trilha adulterada é proibido mesmo que o destino esteja indisponível.
	var public string
	if err = e.DB.QueryRow("SELECT public_key FROM audit_head WHERE id=1").Scan(&public); err != nil {
		return false, err
	}
	key, err := hex.DecodeString(public)
	if err != nil {
		return false, err
	}
	previous := record.Previous
	// Confere também a âncora anterior, não apenas a assinatura individual.
	if seq > 1 {
		if err = e.DB.QueryRow("SELECT hash FROM security_audit WHERE seq=?", seq-1).Scan(&previous); err != nil {
			return false, err
		}
	} else {
		previous = ""
	}
	rows, err := e.DB.Query("SELECT seq,dead_letter,next_at FROM audit_delivery WHERE seq>=? AND seq<=? ORDER BY seq", seq, records[len(records)-1].Seq)
	if err != nil {
		return false, err
	}
	type deliveryState struct {
		seq  int64
		dead int
		next int64
	}
	deliveries := []deliveryState{}
	for rows.Next() {
		var state deliveryState
		if err = rows.Scan(&state.seq, &state.dead, &state.next); err != nil {
			break
		}
		deliveries = append(deliveries, state)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return false, err
	}
	if len(deliveries) != len(records) {
		return false, fmt.Errorf("audit delivery gap")
	}
	bytesUsed := 32
	count := 0
	for i, item := range records {
		if item.Stream != record.Stream {
			return false, fmt.Errorf("audit stream mismatch")
		}
		if err = db.VerifyAuditRecord(item, key, previous, seq+int64(i)); err != nil {
			return false, err
		}
		// Nunca atravessa dead-letter, backoff ou lacuna de entrega.
		state := deliveries[i]
		if state.seq != item.Seq {
			return false, fmt.Errorf("audit delivery gap")
		}
		if state.dead != 0 || state.next > time.Now().UnixMilli() {
			break
		}
		raw, _ := json.Marshal(item)
		if bytesUsed+len(raw)+1 > MaxBatchBytes {
			break
		}
		bytesUsed += len(raw) + 1
		previous = item.Hash
		count++
	}
	if count == 0 {
		return false, fmt.Errorf("audit record exceeds batch limit or is blocked")
	}
	records = records[:count]
	b, _ := json.Marshal(record)
	if e.BatchSize > 1 {
		b, _ = json.Marshal(Batch{Records: records})
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.URL, bytes.NewReader(b))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.BatchSize > 1 {
		req.Header.Set("Content-Type", BatchContentType)
	}
	req.Header.Set("Authorization", "Bearer "+e.Token)
	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	code := "transport"
	valid := false
	var ack ACK
	permanent := false
	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			dec := json.NewDecoder(io.LimitReader(resp.Body, 4096))
			err = dec.Decode(&ack)
			var trailing any
			if err == nil && dec.Decode(&trailing) != io.EOF {
				err = fmt.Errorf("invalid trailing ACK data")
			}
			if err == nil && ack.Stream == record.Stream && ack.Seq >= seq && ack.Seq < seq+int64(len(records)) {
				valid = records[ack.Seq-seq].Hash == ack.Hash
			}
			code = "invalid_ack"
			permanent = !valid
		} else {
			code = fmt.Sprintf("http_%d", resp.StatusCode)
			permanent = resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 429
		}
	}
	if valid {
		_, err = e.DB.Exec("UPDATE audit_delivery SET acknowledged=1,error_code='' WHERE seq>=? AND seq<=? AND acknowledged=0", seq, ack.Seq)
		if err == nil {
			telemetry.Count(telemetry.AuditBatchRecords, uint64(ack.Seq-seq+1))
		}
		return err == nil, err
	}
	attempts++
	delay := time.Second << min(attempts, 8)
	dead = 0
	if permanent || attempts >= 20 {
		dead = 1
	}
	_, saveErr := e.DB.Exec("UPDATE audit_delivery SET attempts=?,next_at=?,dead_letter=?,error_code=? WHERE seq=? AND acknowledged=0", attempts, time.Now().Add(delay).UnixMilli(), dead, code, seq)
	if saveErr != nil {
		return false, saveErr
	}
	return false, fmt.Errorf("audit export sequence %d: %s", seq, code)
}

// Drena por até um segundo ou 100 lotes; nunca pula backoff, lacuna ou dead-letter.
func (e *Exporter) Run(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			deadline := time.Now().Add(time.Second)
			for range 100 {
				if time.Now().After(deadline) {
					break
				}
				if ctx.Err() != nil {
					return
				}
				advanced, err := e.step(ctx)
				if err != nil {
					log.Printf("[audit] %v", err)
					break
				}
				if !advanced {
					break
				}
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
	r.Body = http.MaxBytesReader(w, r.Body, MaxBatchBytes)
	dec := json.NewDecoder(r.Body)
	var records []db.AuditRecord
	if r.Header.Get("Content-Type") == BatchContentType {
		var batch Batch
		if err := dec.Decode(&batch); err != nil {
			http.Error(w, "invalid batch", http.StatusBadRequest)
			return
		}
		records = batch.Records
	} else {
		var record db.AuditRecord
		if err := dec.Decode(&record); err != nil {
			http.Error(w, "invalid record", http.StatusBadRequest)
			return
		}
		records = []db.AuditRecord{record}
	}
	var extra any
	if len(records) == 0 || len(records) > MaxBatchRecords || dec.Decode(&extra) != io.EOF {
		http.Error(w, "invalid batch size or trailing data", http.StatusBadRequest)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.broken {
		http.Error(w, "journal requires recovery", http.StatusServiceUnavailable)
		return
	}
	ack := c.last
	var journal bytes.Buffer
	var added []ACK
	for i, record := range records {
		if i > 0 && (record.Seq != records[i-1].Seq+1 || record.Previous != records[i-1].Hash || record.Stream != records[i-1].Stream) {
			http.Error(w, "batch gap", http.StatusConflict)
			return
		}
		if old, duplicate := c.receipts[record.Seq]; duplicate {
			if old.Stream != record.Stream || old.Hash != record.Hash || db.VerifyAuditRecord(record, c.public, record.Previous, record.Seq) != nil {
				http.Error(w, "conflicting duplicate", http.StatusConflict)
				return
			}
			continue
		}
		if record.Stream == "" || (ack.Stream != "" && record.Stream != ack.Stream) || db.VerifyAuditRecord(record, c.public, ack.Hash, ack.Seq+1) != nil {
			http.Error(w, "journal integrity validation failed", http.StatusConflict)
			return
		}
		_ = json.NewEncoder(&journal).Encode(record)
		ack = ACK{record.Stream, record.Seq, record.Hash}
		added = append(added, ack)
	}
	if journal.Len() > 0 {
		n, err := c.file.Write(journal.Bytes())
		if err == nil && n != journal.Len() {
			err = io.ErrShortWrite
		}
		if err == nil {
			err = c.file.Sync()
		}
		if err != nil {
			c.broken = true
			http.Error(w, "journal requires recovery", http.StatusServiceUnavailable)
			return
		}
		c.last = ack
		for _, receipt := range added {
			c.receipts[receipt.Seq] = receipt
		}
	}
	// Replay antigo confirma somente o prefixo enviado, nunca o head além dele.
	last := records[len(records)-1]
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(c.receipts[last.Seq])
}

const MaxBatchRecords = 128
const MaxBatchBytes = 1 << 20
const BatchContentType = "application/vnd.regente.audit-batch+json"

type Batch struct {
	Records []db.AuditRecord `json:"records"`
}
