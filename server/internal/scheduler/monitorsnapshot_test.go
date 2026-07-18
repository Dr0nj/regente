package scheduler

// M1 — imutabilidade TOTAL do Monitoring (2026-07-18). Uma instance já ordenada
// congela TUDO que o card/lista/grafo exibem (label, tipo, gates, condições);
// editar/publicar no Design não a reescreve. A mudança só entra via Force (ordem
// nova, as duas convivem) ou na daily seguinte. Ver docs/monitoring-immutability-plan.md.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// scanStr — lê uma coluna string de uma instance (helper dos testes M1).
func scanStr(t *testing.T, s *Scheduler, col, id string) string {
	t.Helper()
	var v string
	if err := s.db.QueryRow(`SELECT COALESCE(`+col+`,'') FROM instances WHERE id=?`, id).Scan(&v); err != nil {
		t.Fatalf("query %s de %s: %v", col, id, err)
	}
	return v
}

// (a) renomear label + trocar jobType APÓS ordenar NÃO muda o card já ordenado.
func TestM1_FrozenLabelAndTypeSurviveDesignEdit(t *testing.T) {
	s := newTestScheduler(t)
	date := "2026-07-18"
	s.mu.Lock()
	s.defs = []domain.JobDefinition{{ID: "job0002", Label: "job0002", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}}
	s.mu.Unlock()
	if n := s.RunDaily(date); n != 1 {
		t.Fatalf("esperava 1 instance materializada, veio %d", n)
	}

	// Design: renomeia + troca tipo + publica (a def viva muda).
	s.mu.Lock()
	s.defs = []domain.JobDefinition{{ID: "job0002", Label: "job0002-b", JobType: "SSH", Schedule: domain.Schedule{Enabled: true}}}
	s.mu.Unlock()

	// O card já ordenado reflete o CONGELADO na ordem, não a def viva.
	id := "job0002-" + date
	if label := scanStr(t, s, "label", id); label != "job0002" {
		t.Fatalf("label deveria ficar CONGELADO (job0002), veio %q", label)
	}
	if jt := scanStr(t, s, "job_type", id); jt != "COMMAND" {
		t.Fatalf("job_type deveria ficar CONGELADO (COMMAND), veio %q", jt)
	}
}

