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
	"strconv"
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

// Cálculo de datas (Control-M %%CALCDATE): token de data com OFFSET —
// %%ODATE+3 · %%ORDERDATE-2 · ${var.ODATE+1B}. Sufixo B = dias ÚTEIS (pula
// fim de semana; com calendar no job, feriados também — ctx.BusinessDay).
// Resolvidos ANTES dos tokens simples (senão %%ODATE consumiria o prefixo e
// o "+3" ficaria solto no texto). O B é opcional e GULOSO: um "B" imediato
// após o número conta como marcador de dia útil (ex. "%%ODATE+1Backup" =
// +1 dia útil + "ackup" — sintaxe ambígua é responsabilidade de quem escreve;
// use separador claro tipo "%%ODATE+1_backup" pra evitar).
var ctmCalcToken = regexp.MustCompile(`%%(ODATE|ORDERDATE|RUNDATE)([+-]\d{1,3})(B?)`)
var varCalcToken = regexp.MustCompile(`\$\{var\.(ODATE|ORDERDATE|RUNDATE)([+-]\d{1,3})(B?)\}`)

// layout de cada token-base de data (o resultado preserva o formato do token).
var calcLayouts = map[string]string{
	"ODATE":     "20060102",
	"ORDERDATE": "2006-01-02",
	"RUNDATE":   "20060102",
}

// setVarDirective — SET de variável em runtime (Control-M ctmvar): o job
// ATRIBUI uma variável GLOBAL imprimindo, em linha própria do output:
//
//	%%SET NOME=VALOR
//
// O scheduler varre o output no término (FinishInstance) e grava no
// VariableStore — qualquer job seguinte lê via %%NOME / ${var.NOME}.
var setVarDirective = regexp.MustCompile(`(?m)^\s*%%SET\s+([A-Za-z][A-Za-z0-9_.-]*)\s*=\s*(.*?)\s*$`)

// maxSetVarsPerJob — teto de diretivas %%SET aplicadas por término (defesa
// contra output que gera lixo em massa no VariableStore).
const maxSetVarsPerJob = 20

// VarContext combina escopos para resolução.
type VarContext struct {
	Runtime    map[string]string
	Definition map[string]string
	Global     map[string]string

	// BusinessDay — "este dia conta como útil?" para offsets B (ex.: %%ODATE+3B).
	// nil = Mon–Fri puro. O scheduler injeta o calendar do job (feriados etc.).
	BusinessDay func(t time.Time) bool
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

// InterpolateString substitui ${var.X} e %%X no input. Tokens de CÁLCULO de
// data (%%ODATE+3, ${var.ODATE-1B}) resolvem primeiro — ver ctmCalcToken.
func InterpolateString(s string, ctx VarContext) string {
	out := varCalcToken.ReplaceAllStringFunc(s, func(match string) string {
		m := varCalcToken.FindStringSubmatch(match)
		if len(m) != 4 {
			return match
		}
		if v, ok := resolveDateCalc(ctx, m[1], m[2], m[3] == "B"); ok {
			return v
		}
		return match
	})
	out = ctmCalcToken.ReplaceAllStringFunc(out, func(match string) string {
		m := ctmCalcToken.FindStringSubmatch(match)
		if len(m) != 4 {
			return match
		}
		if v, ok := resolveDateCalc(ctx, m[1], m[2], m[3] == "B"); ok {
			return v
		}
		return match
	})
	out = varToken.ReplaceAllStringFunc(out, func(match string) string {
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

// resolveDateCalc — %%ODATE+3 / %%ORDERDATE-2B: pega o valor do token-base no
// contexto, soma o offset (dias corridos, ou ÚTEIS com sufixo B) e devolve no
// MESMO formato do token. Falha (token ausente/data inválida) = fica intacto.
func resolveDateCalc(ctx VarContext, base, offsetStr string, business bool) (string, bool) {
	layout, ok := calcLayouts[base]
	if !ok {
		return "", false
	}
	raw, ok := ctx.lookup(base)
	if !ok {
		return "", false
	}
	t, err := time.Parse(layout, raw)
	if err != nil {
		return "", false
	}
	n, err := strconv.Atoi(offsetStr)
	if err != nil {
		return "", false
	}
	if !business {
		return t.AddDate(0, 0, n).Format(layout), true
	}
	isBiz := ctx.BusinessDay
	if isBiz == nil {
		isBiz = func(d time.Time) bool {
			wd := d.Weekday()
			return wd != time.Saturday && wd != time.Sunday
		}
	}
	step := 1
	if n < 0 {
		step, n = -1, -n
	}
	const guard = 1000 // defesa: calendar sem nenhum dia útil não trava o loop
	for i := 0; n > 0 && i < guard; i++ {
		t = t.AddDate(0, 0, step)
		if isBiz(t) {
			n--
		}
	}
	if n > 0 {
		return "", false
	}
	return t.Format(layout), true
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
