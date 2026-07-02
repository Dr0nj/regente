// Package scheduler — F18 Variables interpolation (Control-M AutoEdit).
//
// Substitui ${var.NAME} e %%NAME em todos os campos string de params,
// recursivamente. Escopos (precedência alta → baixa):
//
//  1. Runtime injetadas pelo scheduler (order_date, instance_id, parent.*,
//     e os tokens Control-M em MAIÚSCULAS: ODATE, ORDERDATE, RUNDATE, TIME,
//     JOBNAME, JOBLABEL, FOLDER, INSTANCEID)
//  2. Variables do JobDefinition
//  3. Variables globais (VariableStore, CRUD em /api/variables)
//
// Sintaxes aceitas (equivalentes, mesma resolução):
//   - ${var.NAME}  — nativa do Regente
//   - %%NAME       — Control-M AutoEdit (paridade p/ migração de jobs CTM)
//
// Tokens não resolvidos ficam INTACTOS para visibilidade (o operador vê no
// output que %%FOO não resolveu, em vez de sumir silenciosamente).
package scheduler

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

var varToken = regexp.MustCompile(`\$\{var\.([A-Za-z0-9_.-]+)\}`)

// ctmToken — %%NAME (Control-M AutoEdit). Nome = letra seguida de letras/
// dígitos/_ (SEM ponto/hífen: senão "%%JOBNAME-%%ODATE.log" viraria o nome
// "JOBNAME-" e nada resolveria). Vars com ponto no nome (parent.exit_code)
// usam a sintaxe ${var.NAME}. "%%" sem nome válido depois fica intacto.
var ctmToken = regexp.MustCompile(`%%([A-Za-z][A-Za-z0-9_]*)`)

// VarContext combina escopos para resolução.
type VarContext struct {
	Runtime    map[string]string
	Definition map[string]string
	Global     map[string]string
}

func (ctx VarContext) lookup(name string) (string, bool) {
	if v, ok := ctx.Runtime[name]; ok {
		return v, true
	}
	if v, ok := ctx.Definition[name]; ok {
		return v, true
	}
	if v, ok := ctx.Global[name]; ok {
		return v, true
	}
	return "", false
}

// InterpolateString substitui ${var.X} e %%X no input.
func InterpolateString(s string, ctx VarContext) string {
	out := varToken.ReplaceAllStringFunc(s, func(match string) string {
		m := varToken.FindStringSubmatch(match)
		if len(m) != 2 {
			return match
		}
		if v, ok := ctx.lookup(m[1]); ok {
			return v
		}
		return match // intacto
	})
	return ctmToken.ReplaceAllStringFunc(out, func(match string) string {
		m := ctmToken.FindStringSubmatch(match)
		if len(m) != 2 {
			return match
		}
		if v, ok := ctx.lookup(m[1]); ok {
			return v
		}
		return match // intacto (visibilidade de token não resolvido)
	})
}

// InterpolateValue aplica recursivamente em strings dentro de
// map[string]interface{} / []interface{}. Outros tipos passam direto.
func InterpolateValue(v interface{}, ctx VarContext) interface{} {
	switch vv := v.(type) {
	case string:
		return InterpolateString(vv, ctx)
	case map[string]interface{}:
		out := make(map[string]interface{}, len(vv))
		for k, x := range vv {
			out[k] = InterpolateValue(x, ctx)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(vv))
		for i, x := range vv {
			out[i] = InterpolateValue(x, ctx)
		}
		return out
	default:
		return v
	}
}

// InterpolateParams é o helper usado pelo scheduler antes de despachar.
func InterpolateParams(params map[string]interface{}, ctx VarContext) map[string]interface{} {
	if params == nil {
		return nil
	}
	out := make(map[string]interface{}, len(params))
	for k, v := range params {
		out[k] = InterpolateValue(v, ctx)
	}
	return out
}

// BuildContext monta um VarContext a partir do def + runtime info.
func BuildContext(def domain.JobDefinition, instanceID, orderDate string, parentExitCode *int, parentOutput string) VarContext {
	now := time.Now()
	rt := map[string]string{
		"order_date":  orderDate,
		"instance_id": instanceID,
		"def_id":      def.ID,
		"def_label":   def.Label,
		"def_team":    def.Team,
		// Tokens Control-M (MAIÚSCULAS) — os nomes que um job migrado do CTM
		// espera encontrar via %%NOME. ODATE = data da ordem compacta (AutoEdit
		// clássico); RUNDATE/TIME = instante do DISPATCH (≠ ODATE em rerun/carry).
		"ODATE":      strings.ReplaceAll(orderDate, "-", ""), // YYYYMMDD
		"ORDERDATE":  orderDate,                              // YYYY-MM-DD
		"RUNDATE":    now.Format("20060102"),
		"TIME":       now.Format("150405"),
		"JOBNAME":    def.ID,
		"JOBLABEL":   def.Label,
		"FOLDER":     def.Team,
		"INSTANCEID": instanceID,
	}
	if parentExitCode != nil {
		rt["parent.exit_code"] = fmt.Sprintf("%d", *parentExitCode)
	}
	if parentOutput != "" {
		rt["parent.output"] = parentOutput
	}
	return VarContext{
		Runtime:    rt,
		Definition: def.Variables,
		Global:     nil,
	}
}
