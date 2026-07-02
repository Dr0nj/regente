// FILE_WATCH — paridade Control-M: job que espera um ARQUIVO aparecer no host
// do agente. Sucesso (exit 0) quando o arquivo existe (e, se stableSec > 0,
// quando o tamanho ficou estável por stableSec segundos — arquivo terminou de
// ser escrito/transferido). Timeout do job (def.Timeout) esgotado sem arquivo
// estável → exit 1 (NOTOK) e o operador vê o motivo no output.
//
// Params:
//   - path        (string, obrigatório) — caminho do arquivo no host do agente
//   - intervalSec (number, opcional, default 5, mín 1) — cadência do poll
//   - stableSec   (number, opcional, default 0) — 0 = basta existir; >0 exige
//     tamanho inalterado por N segundos antes de dar OK
package main

import (
	"fmt"
	"os"
	"time"
)

func runFileWatch(params map[string]interface{}, timeoutSec int, emit func(string)) (int, string) {
	path, _ := params["path"].(string)
	if path == "" {
		return -1, "missing 'path' param"
	}
	interval := time.Duration(numParam(params, "intervalSec", 5)) * time.Second
	if interval < time.Second {
		interval = time.Second
	}
	stable := time.Duration(numParam(params, "stableSec", 0)) * time.Second
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)

	emit(fmt.Sprintf("[file-watch] aguardando %s (poll %s, estabilidade %s, timeout %ds)\n",
		path, interval, stable, timeoutSec))

	var lastSize int64 = -1
	var stableSince time.Time
	polls := 0
	for {
		if time.Now().After(deadline) {
			if lastSize >= 0 {
				return 1, fmt.Sprintf("timeout: %s existe mas o tamanho não estabilizou por %s (último: %d bytes)", path, stable, lastSize)
			}
			return 1, fmt.Sprintf("timeout: arquivo %s não apareceu em %ds", path, timeoutSec)
		}
		st, err := os.Stat(path)
		switch {
		case err == nil && st.IsDir():
			return 1, fmt.Sprintf("%s existe mas é um DIRETÓRIO (esperado arquivo)", path)
		case err == nil:
			size := st.Size()
			if stable <= 0 {
				return 0, fmt.Sprintf("arquivo encontrado: %s (%d bytes)", path, size)
			}
			if size != lastSize {
				// tamanho mudou (ou 1ª vez que vimos o arquivo) — re-arma a janela
				if lastSize < 0 {
					emit(fmt.Sprintf("[file-watch] %s apareceu (%d bytes) — aguardando estabilizar\n", path, size))
				}
				lastSize = size
				stableSince = time.Now()
			} else if time.Since(stableSince) >= stable {
				return 0, fmt.Sprintf("arquivo encontrado e estável: %s (%d bytes, estável por %s)", path, size, stable)
			}
		default:
			// ainda não existe — a cada ~10 polls emite um sinal de vida
			if polls > 0 && polls%10 == 0 {
				emit(fmt.Sprintf("[file-watch] ainda aguardando %s (%d polls)\n", path, polls))
			}
			lastSize = -1
		}
		polls++
		time.Sleep(interval)
	}
}

// numParam lê um número de params aceitando float64 (JSON), int e string vazia.
func numParam(params map[string]interface{}, key string, def int) int {
	switch v := params[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return def
}