// (b) Force depois do rename cria ordem NOVA com a def atual; as duas convivem.
func TestM1_ForceAfterRenameCoexists(t *testing.T) {
	s := newTestScheduler(t)
	date := time.Now().Format("2006-01-02") // ForceOrder usa "hoje"
	s.mu.Lock()
	s.defs = []domain.JobDefinition{{ID: "job0002", Label: "job0002", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}}
	s.mu.Unlock()
	s.RunDaily(date)

	// rename + republish. Confirm:true segura a forçada em WAIT CONFIRM — o
	// ForceOrder não dispara o dispatch async (evita a goroutine mock-finish
	// escrevendo no DB após o Close do teste, flake "directory not empty").
	s.mu.Lock()
	s.defs = []domain.JobDefinition{{ID: "job0002", Label: "job0002-b", JobType: "COMMAND", Confirm: true, Schedule: domain.Schedule{Enabled: true}}}
	s.mu.Unlock()

	fid, err := s.ForceOrder("job0002")
	if err != nil {
		t.Fatalf("force: %v", err)
	}

	// A daily fica job0002; a forçada nasce job0002-b (def atual). O label é
	// gravado no INSERT síncrono do ForceOrder, antes do dispatch async.
	if lbl := scanStr(t, s, "label", "job0002-"+date); lbl != "job0002" {
		t.Fatalf("daily deveria ficar job0002, veio %q", lbl)
	}
	if lbl := scanStr(t, s, "label", fid); lbl != "job0002-b" {
		t.Fatalf("forçada deveria pegar a def atual job0002-b, veio %q", lbl)
	}
	var cnt int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM instances WHERE definition_id='job0002'`).Scan(&cnt); err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 2 {
		t.Fatalf("esperava 2 instances convivendo (job0002 + job0002-b), veio %d", cnt)
	}
}

// (c) consumidor NOVO criado após a ordem: o OK do pai JÁ ORDENADO não produz a
// condição nova (applyConditionsOut é snapshot-only — E2).
func TestM1_NewConsumerNoRetroactiveCondition(t *testing.T) {
	s := newTestScheduler(t)
	s.AttachConditions(NewConditionEngine(s.db))
	date := "2026-07-18"

	// Só o PAI existe quando a daily roda → snapshot do pai SEM PAI-TO-FILHO.
	parent := domain.JobDefinition{ID: "pai", Label: "pai", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	s.mu.Lock()
	s.defs = []domain.JobDefinition{parent}
	s.mu.Unlock()
	s.RunDaily(date)

	// DEPOIS da ordem: cria o consumidor no Design e liga (a def viva do pai ganha
	// o OutAdd, mas a instance do pai já ordenada tem o snapshot ANTIGO).
	link := domain.LinkCondName("pai", "filho") // PAI-TO-FILHO
	parentLinked := parent
	parentLinked.ConditionsOutAdd = []string{link}
	child := domain.JobDefinition{
		ID: "filho", Label: "filho", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true},
		ConditionsIn: []string{link}, ConditionsOutRemove: []string{link},
	}
	s.mu.Lock()
	s.defs = []domain.JobDefinition{parentLinked, child}
	s.mu.Unlock()

	// O pai de HOJE termina OK. Snapshot-only → NÃO cria a condição nova.
	s.applyConditionsOut("pai-"+date, "test")
	if s.conditions.Has(link, date) {
		t.Fatalf("OK do pai já ordenado NÃO deveria criar %s (snapshot-only, E2)", link)
	}
}

// (d) a daily do dia SEGUINTE pega a def nova (a imutabilidade é por-ordem).
func TestM1_NextDailyPicksNewDef(t *testing.T) {
	s := newTestScheduler(t)
	s.mu.Lock()
	s.defs = []domain.JobDefinition{{ID: "j", Label: "old", JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}}
	s.mu.Unlock()
	s.RunDaily("2026-07-18")

	s.mu.Lock()
	s.defs = []domain.JobDefinition{{ID: "j", Label: "new", JobType: "SSH", Schedule: domain.Schedule{Enabled: true}}}
	s.mu.Unlock()
	s.RunDaily("2026-07-19")

	if lbl := scanStr(t, s, "label", "j-2026-07-18"); lbl != "old" {
		t.Fatalf("dia 18 deveria ficar 'old' (congelado), veio %q", lbl)
	}
	if lbl := scanStr(t, s, "label", "j-2026-07-19"); lbl != "new" {
		t.Fatalf("dia 19 deveria pegar a def nova 'new', veio %q", lbl)
	}
}

// Backfill one-time (MigrateMonitoringSnapshot): instance legada com snapshot mas
// colunas M1 vazias → o boot congela label/tipo/confirm/conds a partir do snapshot.
func TestM1_BackfillFillsFrozenColumns(t *testing.T) {
	s := newTestScheduler(t)
	s.AttachConditions(NewConditionEngine(s.db))
	date := "2026-07-18"

	def := domain.JobDefinition{
		ID: "leg", Label: "Legacy Job", JobType: "SSH", Confirm: true,
		ConditionsIn: []string{"X"}, ConditionsOutAdd: []string{"Y"},
	}
	snap, _ := json.Marshal(def)
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, definition_snapshot) VALUES(?,?,?,?,?,?)`,
		"leg-"+date, "leg", date, "WAITING", time.Now(), string(snap),
	); err != nil {
		t.Fatalf("seed: %v", err)
	}

	s.MigrateMonitoringSnapshot()

	id := "leg-" + date
	if lbl := scanStr(t, s, "label", id); lbl != "Legacy Job" {
		t.Fatalf("backfill deveria congelar label, veio %q", lbl)
	}
	if jt := scanStr(t, s, "job_type", id); jt != "SSH" {
		t.Fatalf("backfill deveria congelar job_type, veio %q", jt)
	}
	var confirmReq int
	if err := s.db.QueryRow(`SELECT confirm_req FROM instances WHERE id=?`, id).Scan(&confirmReq); err != nil {
		t.Fatalf("query confirm_req: %v", err)
	}
	if confirmReq != 1 {
		t.Fatalf("backfill deveria congelar confirm_req=1, veio %d", confirmReq)
	}
	if ci := scanStr(t, s, "conds_in", id); ci == "" {
		t.Fatal("backfill deveria congelar conds_in (não-vazio)")
	}
	if co := scanStr(t, s, "conds_out_add", id); co == "" {
		t.Fatal("backfill deveria congelar conds_out_add (não-vazio)")
	}

	// Idempotência: segunda chamada não explode nem duplica (meta_flags).
	s.MigrateMonitoringSnapshot()
}
