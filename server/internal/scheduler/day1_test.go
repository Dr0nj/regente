package scheduler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// ---------------------------------------------------------------------------
// DAY-1 — o dia de NEGÓCIO vira no daily_at, não à meia-noite.
//
// O bug de produção (regentehub.com, 2026-07-30 22h): o operador força um job, o
// server devolve 200 e a ordem SOME da tela. O server carimbava a ordem com a
// data-CALENDÁRIO (que já tinha virado) enquanto a diária corrente ainda era a
// de ontem — a ordem nascia num dia que a daily nunca materializou.
//
// A régua correta é a que o produto já promete no ODAT: entre `daily_at` de D e
// `daily_at` de D+1, o dia é D. Alinhar fuso só encolhe a janela ruim; o que
// fecha é ancorar a data em daily_at.
//
// Toda a bateria roda com o server em UTC e o negócio em America/Sao_Paulo —
// justamente o par que ninguém tinha testado.
// ---------------------------------------------------------------------------

// spDay1 prepara o cenário canônico: negócio em SP (UTC-3), daily às 15:00.
func spDay1(t *testing.T) *Scheduler {
	t.Helper()
	s := newTestScheduler(t)
	setSetting(t, s, "daily_timezone", "America/Sao_Paulo")
	setSetting(t, s, "daily_at", "15:00")
	return s
}

// atSP crava o relógio do processo (UTC) num horário de PAREDE de São Paulo.
func atSP(t *testing.T, s *Scheduler, wall string) {
	t.Helper()
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	tt, err := time.ParseInLocation("2006-01-02 15:04", wall, loc)
	if err != nil {
		t.Fatalf("hora %q: %v", wall, err)
	}
	s.nowFn = func() time.Time { return tt.UTC() }
}

// TestDay1_DataDeNegocioViraNoDailyAt — o aceite da spec, ponto a ponto. Com
// daily_at=15:00 o dia 30 vai das 15:00 do dia 30 às 14:59 do dia 31.
func TestDay1_DataDeNegocioViraNoDailyAt(t *testing.T) {
	s := spDay1(t)
	cases := []struct{ wall, want string }{
		{"2026-07-30 15:00", "2026-07-30"}, // a virada em si já é o dia novo
		{"2026-07-30 23:50", "2026-07-30"}, // ordem antes da meia-noite…
		{"2026-07-31 00:10", "2026-07-30"}, // …e ordem depois: MESMO dia de negócio
		{"2026-07-31 06:00", "2026-07-30"}, // manhã ainda é ontem — é o dia que a daily materializou
		{"2026-07-31 14:59", "2026-07-30"}, // até um minuto antes do daily_at
		{"2026-07-31 15:00", "2026-07-31"}, // aqui, e só aqui, o dia vira
		{"2026-07-31 15:01", "2026-07-31"},
	}
	for _, c := range cases {
		atSP(t, s, c.wall)
		if got := s.TodayDate(); got != c.want {
			t.Fatalf("%s (SP) deveria ser o dia de negócio %s, veio %s", c.wall, c.want, got)
		}
	}
}

// TestDay1_MeiaNoiteMantemDataDeCalendario — o default do produto (daily_at
// 00:00) NÃO muda de comportamento: data de negócio == data-calendário na
// timezone da daily. Instalação existente não sente a mudança.
func TestDay1_MeiaNoiteMantemDataDeCalendario(t *testing.T) {
	s := newTestScheduler(t)
	setSetting(t, s, "daily_timezone", "America/Sao_Paulo")
	for _, wall := range []string{"2026-07-31 00:00", "2026-07-31 03:20", "2026-07-31 23:59"} {
		atSP(t, s, wall)
		if got := s.TodayDate(); got != "2026-07-31" {
			t.Fatalf("com daily_at=00:00, %s (SP) deveria ser 2026-07-31, veio %s", wall, got)
		}
	}
}

// TestDay1_AutoDailyNaoDisparaNaMeiaNoite — a virada da daily acompanha a data
// de negócio: passar da meia-noite não materializa dia nenhum; às 15:00 sim.
func TestDay1_AutoDailyNaoDisparaNaMeiaNoite(t *testing.T) {
	s := spDay1(t)
	// Uma definition qualquer: sem NENHUMA a daily deliberadamente não marca o dia
	// e o teste mediria o guard em vez da virada. Vai pelo STORE porque o
	// autoDailyIfDue passa por dailySync → reloadDefs.
	if err := s.store.Save(domain.JobDefinition{ID: "d1", Label: "D1", Team: "T"}); err != nil {
		t.Fatalf("seed definition: %v", err)
	}
	// A diária do dia 30 já rodou (às 15:00 dele) — é o estado de regime.
	if _, err := s.db.Exec(
		`INSERT OR REPLACE INTO daily_runs(order_date, started_at) VALUES(?, CURRENT_TIMESTAMP)`, "2026-07-30",
	); err != nil {
		t.Fatalf("seed daily_runs: %v", err)
	}

	for _, wall := range []string{"2026-07-30 23:50", "2026-07-31 00:10", "2026-07-31 14:59"} {
		atSP(t, s, wall)
		s.autoDailyIfDue()
		if n := countDailyRuns(t, s, "2026-07-31"); n != 0 {
			t.Fatalf("às %s (SP) o dia de negócio ainda é 30 — a diária de 31 NÃO podia rodar", wall)
		}
	}

	atSP(t, s, "2026-07-31 15:00")
	s.autoDailyIfDue()
	if n := countDailyRuns(t, s, "2026-07-31"); n != 1 {
		t.Fatalf("às 15:00 (daily_at) a diária de 31 deveria materializar; daily_runs=%d", n)
	}
}

