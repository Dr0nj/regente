// Package journal — aceite e resultados duráveis por executionId.
package journal

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const Version = 1

// UnknownExitCode é reservado ao adapter; não representa um exit do processo.
const UnknownExitCode = -2147483648
const MaxOutput = 5 << 20

var ErrConflict = errors.New("journal execution identity conflict")
var ErrCapacity = errors.New("journal admission is full")
var ErrOutputLimit = errors.New("journal output limit exceeded")

type Envelope struct {
	Protocol       int             `json:"protocol"`
	Kind           string          `json:"kind"`
	MessageID      string          `json:"messageId"`
	ExecutionID    string          `json:"executionId"`
	OrderID        string          `json:"orderId"`
	Attempt        int             `json:"attempt"`
	AgentID        string          `json:"agentId"`
	Fence          int64           `json:"fence"`
	IdempotencyKey string          `json:"idempotencyKey"`
	Definition     json.RawMessage `json:"definition,omitempty"`
}
type Entry struct {
	Envelope                                 Envelope
	State                                    string
	AcceptedAck, StartedAck, CancelRequested bool
	ExitCode                                 int
	Output, Reason                           string
	ProcessID                                int
}
type Chunk struct {
	Seq  int64
	Text string
}
type Journal struct {
	DB         *sql.DB
	file       *os.File
	mu         sync.Mutex
	AgentID    string
	MaxPending int
}

func Open(path, agent string) (j *Journal, err error) {
	if path == "" || agent == "" {
		return nil, errors.New("durable journal path and agent identity are required")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockFile(lock); err != nil {
		lock.Close()
		return nil, fmt.Errorf("journal already owned or lock unavailable: %w", err)
	}
	defer func() {
		if err != nil {
			lock.Close()
		}
	}()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	file.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	d, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1)
	defer func() {
		if err != nil {
			d.Close()
		}
	}()
	var integrity string
	if err = d.QueryRow("PRAGMA quick_check").Scan(&integrity); err != nil {
		return nil, err
	}
	if integrity != "ok" {
		return nil, errors.New("journal integrity check failed; preserve the journal for recovery")
	}
	var count int
	if err = d.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='journal_meta'").Scan(&count); err != nil {
		return nil, err
	}
	if count == 0 {
		// Uma base existente de outra aplicação nunca recebe um schema inventado.
		var tables int
		if err = d.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil {
			return nil, err
		}
		if tables != 0 {
			return nil, errors.New("unknown journal database")
		}
		tx, e := d.Begin()
		if e != nil {
			return nil, e
		}
		for _, s := range []string{
			"CREATE TABLE journal_meta(version INTEGER NOT NULL,agent_id TEXT NOT NULL)",
			"CREATE TABLE journal_entries(execution_id TEXT PRIMARY KEY,envelope TEXT NOT NULL,fingerprint TEXT NOT NULL,state TEXT NOT NULL,accepted_ack INTEGER NOT NULL DEFAULT 0,started_ack INTEGER NOT NULL DEFAULT 0,cancel_requested INTEGER NOT NULL DEFAULT 0,exit_code INTEGER NOT NULL DEFAULT 0,output TEXT NOT NULL DEFAULT '',reason TEXT NOT NULL DEFAULT '',process_id INTEGER NOT NULL DEFAULT 0,output_bytes INTEGER NOT NULL DEFAULT 0,last_seq INTEGER NOT NULL DEFAULT 0,acked_at INTEGER NOT NULL DEFAULT 0)",
			"CREATE TABLE journal_chunks(execution_id TEXT NOT NULL REFERENCES journal_entries(execution_id),seq INTEGER NOT NULL,chunk TEXT NOT NULL,acked INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(execution_id,seq))",
		} {
			if _, e = tx.Exec(s); e != nil {
				tx.Rollback()
				return nil, e
			}
		}
		if _, e = tx.Exec("INSERT INTO journal_meta(version,agent_id) VALUES(?,?)", Version, agent); e != nil {
			tx.Rollback()
			return nil, e
		}
		if e = tx.Commit(); e != nil {
			return nil, e
		}
	}
	if err = d.QueryRow("SELECT COUNT(*) FROM journal_meta").Scan(&count); err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, errors.New("journal metadata is corrupt")
	}
	var version int
	var owner string
	if err = d.QueryRow("SELECT version,agent_id FROM journal_meta").Scan(&version, &owner); err != nil {
		return nil, err
	}
	if version != Version || owner != agent {
		return nil, errors.New("journal version or agent identity is incompatible")
	}
	var invalid int
	if err = d.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE state NOT IN ('accepted','starting','running','result_pending','cancel_pending','uncertain','acked') OR execution_id=''").Scan(&invalid); err != nil {
		return nil, err
	}
	if invalid != 0 {
		return nil, errors.New("journal state is corrupt; automatic recovery refused")
	}
	rows, e := d.Query("SELECT execution_id,envelope,fingerprint,state FROM journal_entries")
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var id, raw, hash, state string
		if e = rows.Scan(&id, &raw, &hash, &state); e != nil {
			rows.Close()
			return nil, e
		}
		var message Envelope
		if json.Unmarshal([]byte(raw), &message) != nil || message.ExecutionID != id || !valid(message, agent) || (message.Kind != "dispatch" && message.Kind != "cancel") || (state != "acked" && message.Kind == "dispatch" && fingerprint(message) != hash) {
			rows.Close()
			return nil, errors.New("journal envelope is corrupt; automatic recovery refused")
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	// O lock de arquivo prova que o worker anterior terminou; não prova que seus efeitos/filhos pararam.
	if _, err = d.Exec("UPDATE journal_entries SET state='uncertain',reason='worker restarted with an unresolved effect' WHERE state IN ('starting','running')"); err != nil {
		return nil, err
	}
	return &Journal{DB: d, file: lock, AgentID: agent, MaxPending: 1000}, nil
}
func (j *Journal) Close() error {
	err := j.DB.Close()
	e := j.file.Close()
	if err != nil {
		return err
	}
	return e
}
func fingerprint(e Envelope) string {
	b, _ := json.Marshal(e)
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}
func valid(e Envelope, agent string) bool {
	return e.Protocol == 2 && e.ExecutionID != "" && e.OrderID != "" && e.AgentID == agent && e.Fence > 0 && e.Attempt > 0 && e.IdempotencyKey == e.ExecutionID
}
func sameIdentity(a, b Envelope) bool {
	return a.ExecutionID == b.ExecutionID && a.AgentID == b.AgentID && a.OrderID == b.OrderID && a.Fence == b.Fence && a.Attempt == b.Attempt
}

