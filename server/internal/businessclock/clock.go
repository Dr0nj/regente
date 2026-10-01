// Package businessclock centraliza datas de negócio e resolução de horários civis.
package businessclock

import (
	"fmt"
	"sync"
	"time"
	_ "time/tzdata" // IANA disponível também em Windows e imagens mínimas.
)

var locations sync.Map

func loadLocation(name string) (*time.Location, error) {
	if v, ok := locations.Load(name); ok {
		return v.(*time.Location), nil
	}
	loc, err := time.LoadLocation(name)
	if err == nil {
		locations.Store(name, loc)
	}
	return loc, err
}

type Clock interface{ Now() time.Time }
type System struct{}

func (System) Now() time.Time { return time.Now().UTC() }

type Func func() time.Time

func (f Func) Now() time.Time { return f().UTC() }

// Calendar é congelado no snapshot da ordem; não contém o relógio do host.
type Calendar struct {
	Timezone string `json:"timezone"`
	DailyAt  string `json:"dailyAt"`
}

func Default() Calendar { return Calendar{Timezone: "UTC", DailyAt: "00:00"} }
func ParseHM(hm string) (int, int, bool) {
	t, err := time.Parse("15:04", hm)
	if err != nil || len(hm) != 5 {
		return 0, 0, false
	}
	return t.Hour(), t.Minute(), true
}
func (c Calendar) Validate() error {
	if c.Timezone == "" || c.Timezone == "Local" {
		return fmt.Errorf("business timezone must be an explicit IANA name (for example UTC)")
	}
	if _, err := loadLocation(c.Timezone); err != nil {
		return fmt.Errorf("invalid business timezone %q", c.Timezone)
	}
	if _, _, ok := ParseHM(c.DailyAt); !ok {
		return fmt.Errorf("daily_at must use HH:MM (00:00–23:59)")
	}
	return nil
}
func (c Calendar) Location() *time.Location {
	loc, err := loadLocation(c.Timezone)
	if err != nil || c.Timezone == "" || c.Timezone == "Local" {
		return time.UTC
	}
	return loc
}
func NextDate(date string, n int) string {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return ""
	}
	return d.AddDate(0, 0, n).Format("2006-01-02")
}

// Wall resolve fold pela primeira ocorrência e gap pelo primeiro instante válido
// após a lacuna. Itera períodos IANA reais; não supõe transições de uma hora.
func (c Calendar) Wall(date, hm string) time.Time {
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return time.Time{}
	}
	h, m, ok := ParseHM(hm)
	if !ok {
		return time.Time{}
	}
	wall := d.Add(time.Duration(h*60+m) * time.Minute)
	loc := c.Location()
	lo, hi := wall.Add(-72*time.Hour), wall.Add(72*time.Hour)
	var first, gap time.Time
	for cursor := lo; cursor.Before(hi); {
		local := cursor.In(loc)
		start, end := local.ZoneBounds()
		_, off := local.Zone()
		candidate := wall.Add(-time.Duration(off) * time.Second)
		if (start.IsZero() || !candidate.Before(start)) && (end.IsZero() || candidate.Before(end)) {
			if first.IsZero() || candidate.Before(first) {
				first = candidate
			}
		}
		if end.IsZero() || !end.Before(hi) {
			break
		}
		_, nextOff := end.In(loc).Zone()
		gapStart := end.UTC().Add(time.Duration(off) * time.Second)
		gapEnd := end.UTC().Add(time.Duration(nextOff) * time.Second)
		if nextOff > off && !wall.Before(gapStart) && wall.Before(gapEnd) {
			gap = end.UTC()
		}
		cursor = end
	}
	if !first.IsZero() {
		return first.UTC()
	}
	return gap.UTC()
}
func (c Calendar) Start(date string) time.Time { return c.Wall(date, c.DailyAt) }
func (c Calendar) BusinessDate(now time.Time) string {
	date := now.In(c.Location()).Format("2006-01-02")
	if now.Before(c.Start(date)) {
		return NextDate(date, -1)
	}
	return date
}

// At mapeia HH:MM na diária: antes da virada pertence à manhã de D+1.
func (c Calendar) At(date, hm string) time.Time {
	if _, _, ok := ParseHM(hm); !ok {
		return time.Time{}
	}
	if hm < c.DailyAt {
		date = NextDate(date, 1)
	}
	return c.Wall(date, hm)
}
func (c Calendar) WindowEnd(date, from, to string) time.Time {
	end := c.At(date, to)
	start := c.At(date, from)
	if !end.IsZero() && !start.IsZero() && end.Before(start) {
		end = c.At(NextDate(date, 1), to)
	}
	return end
}
