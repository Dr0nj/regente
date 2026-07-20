// Package domain — CONDIÇÕES: o modelo ÚNICO de dependência do Regente.
//
// Desde 2026-07-17 (report do usuário: "todas as condições de dependências têm
// que ser uma coisa só"), NÃO existem dois sistemas. Toda dependência é uma
// CONDIÇÃO nomeada num POOL global (tabela `conditions`), como as global
// conditions do Control-M:
//
//   - um job ADICIONA condições ao pool quando termina OK/Set OK
//     (ConditionsOutAdd) — ou via ação On/Do `set-condition`;
//   - um job DEPENDE de condições (ConditionsIn): só roda quando TODAS
//     existem no pool, no escopo de data resolvido (@odat/@prev/@stat);
//   - um job REMOVE condições do pool quando termina OK/Set OK
//     (ConditionsOutRemove) — é o "delete input conditions on OK" que faz o
//     consumo: Set OK + rerun => o job volta a esperar, porque o próprio
//     Set OK apagou a condição de entrada.
//
// A SETINHA do grafo (Design) é só açúcar de UI: ligar A→B escreve
// `LinkCondName(A,B)` no OutAdd de A e no In + OutRemove de B — exatamente o
// que o usuário faria digitando à mão. Não há condição "especial" de aresta.
//
// O campo `upstream[]` segue existindo por DOIS motivos, nenhum deles gate:
//  1. compat de leitura: YAML antigo com `upstream:` é EXPANDIDO em condições
//     explícitas ao carregar (NormalizeConditions, idempotente);
//  2. visão derivada: depois da expansão, `upstream` é RECALCULADO por
//     name-matching produtor→consumidor e serve só de topologia para o
//     canvas/WhatIf/RCA/forecast. Nunca é gate e não precisa ser persistido.
package domain

import "strings"

// SplitCondRef — "NOME@prev" → ("NOME", DateRefPrev). Sem sufixo (ou sufixo
// desconhecido, tratado como parte do nome) = odat. Case-insensitive no ref.
func SplitCondRef(name string) (base string, ref DateRef) {
	if i := strings.LastIndex(name, "@"); i > 0 {
		switch strings.ToLower(name[i+1:]) {
		case "odat":
			return name[:i], DateRefOdat
		case "prev":
			return name[:i], DateRefPrev
		case "stat":
			return name[:i], DateRefStat
		}
	}
	return name, DateRefOdat
}

// WithCondRef — inverso do SplitCondRef: odat não ganha sufixo (default).
func WithCondRef(base string, ref DateRef) string {
	if ref == "" || ref == DateRefOdat {
		return base
	}
	return base + "@" + string(ref)
}

// LinkCondName — nome da condição sintetizada por uma ligação de grafo A→B
// (a setinha do Design e a expansão do upstream legado usam o MESMO nome,
// então ligar pela caixinha ou digitar à mão dá no mesmo lugar).
func LinkCondName(fromID, toID string) string {
	return strings.ToUpper(fromID + "-TO-" + toID)
}

