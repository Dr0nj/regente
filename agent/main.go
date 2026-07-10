// regente-agent — braço executor local.
//
// Conecta ao regente-server via WebSocket (conexão outbound), anuncia
// capabilities (COMMAND, REST, ...), recebe mensagens "dispatch" com
// um JobInstance e executa localmente. Devolve o resultado via mensagem
// "result" na mesma conexão.
//
// Uso:
//
//	regente-agent -server ws://localhost:8080/ws/agent -token dev-token -id agent-macbook
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

// agentVersion — versão reportada na tela de Agentes (handshake). processStarted
// marca o início do processo, pra calcular uptime no servidor/UI.
const agentVersion = "0.1.0"

var processStarted = time.Now()

func main() {
	var (
		server    = flag.String("server", "ws://localhost:8080/ws/agent", "regente-server WebSocket URL")
		token     = flag.String("token", envOr("REGENTE_TOKEN", "dev-token"), "Bearer token")
		agentID   = flag.String("id", hostnameOr("agent-local"), "Agent ID (unique)")
		caps      = flag.String("caps", "COMMAND,SCRIPT,HTTP,REST,WASM,DATABASE,FILE_WATCH", "Comma-separated capabilities advertised")
		agentEnv  = flag.String("env", envOr("REGENTE_AGENT_ENV", ""), "Ambiente/site deste agente (ADV-2; ex.: prod, dc-sp). Vazio = generalista; job com environment só roteia pra agente do mesmo env")
		transport = flag.String("transport", envOr("REGENTE_AGENT_TRANSPORT", "ws"), "Transporte: ws (WebSocket) | http (long-poll, serverless-friendly)")
	)
	flag.Parse()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Fase 2 — transporte HTTP long-poll: control plane stateless (scale-to-zero).
	if strings.EqualFold(*transport, "http") {
		base := httpBase(*server)
		log.Printf("regente-agent id=%s caps=%s env=%q transport=http -> %s", *agentID, *caps, *agentEnv, base)
		runAgentHTTP(base, *token, *agentID, *caps, *agentEnv, stop)
		return
	}

	u, err := url.Parse(*server)
	if err != nil {
		log.Fatalf("parse url: %v", err)
	}
	q := u.Query()
	q.Set("token", *token)
	q.Set("id", *agentID)
	q.Set("caps", *caps)
	if *agentEnv != "" {
		q.Set("env", *agentEnv) // ADV-2 — roteamento por ambiente
	}
	// Metadata pra tela de Agentes (OS/host/versão/uptime).
	q.Set("os", runtime.GOOS)
	q.Set("arch", runtime.GOARCH)
	if h, err := os.Hostname(); err == nil {
		q.Set("host", h)
	}
	q.Set("ver", agentVersion)
	q.Set("started", processStarted.Format(time.RFC3339))
	u.RawQuery = q.Encode()

	log.Printf("regente-agent id=%s caps=%s transport=ws -> %s", *agentID, *caps, *server)

	for {
		select {
		case <-stop:
			log.Println("shutting down")
			return
		default:
		}
		if err := runAgent(u.String()); err != nil {
			log.Printf("connection lost: %v (reconnect in 3s)", err)
			select {
			case <-stop:
				return
			case <-time.After(3 * time.Second):
			}
		}
	}
}

func runAgent(wsURL string) error {
	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return err
	}
	defer c.Close()
	log.Println("connected")

	// Escritas no ws vêm de várias goroutines (heartbeat, result, output
	// streaming) — serializa com mutex (gorilla não é safe p/ writes concorrentes).
	var writeMu sync.Mutex
	send := func(v interface{}) error {
		raw, _ := json.Marshal(v)
		writeMu.Lock()
		defer writeMu.Unlock()
		return c.WriteMessage(websocket.TextMessage, raw)
	}

	// Heartbeat a cada 30s
	hbDone := make(chan struct{})
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-hbDone:
				return
			case <-t.C:
				if err := send(map[string]string{"event": "heartbeat"}); err != nil {
					return
				}
			}
		}
	}()
	defer close(hbDone)

	for {
		_, msg, err := c.ReadMessage()
		if err != nil {
			return err
		}
		var job struct {
			Event      string                 `json:"event"`
			InstanceID string                 `json:"instanceId"`
			JobType    string                 `json:"jobType"`
			Params     map[string]interface{} `json:"params"`
			Timeout    int                    `json:"timeout"`
			PingID     string                 `json:"pingId"`
		}
		if err := json.Unmarshal(msg, &job); err != nil {
			continue
		}
		// Ping ativo (health) — responde na hora pra o servidor medir latência.
		if job.Event == "ping" {
			_ = send(map[string]string{"event": "pong", "pingId": job.PingID})
			continue
		}
		if job.Event != "dispatch" {
			continue
		}
		log.Printf("dispatch instance=%s jobType=%s", job.InstanceID, job.JobType)
		go func() {
			// emit streama stdout/stderr em chunks durante a execução (B4).
			emit := func(chunk string) {
				_ = send(map[string]interface{}{
					"event":      "output",
					"instanceId": job.InstanceID,
					"chunk":      chunk,
				})
			}
			code, out := executeJob(job.JobType, job.Params, job.Timeout, emit)
			if err := send(map[string]interface{}{
				"event":      "result",
				"instanceId": job.InstanceID,
				"exitCode":   code,
				"output":     out,
			}); err != nil {
				log.Printf("send result: %v", err)
			}
			log.Printf("instance=%s done exitCode=%d", job.InstanceID, code)
		}()
	}
}

