package scheduler

// Bateria da semântica de dependência por EVENTO com consumo por instância
// (schemaV15, 2026-07-13). Cenário-guia do usuário:
//
//	A rodou OK e satisfez B (1 linha verde). Forço uma CÓPIA de B → ela NÃO
//	pode ser satisfeita pelo término que a B original já consumiu (entra em
//	WAIT EVENT). Rerun de A NÃO apaga a linha verde da B original (o latch é
//	da instance); o novo término de A é que destrava a cópia. Cancel idem.
//	Rerun da PRÓPRIA B reseta as linhas dela (e ela pode re-clamar).

import (
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

func depClaimCount(t *testing.T, s *Scheduler, consumerID string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM dep_claims WHERE consumer_instance_id=?`, consumerID).Scan(&n); err != nil {
		t.Fatalf("count claims %s: %v", consumerID, err)
	}
	return n
}

func waitStatus(t *testing.T, s *Scheduler, id string, want ...string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var st string
	for time.Now().Before(deadline) {
		s.Tick()
		_, st, _, _ = carriedState(t, s, id)
		for _, w := range want {
			if st == w {
				return st
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return st
}

func depDefs() (domain.JobDefinition, domain.JobDefinition) {
	defA := domain.JobDefinition{ID: "A", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	defB := domain.JobDefinition{
		ID: "B", JobType: "COMMAND",
		Schedule: domain.Schedule{Enabled: true},
		Upstream: []domain.Upstream{{From: "A", Condition: domain.CondOnSuccess}},
	}
	return defA, defB
}

// O cenário completo do usuário, na ordem em que ele descreveu.
func TestDepEvents_ForcedCopyWaitsFreshEvent(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	defA, defB := depDefs()

	// Dia já aconteceu: A OK, B OK (1 linha verde consumida).
	seedInst(t, s, "A-1", today, string(domain.StatusOK), defA)
	seedInst(t, s, "B-1", today, string(domain.StatusOK), defB)
	s.defs = []domain.JobDefinition{defA, defB} // ForceOrder lê de s.defs

	// Force de uma CÓPIA de B (Order Force do Design).
	copyID, err := s.ForceOrder("B")
	if err != nil {
		t.Fatalf("ForceOrder: %v", err)
	}

	// O término antigo de A foi consumido (backfill dá o claim à B-1, que já
	// rodou) → a cópia NÃO herda a condição: fica WAITING em WAIT EVENT.
	for i := 0; i < 3; i++ {
		s.Tick()
	}
	if _, st, _, _ := carriedState(t, s, copyID); st != string(domain.StatusWaiting) {
		t.Fatalf("cópia forçada deveria esperar um evento NOVO, está %s", st)
	}
	if ex, err := s.Explain(copyID); err != nil || hasKind(ex.Blockers, GateDep) == nil {
		t.Fatalf("Explain da cópia deveria apontar WAIT_DEP (evento consumido), veio %+v (err=%v)", ex.Blockers, err)
	}
	if n := depClaimCount(t, s, "B-1"); n != 1 {
		t.Fatalf("B-1 (histórica) deveria deter o claim do evento antigo, tem %d", n)
	}

	// Rerun do antecessor A (o operador quer justamente passar exec pra cópia).
	if _, err := s.db.Exec(
		`UPDATE instances SET status=?, started_at=NULL, finished_at=NULL, exit_code=NULL, output=NULL WHERE id=?`,
		string(domain.StatusWaiting), "A-1",
	); err != nil {
		t.Fatalf("rerun A: %v", err)
	}
	s.ResetDepClaims("A-1") // o handler de rerun faz isso (A não tem upstream: no-op)

	// A linha da B ORIGINAL permanece intacta (latch é da instance, não do pai).
	if n := depClaimCount(t, s, "B-1"); n != 1 {
		t.Fatalf("rerun de A não pode apagar o claim da B original, tem %d", n)
	}

	// A re-roda (mock OK) → evento NOVO → a cópia clama e dispara.
	if st := waitStatus(t, s, "A-1", string(domain.StatusOK)); st != string(domain.StatusOK) {
		t.Fatalf("A deveria re-rodar a OK, está %s", st)
	}
	if st := waitStatus(t, s, copyID, string(domain.StatusRunning), string(domain.StatusOK)); st != string(domain.StatusRunning) && st != string(domain.StatusOK) {
		t.Fatalf("novo término de A deveria destravar a cópia, está %s", st)
	}
	if n := depClaimCount(t, s, copyID); n != 1 {
		t.Fatalf("cópia deveria ter clamado o evento novo, tem %d claims", n)
	}
}

// Rerun e CANCEL do pai não tocam a linha já satisfeita do filho (latch).
func TestDepEvents_RerunAndCancelUpstreamKeepLatch(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	defA, defB := depDefs()

	seedInst(t, s, "A-1", today, string(domain.StatusOK), defA)
	seedInst(t, s, "B-1", today, string(domain.StatusWaiting), defB)

	// Tick: B clama o evento de A (backfill lazy) e dispara.
	if st := waitStatus(t, s, "B-1", string(domain.StatusOK)); st != string(domain.StatusOK) {
		t.Fatalf("B deveria rodar a OK com A OK, está %s", st)
	}
	if n := depClaimCount(t, s, "B-1"); n != 1 {
		t.Fatalf("B deveria ter latch da aresta, tem %d", n)
	}

	// Rerun do pai → linha de B segue satisfeita; B segue OK.
	_, _ = s.db.Exec(`UPDATE instances SET status=? WHERE id=?`, string(domain.StatusWaiting), "A-1")
	s.Tick()
	if n := depClaimCount(t, s, "B-1"); n != 1 {
		t.Fatalf("rerun do pai apagou o latch de B (não podia), claims=%d", n)
	}

	// Cancel do pai (rerun cancelado no meio) → idem: latch intacto.
	_, _ = s.db.Exec(`UPDATE instances SET status=?, finished_at=CURRENT_TIMESTAMP WHERE id=?`, string(domain.StatusCancelled), "A-1")
	s.Tick()
	if n := depClaimCount(t, s, "B-1"); n != 1 {
		t.Fatalf("cancel do pai apagou o latch de B (não podia), claims=%d", n)
	}
	if _, st, _, _ := carriedState(t, s, "B-1"); st != string(domain.StatusOK) {
		t.Fatalf("B deveria permanecer OK, está %s", st)
	}
}

// Um término satisfaz NO MÁXIMO uma instance da mesma definition: com duas
// cópias esperando, só a de maior prioridade (daily antes de forçada) clama.
func TestDepEvents_EventNotDoubleConsumed(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	defA, defB := depDefs()

	seedInst(t, s, "A-1", today, string(domain.StatusOK), defA)
	seedInst(t, s, "B-1", today, string(domain.StatusWaiting), defB) // daily
	// cópia forçada (Order Force) esperando junto
	seedInst(t, s, "B-2", today, string(domain.StatusWaiting), defB)
	_, _ = s.db.Exec(`UPDATE instances SET forced=1, force_mode=? WHERE id=?`, ForceModeOrder, "B-2")

	// 1 evento (A), 2 famintos: a daily B-1 leva; B-2 fica em WAIT EVENT.
	if st := waitStatus(t, s, "B-1", string(domain.StatusRunning), string(domain.StatusOK)); st != string(domain.StatusRunning) && st != string(domain.StatusOK) {
		t.Fatalf("B-1 (daily) deveria clamar o evento e rodar, está %s", st)
	}
	for i := 0; i < 3; i++ {
		s.Tick()
	}
	if _, st, _, _ := carriedState(t, s, "B-2"); st != string(domain.StatusWaiting) {
		t.Fatalf("B-2 não pode consumir o MESMO evento que B-1: deveria estar WAITING, está %s", st)
	}
	if n := depClaimCount(t, s, "B-2"); n != 0 {
		t.Fatalf("B-2 não deveria ter claim, tem %d", n)
	}
}

// Rerun do PRÓPRIO consumidor reseta as linhas dele — os eventos clamados voltam
// pro pool e o job re-clama no próximo tick (paridade com conditions persistentes).
func TestDepEvents_RerunConsumerResetsAndReclaims(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	defA, defB := depDefs()

	seedInst(t, s, "A-1", today, string(domain.StatusOK), defA)
	seedInst(t, s, "B-1", today, string(domain.StatusWaiting), defB)
	if st := waitStatus(t, s, "B-1", string(domain.StatusOK)); st != string(domain.StatusOK) {
		t.Fatalf("B deveria rodar a OK, está %s", st)
	}

	// Rerun de B (o handler: reset + ResetDepClaims).
	_, _ = s.db.Exec(
		`UPDATE instances SET status=?, started_at=NULL, finished_at=NULL, exit_code=NULL, output=NULL WHERE id=?`,
		string(domain.StatusWaiting), "B-1",
	)
	s.ResetDepClaims("B-1")
	if n := depClaimCount(t, s, "B-1"); n != 0 {
		t.Fatalf("rerun de B deveria resetar os claims DELE, tem %d", n)
	}

	// O evento de A ficou livre de novo → B re-clama e re-roda.
	if st := waitStatus(t, s, "B-1", string(domain.StatusRunning), string(domain.StatusOK)); st != string(domain.StatusRunning) && st != string(domain.StatusOK) {
		t.Fatalf("B deveria re-clamar o evento liberado e rodar, está %s", st)
	}
	if n := depClaimCount(t, s, "B-1"); n != 1 {
		t.Fatalf("B deveria ter re-clamado 1 evento, tem %d", n)
	}
}

// Falha NÃO consome (regra do usuário): o término NOTOK terminal DEVOLVE o
// evento pro pool — um clone pendente pode usá-lo sem rerun do pai. E o
// backfill de consumo histórico não pode entregar o evento de volta ao falho.
func TestDepEvents_NotOKReleasesEventBackToPool(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	defA, defB := depDefs()

	seedInst(t, s, "A-1", today, string(domain.StatusOK), defA)
	// B-1 partiu consumindo o evento de A (como o pré-passe do tick faria).
	seedInst(t, s, "B-1", today, string(domain.StatusRunning), defB)
	if !s.tryClaimEdge("B-1", "B", "A", today, domain.CondOnSuccess) {
		t.Fatal("B-1 deveria clamar o evento de A")
	}

	// B-1 falha TERMINAL (Retries=0) → o evento volta pro pool.
	s.FinishInstance("B-1", domain.StatusNotOK, 1, "boom")
	if n := depClaimCount(t, s, "B-1"); n != 0 {
		t.Fatalf("NOTOK terminal deveria devolver o evento (claims=0), tem %d", n)
	}

	// Clone forçado pendente consome o evento devolvido e roda — SEM rerun de A.
	seedInst(t, s, "B-2", today, string(domain.StatusWaiting), defB)
	_, _ = s.db.Exec(`UPDATE instances SET forced=1, force_mode=? WHERE id=?`, ForceModeOrder, "B-2")
	if st := waitStatus(t, s, "B-2", string(domain.StatusRunning), string(domain.StatusOK)); st != string(domain.StatusRunning) && st != string(domain.StatusOK) {
		t.Fatalf("clone deveria clamar o evento devolvido pela falha e rodar, está %s", st)
	}
	if n := depClaimCount(t, s, "B-2"); n != 1 {
		t.Fatalf("clone deveria deter o claim do evento devolvido, tem %d", n)
	}
	// Backfill histórico não devolve nada ao B-1 falho (NOTOK não consome).
	if n := depClaimCount(t, s, "B-1"); n != 0 {
		t.Fatalf("B-1 (NOTOK) não pode reconsumir via backfill: claims deveriam ser 0, tem %d", n)
	}
}

// Set OK do pai emite evento consumível (destrava on-success a partir de pai falho)
// — e o NOTOK terminal do pai emite evento pra arestas on-failure.
func TestDepEvents_SetOKAndOnFailure(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	defA, defB := depDefs()
	defC := domain.JobDefinition{
		ID: "C", JobType: "COMMAND",
		Schedule: domain.Schedule{Enabled: true},
		Upstream: []domain.Upstream{{From: "A", Condition: domain.CondOnFailure}},
	}

	seedInst(t, s, "A-1", today, string(domain.StatusNotOK), defA)
	seedInst(t, s, "B-1", today, string(domain.StatusWaiting), defB) // on-success: preso
	seedInst(t, s, "C-1", today, string(domain.StatusWaiting), defC) // on-failure: destrava

	if st := waitStatus(t, s, "C-1", string(domain.StatusRunning), string(domain.StatusOK)); st != string(domain.StatusRunning) && st != string(domain.StatusOK) {
		t.Fatalf("NOTOK do pai deveria destravar a aresta on-failure, C está %s", st)
	}
	if _, st, _, _ := carriedState(t, s, "B-1"); st != string(domain.StatusWaiting) {
		t.Fatalf("B (on-success) deveria seguir WAITING com A NOTOK, está %s", st)
	}

	// Set OK em A → evento OK novo → B destrava.
	if err := s.SetOK("A-1"); err != nil {
		t.Fatalf("SetOK: %v", err)
	}
	if st := waitStatus(t, s, "B-1", string(domain.StatusRunning), string(domain.StatusOK)); st != string(domain.StatusRunning) && st != string(domain.StatusOK) {
		t.Fatalf("Set OK do pai deveria destravar B, está %s", st)
	}
}
