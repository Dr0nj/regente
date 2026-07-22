package scheduler

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
)

// captureBus — Bus que REGISTRA os frames de Dispatch (para assertar o sinal de
// kill do cancel) e é no-op no resto. Não roteia nada — não há agente real nos
// testes; o que importa é qual payload foi endereçado a qual agente.
type captureBus struct {
	mu       sync.Mutex
	dispatch []capturedFrame
}

type capturedFrame struct {
	agentID string
	raw     []byte
}

func (b *captureBus) BroadcastWeb(string, interface{})     {}
func (b *captureBus) PickAgent(string, string) *hub.Client { return nil }
func (b *captureBus) GetAgent(string) *hub.Client          { return nil }
func (b *captureBus) HasAgent(string, string, string) bool { return false }
func (b *captureBus) Dispatch(agentID, _, _ string, raw []byte) (hub.DispatchOutcome, string) {
	b.mu.Lock()
	b.dispatch = append(b.dispatch, capturedFrame{agentID, append([]byte(nil), raw...)})
	b.mu.Unlock()
	return hub.DispatchSent, agentID
}

// lastCancel devolve o último frame event=="cancel" enviado (agente + instanceId).
func (b *captureBus) lastCancel() (agentID, instanceID string, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := len(b.dispatch) - 1; i >= 0; i-- {
		var m map[string]string
		if json.Unmarshal(b.dispatch[i].raw, &m) == nil && m["event"] == "cancel" {
			return b.dispatch[i].agentID, m["instanceId"], true
		}
	}
	return "", "", false
}

// Cancel de um RUNNING mata o processo (frame de kill roteado ao agente que roda
// a instance) e finaliza NOTOK terminal — SEM re-armar o retry, mesmo com
// Retries>0 (foi kill manual, não falha orgânica).
func TestCancel_RunningKillsAndFinishesNotOKWithoutRetry(t *testing.T) {
	s := newTestScheduler(t)
	bus := &captureBus{}
	s.hub = bus
	today := time.Now().Format("2006-01-02")
	// Retries>0 de propósito: o kill NÃO pode disparar auto-retry.
	def := domain.JobDefinition{ID: "k", JobType: "COMMAND", Retries: 3, Schedule: domain.Schedule{Enabled: true}}
	id := seedInst(t, s, "k-1", today, string(domain.StatusRunning), def)
	if _, err := s.db.Exec(`UPDATE instances SET agent_id=? WHERE id=?`, "agent-x", id); err != nil {
		t.Fatalf("set agent: %v", err)
	}

	st, err := s.Cancel(id)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if st != domain.StatusNotOK {
		t.Fatalf("status retornado esperado NOTOK, veio %s", st)
	}

	// O kill foi roteado pro agente certo, com o instanceId certo.
	ag, inst, ok := bus.lastCancel()
	if !ok || ag != "agent-x" || inst != id {
		t.Fatalf("frame de kill não roteado: agent=%q inst=%q ok=%v", ag, inst, ok)
	}

	// Persistiu NOTOK terminal (exit -1, output de kill) e NÃO voltou a WAITING.
	var status, output string
	var exit int
	if err := s.db.QueryRow(
		`SELECT status, COALESCE(exit_code,0), COALESCE(output,'') FROM instances WHERE id=?`, id,
	).Scan(&status, &exit, &output); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != string(domain.StatusNotOK) {
		t.Fatalf("status persistido esperado NOTOK (retry NÃO deveria re-armar), veio %s", status)
	}
	if exit != -1 || !strings.Contains(output, "killed") {
		t.Fatalf("foto do kill inesperada: exit=%d out=%q", exit, output)
	}
}

// Um resultado TARDIO do agente (a execução morta reporta depois) é IGNORADO
// pelo guard terminal do FinishInstance: não sobrescreve a foto do kill nem
// re-arma o retry.
func TestCancel_LateAgentResultIgnored(t *testing.T) {
	s := newTestScheduler(t)
	s.hub = &captureBus{}
	today := time.Now().Format("2006-01-02")
	def := domain.JobDefinition{ID: "k2", JobType: "COMMAND", Retries: 2, Schedule: domain.Schedule{Enabled: true}}
	id := seedInst(t, s, "k2-1", today, string(domain.StatusRunning), def)

	if _, err := s.Cancel(id); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	// O agente ainda devolve o resultado da execução derrubada, DEPOIS do kill.
	s.FinishInstance(id, domain.StatusNotOK, 137, "saida parcial do processo derrubado")

	var status, output string
	if err := s.db.QueryRow(
		`SELECT status, COALESCE(output,'') FROM instances WHERE id=?`, id,
	).Scan(&status, &output); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != string(domain.StatusNotOK) || !strings.Contains(output, "killed") {
		t.Fatalf("resultado tardio não deveria sobrescrever o kill nem re-armar retry: status=%s out=%q", status, output)
	}
}

// Cancel de uma ordem que ainda não rodou (WAITING) vira CANCELLED — nada a
// matar. (HELD segue o mesmo caminho.)
func TestCancel_WaitingBecomesCancelled(t *testing.T) {
	s := newTestScheduler(t)
	s.hub = &captureBus{}
	today := time.Now().Format("2006-01-02")
	def := domain.JobDefinition{ID: "w", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	id := seedInst(t, s, "w-1", today, string(domain.StatusWaiting), def)

	st, err := s.Cancel(id)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if st != domain.StatusCancelled {
		t.Fatalf("WAITING cancelado deveria virar CANCELLED, veio %s", st)
	}
	var status string
	_ = s.db.QueryRow(`SELECT status FROM instances WHERE id=?`, id).Scan(&status)
	if status != string(domain.StatusCancelled) {
		t.Fatalf("persistido esperado CANCELLED, veio %s", status)
	}
}

// Cancel de uma instance já terminal é rejeitado (nada a cancelar).
func TestCancel_TerminalRejected(t *testing.T) {
	s := newTestScheduler(t)
	s.hub = &captureBus{}
	today := time.Now().Format("2006-01-02")
	def := domain.JobDefinition{ID: "d", JobType: "COMMAND"}
	id := seedInst(t, s, "d-1", today, string(domain.StatusOK), def)
	if _, err := s.Cancel(id); err == nil {
		t.Fatal("cancel de instance OK (terminal) deveria falhar")
	}
}
