package scheduler

import (
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

func day(s string) time.Time {
	t, _ := time.ParseInLocation("2006-01-02", s, time.Local)
	return t
}

// TestSchedulePreview_Weekly — semanal seg/qua/sex devolve EXATAMENTE esses dias
// no intervalo (fonte única: IsScheduledOn). Sem CalendarStore → noop.
func TestSchedulePreview_Weekly(t *testing.T) {
	s := newTestScheduler(t)
	def := domain.JobDefinition{ID: "j", Schedule: domain.Schedule{
		Frequency: "weekly", DaysOfWeek: []string{"mon", "wed", "fri"},
	}}
	got := s.SchedulePreview(def, day("2026-06-29"), day("2026-07-05")) // seg→dom
	want := []string{"2026-06-29", "2026-07-01", "2026-07-03"}          // seg, qua, sex
	if !eqStr(got, want) {
		t.Fatalf("weekly preview = %v, want %v", got, want)
	}
}

// TestSchedulePreview_Daily — diário marca TODOS os dias do intervalo.
func TestSchedulePreview_Daily(t *testing.T) {
	s := newTestScheduler(t)
	def := domain.JobDefinition{ID: "j", Schedule: domain.Schedule{Frequency: "daily"}}
	got := s.SchedulePreview(def, day("2026-06-29"), day("2026-07-02"))
	want := []string{"2026-06-29", "2026-06-30", "2026-07-01", "2026-07-02"}
	if !eqStr(got, want) {
		t.Fatalf("daily preview = %v, want %v", got, want)
	}
}

// TestSchedulePreview_MonthFilter — filtro de meses tira o mês inteiro.
func TestSchedulePreview_MonthFilter(t *testing.T) {
	s := newTestScheduler(t)
	def := domain.JobDefinition{ID: "j", Schedule: domain.Schedule{
		Frequency: "daily", MonthsOfYear: []int{7}, // só julho
	}}
	got := s.SchedulePreview(def, day("2026-06-29"), day("2026-07-02"))
	want := []string{"2026-07-01", "2026-07-02"} // junho excluído pelo filtro
	if !eqStr(got, want) {
		t.Fatalf("month-filter preview = %v, want %v", got, want)
	}
}

// TestSchedulePreview_Monthly — dias do mês específicos (1 e 15).
func TestSchedulePreview_Monthly(t *testing.T) {
	s := newTestScheduler(t)
	def := domain.JobDefinition{ID: "j", Schedule: domain.Schedule{
		Frequency: "monthly", DaysOfMonth: []int{1, 15},
	}}
	got := s.SchedulePreview(def, day("2026-07-01"), day("2026-07-31"))
	want := []string{"2026-07-01", "2026-07-15"}
	if !eqStr(got, want) {
		t.Fatalf("monthly preview = %v, want %v", got, want)
	}
}

// TestSchedulePreview_WeeklyNoDays — semanal SEM dia escolhido = schedule
// incompleto → NÃO roda nenhum dia (regressão: antes rodava todos os dias).
func TestSchedulePreview_WeeklyNoDays(t *testing.T) {
	s := newTestScheduler(t)
	def := domain.JobDefinition{ID: "j", Schedule: domain.Schedule{Frequency: "weekly"}}
	got := s.SchedulePreview(def, day("2026-06-29"), day("2026-07-05"))
	if len(got) != 0 {
		t.Fatalf("weekly sem dias deveria ser vazio, got %v", got)
	}
}

// TestSchedulePreview_MonthlyNoDays — mensal SEM dia escolhido → NÃO roda.
func TestSchedulePreview_MonthlyNoDays(t *testing.T) {
	s := newTestScheduler(t)
	def := domain.JobDefinition{ID: "j", Schedule: domain.Schedule{Frequency: "monthly"}}
	got := s.SchedulePreview(def, day("2026-07-01"), day("2026-07-31"))
	if len(got) != 0 {
		t.Fatalf("monthly sem dias deveria ser vazio, got %v", got)
	}
}

func eqStr(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
