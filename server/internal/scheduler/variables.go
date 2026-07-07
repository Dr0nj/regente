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
//
// CTM-2 (2026-07-06): os tokens NATIVOS de data (EOM/BOM/EOY/BOY/NEXTBD/
// PREVBD/FIRSTBD/LASTBD — ver resolveNativeDate) também servem de BASE para
// offset: %%EOM-1 = penúltimo dia do mês; %%LASTBD-1B = penúltimo dia útil.
const calcBases = `ODATE|ORDERDATE|RUNDATE|EOM|BOM|EOY|BOY|NEXTBD|PREVBD|FIRSTBD|LASTBD`

var ctmCalcToken = regexp.MustCompile(`%%(` + calcBases + `)([+-]\d{1,3})(B?)`)
var varCalcToken = regexp.MustCompile(`\$\{var\.(` + calcBases + `)([+-]\d{1,3})(B?)\}`)

// layout de cada token-base de data (o resultado preserva o formato do token).
// Tokens nativos usam o compacto YYYYMMDD (mesmo formato do ODATE).
var calcLayouts = map[string]string{
	"ODATE":     "20060102",
	"ORDERDATE": "2006-01-02",
	"RUNDATE":   "20060102",
	"EOM":       "20060102",
	"BOM":       "20060102",
	"EOY":       "20060102",
	"BOY":       "20060102",
	"NEXTBD":    "20060102",
	"PREVBD":    "20060102",
	"FIRSTBD":   "20060102",
	"LASTBD":    "20060102",
}

// setVarDirective — SET de variável em runtime (Control-M ctmvar): o job
// ATRIBUI uma variável GLOBAL imprimindo, em linha própria do output:
//
//	%%SET NOME=VALOR
//
// O scheduler varre o output no término (FinishInstance) e grava no
// VariableStore — qualquer job seguinte lê via %%NOME / ${var.NOME}.
var setVarDirective = regexp.MustCompile(`(?m)^\s*%%SET\s+([A-Za-z][A-Za-z0-9_.-]*)\s*=\s*(.*?)\s*$`)

// setLocalVarDirective — CTM-1 (2026-07-06): SET de variável com escopo LOCAL
// (Control-M ctmvar local): o valor vive SÓ dentro da própria instance —
//
//	%%SETLOCAL NOME=VALOR
//
// Persistido na coluna instances.local_vars (JSON), aplicado a CADA término de
// tentativa (uma tentativa falha passa estado pra próxima via retry) e lido na
// interpolação dos params da MESMA instance (retries, reruns e voltas cyclic).
// Não vaza pro VariableStore global — outro job nunca enxerga.
// (O regex global %%SET exige espaço após SET, então %%SETLOCAL não colide.)
var setLocalVarDirective = regexp.MustCompile(`(?m)^\s*%%SETLOCAL\s+([A-Za-z][A-Za-z0-9_.-]*)\s*=\s*(.*?)\s*$`)

// maxSetVarsPerJob — teto de diretivas %%SET aplicadas por término (defesa
// contra output que gera lixo em massa no VariableStore).
const maxSetVarsPerJob = 20

// VarContext combina escopos para resolução.
type VarContext struct {
	Runtime    map[string]string
	Local      map[string]string // CTM-1: %%SETLOCAL da própria instance
	Definition map[string]string
	Global     map[string]string

	// BusinessDay — "este dia conta como útil?" para offsets B (ex.: %%ODATE+3B).
	// nil = Mon–Fri puro. O scheduler injeta o calendar do job (feriados etc.).
	BusinessDay func(t time.Time) bool
}

