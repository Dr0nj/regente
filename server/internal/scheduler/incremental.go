package scheduler

import (
	"container/heap"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/telemetry"
	"sort"
	"strings"
	"sync"
	"time"
)

// Índices são pistas recuperáveis; toda avaliação relê a instância e os claims
// continuam transacionais. Notificações perdidas são recuperadas por keyset.
// Não há retenção de snapshots antigos nem reconstrução a partir do workspace vivo.
type candidateIndex struct {
	mu               sync.Mutex
	plans            map[[32]byte]cachedPlan
	planOrder        [][32]byte
	date, cursor     string
	nextReconcile    time.Time
	pending          map[string]bool
	queue            []string
	queueHead        int
	consumers        map[string]map[string]bool
	tracked          map[string]*candidate
	timers           candidateHeap
	conditions       CondIndex
	pages, evaluated uint64
	reconciledAt     time.Time
}
type candidate struct {
	id    string
	keys  []string
	due   time.Time
	index int
}
type candidateHeap []*candidate

func (h candidateHeap) Len() int { return len(h) }
func (h candidateHeap) Less(i, j int) bool {
	if h[i].due.Equal(h[j].due) {
		return h[i].id < h[j].id
	}
	return h[i].due.Before(h[j].due)
}
func (h candidateHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i]; h[i].index = i; h[j].index = j }
func (h *candidateHeap) Push(x any)   { v := x.(*candidate); v.index = len(*h); *h = append(*h, v) }
func (h *candidateHeap) Pop() any {
	a := *h
	v := a[len(a)-1]
	a[len(a)-1] = nil
	v.index = -1
	*h = a[:len(a)-1]
	return v
}