// httpBase converte a -server (ws://…/ws/agent) na origem HTTP do control plane.
// Aceita também http(s):// direto.
func httpBase(server string) string {
	b := server
	b = strings.Replace(b, "wss://", "https://", 1)
	b = strings.Replace(b, "ws://", "http://", 1)
	b = strings.TrimSuffix(b, "/ws/agent")
	return strings.TrimRight(b, "/")
}

// runAgentHTTP — loop de long-poll (transporte HTTP, Fase 2). Resiliente:
// erros de rede viram retry com backoff curto; não derruba o processo.
func runAgentHTTP(base, token, id, caps, env string, stop <-chan os.Signal) {
	pollURL := base + "/api/agent/poll?id=" + url.QueryEscape(id) + "&caps=" + url.QueryEscape(caps)
	if env != "" {
		pollURL += "&env=" + url.QueryEscape(env) // ADV-2
	}
	// Client com timeout > janela de long-poll do server (25s) para não cortar.
	client := &http.Client{Timeout: 35 * time.Second}

	post := func(path string, v interface{}) {
		raw, _ := json.Marshal(v)
		req, err := http.NewRequest("POST", base+path, bytes.NewReader(raw))
		if err != nil {
			return
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		if res, err := http.DefaultClient.Do(req); err == nil {
			res.Body.Close()
		} else {
			log.Printf("post %s: %v", path, err)
		}
	}

	for {
		select {
		case <-stop:
			log.Println("shutting down")
			return
		default:
		}

		req, _ := http.NewRequest("GET", pollURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := client.Do(req)
		if err != nil {
			log.Printf("poll: %v (retry in 3s)", err)
			if sleepOrStop(stop, 3*time.Second) {
				return
			}
			continue
		}
		if res.StatusCode == http.StatusNoContent {
			res.Body.Close()
			continue // nada pendente; re-polla imediatamente
		}
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			log.Printf("poll: HTTP %d (retry in 3s)", res.StatusCode)
			if sleepOrStop(stop, 3*time.Second) {
				return
			}
			continue
		}
		raw, _ := io.ReadAll(res.Body)
		res.Body.Close()

		var job struct {
			Event      string                 `json:"event"`
			InstanceID string                 `json:"instanceId"`
			JobType    string                 `json:"jobType"`
			Params     map[string]interface{} `json:"params"`
			Timeout    int                    `json:"timeout"`
		}
		if err := json.Unmarshal(raw, &job); err != nil || job.Event != "dispatch" {
			continue
		}
		log.Printf("dispatch instance=%s jobType=%s", job.InstanceID, job.JobType)
		go func() {
			emit := func(chunk string) {
				post("/api/agent/output", map[string]interface{}{"instanceId": job.InstanceID, "chunk": chunk})
			}
			code, out := executeJob(job.JobType, job.Params, job.Timeout, emit)
			post("/api/agent/result", map[string]interface{}{"instanceId": job.InstanceID, "exitCode": code, "output": out})
			log.Printf("instance=%s done exitCode=%d", job.InstanceID, code)
		}()
	}
}

// sleepOrStop espera d ou o sinal de parada; retorna true se foi parada.
func sleepOrStop(stop <-chan os.Signal, d time.Duration) bool {
	select {
	case <-stop:
		return true
	case <-time.After(d):
		return false
	}
}

func executeJob(jobType string, params map[string]interface{}, timeoutSec int, emit func(string)) (int, string) {
	switch strings.ToUpper(jobType) {
	case "COMMAND":
		return runCommand(params, timeoutSec, emit)
	case "SCRIPT":
		return runScript(params, timeoutSec, emit)
	case "HTTP", "REST":
		return runREST(params, timeoutSec)
	case "FILE_WATCH", "FILEWATCH":
		return runFileWatch(params, timeoutSec, emit)
	case "DATABASE", "DB":
		return runDatabase(params, timeoutSec, emit)
	case "WASM":
		return runWASM(params, timeoutSec, emit)
	case "K8S_JOB", "K8S":
		return runK8sJob(params, timeoutSec, emit)
	case "LAMBDA", "AWS_LAMBDA":
		return runLambdaJob(params, timeoutSec, emit)
	case "GCP_RUN", "CLOUD_RUN_JOB":
		return runCloudRunJob(params, timeoutSec, emit)
	default:
		return -1, fmt.Sprintf("unsupported jobType %q", jobType)
	}
}

// streamWriter acumula a saída do processo num buffer E emite cada chunk em
// tempo real (B4 — stream de stdout/stderr pro detalhe da instance).
type streamWriter struct {
	buf  *bytes.Buffer
	emit func(string)
}

func (w *streamWriter) Write(p []byte) (int, error) {
	w.buf.Write(p)
	if w.emit != nil {
		w.emit(string(p))
	}
	return len(p), nil
}

// runScript — executa um arquivo de script no OS do agent.
// Params: scriptPath (obrigatório), args (string, opcional), cwd (opcional).
// Resolve o interpretador pela extensão: .ps1→powershell, .bat/.cmd→cmd,
// .sh→sh; senão executa o próprio arquivo.
func runScript(params map[string]interface{}, timeoutSec int, emit func(string)) (int, string) {
	path, _ := params["scriptPath"].(string)
	if path == "" {
		return -1, "missing 'scriptPath' param"
	}
	args, _ := params["args"].(string)
	var name string
	var argv []string
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".ps1"):
		name = "powershell"
		argv = []string{"-ExecutionPolicy", "Bypass", "-File", path}
	case strings.HasSuffix(lower, ".bat"), strings.HasSuffix(lower, ".cmd"):
		name = "cmd"
		argv = []string{"/c", path}
	case strings.HasSuffix(lower, ".sh"):
		name = "sh"
		argv = []string{path}
	default:
		name = path
		argv = []string{}
	}
	if args != "" {
		argv = append(argv, strings.Fields(args)...)
	}
	cmd := exec.Command(name, argv...)
	var buf bytes.Buffer
	sw := &streamWriter{buf: &buf, emit: emit}
	cmd.Stdout = sw
	cmd.Stderr = sw
	if cwd, ok := params["cwd"].(string); ok && cwd != "" {
		cmd.Dir = cwd
	}
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	timer := time.AfterFunc(time.Duration(timeoutSec)*time.Second, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	defer timer.Stop()
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
			buf.WriteString("\n" + err.Error())
		}
	}
	return code, buf.String()
}

