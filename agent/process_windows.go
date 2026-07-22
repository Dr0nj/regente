//go:build windows

package main

import "os/exec"

// configureCancel — no Windows o Cancel padrão do exec.CommandContext
// (Process.Kill) já derruba o processo do PowerShell, e cmdlets como Start-Sleep
// rodam DENTRO dele, sem filho que sobreviva segurando o pipe. O backstop
// cmd.WaitDelay (setado em runCommand) cobre o caso raro de um neto que persista.
// Matar a árvore inteira de forma garantida exigiria um Job Object; fica como
// follow-up, já que o caso comum (cmdlets) não deixa órfãos.
func configureCancel(cmd *exec.Cmd) {
	_ = cmd // sem alteração de SysProcAttr/Cancel: o kill padrão basta aqui.
}
