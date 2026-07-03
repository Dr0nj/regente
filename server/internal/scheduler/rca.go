// Package scheduler — RCA automático (Root Cause Analysis).
//
// Diferencial sobre o Control-M: quando um job falhou OU está travado esperando um
// upstream, o operador quer saber a CAUSA RAIZ — não o vizinho imediato, mas o job
// LÁ NO FUNDO que falhou por conta própria e derrubou a cadeia. Este análise sobe
// o grafo de dependências seguindo só os ancestrais em estado de FALHA (NOTOK/
// CANCELLED) e devolve a(s) raiz(es): jobs falhos cujos próprios upstreams NÃO
// falharam (falharam sozinhos, não por cascata).
//
// Read-only, reusa o snapshot de defs + defInstanceStatus. Não toca o hot path.
package scheduler

import (
	"fmt"
	"strings"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// rcaMaxVisit — teto de nós visitados na subida (defesa contra grafos densos).
const rcaMaxVisit = 2000

type RCACause struct {
	DefID  string `json:"defId"`
	Label  string `json:"label,omitempty"`
	Team   string `json:"team,omitempty"`
	Status string `json:"status"`          // NOTOK | CANCELLED
	Depth  int    `json:"depth"`           // saltos do alvo até esta raiz (0 = o próprio alvo)
	Reason string `json:"reason,omitempty"`
}

type RCA struct {
	InstanceID string     `json:"instanceId"`
	DefID      string     `json:"definitionId"`
	Status     string     `json:"status"`
	OrderDate  string     `json:"orderDate"`
	Summary    string     `json:"summary"`
	Roots      []RCACause `json:"roots"`          // causa(s) raiz da falha/bloqueio
	Chain      []string   `json:"chain,omitempty"` // um caminho do alvo até uma raiz (defIDs)
}

// isFailed — estado terminal de falha que propaga uma causa raiz.
func isFailed(status string) bool {
	return status == string(domain.StatusNotOK) || status == string(domain.StatusCancelled)
}

// RCA analisa a causa raiz da falha/bloqueio da instance dada. Só é significativo
// quando o alvo falhou, foi cancelado, ou está WAITING travado por um upstream
// falho; caso contrário devolve um summary explicando que não há causa a rastrear.
func (s *Scheduler) RCA(instanceID string) (RCA, error) {
	var defID, status, orderDate string
	err := s.db.QueryRow(
		`SELECT definition_id, status, order_date FROM instances WHERE id=?`, instanceID,
	).Scan(&defID, &status, &orderDate)
	if err != nil {
		return RCA{}, err
	}

	s.mu.Lock()
	defs := make(map[string]domain.JobDefinition, len(s.defs))
	for _, d := range s.defs {
		defs[d.ID] = d
	}
	s.mu.Unlock()

	rca := RCA{InstanceID: instanceID, DefID: defID, Status: status, OrderDate: orderDate, Roots: []RCACause{}}

	statusCache := map[string]string{defID: status}
	statusOf := func(id string) string {
		st, ok := statusCache[id]
		if !ok {
			st = s.defInstanceStatus(orderDate, id)
			statusCache[id] = st
		}
		return st
	}

	// hasFailedParent — algum upstream deste job está em estado de falha?
	hasFailedParent := func(id string) (string, bool) {
		for _, u := range defs[id].Upstream {
			if isFailed(statusOf(u.From)) {
				return u.From, true
			}
		}
		return "", false
	}

	// Caso base: alvo terminou OK ou está rodando/aguardando horário → nada a rastrear.
	targetFailed := isFailed(status)
	_, targetBlockedByFail := hasFailedParent(defID)
	if !targetFailed && !targetBlockedByFail {
		switch status {
		case string(domain.StatusOK):
			rca.Summary = "Job concluiu com sucesso — sem causa raiz a rastrear."
		case string(domain.StatusRunning):
			rca.Summary = "Job em execução — sem falha até agora."
		default:
			rca.Summary = "Job não está falho nem bloqueado por um upstream falho — sem causa raiz de falha a rastrear."
		}
		return rca, nil
	}

	// Sobe a cadeia seguindo SÓ ancestrais em falha. Uma raiz = job falho sem
	// nenhum upstream falho (falhou por conta própria). BFS com depth.
	type qi struct {
		id    string
		depth int
	}
	// A busca começa nos "pontos de falha" imediatos: se o alvo falhou, ele é o
	// ponto de partida; se está só bloqueado, começa pelos pais falhos.
	start := []qi{}
	if targetFailed {
		start = append(start, qi{defID, 0})
	} else {
		for _, u := range defs[defID].Upstream {
			if isFailed(statusOf(u.From)) {
				start = append(start, qi{u.From, 1})
			}
		}
	}

	visited := map[string]bool{}
	parentOf := map[string]string{} // p/ reconstruir um caminho até a raiz
	roots := map[string]RCACause{}
	queue := start
	for _, q := range start {
		visited[q.id] = true
	}
	visits := 0
	for len(queue) > 0 && visits < rcaMaxVisit {
		cur := queue[0]
		queue = queue[1:]
		visits++
		curStatus := statusOf(cur.id)
		failedParent, hasFP := hasFailedParent(cur.id)
		if !hasFP {
			// Raiz: falhou sem nenhum ancestral falho.
			d := defs[cur.id]
			reason := "falhou por conta própria (nenhum upstream falho)"
			if curStatus == string(domain.StatusCancelled) {
				reason = "cancelado sem upstream falho (dependência impossível ou cancelamento manual)"
			}
			roots[cur.id] = RCACause{
				DefID: cur.id, Label: d.Label, Team: d.Team, Status: curStatus, Depth: cur.depth, Reason: reason,
			}
			continue
		}
		// Continua subindo pelos pais falhos.
		for _, u := range defs[cur.id].Upstream {
			if !isFailed(statusOf(u.From)) || visited[u.From] {
				continue
			}
			visited[u.From] = true
			parentOf[u.From] = cur.id
			queue = append(queue, qi{u.From, cur.depth + 1})
		}
		_ = failedParent
	}

	// Ordena as raízes por profundidade (mais funda primeiro) e monta a resposta.
	for _, r := range roots {
		rca.Roots = append(rca.Roots, r)
	}
	// insertion sort simples (poucas raízes)
	for i := 1; i < len(rca.Roots); i++ {
		for j := i; j > 0 && rca.Roots[j].Depth > rca.Roots[j-1].Depth; j-- {
			rca.Roots[j], rca.Roots[j-1] = rca.Roots[j-1], rca.Roots[j]
		}
	}

	if len(rca.Roots) == 0 {
		rca.Summary = "Falha detectada, mas sem causa raiz distinta encontrada (ciclo ou dados incompletos)."
		return rca, nil
	}

	// Caminho até a raiz mais funda (reconstruído por parentOf), do alvo pra baixo.
	deepest := rca.Roots[0]
	chain := []string{}
	for id := deepest.DefID; id != ""; id = parentOf[id] {
		chain = append([]string{id}, chain...) // prepend
		if id == deepest.DefID && parentOf[id] == "" {
			// raiz sem parent registrado (partiu do alvo direto)
		}
	}
	// Prefixa o alvo se ele não é a própria raiz.
	if deepest.DefID != defID {
		chain = append([]string{defID}, chain...)
	}
	rca.Chain = chain

	names := make([]string, 0, len(rca.Roots))
	for _, r := range rca.Roots {
		label := r.Label
		if label == "" {
			label = r.DefID
		}
		names = append(names, label)
	}
	if targetFailed && len(rca.Roots) == 1 && rca.Roots[0].DefID == defID {
		rca.Summary = "Este job é a própria causa raiz: " + rca.Roots[0].Reason + "."
	} else if len(rca.Roots) == 1 {
		rca.Summary = fmt.Sprintf("Causa raiz: '%s' (%s), a %d salto(s) upstream.", names[0], rca.Roots[0].Status, rca.Roots[0].Depth)
	} else {
		rca.Summary = fmt.Sprintf("%d causas raiz: %s.", len(rca.Roots), strings.Join(names, ", "))
	}
	return rca, nil
}
