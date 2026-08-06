package scheduler

// ST-1 — a EXECUÇÃO como registro próprio (instance_runs). Ver runs.go.
//
// O caso que motivou a trilha (auditoria de 2026-08-05, report do usuário: job
// de 2 min exatos com avg de 10 min e max de 98 min na aba Statistics) está em
// TestRuns_SetOKAfterFailedAttemptDoesNotInflateStats.

import (
	"database/sql"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// claimRunning reproduz o CLAIM do startInstance (WAITING→RUNNING + started_at +
// abertura da execução) sem passar pelo dispatch, que é assíncrono e depende de
// agente. A fidelidade do claim real está coberta ponta-a-ponta em
// TestRuns_StartInstanceOpensAndClosesRun.
func claimRunning(t *testing.T, s *Scheduler, id string) {
	t.Helper()
	if _, err := s.db.Exec(
		`UPDATE instances SET status=?, started_at=CURRENT_TIMESTAMP WHERE id=? AND status=?`,
		string(domain.StatusRunning), id, string(domain.StatusWaiting),
	); err != nil {
		t.Fatalf("claim %s: %v", id, err)
	}
	s.recordRunStart(id)
}

// runRows devolve as execuções gravadas de uma instance (velha → nova).
func runRows(t *testing.T, s *Scheduler, instanceID string) []RunSample {
	t.Helper()
	rows, err := s.db.Query(
		`SELECT instance_id, order_date, attempt, status, started_at, finished_at
		   FROM instance_runs WHERE instance_id=? ORDER BY id`, instanceID)
	if err != nil {
		t.Fatalf("read runs: %v", err)
	}
	defer rows.Close()
	var out []RunSample
	for rows.Next() {
		var r RunSample
		// finished_at é NULO enquanto a execução está em curso (COALESCE não
		// serve: a expressão perde a afinidade DATETIME e volta como string).
		var fin sql.NullTime
		if err := rows.Scan(&r.InstanceID, &r.OrderDate, &r.Attempt, &r.Status, &r.StartedAt, &fin); err != nil {
			t.Fatalf("scan run: %v", err)
		}
		r.FinishedAt = fin.Time
		out = append(out, r)
	}
	return out
}

// openRuns — execuções ainda em curso (sem fim gravado).
func openRuns(t *testing.T, s *Scheduler, instanceID string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM instance_runs WHERE instance_id=? AND finished_at IS NULL`, instanceID,
	).Scan(&n); err != nil {
		t.Fatalf("count open runs: %v", err)
	}
	return n
}

// REGRA (a do operador): a execução começa quando o job ENTRA EM RUNNING e
// termina no resultado da tentativa. startInstance abre a linha, FinishInstance
// fecha — ponta-a-ponta, com o dispatch real (demo-mode finaliza OK em 1s).
func TestRuns_StartInstanceOpensAndClosesRun(t *testing.T) {
	s := newTestScheduler(t) // DemoMode = true
	today := time.Now().Format("2006-01-02")
	def := domain.JobDefinition{ID: "e2e", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	seedInst(t, s, "e2e-1", today, string(domain.StatusWaiting), def)

	s.startInstance("e2e-1", def)
	// O mock do demo-mode finaliza em 1s; espera o fim gravado (teto generoso).
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) && openRuns(t, s, "e2e-1") > 0 {
		time.Sleep(50 * time.Millisecond)
	}

	runs := runRows(t, s, "e2e-1")
	if len(runs) != 1 {
		t.Fatalf("uma execução esperada, veio %d", len(runs))
	}
	if runs[0].Status != string(domain.StatusOK) {
		t.Fatalf("execução deveria fechar OK, veio %s", runs[0].Status)
	}
	if runs[0].Attempt != 1 || runs[0].OrderDate != today {
		t.Fatalf("attempt/ODAT errados na execução: %+v", runs[0])
	}
	if st := s.JobStats("e2e"); st.Runs != 1 || st.OK != 1 || len(st.Recent) != 1 {
		t.Fatalf("Statistics deveria enxergar a execução: %+v", st)
	}
}

// REGRA: cada TENTATIVA é uma execução. O retry re-arma a MESMA instance (a
// linha de instances só guarda a última tentativa) — a lista de últimas runs
// mostra as duas, como o Control-M.
func TestRuns_OneRowPerAttempt(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	// retryDelayMin>0 → o retry é AGENDADO (sem goroutine): re-arma WAITING na hora.
	def := domain.JobDefinition{
		ID: "rt", JobType: "COMMAND", Retries: 1, RetryDelayMin: 60,
		Schedule: domain.Schedule{Enabled: true},
	}
	seedInst(t, s, "rt-1", today, string(domain.StatusWaiting), def)

	claimRunning(t, s, "rt-1")
	s.FinishInstance("rt-1", domain.StatusNotOK, 1, "falhou") // → retry agendado
	claimRunning(t, s, "rt-1")
	s.FinishInstance("rt-1", domain.StatusOK, 0, "passou")

	runs := runRows(t, s, "rt-1")
	if len(runs) != 2 {
		t.Fatalf("duas tentativas = duas execuções, veio %d: %+v", len(runs), runs)
	}
	if runs[0].Status != "NOTOK" || runs[1].Status != "OK" {
		t.Fatalf("ordem/status das execuções errados: %+v", runs)
	}
	if runs[0].Attempt != 1 || runs[1].Attempt != 2 {
		t.Fatalf("attempt não acompanhou o retry: %+v", runs)
	}
	if n := openRuns(t, s, "rt-1"); n != 0 {
		t.Fatalf("%d execução(ões) ficaram abertas", n)
	}
	st := s.JobStats("rt")
	if st.Runs != 2 || st.OK != 1 || st.NotOK != 1 {
		t.Fatalf("Statistics deveria contar as duas tentativas: %+v", st)
	}
}

// REGRA — a auditoria que originou a trilha: Set OK num job que NÃO está rodando
// não é execução. Antes, o Set OK carimbava finished_at=AGORA sobre o started_at
// da tentativa anterior (o retry agendado zera o finished_at) e a Statistics lia
// aquele intervalo como "duração desta run" — daí um job de 2 min exibir máximo
// de 98 min. A duração medida agora é a da EXECUÇÃO gravada.
func TestRuns_SetOKAfterFailedAttemptDoesNotInflateStats(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	def := domain.JobDefinition{
		ID: "inf", JobType: "COMMAND", Retries: 1, RetryDelayMin: 60,
		Schedule: domain.Schedule{Enabled: true},
	}
	seedInst(t, s, "inf-1", today, string(domain.StatusWaiting), def)

	// Tentativa curta que falhou; o retry ficou agendado (instance volta a WAITING
	// com started_at preservado e finished_at NULO).
	claimRunning(t, s, "inf-1")
	s.FinishInstance("inf-1", domain.StatusNotOK, 1, "falhou")

	// …e isso tudo aconteceu 98 minutos atrás.
	ago := time.Now().Add(-98 * time.Minute)
	if _, err := s.db.Exec(`UPDATE instances SET started_at=? WHERE id='inf-1'`, ago); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(
		`UPDATE instance_runs SET started_at=?, finished_at=? WHERE instance_id='inf-1'`,
		ago, ago.Add(2*time.Minute),
	); err != nil {
		t.Fatal(err)
	}

	// O operador desiste do retry e conclui na mão.
	if err := s.SetOK("inf-1"); err != nil {
		t.Fatalf("set ok: %v", err)
	}

	// A linha da instance AGORA mente: finished_at(agora) − started_at(98min) —
	// é exatamente o número que a Statistics exibia.
	var started, finished time.Time
	if err := s.db.QueryRow(`SELECT started_at, finished_at FROM instances WHERE id='inf-1'`).Scan(&started, &finished); err != nil {
		t.Fatal(err)
	}
	if lie := finished.Sub(started); lie < 90*time.Minute {
		t.Fatalf("cenário não reproduziu a inflação (delta da instance = %s)", lie)
	}

	st := s.JobStats("inf")
	if st.Runs != 1 || st.NotOK != 1 || st.OK != 0 {
		t.Fatalf("Set OK não é execução — esperado 1 run NOTOK, veio %+v", st)
	}
	if st.MaxMs != 0 || st.AvgMs != 0 {
		t.Fatalf("sem execução OK, a distribuição de duração tem que zerar: %+v", st)
	}
	if len(st.Recent) != 1 || st.Recent[0].DurationMs != 2*60_000 {
		t.Fatalf("a duração exibida tem que ser a da execução (2min), veio %+v", st.Recent)
	}
}

// REGRA: instance que nunca entrou em RUNNING não é run nenhuma — nem na
// contagem, nem na taxa de sucesso. (Set OK num WAITING preso é rotina.)
func TestRuns_NeverRanIsNotARun(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	def := domain.JobDefinition{ID: "nr", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	seedInst(t, s, "nr-1", today, string(domain.StatusWaiting), def)

	if err := s.SetOK("nr-1"); err != nil {
		t.Fatalf("set ok: %v", err)
	}
	if st := s.JobStats("nr"); st.Runs != 0 || st.OK != 0 || st.SuccessRate != 0 {
		t.Fatalf("WAITING concluído na mão não pode virar run: %+v", st)
	}
	if n := len(runRows(t, s, "nr-1")); n != 0 {
		t.Fatalf("nenhuma execução deveria existir, veio %d", n)
	}
}

// REGRA: claim revertido (dispatch sem agente) não deixa execução aberta —
// senão sobraria uma run "em curso" eterna para um job que nunca rodou.
func TestRuns_DiscardRevertedClaim(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	def := domain.JobDefinition{ID: "rv", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	seedInst(t, s, "rv-1", today, string(domain.StatusWaiting), def)

	claimRunning(t, s, "rv-1")
	if openRuns(t, s, "rv-1") != 1 {
		t.Fatal("o claim deveria ter aberto uma execução")
	}
	s.discardRunStart("rv-1")
	if n := len(runRows(t, s, "rv-1")); n != 0 {
		t.Fatalf("execução revertida deveria sumir, sobraram %d", n)
	}
}

// REGRA: o cancel de um RUNNING (kill) encerra a execução como NOTOK — o tempo
// até o kill é tempo de execução de verdade.
func TestRuns_KillClosesRun(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	def := domain.JobDefinition{ID: "kl", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	seedInst(t, s, "kl-1", today, string(domain.StatusWaiting), def)

	claimRunning(t, s, "kl-1")
	if _, err := s.Cancel("kl-1"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	runs := runRows(t, s, "kl-1")
	if len(runs) != 1 || runs[0].Status != "NOTOK" {
		t.Fatalf("kill deveria fechar a execução como NOTOK: %+v", runs)
	}
	if n := openRuns(t, s, "kl-1"); n != 0 {
		t.Fatalf("%d execução(ões) ficaram abertas após o kill", n)
	}
}
