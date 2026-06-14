// Package auth — autenticação por senha + sessões em SQLite (F11.10).
//
// Modelo simples para v1:
//   - users(role): admin | operator | viewer
//   - sessions(token PK, user_id, expires_at) — 7d default, rotacionada a cada login
//   - bcrypt para password hashing (cost 10)
//   - bootstrap: se nao houver usuario admin, cria admin/admin com must_change_pw=1
//
// folder_acls existe no schema mas ainda nao e consumida (F11.10b).
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
	"golang.org/x/crypto/bcrypt"
)

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

func (r Role) Valid() bool {
	return r == RoleAdmin || r == RoleOperator || r == RoleViewer
}

// CanWrite indica se a role pode mutar definitions/folders/instances.
func (r Role) CanWrite() bool {
	return r == RoleAdmin || r == RoleOperator
}

// CanAdmin indica se a role pode gerenciar users.
func (r Role) CanAdmin() bool {
	return r == RoleAdmin
}

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
	MustChangePW bool      `json:"mustChangePassword"`
}

const sessionTTL = 7 * 24 * time.Hour
const ctxUserKey ctxKey = "regente.user"

type ctxKey string

// Bootstrap cria o admin inicial se nenhum usuario existir.
// Senha default = "admin" + must_change_pw=1.
func Bootstrap(db *db.DB) error {
	var n int
	if err := db.QueryRow("SELECT COUNT(1) FROM users").Scan(&n); err != nil {
		return fmt.Errorf("count users: %w", err)
	}
	if n > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if _, err := db.Exec(
		"INSERT INTO users(username,password_hash,role,must_change_pw) VALUES(?,?,?,1)",
		"admin", string(hash), string(RoleAdmin),
	); err != nil {
		return err
	}
	log.Println("[auth] bootstrap: created default user admin/admin (must change password)")
	return nil
}

// Login verifica senha e cria nova sessao. Retorna (token, user, err).
func Login(db *db.DB, username, password string) (string, *User, error) {
	row := db.QueryRow("SELECT id, username, password_hash, role, created_at, must_change_pw FROM users WHERE username = ?", username)
	var (
		id           int64
		uname        string
		hash         string
		role         string
		created      time.Time
		mustChangePW int
	)
	if err := row.Scan(&id, &uname, &hash, &role, &created, &mustChangePW); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, ErrInvalidCredentials
		}
		return "", nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return "", nil, ErrInvalidCredentials
	}
	tok, err := newToken()
	if err != nil {
		return "", nil, err
	}
	expires := time.Now().Add(sessionTTL)
	if _, err := db.Exec("INSERT INTO sessions(token,user_id,expires_at) VALUES(?,?,?)", tok, id, expires); err != nil {
		return "", nil, err
	}
	return tok, &User{
		ID:           id,
		Username:     uname,
		Role:         Role(role),
		CreatedAt:    created,
		MustChangePW: mustChangePW != 0,
	}, nil
}

// LoginFederated cria/recupera um usuário federado (SSO/OIDC) e abre uma sessão
// sem senha local. H1: o IdP já autenticou; aqui só provisionamos o usuário
// (defaultRole no 1º acesso) e emitimos o token de sessão do Regente. O hash
// "!federated" não é bcrypt válido → esses usuários nunca logam por senha.
func LoginFederated(db *db.DB, username string, defaultRole Role) (string, *User, error) {
	if username == "" {
		return "", nil, errors.New("federated login: empty username")
	}
	if !defaultRole.Valid() {
		defaultRole = RoleViewer
	}
	var (
		id      int64
		role    string
		created time.Time
	)
	err := db.QueryRow("SELECT id, role, created_at FROM users WHERE username = ?", username).Scan(&id, &role, &created)
	if errors.Is(err, sql.ErrNoRows) {
		newID, e := db.InsertID(
			"INSERT INTO users(username,password_hash,role,must_change_pw) VALUES(?,?,?,0)",
			username, "!federated", string(defaultRole),
		)
		if e != nil {
			return "", nil, e
		}
		id, role, created = newID, string(defaultRole), time.Now()
	} else if err != nil {
		return "", nil, err
	}
	tok, e := newToken()
	if e != nil {
		return "", nil, e
	}
	if _, e := db.Exec("INSERT INTO sessions(token,user_id,expires_at) VALUES(?,?,?)", tok, id, time.Now().Add(sessionTTL)); e != nil {
		return "", nil, e
	}
	return tok, &User{ID: id, Username: username, Role: Role(role), CreatedAt: created}, nil
}

