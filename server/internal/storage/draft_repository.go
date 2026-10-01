package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Dr0nj/regente-server/internal/db"
	git "github.com/go-git/go-git/v5"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrDraftConflict = errors.New("draft changed in another request or node; reload before editing")
var ErrDraftNotFound = errors.New("draft not found")
var ErrDraftRecovery = errors.New("legacy draft needs its original local clone or a verified export; metadata has been preserved")
var ErrDraftPublishing = errors.New("draft publication is pending; retry publication or export before discarding")

const draftColumns = "id, actor, folders_json, new_folders_json, base_sha, path, created_at, last_touch, revision, state, dirty, publication"

type draftScanner interface{ Scan(...any) error }

func scanDraft(row draftScanner) (*DesignSession, error) {
	s := new(DesignSession)
	var folders, newFolders string
	var dirty int
	if err := row.Scan(&s.ID, &s.Actor, &folders, &newFolders, &s.BaseSHA, &s.Path, &s.CreatedAt, &s.LastTouch, &s.Revision, &s.State, &dirty, &s.publication); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(folders), &s.Folders); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(newFolders), &s.NewFolders); err != nil {
		return nil, err
	}
	s.sharedDirty = dirty != 0
	s.RecoveryRequired = s.Revision == 0
	return s, nil
}

func (m *SessionManager) Shared() bool { return m.db != nil }