// producesBase — o def cria a condição de base `b`? (OutAdd ou ação set-condition)
func producesBase(d *JobDefinition, b string) bool {
	for _, n := range d.ConditionsOutAdd {
		if bb, _ := SplitCondRef(n); bb == b {
			return true
		}
	}
	for _, a := range d.Actions {
		if a.Do == "set-condition" && a.Condition != "" {
			if bb, _ := SplitCondRef(a.Condition); bb == b {
				return true
			}
		}
	}
	return false
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// alreadyLinked — o par produtor P → consumidor C já está fechado por alguma
// condição explícita? (regra de idempotência da expansão: um upstream legado —
// ou a visão derivada re-salva em YAML — NÃO re-expande um vínculo que já
// existe como condição.)
func alreadyLinked(p, c *JobDefinition) bool {
	for _, n := range c.ConditionsIn {
		b, _ := SplitCondRef(n)
		if producesBase(p, b) {
			return true
		}
	}
	return false
}

// expandLegacyUpstream — converte as arestas `upstream` de UM consumidor em
// condições explícitas (mutação in-place em c e nos produtores). Ver regras no
// cabeçalho do arquivo. `byID` é o escopo (pode faltar produtor — aresta é
// ignorada: sem produtor não há o que expandir).
func expandLegacyUpstream(c *JobDefinition, byID map[string]*JobDefinition) {
	for _, u := range c.Upstream {
		if u.From == "" || u.From == c.ID {
			continue
		}
		p, ok := byID[u.From]
		if !ok || alreadyLinked(p, c) {
			continue
		}
		name := LinkCondName(u.From, c.ID)
		suffixed := WithCondRef(name, u.DateRef)
		// Lado PRODUTOR: adiciona na própria diária (ODAT), que é o que um
		// consumidor @odat (mesma origem) ou @prev (pai da diária anterior)
		// procura. Aresta @stat pedia "qualquer data" → produtor cria a
		// PERMANENTE (@stat), que é o único escopo que um IN @stat enxerga.
		prodName := name
		if u.DateRef == DateRefStat {
			prodName = WithCondRef(name, DateRefStat)
		}
		switch u.Condition {
		case CondAlways:
			// "always" só exigia o pai EXISTIR no dia — não é expressável (nem
			// necessário) como condição de pool; a ligação vira topologia sem gate.
			continue
		case CondOnFailure:
			// pai ADICIONA a condição só quando FALHA (ação On/Do), nunca no OK.
			addNotOKSetCondition(p, prodName)
		case CondOnComplete:
			// OK adiciona (OutAdd) E NOTOK adiciona (ação On/Do).
			if !containsStr(p.ConditionsOutAdd, prodName) {
				p.ConditionsOutAdd = append(p.ConditionsOutAdd, prodName)
			}
			addNotOKSetCondition(p, prodName)
		default: // on-success | "" (default do produto)
			if !containsStr(p.ConditionsOutAdd, prodName) {
				p.ConditionsOutAdd = append(p.ConditionsOutAdd, prodName)
			}
		}
		if !containsStr(c.ConditionsIn, suffixed) {
			c.ConditionsIn = append(c.ConditionsIn, suffixed)
		}
		// A setinha SEMPRE arma a deleção no consumidor (regra do usuário:
		// adicionar via seta => entrada + deleção automáticas).
		if !containsStr(c.ConditionsOutRemove, suffixed) {
			c.ConditionsOutRemove = append(c.ConditionsOutRemove, suffixed)
		}
	}
}

func addNotOKSetCondition(p *JobDefinition, name string) {
	for _, a := range p.Actions {
		if a.Do == "set-condition" && a.Condition == name && a.On == "result" && a.Status == string(StatusNotOK) {
			return
		}
	}
	p.Actions = append(p.Actions, ActionRule{
		On: "result", Status: string(StatusNotOK), Do: "set-condition", Condition: name,
	})
}

// deriveUpstreamView — recalcula `upstream[]` como VISÃO da topologia: uma
// aresta P→C para cada base de ConditionsIn de C que algum P do escopo produz
// (OutAdd ou ação set-condition). NÃO é gate — o gate é o pool. O dateRef da
// aresta espelha o sufixo da condição de entrada (informação de pareamento
// visual: qual diária do pai casa com este filho).
func deriveUpstreamView(c *JobDefinition, defs []*JobDefinition) {
	c.Upstream = nil
	seen := map[string]bool{}
	for _, n := range c.ConditionsIn {
		b, ref := SplitCondRef(n)
		for _, p := range defs {
			if p.ID == c.ID || seen[p.ID] || !producesBase(p, b) {
				continue
			}
			seen[p.ID] = true
			e := Upstream{From: p.ID, Condition: CondOnSuccess}
			if ref != DateRefOdat {
				e.DateRef = ref
			}
			c.Upstream = append(c.Upstream, e)
		}
	}
}

// NormalizeConditions — normaliza um ESCOPO de definitions (folder/workspace
// inteiro) para o modelo único de condições. Chamada no CHOKEPOINT de leitura
// (FileStore.List): scheduler, API, sessions e publish enxergam sempre defs já
// normalizadas. Idempotente — rodar duas vezes não duplica nada.
func NormalizeConditions(defs []JobDefinition) []JobDefinition {
	ptrs := make([]*JobDefinition, len(defs))
	byID := make(map[string]*JobDefinition, len(defs))
	for i := range defs {
		ptrs[i] = &defs[i]
		byID[defs[i].ID] = &defs[i]
	}
	// 1) expande upstream legado em condições explícitas (muta consumidor E produtor)
	for _, c := range ptrs {
		if len(c.Upstream) > 0 {
			expandLegacyUpstream(c, byID)
		}
	}
	// 2) saneia a lógica booleana de entrada (CL) e completa o invariante
	//    "membros não-$TIME ⊆ ConditionsIn" ANTES de derivar topologia (que lê
	//    ConditionsIn). $TIME nunca entra em ConditionsIn → topologia fica limpa.
	for _, c := range ptrs {
		normalizeConditionLogic(c)
	}
	// 3) upstream vira visão derivada (topologia por name-matching)
	for _, c := range ptrs {
		deriveUpstreamView(c, ptrs)
	}
	return defs
}

// ============================================================================
// CL — lógica booleana de ENTRADA (AND/OR em forma DNF)
// ============================================================================
//
// A entrada de um job passou de um AND implícito de `ConditionsIn` para uma
// expressão booleana opcional em forma DNF (disjunção de conjunções — dois
// níveis, cada nível com seu operador):
//
//	ConditionLogic{ Op, Groups[] }   com  CondGroup{ Op, Members[] }
//	avaliação: Op( group.Op( sat(member) ) for group in Groups )
//
// Cobre os dois exemplos canônicos do usuário:
//
//	(C1 AND C2) OR C3  →  Op=OR,  Groups=[{AND,[C1,C2]}, {AND,[C3]}]
//	(C1 OR C2) AND C3  →  Op=AND, Groups=[{OR,[C1,C2]}, {AND,[C3]}]
//
// Semântica do OR: "o primeiro ramo que chega satisfaz e dispara" — assim que
// QUALQUER grupo fica verdadeiro, o job roda (não espera os demais).
//
// Retrocompat (CL-3): `ConditionLogic` nil (ou sem grupos) = UM grupo AND sobre
// `ConditionsIn` — exatamente o gate antigo, nada muda. Os membros não-`$TIME`
// da lógica são um SUBCONJUNTO de `ConditionsIn` (NormalizeConditions garante o
// superconjunto), então topologia (upstream derivado), linhas do Monitoring e a
// coluna congelada `conds_in` seguem lendo `ConditionsIn` sem saber da lógica.

// Operadores da lógica de entrada.
const (
	CondOpAnd = "AND"
	CondOpOr  = "OR"
)

// CondTokenTime — membro RESERVADO satisfeito quando now >= scheduledAt (o mesmo
// relógio do gate de janela). Habilita o fallback temporal "condição OU horário"
// (CL-2): representa o "a partir de" (windowFrom) como um membro da expressão.
const CondTokenTime = "$TIME"

// CondGroup — membros combinados por um operador (AND|OR). Membros são refs de
// condição (com sufixo @odat/@prev/@stat) ou o token reservado `$TIME`.
type CondGroup struct {
	Op      string   `yaml:"op" json:"op"` // "AND" | "OR"
	Members []string `yaml:"members" json:"members"`
}

// ConditionLogic — expressão booleana de entrada em forma DNF. `Op` combina os
// grupos (topo); cada grupo combina seus membros pelo `CondGroup.Op`.
type ConditionLogic struct {
	Op     string      `yaml:"op" json:"op"` // operador de TOPO entre os grupos
	Groups []CondGroup `yaml:"groups" json:"groups"`
}

// UsesTimeToken — a expressão referencia o token `$TIME`? (CL-2: quando sim, o
// gate de JANELA deixa de ser piso — a trava de início passa a ser SÓ o `$TIME`
// dentro da expressão, senão o OR nunca anteciparia por condição.)
func (l *ConditionLogic) UsesTimeToken() bool {
	if l == nil {
		return false
	}
	for _, g := range l.Groups {
		for _, m := range g.Members {
			if m == CondTokenTime {
				return true
			}
		}
	}
	return false
}

// normCondOp — normaliza o operador; qualquer coisa != OR (vazio/desconhecido)
// vira AND, o default retrocompat mais seguro (um grupo malformado nunca vira um
// OR que dispara cedo demais).
func normCondOp(op string) string {
	if strings.EqualFold(op, CondOpOr) {
		return CondOpOr
	}
	return CondOpAnd
}

// CondGroupEval / CondLogicEval — resultado ESTRUTURADO da avaliação, pro Explain
// dizer QUAL ramo falta ("aguardando (C1 E C2) OU C3 — nenhum satisfeito").
type CondGroupEval struct {
	Op        string
	Members   []string
	Unmet     []string // membros insatisfeitos deste grupo
	Satisfied bool
	// Required — grupo de membros AVULSOS de ConditionsIn (fora da lógica),
	// sempre ANDado com a expressão (a setinha adiciona condição simples num job
	// com lógica → obrigatória). Rendedizado com "E", independente do topo.
	Required bool
}
type CondLogicEval struct {
	Op        string
	Groups    []CondGroupEval
	Satisfied bool
}

// evalGroup — avalia UM grupo pelo seu operador. Grupo sem membros = satisfeito
// (nunca bloqueia por grupo vazio malformado).
func evalGroup(g CondGroup, sat func(member string) bool) CondGroupEval {
	gop := normCondOp(g.Op)
	ge := CondGroupEval{Op: gop, Members: g.Members}
	gAll, gAny := true, false
	for _, m := range g.Members {
		if sat(m) {
			gAny = true
		} else {
			gAll = false
			ge.Unmet = append(ge.Unmet, m)
		}
	}
	switch {
	case len(g.Members) == 0:
		ge.Satisfied = true
	case gop == CondOpOr:
		ge.Satisfied = gAny
	default:
		ge.Satisfied = gAll
	}
	return ge
}

// looseMembers — membros de flatIn (ConditionsIn) NÃO cobertos por nenhum grupo
// da lógica. São requisitos AND adicionais (ver CondGroupEval.Required).
func looseMembers(logic *ConditionLogic, flatIn []string) []string {
	covered := map[string]bool{}
	for _, g := range logic.Groups {
		for _, m := range g.Members {
			covered[m] = true
		}
	}
	var loose []string
	for _, n := range flatIn {
		if !covered[n] {
			loose = append(loose, n)
		}
	}
	return loose
}

// EvalConditionLogic — avalia a expressão DNF com o predicado `sat(member)`
// (satisfação de um membro: existe no pool / `$TIME` atingido). Puro e sem I/O —
// o scheduler injeta `sat`. logic nil ⇒ AND de `flatIn` (retrocompat). Quando há
// lógica, o resultado é `topOp(grupos) E (membros AVULSOS de flatIn)`: qualquer
// nome de ConditionsIn fora da lógica é requisito AND (a setinha adiciona
// condição simples num job com lógica → obrigatória).
func EvalConditionLogic(logic *ConditionLogic, flatIn []string, sat func(member string) bool) CondLogicEval {
	// Sem lógica: AND implícito de todas as flatIn.
	if logic == nil || len(logic.Groups) == 0 {
		res := CondLogicEval{Op: CondOpAnd}
		if len(flatIn) == 0 {
			res.Satisfied = true
			return res
		}
		ge := evalGroup(CondGroup{Op: CondOpAnd, Members: flatIn}, sat)
		res.Groups = []CondGroupEval{ge}
		res.Satisfied = ge.Satisfied
		return res
	}
	topOp := normCondOp(logic.Op)
	res := CondLogicEval{Op: topOp}
	anySat, allSat := false, true
	for _, g := range logic.Groups {
		ge := evalGroup(g, sat)
		if ge.Satisfied {
			anySat = true
		} else {
			allSat = false
		}
		res.Groups = append(res.Groups, ge)
	}
	logicSat := allSat
	if topOp == CondOpOr {
		logicSat = anySat
	}
	// Membros avulsos (fora da lógica) = requisitos AND adicionais.
	looseSat := true
	if loose := looseMembers(logic, flatIn); len(loose) > 0 {
		ge := evalGroup(CondGroup{Op: CondOpAnd, Members: loose}, sat)
		ge.Required = true
		res.Groups = append(res.Groups, ge)
		looseSat = ge.Satisfied
	}
	res.Satisfied = logicSat && looseSat
	return res
}

// RenderExpr — expressão legível em português ("(C1 E C2) OU C3"), pro Explain e
// o summary do blocker. Grupos com >1 membro ganham parênteses; grupos avulsos
// (Required) são ANDados no fim ("… E AVULSO"); vazio devolve "".
func (e CondLogicEval) RenderExpr() string {
	if len(e.Groups) == 0 {
		return ""
	}
	sep := " E "
	if e.Op == CondOpOr {
		sep = " OU "
	}
	var logicParts, reqParts []string
	for _, g := range e.Groups {
		if g.Required {
			reqParts = append(reqParts, g.render())
		} else {
			logicParts = append(logicParts, g.render())
		}
	}
	logicStr := strings.Join(logicParts, sep)
	// Ao ANDar um OR-de-topo com requisitos avulsos, parênteses evitam ambiguidade.
	if len(logicParts) > 1 && e.Op == CondOpOr && len(reqParts) > 0 {
		logicStr = "(" + logicStr + ")"
	}
	all := make([]string, 0, len(reqParts)+1)
	if logicStr != "" {
		all = append(all, logicStr)
	}
	all = append(all, reqParts...)
	return strings.Join(all, " E ")
}

func (g CondGroupEval) render() string {
	if len(g.Members) == 0 {
		return ""
	}
	sep := " E "
	if g.Op == CondOpOr {
		sep = " OU "
	}
	parts := make([]string, len(g.Members))
	for i, m := range g.Members {
		parts[i] = renderMember(m)
	}
	s := strings.Join(parts, sep)
	if len(g.Members) > 1 {
		return "(" + s + ")"
	}
	return s
}

// renderMember — o token `$TIME` vira "horário" nas mensagens (Explain/summary).
func renderMember(m string) string {
	if m == CondTokenTime {
		return "horário"
	}
	return m
}

// normalizeConditionLogic — saneia a lógica de UM job e garante o invariante
// "membros não-$TIME ⊆ ConditionsIn" (topologia/linhas/coluna congelada leem
// ConditionsIn). Descarta lógica vazia (vira nil = AND implícito). Chamada por
// NormalizeConditions no chokepoint de leitura.
func normalizeConditionLogic(d *JobDefinition) {
	if d.ConditionLogic == nil {
		return
	}
	// Poda grupos vazios; lógica sem grupo = sem lógica (nil).
	groups := d.ConditionLogic.Groups[:0]
	for _, g := range d.ConditionLogic.Groups {
		if len(g.Members) == 0 {
			continue
		}
		g.Op = normCondOp(g.Op)
		groups = append(groups, g)
		for _, m := range g.Members {
			if m == CondTokenTime {
				continue
			}
			if !containsStr(d.ConditionsIn, m) {
				d.ConditionsIn = append(d.ConditionsIn, m)
			}
		}
	}
	if len(groups) == 0 {
		d.ConditionLogic = nil
		return
	}
	d.ConditionLogic.Op = normCondOp(d.ConditionLogic.Op)
	d.ConditionLogic.Groups = groups
}

// ExpandSnapshotConditions — versão para a def CONGELADA de uma instance
// (definition_snapshot): expande o lado CONSUMIDOR do upstream legado (In +
// OutRemove) para o gate de instances ordenadas ANTES da unificação. O lado
// produtor (OutAdd do pai) vem da def VIVA normalizada (applyConditionsOut faz
// a união). `live` = defs vivas, para a regra de idempotência alreadyLinked.
func ExpandSnapshotConditions(d JobDefinition, live map[string]JobDefinition) JobDefinition {
	for _, u := range d.Upstream {
		if u.From == "" || u.From == d.ID || u.Condition == CondAlways {
			continue
		}
		if p, ok := live[u.From]; ok && alreadyLinked(&p, &d) {
			continue
		}
		suffixed := WithCondRef(LinkCondName(u.From, d.ID), u.DateRef)
		if !containsStr(d.ConditionsIn, suffixed) {
			d.ConditionsIn = append(d.ConditionsIn, suffixed)
		}
		if !containsStr(d.ConditionsOutRemove, suffixed) {
			d.ConditionsOutRemove = append(d.ConditionsOutRemove, suffixed)
		}
	}
	return d
}
