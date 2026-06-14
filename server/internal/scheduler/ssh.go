package scheduler

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// runSSH executa um job SSH no PRÓPRIO server (C1 — agentless): faz shell-out
// pro cliente `ssh` do SO (OpenSSH), sem precisar de agente no alvo. Streama o
// stdout/stderr como instance_events (kind=output), igual ao agente (B4).
//
// Params (actionConfig):
//   host (obrigatório), command (obrigatório), user, port, keyPath, strictHostKey.
//
// Auth é por chave (BatchMode=yes — nunca pede senha interativa). Use chaves
// do ssh-agent/known config ou aponte keyPath.
func (s *Scheduler) runSSH(id string, def domain.JobDefinition) {
	params := InterpolateParams(def.Params, s.buildVarContext(def, id))
	str := func(k string) string {
		if v, ok := params[k].(string); ok {
			return v
		}
		return ""
	}
	host := str("host")
	command := str("command")
	if host == "" || command == "" {
		s.emitEvent(id, "output", "ssh", "missing 'host' or 'command' param")
		s.FinishInstance(id, domain.StatusNotOK, -1, "missing 'host' or 'command' param")
		return
	}
	user := str("user")
	target := host
	if user != "" {
		target = user + "@" + host
	}

	args := []string{"-o", "BatchMode=yes"}
	strict := str("strictHostKey")
	if strict == "" {
		strict = "accept-new"
	}
	args = append(args, "-o", "StrictHostKeyChecking="+strict)
	if p := str("port"); p != "" {
		args = append(args, "-p", p)
	}
	if k := str("keyPath"); k != "" {
		args = append(args, "-i", k)
	}
	args = append(args, target, command)

	timeoutSec := def.Timeout
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ssh", args...)
	s.emitEvent(id, "submitted", "scheduler", "ssh "+target)

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		s.emitEvent(id, "output", "ssh", "ssh start: "+err.Error())
		s.FinishInstance(id, domain.StatusNotOK, -1, "ssh start: "+err.Error())
		return
	}

	var full strings.Builder
	done := make(chan struct{}, 2)
	stream := func(r interface{ Read([]byte) (int, error) }) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			full.WriteString(line)
			full.WriteString("\n")
			s.emitEvent(id, "output", "ssh", line)
		}
		done <- struct{}{}
	}
	go stream(stdout)
	go stream(stderr)
	<-done
	<-done

	err := cmd.Wait()
	code := 0
	status := domain.StatusOK
	if ctx.Err() == context.DeadlineExceeded {
		code, status = -1, domain.StatusNotOK
		full.WriteString(fmt.Sprintf("(timeout após %ds)\n", timeoutSec))
	} else if err != nil {
		status = domain.StatusNotOK
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
			full.WriteString(err.Error() + "\n")
		}
	}
	s.FinishInstance(id, status, code, full.String())
}
