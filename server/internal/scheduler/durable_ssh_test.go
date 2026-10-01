package scheduler

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/Dr0nj/regente-server/internal/domain"
	"golang.org/x/crypto/ssh"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestI10RealSSHReceiptsLossAndCancellation(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		if os.Getenv("REGENTE_REQUIRE_INTEGRATION") == "1" {
			t.Fatal(err)
		}
		t.Skip("OpenSSH client required")
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.MarshalPrivateKey(private, "synthetic I10 fixture")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key")
	if err = os.WriteFile(keyPath, pem.EncodeToMemory(key), 0600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		u, err := user.Current()
		if err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{keyPath, "/reset"}, {keyPath, "/inheritance:r", "/grant:r", "*" + u.Uid + ":F"}, {keyPath, "/setowner", "*" + u.Uid}} {
			if out, err := exec.Command("icacls", args...).CombinedOutput(); err != nil {
				t.Fatalf("synthetic key ACL: %v %s", err, out)
			}
		}
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if string(k.Marshal()) != string(signer.PublicKey().Marshal()) {
			return nil, fmt.Errorf("unauthorized key")
		}
		return nil, nil
	}}
	cfg.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var effects atomic.Int32
	go func() {
		for {
			conn, err := listener.Accept()
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
						incoming.Reject(ssh.UnknownChannelType, "session required")
						continue
					}
					ch, requests, err := incoming.Accept()
					if err != nil {
						return
					}
					go func() {
						defer ch.Close()
						for r := range requests {
							if r.Type != "exec" {
								r.Reply(false, nil)
								continue
							}
							var cmd struct{ Command string }
							ssh.Unmarshal(r.Payload, &cmd)
							r.Reply(true, nil)
							effects.Add(1)
							switch cmd.Command {
							case "known":
								ch.Write([]byte("remote confirmed"))
								ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
							case "lost":
								c.Close()
							case "wait":
								c.Wait()
							}
							return
						}
					}()
				}
			}()
		}
	}()
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	defs := []domain.JobDefinition{}
	for _, id := range []string{"known", "lost", "wait"} {
		defs = append(defs, domain.JobDefinition{ID: id, Team: "test", JobType: "SSH", AgentID: "SERVER-AGENT", Retries: 3, Params: map[string]interface{}{"host": "127.0.0.1", "port": port, "user": "synthetic", "keyPath": keyPath, "knownHostsPath": filepath.Join(dir, "known_hosts"), "strictHostKey": "no", "command": id}})
	}
	f := durableTest(t, defs...)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err = f.s.StartDurableInternal(ctx, nil, dir, "ssh-node", "test", false, true); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"known", "lost", "wait"} {
		var raw string
		f.d.QueryRow("SELECT definition_snapshot FROM instances WHERE id=?", id+"-2026-09-30").Scan(&raw)
		var def domain.JobDefinition
		json.Unmarshal([]byte(raw), &def)
		f.s.startInstance(id+"-2026-09-30", def)
	}
	awaitDurableState(t, f, "known-2026-09-30", "OK")
	awaitDurableState(t, f, "lost-2026-09-30", "UNCERTAIN")
	deadline := time.Now().Add(5 * time.Second)
	for effects.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if effects.Load() != 3 {
		t.Fatal("remote effect count", effects.Load())
	}
	o, err := f.e.RuntimeOrder("wait-2026-09-30")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.e.CancelFor(o.ID, "operator"); err != nil {
		t.Fatal(err)
	}
	awaitDurableState(t, f, "wait-2026-09-30", "NOTOK")
	var cancelCode int
	var cancelOutput string
	if err = f.d.QueryRow("SELECT a.exit_code,a.result_output FROM execution_attempts a JOIN runtime_orders r ON r.order_id=a.order_id WHERE r.instance_id=?", "wait-2026-09-30").Scan(&cancelCode, &cancelOutput); err != nil || cancelCode != -1 || !strings.Contains(cancelOutput, "cancellation acknowledged") {
		t.Fatal("cancel receipt lost sysout or exit code", cancelCode, cancelOutput, err)
	}
	f.reopen(t)
	if err = f.s.StartDurableInternal(ctx, nil, dir, "ssh-node", "test", false, true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	if effects.Load() != 3 || scalar(t, f.d, "SELECT COUNT(*) FROM execution_attempts") != 3 {
		t.Fatal("remote effect replayed", effects.Load())
	}
}
