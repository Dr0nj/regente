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
// stdout/stderr para instance_output (OL-1, por tentativa/live-tail), igual ao
// agente — não para instance_events (que é o jornal de agendamento).
//
// Params (actionConfig):
//   host (obrigatório), command (obrigatório), user, port, keyPath, strictHostKey.
//
// Auth é por chave (BatchMode=yes — nunca pede senha interativa). Use chaves
// do ssh-agent/known config ou aponte keyPath.
func (s *Scheduler) runSSH(id string, def domain.JobDefinition) {
	params := InterpolateParams(def.Params, s.buildVarContext(def, id))
	str := func(k string) string {
		switch v := params[k].(type) {
		case string:
			return v
		// `port: 22` no YAML chega como número e era silenciosamente DROPADO
		// (schema ADV-1 declara port como scalar — string ou número).
		case int:
			return fmt.Sprintf("%d", v)
		case int64:
			return fmt.Sprintf("%d", v)
		case uint64:
			return fmt.Sprintf("%d", v)
		case float64:
			if v == float64(int64(v)) {
				return fmt.Sprintf("%d", int64(v))
			}
		}
		return ""
	}
	host := str("host")
	command := str("command")
	if host == "" || command == "" {
		s.appendOutput(id, "missing 'host' or 'command' param\n")
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
		s.appendOutput(id, "ssh start: "+err.Error()+"\n")
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
			s.appendOutput(id, line+"\n")
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
