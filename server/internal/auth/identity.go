package auth

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
)

func Digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func Mode(raw string) (string, error) {
	m := strings.ToLower(strings.TrimSpace(raw))
	if m == "" {
		m = "local"
	}
	if m != "local" && m != "hybrid" && m != "oidc" {
		return "", errors.New("auth mode must be local, hybrid or oidc")
	}
	return m, nil
}

// ConfigureSession converte o token recém-emitido em sessão curta e/ou de navegador.
func ConfigureSession(d *db.DB, token, source string, browser bool, expires time.Time) error {
	csrf, err := newToken()
	if err != nil {
		return err
	}
	b := 0
	if browser {
		b = 1
	}
	_, err = d.Exec("UPDATE sessions SET source=?,browser=?,csrf=?,expires_at=? WHERE token=?", source, b, csrf, expires, Digest(token))
	return err
}

// LoginExternal só resolve o par estável; claims de apresentação não ligam contas.
func LoginExternal(d *db.DB, issuer, subject, display string, role Role, expires time.Time) (string, *User, error) {
	if issuer == "" || subject == "" || !expires.After(time.Now()) {
		return "", nil, ErrInvalidCredentials
	}
	if !role.Valid() {
		role = RoleViewer
	}
	tx, err := d.Begin()
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRow("SELECT user_id FROM external_identities WHERE issuer=? AND subject=?", issuer, subject).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		// Identificador interno opaco evita colisão com admin ou conta legada.
		suffix, e := newToken()
		if e != nil {
			return "", nil, e
		}
		if len(display) > 80 {
			display = display[:80]
		}
		name := fmt.Sprintf("%s [sso-%s]", display, suffix[:16])
		if e = tx.QueryRow("INSERT INTO users(username,password_hash,role,must_change_pw) VALUES(?,?,?,0) RETURNING id", name, "!federated", string(role)).Scan(&id); e != nil {
			return "", nil, e
		}
		if _, e = tx.Exec("INSERT INTO external_identities(issuer,subject,user_id) VALUES(?,?,?)", issuer, subject, id); e != nil {
			return "", nil, e
		}
		if e = identityAudit(tx, "oidc", "provision", id, issuer, subject); e != nil {
			return "", nil, e
		}
	} else if err != nil {
		return "", nil, err
	}
	var disabled, pending int
	if err = tx.QueryRow("SELECT disabled,requires_link FROM users WHERE id=?", id).Scan(&disabled, &pending); err != nil || disabled != 0 || pending != 0 {
		return "", nil, ErrInvalidCredentials
	}
	token, err := newToken()
	if err != nil {
		return "", nil, err
	}
	csrf, err := newToken()
	if err != nil {
		return "", nil, err
	}
	limit := time.Now().Add(5 * time.Minute)
	if expires.After(limit) {
		expires = limit
	}
	if _, err = tx.Exec("INSERT INTO sessions(token,user_id,expires_at,source,browser,csrf) VALUES(?,?,?,'oidc',1,?)", Digest(token), id, expires, csrf); err != nil {
		return "", nil, err
	}
	if err = tx.Commit(); err != nil {
		return "", nil, err
	}
	u, err := Resolve(d, token)
	return token, u, err
}

func identityAudit(tx *db.Tx, actor, action string, userID int64, issuer, subject string) error {
	id, err := newToken()
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO identity_audit(event_id,actor,action,user_id,issuer,subject) VALUES(?,?,?,?,?,?)", id, actor, action, userID, issuer, subject)
	return err
}

// LinkExternal nega remapeamento de identidade já existente. Operador confirma o par no IdP.
func LinkExternal(d *db.DB, actor string, userID int64, issuer, subject string) error {
	if issuer == "" || subject == "" || userID <= 0 {
		return errors.New("issuer, subject and user are required")
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO external_identities(issuer,subject,user_id) VALUES(?,?,?)", issuer, subject, userID); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE users SET requires_link=0 WHERE id=?", userID); err != nil {
		return err
	}
	if err = identityAudit(tx, actor, "link", userID, issuer, subject); err != nil {
		return err
	}
	return tx.Commit()
}

func SetAccess(d *db.DB, actor string, userID int64, disabled bool) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	n := 0
	if disabled {
		n = 1
	}
	res, err := tx.Exec("UPDATE users SET disabled=? WHERE id=?", n, userID)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err != nil || count != 1 {
		return errors.New("user not found")
	}
	if _, err = tx.Exec("DELETE FROM sessions WHERE user_id=?", userID); err != nil {
		return err
	}
	if err = identityAudit(tx, actor, fmt.Sprintf("disabled=%t", disabled), userID, "", ""); err != nil {
		return err
	}
	return tx.Commit()
}
