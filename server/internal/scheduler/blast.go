// Package scheduler — Blast Radius ("se eu cancelar/segurar este job AGORA?").
//
// Diferencial sobre o Control-M: análise de impacto de uma AÇÃO, não do grafo
// estático. Cancelar/segurar um job impede que seus sucessores rodem — em cascata.
// Percorre o grafo de dependências (reverso) a partir do alvo, visitando SÓ os
// jobs do raio (não a daily inteira) → barato mesmo a 100k–1M.
//
// Modelo: um sucessor Y é IMPACTADO se depende do alvo (direta ou transitivamente)
// por uma aresta que NÃO é `always` E ainda não rodou (WAITING/HELD). Arestas
// `always` são satisfeitas de qualquer jeito → não propagam. Jobs que já rodaram
// (RUNNING/terminal) estão além do gate → não são impactados nem propagam.
package scheduler

import (
	"github.com/Dr0nj/regente-server/internal/domain"
)

// blastDetailCap — teto das listas detalhadas (contadores são sempre exatos).
const blastDetailCap = 500

type BlastNode struct {
	DefID  string `json:"defId"`
	Label  string `json:"label,omitempty"`
	Team   string `json:"team,omitempty"`
	Status string `json:"status,omitempty"`
	HasSLA bool   `json:"hasSla,omitempty"`
	Depth  int    `json:"depth"`
}

type BlastCounts struct {
	Downstream    int `json:"downstream"`
	SLAAtRisk     int `json:"slaAtRisk"`
	TeamsAffected int `json:"teamsAffected"`
	MaxDepth      int `json:"maxDepth"`
}

type BlastRadius struct {
	InstanceID string      `json:"instanceId"`
	DefID      string      `json:"definitionId"`
	Status     string      `json:"status"`
	OrderDate  string      `json:"orderDate"`
	Counts     BlastCounts `json:"counts"`
	Downstream []BlastNode `json:"downstream"`
	SLAAtRisk  []BlastNode `json:"slaAtRisk"`
	Truncated  bool        `json:"truncated"`
}

type blastEdge struct {
	to   string
	cond domain.EdgeCondition
}

// BlastRadius calcula o impacto de cancelar/segurar a instance dada: o conjunto de
// jobs ainda-não-rodados (WAITING/HELD) que dependem dela por arestas não-`always`,
// em cascata, mais os SLAs em risco e times afetados.
func (s *Scheduler) BlastRadius(instanceID string) (BlastRadius, error) {
	var defID, status, orderDate string
	err := s.db.QueryRow(
		`SELECT definition_id, status, order_date FROM instances WHERE id=?`, instanceID,
	).Scan(&defID, &status, &orderDate)
	if err != nil {
		return BlastRadius{}, err
	}

	// Grafo reverso (defID → sucessores) a partir das defs vivas. O(arestas), uma vez.
	s.mu.Lock()
	defs := make(map[string]domain.JobDefinition, len(s.defs))
	successors := map[string][]blastEdge{}
	for _, d := range s.defs {
		defs[d.ID] = d
		for _, u := range d.Upstream {
			successors[u.From] = append(successors[u.From], blastEdge{to: d.ID, cond: u.Condition})
		}
	}
	s.mu.Unlock()

	br := BlastRadius{
		InstanceID: instanceID, DefID: defID, Status: status, OrderDate: orderDate,
		Downstream: []BlastNode{}, SLAAtRisk: []BlastNode{},
	}

	// BFS no grafo reverso. depth[def] = distância do alvo. Só propaga por jobs
	// ainda gated (WAITING/HELD); arestas `always` não bloqueiam.
	visited := map[string]bool{defID: true}
	depth := map[string]int{defID: 0}
	queue := []string{defID}
	teams := map[string]bool{}
	statusCache := map[string]string{}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range successors[cur] {
			if e.cond == domain.CondAlways || visited[e.to] {
				continue
			}
			st, ok := statusCache[e.to]
			if !ok {
				st = s.defInstanceStatus(orderDate, e.to)
				statusCache[e.to] = st
			}
			// Só jobs ainda-não-rodados são impedidos (e propagam). Jobs já rodando/
			// terminais estão além do gate → param a cascata.
			if st != string(domain.StatusWaiting) && st != string(domain.StatusHeld) {
				continue
			}
			visited[e.to] = true
			depth[e.to] = depth[cur] + 1
			queue = append(queue, e.to)

			d := defs[e.to]
			node := BlastNode{DefID: e.to, Label: d.Label, Team: d.Team, Status: st, HasSLA: d.SLA != nil, Depth: depth[e.to]}
			br.Counts.Downstream++
			if node.Depth > br.Counts.MaxDepth {
				br.Counts.MaxDepth = node.Depth
			}
			if d.Team != "" {
				teams[d.Team] = true
			}
			if len(br.Downstream) < blastDetailCap {
				br.Downstream = append(br.Downstream, node)
			} else {
				br.Truncated = true
			}
			if node.HasSLA {
				br.Counts.SLAAtRisk++
				if len(br.SLAAtRisk) < blastDetailCap {
					br.SLAAtRisk = append(br.SLAAtRisk, node)
				}
			}
		}
	}
	br.Counts.TeamsAffected = len(teams)
	return br, nil
}

// defInstanceStatus — status "mais determinante" (statusRank) da instance daquela
// def naquele order_date; "" se não há. Lê só a def visitada (não a daily inteira).
func (s *Scheduler) defInstanceStatus(orderDate, defID string) string {
	rows, err := s.db.Query(
		`SELECT status FROM instances WHERE order_date=? AND definition_id=?`, orderDate, defID,
	)
	if err != nil {
		return ""
	}
	defer rows.Close()
	best := ""
	for rows.Next() {
		var st string
		if rows.Scan(&st) != nil {
			continue
		}
		if best == "" || statusRank(st) > statusRank(best) {
			best = st
		}
	}
	if err := rows.Err(); err != nil {
		return "" // parcial mentiria o status "mais determinante"; "" = desconhecido
	}
	return best
}
