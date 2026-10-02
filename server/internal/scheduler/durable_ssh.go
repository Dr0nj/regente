package scheduler

import (
	"bytes"
	"context"
	"fmt"
	"github.com/Dr0nj/regente-agent/journal"
	"github.com/Dr0nj/regente-agent/security"
	"github.com/Dr0nj/regente-server/internal/domain"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

type sshWriter struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	emit func(string)
}

func (w *sshWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	remain := journal.MaxOutput - w.buf.Len()
	if remain > len(p) {
		remain = len(p)
	}
	if remain > 0 {
		w.buf.Write(p[:remain])
	}
	if w.emit != nil {
		w.emit(string(p))
	}
	return len(p), nil
}
func executeDurableSSH(parent context.Context, def domain.JobDefinition, emit func(string)) (int, string) {
	str := func(key string) string {
		v := def.Params[key]
		switch n := v.(type) {
		case string:
			return n
		case int, int64, uint64, float64:
			return fmt.Sprint(n)
		}
		return ""
	}
	host, command := str("host"), str("command")
	if host == "" || command == "" {
		return -1, "missing 'host' or 'command' param"
	}
	port := str("port")
	if port == "" {
		port = "22"
	}
	originalHost := host
	if security.Restricted(parent) {
		if user := str("user"); user != "" && !regexp.MustCompile("^[a-zA-Z0-9_][a-zA-Z0-9_.-]*$").MatchString(user) {
			return -1, "SSH user denied by execution policy"
		}
		pinned, err := security.PinnedAddress(parent, host, port)
		if err != nil {
			return -1, "SSH destination denied by execution policy"
		}
		host, _, _ = net.SplitHostPort(pinned)
		if str("strictHostKey") != "yes" || str("knownHostsPath") == "" {
			return -1, "restricted SSH requires strictHostKey=yes and knownHostsPath"
		}
	}
	target := host
	if user := str("user"); user != "" {
		target = user + "@" + host
	}
	strict := str("strictHostKey")
	if strict == "" {
		strict = "accept-new"
	}
	args := []string{"-o", "IdentitiesOnly=yes", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=" + strict}
	if security.Restricted(parent) {
		args = append(args, "-F", os.DevNull, "-o", "ProxyCommand=none", "-o", "ProxyJump=none", "-o", "PermitLocalCommand=no", "-o", "ForwardAgent=no", "-o", "HostKeyAlias="+originalHost)
	}
	if known := str("knownHostsPath"); known != "" {
		args = append(args, "-o", "UserKnownHostsFile="+known)
	}
	if port := str("port"); port != "" {
		args = append(args, "-p", port)
	}
	if key := str("keyPath"); key != "" {
		args = append(args, "-i", key)
	}
	args = append(args, target, command)
	timeout := def.Timeout
	if timeout <= 0 {
		timeout = 300
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(timeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Env = security.CleanEnvironment()
	journal.ConfigureCancel(cmd)
	cmd.WaitDelay = 5 * time.Second
	writer := &sshWriter{emit: emit}
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err := cmd.Start(); err != nil {
		return -1, "ssh start: " + err.Error()
	}
	if err := journal.RecordProcess(ctx, cmd.Process.Pid); err != nil {
		if cmd.Cancel != nil {
			_ = cmd.Cancel()
		} else {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		return journal.UnknownExitCode, "SSH process identity could not be persisted; external outcome unknown"
	}
	err := cmd.Wait()
	writer.mu.Lock()
	output := writer.buf.String()
	writer.mu.Unlock()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	if code >= 255 || code < 0 || ctx.Err() != nil {
		return journal.UnknownExitCode, "SSH transport ended without a remote completion receipt; external effect outcome unknown"
	}
	if err != nil && code == -1 {
		output += "\n" + err.Error()
	}
	return code, strings.ToValidUTF8(output, "�")
}
