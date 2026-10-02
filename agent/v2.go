package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Dr0nj/regente-agent/journal"
	"github.com/Dr0nj/regente-agent/security"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type v2HTTP struct {
	Base, Token, ID, Caps, Env, Version string
	Slots, Pending                      int
	Client                              *http.Client
}

func (t *v2HTTP) Poll(ctx context.Context, available bool) (*journal.Envelope, error) {
	q := url.Values{"id": {t.ID}, "caps": {t.Caps}, "env": {t.Env}, "protocol": {"2"}, "ver": {t.Version}, "available": {"0"}}
	q.Set("journal", "1")
	q.Set("slots", strconv.Itoa(t.Slots))
	q.Set("pendingLimit", strconv.Itoa(t.Pending))
	q.Set("os", runtime.GOOS)
	q.Set("arch", runtime.GOARCH)
	host, _ := os.Hostname()
	q.Set("host", host)
	if available {
		q.Set("available", "1")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", t.Base+"/api/agent/v2/poll?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+t.Token)
	res, err := t.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == 204 {
		return nil, nil
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("protocol 2 poll: HTTP %d", res.StatusCode)
	}
	var msg journal.Envelope
	if err = json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&msg); err != nil {
		return nil, err
	}
	return &msg, nil
}
func (t *v2HTTP) Post(ctx context.Context, path string, body any) (journal.Receipt, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return journal.Receipt{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", t.Base+"/api/agent/v2/"+path, bytes.NewReader(raw))
	if err != nil {
		return journal.Receipt{}, err
	}
	req.Header.Set("Authorization", "Bearer "+t.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := t.Client.Do(req)
	if err != nil {
		return journal.Receipt{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return journal.Receipt{}, fmt.Errorf("protocol 2 %s: HTTP %d; journal retained", path, res.StatusCode)
	}
	var receipt journal.Receipt
	err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&receipt)
	return receipt, err
}
func runAgentV2(base, token, id, caps, environment, path string, concurrency, pending int, stop <-chan os.Signal) error {
	j, err := journal.Open(path, id)
	if err != nil {
		return err
	}
	defer j.Close()
	j.MaxPending = pending
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-stop:
			cancel()
		case <-ctx.Done():
		}
	}()
	t := &v2HTTP{Base: base, Token: token, ID: id, Caps: caps, Env: environment, Version: agentVersion, Slots: concurrency, Pending: pending, Client: controlClient(10 * time.Second)}
	w := journal.NewWorker(j, t, func(ctx context.Context, e journal.Envelope, emit func(string)) (code int, out string) {
		var def struct {
			ID          string                 `json:"id"`
			Environment string                 `json:"environment"`
			DryRun      bool                   `json:"dryRun"`
			JobType     string                 `json:"jobType"`
			Params      map[string]interface{} `json:"actionConfig"`
			Timeout     int                    `json:"timeout"`
		}
		if json.Unmarshal(e.Definition, &def) != nil {
			return -1, "invalid frozen execution definition"
		}
		prepared, params, redact, err := security.Prepare(ctx, *executionPolicy, *jobSecrets, def.Environment, def.ID, strings.ToUpper(def.JobType), def.Params)
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
		if strings.EqualFold(def.JobType, "HTTP") || strings.EqualFold(def.JobType, "REST") {
			return runRESTContext(ctx, def.Params, def.Timeout, true)
		}
		code, out = executeJob(ctx, def.JobType, def.Params, def.Timeout, emit)
		switch strings.ToUpper(def.JobType) {
		case "FILE_TRANSFER", "DATABASE", "LAMBDA", "BATCH", "GLUE", "STEP_FUNCTION":
			if code != 0 {
				return journal.UnknownExitCode, "external adapter did not confirm completion; verify destination state before resolving"
			}
		}
		return code, out
	}, concurrency)
	defer w.Stop()
	nextCompact := time.Time{}
	for {
		if time.Now().After(nextCompact) {
			if err = j.Compact(time.Now().Add(-7 * 24 * time.Hour)); err != nil {
				return fmt.Errorf("journal retention failed: %w", err)
			}
			nextCompact = time.Now().Add(time.Hour)
		}
		if ctx.Err() != nil {
			return nil
		}
		if err = w.Tick(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("durable execution: %v", err)
			// Erros de storage não podem permitir novo efeito. Rede conserva o journal e reenvia.
			var network *journal.TransportError
			if !errors.As(err, &network) && !errors.Is(err, journal.ErrCapacity) {
				return fmt.Errorf("durable worker stopped; preserve journal: %w", err)
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second):
		}
	}
}
