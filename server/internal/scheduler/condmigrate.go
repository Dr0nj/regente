// Package scheduler — migração one-time da UNIFICAÇÃO de dependências
// (2026-07-17): tudo virou condição num pool único (domain/conditions.go).
//
// O que precisa de backfill: instances de dias ATIVOS ordenadas ANTES da
// unificação carregam `upstream` legado no snapshot. O gate delas passa a ser
// a condição sintetizada A-TO-B (ExpandSnapshotConditions) — mas se o PAI já
// tinha terminado OK antes do upgrade, ninguém vai criar essa condição
// retroativamente (no modelo velho isso era um dep_event, que aposentamos).
// Este backfill semeia no pool a condição de cada par (pai OK → filho ainda
// não consumido), best-effort, exatamente uma vez por banco (meta_flags).
package scheduler

import (
	"encoding/json"
	"log"

	"github.com/Dr0nj/regente-server/internal/domain"
)

const condsUnifyFlag = "conditions-unify-backfill"

// MigrateConditionsUnify — roda o backfill se ainda não rodou neste banco.
// Chamar no boot, depois das migrações de schema (meta_flags precisa existir).
func (s *Scheduler) MigrateConditionsUnify() {
	if s.conditions == nil {
		return
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM meta_flags WHERE name=?`, condsUnifyFlag).Scan(&n); err != nil {
		log.Printf("[conditions] backfill: meta_flags indisponível: %v", err)
		return
	}
	if n > 0 {
		return // já rodou
	}
	seeded := 0
	rows, err := s.db.Query(
		`SELECT id, definition_id, ` + odateExpr + `, COALESCE(definition_snapshot,'')
		 FROM instances
		 WHERE status IN ('WAITING','HELD') AND definition_snapshot LIKE '%"upstream"%'`,
	)
	if err == nil {
		type consumer struct{ id, defID, odate, snap string }
		var consumers []consumer
		for rows.Next() {
			var c consumer
			if rows.Scan(&c.id, &c.defID, &c.odate, &c.snap) == nil {
				consumers = append(consumers, c)
			}
		}
		rows.Close()
		for _, c := range consumers {
			var def domain.JobDefinition
			if json.Unmarshal([]byte(c.snap), &def) != nil {
				continue
			}
			// Consumidor da MESMA origem já terminou OK? Então no modelo velho o
			// evento foi CONSUMIDO — não semear (rerun tem que esperar término novo).
			var consumed int
			_ = s.db.QueryRow(
				`SELECT COUNT(*) FROM instances WHERE definition_id=? AND status='OK' AND `+odateExpr+`=?`,
				c.defID, c.odate,
			).Scan(&consumed)
			if consumed > 0 {
				continue
			}
			for _, u := range def.Upstream {
				if u.From == "" || u.Condition == domain.CondAlways || u.Condition == domain.CondOnFailure {
					continue // sem gate de pool / só-falha: nada a semear de um OK
				}
				wantDate, anyDate := s.resolveEdgeDate(c.odate, u.DateRef)
				q := `SELECT COUNT(*) FROM instances WHERE definition_id=? AND status='OK'`
				args := []any{u.From}
				if !anyDate {
					q += ` AND ` + odateExpr + `=?`
					args = append(args, wantDate)
				}
				var parentOK int
				_ = s.db.QueryRow(q, args...).Scan(&parentOK)
				if parentOK == 0 {
					continue
				}
				name := domain.LinkCondName(u.From, def.ID)
				scope := wantDate
				if anyDate { // aresta @stat → a condição que o IN @stat enxerga é a permanente
					scope = ""
				}
				if err := s.conditions.Set(name, scope, "migration"); err == nil {
					seeded++
				}
			}
		}
	}
	if _, err := s.db.Exec(`INSERT INTO meta_flags(name) VALUES(?)`, condsUnifyFlag); err != nil {
		log.Printf("[conditions] backfill: falha ao marcar flag: %v", err)
		return
	}
	if seeded > 0 {
		log.Printf("[conditions] unificação: %d condição(ões) semeada(s) no pool a partir de pais já OK", seeded)
	}
}

// resolveEdgeDate — resolve o dateRef de uma aresta upstream LEGADA contra o
// ODAT do consumidor (usado só pelo backfill acima). anyDate=true (stat) = sem
// filtro de data.
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
