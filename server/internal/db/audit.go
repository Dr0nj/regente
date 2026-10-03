package db

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

var ErrAuditUnavailable = errors.New("mandatory audit unavailable")
var ErrAuditFull = errors.New("mandatory audit backlog is full")

type AuditIdentity struct {
	Request string `json:"request,omitempty"`
	Actor   string `json:"actor"`
	Route   string `json:"route,omitempty"`
	IP      string `json:"ip,omitempty"`
}
type Mutation struct {
	Table      string   `json:"table"`
	Operation  string   `json:"operation"`
	Statement  string   `json:"statement"`
	Count      int64    `json:"count"`
	References []string `json:"references,omitempty"`
	Actor      string   `json:"actor,omitempty"`
}
type AuditPayload struct {
	Identity  AuditIdentity `json:"identity"`
	Mutations []Mutation    `json:"mutations,omitempty"`
	Event     string        `json:"event,omitempty"`
	Outcome   string        `json:"outcome,omitempty"`
}
type AuditRecord struct {
	Stream    string `json:"stream"`
	Seq       int64  `json:"seq"`
	TS        string `json:"ts"`
	Previous  string `json:"previous"`
	Payload   string `json:"payload"`
	Hash      string `json:"hash"`
	Signature string `json:"signature"`
}
type auditConfig struct {
	key      ed25519.PrivateKey
	capacity int64
}

func schemaV32() string {
	return `
CREATE TABLE audit_head (id INTEGER PRIMARY KEY, stream TEXT NOT NULL, seq BIGINT NOT NULL DEFAULT 0, hash TEXT NOT NULL DEFAULT '', public_key TEXT NOT NULL);
CREATE TABLE security_audit (seq BIGINT PRIMARY KEY, ts TEXT NOT NULL, previous TEXT NOT NULL, payload TEXT NOT NULL, hash TEXT NOT NULL, signature TEXT NOT NULL);
CREATE TABLE audit_delivery (seq BIGINT PRIMARY KEY REFERENCES security_audit(seq), attempts INTEGER NOT NULL DEFAULT 0, next_at BIGINT NOT NULL DEFAULT 0, category TEXT NOT NULL DEFAULT 'security', acknowledged INTEGER NOT NULL DEFAULT 0, dead_letter INTEGER NOT NULL DEFAULT 0, error_code TEXT NOT NULL DEFAULT '');
CREATE INDEX idx_audit_delivery_pending ON audit_delivery(acknowledged,seq);
`
}

// Chave externa ao banco. Não regenerar silenciosamente sobre uma trilha existente.
func (d *DB) EnableAudit(path string, capacity int64) error {
	if d.dialect == SQLite {
		var sync int
		if err := d.QueryRow("PRAGMA synchronous").Scan(&sync); err != nil || sync < 2 {
			return fmt.Errorf("mandatory audit requires SQLite synchronous FULL")
		}
	}
	if capacity < 1 {
		return fmt.Errorf("audit capacity must be positive")
	}
	var stored string
	err := d.QueryRow("SELECT public_key FROM audit_head WHERE id=1").Scan(&stored)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	key, err := os.ReadFile(path)
	if os.IsNotExist(err) && stored == "" {
		_, private, e := ed25519.GenerateKey(rand.Reader)
		if e != nil {
			return e
		}
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		_, e = f.Write(private)
		if e == nil {
			e = f.Sync()
		}
		closeErr := f.Close()
		if e == nil {
			e = closeErr
		}
		if e != nil {
			return e
		}
		key = private
		err = nil
	}
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return fmt.Errorf("audit signing key missing or invalid")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("audit signing key requires owner-only permissions")
	}
	private := ed25519.PrivateKey(key)
	if !bytes.Equal(private, ed25519.NewKeyFromSeed(key[:ed25519.SeedSize])) {
		return fmt.Errorf("invalid audit private key")
	}
	public := hex.EncodeToString(private.Public().(ed25519.PublicKey))
	if stored != "" && stored != public {
		return fmt.Errorf("audit signing key does not match ledger")
	}
	if stored == "" {
		b := make([]byte, 16)
		if _, err = rand.Read(b); err != nil {
			return err
		}
		if _, err = d.Exec("INSERT INTO audit_head(id,stream,public_key) VALUES(1,?,?)", hex.EncodeToString(b), public); err != nil {
			return err
		}
	}
	if err = os.WriteFile(path+".pub", []byte(public+"\n"), 0644); err != nil {
		return err
	}
	d.audit = &auditConfig{key: private, capacity: capacity}
	return d.VerifyAudit()
}

