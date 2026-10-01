//go:build !windows

package main

import (
	"github.com/Dr0nj/regente-agent/journal"
	"os/exec"
)

func configureCancel(cmd *exec.Cmd) { journal.ConfigureCancel(cmd) }
