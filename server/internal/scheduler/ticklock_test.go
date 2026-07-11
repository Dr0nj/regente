package scheduler

import (
	"sync"
	"testing"
)

// TestTickGuard_EmProcesso — a camada em-processo serializa: enquanto um tick
// segura o guard, um segundo tryEnter falha; após release, volta a entrar.
func TestTickGuard_EmProcesso(t *testing.T) {
	g := &tickGuard{} // sem db → só a camada em-processo

	ok1, rel1 := g.tryEnter()
	if !ok1 {
		t.Fatal("1º tryEnter devia entrar")
	}
	if ok2, _ := g.tryEnter(); ok2 {
		t.Fatal("2º tryEnter devia PULAR (tick já em andamento)")
	}
	rel1()
	ok3, rel3 := g.tryEnter()
	if !ok3 {
		t.Fatal("após release, tryEnter devia entrar de novo")
	}
	rel3()
}

// TestTickGuard_Concorrente — sob N goroutines competindo, exatamente UMA entra
// por vez (nunca duas seções críticas simultâneas).
func TestTickGuard_Concorrente(t *testing.T) {
	g := &tickGuard{}
	var inside, maxInside int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, release := g.tryEnter()
			if !ok {
				return // pulou — comportamento esperado sob contenção
			}
			mu.Lock()
			inside++
			if inside > maxInside {
				maxInside = inside
			}
			cur := inside
			mu.Unlock()
			if cur > 1 {
				t.Errorf("dois ticks na seção crítica ao mesmo tempo (inside=%d)", cur)
			}
			mu.Lock()
			inside--
			mu.Unlock()
			release()
		}()
	}
	wg.Wait()
	if maxInside > 1 {
		t.Fatalf("maxInside=%d, esperava 1", maxInside)
	}
}

// TestTickGuard_Nil — guard nil (defensivo) sempre entra.
func TestTickGuard_Nil(t *testing.T) {
	var g *tickGuard
	if ok, _ := g.tryEnter(); !ok {
		t.Fatal("guard nil devia entrar (no-op)")
	}
}
