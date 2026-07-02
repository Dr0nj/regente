package api

import (
	"strings"
	"testing"
)

// decodeDefinition aceita o campo de parâmetros sob "actionConfig" (tag json
// oficial, o que o front manda) E "params" (nome do YAML — integrações externas
// mandavam "params" e o campo era ignorado em silêncio).
func TestDecodeDefinition_ParamsAlias(t *testing.T) {
	// 1) actionConfig (caminho oficial) segue funcionando.
	d, err := decodeDefinition(strings.NewReader(
		`{"id":"a","team":"t","jobType":"COMMAND","actionConfig":{"command":"echo oficial"}}`))
	if err != nil {
		t.Fatalf("decode actionConfig: %v", err)
	}
	if got := d.Params["command"]; got != "echo oficial" {
		t.Fatalf("actionConfig.command = %v", got)
	}

	// 2) "params" (alias YAML) agora também entra.
	d, err = decodeDefinition(strings.NewReader(
		`{"id":"b","team":"t","jobType":"COMMAND","params":{"command":"echo alias"}}`))
	if err != nil {
		t.Fatalf("decode params: %v", err)
	}
	if got := d.Params["command"]; got != "echo alias" {
		t.Fatalf("params.command = %v (alias ignorado)", got)
	}

	// 3) Ambos presentes → actionConfig vence.
	d, err = decodeDefinition(strings.NewReader(
		`{"id":"c","team":"t","jobType":"COMMAND","actionConfig":{"command":"vence"},"params":{"command":"perde"}}`))
	if err != nil {
		t.Fatalf("decode both: %v", err)
	}
	if got := d.Params["command"]; got != "vence" {
		t.Fatalf("com ambos, actionConfig deveria vencer; veio %v", got)
	}
}
