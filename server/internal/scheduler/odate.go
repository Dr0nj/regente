// Package scheduler — ODAT: a data de ORIGEM de uma instance (Control-M ODATE).
//
// O carry-over da virada da daily AVANÇA instances.order_date (o "dia ativo",
// que o tick/API/RBAC filtram) preservando a origem em carried_from. O ODAT é
// a data em que a ordem entrou em schedule pela PRIMEIRA vez:
//
//	ODAT = COALESCE(NULLIF(carried_from,''), order_date)
//
// TODO O ESCOPO DE DATA de dependências e conditions usa ODAT, nunca o
// order_date avançado — um job carregado do dia 14 continua sendo "do dia 14"
// para eventos, conditions e para o operador (report do usuário, 2026-07-16:
// jobs carregados disputavam os eventos dos jobs FRESCOS do dia).
package scheduler

import (
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// ForceModeOrder — instances.force_mode do "Order Force" (Design): ordem NOVA
// fora do agendamento que RESPEITA os gates de runtime (condições, agente,
// recursos, confirm). "" = "Run Now" clássico (bypass).
const ForceModeOrder = "order"

// odateExpr — expressão SQL do ODAT de uma linha de instances.
const odateExpr = `COALESCE(NULLIF(carried_from,''), order_date)`

// Odate — data de origem da ordem (ver cabeçalho do arquivo).
func (r instRow) Odate() string {
	if r.CarriedFrom != "" {
		return r.CarriedFrom
	}
	return r.OrderDate
}

// instanceOdate — ODAT (origem) de uma instance pelo id; `fallback` cobre a
// instance sumida no meio (best-effort dos hooks de actions).
func (s *Scheduler) instanceOdate(id, fallback string) string {
	var odate string
	if err := s.db.QueryRow(`SELECT `+odateExpr+` FROM instances WHERE id=?`, id).Scan(&odate); err != nil || odate == "" {
		return fallback
	}
	return odate
}

// prevDaily — a diária ANTERIOR a `odate`: o último New Day registrado antes
// dela (daily_runs). Cobre lacunas (server desligado num dia = a "anterior" é
// a última que RODOU, como no Control-M). Sem registro, cai em odate-1 dia.
func (s *Scheduler) prevDaily(odate string) string {
	var prev string
	if err := s.db.QueryRow(
		`SELECT COALESCE(MAX(order_date),'') FROM daily_runs WHERE order_date < ?`, odate,
	).Scan(&prev); err == nil && prev != "" {
		return prev
	}
	if t, err := time.Parse("2006-01-02", odate); err == nil {
		return t.AddDate(0, 0, -1).Format("2006-01-02")
	}
	return odate
}

// splitCondRef — "NOME@prev" → ("NOME", "prev"). Regra canônica no domain
// (SplitCondRef) — o mesmo parser que a normalização de defs usa.
func splitCondRef(name string) (base string, ref domain.DateRef) {
	return domain.SplitCondRef(name)
}

// daysBetween — dias-calendário entre duas datas YYYY-MM-DD (b - a).
// Datas malformadas contam 0 (comportamento conservador: não mata ninguém).
func daysBetween(a, b string) int {
	ta, errA := time.Parse("2006-01-02", a)
	tb, errB := time.Parse("2006-01-02", b)
	if errA != nil || errB != nil {
		return 0
	}
	return int(tb.Sub(ta).Hours() / 24)
}

