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
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

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

// resolveEdgeDate — resolve o dateRef de uma aresta upstream contra o ODAT do
// consumidor. anyDate=true (stat) = sem filtro de data.
func (s *Scheduler) resolveEdgeDate(consumerOdate string, ref domain.DateRef) (date string, anyDate bool) {
	switch ref {
	case domain.DateRefStat:
		return "", true
	case domain.DateRefPrev:
		return s.prevDaily(consumerOdate), false
	default: // "" | odat
		return consumerOdate, false
	}
}

// splitCondRef — "NOME@prev" → ("NOME", "prev"). Sem sufixo (ou sufixo
// desconhecido, tratado como parte do nome) = odat. Case-insensitive no ref.
func splitCondRef(name string) (base string, ref domain.DateRef) {
	if i := strings.LastIndex(name, "@"); i > 0 {
		switch strings.ToLower(name[i+1:]) {
		case "odat":
			return name[:i], domain.DateRefOdat
		case "prev":
			return name[:i], domain.DateRefPrev
		case "stat":
			return name[:i], domain.DateRefStat
		}
	}
	return name, domain.DateRefOdat
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

// instIndex — instances indexadas por (definition, ODAT) + agregado por
// definition. Com o carry-over o dia ativo mistura origens (a fresca de hoje
// ao lado da carregada do dia 14); o gate de dependência consulta a cópia do
// pai da diária resolvida pela aresta (odat/prev) — só stat aceita qualquer.
// Dentro da mesma chave vence a "mais determinante" pelo statusRank.
type instIndex struct {
	byDefDate map[string]instRow
	byDef     map[string]instRow
}

func defDateKey(defID, odate string) string { return defID + "\x00" + odate }

func buildInstIndex(insts []instRow) instIndex {
	ix := instIndex{byDefDate: map[string]instRow{}, byDef: map[string]instRow{}}
	for _, r := range insts {
		ix.add(r)
	}
	return ix
}

func (ix instIndex) add(r instRow) {
	k := defDateKey(r.DefID, r.Odate())
	if prev, ok := ix.byDefDate[k]; !ok || statusRank(r.Status) > statusRank(prev.Status) {
		ix.byDefDate[k] = r
	}
	if prev, ok := ix.byDef[r.DefID]; !ok || statusRank(r.Status) > statusRank(prev.Status) {
		ix.byDef[r.DefID] = r
	}
}

// lookup — a cópia do upstream que casa a data resolvida da aresta
// (anyDate/stat = qualquer diária, melhor rank).
func (ix instIndex) lookup(defID, date string, anyDate bool) (instRow, bool) {
	if anyDate {
		r, ok := ix.byDef[defID]
		return r, ok
	}
	r, ok := ix.byDefDate[defDateKey(defID, date)]
	return r, ok
}
