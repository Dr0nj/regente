package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Dr0nj/regente-agent/journal"
	"github.com/Dr0nj/regente-agent/security"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestRemoteSSHReceiptsPolicyHostKeyAndCancellation(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(meta ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if meta.User() != "runner" || string(k.Marshal()) != string(signer.PublicKey().Marshal()) {
			return nil, fmt.Errorf("denied")
		}
		return nil, nil
	}}
	cfg.AddHostKey(signer)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	var effects atomic.Int32
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				c, channels, requests, err := ssh.NewServerConn(conn, cfg)
				if err != nil {
					return
				}
				defer c.Close()
				go ssh.DiscardRequests(requests)
				for incoming := range channels {
					if incoming.ChannelType() != "session" {
						incoming.Reject(ssh.UnknownChannelType, "session only")
						continue
					}
					ch, reqs, err := incoming.Accept()
					if err != nil {
						return
					}
					defer ch.Close()
					for req := range reqs {
						if req.Type != "exec" {
							req.Reply(false, nil)
							continue
						}
						var cmd struct{ Command string }
						ssh.Unmarshal(req.Payload, &cmd)
						effects.Add(1)
						req.Reply(true, nil)
						switch cmd.Command {
						case "ok", "exit7":
							ch.Write([]byte("stdout"))
							ch.Stderr().Write([]byte("stderr"))
							code := uint32(0)
							if cmd.Command == "exit7" {
								code = 7
							}
							ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{code}))
						case "wait":
							c.Wait()
						case "lost":
							c.Close()
						}
						return
					}
				}
			}()
		}
	}()
	dir := t.TempDir()
	keyPath, knownPath, policyPath := filepath.Join(dir, "key"), filepath.Join(dir, "known_hosts"), filepath.Join(dir, "policy.json")
	pemKey, err := ssh.MarshalPrivateKey(key, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyPath, pem.EncodeToMemory(pemKey), 0600); err != nil {
		t.Fatal(err)
	}
	known := knownhosts.Line([]string{l.Addr().String()}, signer.PublicKey()) + "\n"
	if err = os.WriteFile(knownPath, []byte(known), 0600); err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(l.Addr().String())
	policy := security.Policy{Environment: "lab", Jobs: map[string]security.Job{"remote": {
		Types: []string{"SSH_REMOTE"}, Destinations: []security.Destination{{Host: "127.0.0.1", Port: port, Networks: []string{"127.0.0.1/32"}}},
		SSH: &security.SSHRule{User: "runner", Commands: []string{"ok", "exit7", "lost", "wait"}, KeyPath: keyPath, KnownHostsPath: knownPath}}}}
	raw, _ := json.Marshal(policy)
	if err = os.WriteFile(policyPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	params := map[string]interface{}{"host": "127.0.0.1", "port": port, "user": "runner", "command": "ok", "strictHostKey": "yes", "keyPath": keyPath, "knownHostsPath": knownPath}
	prepare := func(ctx context.Context) context.Context {
		t.Helper()
		prepared, _, _, err := security.Prepare(ctx, policyPath, "", "lab", "remote", "SSH_REMOTE", params)
		if err != nil {
			t.Fatal(err)
		}
		return prepared
	}
	for _, test := range []struct {
		command string
		code    int
	}{{"ok", 0}, {"exit7", 7}, {"lost", journal.RemoteUnknownExitCode}} {
		params["command"] = test.command
		code, out := runRemoteSSH(prepare(context.Background()), params, 3, nil)
		if code != test.code {
			t.Fatalf("%s: code=%d output=%s", test.command, code, out)
		}
		if test.code >= 0 && (!strings.Contains(out, "stdout") || !strings.Contains(out, "stderr")) {
			t.Fatal(out)
		}
	}
	before := effects.Load()
	for field, bad := range map[string]string{"user": "root", "command": "unapproved", "keyPath": "/other/key", "knownHostsPath": "/other/hosts", "strictHostKey": "no"} {
		old := params[field]
		params[field] = bad
		if _, _, _, err := security.Prepare(context.Background(), policyPath, "", "lab", "remote", "SSH_REMOTE", params); err == nil {
			t.Fatalf("allowed %s", field)
		}
		params[field] = old
	}
	params["command"] = "ok"
	ctx := prepare(context.Background())
	params["host"] = "169.254.169.254"
	if code, _ := runRemoteSSH(ctx, params, 1, nil); code != -1 {
		t.Fatal(code)
	}
	params["host"] = "127.0.0.1"
	_, wrongKey, _ := ed25519.GenerateKey(rand.Reader)
	wrongSigner, _ := ssh.NewSignerFromKey(wrongKey)
	if err = os.WriteFile(knownPath, []byte(knownhosts.Line([]string{l.Addr().String()}, wrongSigner.PublicKey())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _ := runRemoteSSH(ctx, params, 1, nil); code != -1 {
		t.Fatal(code)
	}
	if effects.Load() != before {
		t.Fatal("denial caused effect")
	}
	if err = os.WriteFile(knownPath, []byte(known), 0600); err != nil {
		t.Fatal(err)
	}
	params["command"] = "wait"
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan int, 1)
	prepared := prepare(ctx)
	go func() { code, _ := runRemoteSSH(prepared, params, 5, nil); result <- code }()
	deadline := time.Now().Add(3 * time.Second)
	for effects.Load() == before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case code := <-result:
		if code != journal.RemoteUnknownExitCode {
			t.Fatalf("cancel asserted remote completion: %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("local cancellation did not return")
	}
}
