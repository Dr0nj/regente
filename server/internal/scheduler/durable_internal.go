package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Dr0nj/regente-agent/journal"
	"github.com/Dr0nj/regente-agent/security"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/execution"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/serveragent"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type internalTransport struct {
	e     *execution.Engine
	agent string
}

func (t internalTransport) Poll(_ context.Context, available bool) (*journal.Envelope, error) {
	msg, err := t.e.ClaimCapacity(t.agent, available)
	if err != nil || msg == nil {
		return nil, err
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	var envelope journal.Envelope
	err = json.Unmarshal(raw, &envelope)
	return &envelope, err
}
func (t internalTransport) Post(_ context.Context, path string, body any) (journal.Receipt, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return journal.Receipt{}, err
	}
	var result execution.Receipt
	switch path {
	case "ack":
		var b struct {
			execution.Identity
			Kind   string
			Reason string
		}
		if err = json.Unmarshal(raw, &b); err == nil {
			b.AgentID = t.agent
			result, err = t.e.AcknowledgeReason(b.Identity, b.Kind, b.Reason)
		}
	case "output":
		var b execution.Output
		if err = json.Unmarshal(raw, &b); err == nil {
			b.AgentID = t.agent
			result, err = t.e.Append(b)
		}
	case "result":
		var b execution.Result
		if err = json.Unmarshal(raw, &b); err == nil {
			b.AgentID = t.agent
			result, err = t.e.Complete(b)
		}
	default:
		err = execution.ErrInvalid
	}
	if err != nil {
		return journal.Receipt{}, err
	}
	raw, err = json.Marshal(result)
	var receipt journal.Receipt
	if err == nil {
		err = json.Unmarshal(raw, &receipt)
	}
	return receipt, err
}
func (s *Scheduler) StartDurableInternal(parent context.Context, presence *hub.Hub, dir, node, version string, httpEnabled, sshEnabled bool) error {
	if !httpEnabled && !sshEnabled {
		return nil
	}
	if s.durable == nil {
		return errors.New("durable execution engine required")
	}
	if !regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$").MatchString(node) {
		return errors.New("durable internal execution requires a stable node-id (1-64 letters, digits, dots, underscores or hyphens)")
	}
	id := "SERVER-AGENT." + node
	caps := []string{execution.Capability}
	if httpEnabled {
		caps = append(caps, "HTTP", "REST")
	}
	if sshEnabled {
		caps = append(caps, "SSH")
	}
	hash := sha256.Sum256([]byte(node))
	path := filepath.Join(dir, fmt.Sprintf("internal-%x.db", hash[:8]))
	j, err := journal.Open(path, id)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO machine_principals(agent_id,environment,capabilities,internal) VALUES(?,?,?,1) ON CONFLICT(agent_id) DO UPDATE SET environment=excluded.environment,capabilities=excluded.capabilities WHERE machine_principals.internal=1", id, s.RuntimePolicy.Environment, strings.Join(caps, ","))
	if err != nil {
		j.Close()
		return err
	}
	var internal bool
	if err = s.db.QueryRow("SELECT internal FROM machine_principals WHERE agent_id=?", id).Scan(&internal); err != nil || !internal {
		j.Close()
		return errors.New("internal principal conflicts with an external credential")
	}
	if err = s.ReportAgentCapacity(id, 4, 128, true); err != nil {
		j.Close()
		return err
	}
	s.internalAgentID = id
	if _, err = s.db.Exec("INSERT INTO agents(id,os,arch,host,version,capabilities,started_at,connected_at,first_seen,last_seen_at,online) VALUES(?,?,?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1) ON CONFLICT(id) DO UPDATE SET version=excluded.version,capabilities=excluded.capabilities,last_seen_at=CURRENT_TIMESTAMP,online=1", id, runtime.GOOS, runtime.GOARCH, node, version, strings.Join(caps, ",")); err != nil {
		j.Close()
		return err
	}
	client := &hub.Client{ID: id, Kind: hub.ClientAgent, Environment: s.RuntimePolicy.Environment, StrictIdentity: true, Send: make(chan []byte, 64), Capabilities: caps, OS: runtime.GOOS, Arch: runtime.GOARCH, Host: node, Version: version, Started: time.Now().Format(time.RFC3339)}
	if presence != nil {
		presence.Register(client)
	}
	ctx, cancel := context.WithCancel(parent)
	worker := journal.NewWorker(j, internalTransport{s.durable, id}, func(ctx context.Context, envelope journal.Envelope, emit func(string)) (code int, out string) {
		var def domain.JobDefinition
		if json.Unmarshal(envelope.Definition, &def) != nil {
			return -1, "invalid frozen execution definition"
		}
		prepared, params, redact, err := security.Prepare(ctx, s.ExecutionPolicyPath, s.JobSecretsFile, def.Environment, def.ID, strings.ToUpper(def.JobType), def.Params)
		if err != nil {
			return -1, err.Error()
		}
		ctx = prepared
		def.Params = params
		defer func() { out = redact(out) }()
		originalEmit := emit
		emit = func(s string) { originalEmit(redact(s)) }
		if def.DryRun {
			return 0, "[dry run] no external effect executed"
		}
		switch strings.ToUpper(def.JobType) {
		case "HTTP", "REST":
			if httpEnabled {
				return serveragent.RunDurableHTTP(ctx, def.Params, def.Timeout)
			}
		case "SSH":
			if sshEnabled {
				return executeDurableSSH(ctx, def, emit)
			}
		}
		return -1, "internal executor capability is disabled"
	}, 4)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer j.Close()
		defer worker.Stop()
		defer cancel()
		if presence != nil {
			defer presence.Unregister(client)
		}
		defer s.db.Exec("UPDATE agents SET online=0 WHERE id=?", id)
		nextCompact := time.Time{}
		for {
			if ctx.Err() != nil {
				return
			}
			select {
			case <-s.quit:
				return
			default:
			}
			if time.Now().After(nextCompact) {
				if err = j.Compact(time.Now().Add(-7 * 24 * time.Hour)); err != nil {
					return
				}
				nextCompact = time.Now().Add(time.Hour)
			}
			if err = worker.Tick(ctx); err != nil {
				var network *journal.TransportError
				if !errors.As(err, &network) && !errors.Is(err, journal.ErrCapacity) {
					s.hub.BroadcastWeb("agent.error", map[string]string{"id": id, "reason": "durable journal unavailable; effects stopped"})
					return
				}
			}
			s.db.Exec("UPDATE agents SET last_seen_at=CURRENT_TIMESTAMP,online=1 WHERE id=?", id)
			s.ReportAgentCapacity(id, 4, 128, true)
			if presence != nil {
				presence.Touch(id)
			}
			select {
			case <-ctx.Done():
				return
			case <-s.quit:
				return
			case <-time.After(250 * time.Millisecond):
			}
		}
	}()
	return nil
}