// Logout invalida o token (best-effort; nao falha se nao existir).
func Logout(db *db.DB, token string) error {
	_, err := db.Exec("DELETE FROM sessions WHERE token = ?", token)
	return err
}

// Resolve devolve o user de um token valido ou erro.
func Resolve(db *db.DB, token string) (*User, error) {
	row := db.QueryRow(`
		SELECT u.id, u.username, u.role, u.created_at, u.must_change_pw, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token = ?`, token)
	var (
		id           int64
		uname        string
		role         string
		created      time.Time
		mustChangePW int
		expires      time.Time
	)
	if err := row.Scan(&id, &uname, &role, &created, &mustChangePW, &expires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	if time.Now().After(expires) {
		_, _ = db.Exec("DELETE FROM sessions WHERE token = ?", token)
		return nil, ErrInvalidToken
	}
	return &User{
		ID:           id,
		Username:     uname,
		Role:         Role(role),
		CreatedAt:    created,
		MustChangePW: mustChangePW != 0,
	}, nil
}

// ChangePassword troca a senha (verifica senha atual primeiro, exceto quando
// mustChange=true ou caller eh admin).
func ChangePassword(db *db.DB, userID int64, current, next string, force bool) error {
	if next == "" {
		return errors.New("password cannot be empty")
	}
	if !force {
		var hash string
		if err := db.QueryRow("SELECT password_hash FROM users WHERE id = ?", userID).Scan(&hash); err != nil {
			return err
		}
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(current)); err != nil {
			return ErrInvalidCredentials
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = db.Exec("UPDATE users SET password_hash=?, must_change_pw=0 WHERE id=?", string(hash), userID)
	return err
}

// CreateUser insere novo usuario (admin only no caller).
func CreateUser(db *db.DB, username, password string, role Role) (*User, error) {
	if username == "" || password == "" {
		return nil, errors.New("username and password are required")
	}
	if !role.Valid() {
		return nil, errors.New("invalid role")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	id, err := db.InsertID(
		"INSERT INTO users(username,password_hash,role,must_change_pw) VALUES(?,?,?,1)",
		username, string(hash), string(role),
	)
	if err != nil {
		return nil, err
	}
	return &User{ID: id, Username: username, Role: role, CreatedAt: time.Now(), MustChangePW: true}, nil
}

// SetRole atualiza role de um user.
func SetRole(db *db.DB, userID int64, role Role) error {
	if !role.Valid() {
		return errors.New("invalid role")
	}
	_, err := db.Exec("UPDATE users SET role=? WHERE id=?", string(role), userID)
	return err
}

// DeleteUser remove user (cascateia sessions/acls).
func DeleteUser(db *db.DB, userID int64) error {
	_, err := db.Exec("DELETE FROM users WHERE id=?", userID)
	return err
}

// ListUsers retorna todos.
func ListUsers(db *db.DB) ([]User, error) {
	rows, err := db.Query("SELECT id, username, role, created_at, must_change_pw FROM users ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		var role string
		var mustChange int
		if err := rows.Scan(&u.ID, &u.Username, &role, &u.CreatedAt, &mustChange); err != nil {
			return nil, err
		}
		u.Role = Role(role)
		u.MustChangePW = mustChange != 0
		out = append(out, u)
	}
	return out, rows.Err()
}

// FromContext extrai o user injetado pelo middleware.
func FromContext(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(ctxUserKey).(*User)
	return u, ok
}

// WithUser injeta o user no context.
func WithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, ctxUserKey, u)
}

// ExtractToken devolve o bearer token do request (header ou query).
func ExtractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	if q := r.URL.Query().Get("token"); q != "" {
		return q
	}
	return ""
}

// PurgeExpiredSessions deve ser chamado periodicamente (ou no boot).
func PurgeExpiredSessions(db *db.DB) error {
	_, err := db.Exec("DELETE FROM sessions WHERE expires_at < CURRENT_TIMESTAMP")
	return err
}

// ─────────────────────────────────────────────────────────────────
// F11.10b — Per-folder ACL
// ─────────────────────────────────────────────────────────────────
// Modelo:
//   - admin: bypassa ACL (acesso total).
//   - operator/viewer: se NAO houver nenhuma linha em folder_acls
//     para o user, comportamento default = somente leitura em todas
//     as folders. Se houver pelo menos uma linha, o user passa para
//     "modo restrito" e so ve folders explicitamente listadas com
//     read+; so muta folders com write.
//   - perms: string CSV simples, valores "r" e "w" (subset).
//
// O modelo "default deny quando ha qualquer ACL" e a unica forma de
// dar permissao granular sem precisar listar TODAS as folders existentes
// para cada user.