// Configuração antes de iniciar o serviço. Mantém o avaliador completo para comparação.
func (s *Scheduler) EnableIncremental() { s.incremental = newCandidateIndex() }
func newCandidateIndex() *candidateIndex {
	return &candidateIndex{pending: map[string]bool{}, consumers: map[string]map[string]bool{}, tracked: map[string]*candidate{}}
}
func (s *Scheduler) WakeInstance(id string) {
	if s.incremental != nil && id != "" {
		c := s.incremental
		c.mu.Lock()
		c.enqueue(id)
		c.mu.Unlock()
	}
}
func (c *candidateIndex) enqueue(id string) {
	if !c.pending[id] {
		c.pending[id] = true
		c.queue = append(c.queue, id)
	}
}
func (c *candidateIndex) conditionChanged(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id := range c.consumers[key] {
		c.enqueue(id)
	}
}
func (c *candidateIndex) conditionsChanged(next CondIndex) {
	if next == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range next {
		if !c.conditions[k] {
			for id := range c.consumers[k] {
				c.enqueue(id)
			}
		}
	}
	for k := range c.conditions {
		if !next[k] {
			for id := range c.consumers[k] {
				c.enqueue(id)
			}
		}
	}
	c.conditions = next
}
func (c *candidateIndex) remove(id string) {
	v := c.tracked[id]
	if v == nil {
		return
	}
	for _, key := range v.keys {
		delete(c.consumers[key], id)
		if len(c.consumers[key]) == 0 {
			delete(c.consumers, key)
		}
	}
	if v.index >= 0 {
		heap.Remove(&c.timers, v.index)
	}
	delete(c.tracked, id)
}
func (c *candidateIndex) observe(s *Scheduler, r instRow, defs map[string]domain.JobDefinition, condIdx CondIndex, now time.Time) {
	def, ok := defForInstance(r, defs)
	var blockers []Blocker
	if ok && r.Status == "WAITING" {
		blockers = s.gateInstance(r, def, condIdx, now, true)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evaluated++
	telemetry.Count(telemetry.SchedulerEvaluated, 1)
	c.remove(r.ID)
	if r.Status != "WAITING" && r.Status != "RUNNING" {
		return
	}
	v := &candidate{id: r.ID, index: -1}
	c.tracked[r.ID] = v
	if r.Status == "RUNNING" {
		v.due = now.Add(time.Minute)
	} else {
		if !ok {
			return
		}
		keys := map[string]bool{}
		for _, name := range def.ConditionsIn {
			base, scope := resolveCondScope(name, r.Odate(), s.prevDaily)
			keys[condKey(base, scope)] = true
			keys[condKey(base, "")] = true
		}
		for key := range keys {
			if c.consumers[key] == nil {
				c.consumers[key] = map[string]bool{}
			}
			c.consumers[key][r.ID] = true
			v.keys = append(v.keys, key)
		}
		// Horário e $TIME usam o calendário congelado. Agente/recurso sem evento
		// compartilhado têm retry limitado; waits de condição/confirm usam eventos.
		if now.Before(r.ScheduledAt) {
			v.due = r.ScheduledAt
		} else {
			if len(blockers) == 0 || blockers[0].Kind == GateAgent || blockers[0].Kind == GateResource {
				v.due = now.Add(2 * time.Second)
			}
		}
		if start, ok := orderForceWindowStart(def, r.Odate()); ok && now.Before(start) && (v.due.IsZero() || start.Before(v.due)) {
			v.due = start
		}
	}
	if !v.due.IsZero() {
		heap.Push(&c.timers, v)
	}
}

func (s *Scheduler) tickCandidates(today string, now time.Time) ([]instRow, error) {
	defer telemetry.Measure(telemetry.SchedulerCandidates)()
	c := s.incremental
	if c == nil {
		if _, err := s.db.Exec("DELETE FROM scheduler_changes"); err != nil {
			return nil, err
		}
		return s.readTickInstances(" WHERE i.order_date=? ORDER BY i.scheduled_at,i.created_at,i.id", today)
	}
	c.mu.Lock()
	if c.date != today {
		c.date = today
		c.cursor = ""
		c.nextReconcile = time.Time{}
		c.pending = map[string]bool{}
		c.queue = nil
		c.queueHead = 0
		c.consumers = map[string]map[string]bool{}
		c.tracked = map[string]*candidate{}
		c.timers = nil
		c.conditions = nil
	}
	c.mu.Unlock()
	// Consumo coalescido e limitado. A versão evita apagar evento posterior à
	// leitura. Crash após consumo é recuperado pela passagem keyset completa.
	changes, err := s.db.Query("SELECT instance_id,version FROM scheduler_changes ORDER BY changed_at,instance_id LIMIT 256")
	if err != nil {
		return nil, err
	}
	type change struct {
		id      string
		version int64
	}
	events := []change{}
	for changes.Next() {
		var event change
		if err = changes.Scan(&event.id, &event.version); err != nil {
			break
		}
		events = append(events, event)
	}
	if err == nil {
		err = changes.Err()
	}
	changes.Close()
	if err != nil {
		return nil, err
	}
	if len(events) > 0 {
		tx, err := s.db.Begin()
		if err != nil {
			return nil, err
		}
		for _, event := range events {
			s.WakeInstance(event.id)
			if _, err = tx.Exec("DELETE FROM scheduler_changes WHERE instance_id=? AND version=?", event.id, event.version); err != nil {
				tx.Rollback()
				return nil, err
			}
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
	}

	c.mu.Lock()
	for c.timers.Len() > 0 && !c.timers[0].due.After(now) {
		v := heap.Pop(&c.timers).(*candidate)
		c.enqueue(v.id)
	}
	n := min(len(c.queue)-c.queueHead, 256)
	ids := append([]string(nil), c.queue[c.queueHead:c.queueHead+n]...)
	for i := c.queueHead; i < c.queueHead+n; i++ {
		c.queue[i] = ""
	}
	c.queueHead += n
	if c.queueHead == len(c.queue) {
		c.queue = nil
		c.queueHead = 0
	} else if c.queueHead > 4096 && c.queueHead*2 > len(c.queue) {
		c.queue = append([]string(nil), c.queue[c.queueHead:]...)
		c.queueHead = 0
	}

	for _, id := range ids {
		delete(c.pending, id)
	}
	cursor := c.cursor
	reconcile := !now.Before(c.nextReconcile)
	c.mu.Unlock()
	var out []instRow
	if len(ids) > 0 {
		args := []any{today}
		for _, id := range ids {
			args = append(args, id)
		}
		rows, err := s.readTickInstances(" WHERE i.order_date=? AND i.id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+")", args...)
		if err != nil {
			for _, id := range ids {
				s.WakeInstance(id)
			}
			return nil, err
		}
		out = rows
		found := map[string]bool{}
		for _, r := range rows {
			found[r.ID] = true
		}
		c.mu.Lock()
		for _, id := range ids {
			if !found[id] {
				c.remove(id)
			}
		}
		c.mu.Unlock()
	}
	if reconcile {
		rows, err := s.readTickInstances(" WHERE i.order_date=? AND i.id>? ORDER BY i.id LIMIT 256", today, cursor)
		if err != nil {
			for _, r := range out {
				s.WakeInstance(r.ID)
			}
			return nil, err
		}
		c.mu.Lock()
		c.pages++
		telemetry.Count(telemetry.SchedulerPage, 1)
		if len(rows) < 256 {
			c.cursor = ""
			c.nextReconcile = now.Add(30 * time.Second)
			c.reconciledAt = now
		} else {
			c.cursor = rows[len(rows)-1].ID
		}
		c.mu.Unlock()
		out = append(out, rows...)
	}
	seen := map[string]bool{}
	unique := out[:0]
	for _, r := range out {
		if !seen[r.ID] {
			seen[r.ID] = true
			unique = append(unique, r)
		}
	}
	sort.Slice(unique, func(i, j int) bool {
		if unique[i].ScheduledAt.Equal(unique[j].ScheduledAt) {
			return unique[i].ID < unique[j].ID
		}
		return unique[i].ScheduledAt.Before(unique[j].ScheduledAt)
	})
	return unique, nil
}

// Cache limitado, chave inclui identidade/conteúdo/checksum e a geração viva
// usada exclusivamente para compatibilidade das condições legadas.
type cachedPlan struct {
	def    domain.JobDefinition
	reason string
}

func (s *Scheduler) prepareCandidate(r instRow, live map[string]domain.JobDefinition) instRow {
	if r.Snapshot == "" {
		return r
	}
	s.mu.Lock()
	generation := s.defsGeneration
	s.mu.Unlock()
	key := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s:%s", generation, r.DefID, r.SnapshotChecksum, r.Snapshot)))
	c := s.incremental
	c.mu.Lock()
	plan, ok := c.plans[key]
	c.mu.Unlock()
	if !ok {
		plan.reason = snapshotError(r)
		if plan.reason == "" {
			_ = json.Unmarshal([]byte(r.Snapshot), &plan.def)
			plan.def = domain.ExpandSnapshotConditions(plan.def, live)
		}
		c.mu.Lock()
		if c.plans == nil {
			c.plans = map[[32]byte]cachedPlan{}
		}
		if len(c.planOrder) >= 1024 {
			delete(c.plans, c.planOrder[0])
			c.planOrder = c.planOrder[1:]
		}
		c.plans[key] = plan
		c.planOrder = append(c.planOrder, key)
		c.mu.Unlock()
	}
	r.validated = true
	r.validation = plan.reason
	if plan.reason == "" {
		r.resolved = &plan.def
	}
	return r
}

type IncrementalProgress struct {
	Enabled                 bool
	Pending, Tracked, Plans int
	Pages, Evaluated        uint64
	ReconciledAt            time.Time
}

func (s *Scheduler) IncrementalProgress() IncrementalProgress {
	c := s.incremental
	if c == nil {
		return IncrementalProgress{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return IncrementalProgress{true, len(c.pending), len(c.tracked), len(c.plans), c.pages, c.evaluated, c.reconciledAt}
}
