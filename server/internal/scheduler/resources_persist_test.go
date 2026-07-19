package scheduler

import "testing"

// TestResourceRegistryPersistsAcrossRestart — o registry de capacidade (metade
// "ambiente" dos recursos) tem que sobreviver a restart do server. Antes da
// schemaV20 o ResourceTracker era 100% em memória e reiniciar zerava tudo o que o
// operador tinha configurado no painel Recursos. Simula o restart trocando o
// tracker por um novo ligado ao MESMO banco.
func TestResourceRegistryPersistsAcrossRestart(t *testing.T) {
	database := newTestDB(t)

	// "Boot 1": operador configura via painel (SetCapacity) e um job de passagem
	// cria um recurso desconhecido (TryAcquire → cap default 1).
	t1 := NewResourceTracker()
	if err := t1.LoadFromDB(database); err != nil {
		t.Fatalf("load 1: %v", err)
	}
	t1.SetCapacity("slots", 5)
	if !t1.TryAcquire("inst-1", map[string]int{"sap": 1}) {
		t.Fatalf("acquire sap")
	}

	// "Restart": tracker novo, mesmo banco. Só a CAPACIDADE é durável (o uso é
	// reconstruído à parte por RebuildResourcesFromRunning), então checamos caps.
	t2 := NewResourceTracker()
	if err := t2.LoadFromDB(database); err != nil {
		t.Fatalf("load 2: %v", err)
	}
	caps := map[string]int{}
	for _, r := range t2.Snapshot() {
		caps[r.Name] = r.Capacity
	}
	if caps["slots"] != 5 {
		t.Errorf("slots capacity após restart = %d, quer 5", caps["slots"])
	}
	if caps["sap"] != 1 {
		t.Errorf("sap (auto-criado) capacity após restart = %d, quer 1", caps["sap"])
	}

	// Delete também é durável: some após o próximo restart.
	if err := t2.Delete("slots"); err != nil {
		t.Fatalf("delete slots: %v", err)
	}
	t3 := NewResourceTracker()
	if err := t3.LoadFromDB(database); err != nil {
		t.Fatalf("load 3: %v", err)
	}
	for _, r := range t3.Snapshot() {
		if r.Name == "slots" {
			t.Errorf("slots ainda presente após delete + restart")
		}
	}
}
