// Package scheduler — F14 Calendars: regras de elegibilidade.
//
// IsEligibleDate: dado um calendar e uma data, retorna se o job deve rodar
// naquele dia. Lógica:
//
//  1. Se ExcludeDates contém a data → false (override máximo).
//  2. Se IncludeDates contém a data → true (override de holidays).
//  3. Se Holidays contém a data → false.
//  4. Se BusinessDays vazio → true (qualquer dia).
//  5. Se weekday(date) ∈ BusinessDays → true.
//  6. Caso contrário → false.
package scheduler

import (
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

func IsEligibleDate(cal *domain.Calendar, date time.Time) bool {
	if cal == nil {
		return true
	}
	iso := date.Format("2006-01-02")
	for _, d := range cal.ExcludeDates {
		if d == iso {
			return false
		}
	}
	for _, d := range cal.IncludeDates {
		if d == iso {
			return true
		}
	}
	for _, d := range cal.Holidays {
		if d == iso {
			return false
		}
	}
	if len(cal.BusinessDays) == 0 {
		return true
	}
	wd := strings.ToLower(date.Weekday().String()[:3]) // "mon","tue",...
	for _, b := range cal.BusinessDays {
		if strings.ToLower(strings.TrimSpace(b)) == wd {
			return true
		}
	}
	return false
}

// calLookup — abstrai o CalendarStore (evita ciclo de import; o store implementa Get).
type calLookup interface {
	Get(name string) (*domain.Calendar, error)
}

// noopCalLookup — store vazio (sem calendars configurados): nenhum calendar
// existe, então include/exclude não filtram. Usado pelo SchedulePreview quando
// o scheduler roda sem CalendarStore.
type noopCalLookup struct{}

func (noopCalLookup) Get(string) (*domain.Calendar, error) { return nil, nil }

// SchedulePreview devolve as datas (YYYY-MM-DD) em que a definition RODARIA no
// intervalo [from, to] inclusivo, avaliando IsScheduledOn dia a dia — a MESMA
// regra do RunDaily (fonte única). Garante que o calendário do preview na UI bate
// EXATAMENTE com o que a daily materializaria: dia presente = roda, ausente = não
// roda. Limitado a ~2 anos por chamada (defesa contra range absurdo).
func (s *Scheduler) SchedulePreview(d domain.JobDefinition, from, to time.Time) []string {
	var store calLookup = noopCalLookup{}
	if s.calStore != nil {
		store = s.calStore
	}
	out := []string{}
	guard := 0
	for t := from; !t.After(to) && guard < 800; t = t.AddDate(0, 0, 1) {
		guard++
		if IsScheduledOn(d, t, store) {
			out = append(out, t.Format("2006-01-02"))
		}
	}
	return out
}

// IsScheduledOn decide se a definition deve ser ordenada na data dada,
// combinando: filtro de meses + recorrência (Frequency) + calendars do job
// (include/exclude) + shift (roll para dia útil). É a regra usada pela daily —
// e, por ser fonte única, DryRun e SchedulePreview enxergam o shift de graça.
func IsScheduledOn(d domain.JobDefinition, date time.Time, store calLookup) bool {
	// Caminho normal: o dia nominal da recorrência é elegível.
	if nominalScheduledOn(d, date, store) && shiftEligible(d, date, store) {
		return true
	}

	// Shift (Control-M "roll"): o dia NOMINAL caiu num dia não-elegível e a
	// execução rola pro dia elegível mais próximo (adiante ou atrás). A data
	// avaliada `date` só "herda" a execução se for esse dia mais próximo.
	const shiftLookback = 31 // dias — cobre qualquer feriado/ponte realista
	switch d.Schedule.Shift {
	case "next-businessday":
		// `date` roda se é elegível E existe um dia nominal B < date não-elegível
		// com TODOS os dias entre B e date também não-elegíveis.
		if !shiftEligible(d, date, store) {
			return false
		}
		for b := date.AddDate(0, 0, -1); date.Sub(b) <= shiftLookback*24*time.Hour; b = b.AddDate(0, 0, -1) {
			if shiftEligible(d, b, store) {
				return false // achou dia elegível antes de um nominal → não herda
			}
			if nominalScheduledOn(d, b, store) {
				return true // nominal caiu em dia ruim; `date` é o 1º dia bom depois
			}
		}
		return false
	case "prev-businessday":
		// Espelho: `date` roda se é elegível E existe um nominal B > date
		// não-elegível com todos os dias entre date e B não-elegíveis.
		if !shiftEligible(d, date, store) {
			return false
		}
		for b := date.AddDate(0, 0, 1); b.Sub(date) <= shiftLookback*24*time.Hour; b = b.AddDate(0, 0, 1) {
			if shiftEligible(d, b, store) {
				return false
			}
			if nominalScheduledOn(d, b, store) {
				return true // nominal cairá em dia ruim; `date` é o último dia bom antes
			}
		}
		return false
	}
	return false
}

// nominalScheduledOn — o componente "dia nominal" do schedule: meses + recorrência.
// NÃO inclui calendars (a elegibilidade fica em shiftEligible p/ o shift poder rolar).
func nominalScheduledOn(d domain.JobDefinition, date time.Time, store calLookup) bool {
	sched := d.Schedule
	if len(sched.MonthsOfYear) > 0 && !containsInt(sched.MonthsOfYear, int(date.Month())) {
		return false
	}
	return matchesFrequency(d, date, store)
}

// shiftEligible — elegibilidade do DIA para fins de calendário/shift:
//   - job COM calendars → include (todos) + exclude (nenhum), como sempre;
//   - job SEM calendars e COM shift configurado → dia útil Mon-Fri (senão o
//     shift seria um no-op silencioso; "monthly dia 5, rola se cair no fim de
//     semana" funciona sem precisar criar calendar);
//   - job sem calendars e sem shift → sempre elegível (comportamento clássico).
func shiftEligible(d domain.JobDefinition, date time.Time, store calLookup) bool {
	refs := effectiveCalendarRefs(d)
	if len(refs) == 0 {
		if d.Schedule.Shift == "next-businessday" || d.Schedule.Shift == "prev-businessday" {
			wd := date.Weekday()
			return wd != time.Saturday && wd != time.Sunday
		}
		return true
	}
	for _, ref := range refs {
		cal, err := store.Get(ref.Name)
		if err != nil || cal == nil {
			continue // calendar ausente não bloqueia (logado pelo caller se quiser)
		}
		eligible := IsEligibleDate(cal, date)
		if ref.Mode == "exclude" {
			if eligible {
				return false // negar: dia pertence ao calendar → não roda
			}
		} else { // include
			if !eligible {
				return false
			}
		}
	}
	return true
}

func effectiveCalendarRefs(d domain.JobDefinition) []domain.CalendarRef {
	refs := make([]domain.CalendarRef, 0, len(d.Calendars)+1)
	if d.Calendar != "" {
		refs = append(refs, domain.CalendarRef{Name: d.Calendar, Mode: "include"})
	}
	for _, r := range d.Calendars {
		mode := r.Mode
		if mode != "exclude" {
			mode = "include"
		}
		refs = append(refs, domain.CalendarRef{Name: r.Name, Mode: mode})
	}
	return refs
}

// matchesFrequency avalia o componente "em quais dias" do schedule.
func matchesFrequency(d domain.JobDefinition, date time.Time, store calLookup) bool {
	s := d.Schedule
	switch s.Frequency {
	case "", "daily":
		return true
	case "weekly":
		if len(s.DaysOfWeek) == 0 {
			// Semanal sem nenhum dia escolhido = schedule incompleto.
			// NÃO roda (só "daily" roda todo dia). O preview reflete isso.
			return false
		}
		wd := strings.ToLower(date.Weekday().String()[:3])
		for _, x := range s.DaysOfWeek {
			if strings.ToLower(strings.TrimSpace(x)) == wd {
				return true
			}
		}
		return false
	case "monthly":
		if len(s.DaysOfMonth) == 0 {
			// Mensal sem nenhum dia escolhido = schedule incompleto. NÃO roda.
			return false
		}
		last := lastDayOfMonth(date)
		for _, dom := range s.DaysOfMonth {
			if dom == -1 && date.Day() == last {
				return true
			}
			if dom == date.Day() {
				return true
			}
		}
		return false
	case "businessday":
		if len(s.NthBusinessDays) == 0 {
			return true
		}
		bdays := businessDaysOfMonth(date, businessCalendar(d, store))
		idx := indexOfDate(bdays, date)
		if idx < 0 {
			return false // não é dia útil
		}
		nth := idx + 1              // 1-based
		fromEnd := len(bdays) - idx // 1 = último
		for _, n := range s.NthBusinessDays {
			if n > 0 && n == nth {
				return true
			}
			if n < 0 && (-n) == fromEnd {
				return true
			}
		}
		return false
	case "advanced":
		return matchesAdvancedRule(s.AdvancedRule, date, businessCalendar(d, store))
	default:
		return true
	}
}

// businessCalendar escolhe o calendar usado para contar dias úteis: o primeiro
// include do job, senão nil (=> Mon-Fri puro).
func businessCalendar(d domain.JobDefinition, store calLookup) *domain.Calendar {
	for _, ref := range effectiveCalendarRefs(d) {
		if ref.Mode != "include" {
			continue
		}
		if cal, err := store.Get(ref.Name); err == nil && cal != nil {
			return cal
		}
	}
	return nil
}

// businessDaysOfMonth lista os dias úteis do mês de `date`. Dia útil = weekday
// Mon-Fri (ou BusinessDays do calendar, se houver) e não é holiday/exclude.
func businessDaysOfMonth(date time.Time, cal *domain.Calendar) []time.Time {
	first := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	out := []time.Time{}
	for day := first; day.Month() == first.Month(); day = day.AddDate(0, 0, 1) {
		if isBusinessDay(day, cal) {
			out = append(out, day)
		}
	}
	return out
}

func isBusinessDay(day time.Time, cal *domain.Calendar) bool {
	if cal != nil {
		// usa a própria elegibilidade do calendar (businessDays + holidays + include/exclude)
		return IsEligibleDate(cal, day)
	}
	wd := day.Weekday()
	return wd != time.Saturday && wd != time.Sunday
}

// matchesAdvancedRule — enum de regras nomeadas (v1).
func matchesAdvancedRule(rule string, date time.Time, cal *domain.Calendar) bool {
	bdays := businessDaysOfMonth(date, cal)
	idx := indexOfDate(bdays, date)
	switch rule {
	case "first-businessday":
		return idx == 0
	case "last-businessday":
		return idx == len(bdays)-1 && idx >= 0
	case "first-businessday-not-monday":
		// primeiro dia útil do mês cujo weekday != segunda
		for _, bd := range bdays {
			if bd.Weekday() != time.Monday {
				return sameDate(bd, date)
			}
		}
		return false
	case "penultimate-businessday":
		return idx == len(bdays)-2 && idx >= 0
	default:
		return false
	}
}

func lastDayOfMonth(t time.Time) int {
	return time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, t.Location()).Day()
}

func indexOfDate(list []time.Time, d time.Time) int {
	for i, x := range list {
		if sameDate(x, d) {
			return i
		}
	}
	return -1
}

func sameDate(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
