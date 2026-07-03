// Package scheduler — Job Neighborhood (Control-M "Neighborhood" view).
//
// Diferencial de observabilidade: dado UM job, mostra o grafo LOCAL ao redor —
// ancestrais (de quem depende) e descendentes (quem depende dele) até `radius`
// saltos, cada um com o status da instância no dia. É o "zoom" de contexto que o
// operador quer antes de agir num job: quem me trava, quem eu travo.
//
// Read-only, barato: visita só a vizinhança (não a daily inteira), reusa o grafo
// de deps e `defInstanceStatus`. Não toca o hot path (tick/dispatch).
package scheduler

import (
	"github.com/Dr0nj/regente-server/internal/domain"
)

// neighborhoodMaxRadius — teto de saltos (defesa contra grafos densos).
const neighborhoodMaxRadius = 4

// neighborhoodDetailCap — teto de nós por direção (o count segue exato).
const neighborhoodDetailCap = 500

type NeighborNode struct {
	DefID     string `json:"defId"`
	Label     string `json:"label,omitempty"`
	Team      string `json:"team,omitempty"`
	Status    string `json:"status,omitempty"` // status da instance no dia; "" se não ordenado
	Depth     int    `json:"depth"`            // saltos até o alvo (1 = vizinho direto)
	Condition string `json:"condition,omitempty"`
	HasSLA    bool   `json:"hasSla,omitempty"`
}

type Neighborhood struct {
	InstanceID string         `json:"instanceId"`
	DefID      string         `json:"definitionId"`
	Label      string         `json:"label,omitempty"`
	Team       string         `json:"team,omitempty"`
	Status     string         `json:"status"`
	OrderDate  string         `json:"orderDate"`
	Radius     int            `json:"radius"`
	Upstream   []NeighborNode `json:"upstream"`   // ancestrais (de quem o alvo depende)
	Downstream []NeighborNode `json:"downstream"` // descendentes (quem depende do alvo)
	Truncated  bool           `json:"truncated"`
}

// Neighborhood devolve a vizinhança do job da instance dada até `radius` saltos em
// cada direção. radius<=0 vira 1; acima do teto é clampado.
func (s *Scheduler) Neighborhood(instanceID string, radius int) (Neighborhood, error) {
	var defID, status, orderDate string
	err := s.db.QueryRow(
		`SELECT definition_id, status, order_date FROM instances WHERE id=?`, instanceID,
	).Scan(&defID, &status, &orderDate)
	if err != nil {
		return Neighborhood{}, err
	}
	if radius <= 0 {
		radius = 1
	}
	if radius > neighborhoodMaxRadius {
		radius = neighborhoodMaxRadius
	}

	// Grafos direto (def → upstreams) e reverso (def → sucessores), uma passada.
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

	nb := Neighborhood{
		InstanceID: instanceID, DefID: defID, Status: status, OrderDate: orderDate, Radius: radius,
		Upstream: []NeighborNode{}, Downstream: []NeighborNode{},
	}
	if d, ok := defs[defID]; ok {
		nb.Label, nb.Team = d.Label, d.Team
	}
	statusCache := map[string]string{}
	statusOf := func(id string) string {
		st, ok := statusCache[id]
		if !ok {
			st = s.defInstanceStatus(orderDate, id)
			statusCache[id] = st
		}
		return st
	}

	// BFS upstream: segue as arestas de dependência do alvo pra trás (pais).
	{
		visited := map[string]bool{defID: true}
		type qi struct {
			id    string
			depth int
		}
		queue := []qi{{defID, 0}}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			if cur.depth >= radius {
				continue
			}
			for _, u := range defs[cur.id].Upstream {
				if visited[u.From] {
					continue
				}
				visited[u.From] = true
				d := defs[u.From]
				node := NeighborNode{
					DefID: u.From, Label: d.Label, Team: d.Team, Status: statusOf(u.From),
					Depth: cur.depth + 1, HasSLA: d.SLA != nil,
				}
				if cur.id == defID { // vizinho direto: rotula a condição da aresta
					node.Condition = string(u.Condition)
				}
				if len(nb.Upstream) < neighborhoodDetailCap {
					nb.Upstream = append(nb.Upstream, node)
				} else {
					nb.Truncated = true
				}
				queue = append(queue, qi{u.From, cur.depth + 1})
			}
		}
	}

	// BFS downstream: segue o grafo reverso (sucessores).
	{
		visited := map[string]bool{defID: true}
		type qi struct {
			id    string
			depth int
		}
		queue := []qi{{defID, 0}}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			if cur.depth >= radius {
				continue
			}
			for _, e := range successors[cur.id] {
				if visited[e.to] {
					continue
				}
				visited[e.to] = true
				d := defs[e.to]
				node := NeighborNode{
					DefID: e.to, Label: d.Label, Team: d.Team, Status: statusOf(e.to),
					Depth: cur.depth + 1, HasSLA: d.SLA != nil,
				}
				if cur.id == defID {
					node.Condition = string(e.cond)
				}
				if len(nb.Downstream) < neighborhoodDetailCap {
					nb.Downstream = append(nb.Downstream, node)
				} else {
					nb.Truncated = true
				}
				queue = append(queue, qi{e.to, cur.depth + 1})
			}
		}
	}

	return nb, nil
}
