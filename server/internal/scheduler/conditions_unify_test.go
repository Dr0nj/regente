package scheduler

// Bateria do MODELO ÚNICO de dependência: condições num pool global
// (2026-07-17, docs/conditions-events.md). Cenário-guia do usuário:
//
//	A→B ligados pela setinha = A ganha OutAdd A-TO-B; B ganha In + OutRemove
//	A-TO-B. A termina OK → a condição entra no POOL. B roda e, no OK, APAGA a
//	condição (consumo). Set OK em B faz o mesmo (aplica OutAdd e OutRemove) —
//	então Set OK + rerun deixa B esperando de novo (a condição sumiu). Falha
//	NÃO consome. Deletar a condição no painel Condições impede o B de rodar.

import (
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// poolHas — a condição existe no pool naquele escopo?
func poolHas(t *testing.T, s *Scheduler, name, scope string) bool {
	t.Helper()
	return s.conditions.Has(name, scope)
}

func waitStatus(t *testing.T, s *Scheduler, id string, want ...string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var st string
	for time.Now().Before(deadline) {
		s.Tick()
		_, st, _ = carriedState(t, s, id)
		for _, w := range want {
			if st == w {
				return st
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return st
}

// linkedDefs — A→B como a SETINHA cria (já normalizados: upstream expandido em
// condições explícitas + visão derivada).
func linkedDefs() (domain.JobDefinition, domain.JobDefinition, string) {
	defs := domain.NormalizeConditions([]domain.JobDefinition{
		{ID: "A", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}},
		{
			ID: "B", JobType: "COMMAND",
			Schedule: domain.Schedule{Enabled: true},
			Upstream: []domain.Upstream{{From: "A", Condition: domain.CondOnSuccess}},
		},
	})
	return defs[0], defs[1], domain.LinkCondName("A", "B")
}

// A normalização em si: setinha (upstream legado) vira condições explícitas
// nos DOIS lados, idempotente, e upstream é recalculado como visão.
func TestCondUnify_NormalizeExpandsAndDerives(t *testing.T) {
	defA, defB, name := linkedDefs()
	if len(defA.ConditionsOutAdd) != 1 || defA.ConditionsOutAdd[0] != name {
		t.Fatalf("produtor deveria ganhar OutAdd [%s], veio %v", name, defA.ConditionsOutAdd)
	}
	if len(defB.ConditionsIn) != 1 || defB.ConditionsIn[0] != name {
		t.Fatalf("consumidor deveria ganhar In [%s], veio %v", name, defB.ConditionsIn)
	}
	if len(defB.ConditionsOutRemove) != 1 || defB.ConditionsOutRemove[0] != name {
		t.Fatalf("consumidor deveria ganhar OutRemove [%s] (deleção automática da setinha), veio %v", name, defB.ConditionsOutRemove)
	}
	// Idempotência: normalizar de novo (upstream agora é a visão derivada) não duplica.
	again := domain.NormalizeConditions([]domain.JobDefinition{defA, defB})
	if len(again[0].ConditionsOutAdd) != 1 || len(again[1].ConditionsIn) != 1 || len(again[1].ConditionsOutRemove) != 1 {
		t.Fatalf("re-normalizar duplicou condições: %v / %v / %v",
			again[0].ConditionsOutAdd, again[1].ConditionsIn, again[1].ConditionsOutRemove)
	}
	// Visão derivada: B enxerga A como upstream (topologia p/ canvas/WhatIf).
	if len(again[1].Upstream) != 1 || again[1].Upstream[0].From != "A" {
		t.Fatalf("upstream derivado de B deveria ser [A], veio %+v", again[1].Upstream)
	}
	// Condições digitadas À MÃO linkam do mesmo jeito (mesmo lugar = pool).
	manual := domain.NormalizeConditions([]domain.JobDefinition{
		{ID: "P", JobType: "COMMAND", ConditionsOutAdd: []string{"MINHA-COND"}},
		{ID: "C", JobType: "COMMAND", ConditionsIn: []string{"MINHA-COND"}, ConditionsOutRemove: []string{"MINHA-COND"}},
	})
	if len(manual[1].Upstream) != 1 || manual[1].Upstream[0].From != "P" {
		t.Fatalf("condição manual deveria derivar a MESMA topologia da setinha, veio %+v", manual[1].Upstream)
	}
}

// Fluxo feliz: A termina OK → condição no pool → B roda → OK de B CONSOME
// (OutRemove) → pool vazio.
func TestCondUnify_HappyPathConsumesOnOK(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	defA, defB, name := linkedDefs()

	seedInst(t, s, "A-1", today, string(domain.StatusWaiting), defA)
	seedInst(t, s, "B-1", today, string(domain.StatusWaiting), defB)

	// B não roda antes de A (pool vazio).
	s.Tick()
	if _, st, _ := carriedState(t, s, "B-1"); st != string(domain.StatusWaiting) {
		t.Fatalf("B não podia rodar sem a condição no pool, está %s", st)
	}
	// A roda (mock OK) → adiciona a condição → B destrava e consome no OK.
	if st := waitStatus(t, s, "A-1", string(domain.StatusOK)); st != string(domain.StatusOK) {
		t.Fatalf("A deveria rodar a OK, está %s", st)
	}
	if !poolHas(t, s, name, today) {
		t.Fatalf("OK de A deveria ter adicionado %s@%s ao pool", name, today)
	}
	if st := waitStatus(t, s, "B-1", string(domain.StatusOK)); st != string(domain.StatusOK) {
		t.Fatalf("B deveria rodar com a condição no pool, está %s", st)
	}
	if poolHas(t, s, name, today) {
		t.Fatalf("OK de B deveria ter CONSUMIDO (deletado) %s do pool", name)
	}
}

// Rerun do consumidor após OK: a condição foi consumida pelo próprio OK → o
// rerun espera até A criar uma NOVA (rerun do pai destrava).
func TestCondUnify_RerunConsumerAfterOKWaits(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	defA, defB, _ := linkedDefs()

	seedInst(t, s, "A-1", today, string(domain.StatusWaiting), defA)
	seedInst(t, s, "B-1", today, string(domain.StatusWaiting), defB)
	waitStatus(t, s, "A-1", string(domain.StatusOK))
	if st := waitStatus(t, s, "B-1", string(domain.StatusOK)); st != string(domain.StatusOK) {
		t.Fatalf("setup: B deveria terminar OK, está %s", st)
	}

	// Rerun de B (como o handler faz: reset de status; o pool não é tocado).
	_, _ = s.db.Exec(
		`UPDATE instances SET status=?, started_at=NULL, finished_at=NULL, exit_code=NULL, output=NULL WHERE id=?`,
		string(domain.StatusWaiting), "B-1",
	)
	for i := 0; i < 3; i++ {
		s.Tick()
	}
	if _, st, _ := carriedState(t, s, "B-1"); st != string(domain.StatusWaiting) {
		t.Fatalf("rerun após OK deveria esperar condição NOVA (WAITING), está %s", st)
	}
	if ex, err := s.Explain("B-1"); err != nil || hasKind(ex.Blockers, GateCondition) == nil {
		t.Fatalf("Explain deveria apontar WAIT_CONDITION, veio %+v (err=%v)", ex.Blockers, err)
	}

	// Rerun do pai → OK novo → condição de volta ao pool → B roda.
	_, _ = s.db.Exec(
		`UPDATE instances SET status=?, started_at=NULL, finished_at=NULL, exit_code=NULL, output=NULL WHERE id=?`,
		string(domain.StatusWaiting), "A-1",
	)
	waitStatus(t, s, "A-1", string(domain.StatusOK))
	if st := waitStatus(t, s, "B-1", string(domain.StatusRunning), string(domain.StatusOK)); st != string(domain.StatusRunning) && st != string(domain.StatusOK) {
		t.Fatalf("rerun do pai deveria destravar o rerun de B, está %s", st)
	}
}

// Set OK aplica as saídas nos DOIS papéis (regra do usuário): como PRODUTOR
// adiciona (destrava sucessor de pai falho); como CONSUMIDOR remove — Set OK
// + rerun = fica aguardando, porque o próprio Set OK apagou a condição.
func TestCondUnify_SetOKAppliesOutsBothRoles(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	defA, defB, name := linkedDefs()

	// Papel PRODUTOR: A falhou; Set OK em A cria a condição e B roda.
	seedInst(t, s, "A-1", today, string(domain.StatusNotOK), defA)
	seedInst(t, s, "B-1", today, string(domain.StatusWaiting), defB)
	s.Tick()
	if _, st, _ := carriedState(t, s, "B-1"); st != string(domain.StatusWaiting) {
		t.Fatalf("B não podia rodar com A NOTOK (pool vazio), está %s", st)
	}
	if err := s.SetOK("A-1"); err != nil {
		t.Fatalf("SetOK A: %v", err)
	}
	if !poolHas(t, s, name, today) {
		t.Fatalf("Set OK do produtor deveria adicionar %s ao pool", name)
	}
	if st := waitStatus(t, s, "B-1", string(domain.StatusOK)); st != string(domain.StatusOK) {
		t.Fatalf("Set OK do pai deveria destravar B, está %s", st)
	}

	// Papel CONSUMIDOR: nova condição no pool; B-2 esperando; Set OK em B-2
	// CONSOME a condição (OutRemove) → rerun fica esperando.
	_ = s.conditions.Set(name, today, "test")
	seedInst(t, s, "B-2", today, string(domain.StatusWaiting), defB)
	if err := s.SetOK("B-2"); err != nil {
		t.Fatalf("SetOK B-2: %v", err)
	}
	if poolHas(t, s, name, today) {
		t.Fatalf("Set OK do consumidor deveria ter DELETADO %s do pool", name)
	}
	_, _ = s.db.Exec(
		`UPDATE instances SET status=?, started_at=NULL, finished_at=NULL, exit_code=NULL, output=NULL WHERE id=?`,
		string(domain.StatusWaiting), "B-2",
	)
	for i := 0; i < 3; i++ {
		s.Tick()
	}
	if _, st, _ := carriedState(t, s, "B-2"); st != string(domain.StatusWaiting) {
		t.Fatalf("Set OK + rerun deveria AGUARDAR (condição consumida), está %s", st)
	}
}

// Falha NÃO consome: B NOTOK deixa a condição no pool — o rerun (ou um clone)
// roda direto, sem rerun do pai.
func TestCondUnify_NotOKDoesNotConsume(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	defA, defB, name := linkedDefs()

	seedInst(t, s, "A-1", today, string(domain.StatusOK), defA)
	_ = s.conditions.Set(name, today, "test") // A OK "de história": condição no pool
	seedInst(t, s, "B-1", today, string(domain.StatusRunning), defB)
	s.FinishInstance("B-1", domain.StatusNotOK, 1, "boom")

	if !poolHas(t, s, name, today) {
		t.Fatalf("NOTOK não podia consumir %s do pool", name)
	}
	_, _ = s.db.Exec(
		`UPDATE instances SET status=?, started_at=NULL, finished_at=NULL, exit_code=NULL, output=NULL WHERE id=?`,
		string(domain.StatusWaiting), "B-1",
	)
	if st := waitStatus(t, s, "B-1", string(domain.StatusRunning), string(domain.StatusOK)); st != string(domain.StatusRunning) && st != string(domain.StatusOK) {
		t.Fatalf("rerun após NOTOK deveria rodar direto (condição segue no pool), está %s", st)
	}
}

// Cenário do usuário: pai OK com o filho em HOLD → a condição está no pool e
// VISÍVEL no painel; o operador DELETA no painel; libera o filho → não roda.
func TestCondUnify_OperatorDeleteFromPoolBlocksChild(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	defA, defB, name := linkedDefs()

	seedInst(t, s, "A-1", today, string(domain.StatusWaiting), defA)
	seedInst(t, s, "B-1", today, string(domain.StatusHeld), defB)
	_, _ = s.db.Exec(`UPDATE instances SET held_from_status='WAITING' WHERE id='B-1'`)

	waitStatus(t, s, "A-1", string(domain.StatusOK))
	if !poolHas(t, s, name, today) {
		t.Fatalf("condição %s deveria estar no pool (visível no painel)", name)
	}
	// Operador deleta no painel Condições.
	if err := s.conditions.Unset(name, today); err != nil {
		t.Fatalf("unset: %v", err)
	}
	// Libera o filho: NÃO roda — a condição sumiu.
	_, _ = s.db.Exec(`UPDATE instances SET status='WAITING', held_from_status='' WHERE id='B-1'`)
	for i := 0; i < 3; i++ {
		s.Tick()
	}
	if _, st, _ := carriedState(t, s, "B-1"); st != string(domain.StatusWaiting) {
		t.Fatalf("filho liberado após a deleção da condição NÃO podia rodar, está %s", st)
	}
	if ex, err := s.Explain("B-1"); err != nil || hasKind(ex.Blockers, GateCondition) == nil {
		t.Fatalf("Explain deveria apontar WAIT_CONDITION, veio %+v (err=%v)", ex.Blockers, err)
	}
	// Operador adiciona de volta (painel) → roda na hora.
	_ = s.conditions.Set(name, today, "operator")
	if st := waitStatus(t, s, "B-1", string(domain.StatusRunning), string(domain.StatusOK)); st != string(domain.StatusRunning) && st != string(domain.StatusOK) {
		t.Fatalf("condição re-adicionada deveria liberar o filho, está %s", st)
	}
}

// Cópia forçada (Order Force) de um consumidor cuja condição JÁ FOI consumida
// entra em WAIT COND; se a condição ainda existe, a cópia roda (pool puro).
func TestCondUnify_ForcedCopyFollowsPool(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	defA, defB, name := linkedDefs()

	// Dia já aconteceu: A OK, B OK (condição consumida — pool vazio).
	seedInst(t, s, "A-1", today, string(domain.StatusOK), defA)
	seedInst(t, s, "B-1", today, string(domain.StatusOK), defB)
	s.defs = []domain.JobDefinition{defA, defB} // ForceOrder lê de s.defs

	copyID, err := s.ForceOrder("B")
	if err != nil {
		t.Fatalf("ForceOrder: %v", err)
	}
	for i := 0; i < 3; i++ {
		s.Tick()
	}
	if _, st, _ := carriedState(t, s, copyID); st != string(domain.StatusWaiting) {
		t.Fatalf("cópia forçada com condição consumida deveria esperar, está %s", st)
	}
	// Rerun do pai → condição volta → a cópia roda.
	_, _ = s.db.Exec(
		`UPDATE instances SET status=?, started_at=NULL, finished_at=NULL, exit_code=NULL, output=NULL WHERE id=?`,
		string(domain.StatusWaiting), "A-1",
	)
	waitStatus(t, s, "A-1", string(domain.StatusOK))
	if !poolHas(t, s, name, today) {
		t.Fatalf("rerun do pai deveria repor %s no pool", name)
	}
	if st := waitStatus(t, s, copyID, string(domain.StatusRunning), string(domain.StatusOK)); st != string(domain.StatusRunning) && st != string(domain.StatusOK) {
		t.Fatalf("cópia deveria rodar com a condição reposta, está %s", st)
	}
}

// Aresta legada on-failure vira ação On/Do set-condition no produtor: o NOTOK
// do pai cria a condição e destrava o filho on-failure; on-success segue preso.
func TestCondUnify_OnFailureEdgeBecomesAction(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	defs := domain.NormalizeConditions([]domain.JobDefinition{
		{ID: "A", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}},
		{ID: "B", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true},
			Upstream: []domain.Upstream{{From: "A", Condition: domain.CondOnSuccess}}},
		{ID: "C", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true},
			Upstream: []domain.Upstream{{From: "A", Condition: domain.CondOnFailure}}},
	})
	defA, defB, defC := defs[0], defs[1], defs[2]
	if len(defA.Actions) != 1 || defA.Actions[0].Do != "set-condition" {
		t.Fatalf("on-failure deveria virar ação set-condition no produtor, veio %+v", defA.Actions)
	}

	seedInst(t, s, "A-1", today, string(domain.StatusRunning), defA)
	seedInst(t, s, "B-1", today, string(domain.StatusWaiting), defB)
	seedInst(t, s, "C-1", today, string(domain.StatusWaiting), defC)

	s.FinishInstance("A-1", domain.StatusNotOK, 1, "boom")
	if st := waitStatus(t, s, "C-1", string(domain.StatusRunning), string(domain.StatusOK)); st != string(domain.StatusRunning) && st != string(domain.StatusOK) {
		t.Fatalf("NOTOK do pai deveria destravar o filho on-failure, C está %s", st)
	}
	if _, st, _ := carriedState(t, s, "B-1"); st != string(domain.StatusWaiting) {
		t.Fatalf("B (on-success) deveria seguir WAITING com A NOTOK, está %s", st)
	}
	// Set OK em A → OutAdd A-TO-B → B destrava.
	if err := s.SetOK("A-1"); err != nil {
		t.Fatalf("SetOK: %v", err)
	}
	if st := waitStatus(t, s, "B-1", string(domain.StatusRunning), string(domain.StatusOK)); st != string(domain.StatusRunning) && st != string(domain.StatusOK) {
		t.Fatalf("Set OK do pai deveria destravar B, está %s", st)
	}
}

