// Package scheduler — o POOL de condições (Control-M global conditions).
//
// Desde a unificação (2026-07-17) este é o ÚNICO sistema de dependência do
// Regente — ver domain/conditions.go e docs/conditions-events.md. Persistência
// simples em SQLite (tabela `conditions`). Escopo:
//   - scope_date = 'YYYY-MM-DD' → condição daquela diária (ODAT).
//   - scope_date = ”           → permanente/estática (@stat).
//
// Toda mudança dispara onChange (broadcast WS `condition.changed` — o painel
// Condições do Monitoring e as linhas do grafo são um REFLEXO deste pool).
// Avaliação:
//   - Job com ConditionsIn=[X,Y] só fica ready quando AMBAS existem no escopo
//     resolvido contra o seu ODAT (@odat default / @prev / @stat).
//   - Pós-finish OK (ou Set OK), ConditionsOutAdd cria, ConditionsOutRemove apaga.
package scheduler

import (
	"database/sql"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
)

type ConditionEngine struct {
	db *db.DB
	// onChange — notificação de mudança no pool (name, scope, kind: set|unset).
	// Best-effort, pode ser nil (testes). O Scheduler pluga o broadcast WS aqui.
	onChange func(name, scope, kind string)
}

func NewConditionEngine(db *db.DB) *ConditionEngine {
	return &ConditionEngine{db: db}
}

// OnChange registra o callback de mudança do pool (chamado em Set/Unset).
func (c *ConditionEngine) OnChange(fn func(name, scope, kind string)) { c.onChange = fn }

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
	if err == nil && c.onChange != nil {
		c.onChange(name, scopeDate, "set")
	}
	return err
}

// Unset removes a condition for the given scope (or all if scopeDate empty AND match-any).
func (c *ConditionEngine) Unset(name, scopeDate string) error {
	var err error
	if scopeDate == "" {
		// remove permanent only
		_, err = c.db.Exec(`DELETE FROM conditions WHERE name=? AND scope_date=''`, name)
	} else {
		_, err = c.db.Exec(`DELETE FROM conditions WHERE name=? AND scope_date=?`, name, scopeDate)
	}
	if err == nil && c.onChange != nil {
		c.onChange(name, scopeDate, "unset")
	}
	return err
}

// CondIndex — foto do pool inteiro numa query (hot path do tick): chave
// name+"\x00"+scope_date. O pool é pequeno (condições vivas do ambiente);
// carregar uma vez por tick evita um COUNT por condição por instance WAITING
// (a 100k–1M instances isso importa).
type CondIndex map[string]bool

func condKey(name, scope string) string { return name + "\x00" + scope }

// LoadIndex — carrega o pool inteiro num CondIndex.
func (c *ConditionEngine) LoadIndex() CondIndex {
	idx := CondIndex{}
	rows, err := c.db.Query(`SELECT name, scope_date FROM conditions`)
	if err != nil {
		return idx
	}
	defer rows.Close()
	for rows.Next() {
		var name, scope string
		if rows.Scan(&name, &scope) == nil {
			idx[condKey(name, scope)] = true
		}
	}
	return idx
}

// hasIdx — existência no índice (ou no banco, se idx nil): escopo exato OU permanente.
func (c *ConditionEngine) hasIdx(name, scope string, idx CondIndex) bool {
	if idx != nil {
		return idx[condKey(name, scope)] || idx[condKey(name, "")]
	}
	return c.Has(name, scope)
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

// resolveCondScope — resolve o sufixo de data de UMA condition contra o ODAT
// (origem) do job. prevOf resolve a diária anterior (injetado pelo Scheduler —
// daily_runs cobre lacunas de New Day).
//
//	NOME / NOME@odat → scope_date = ODAT do job
//	NOME@prev        → scope_date = diária anterior ao ODAT
//	NOME@stat        → scope_date = '' (estática/permanente)
func resolveCondScope(name, odate string, prevOf func(string) string) (base, scope string) {
	base, ref := splitCondRef(name)
	switch ref {
	case domain.DateRefStat:
		return base, ""
	case domain.DateRefPrev:
		return base, prevOf(odate)
	default:
		return base, odate
	}
}

// Apply post-finish hook: para cada job que terminou OK, adiciona
// ConditionsOutAdd e remove ConditionsOutRemove — no scope resolvido de cada
// nome (@odat default = ODAT do produtor; @prev; @stat = permanente).
func (c *ConditionEngine) ApplyOutcomes(def domain.JobDefinition, odate, actor string, prevOf func(string) string) {
	for _, n := range def.ConditionsOutAdd {
		base, scope := resolveCondScope(n, odate, prevOf)
		_ = c.Set(base, scope, actor)
	}
	for _, n := range def.ConditionsOutRemove {
		base, scope := resolveCondScope(n, odate, prevOf)
		_ = c.Unset(base, scope)
	}
}

// MissingCond — uma condition de entrada ainda não satisfeita, com o escopo
// resolvido (p/ o Explain dizer DE QUAL diária ela falta).
type MissingCond struct {
	Name       string // nome como declarado (com sufixo, se houver)
	Base       string // nome sem sufixo (o que se procura na tabela)
	Scope      string // scope_date resolvido ('' = estática)
	ScopeLabel string // sufixo legível p/ mensagens ("", " (diária X)", " (estática)")
}

// Missing returns the conditions in `names` that are NOT set — cada nome com o
// sufixo @odat/@prev/@stat resolvido contra o ODAT (origem) do job.
// FONTE ÚNICA do "falta qual condição?" — usada pelo gate do tick (com o
// CondIndex pré-carregado), pelo AllSatisfied e pelo Explain ("por que não
// rodou"). Vazio = todas satisfeitas. idx nil = consulta direta ao banco.
func (c *ConditionEngine) Missing(names []string, odate string, prevOf func(string) string) []MissingCond {
	return c.MissingIdx(names, odate, prevOf, nil)
}

func (c *ConditionEngine) MissingIdx(names []string, odate string, prevOf func(string) string, idx CondIndex) []MissingCond {
	var out []MissingCond
	for _, n := range names {
		base, scope := resolveCondScope(n, odate, prevOf)
		if c.hasIdx(base, scope, idx) {
			continue
		}
		label := ""
		switch {
		case scope == "":
			label = " (estática)"
		case scope != odate:
			label = " (diária " + scope + ")"
		}
		out = append(out, MissingCond{Name: n, Base: base, Scope: scope, ScopeLabel: label})
	}
	return out
}

// AllSatisfied returns true if every cond in `names` exists no escopo resolvido.
func (c *ConditionEngine) AllSatisfied(names []string, odate string, prevOf func(string) string) bool {
	return len(c.Missing(names, odate, prevOf)) == 0
}
