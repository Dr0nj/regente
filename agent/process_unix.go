//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// configureCancel coloca o comando em seu PRÓPRIO process group e faz o
// cancelamento (timeout OU o KILL do operador via running.kill) sinalizar o
// GRUPO inteiro, não só o shell.
//
// Sem isto, `sh -c "sleep 30"` faz o shell dar fork no comando: o Cancel padrão
// do exec.CommandContext mata só o shell (o filho direto) e o `sleep` fica
// ÓRFÃO vivo, ainda segurando a ponta de escrita do pipe de stdout/stderr. A
// goroutine de cópia dentro de cmd.Wait() nunca vê EOF e Wait pendura até o
// processo órfão morrer sozinho — foi o que travava TestRunCommand_CancelAborts-
// Process na CI Linux (no Windows passava porque Start-Sleep roda DENTRO do
// PowerShell, sem filho que sobreviva).
func configureCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// PID negativo = todo o process group (o shell é o líder do grupo).
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
