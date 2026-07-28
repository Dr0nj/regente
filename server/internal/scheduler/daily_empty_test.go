package scheduler

import (
	"testing"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// Regressão da PRIMEIRA instalação real em VPS (2026-07-28): a daily correu
// antes do GitOps terminar de conectar, o workspace ainda estava vazio, ela
// materializou 0 instances e CARIMBOU daily_runs. Depois disso o dia estava
// "processado": nem com o clone pronto e as definitions carregadas o board
// voltava a ser materializado — só com um POST /api/daily/run manual.
//
// A invariante: dia sem NENHUMA definition não é um dia processado.
func TestRunDaily_ZeroDefinitionsDoesNotMarkTheDay(t *testing.T) {
	s := newTestScheduler(t) // FileStore sobre TempDir vazio → zero definitions
	const date = "2026-07-28"

	if n := s.RunDaily(date); n != 0 {
		t.Fatalf("sem definitions a daily não deveria criar nada, criou %d", n)
	}

	var marked int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM daily_runs WHERE order_date=?`, date).Scan(&marked); err != nil {
		t.Fatalf("count daily_runs: %v", err)
	}
	if marked != 0 {
		t.Fatalf("o dia %s NÃO pode ficar marcado como processado com zero definitions (daily_runs=%d): "+
			"é isso que impede a materialização quando o GitOps conecta minutos depois", date, marked)
	}
}

// A guarda acima tem de valer SÓ para o conjunto vazio. Com definitions
// carregadas o dia é marcado normalmente — inclusive quando todas ficam de fora
// pelo gating (schedule.enabled=false), que é uma daily legítima de 0 instances.
func TestRunDaily_WithDefinitionsMarksTheDayEvenWhenNothingIsScheduled(t *testing.T) {
	s := newTestScheduler(t)
	const date = "2026-07-29"

	s.mu.Lock()
	s.defs = []domain.JobDefinition{{
		ID: "desligado", Label: "DESLIGADO", Team: "T",
		Schedule: domain.Schedule{Enabled: false}, // gating exclui, mas a def EXISTE
	}}
	s.mu.Unlock()

	if n := s.RunDaily(date); n != 0 {
		t.Fatalf("def com schedule.enabled=false não deveria materializar, criou %d", n)
	}

	var marked int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM daily_runs WHERE order_date=?`, date).Scan(&marked); err != nil {
		t.Fatalf("count daily_runs: %v", err)
	}
	if marked != 1 {
		t.Fatalf("com definitions carregadas o dia %s tem de ser marcado como processado (daily_runs=%d): "+
			"0 instances por gating é uma daily legítima, não configuração pela metade", date, marked)
	}
}
