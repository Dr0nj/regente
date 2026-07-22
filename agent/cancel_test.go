package main

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// longSleepCmd — um comando que dorme MUITO mais que o teste, para provar que o
// cancel aborta o processo (senão o teste estouraria o próprio deadline).
func longSleepCmd() string {
	if runtime.GOOS == "windows" {
		return "Start-Sleep -Seconds 30"
	}
	return "sleep 30"
}

// O cancel (o mesmo efeito de running.kill: cancelar o ctx) ABORTA o processo em
// execução via exec.CommandContext e o runner devolve falha (code != 0) — é o
// que faz "clico em Cancel e ele mata o script".
func TestRunCommand_CancelAbortsProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		code, _ := runCommand(ctx, map[string]interface{}{"command": longSleepCmd()}, 30, nil)
		done <- code
	}()
	time.Sleep(300 * time.Millisecond) // deixa o processo subir
	cancel()                           // = frame "cancel" → running.kill

	select {
	case code := <-done:
		if code == 0 {
			t.Fatalf("comando cancelado deveria falhar (code != 0), veio %d", code)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("runCommand não retornou após cancel — o processo não foi morto")
	}
}

// running.kill invoca o cancel registrado e é no-op para id inexistente.
func TestRunRegistry_KillInvokesRegisteredCancel(t *testing.T) {
	called := make(chan struct{}, 1)
	running.add("cancel-test-i1", func() { called <- struct{}{} })
	defer running.remove("cancel-test-i1")

	running.kill("cancel-test-i1")
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("kill deveria invocar o cancel registrado")
	}

	running.kill("cancel-test-inexistente") // no-op — não pode panicar
}