// runCommand — executa um comando shell no OS do agent.
// Params: command (string, obrigatório), cwd (string, opcional).
func runCommand(params map[string]interface{}, timeoutSec int, emit func(string)) (int, string) {
	cmdStr, _ := params["command"].(string)
	if cmdStr == "" {
		return -1, "missing 'command' param"
	}
	var shell, flagArg string
	if runtime.GOOS == "windows" {
		shell, flagArg = "powershell", "-Command"
	} else {
		shell, flagArg = "sh", "-c"
	}
	cmd := exec.Command(shell, flagArg, cmdStr)
	var buf bytes.Buffer
	sw := &streamWriter{buf: &buf, emit: emit}
	cmd.Stdout = sw
	cmd.Stderr = sw
	if cwd, ok := params["cwd"].(string); ok && cwd != "" {
		cmd.Dir = cwd
	}
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	timer := time.AfterFunc(time.Duration(timeoutSec)*time.Second, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	defer timer.Stop()
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = -1
			buf.WriteString("\n" + err.Error())
		}
	}
	return code, buf.String()
}

// runREST — executa uma chamada HTTP.
// Params: method (default GET), url (obrigatório), headers (map), body (string),
//
//	expectStatus ([]int opcional — se definido e não bater, vira falha).
func runREST(params map[string]interface{}, timeoutSec int) (int, string) {
	method, _ := params["method"].(string)
	if method == "" {
		method = "GET"
	}
	urlStr, _ := params["url"].(string)
	if urlStr == "" {
		return -1, "missing 'url' param"
	}
	var body io.Reader
	if b, ok := params["body"].(string); ok && b != "" {
		body = strings.NewReader(b)
	}
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	req, err := http.NewRequest(strings.ToUpper(method), urlStr, body)
	if err != nil {
		return -1, err.Error()
	}
	if headers, ok := params["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			if sv, ok := v.(string); ok {
				req.Header.Set(k, sv)
			}
		}
	}
	res, err := client.Do(req)
	if err != nil {
		return -1, err.Error()
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	code := 0
	if res.StatusCode >= 400 {
		code = res.StatusCode
	}
	// expectStatus override — aceita lista, escalar OU string CSV "200,204"
	// (schema ADV-1: intlist). `expectStatus: 200` no YAML chegava como número
	// solto e era IGNORADO; a UI emite CSV e também era ignorada.
	exp, _ := params["expectStatus"].([]interface{})
	if n, ok := toInt(params["expectStatus"]); ok {
		exp = []interface{}{n}
	}
	if s, ok := params["expectStatus"].(string); ok {
		for _, part := range strings.Split(s, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
				exp = append(exp, n)
			}
		}
	}
	if len(exp) > 0 {
		match := false
		for _, e := range exp {
			if n, ok := toInt(e); ok && n == res.StatusCode {
				match = true
				break
			}
		}
		if !match {
			code = res.StatusCode
			if code == 0 {
				code = -1
			}
		} else {
			code = 0
		}
	}
	return code, fmt.Sprintf("HTTP %d\n%s", res.StatusCode, string(raw))
}

