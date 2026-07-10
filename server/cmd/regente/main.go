// regente — CLI DevEx do orquestrador (Diferenciais §3/§4, 2026-07-07).
//
//	regente test <job.yaml | workspace-dir> [-date] [-json]   D-7 testing framework
//	regente dev [daily] [-workspace] [-date] [-addr]          D-8 local dev mode
//	regente promote -from dev -to main [-folders a,b]         D-9 multi-env promotion
//
// Onde o Control-M perde feio: aqui o ciclo definir→testar→rodar local→promover
// é linha de comando + Git, sem console proprietário no meio. O DSL Go
// (pkg/jobdsl) fecha o quarteto: gerar → `test` → `dev` → `promote`.
package main

import (
	"fmt"
	"os"
	"strings"
)

const usage = `regente — DevEx do orquestrador Regente

Uso:
  regente test <job.yaml | workspace-dir> [flags]   valida + simula (CI-friendly)
  regente dev  [daily] [flags]                      sobe um Regente local descartável
  regente promote -from <branch> -to <branch>       promove jobs entre ambientes (Git)
  regente ops  <subcomando> [flags]                 opera um server vivo (query/ações/daily/archives)

Detalhes por comando: regente <comando> -h
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "test":
		err = cmdTest(os.Args[2:])
	case "dev":
		err = cmdDev(os.Args[2:])
	case "promote":
		err = cmdPromote(os.Args[2:])
	case "ops":
		err = cmdOps(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "comando desconhecido %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}

// reorderArgs move os argumentos POSICIONAIS para o fim, para que o flag
// package (que para no 1º não-flag) parseie flags dadas em qualquer ordem —
// `regente test ws -json` funciona igual a `regente test -json ws`. `boolFlags`
// são as flags que NÃO consomem o próximo token como valor.
func reorderArgs(args []string, boolFlags ...string) []string {
	isBool := map[string]bool{}
	for _, b := range boolFlags {
		isBool[b] = true
	}
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" { // separador explícito: o resto é tudo posicional
			positional = append(positional, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			// só consome o próximo token como valor se não for "-flag=valor" nem bool
			if !strings.Contains(a, "=") && !isBool[name] && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		positional = append(positional, a)
	}
	return append(flags, positional...)
}