// FolderACL representa uma linha em folder_acls.
type FolderACL struct {
	UserID     int64  `json:"userId"`
	FolderName string `json:"folder"`
	Perms      string `json:"perms"` // "r", "rw", etc.
}

// HasRead retorna se a string de perms inclui leitura.
func aclHasRead(p string) bool { return strings.Contains(p, "r") }

// HasWrite retorna se a string de perms inclui escrita.
func aclHasWrite(p string) bool { return strings.Contains(p, "w") }

// userACLMap carrega TODAS as ACLs de um user em map name->perms.
// Retorna (acls, hasAny). Se hasAny=false, user esta em modo permissivo
// (default read-all). Se hasAny=true, user esta em modo restrito.
func userACLMap(db *db.DB, userID int64) (map[string]string, bool, error) {
	rows, err := db.Query("SELECT folder_name, perms FROM folder_acls WHERE user_id=?", userID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, perms string
		if err := rows.Scan(&name, &perms); err != nil {
			return nil, false, err
		}
		out[name] = perms
	}
	return out, len(out) > 0, rows.Err()
}

// CanReadFolder verifica se user pode ler uma folder.
func CanReadFolder(db *db.DB, u *User, folder string) (bool, error) {
	if u == nil {
		return false, nil
	}
	if u.Role == RoleAdmin {
		return true, nil
	}
	acls, restricted, err := userACLMap(db, u.ID)
	if err != nil {
		return false, err
	}
	if !restricted {
		return true, nil // modo permissivo
	}
	return aclHasRead(acls[folder]), nil
}

// CanWriteFolder verifica se user pode mutar artefatos de uma folder.
// Operator/viewer sem write explicito = no.
func CanWriteFolder(db *db.DB, u *User, folder string) (bool, error) {
	if u == nil {
		return false, nil
	}
	if u.Role == RoleAdmin {
		return true, nil
	}
	if !u.Role.CanWrite() {
		return false, nil // viewer nunca escreve
	}
	acls, restricted, err := userACLMap(db, u.ID)
	if err != nil {
		return false, err
	}
	if !restricted {
		return true, nil // operator sem ACLs = pode tudo
	}
	return aclHasWrite(acls[folder]), nil
}

// FilterReadableFolders devolve subset de folders que user pode ler.
// Para admin / unrestricted user, devolve a lista original.
func FilterReadableFolders(db *db.DB, u *User, folders []string) ([]string, error) {
	if u == nil {
		return nil, nil
	}
	if u.Role == RoleAdmin {
		return folders, nil
	}
	acls, restricted, err := userACLMap(db, u.ID)
	if err != nil {
		return nil, err
	}
	if !restricted {
		return folders, nil
	}
	out := make([]string, 0, len(folders))
	for _, f := range folders {
		if aclHasRead(acls[f]) {
			out = append(out, f)
		}
	}
	return out, nil
}

// ListUserACLs devolve todas as ACLs de um user.
func ListUserACLs(db *db.DB, userID int64) ([]FolderACL, error) {
	rows, err := db.Query("SELECT user_id, folder_name, perms FROM folder_acls WHERE user_id=? ORDER BY folder_name", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FolderACL{}
	for rows.Next() {
		var a FolderACL
		if err := rows.Scan(&a.UserID, &a.FolderName, &a.Perms); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetUserACL upsert de uma ACL. perms="" deleta a linha.
func SetUserACL(db *db.DB, userID int64, folder, perms string) error {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return errors.New("folder required")
	}
	perms = strings.TrimSpace(perms)
	// normalize: so r, w
	clean := ""
	if strings.Contains(perms, "r") {
		clean += "r"
	}
	if strings.Contains(perms, "w") {
		clean += "w"
	}
	if clean == "" {
		_, err := db.Exec("DELETE FROM folder_acls WHERE user_id=? AND folder_name=?", userID, folder)
		return err
	}
	_, err := db.Exec(`
		INSERT INTO folder_acls(user_id, folder_name, perms) VALUES(?,?,?)
		ON CONFLICT(user_id, folder_name) DO UPDATE SET perms=excluded.perms`,
		userID, folder, clean)
	return err
}

func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidToken       = errors.New("invalid or expired token")
)
