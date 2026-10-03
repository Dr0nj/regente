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

	"github.com/Dr0nj/regente-server/internal/db"
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
	// db — persistência durável do REGISTRY de capacidade (tabela `resources`).
	// nil = tracker 100% em memória (testes). Ligado no boot via LoadFromDB e
	// nunca mais mutado, então pode ser lido sem o mutex nos helpers de persist.
	db *db.DB
}

func NewResourceTracker() *ResourceTracker {
	return &ResourceTracker{
		capacity: map[string]int{},
		used:     map[string]int{},
		holders:  map[string]map[string]int{},
	}
}

// LoadFromDB liga o tracker à tabela `resources` e RECARREGA as capacidades
// gravadas. Chamar UMA vez no boot, ANTES de RebuildResourcesFromRunning (que só
// reconstrói o USO de jobs RUNNING; a capacidade autoritativa vem daqui). Sem DB
// (testes) o tracker segue 100% em memória. É a metade "ambiente" da persistência
// de recursos — a metade "job" já viaja no snapshot da instance (schemaV19).
func (t *ResourceTracker) LoadFromDB(database *db.DB) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.db = database
	if database == nil {
		return nil
	}
	rows, err := database.Query(`SELECT name, capacity FROM resources`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var cap int
		if rows.Scan(&name, &cap) == nil {
			t.capacity[name] = cap
		}
	}
	return rows.Err()
}

// persist grava (upsert) a capacidade de um recurso na tabela durável.
// Best-effort — chamar SEM o mutex (faz I/O). Sem DB é no-op.
func (t *ResourceTracker) persist(name string, cap int) error {
	if t.db == nil {
		return nil
	}
	_, err := t.db.Exec(`INSERT OR REPLACE INTO resources(name, capacity) VALUES(?,?)`, name, cap)
	return err
}

// O registry em memória só muda depois do commit durável/auditado.
func (t *ResourceTracker) SetCapacity(name string, cap int) error {
	return t.SetCapacityUsing(t.db, name, cap)
}
func (t *ResourceTracker) SetCapacityUsing(database *db.DB, name string, cap int) error {
	if cap < 0 {
		cap = 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if database != nil {
		if _, err := database.Exec(`INSERT OR REPLACE INTO resources(name,capacity) VALUES(?,?)`, name, cap); err != nil {
			return err
		}
	}
	t.capacity[name] = cap
	return nil
}
func (t *ResourceTracker) Delete(name string) error { return t.DeleteUsing(t.db, name) }
func (t *ResourceTracker) DeleteUsing(database *db.DB, name string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.capacity[name]; !ok {
		return fmt.Errorf("resource %q not found", name)
	}
	if used := t.used[name]; used > 0 {
		return fmt.Errorf("resource %q in use (used=%d); release first", name, used)
	}
	if database != nil {
		if _, err := database.Exec(`DELETE FROM resources WHERE name=?`, name); err != nil {
			return err
		}
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
	for name := range want {
		if _, ok := t.capacity[name]; !ok {
			if err := t.persist(name, 1); err != nil {
				return false
			}
			t.capacity[name] = 1
		}
	}
	if len(t.shortfallsLocked(want)) > 0 {
		return false
	}
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
