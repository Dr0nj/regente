package scheduler

import (
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// E1 — timezone da daily. O relógio de NEGÓCIO (settings.daily_timezone) manda
// em `today`, no horário-alvo e no order_date; o relógio do host (UTC em
// produção) é só a fonte crua. nowFn é injetado pra simular o clock.

func setSetting(t *testing.T, s *Scheduler, key, value string) {
	t.Helper()
	if _, err := s.db.Exec(
		`INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value,
	); err != nil {
		t.Fatalf("set setting %s: %v", key, err)
	}
}

func countDailyRuns(t *testing.T, s *Scheduler, date string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM daily_runs WHERE order_date=?`, date).Scan(&n); err != nil {
		t.Fatalf("count daily_runs: %v", err)
	}
	return n
}

// Aceite da spec: server em UTC, negócio em America/Sao_Paulo (UTC-3),
// daily_at=00:00. Às 02:59Z de 03/jul ainda é 23:59 de 02/jul em SP → a daily
// de 03/jul NÃO roda; às 03:00Z cruza a meia-noite de SP → roda com
// order_date=2026-07-03.
func TestDailyTimezone_AutoDailyCruzaMeiaNoiteDeSP(t *testing.T) {
	s := newTestScheduler(t)
	setSetting(t, s, "daily_timezone", "America/Sao_Paulo")
	// Uma definition qualquer: sem NENHUMA, a daily deliberadamente não marca o dia
	// (ver TestRunDaily_ZeroDefinitionsDoesNotMarkTheDay) e este teste mediria o
	// guard em vez da timezone. Vai pelo STORE, não por s.defs: o autoDailyIfDue
	// passa por dailySync → reloadDefs, que substitui s.defs pelo que está em disco.
	// Com schedule desligada não materializa instance — o que se observa aqui
	// continua sendo só o carimbo em daily_runs.
	if err := s.store.Save(domain.JobDefinition{ID: "tz", Label: "TZ", Team: "T"}); err != nil {
		t.Fatalf("seed definition: %v", err)
	}
	// A diária de 02/jul (o "hoje" de SP às 02:59Z) já rodou — isola o caso.
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO daily_runs(order_date, started_at) VALUES(?, CURRENT_TIMESTAMP)`, "2026-07-02"); err != nil {
		t.Fatalf("seed daily_runs: %v", err)
	}

	s.nowFn = func() time.Time { return time.Date(2026, 7, 3, 2, 59, 0, 0, time.UTC) }
	s.autoDailyIfDue()
	if n := countDailyRuns(t, s, "2026-07-03"); n != 0 {
		t.Fatalf("às 02:59Z (23:59 de 02/jul em SP) a daily de 03/jul NÃO deveria rodar; daily_runs=%d", n)
	}

	s.nowFn = func() time.Time { return time.Date(2026, 7, 3, 3, 0, 0, 0, time.UTC) }
	s.autoDailyIfDue()
	if n := countDailyRuns(t, s, "2026-07-03"); n != 1 {
		t.Fatalf("às 03:00Z (meia-noite de SP) a daily deveria rodar com order_date=2026-07-03; daily_runs=%d", n)
	}
}

// TodayDate é a mesma régua das ordens manuais (POST /api/daily/run e afins):
// 01:00Z de 03/jul = 22:00 de 02/jul em SP → "hoje" de negócio é 2026-07-02.
func TestDailyTimezone_TodayDateNaTimezoneDeNegocio(t *testing.T) {
	s := newTestScheduler(t)
	setSetting(t, s, "daily_timezone", "America/Sao_Paulo")
	s.nowFn = func() time.Time { return time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC) }
	if got := s.TodayDate(); got != "2026-07-02" {
		t.Fatalf("TodayDate esperava 2026-07-02 (dia de SP), veio %s", got)
	}
}

// Nome IANA inválido não pode parar a daily: loga e cai no relógio local do
// server (comportamento clássico). Corrigir o setting recarrega o cache.
func TestDailyTimezone_InvalidoCaiNoLocal(t *testing.T) {
	s := newTestScheduler(t)
	setSetting(t, s, "daily_timezone", "Nao/Existe")
	name, loc := s.DailyTimezone()
	if name != "Nao/Existe" || loc != time.Local {
		t.Fatalf("tz inválida deveria reportar o nome configurado com loc local, veio %q %v", name, loc)
	}
	// Corrige o setting → o cache por nome recarrega pro certo.
	setSetting(t, s, "daily_timezone", "America/Sao_Paulo")
	if _, loc = s.DailyTimezone(); loc.String() != "America/Sao_Paulo" {
		t.Fatalf("corrigir o setting deveria recarregar a location, veio %v", loc)
	}
	// Vazio = local (default do produto).
	setSetting(t, s, "daily_timezone", "")
	if name, loc = s.DailyTimezone(); name != "" || loc != time.Local {
		t.Fatalf("setting vazio deveria ser local, veio %q %v", name, loc)
	}
}
