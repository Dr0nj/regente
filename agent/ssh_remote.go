package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Dr0nj/regente-agent/journal"
	"github.com/Dr0nj/regente-agent/security"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type remoteOutput struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	emit func(string)
}

func (w *remoteOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := min(len(p), journal.MaxOutput-w.buf.Len())
	if n > 0 {
		w.buf.Write(p[:n])
		if w.emit != nil {
			w.emit(strings.ToValidUTF8(string(p[:n]), "�"))
		}
	}
	return len(p), nil
}
func protectedSSHFile(path string) ([]byte, error) {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 1<<20 ||
		(runtime.GOOS != "windows" && st.Mode().Perm()&0077 != 0) {
		return nil, security.ErrPolicy
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, security.ErrPolicy
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 1<<20))
}

// Only v2 calls this executor. No local shell, ssh config, agent forwarding,
// proxy or environment inheritance exists. Commands run on the remote host.
func runRemoteSSH(parent context.Context, params map[string]interface{}, timeoutSec int, emit func(string)) (int, string) {
	if !security.Restricted(parent) {
		return -1, "SSH_REMOTE requires local execution policy"
	}
	str := func(k string) string { v, _ := params[k].(string); return v }
	host, command, user := str("host"), str("command"), str("user")
	if host == "" || command == "" || user == "" || str("strictHostKey") != "yes" {
		return -1, "SSH_REMOTE requires host, user, command and strictHostKey=yes"
	}
	port := "22"
	if v := params["port"]; v != nil && fmt.Sprint(v) != "" {
		port = fmt.Sprint(v)
	}
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(timeoutSec)*time.Second)
	defer cancel()
	address, err := security.PinnedAddress(ctx, host, port)
	if err != nil {
		return -1, "SSH destination denied by execution policy"
	}
	key, err := protectedSSHFile(str("keyPath"))
	if err != nil {
		return -1, "SSH private key unavailable or unprotected"
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return -1, "SSH private key invalid"
	}
	if _, err = protectedSSHFile(str("knownHostsPath")); err != nil {
		return -1, "SSH known_hosts unavailable or unprotected"
	}
	verify, err := knownhosts.New(str("knownHostsPath"))
	if err != nil {
		return -1, "SSH known_hosts invalid"
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		return -1, "SSH destination unavailable before submission"
	}
	defer conn.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	clientConn, channels, requests, err := ssh.NewClientConn(conn, net.JoinHostPort(host, port), &ssh.ClientConfig{
		User: user, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: verify,
	})
	if err != nil {
		return -1, "SSH authentication or host key verification failed before submission"
	}
	client := ssh.NewClient(clientConn, channels, requests)
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return -1, "SSH session unavailable before submission"
	}
	defer session.Close()
	w := &remoteOutput{emit: emit}
	session.Stdout, session.Stderr = w, w
	unknown := func() (int, string) {
		return journal.RemoteUnknownExitCode, "SSH remote completion unconfirmed; inspect destination before resolving or retrying"
	}
	// Once exec is submitted, a failed acknowledgement cannot prove no effect.
	if err = session.Start(command); err != nil {
		return unknown()
	}
	err = session.Wait()
	code := 0
	if err != nil {
		var exit *ssh.ExitError
		if !errors.As(err, &exit) || exit.ExitStatus() < 0 || exit.ExitStatus() >= 255 {
			return unknown()
		}
		code = exit.ExitStatus()
	}
	if ctx.Err() != nil {
		return unknown()
	}
	w.mu.Lock()
	output := strings.ToValidUTF8(w.buf.String(), "�")
	w.mu.Unlock()
	return code, output
}
