package scheduler

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/businessclock"
	"github.com/Dr0nj/regente-server/internal/domain"
)

// SetClock deve ser chamado antes de Run; todos os instantes operacionais partem daqui.
func (s *Scheduler) SetClock(clock businessclock.Clock) { s.nowFn = clock.Now }
func (s *Scheduler) Now() time.Time                     { return s.nowFn().UTC() }

// BusinessCalendar lê as duas chaves no mesmo snapshot SQL. Vazio tem default
// explícito UTC/00:00; erro não é convertido em timezone do host.
func (s *Scheduler) BusinessCalendar() businessclock.Calendar {
	c := businessclock.Default()
	rows, err := s.db.Query("SELECT key,value FROM settings WHERE key IN ('daily_at','daily_timezone')")
	if err != nil {
		c.Timezone = "unavailable"
		return c
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			c.Timezone = "unavailable"
			return c
		}
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if k == "daily_at" {
			c.DailyAt = v
		} else {
			c.Timezone = v
		}
	}
	if rows.Err() != nil {
		c.Timezone = "unavailable"
	}
	return c
}
func frozenCalendar(def domain.JobDefinition) (businessclock.Calendar, bool) {
	if def.BusinessTime == nil {
		return businessclock.Calendar{}, false
	}
	c := *def.BusinessTime
	return c, c.Validate() == nil
}
func (s *Scheduler) freezeTime(def domain.JobDefinition, c businessclock.Calendar) domain.JobDefinition {
	def.BusinessTime = &c
	return def
}
func temporalBlock(def domain.JobDefinition, r instRow) string {
	if _, ok := frozenCalendar(def); ok {
		return ""
	}
	if def.Schedule.WindowTo != "" || (r.Forced && r.ForceMode == ForceModeOrder && def.Schedule.WindowFrom != "") {
		return "Legacy order has no recorded business timezone; reorder it under explicit business settings to evaluate its execution window"
	}
	return ""
}
func orderWindowEnd(def domain.JobDefinition, date string) time.Time {
	c, ok := frozenCalendar(def)
	if !ok {
		return time.Time{}
	}
	from := def.Schedule.WindowFrom
	if from == "" {
		from = def.Schedule.RunAt
	}
	return c.WindowEnd(date, from, def.Schedule.WindowTo)
}
func activityDate(def domain.JobDefinition, t sql.NullTime) string {
	if !t.Valid {
		return ""
	}
	if c, ok := frozenCalendar(def); ok {
		return c.BusinessDate(t.Time)
	}
	// Legado sem zona: não inventar data de atividade, usar ODAT conservador.
	return ""
}
func (s *Scheduler) validateBusinessTime() error {
	if err := s.BusinessCalendar().Validate(); err != nil {
		return fmt.Errorf("business time configuration: %w", err)
	}
	return nil
}
