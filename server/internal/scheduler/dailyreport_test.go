package scheduler

// E5 — relatório/SLO da daily: agregado exato por status, lateStart pelo
// relógio de negócio e envio idempotente (claim em report_sent_at).

import (
	"fmt"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

func seedReportInstance(t *testing.T, s *Scheduler, id, date, status, team, carriedFrom string, exitCode int) {
	t.Helper()
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, team, carried_from, exit_code, finished_at)
		 VALUES(?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`,
		id, "def-"+id, date, status, time.Now(), team, carriedFrom, exitCode,
	); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func TestDailyReport_AgregadoExato(t *testing.T) {
	s := newTestScheduler(t)
	const date = "2026-07-07"
	// 2 OK · 2 NOTOK (1 carregada) · 1 WAITING · 1 RUNNING · 1 HELD · 1 CANCELLED
	seedReportInstance(t, s, "a", date, string(domain.StatusOK), "FIN", "", 0)
	seedReportInstance(t, s, "b", date, string(domain.StatusOK), "FIN", "", 0)
	seedReportInstance(t, s, "c", date, string(domain.StatusNotOK), "FIN", "", 3)
	seedReportInstance(t, s, "d", date, string(domain.StatusNotOK), "RISCO", "2026-07-06", 9)
	seedReportInstance(t, s, "e", date, string(domain.StatusWaiting), "FIN", "", 0)
	seedReportInstance(t, s, "f", date, string(domain.StatusRunning), "FIN", "", 0)
	seedReportInstance(t, s, "g", date, string(domain.StatusHeld), "FIN", "", 0)
	seedReportInstance(t, s, "h", date, string(domain.StatusCancelled), "FIN", "", 0)
	// Ruído de outra diária não conta.
	seedReportInstance(t, s, "z", "2026-07-06", string(domain.StatusNotOK), "FIN", "", 1)

	rep, err := s.BuildDailyReport(date)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	c := rep.Counts
	if c.Ordered != 8 || c.OK != 2 || c.NotOK != 2 || c.Waiting != 1 || c.Running != 1 ||
		c.Held != 1 || c.Cancelled != 1 || c.Carried != 1 {
		t.Fatalf("counts errados: %+v", c)
	}
	if rep.Closed {
		t.Fatal("com WAITING/RUNNING abertos o dia NÃO está fechado")
	}
	if len(rep.Failures) != 2 {
		t.Fatalf("esperava 2 failures detalhadas, veio %d", len(rep.Failures))
	}
	for _, f := range rep.Failures {
		if f.DefID == "def-d" && (f.Team != "RISCO" || f.ExitCode != 9) {
			t.Fatalf("failure def-d deveria trazer team/exitCode, veio %+v", f)
		}
	}

	if _, err := s.BuildDailyReport("07/07/2026"); err == nil {
		t.Fatal("data fora de YYYY-MM-DD deveria dar erro")
	}
}

// lateStart usa o relógio de NEGÓCIO (E1): daily_at 01:00 em SP; started_at
// 04:10Z = 01:10 SP → dentro dos 5 min? não — 10 min = atrasada. 04:03Z = ok.
func TestDailyReport_LateStartNaTimezone(t *testing.T) {
	s := newTestScheduler(t)
	setSetting(t, s, "daily_timezone", "America/Sao_Paulo")
	setSetting(t, s, "daily_at", "01:00")
	const date = "2026-07-07"

	set := func(startedUTC time.Time) {
		if _, err := s.db.Exec(`INSERT OR REPLACE INTO daily_runs(order_date, started_at) VALUES(?,?)`, date, startedUTC); err != nil {
			t.Fatalf("seed daily_runs: %v", err)
		}
	}
	set(time.Date(2026, 7, 7, 4, 3, 0, 0, time.UTC)) // 01:03 SP — no prazo
	rep, err := s.BuildDailyReport(date)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if rep.LateStart {
		t.Fatal("01:03 SP com alvo 01:00+5min NÃO é atraso")
	}
	set(time.Date(2026, 7, 7, 4, 10, 0, 0, time.UTC)) // 01:10 SP — atrasada
	rep, _ = s.BuildDailyReport(date)
	if !rep.LateStart {
		t.Fatal("01:10 SP com alvo 01:00+5min É atraso")
	}
}

// Envio idempotente: daily fechada + canais configurados → envia 1× (claim em
// report_sent_at); re-checar não envia de novo; sem canais nem marca.
func TestDailyReport_EnvioIdempotente(t *testing.T) {
	s := newTestScheduler(t)
	date := s.TodayDate()
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO daily_runs(order_date, started_at) VALUES(?, CURRENT_TIMESTAMP)`, date); err != nil {
		t.Fatalf("seed daily_runs: %v", err)
	}
	seedReportInstance(t, s, "ok-1", date, string(domain.StatusOK), "FIN", "", 0)

	sentAt := func() any {
		var v any
		_ = s.db.QueryRow(`SELECT report_sent_at FROM daily_runs WHERE order_date=?`, date).Scan(&v)
		return v
	}

	// Sem canais configurados → nunca envia nem marca.
	s.maybeSendDailyReport()
	if sentAt() != nil {
		t.Fatal("sem daily_report_channels não deveria marcar report_sent_at")
	}

	setSetting(t, s, "daily_report_channels", "webhook")
	s.mu.Lock()
	s.lastReportCheck = time.Time{} // zera o throttle de 1 min
	s.mu.Unlock()
	s.maybeSendDailyReport()
	first := sentAt()
	if first == nil {
		t.Fatal("daily fechada + canais configurados deveria enviar e marcar report_sent_at")
	}

	// Segunda checagem: já enviado → não re-envia (marca não muda).
	s.mu.Lock()
	s.lastReportCheck = time.Time{}
	s.mu.Unlock()
	s.maybeSendDailyReport()
	if second := sentAt(); fmt.Sprint(second) != fmt.Sprint(first) {
		t.Fatalf("re-checar não pode re-enviar: %v != %v", second, first)
	}
	if rep, _ := s.BuildDailyReport(date); !rep.ReportSent {
		t.Fatal("BuildDailyReport deveria refletir reportSent=true")
	}
}

// Daily aberta (WAITING) sem daily_report_at → não envia; com daily_report_at
// já vencido → envia mesmo aberta (fallback por horário).
func TestDailyReport_FallbackPorHorario(t *testing.T) {
	s := newTestScheduler(t)
	date := s.TodayDate()
	if _, err := s.db.Exec(`INSERT OR REPLACE INTO daily_runs(order_date, started_at) VALUES(?, CURRENT_TIMESTAMP)`, date); err != nil {
		t.Fatalf("seed daily_runs: %v", err)
	}
	seedReportInstance(t, s, "w-1", date, string(domain.StatusWaiting), "FIN", "", 0)
	setSetting(t, s, "daily_report_channels", "webhook")

	sent := func() any {
		var v any
		_ = s.db.QueryRow(`SELECT report_sent_at FROM daily_runs WHERE order_date=?`, date).Scan(&v)
		return v
	}
	s.maybeSendDailyReport()
	if sent() != nil {
		t.Fatal("daily aberta sem daily_report_at não deveria enviar")
	}

	setSetting(t, s, "daily_report_at", "00:00") // já passou (qualquer hora do dia)
	s.mu.Lock()
	s.lastReportCheck = time.Time{}
	s.mu.Unlock()
	s.maybeSendDailyReport()
	if sent() == nil {
		t.Fatal("daily_report_at vencido deveria enviar mesmo com o dia aberto")
	}
}