func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

// runWASM — executor WASM (Fase 3). Roda um módulo WebAssembly WASI no próprio
// agent via wazero (runtime pure-Go, sem CGO — mesmo ethos do SQLite modernc).
// Sandbox por construção: o módulo só enxerga o que for explicitamente provido.
//
// Params: wasmPath (arquivo local) OU wasmUrl (baixa o .wasm); args (string),
//
//	stdin (string). O módulo deve ser um "command" WASI (exporta _start),
//	compilável de Rust/Go/TinyGo/C com target wasm32-wasi.
func runWASM(params map[string]interface{}, timeoutSec int, emit func(string)) (int, string) {
	var wasmBytes []byte
	if path, _ := params["wasmPath"].(string); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return -1, "read wasm: " + err.Error()
		}
		wasmBytes = b
	} else if wurl, _ := params["wasmUrl"].(string); wurl != "" {
		b, err := downloadWASM(wurl, timeoutSec)
		if err != nil {
			return -1, err.Error()
		}
		wasmBytes = b
	} else {
		return -1, "missing 'wasmPath' or 'wasmUrl' param"
	}
	args, _ := params["args"].(string)
	stdin, _ := params["stdin"].(string)
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	return execWASM(wasmBytes, args, stdin, timeoutSec, emit)
}

// execWASM instancia e roda o módulo (testável isoladamente). Captura
// stdout+stderr no buffer e os streama via emit; o exit code WASI vira o
// exitCode do job (0 = OK).
func execWASM(wasmBytes []byte, argStr, stdin string, timeoutSec int, emit func(string)) (int, string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	var buf bytes.Buffer
	sw := &streamWriter{buf: &buf, emit: emit}

	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)

	argv := []string{"job"} // args[0] = nome do "programa" (convenção WASI)
	if argStr != "" {
		argv = append(argv, strings.Fields(argStr)...)
	}
	cfg := wazero.NewModuleConfig().WithStdout(sw).WithStderr(sw).WithArgs(argv...)
	if stdin != "" {
		cfg = cfg.WithStdin(strings.NewReader(stdin))
	}

	_, err := rt.InstantiateWithConfig(ctx, wasmBytes, cfg)
	code := 0
	if err != nil {
		if ee, ok := err.(*sys.ExitError); ok {
			code = int(ee.ExitCode()) // exit(n) do módulo
		} else {
			code = -1
			buf.WriteString("\n" + err.Error())
		}
	}
	return code, buf.String()
}

func downloadWASM(url string, timeoutSec int) ([]byte, error) {
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	res, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download wasm: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("download wasm: HTTP %d", res.StatusCode)
	}
	return io.ReadAll(res.Body)
}

func hostnameOr(def string) string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return def
	}
	return "agent-" + h
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