const selectEntry = "SELECT envelope,state,accepted_ack,started_ack,cancel_requested,exit_code,output,reason,process_id FROM journal_entries WHERE execution_id=?"

func entry(row *sql.Row) (e Entry, err error) {
	var raw string
	err = row.Scan(&raw, &e.State, &e.AcceptedAck, &e.StartedAck, &e.CancelRequested, &e.ExitCode, &e.Output, &e.Reason, &e.ProcessID)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &e.Envelope)
	}
	return
}
func (j *Journal) Entry(id string) (Entry, error) { return entry(j.DB.QueryRow(selectEntry, id)) }
func (j *Journal) Accept(e Envelope) (Entry, error) {
	if !valid(e, j.AgentID) || (e.Kind != "dispatch" && e.Kind != "cancel") || (e.Kind == "dispatch" && len(e.Definition) == 0) {
		return Entry{}, ErrConflict
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	tx, err := j.DB.Begin()
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()
	prior, err := entry(tx.QueryRow(selectEntry, e.ExecutionID))
	if err == nil {
		var hash string
		if err = tx.QueryRow("SELECT fingerprint FROM journal_entries WHERE execution_id=?", e.ExecutionID).Scan(&hash); err != nil {
			return Entry{}, err
		}
		if !sameIdentity(prior.Envelope, e) || (e.Kind == "dispatch" && hash != "" && hash != fingerprint(e)) {
			return Entry{}, ErrConflict
		}
		if e.Kind == "cancel" && prior.State != "acked" && prior.State != "result_pending" {
			next := prior.State
			if next == "accepted" {
				next = "cancel_pending"
			}
			if _, err = tx.Exec("UPDATE journal_entries SET cancel_requested=1,state=? WHERE execution_id=?", next, e.ExecutionID); err != nil {
				return Entry{}, err
			}
			prior.CancelRequested = true
			prior.State = next
		}
		return prior, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Entry{}, err
	}
	var n int
	if err = tx.QueryRow("SELECT COUNT(*) FROM journal_entries WHERE state!='acked'").Scan(&n); err != nil {
		return Entry{}, err
	}
	// Cancelamento sem dispatch vira tombstone; a intenção de controle não exige executar efeito.
	if n >= j.MaxPending && e.Kind != "cancel" {
		return Entry{}, ErrCapacity
	}
	state, hash := "accepted", fingerprint(e)
	cancelled := e.Kind == "cancel"
	if cancelled {
		state, hash = "cancel_pending", ""
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return Entry{}, err
	}
	if _, err = tx.Exec("INSERT INTO journal_entries(execution_id,envelope,fingerprint,state,cancel_requested) VALUES(?,?,?,?,?)", e.ExecutionID, string(raw), hash, state, cancelled); err != nil {
		return Entry{}, err
	}
	if err = tx.Commit(); err != nil {
		return Entry{}, err
	}
	return Entry{Envelope: e, State: state, CancelRequested: cancelled}, nil
}
func (j *Journal) Pending() ([]Entry, error) {
	rows, err := j.DB.Query("SELECT execution_id FROM journal_entries WHERE state!='acked' ORDER BY execution_id LIMIT 1001")
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []Entry{}
	for _, id := range ids {
		e, err := j.Entry(id)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}
func (j *Journal) AckPhase(id, kind string) error {
	column := "accepted_ack"
	if kind == "started" {
		column = "started_ack"
	} else if kind != "accepted" {
		return ErrConflict
	}
	_, err := j.DB.Exec("UPDATE journal_entries SET "+column+"=1 WHERE execution_id=?", id)
	return err
}
func (j *Journal) BeginEffect(id string) (bool, error) {
	r, err := j.DB.Exec("UPDATE journal_entries SET state='starting' WHERE execution_id=? AND state='accepted' AND accepted_ack=1 AND cancel_requested=0", id)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}
func (j *Journal) Running(id string) error {
	_, err := j.DB.Exec("UPDATE journal_entries SET state='running' WHERE execution_id=? AND state='starting' AND started_ack=1", id)
	return err
}
func (j *Journal) CancelBeforeEffect(id string) error {
	_, err := j.DB.Exec("UPDATE journal_entries SET cancel_requested=1,state='cancel_pending' WHERE execution_id=? AND state IN ('accepted','starting')", id)
	return err
}
func (j *Journal) Process(id string, pid int) error {
	_, err := j.DB.Exec("UPDATE journal_entries SET process_id=? WHERE execution_id=? AND state IN ('starting','running')", pid, id)
	return err
}
func (j *Journal) Uncertain(id, why string) error {
	_, err := j.DB.Exec("UPDATE journal_entries SET state='uncertain',reason=? WHERE execution_id=? AND state!='acked'", why, id)
	return err
}
func (j *Journal) Result(id string, code int, output string) error {
	output = strings.ToValidUTF8(output, "�")
	if len(output) > MaxOutput {
		output = output[:MaxOutput]
		for !utf8.ValidString(output) {
			output = output[:len(output)-1]
		}
	}
	// Cancel só é confirmado depois do retorno do executor; não promete rollback do efeito externo.
	r, err := j.DB.Exec("UPDATE journal_entries SET state=CASE WHEN cancel_requested=1 THEN 'cancel_pending' ELSE 'result_pending' END,exit_code=?,output=? WHERE execution_id=? AND state IN ('starting','running')", code, output, id)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n != 1 {
		return ErrConflict
	}
	return err
}
func (j *Journal) Append(id, text string) error {
	text = strings.ToValidUTF8(text, "�")
	if text == "" {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	tx, err := j.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var bytes, seq int
	var state string
	if err = tx.QueryRow("SELECT output_bytes,last_seq,state FROM journal_entries WHERE execution_id=?", id).Scan(&bytes, &seq, &state); err != nil {
		return err
	}
	if state != "starting" && state != "running" {
		return ErrConflict
	}
	if bytes+len(text) > MaxOutput {
		return ErrOutputLimit
	}
	if _, err = tx.Exec("INSERT INTO journal_chunks(execution_id,seq,chunk) VALUES(?,?,?)", id, seq+1, text); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE journal_entries SET output_bytes=output_bytes+?,last_seq=last_seq+1 WHERE execution_id=?", len(text), id); err != nil {
		return err
	}
	return tx.Commit()
}
func (j *Journal) Chunks(id string) ([]Chunk, error) {
	rows, err := j.DB.Query("SELECT seq,chunk FROM journal_chunks WHERE execution_id=? AND acked=0 ORDER BY seq LIMIT 100", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Chunk{}
	for rows.Next() {
		var c Chunk
		if err = rows.Scan(&c.Seq, &c.Text); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (j *Journal) AckChunk(id string, seq int64) error {
	_, err := j.DB.Exec("UPDATE journal_chunks SET acked=1 WHERE execution_id=? AND seq=?", id, seq)
	return err
}
func (j *Journal) Ack(id string) error {
	_, err := j.DB.Exec("UPDATE journal_entries SET state='acked',acked_at=? WHERE execution_id=?", time.Now().UnixMilli(), id)
	return err
}

// Compact remove conteúdo confirmado após retenção, mantendo tombstone para dedup indefinida.
func (j *Journal) Compact(before time.Time) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	tx, err := j.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM journal_chunks WHERE execution_id IN (SELECT execution_id FROM journal_entries WHERE state='acked' AND acked_at<?)", before.UnixMilli()); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE journal_entries SET output='',envelope=json_remove(envelope,'$.definition') WHERE state='acked' AND acked_at<?", before.UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}
