package businessclock

import (
	"testing"
	"time"
)

func TestI06Calendar(t *testing.T) {
	for _, tc := range []struct{ zone, cut, date, hm, want string }{
		{"America/Sao_Paulo", "06:00", "2026-09-29", "02:00", "2026-09-30T05:00:00Z"},
		{"America/New_York", "00:00", "2026-03-08", "02:30", "2026-03-08T07:00:00Z"},
		{"America/New_York", "00:00", "2026-11-01", "01:30", "2026-11-01T05:30:00Z"},
		{"Australia/Lord_Howe", "00:00", "2026-10-04", "02:15", "2026-10-03T15:30:00Z"},
		{"Pacific/Apia", "00:00", "2011-12-30", "12:00", "2011-12-30T10:00:00Z"},
	} {
		t.Run(tc.zone+tc.date, func(t *testing.T) {
			c := Calendar{tc.zone, tc.cut}
			got := c.At(tc.date, tc.hm)
			if got.Format(time.RFC3339) != tc.want {
				t.Fatalf("obtido %v; esperado %s", got, tc.want)
			}
		})
	}
	c := Calendar{"America/Sao_Paulo", "06:00"}
	for _, tc := range []struct{ now, want string }{{"2026-09-29T08:59:59Z", "2026-09-28"}, {"2026-09-29T09:00:00Z", "2026-09-29"}} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		if got := c.BusinessDate(now); got != tc.want {
			t.Fatalf("data %s; esperado %s", got, tc.want)
		}
	}
	if got := c.WindowEnd("2026-09-29", "23:00", "02:00").Format(time.RFC3339); got != "2026-09-30T05:00:00Z" {
		t.Fatal(got)
	}
}
func TestI06DSTBusinessBoundary(t *testing.T) {
	c := Calendar{"America/New_York", "01:30"}
	// A segunda 01:00 não desfaz a virada que já ocorreu na primeira 01:30.
	now, _ := time.Parse(time.RFC3339, "2026-11-01T06:00:00Z")
	if c.BusinessDate(now) != "2026-11-01" {
		t.Fatal("fold retrocedeu a data")
	}
	spring := Calendar{"America/New_York", "02:30"}
	if got := spring.Start("2026-03-08").Format(time.RFC3339); got != "2026-03-08T07:00:00Z" {
		t.Fatal(got)
	}
}
func TestI06Validation(t *testing.T) {
	for _, c := range []Calendar{{"Local", "00:00"}, {"", "00:00"}, {"Not/AZone", "00:00"}, {"UTC", "6:00"}, {"UTC", "24:00"}} {
		if c.Validate() == nil {
			t.Fatalf("aceitou %v", c)
		}
	}
}