func (m *SessionManager) Metadata(sid string) (*DesignSession, error) {
	if m.db == nil {
		m.mu.Lock()
		defer m.mu.Unlock()
		s, ok := m.items[sid]
		if !ok {
			return nil, ErrDraftNotFound
		}
		return s, nil
	}
	s, err := scanDraft(m.db.QueryRow("SELECT "+draftColumns+" FROM design_sessions WHERE id=?", sid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrDraftNotFound
	}
	return s, err
}

func (m *SessionManager) ListShared(actor string) ([]*DesignSession, error) {
	if m.db == nil {
		return m.List(actor), nil
	}
	query := "SELECT " + draftColumns + " FROM design_sessions"
	var args []any
	if actor != "" {
		query += " WHERE actor=?"
		args = append(args, actor)
	}
	query += " ORDER BY created_at DESC"
	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*DesignSession{}
	for rows.Next() {
		s, e := scanDraft(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (m *SessionManager) ownedLegacyPath(p string) bool {
	root, err := filepath.Abs(m.root)
	if err != nil {
		return false
	}
	target, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}

func (m *SessionManager) attach(s *DesignSession, path string) {
	m.mu.Lock()
	token := m.token
	m.mu.Unlock()
	s.Path = path
	s.Store = NewFileStore(path, false)
	s.Git = NewGitOps(path, m.source, m.branch)
	s.Git.InjectToken(token)
}

// Migração online verificável. Não remove nem modifica o clone original.
func (m *SessionManager) migrateLegacy(s *DesignSession) error {
	if !m.ownedLegacyPath(s.Path) {
		return ErrDraftRecovery
	}
	m.attach(s, s.Path)
	repo, err := git.PlainOpen(s.Path)
	if err != nil {
		return ErrDraftRecovery
	}
	remote, err := repo.Remote("origin")
	if err != nil || len(remote.Config().URLs) != 1 || scrubCredentials(remote.Config().URLs[0]) != s.Git.source {
		return fmt.Errorf("legacy draft origin differs; preserve original clone for explicit recovery")
	}
	sha, err := s.Git.HeadSHA()
	if err != nil {
		return ErrDraftRecovery
	}
	if s.BaseSHA == "" {
		s.BaseSHA = sha
	}
	payload, checksum, err := captureDraft(s)
	if err != nil {
		return err
	}
	snap, err := decodeDraft(payload, checksum)
	if err != nil {
		return err
	}
	verify, err := os.MkdirTemp(m.root, "verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(verify)
	if err = restoreDraft(verify, snap); err != nil {
		return fmt.Errorf("legacy draft verification failed; original clone preserved: %w", err)
	}
	s.State = "active"
	if err = m.Checkpoint(s, s.Actor, "legacy-import"); err != nil {
		return err
	}
	return nil
}

// Open fornece um cache privado por operação. Ausência local nunca apaga a linha.
func (m *SessionManager) Open(sid string) (*DesignSession, func(), error) {
	s, err := m.Metadata(sid)
	if err != nil {
		return nil, func() {}, err
	}
	if m.db == nil {
		return s, func() {}, nil
	}
	if s.Revision == 0 {
		if err = m.migrateLegacy(s); err != nil {
			return nil, func() {}, err
		}
		s, err = m.Metadata(sid)
		if err != nil {
			return nil, func() {}, err
		}
	}
	var payload, checksum string
	err = m.db.QueryRow("SELECT payload,checksum FROM design_draft_versions WHERE session_id=? AND revision=?", sid, s.Revision).Scan(&payload, &checksum)
	if err != nil {
		return nil, func() {}, err
	}
	snap, err := decodeDraft(payload, checksum)
	if err != nil {
		return nil, func() {}, err
	}
	if snap.Source != NewGitOps("", m.source, m.branch).source || snap.Branch != NewGitOps("", m.source, m.branch).branch || snap.BaseSHA != s.BaseSHA {
		return nil, func() {}, fmt.Errorf("draft Git source/branch/base differs from server configuration")
	}
	if err = os.MkdirAll(m.root, 0700); err != nil {
		return nil, func() {}, err
	}
	dir, err := os.MkdirTemp(m.root, "cache-")
	if err != nil {
		return nil, func() {}, err
	}
	cleanup := func() {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("[draft-cache] cleanup: %v", err)
		}
	}
	if err = restoreDraft(dir, snap); err != nil {
		cleanup()
		return nil, func() {}, err
	}
	m.attach(s, dir)
	s.Undo = snap.Undo
	s.scoped = true
	s.RecoveryRequired = false
	return s, cleanup, nil
}

// CAS + payload + auditoria pertencem à mesma transação, em ambos os bancos.
func (m *SessionManager) Checkpoint(s *DesignSession, actor, operation string) error {
	if m.db == nil {
		return nil
	}
	payload, checksum, err := captureDraft(s)
	if err != nil {
		return err
	}
	dirty := 0
	if s.Dirty() {
		dirty = 1
	}
	folders, _ := json.Marshal(s.Folders)
	newFolders, _ := json.Marshal(s.NewFolders)
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state := s.State
	if state == "" || state == "legacy" {
		state = "active"
	}
	now := time.Now()
	res, err := tx.Exec("UPDATE design_sessions SET folders_json=?,new_folders_json=?,base_sha=?,revision=revision+1,state=?,dirty=?,last_touch=?,publication=? WHERE id=? AND revision=? AND state IN ('active','legacy')", string(folders), string(newFolders), s.BaseSHA, state, dirty, now, s.publication, s.ID, s.Revision)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrDraftConflict
	}
	next := s.Revision + 1
	if _, err = tx.Exec("INSERT INTO design_draft_versions(session_id,revision,payload,checksum) VALUES(?,?,?,?)", s.ID, next, payload, checksum); err != nil {
		return err
	}
	if err = draftAudit(tx, s.ID, next, actor, operation, checksum, now.UnixMilli()); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.Revision = next
	s.LastTouch = now
	s.State = state
	s.sharedDirty = dirty != 0
	return nil
}

func (m *SessionManager) insertDraft(s *DesignSession, operation string) error {
	if m.db == nil {
		return nil
	}
	// Inserção + revisão inicial indivisíveis: jamais anunciar draft só com metadados.
	payload, checksum, err := captureDraft(s)
	if err != nil {
		return err
	}
	folders, _ := json.Marshal(s.Folders)
	newFolders, _ := json.Marshal(s.NewFolders)
	dirty := 0
	if s.Dirty() {
		dirty = 1
	}
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec("INSERT INTO design_sessions(id,actor,folders_json,new_folders_json,base_sha,path,created_at,last_touch,revision,state,dirty) VALUES(?,?,?,?,?,?,?,?,1,'active',?)", s.ID, s.Actor, string(folders), string(newFolders), s.BaseSHA, s.Path, s.CreatedAt, s.LastTouch, dirty)
	if err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO design_draft_versions(session_id,revision,payload,checksum) VALUES(?,1,?,?)", s.ID, payload, checksum); err != nil {
		return err
	}
	if err = draftAudit(tx, s.ID, 1, s.Actor, operation, checksum, time.Now().UnixMilli()); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.Revision = 1
	s.State = "active"
	s.sharedDirty = dirty != 0
	return nil
}

func (m *SessionManager) DeleteRevision(s *DesignSession, actor string) error {
	if m.db == nil {
		return m.Delete(s.ID)
	}
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec("DELETE FROM design_sessions WHERE id=? AND revision=? AND state IN ('active','legacy','published')", s.ID, s.Revision)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrDraftConflict
	}
	if err = draftAudit(tx, s.ID, s.Revision, actor, "discard", "", time.Now().UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}

type DraftExport struct {
	Format   int    `json:"format"`
	Actor    string `json:"actor"`
	Revision int64  `json:"revision"`
	Payload  string `json:"payload"`
	Checksum string `json:"checksum"`
}

func (m *SessionManager) Export(s *DesignSession) (*DraftExport, error) {
	payload, checksum, err := captureDraft(s)
	if err != nil {
		return nil, err
	}
	return &DraftExport{1, s.Actor, s.Revision, payload, checksum}, nil
}
func (m *SessionManager) Import(export DraftExport, actor string) (*DesignSession, error) {
	if export.Format != 1 {
		return nil, fmt.Errorf("unsupported draft export format")
	}
	snap, err := decodeDraft(export.Payload, export.Checksum)
	if err != nil {
		return nil, err
	}
	probe := NewGitOps("", m.source, m.branch)
	if snap.Source != probe.source || snap.Branch != probe.branch {
		return nil, fmt.Errorf("draft export Git source or branch differs")
	}
	if err = os.MkdirAll(m.root, 0700); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(m.root, "import-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	if err = restoreDraft(dir, snap); err != nil {
		return nil, err
	}
	now := time.Now()
	s := &DesignSession{ID: newSessionID(actor), Actor: actor, BaseSHA: snap.BaseSHA, Folders: snap.Folders, NewFolders: snap.NewFolders, CreatedAt: now, LastTouch: now}
	m.attach(s, dir)
	s.Undo = snap.Undo
	if m.db == nil {
		return nil, fmt.Errorf("draft import requires durable storage")
	}
	if err = m.insertDraft(s, "import"); err != nil {
		return nil, err
	}
	s.Path = ""
	s.Store = nil
	s.Git = nil
	return s, nil
}

func (m *SessionManager) Touch(s *DesignSession) error {
	if m.db == nil {
		return nil
	}
	_, err := m.db.Exec("UPDATE design_sessions SET last_touch=? WHERE id=? AND revision=?", time.Now(), s.ID, s.Revision)
	return err
}
func (m *SessionManager) deleteIdle(s *DesignSession) error {
	if m.db == nil {
		return m.Delete(s.ID)
	}
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec("DELETE FROM design_sessions WHERE id=? AND revision=? AND state='active' AND dirty=0 AND last_touch<=?", s.ID, s.Revision, s.LastTouch)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrDraftConflict
	}
	if err = draftAudit(tx, s.ID, s.Revision, "system", "gc", "", time.Now().UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}
func draftAudit(tx *db.Tx, sid string, revision int64, actor, operation, checksum string, at int64) error {
	if _, err := tx.Exec("INSERT INTO design_draft_audit(event_id,session_id,revision,actor,operation,checksum,at_ms) VALUES(?,?,?,?,?,?,?)", newSessionID("event"), sid, revision, actor, operation, checksum, at); err != nil {
		return err
	}
	detail, _ := json.Marshal(map[string]any{"revision": revision, "checksum": checksum})
	_, err := tx.Exec("INSERT INTO audit_events(kind,actor,action,target,outcome,ip,detail) VALUES('design.draft',?,?,?,'success','',?)", actor, operation, sid, string(detail))
	return err
}