// Backfill da migração: instance WAITING pré-unificação (snapshot com upstream,
// sem condições) + pai já OK ⇒ MigrateConditionsUnify semeia a condição; se um
// consumidor da mesma origem JÁ terminou OK, não semeia (consumo do modelo velho).
func TestCondUnify_MigrationBackfill(t *testing.T) {
	s := newTestScheduler(t)
	today := time.Now().Format("2006-01-02")
	// defs CRUAS (pré-unificação): B tem upstream, sem condições explícitas.
	defA := domain.JobDefinition{ID: "A", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	defB := domain.JobDefinition{ID: "B", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true},
		Upstream: []domain.Upstream{{From: "A", Condition: domain.CondOnSuccess}}}
	name := domain.LinkCondName("A", "B")

	seedInst(t, s, "A-1", today, string(domain.StatusOK), defA)
	seedInst(t, s, "B-1", today, string(domain.StatusWaiting), defB)
	s.MigrateConditionsUnify()
	if !poolHas(t, s, name, today) {
		t.Fatalf("backfill deveria semear %s@%s (pai OK, filho esperando)", name, today)
	}
	// O flag impede rodar de novo; e com consumidor OK não semearia.
	_ = s.conditions.Unset(name, today)
	s.MigrateConditionsUnify()
	if poolHas(t, s, name, today) {
		t.Fatal("backfill deveria rodar exatamente UMA vez (meta_flags)")
	}
	// O gate da instance legada usa a condição sintetizada (ExpandSnapshotConditions).
	s.defs = domain.NormalizeConditions([]domain.JobDefinition{defA, defB})
	_ = s.conditions.Set(name, today, "test")
	if st := waitStatus(t, s, "B-1", string(domain.StatusRunning), string(domain.StatusOK)); st != string(domain.StatusRunning) && st != string(domain.StatusOK) {
		t.Fatalf("instance legada deveria rodar com a condição sintetizada no pool, está %s", st)
	}
}
