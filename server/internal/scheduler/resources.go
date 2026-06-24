// Package scheduler — F15 Resources/Quotas (Control-M Quantitative Resources).
//
// Tracker em memória: nome → capacidade total + uso corrente.
// Pré-start: TryAcquire(def.Resources). Sucesso → start; falha → fica WAITING
// e tenta no próximo tick. Pós-finish: Release(def.Resources).
//
// Configuração inicial via settings.yaml ou via API. Se um recurso solicitado
// não existe no registry, é criado on-the-fly com capacidade default 1
// (compatível com simples gating "no mais 1 por vez").
package scheduler

import (
	"fmt"
	"sync"
)

type ResourceState struct {
	Name     string `json:"name"`
	Capacity int    `json:"capacity"`
	Used     int    `json:"used"`
}

// ResourceShortfall — um recurso de um pedido que NÃO cabe agora. Usado pelo
// Explain ("por que não rodou") e pelo gate read-only do scheduler.
type ResourceShortfall struct {
	Name     string `json:"name"`
	Want     int    `json:"want"`
	Used     int    `json:"used"`
	Capacity int    `json:"capacity"`
}

type ResourceTracker struct {
	mu       sync.Mutex
	capacity map[string]int
	used     map[string]int
	holders  map[string]map[string]int // instanceId → resource → qty (para release)
}

func NewResourceTracker() *ResourceTracker {
	return &ResourceTracker{
		capacity: map[string]int{},
		used:     map[string]int{},
		holders:  map[string]map[string]int{},
	}
}

// SetCapacity define ou atualiza capacidade de um recurso.
func (t *ResourceTracker) SetCapacity(name string, cap int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if cap < 0 {
		cap = 0
	}
	t.capacity[name] = cap
}

// Delete remove um recurso do registry. Recusa se houver uso ativo (used > 0)
// para evitar inconsistência com instances rodando que detêm a quantia.
func (t *ResourceTracker) Delete(name string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.capacity[name]; !ok {
		return fmt.Errorf("resource %q not found", name)
	}
	if t.used[name] > 0 {
		return fmt.Errorf("resource %q in use (used=%d); release first", name, t.used[name])
	}
	delete(t.capacity, name)
	delete(t.used, name)
	return nil
}

// shortfallsLocked — recursos de `want` que NÃO cabem agora. Chamar com mu travado.
// `cap` default é 1 quando o recurso é desconhecido (mesma semântica do TryAcquire),
// mas NÃO persiste no registry — read-only por design (o Explain não pode mutar estado).
func (t *ResourceTracker) shortfallsLocked(want map[string]int) []ResourceShortfall {
	var out []ResourceShortfall
	for name, qty := range want {
		cap, ok := t.capacity[name]
		if !ok {
			cap = 1
		}
		if t.used[name]+qty > cap {
			out = append(out, ResourceShortfall{Name: name, Want: qty, Used: t.used[name], Capacity: cap})
		}
	}
	return out
}

// Shortfalls — read-only: recursos que faltam pra `want` caber. Vazio = cabe.
// FONTE ÚNICA do "está bloqueado por recurso?" compartilhada com TryAcquire e o
// Explain — adicionar semântica nova de recurso aqui reflete nos dois.
func (t *ResourceTracker) Shortfalls(want map[string]int) []ResourceShortfall {
	if len(want) == 0 {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.shortfallsLocked(want)
}

// TryAcquire tenta reservar todos os recursos atomicamente. All-or-nothing.
func (t *ResourceTracker) TryAcquire(instanceID string, want map[string]int) bool {
	if len(want) == 0 {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	// cria recursos desconhecidos com capacidade default 1 (gating "no máx 1 por vez").
	for name := range want {
		if _, ok := t.capacity[name]; !ok {
			t.capacity[name] = 1
		}
	}
	if len(t.shortfallsLocked(want)) > 0 {
		return false
	}
	// commit
	if t.holders[instanceID] == nil {
		t.holders[instanceID] = map[string]int{}
	}
	for name, qty := range want {
		t.used[name] += qty
		t.holders[instanceID][name] += qty
	}
	return true
}

// Reacquire registra o uso de um instance que JÁ está rodando — usado no rebuild
// pós-restart/failover. NÃO checa capacidade (a instance já detém o recurso no
// mundo real; reprovar aqui seria mentir sobre o estado). Idempotente por
// instanceID: zera o registro anterior antes de re-somar. Garante que o recurso
// exista no registry (cria com a quantia detida se for desconhecido).
func (t *ResourceTracker) Reacquire(instanceID string, held map[string]int) {
	if len(held) == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if prev := t.holders[instanceID]; prev != nil {
		for name, qty := range prev {
			if t.used[name] -= qty; t.used[name] < 0 {
				t.used[name] = 0
			}
		}
	}
	t.holders[instanceID] = map[string]int{}
	for name, qty := range held {
		if _, ok := t.capacity[name]; !ok {
			t.capacity[name] = qty
		}
		t.used[name] += qty
		t.holders[instanceID][name] += qty
	}
}

// Release libera tudo o que `instanceID` segura. Idempotente.
func (t *ResourceTracker) Release(instanceID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	held := t.holders[instanceID]
	for name, qty := range held {
		t.used[name] -= qty
		if t.used[name] < 0 {
			t.used[name] = 0
		}
	}
	delete(t.holders, instanceID)
}

// Snapshot devolve estado atual para a UI.
func (t *ResourceTracker) Snapshot() []ResourceState {
	t.mu.Lock()
	defer t.mu.Unlock()
	names := map[string]bool{}
	for n := range t.capacity {
		names[n] = true
	}
	for n := range t.used {
		names[n] = true
	}
	out := []ResourceState{}
	for n := range names {
		out = append(out, ResourceState{Name: n, Capacity: t.capacity[n], Used: t.used[n]})
	}
	return out
}
