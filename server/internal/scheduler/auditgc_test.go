package scheduler

import "testing"

// E2 — retenção de auditoria (audit_retention_days). O GC remove SÓ o que
// passou do prazo, respeita o default infinito e roda em lotes.

func seedEvent(t *testing.T, s *Scheduler, table, ageModifier string) {
	t.Helper()
	var err error
	if table == "instance_events" {
		_, err = s.db.Exec(
			`INSERT INTO instance_events(instance_id, kind, actor, message, ts) VALUES('i1','x','t','seed', datetime('now', ?))`,
			ageModifier,
		)
	} else {
		_, err = s.db.Exec(
			`INSERT INTO audit_events(kind, actor, ts) VALUES('auth.login','t', datetime('now', ?))`,
			ageModifier,
		)
	}
	if err != nil {
		t.Fatalf("seed %s: %v", table, err)
	}
}

func countRows(t *testing.T, s *Scheduler, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestAuditGC_RemoveSoOQuePassouDoPrazo(t *testing.T) {
	s := newTestScheduler(t)
	for _, tbl := range []string{"instance_events", "audit_events"} {
		seedEvent(t, s, tbl, "-40 days")
		seedEvent(t, s, tbl, "-31 days")
		seedEvent(t, s, tbl, "-1 days")
	}

	// Default (setting ausente) = infinito: nada sai.
	s.auditGC()
	if n := countRows(t, s, "instance_events"); n != 3 {
		t.Fatalf("sem setting o GC não deveria remover nada; instance_events=%d", n)
	}

	// Retenção 30d em lotes de 1 (exercita o loop de batches).
	setSetting(t, s, "audit_retention_days", "30")
	orig := auditGCBatch
	auditGCBatch = 1
	defer func() { auditGCBatch = orig }()
	s.auditGC()
	for _, tbl := range []string{"instance_events", "audit_events"} {
		if n := countRows(t, s, tbl); n != 1 {
			t.Fatalf("%s: esperava só o evento recente após GC de 30d, veio %d linhas", tbl, n)
		}
	}

	// Idempotente: rodar de novo não mexe no que está dentro do prazo.
	s.auditGC()
	if n := countRows(t, s, "instance_events"); n != 1 {
		t.Fatalf("re-rodar o GC não deveria remover o evento recente; veio %d", n)
	}
}

func TestAuditGC_SettingInvalidoOuZeroDesliga(t *testing.T) {
	s := newTestScheduler(t)
	seedEvent(t, s, "instance_events", "-400 days")

	for _, v := range []string{"0", "", "abc", "-5"} {
		setSetting(t, s, "audit_retention_days", v)
		s.auditGC()
		if n := countRows(t, s, "instance_events"); n != 1 {
			t.Fatalf("audit_retention_days=%q deveria desligar o GC; sobraram %d linhas", v, n)
		}
	}
}
