package main

import "testing"

func TestDisposableDSN(t *testing.T) {
	for _, tc := range []struct {
		dsn              string
		private, allowed bool
	}{
		{"postgres://regente:synthetic-lab-only@127.0.0.1:1234/regente_capacity_test", false, true},
		{"postgres://regente:synthetic-lab-only@postgres:5432/regente_capacity_test", true, true},
		{"postgres://regente:synthetic-lab-only@postgres:5432/regente_capacity_test", false, false},
		{"postgres://regente:synthetic-lab-only@production:5432/regente_capacity_test", true, false},
		{"postgres://regente:real-secret@postgres:5432/regente_capacity_test", true, false},
		{"postgres://regente:synthetic-lab-only@postgres:5433/regente_capacity_test", true, false},
		{"postgres://regente:synthetic-lab-only@postgres:5432/production", true, false},
		{"postgres://regente:synthetic-lab-only@postgres:5432/regente_capacity_", true, false},
	} {
		if got := disposableDSN(tc.dsn, tc.private); got != tc.allowed {
			t.Fatalf("validação=%v, esperado=%v", got, tc.allowed)
		}
	}
}
