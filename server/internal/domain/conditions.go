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
//   1. compat de leitura: YAML antigo com `upstream:` é EXPANDIDO em condições
//      explícitas ao carregar (NormalizeConditions, idempotente);
//   2. visão derivada: depois da expansão, `upstream` é RECALCULADO por
//      name-matching produtor→consumidor e serve só de topologia para o
//      canvas/WhatIf/RCA/forecast. Nunca é gate e não precisa ser persistido.
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
	// 2) upstream vira visão derivada (topologia por name-matching)
	for _, c := range ptrs {
		deriveUpstreamView(c, ptrs)
	}
	return defs
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
