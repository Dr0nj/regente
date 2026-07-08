package scheduler

// D-4 — performance forecasting: percentis, tendência e ETA por histórico.

import (
	"fmt"
	"testing"
	"time"
)

func TestPercentileAndSlope(t *testing.T) {
	durs := []int64{100, 200, 300, 400, 500}
	if p := percentile(durs, 50); p != 300 {
		t.Fatalf("p50 de 100..500 deveria ser 300, veio %d", p)
	}
	if p := percentile(durs, 90); p != 400 { // índice (5-1)*90/100 = 3
		t.Fatalf("p90 esperava 400, veio %d", p)
	}
	if s := slope([]int64{100, 200, 300}); s != 100 {
		t.Fatalf("série +100/run deveria ter slope 100, veio %f", s)
	}
	if s := slope([]int64{300, 300, 300}); s != 0 {
		t.Fatalf("série constante tem slope 0, veio %f", s)
	}
}

// seedRun insere uma execução OK terminada com a duração dada.
func seedRun(t *testing.T, s *Scheduler, defID string, day int, dur time.Duration) {
	t.Helper()
	start := time.Date(2026, 6, day, 6, 0, 0, 0, time.UTC)
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, started_at, finished_at)
		 VALUES(?,?,?,?,?,?,?)`,
		fmt.Sprintf("%s-%02d", defID, day), defID, fmt.Sprintf("2026-06-%02d", day),
		"OK", start, start, start.Add(dur),
	); err != nil {
		t.Fatal(err)
	}
}

// REGRA: um job ficando mais lento a cada dia é detectado (slower=true) e a
// previsão da próxima execução projeta a tendência ACIMA do p50.
func TestPerfForecast_DetectsSlowdown(t *testing.T) {
	s := newTestScheduler(t)
	for i := 0; i < 10; i++ {
		seedRun(t, s, "lento", i+1, time.Duration(60+i*30)*time.Second) // 60s→330s
	}
	pf := s.PerfForecast("lento")
	if len(pf.Samples) != 10 {
		t.Fatalf("esperava 10 amostras, veio %d", len(pf.Samples))
	}
	if !pf.Slower {
		t.Fatalf("série crescendo 30s/run deveria acusar slower (trend=%f p50=%d)", pf.TrendMsPerRun, pf.P50Ms)
	}
	if pf.NextMs <= pf.P50Ms {
		t.Fatalf("previsão deveria projetar acima do p50 (next=%d p50=%d)", pf.NextMs, pf.P50Ms)
	}
	// série cronológica: primeira amostra é a mais antiga (60s)
	if pf.Samples[0].DurationMs != 60000 {
		t.Fatalf("amostras deveriam ser cronológicas, primeira=%d", pf.Samples[0].DurationMs)
	}
}

// REGRA: com uma instance RUNNING agora, o forecast projeta ETA (nunca negativo).
func TestPerfForecast_EtaForRunning(t *testing.T) {
	s := newTestScheduler(t)
	for i := 0; i < 5; i++ {
		seedRun(t, s, "job", i+1, 10*time.Minute)
	}
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, started_at)
		 VALUES('job-run','job',?, 'RUNNING', CURRENT_TIMESTAMP, ?)`,
		time.Now().Format("2006-01-02"), time.Now().Add(-2*time.Minute),
	); err != nil {
		t.Fatal(err)
	}
	pf := s.PerfForecast("job")
	if pf.EtaMs == nil {
		t.Fatal("com RUNNING em curso deveria haver ETA")
	}
	// p50=10min, decorrido=2min → ETA ~8min (folga de 2min p/ clock/driver)
	if *pf.EtaMs < 6*60*1000 || *pf.EtaMs > 10*60*1000 {
		t.Fatalf("ETA esperado ~8min, veio %dms", *pf.EtaMs)
	}
}

// REGRA: DayDurations agrega p50 POR definition na janela — alimenta as barras
// previstas da Timeline em uma query.
func TestDayDurations(t *testing.T) {
	s := newTestScheduler(t)
	for i := 0; i < 4; i++ {
		seedRun(t, s, "a", i+1, 5*time.Minute)
		seedRun(t, s, "b", i+1, 20*time.Minute)
	}
	p50 := s.DayDurations("2026-06-10", 14)
	if p50["a"] != 5*60*1000 || p50["b"] != 20*60*1000 {
		t.Fatalf("p50 por def errado: %+v", p50)
	}
}
