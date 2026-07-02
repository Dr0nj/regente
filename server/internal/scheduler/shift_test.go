package scheduler

import (
	"testing"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// calMap — calLookup de teste em memória.
type calMap map[string]*domain.Calendar

func (m calMap) Get(name string) (*domain.Calendar, error) { return m[name], nil }

// Shift (Control-M "roll"): dia nominal inelegível pelo calendar rola pro
// próximo/anterior dia elegível; sem shift, simplesmente não roda.
func TestShift_CalendarHoliday(t *testing.T) {
	// 2026-08-05 = quarta (feriado no calendar); 06 = quinta; 04 = terça.
	cals := calMap{"br": {Name: "br", Holidays: []string{"2026-08-05"}}}
	base := domain.JobDefinition{
		ID: "j", Schedule: domain.Schedule{
			Enabled: true, Frequency: "monthly", DaysOfMonth: []int{5},
		},
		Calendars: []domain.CalendarRef{{Name: "br", Mode: "include"}},
	}

	// SEM shift: 05 não roda (feriado) e nenhum outro dia herda.
	if IsScheduledOn(base, day("2026-08-05"), cals) {
		t.Fatal("sem shift: dia 5 (feriado) não deveria rodar")
	}
	if IsScheduledOn(base, day("2026-08-06"), cals) {
		t.Fatal("sem shift: dia 6 não deveria herdar nada")
	}

	// shift=next-businessday: 05 não roda; 06 herda; 07 NÃO (o 06 já pegou).
	next := base
	next.Schedule.Shift = "next-businessday"
	if IsScheduledOn(next, day("2026-08-05"), cals) {
		t.Fatal("next: o próprio feriado não roda")
	}
	if !IsScheduledOn(next, day("2026-08-06"), cals) {
		t.Fatal("next: dia 6 deveria herdar o nominal do dia 5")
	}
	if IsScheduledOn(next, day("2026-08-07"), cals) {
		t.Fatal("next: dia 7 não herda (6 já era elegível)")
	}

	// shift=prev-businessday: 04 antecipa.
	prev := base
	prev.Schedule.Shift = "prev-businessday"
	if !IsScheduledOn(prev, day("2026-08-04"), cals) {
		t.Fatal("prev: dia 4 deveria antecipar o nominal do dia 5")
	}
	if IsScheduledOn(prev, day("2026-08-05"), cals) || IsScheduledOn(prev, day("2026-08-06"), cals) {
		t.Fatal("prev: 5 (feriado) e 6 não rodam")
	}
}

// Ponte de feriados consecutivos: nominal 05, feriados 05+06 → next roda no 07.
func TestShift_ConsecutiveHolidays(t *testing.T) {
	cals := calMap{"br": {Name: "br", Holidays: []string{"2026-08-05", "2026-08-06"}}}
	d := domain.JobDefinition{
		ID: "j", Schedule: domain.Schedule{
			Enabled: true, Frequency: "monthly", DaysOfMonth: []int{5}, Shift: "next-businessday",
		},
		Calendars: []domain.CalendarRef{{Name: "br", Mode: "include"}},
	}
	if IsScheduledOn(d, day("2026-08-06"), cals) {
		t.Fatal("6 também é feriado — não roda")
	}
	if !IsScheduledOn(d, day("2026-08-07"), cals) {
		t.Fatal("7 é o primeiro dia elegível — deveria herdar")
	}
}

// Sem calendar nenhum, shift usa Mon-Fri: monthly dia que cai no SÁBADO rola
// pra segunda (next) ou antecipa pra sexta (prev).
func TestShift_WeekendWithoutCalendar(t *testing.T) {
	// 2026-08-01 = sábado; 2026-08-03 = segunda; 2026-07-31 = sexta.
	d := domain.JobDefinition{
		ID: "j", Schedule: domain.Schedule{
			Enabled: true, Frequency: "monthly", DaysOfMonth: []int{1}, Shift: "next-businessday",
		},
	}
	cals := calMap{}
	if IsScheduledOn(d, day("2026-08-01"), cals) {
		t.Fatal("sábado não roda com shift")
	}
	if IsScheduledOn(d, day("2026-08-02"), cals) {
		t.Fatal("domingo não roda")
	}
	if !IsScheduledOn(d, day("2026-08-03"), cals) {
		t.Fatal("segunda deveria herdar o nominal de sábado")
	}

	d.Schedule.Shift = "prev-businessday"
	if !IsScheduledOn(d, day("2026-07-31"), cals) {
		t.Fatal("sexta deveria antecipar o nominal de sábado")
	}

	// SEM shift, sem calendar: sábado roda normalmente (comportamento clássico).
	d.Schedule.Shift = ""
	if !IsScheduledOn(d, day("2026-08-01"), cals) {
		t.Fatal("sem shift/calendar, o nominal roda no próprio dia")
	}
}

// Dia nominal elegível: shift não muda nada (roda no próprio dia, vizinhos não).
func TestShift_NoOpWhenNominalEligible(t *testing.T) {
	cals := calMap{"br": {Name: "br", Holidays: []string{"2026-12-25"}}}
	d := domain.JobDefinition{
		ID: "j", Schedule: domain.Schedule{
			Enabled: true, Frequency: "monthly", DaysOfMonth: []int{10}, Shift: "next-businessday",
		},
		Calendars: []domain.CalendarRef{{Name: "br", Mode: "include"}},
	}
	// 2026-08-10 = segunda, elegível.
	if !IsScheduledOn(d, day("2026-08-10"), cals) {
		t.Fatal("nominal elegível roda no próprio dia")
	}
	if IsScheduledOn(d, day("2026-08-11"), cals) {
		t.Fatal("dia seguinte não herda quando o nominal rodou")
	}
}