// lookup — precedência alta→baixa: Runtime > Local (instance) > Definition > Global.
func (ctx VarContext) lookup(name string) (string, bool) {
	if v, ok := ctx.Runtime[name]; ok {
		return v, true
	}
	if v, ok := ctx.Local[name]; ok {
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

// businessFn — BusinessDay do contexto, com fallback Mon–Fri puro.
func (ctx VarContext) businessFn() func(time.Time) bool {
	if ctx.BusinessDay != nil {
		return ctx.BusinessDay
	}
	return func(d time.Time) bool {
		wd := d.Weekday()
		return wd != time.Saturday && wd != time.Sunday
	}
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
		if v, ok := resolveNativeDate(ctx, m[1]); ok {
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
		if v, ok := resolveNativeDate(ctx, m[1]); ok {
			return v
		}
		return match // intacto (visibilidade de token não resolvido)
	})
}

// resolveNativeDate — CTM-2: tokens NATIVOS de data como VALOR direto,
// derivados do ODATE da instance (formato compacto YYYYMMDD):
//
//	%%EOM     último dia do mês       %%BOM     primeiro dia do mês
//	%%EOY     último dia do ano       %%BOY     primeiro dia do ano
//	%%NEXTBD  próximo dia útil        %%PREVBD  dia útil anterior
//	%%FIRSTBD 1º dia útil do mês      %%LASTBD  último dia útil do mês
//
// "Dia útil" respeita o calendar do job via ctx.BusinessDay (feriados);
// sem calendar, Mon–Fri puro. Todos aceitam offset (%%EOM-1, %%LASTBD-1B) —
// ver ctmCalcToken. Um lookup explícito (Runtime/Local/Definition/Global) com
// o mesmo nome tem precedência: aqui é só o fallback nativo.
func resolveNativeDate(ctx VarContext, name string) (string, bool) {
	const layout = "20060102"
	if _, isNative := map[string]bool{"EOM": true, "BOM": true, "EOY": true, "BOY": true,
		"NEXTBD": true, "PREVBD": true, "FIRSTBD": true, "LASTBD": true}[name]; !isNative {
		return "", false
	}
	raw, ok := ctx.lookup("ODATE")
	if !ok {
		return "", false
	}
	t, err := time.Parse(layout, raw)
	if err != nil {
		return "", false
	}
	isBiz := ctx.businessFn()
	// step — anda dia a dia até cair num dia útil (guard: calendar sem dia útil).
	step := func(from time.Time, dir int) (time.Time, bool) {
		const guard = 1000
		d := from
		for i := 0; i < guard; i++ {
			if isBiz(d) {
				return d, true
			}
			d = d.AddDate(0, 0, dir)
		}
		return time.Time{}, false
	}
	var out time.Time
	okStep := true
	switch name {
	case "EOM":
		out = time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, t.Location())
	case "BOM":
		out = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	case "EOY":
		out = time.Date(t.Year(), 12, 31, 0, 0, 0, 0, t.Location())
	case "BOY":
		out = time.Date(t.Year(), 1, 1, 0, 0, 0, 0, t.Location())
	case "NEXTBD":
		out, okStep = step(t.AddDate(0, 0, 1), +1)
	case "PREVBD":
		out, okStep = step(t.AddDate(0, 0, -1), -1)
	case "FIRSTBD":
		out, okStep = step(time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()), +1)
	case "LASTBD":
		out, okStep = step(time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, t.Location()), -1)
	}
	if !okStep {
		return "", false
	}
	return out.Format(layout), true
}

// resolveDateCalc — %%ODATE+3 / %%ORDERDATE-2B / %%EOM-1: pega o valor do
// token-base no contexto (ou o token NATIVO — EOM/NEXTBD/...), soma o offset
// (dias corridos, ou ÚTEIS com sufixo B) e devolve no MESMO formato do token.
// Falha (token ausente/data inválida) = fica intacto.
func resolveDateCalc(ctx VarContext, base, offsetStr string, business bool) (string, bool) {
	layout, ok := calcLayouts[base]
	if !ok {
		return "", false
	}
	raw, ok := ctx.lookup(base)
	if !ok {
		raw, ok = resolveNativeDate(ctx, base)
		if !ok {
			return "", false
		}
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
	isBiz := ctx.businessFn()
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
