package auth

import (
	"errors"
	"github.com/Dr0nj/regente-server/internal/db"
	"golang.org/x/crypto/bcrypt"
	"strings"
)

func ProductionPassword(password string) error {
	if len(password) < 12 || len(password) > 72 || strings.TrimSpace(password) != password {
		return errors.New("production passwords require 12-72 bytes without surrounding whitespace")
	}
	return nil
}

// BootstrapProduction nunca cria credencial de exemplo nem sobrescreve contas.
func BootstrapProduction(d *db.DB, username, password, emergency string) error {
	var count int
	if err := d.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		if username == "" || strings.TrimSpace(username) != username || len(username) > 80 {
			return errors.New("invalid production bootstrap username")
		}
		if ProductionPassword(password) != nil {
			return errors.New("empty production database requires REGENTE_BOOTSTRAP_PASSWORD with 12-72 bytes without surrounding whitespace")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		if _, err = d.Exec("INSERT INTO users(username,password_hash,role,must_change_pw) VALUES(?,?,'admin',0) ON CONFLICT(username) DO NOTHING", username, string(hash)); err != nil {
			return err
		}
	}
	rows, err := d.Query("SELECT password_hash FROM users WHERE disabled=0 AND requires_link=0")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var hash string
		if err = rows.Scan(&hash); err != nil {
			return err
		}
		if hash == "!federated" {
			continue
		}
		if _, err = bcrypt.Cost([]byte(hash)); err != nil {
			return errors.New("invalid active local password hash; repair or disable the account before production")
		}
		for _, example := range []string{"admin", "password", "change-me", "dev-token"} {
			if bcrypt.CompareHashAndPassword([]byte(hash), []byte(example)) == nil {
				return errors.New("development password detected; rotate or disable the account before production")
			}
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	rows.Close()
	if emergency != "" {
		if err = d.QueryRow("SELECT COUNT(*) FROM users WHERE username=? AND role='admin' AND disabled=0 AND requires_link=0 AND must_change_pw=0 AND password_hash!='!federated'", emergency).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return errors.New("emergency user must be an active local administrator with a rotated password")
		}
	}
	return nil
}
