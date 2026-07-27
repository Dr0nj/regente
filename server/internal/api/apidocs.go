// API-1 — Contrato OpenAPI + viewer embutidos no binário (go:embed): sempre
// disponíveis, zero flag, zero CDN — a spec é contrato DESTA versão do server,
// então viaja com ele (diferente do -docs-dir, que serve um site gerado fora).
//
//	GET /api-docs/               → viewer HTML self-contained (single-origin)
//	GET /api-docs/openapi.yaml   → o contrato cru (a fonte, escrita à mão)
//	GET /api-docs/openapi.json   → o mesmo contrato em JSON (Postman/codegen)
//
// Público como /docs e /health: é documentação, não dado. A conversão
// YAML→JSON é feita UMA vez, andando os yaml.Node — encoding/json de map
// ordenaria as chaves alfabeticamente e embaralharia a ordem curada das
// rotas/campos, que é parte da apresentação do contrato.
package api

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

//go:embed openapi.yaml
var openAPIYAML []byte

//go:embed apidocs.html
var apiDocsHTML []byte

// openAPIJSON converte a spec YAML pra JSON preservando a ordem dos mapas.
var openAPIJSON = sync.OnceValues(func() ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(openAPIYAML, &doc); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := yamlNodeJSON(&buf, &doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
})

// yamlNodeJSON escreve o node como JSON, na ordem do documento.
func yamlNodeJSON(buf *bytes.Buffer, n *yaml.Node) error {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			buf.WriteString("null")
			return nil
		}
		return yamlNodeJSON(buf, n.Content[0])
	case yaml.MappingNode:
		buf.WriteByte('{')
		for i := 0; i < len(n.Content); i += 2 {
			if i > 0 {
				buf.WriteByte(',')
			}
			key, err := json.Marshal(n.Content[i].Value)
			if err != nil {
				return err
			}
			buf.Write(key)
			buf.WriteByte(':')
			if err := yamlNodeJSON(buf, n.Content[i+1]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
		return nil
	case yaml.SequenceNode:
		buf.WriteByte('[')
		for i, c := range n.Content {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := yamlNodeJSON(buf, c); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
		return nil
	case yaml.AliasNode:
		return yamlNodeJSON(buf, n.Alias)
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!null":
			buf.WriteString("null")
		case "!!bool", "!!int", "!!float":
			// A spec é nossa (valores decimais simples); o literal YAML é JSON válido.
			buf.WriteString(n.Value)
		default:
			s, err := json.Marshal(n.Value)
			if err != nil {
				return err
			}
			buf.Write(s)
		}
		return nil
	}
	return fmt.Errorf("yaml node kind %d inesperado", n.Kind)
}

// OpenAPIAssets devolve o viewer e a spec (YAML + JSON) embutidos — a MESMA
// fonte que /api-docs serve. Existe para o docsite gerar a referência de API
// como página ESTÁTICA (que precisa abrir do disco, sem server rodando).
func OpenAPIAssets() (viewer, specYAML, specJSON []byte, err error) {
	j, err := openAPIJSON()
	if err != nil {
		return nil, nil, nil, err
	}
	return apiDocsHTML, openAPIYAML, j, nil
}

func (s *server) apiDocsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(apiDocsHTML)
}

func (s *server) apiDocsSpec(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, ".json") {
		out, err := openAPIJSON()
		if err != nil {
			http.Error(w, "invalid spec: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(out)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(openAPIYAML)
}