func (d *DB) AuditEnabled() bool { return d != nil && d.audit != nil }
func (d *DB) WithAuditIdentity(identity AuditIdentity) *DB {
	copy := *d
	identity.Actor = boundedAuditIdentity(identity.Actor, 256)
	identity.Route = boundedAuditIdentity(identity.Route, 2048)
	identity.IP = boundedAuditIdentity(identity.IP, 64)
	identity.Request = boundedAuditIdentity(identity.Request, 128)
	copy.identity = identity
	return &copy
}

var mutationRE = regexp.MustCompile(`(?i)\b(INSERT(?:\s+OR\s+(?:REPLACE|IGNORE))?\s+INTO|UPDATE|DELETE\s+FROM)\s+([a-z_][a-z_0-9]*)`)

// Estas tabelas são telemetria, output ou locks; nunca são apresentadas como trilha obrigatória.
var auditTelemetry = map[string]bool{"agents": true, "execution_agent_capacity": true, "execution_queue_lock": true, "instance_events": true, "instance_output": true, "execution_output": true, "sla_breaches": true, "alert_events": true, "auth_transactions": true, "web_tickets": true, "meta_flags": true}

func mutation(query string) (Mutation, bool) {
	m := mutationRE.FindStringSubmatch(query)
	if len(m) == 0 {
		return Mutation{}, false
	}
	table := strings.ToLower(m[2])
	if auditTelemetry[table] || (table == "audit_head" || table == "audit_delivery" || table == "audit_events") || table == "security_audit" || strings.HasPrefix(table, "schema_") {
		return Mutation{}, false
	}
	// Presença do token não altera sua identidade ou escopo.
	if table == "agent_tokens" && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(query)), "UPDATE AGENT_TOKENS SET LAST_USED_AT") {
		return Mutation{}, false
	}
	op := strings.Fields(strings.ToLower(m[1]))[0]
	hash := sha256.Sum256([]byte(strings.TrimSpace(query)))
	return Mutation{Table: table, Operation: op, Statement: hex.EncodeToString(hash[:])}, true
}
func (t *Tx) track(query string, count int64, args ...any) {
	if t.audit == nil || count == 0 {
		return
	}
	m, ok := mutation(query)
	if !ok {
		return
	}
	if t.changes == nil {
		t.changes = make(map[string]Mutation)
	}
	key := m.Table + ":" + m.Statement
	old := t.changes[key]
	m.Count = old.Count + count
	m.References = old.References
	m.Actor = old.Actor
	for _, ref := range safeAuditReferences(query, args) {
		if strings.HasPrefix(ref, "actor=") {
			m.Actor = strings.TrimPrefix(ref, "actor=")
		}
		if len(m.References) >= 32 {
			break
		}
		found := false
		for _, previous := range m.References {
			if previous == ref {
				found = true
				break
			}
		}
		if !found {
			m.References = append(m.References, ref)
		}
	}
	t.changes[key] = m
}
func (t *Tx) appendAudit(payload AuditPayload) error {
	category, capacity := "security", t.audit.capacity
	if payload.Event != "" {
		category = "control"
		capacity = 10000
		if strings.HasPrefix(payload.Event, "access.") {
			category = "access"
			capacity = 1000
		}
	}
	var pending int64
	if err := t.Tx.QueryRow(rebind("SELECT COUNT(*) FROM audit_delivery WHERE acknowledged=0 AND category=?", t.dialect), category).Scan(&pending); err != nil {
		return fmt.Errorf("%w: backlog read", ErrAuditUnavailable)
	}
	if pending >= capacity {
		return ErrAuditFull
	}
	var r AuditRecord
	if err := t.Tx.QueryRow("SELECT stream,seq,hash FROM audit_head WHERE id=1").Scan(&r.Stream, &r.Seq, &r.Previous); err != nil {
		return fmt.Errorf("%w: head read", ErrAuditUnavailable)
	}
	r.Seq++
	r.TS = time.Now().UTC().Format(time.RFC3339Nano)
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	r.Payload = string(b)
	r.Hash = recordHash(r)
	r.Signature = hex.EncodeToString(ed25519.Sign(t.audit.key, []byte(r.Hash)))
	for _, stmt := range []struct {
		q string
		a []any
	}{
		{"INSERT INTO security_audit(seq,ts,previous,payload,hash,signature) VALUES(?,?,?,?,?,?)", []any{r.Seq, r.TS, r.Previous, r.Payload, r.Hash, r.Signature}},
		{"INSERT INTO audit_delivery(seq,category) VALUES(?,?)", []any{r.Seq, category}},
		{"UPDATE audit_head SET seq=?,hash=? WHERE id=1", []any{r.Seq, r.Hash}},
	} {
		if _, err := t.Tx.Exec(rebind(stmt.q, t.dialect), stmt.a...); err != nil {
			return fmt.Errorf("%w: journal write", ErrAuditUnavailable)
		}
	}
	return nil
}
func recordHash(r AuditRecord) string {
	r.Hash = ""
	r.Signature = ""
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func VerifyAuditRecord(r AuditRecord, public ed25519.PublicKey, previous string, seq int64) error {
	signature, err := hex.DecodeString(r.Signature)
	if err != nil || r.Seq != seq || r.Previous != previous || r.Hash != recordHash(r) || !ed25519.Verify(public, []byte(r.Hash), signature) {
		return fmt.Errorf("audit integrity mismatch at sequence %d", r.Seq)
	}
	return nil
}
func (d *DB) AuditRecords(after int64, limit int) ([]AuditRecord, error) {
	var stream string
	if err := d.QueryRow("SELECT stream FROM audit_head WHERE id=1").Scan(&stream); err != nil {
		return nil, err
	}
	rows, err := d.Query("SELECT seq,ts,previous,payload,hash,signature FROM security_audit WHERE seq>? ORDER BY seq LIMIT ?", after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditRecord{}
	for rows.Next() {
		r := AuditRecord{Stream: stream}
		if err = rows.Scan(&r.Seq, &r.TS, &r.Previous, &r.Payload, &r.Hash, &r.Signature); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (d *DB) VerifyAudit() error {
	opts := &sql.TxOptions{ReadOnly: true}
	if d.dialect == Postgres {
		opts.Isolation = sql.LevelRepeatableRead
	}
	tx, err := d.DB.BeginTx(context.Background(), opts)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var seq int64
	var hash, public, stream string
	if err = tx.QueryRow("SELECT stream,seq,hash,public_key FROM audit_head WHERE id=1").Scan(&stream, &seq, &hash, &public); err != nil {
		return err
	}
	if d.audit != nil && public != hex.EncodeToString(d.audit.key.Public().(ed25519.PublicKey)) {
		return fmt.Errorf("audit public key altered")
	}
	key, err := hex.DecodeString(public)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return fmt.Errorf("invalid audit public key")
	}
	rows, err := tx.Query("SELECT seq,ts,previous,payload,hash,signature FROM security_audit ORDER BY seq")
	if err != nil {
		return err
	}
	defer rows.Close()
	previous := ""
	var cursor int64
	for rows.Next() {
		r := AuditRecord{Stream: stream}
		if err = rows.Scan(&r.Seq, &r.TS, &r.Previous, &r.Payload, &r.Hash, &r.Signature); err != nil {
			return err
		}
		if err = VerifyAuditRecord(r, key, previous, cursor+1); err != nil {
			return err
		}
		cursor = r.Seq
		previous = r.Hash
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if cursor != seq || previous != hash {
		return fmt.Errorf("audit head mismatch")
	}
	var missing int64
	if err = tx.QueryRow("SELECT COUNT(*) FROM security_audit a LEFT JOIN audit_delivery d ON d.seq=a.seq WHERE d.seq IS NULL").Scan(&missing); err != nil {
		return err
	}
	if missing != 0 {
		return fmt.Errorf("audit delivery gap detected")
	}
	return nil
}
func (d *DB) AuditObservation(event, outcome string) error {
	if !d.AuditEnabled() {
		return nil
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = tx.appendAudit(AuditPayload{Identity: d.identity, Event: event, Outcome: outcome}); err != nil {
		return err
	}
	return tx.Tx.Commit()
}
func (t *Tx) Commit() error {
	if t.audit != nil && len(t.changes) > 0 {
		mutations := make([]Mutation, 0, len(t.changes))
		keys := make([]string, 0, len(t.changes))
		for key := range t.changes {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			mutations = append(mutations, t.changes[key])
		}
		identity := t.identity
		if identity.Actor == "" {
			identity.Actor = "system"
		}
		if err := t.appendAudit(AuditPayload{Identity: identity, Mutations: mutations, Outcome: "committed"}); err != nil {
			_ = t.Tx.Rollback()
			return err
		}
	}
	return t.Tx.Commit()
}

func (t *Tx) AuditAdministrative(event string) error {
	if t.audit == nil {
		return ErrAuditUnavailable
	}
	return t.appendAudit(AuditPayload{Identity: t.identity, Event: event, Outcome: "accepted"})
}

func (d *DB) VerifyAuditCheckpoint(stream string, seq int64, hash string) error {
	if err := d.VerifyAudit(); err != nil {
		return err
	}
	var own string
	var head int64
	if err := d.QueryRow("SELECT stream,seq FROM audit_head WHERE id=1").Scan(&own, &head); err != nil {
		return err
	}
	if own != stream || seq < 0 || seq > head {
		return fmt.Errorf("audit rollback or stream mismatch against independent checkpoint")
	}
	if seq == 0 {
		if hash != "" {
			return fmt.Errorf("invalid empty checkpoint")
		}
		return nil
	}
	var stored string
	if err := d.QueryRow("SELECT hash FROM security_audit WHERE seq=?", seq).Scan(&stored); err != nil {
		return err
	}
	if stored != hash {
		return fmt.Errorf("audit independent checkpoint mismatch")
	}
	return nil
}

func (d *DB) WithAuditActor(actor string) *DB {
	identity := d.identity
	identity.Actor = actor
	return d.WithAuditIdentity(identity)
}

var insertRefsRE = regexp.MustCompile(`(?is)^\s*INSERT(?:\s+OR\s+(?:IGNORE|REPLACE))?\s+INTO\s+\w+\s*\(([^)]+)\)\s*VALUES\s*\(([^)]+)\)`)
var whereRefsRE = regexp.MustCompile(`(?i)\b(id|user_id|agent_id|execution_id|order_id|instance_id|order_date|definition_id)\s*=\s*\?`)
var safeRefColumns = map[string]bool{"id": true, "user_id": true, "agent_id": true, "execution_id": true, "order_id": true, "instance_id": true, "order_date": true, "definition_id": true, "session_id": true, "folder_name": true, "name": true, "actor": true, "action": true, "operation": true}

// Apenas identificadores declarados. Tokens, payloads, valores e output são proibidos.
func safeAuditReferences(query string, args []any) []string {
	refs := []string{}
	add := func(column string, index int) {
		if index < 0 || index >= len(args) || !safeRefColumns[column] {
			return
		}
		value := fmt.Sprint(args[index])
		if len(value) > 128 {
			return
		}
		refs = append(refs, column+"="+value)
	}
	if m := insertRefsRE.FindStringSubmatch(query); len(m) > 0 {
		columns := strings.Split(m[1], ",")
		values := strings.Split(m[2], ",")
		index := 0
		for i, v := range values {
			if strings.TrimSpace(v) == "?" && i < len(columns) {
				add(strings.ToLower(strings.TrimSpace(columns[i])), index)
			}
			index += strings.Count(v, "?")
		}
	}
	upper := strings.ToUpper(query)
	where := strings.Index(upper, " WHERE ")
	if where >= 0 {
		for _, m := range whereRefsRE.FindAllStringSubmatchIndex(query[where:], -1) {
			column := strings.ToLower(query[where+m[2] : where+m[3]])
			index := strings.Count(query[:where+m[1]], "?") - 1
			add(column, index)
		}
	}
	return refs
}

func boundedAuditIdentity(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	digest := sha256.Sum256([]byte(value))
	return strings.ToValidUTF8(value[:max(0, limit-80)], "?") + "...sha256:" + hex.EncodeToString(digest[:])
}
