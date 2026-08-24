package scheduler

import (
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// seedFolderDefs — publica N jobs na folder, direto no slice de defs (mesmo
// atalho do seedForceDef: o que OrderFolder lê é s.defs).
func seedFolderDefs(t *testing.T, s *Scheduler, defs ...domain.JobDefinition) {
	t.Helper()
	s.mu.Lock()
	s.defs = defs
	s.mu.Unlock()
}

func countFolderInstances(t *testing.T, s *Scheduler, folder, date string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM instances WHERE team=? AND order_date=?`, folder, date).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// OrderFolder ordena a folder INTEIRA na diária ativa e não toca em outras folders.
func TestOrderFolder_OrdersWholeFolderIntoActiveDaily(t *testing.T) {
	s := newTestScheduler(t)
	seedFolderDefs(t, s,
		domain.JobDefinition{ID: "a", Label: "A", Team: "FIN", JobType: "COMMAND"},
		domain.JobDefinition{ID: "b", Label: "B", Team: "FIN", JobType: "COMMAND"},
		domain.JobDefinition{ID: "c", Label: "C", Team: "FIN", JobType: "COMMAND"},
		domain.JobDefinition{ID: "z", Label: "Z", Team: "OUTRA", JobType: "COMMAND"},
	)

	res, err := s.OrderFolder("FIN")
	if err != nil {
		t.Fatalf("order folder: %v", err)
	}
	if len(res.Ordered) != 3 {
		t.Fatalf("esperava 3 ordens criadas, veio %d (%v)", len(res.Ordered), res.Ordered)
	}
	if len(res.Skipped) != 0 {
		t.Fatalf("nada deveria ser pulado na 1ª ordem, veio %v", res.Skipped)
	}
	// A diária ATIVA é a data de NEGÓCIO — o mesmo dia que a tela pede.
	if res.OrderDate != s.TodayDate() {
		t.Fatalf("order_date deveria ser a diária ativa %s, veio %s", s.TodayDate(), res.OrderDate)
	}
	if n := countFolderInstances(t, s, "FIN", res.OrderDate); n != 3 {
		t.Fatalf("esperava 3 instances de FIN no dia, veio %d", n)
	}
	// Folder vizinha não entra na ordem.
	if n := countFolderInstances(t, s, "OUTRA", res.OrderDate); n != 0 {
		t.Fatalf("OUTRA não foi ordenada, mas tem %d instance(s)", n)
	}

	// force_mode='order': bypassa o AGENDAMENTO, mantém os gates de runtime.
	var forced int
	var mode string
	if err := s.db.QueryRow(`SELECT forced, force_mode FROM instances WHERE definition_id='a' AND order_date=?`, res.OrderDate).Scan(&forced, &mode); err != nil {
		t.Fatalf("select: %v", err)
	}
	if forced != 1 || mode != ForceModeOrder {
		t.Fatalf("esperava forced=1 force_mode=%q, veio forced=%d mode=%q", ForceModeOrder, forced, mode)
	}
}

// Chamar duas vezes NÃO duplica: quem já está na diária ativa é pulado.
func TestOrderFolder_SkipsWhatIsAlreadyInTheDaily(t *testing.T) {
	s := newTestScheduler(t)
	seedFolderDefs(t, s,
		domain.JobDefinition{ID: "a", Label: "A", Team: "FIN", JobType: "COMMAND"},
		domain.JobDefinition{ID: "b", Label: "B", Team: "FIN", JobType: "COMMAND"},
	)

	first, err := s.OrderFolder("FIN")
	if err != nil {
		t.Fatalf("1ª ordem: %v", err)
	}
	second, err := s.OrderFolder("FIN")
	if err != nil {
		t.Fatalf("2ª ordem: %v", err)
	}
	if len(second.Ordered) != 0 {
		t.Fatalf("a 2ª ordem não deveria criar nada, criou %v", second.Ordered)
	}
	if len(second.Skipped) != 2 {
		t.Fatalf("a 2ª ordem deveria pular os 2 jobs, pulou %v", second.Skipped)
	}
	if n := countFolderInstances(t, s, "FIN", first.OrderDate); n != 2 {
		t.Fatalf("a folder não pode duplicar: esperava 2 instances, veio %d", n)
	}
}

// Job publicado DEPOIS da 1ª ordem entra na 2ª (o resto continua pulado).
func TestOrderFolder_OrdersOnlyTheMissingOnes(t *testing.T) {
	s := newTestScheduler(t)
	seedFolderDefs(t, s, domain.JobDefinition{ID: "a", Label: "A", Team: "FIN", JobType: "COMMAND"})
	if _, err := s.OrderFolder("FIN"); err != nil {
		t.Fatalf("1ª ordem: %v", err)
	}

	seedFolderDefs(t, s,
		domain.JobDefinition{ID: "a", Label: "A", Team: "FIN", JobType: "COMMAND"},
		domain.JobDefinition{ID: "novo", Label: "NOVO", Team: "FIN", JobType: "COMMAND"},
	)
	res, err := s.OrderFolder("FIN")
	if err != nil {
		t.Fatalf("2ª ordem: %v", err)
	}
	if len(res.Ordered) != 1 || len(res.Skipped) != 1 {
		t.Fatalf("esperava 1 ordenado (novo) e 1 pulado (a), veio ordered=%v skipped=%v", res.Ordered, res.Skipped)
	}
	if n := countFolderInstances(t, s, "FIN", res.OrderDate); n != 2 {
		t.Fatalf("esperava 2 instances no dia, veio %d", n)
	}
}

// Folder sem definition publicada é erro — não silêncio com 0 ordens.
func TestOrderFolder_UnknownFolderIsAnError(t *testing.T) {
	s := newTestScheduler(t)
	seedFolderDefs(t, s, domain.JobDefinition{ID: "a", Label: "A", Team: "FIN", JobType: "COMMAND"})
	if _, err := s.OrderFolder("NAO-EXISTE"); err == nil {
		t.Fatal("folder inexistente deveria devolver erro")
	}
}

// DAY-1 — a ordem manual (folder E job) entra na diária ATIVA: antes de daily_at
// o dia de negócio ainda é o ANTERIOR, e é nele que a ordem tem que nascer.
// Gravando a data-calendário, o card aparecia num dia que o board não mostra.
func TestOrderIntoActiveDaily_BeforeDailyAtBelongsToPreviousDay(t *testing.T) {
	s := newTestScheduler(t)
	seedFolderDefs(t, s, domain.JobDefinition{ID: "a", Label: "A", Team: "FIN", JobType: "COMMAND"})
	setSetting(t, s, "daily_at", "06:00")
	// 02:00 — depois da meia-noite, ANTES da virada do dia de negócio.
	fixed := time.Date(2026, 8, 24, 2, 0, 0, 0, time.Local)
	s.nowFn = func() time.Time { return fixed }

	want := "2026-08-23" // o dia de negócio corrente às 02:00 com daily_at=06:00
	if got := s.TodayDate(); got != want {
		t.Fatalf("diária ativa deveria ser %s, veio %s", want, got)
	}

	res, err := s.OrderFolder("FIN")
	if err != nil {
		t.Fatalf("order folder: %v", err)
	}
	if res.OrderDate != want {
		t.Fatalf("Order Folder gravou order_date=%s, esperado a diária ativa %s", res.OrderDate, want)
	}
	if n := countFolderInstances(t, s, "FIN", want); n != 1 {
		t.Fatalf("a ordem deveria estar na diária ativa %s, tem %d instance(s)", want, n)
	}

	// O Order Force individual obedece à MESMA régua (era o bug: time.Now()).
	seedFolderDefs(t, s, domain.JobDefinition{ID: "solo", Label: "SOLO", Team: "FIN", JobType: "COMMAND"})
	id, err := s.ForceOrder("solo")
	if err != nil {
		t.Fatalf("force order: %v", err)
	}
	var od string
	if err := s.db.QueryRow(`SELECT order_date FROM instances WHERE id=?`, id).Scan(&od); err != nil {
		t.Fatalf("select: %v", err)
	}
	if od != want {
		t.Fatalf("Order Force gravou order_date=%s, esperado a diária ativa %s", od, want)
	}
}
