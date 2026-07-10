package scheduler

// ADV-3 Statistics — retrato histórico por definition (aba Stats do drawer).

import (
	"fmt"
	"testing"
	"time"
)

// seedRunStatus insere uma execução terminada com status e duração dados.
func seedRunStatus(t *testing.T, s *Scheduler, defID string, day int, dur time.Duration, status string) {
	t.Helper()
	start := time.Date(2026, 6, day, 6, 0, 0, 0, time.UTC)
	if _, err := s.db.Exec(
		`INSERT INTO instances(id, definition_id, order_date, status, scheduled_at, started_at, finished_at)
		 VALUES(?,?,?,?,?,?,?)`,
		fmt.Sprintf("%s-%s-%02d", defID, status, day), defID, fmt.Sprintf("2026-06-%02d", day),
		status, start, start, start.Add(dur),
	); err != nil {
		t.Fatal(err)
	}
}

func TestJobStats(t *testing.T) {
	s := newTestScheduler(t)
	// 4 OK (5/10/15/20min) + 1 NOTOK — o NOTOK é o mais RECENTE (dia 25)
	for i, min := range []int{5, 10, 15, 20} {
		seedRunStatus(t, s, "carga", i+1, time.Duration(min)*time.Minute, "OK")
	}
	seedRunStatus(t, s, "carga", 25, 1*time.Minute, "NOTOK")

	st := s.JobStats("carga")
	if st.Runs != 5 || st.OK != 4 || st.NotOK != 1 {
		t.Fatalf("contagem errada: %+v", st)
	}
	if st.SuccessRate != 0.8 {
		t.Fatalf("successRate esperado 0.8, veio %f", st.SuccessRate)
	}
	// distribuição SÓ sobre os OK (NOTOK de 1min não pode puxar o min)
	if st.MinMs != 5*60_000 || st.MaxMs != 20*60_000 {
		t.Fatalf("min/max sobre OK errados: %+v", st)
	}
	if st.AvgMs != 12*60_000+30_000 { // (5+10+15+20)/4 = 12.5min
		t.Fatalf("avg esperado 12.5min, veio %d", st.AvgMs)
	}
	if st.P50Ms != 10*60_000 { // índice (4-1)*50/100 = 1 → 10min (mesma regra do D-4)
		t.Fatalf("p50 esperado 10min, veio %d", st.P50Ms)
	}
	// última execução = o NOTOK do dia 25
	if st.LastStatus != "NOTOK" || st.LastDurationMs != 1*60_000 || st.LastFinishedAt == nil {
		t.Fatalf("última execução errada: %+v", st)
	}
}

func TestJobStats_Empty(t *testing.T) {
	s := newTestScheduler(t)
	st := s.JobStats("nunca-rodou")
	if st.Runs != 0 || st.SuccessRate != 0 || st.LastStatus != "" {
		t.Fatalf("def sem histórico deveria zerar tudo: %+v", st)
	}
}
