// Package scheduler — F18 Variables interpolation (Control-M AutoEdit).
//
// Substitui ${var.NAME} em todos os campos string de params, recursivamente.
// Escopos (precedência alta → baixa):
//
//  1. Runtime injetadas pelo scheduler (order_date, instance_id, parent.*)
//  2. Variables do JobDefinition
//  3. Variables globais (settings.yaml — futuro)
//
// Sintaxe: ${var.NAME}.  Tokens não resolvidos ficam intactos para visibilidade.
package scheduler

import (
	"fmt"
	"regexp"

	"github.com/Dr0nj/regente-server/internal/domain"
)

var varToken = regexp.MustCompile(`\$\{var\.([A-Za-z0-9_.-]+)\}`)

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

// InterpolateString substitui ${var.X} no input.
func InterpolateString(s string, ctx VarContext) string {
	return varToken.ReplaceAllStringFunc(s, func(match string) string {
		m := varToken.FindStringSubmatch(match)
		if len(m) != 2 {
			return match
		}
		if v, ok := ctx.lookup(m[1]); ok {
			return v
		}
		return match // intacto
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
	rt := map[string]string{
		"order_date":  orderDate,
		"instance_id": instanceID,
		"def_id":      def.ID,
		"def_label":   def.Label,
		"def_team":    def.Team,
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
