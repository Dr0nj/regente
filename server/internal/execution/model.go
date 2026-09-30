// Package execution — contrato v2 do servidor, restrito ao laboratório I08.
package execution

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"time"
)

const ProtocolVersion = 2
const Capability = "EXECUTION_V2"
const MaxOutputBytes = 5 << 20

var ErrConflict = errors.New("execution state or identity conflict")
var ErrCapacity = errors.New("execution admission is full; retry later")
var ErrInvalid = errors.New("invalid execution request")
var ErrNotFound = errors.New("execution record not found")
var ErrOutputLimit = errors.New("execution output limit exceeded")

type Engine struct {
	DB             *db.DB
	Now            func() time.Time
	MaxPending     int
	DeliveryLease  time.Duration
	ExecutionLease time.Duration
	MaxDeliveries  int
	// A notificação é apenas uma pista. A recuperação consulta sempre a outbox.
	Notify func(string)
}

func New(d *db.DB, now func() time.Time) *Engine {
	if now == nil {
		now = time.Now
	}
	return &Engine{DB: d, Now: now, MaxPending: 1000, DeliveryLease: 30 * time.Second, ExecutionLease: 5 * time.Minute, MaxDeliveries: 10}
}
func (e *Engine) now() int64 { return e.Now().UTC().UnixMilli() }
func checksum(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
func newID() (string, error) {
	v := make([]byte, 16)
	if _, err := rand.Read(v); err != nil {
		return "", err
	}
	return hex.EncodeToString(v), nil
}
func validKey(s string) bool { return len(s) > 0 && len(s) <= 128 }
func (e *Engine) notify(id string) {
	if e.Notify != nil {
		func() { defer func() { _ = recover() }(); e.Notify(id) }()
	}
}

type Order struct {
	ID               string `json:"id"`
	SourceInstanceID string `json:"sourceInstanceId"`
	State            string `json:"state"`
	CurrentExecution string `json:"currentExecution"`
	Attempt          int    `json:"attempt"`
	Fence            int64  `json:"fence"`
}
type Attempt struct {
	ExecutionID string `json:"executionId"`
	OrderID     string `json:"orderId"`
	Attempt     int    `json:"attempt"`
	Fence       int64  `json:"fence"`
	AgentID     string `json:"agentId"`
	State       string `json:"state"`
	LeaseUntil  int64  `json:"leaseUntil"`
	AcceptedAt  int64  `json:"acceptedAt"`
	StartedAt   int64  `json:"startedAt"`
	FinishedAt  int64  `json:"finishedAt"`
}
type Identity struct {
	Protocol    int    `json:"protocol"`
	ExecutionID string `json:"executionId"`
	Fence       int64  `json:"fence"`
	// AgentID nunca vem do body: a API preenche a partir do principal autenticado.
	AgentID string `json:"-"`
}
type Result struct {
	Identity
	ExitCode int    `json:"exitCode"`
	Output   string `json:"output"`
}
type Output struct {
	Identity
	Seq   int64  `json:"seq"`
	Chunk string `json:"chunk"`
}
type Envelope struct {
	Protocol       int                   `json:"protocol"`
	Kind           string                `json:"kind"`
	MessageID      string                `json:"messageId"`
	ExecutionID    string                `json:"executionId"`
	OrderID        string                `json:"orderId"`
	Attempt        int                   `json:"attempt"`
	AgentID        string                `json:"agentId"`
	Fence          int64                 `json:"fence"`
	IdempotencyKey string                `json:"idempotencyKey"`
	Definition     *domain.JobDefinition `json:"definition,omitempty"`
}
type Receipt struct {
	Accepted    bool   `json:"accepted"`
	Duplicate   bool   `json:"duplicate"`
	ExecutionID string `json:"executionId"`
}

func receipt(id string, duplicate bool) Receipt { return Receipt{true, duplicate, id} }
func resultChecksum(r Result) string {
	b, _ := json.Marshal(struct {
		Exit   int
		Output string
	}{r.ExitCode, r.Output})
	return checksum(string(b))
}
func terminal(state string) bool {
	return state == "succeeded" || state == "failed" || state == "cancelled"
}
