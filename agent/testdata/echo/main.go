//go:build ignore

// Fixture WASI para o teste do executor WASM. Compilado para testdata/echo.wasm
// com: GOOS=wasip1 GOARCH=wasm go build -o ../echo.wasm .
// Imprime os args (menos o nome) e o stdin, e sai 0.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) > 1 {
		fmt.Println("args: " + strings.Join(os.Args[1:], " "))
	}
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		fmt.Println("stdin: " + sc.Text())
	}
	fmt.Println("hello from wasm")
}
