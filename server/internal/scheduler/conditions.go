// Package scheduler — F16 Conditions IN/OUT (Control-M global conditions).
//
// Persistência simples em SQLite (tabela `conditions`). Escopo:
//   - scope_date = 'YYYY-MM-DD' → condição daquele order_date.
//   - scope_date = ”           → permanente (não limpa entre dailies).
//
// Set/Unset emitem broadcast WS para a UI. Avaliação:
//   - Job com ConditionsIn=[X,Y] só fica ready quando AMBAS existem para
//     o seu order_date (ou permanente).
//   - Pós-finish (status OK), ConditionsOutAdd cria, ConditionsOutRemove apaga.
package scheduler

import (
	"database/sql"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
)

type ConditionEngine struct {
	db *db.DB
}

func NewConditionEngine(db *db.DB) *ConditionEngine {
	return &ConditionEngine{db: db}
}

// Has reports if condition with `name` is set for `scopeDate` or permanent.
func (c *ConditionEngine) Has(name, scopeDate string) bool {
	var n int
	_ = c.db.QueryRow(
		`SELECT COUNT(*) FROM conditions WHERE name=? AND (scope_date=? OR scope_date='')`,
		name, scopeDate,
	).Scan(&n)
	return n > 0
}

// Set adds a condition. setBy = ator ("scheduler", "operator", username).
func (c *ConditionEngine) Set(name, scopeDate, setBy string) error {
	_, err := c.db.Exec(
		`INSERT OR REPLACE INTO conditions(name, scope_date, set_at, set_by) VALUES(?,?,?,?)`,
		name, scopeDate, time.Now(), setBy,
	)
	return err
}

// Unset removes a condition for the given scope (or all if scopeDate empty AND match-any).
func (c *ConditionEngine) Unset(name, scopeDate string) error {
	if scopeDate == "" {
		// remove permanent only
		_, err := c.db.Exec(`DELETE FROM conditions WHERE name=? AND scope_date=''`, name)
		return err
	}
	_, err := c.db.Exec(`DELETE FROM conditions WHERE name=? AND scope_date=?`, name, scopeDate)
	return err
}

// List returns all conditions matching scope (or all if scopeDate is "*").
func (c *ConditionEngine) List(scopeDate string) ([]domain.Condition, error) {
	var rows *sql.Rows
	var err error
	if scopeDate == "*" {
		rows, err = c.db.Query(`SELECT name, scope_date, set_at, COALESCE(set_by,'') FROM conditions ORDER BY set_at DESC`)
	} else {
		rows, err = c.db.Query(
			`SELECT name, scope_date, set_at, COALESCE(set_by,'') FROM conditions WHERE scope_date=? OR scope_date='' ORDER BY set_at DESC`,
			scopeDate,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Condition{}
	for rows.Next() {
		var c domain.Condition
		_ = rows.Scan(&c.Name, &c.ScopeDate, &c.SetAt, &c.SetBy)
		out = append(out, c)
	}
	return out, nil
}

// Apply post-finish hook: para cada job que terminou OK,
// adiciona ConditionsOutAdd e remove ConditionsOutRemove no scope do orderDate.
func (c *ConditionEngine) ApplyOutcomes(def domain.JobDefinition, orderDate, actor string) {
	for _, n := range def.ConditionsOutAdd {
		_ = c.Set(n, orderDate, actor)
	}
	for _, n := range def.ConditionsOutRemove {
		_ = c.Unset(n, orderDate)
	}
}

// Missing returns the conditions in `names` that are NOT set for orderDate.
// FONTE ÚNICA do "falta qual condition?" — usada pelo AllSatisfied (gating) e
// pelo Explain ("por que não rodou"). Vazio = todas satisfeitas.
func (c *ConditionEngine) Missing(names []string, orderDate string) []string {
	var out []string
	for _, n := range names {
		if !c.Has(n, orderDate) {
			out = append(out, n)
		}
	}
	return out
}

// AllSatisfied returns true if every cond in `names` exists for orderDate.
func (c *ConditionEngine) AllSatisfied(names []string, orderDate string) bool {
	return len(c.Missing(names, orderDate)) == 0
}