// TestDay1_OrdemManualCaiNaDiariaCorrente — a invariante que o bug violava: a
// data que o server carimba numa ordem criada AGORA é sempre a da diária que
// está no ar. Se divergir, a ordem existe no banco e não aparece no board.
func TestDay1_OrdemManualCaiNaDiariaCorrente(t *testing.T) {
	s := spDay1(t)
	if _, err := s.db.Exec(
		`INSERT OR REPLACE INTO daily_runs(order_date, started_at) VALUES(?, CURRENT_TIMESTAMP)`, "2026-07-30",
	); err != nil {
		t.Fatalf("seed daily_runs: %v", err)
	}
	// 22h de SP = 01h UTC do dia seguinte: o cenário EXATO do report.
	atSP(t, s, "2026-07-30 22:00")
	if got := s.TodayDate(); got != "2026-07-30" {
		t.Fatalf("às 22:00 de 30 a ordem deveria nascer em 2026-07-30, veio %s", got)
	}
	// E do outro lado da meia-noite continua na mesma diária.
	atSP(t, s, "2026-07-31 01:00")
	if got := s.TodayDate(); got != "2026-07-30" {
		t.Fatalf("às 01:00 de 31 (diária corrente = 30) a ordem deveria nascer em 2026-07-30, veio %s", got)
	}

	var last string
	if err := s.db.QueryRow(`SELECT MAX(order_date) FROM daily_runs`).Scan(&last); err != nil {
		t.Fatalf("última diária: %v", err)
	}
	if s.TodayDate() != last {
		t.Fatalf("a data da ordem (%s) tem que ser a da diária corrente (%s)", s.TodayDate(), last)
	}
}

// seedNotOKAt — NOTOK numa diária, com o término num INSTANTE dado (UTC).
func seedNotOKAt(t *testing.T, s *Scheduler, id, orderDate string, finished time.Time) {
	t.Helper()
	def := domain.JobDefinition{ID: id, JobType: "COMMAND", Schedule: domain.Schedule{Enabled: true}}
	snap, _ := json.Marshal(def)
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, definition_snapshot, attempts, started_at, finished_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		id, def.ID, orderDate, string(domain.StatusNotOK), time.Now(), string(snap), 1,
		finished.Add(-time.Hour), finished,
	); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

// TestDay1_CarryContaIdadeEmDiasDeNegocio — o carry-over NÃO pode ganhar (nem
// perder) vida com a mudança. A idade compara LABELS de order_date, que são
// datas de negócio; um timestamp cru tem que ser lido pela MESMA régua.
//
// Com daily_at=15:00, um NOTOK que falhou às 02:00 do dia 31 falhou no dia de
// NEGÓCIO 30 — mesma vida de um que falhou às 18:00 do dia 30. Datá-lo como 31
// (a régua da meia-noite) dava a ele uma diária extra de sobrevida.
func TestDay1_CarryContaIdadeEmDiasDeNegocio(t *testing.T) {
	s := spDay1(t)
	utc := func(wall string) time.Time {
		t.Helper()
		loc, _ := time.LoadLocation("America/Sao_Paulo")
		tt, err := time.ParseInLocation("2006-01-02 15:04", wall, loc)
		if err != nil {
			t.Fatalf("hora %q: %v", wall, err)
		}
		return tt.UTC()
	}
	// Falhou às 02:00 de 31 = dia de negócio 30 → 2 diárias até 01/ago → expira
	// (NOTOK sem keepActive tem baseline de 1 diária).
	seedNotOKAt(t, s, "madrugada", "2026-07-30", utc("2026-07-31 02:00"))
	// Falhou às 16:00 de 31 = dia de negócio 31 → 1 diária → sobrevive.
	seedNotOKAt(t, s, "tarde", "2026-07-30", utc("2026-07-31 16:00"))

	s.RunDaily("2026-08-01")

	if od, _, _ := carriedState(t, s, "madrugada"); od != "2026-07-30" {
		t.Fatalf("falha às 02:00 de 31 é do dia de negócio 30 (2 diárias) e NÃO podia carregar; foi pra %s", od)
	}
	if od, _, _ := carriedState(t, s, "tarde"); od != "2026-08-01" {
		t.Fatalf("falha às 16:00 de 31 é do dia de negócio 31 (1 diária) e deveria carregar; ficou em %s", od)
	}
}

// TestDay1_AddDaysAndaEmDiarias — a aritmética de retenção/forecast é sobre o
// LABEL do dia. Somar dias a um instante e formatar depois volta a datar pela
// meia-noite, que é exatamente o bug.
func TestDay1_AddDaysAndaEmDiarias(t *testing.T) {
	if got := AddDays("2026-07-31", 1); got != "2026-08-01" {
		t.Fatalf("D+1 de 2026-07-31 deveria ser 2026-08-01, veio %s", got)
	}
	if got := AddDays("2026-03-01", -1); got != "2026-02-28" {
		t.Fatalf("D-1 de 2026-03-01 deveria ser 2026-02-28, veio %s", got)
	}
	if got := AddDays("nao-e-data", 1); got != "nao-e-data" {
		t.Fatalf("data malformada deveria voltar intacta, veio %s", got)
	}
}
