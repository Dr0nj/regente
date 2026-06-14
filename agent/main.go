// regente-agent — braço executor local.
//
// Conecta ao regente-server via WebSocket (conexão outbound), anuncia
// capabilities (COMMAND, REST, ...), recebe mensagens "dispatch" com
// um JobInstance e executa localmente. Devolve o resultado via mensagem
// "result" na mesma conexão.
//
// Uso:
//   regente-agent -server ws://localhost:8080/ws/agent -token dev-token -id agent-macbook
package main

import (
	"bytes"
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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	var (
		server  = flag.String("server", "ws://localhost:8080/ws/agent", "regente-server WebSocket URL")
		token   = flag.String("token", envOr("REGENTE_TOKEN", "dev-token"), "Bearer token")
		agentID = flag.String("id", hostnameOr("agent-local"), "Agent ID (unique)")
		caps    = flag.String("caps", "COMMAND,SCRIPT,HTTP,REST", "Comma-separated capabilities advertised")
	)
	flag.Parse()

	u, err := url.Parse(*server)
	if err != nil {
		log.Fatalf("parse url: %v", err)
	}
	q := u.Query()
	q.Set("token", *token)
	q.Set("id", *agentID)
	q.Set("caps", *caps)
	u.RawQuery = q.Encode()

	log.Printf("regente-agent id=%s caps=%s -> %s", *agentID, *caps, *server)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

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
		}
		if err := json.Unmarshal(msg, &job); err != nil {
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

func executeJob(jobType string, params map[string]interface{}, timeoutSec int, emit func(string)) (int, string) {
	switch strings.ToUpper(jobType) {
	case "COMMAND":
		return runCommand(params, timeoutSec, emit)
	case "SCRIPT":
		return runScript(params, timeoutSec, emit)
	case "HTTP", "REST":
		return runREST(params, timeoutSec)
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
//         expectStatus ([]int opcional — se definido e não bater, vira falha).
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
	// expectStatus override
	if exp, ok := params["expectStatus"].([]interface{}); ok && len(exp) > 0 {
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
