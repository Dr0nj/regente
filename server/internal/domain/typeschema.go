// typeschema.go — ADV-1: schema DEDICADO por jobType.
//
// Fonte ÚNICA e declarativa do contrato de params (actionConfig) de cada tipo:
// campos, obrigatoriedade, kind (tipo do valor), enums e aliases — espelhando o
// que os EXECUTORES realmente leem (agent/main.go, aws.go, database.go,
// filewatch.go, k8s.go, gcp.go e scheduler/ssh.go). O validador (validate.go)
// e o catálogo da API (GET /api/jobtypes) derivam DAQUI; o guia do front
// (code-schema.ts) é o espelho humano — campo novo num executor TEM que entrar
// aqui e lá.
//
// Regras:
//   - jobType conhecido → params validados CONTRA o schema: obrigatórios
//     presentes, kinds corretos, enums respeitados e campo DESCONHECIDO é erro
//     (pega typo tipo `comand:` no publish/lint, não em produção às 3h).
//   - jobType desconhecido → aceito com params livres (mundo aberto preservado:
//     só roda se um agente anunciar a capability; senão WAIT_AGENT).
//   - Aliases de tipo (REST→HTTP, DB→DATABASE, …) e de campo
//     (functionName→function) contam como o canônico.
package domain

import (
	"fmt"
	"sort"
	"strings"
)

// FieldKind — tipo aceito no VALOR de um param.
type FieldKind string

const (
	KindString  FieldKind = "string"
	KindInt     FieldKind = "int"
	KindBool    FieldKind = "bool"    // bool YAML/JSON ou "true"/"false" string (executores legados leem string)
	KindMap     FieldKind = "map"     // mapa chave→valor (headers, env, arguments)
	KindArray   FieldKind = "array"   // lista livre (branches)
	KindEnum    FieldKind = "enum"    // string dentro de Enum (case-insensitive)
	KindScalar  FieldKind = "scalar"  // string OU número (ex.: port)
	KindIntList FieldKind = "intlist" // int, lista de ints OU string CSV "200,204" (ex.: expectStatus)
	KindJSON    FieldKind = "json"    // JSON: string OU estrutura (o editor visual emite objeto; YAML aceita string)
)

// FieldSpec — um param do actionConfig.
type FieldSpec struct {
	Name     string    `json:"name"`
	Aliases  []string  `json:"aliases,omitempty"` // nomes alternativos aceitos (contam como Name)
	Kind     FieldKind `json:"kind"`
	Required bool      `json:"required,omitempty"`
	Enum     []string  `json:"enum,omitempty"`
	Desc     string    `json:"desc,omitempty"`
}

// JobTypeSchema — o contrato de UM jobType.
type JobTypeSchema struct {
	Type      string      `json:"type"`
	Aliases   []string    `json:"aliases,omitempty"`
	Agentless bool        `json:"agentless,omitempty"` // roda no server (SSH) ou é gate interno (WAIT/CHOICE/PARALLEL)
	Summary   string      `json:"summary"`
	Fields    []FieldSpec `json:"fields"`
	// Check — regra cross-field que o shape declarativo não expressa
	// (ex.: WAIT exige seconds OU until; WASM exige wasmPath OU wasmUrl).
	// Roda DEPOIS das checagens declarativas, só se cfg != nil.
	Check func(cfg map[string]interface{}) error `json:"-"`
}

// jobTypeSchemas — o registry, em ordem de apresentação (catálogo da API).
var jobTypeSchemas = []JobTypeSchema{
	{
		Type: "COMMAND", Summary: "Comando shell no host do agente (Windows executa via powershell).",
		Fields: []FieldSpec{
			{Name: "command", Kind: KindString, Required: true, Desc: "o comando a executar"},
			{Name: "cwd", Kind: KindString, Desc: "diretório de trabalho"},
		},
	},
	{
		Type: "SCRIPT", Summary: "Script .sh/.bat/.ps1 no host do agente.",
		Fields: []FieldSpec{
			{Name: "scriptPath", Kind: KindString, Required: true, Desc: "caminho do script no host do agente"},
			{Name: "args", Kind: KindString, Desc: "argumentos (string única)"},
			{Name: "cwd", Kind: KindString, Desc: "diretório de trabalho"},
		},
	},
	{
		Type: "SSH", Agentless: true, Summary: "Comando remoto via SSH — agentless (shell-out no próprio server).",
		Fields: []FieldSpec{
			{Name: "host", Kind: KindString, Required: true, Desc: "host/IP alvo"},
			{Name: "command", Kind: KindString, Required: true, Desc: "comando remoto"},
			{Name: "user", Kind: KindString, Desc: "usuário SSH"},
			{Name: "port", Kind: KindScalar, Desc: "porta (default 22)"},
			{Name: "keyPath", Kind: KindString, Desc: "chave privada (BatchMode — nunca pede senha)"},
			{Name: "strictHostKey", Kind: KindString, Desc: "StrictHostKeyChecking (default accept-new)"},
		},
	},
	{
		Type: "HTTP", Aliases: []string{"REST"}, Summary: "Chamada REST pelo agente.",
		Fields: []FieldSpec{
			{Name: "url", Kind: KindString, Required: true, Desc: "URL alvo"},
			{Name: "method", Kind: KindEnum, Enum: []string{"GET", "POST", "PUT", "PATCH", "DELETE"}, Desc: "default GET"},
			{Name: "headers", Kind: KindMap, Desc: "mapa nome→valor"},
			{Name: "body", Kind: KindString, Desc: "corpo da requisição"},
			{Name: "expectStatus", Kind: KindIntList, Desc: `status esperado(s) — 200, [200,204] ou "200,204"; fora deles = NOTOK`},
		},
	},
	{
		Type: "DATABASE", Aliases: []string{"DB"}, Summary: "SQL em Postgres/MySQL/SQLite pelo agente (drivers pure-Go).",
		Fields: []FieldSpec{
			{Name: "driver", Kind: KindEnum, Required: true,
				Enum: []string{"postgres", "postgresql", "pg", "pgx", "mysql", "mariadb", "sqlite", "sqlite3"},
				Desc: "postgres|mysql|sqlite (aliases aceitos)"},
			{Name: "dsn", Kind: KindString, Required: true, Desc: "connection string"},
			{Name: "sql", Kind: KindString, Required: true, Desc: "o statement (SELECT mostra linhas; DML mostra rows-affected)"},
			{Name: "maxRows", Kind: KindInt, Desc: "linhas renderizadas de um SELECT"},
			{Name: "query", Kind: KindBool, Desc: "força tratar como SELECT (default: auto)"},
		},
	},
	{
		Type: "FILE_WATCH", Aliases: []string{"FILEWATCH"}, Summary: "Espera um arquivo chegar no host do agente.",
		Fields: []FieldSpec{
			{Name: "path", Kind: KindString, Required: true, Desc: "caminho do arquivo no host do agente"},
			{Name: "intervalSec", Kind: KindInt, Desc: "cadência do poll (default 5, mín 1)"},
			{Name: "stableSec", Kind: KindInt, Desc: "tamanho estável por N s antes de OK (0 = basta existir)"},
		},
	},
	{
		Type: "FILE_TRANSFER", Aliases: []string{"MFT"}, Summary: "MFT nativo: transfere arquivos entre local, SFTP e S3 pelo agente.",
		Fields: []FieldSpec{
			{Name: "src", Kind: KindString, Required: true,
				Desc: "origem: caminho local, sftp://user:pass@host:22/caminho ou s3://bucket/chave (glob *.csv em local/sftp)"},
			{Name: "dst", Kind: KindString, Required: true,
				Desc: "destino nas mesmas formas; termine com / para diretório/prefixo (obrigatório com glob)"},
			{Name: "checksum", Kind: KindBool, Desc: "true = relê o destino e compara SHA-256 (verificação fim-a-fim)"},
			{Name: "deleteSource", Kind: KindBool, Desc: "true = remove a origem após transferir+verificar (move)"},
			{Name: "overwrite", Kind: KindBool, Desc: "false = falha se o destino já existe (default true)"},
			{Name: "mkdirs", Kind: KindBool, Desc: "cria diretórios de destino que faltam (default true)"},
			{Name: "keyPath", Kind: KindString, Desc: "chave privada p/ endpoints sftp (arquivo no host do agente)"},
			{Name: "password", Kind: KindString, Desc: "senha sftp (a userinfo da URL vence)"},
			{Name: "hostKeyFingerprint", Kind: KindString, Desc: `pin "SHA256:..." do host sftp; vazio = aceita qualquer`},
			{Name: "region", Kind: KindString, Desc: "região S3 (default env AWS_REGION do agente)"},
			{Name: "accessKeyId", Kind: KindString, Desc: "default env AWS_ACCESS_KEY_ID"},
			{Name: "secretAccessKey", Kind: KindString, Desc: "default env AWS_SECRET_ACCESS_KEY"},
			{Name: "sessionToken", Kind: KindString, Desc: "default env AWS_SESSION_TOKEN"},
			{Name: "s3Endpoint", Kind: KindString, Desc: "override da URL base do S3 (testes/MinIO, path-style)"},
		},
	},
	{
		Type: "LAMBDA", Aliases: []string{"AWS_LAMBDA"}, Summary: "Invoca uma função AWS Lambda (SigV4 pelo agente, sem SDK).",
		Fields: []FieldSpec{
			{Name: "function", Aliases: []string{"functionName"}, Kind: KindString, Required: true, Desc: "nome ou ARN da função"},
			{Name: "region", Kind: KindString, Desc: "default env AWS_REGION do agente"},
			{Name: "payload", Kind: KindJSON, Desc: "JSON enviado como event (string ou objeto; default {})"},
			{Name: "invocationType", Kind: KindEnum, Enum: []string{"RequestResponse", "Event"},
				Desc: "reservado — o executor atual invoca síncrono (RequestResponse)"},
			{Name: "accessKeyId", Kind: KindString, Desc: "default env AWS_ACCESS_KEY_ID"},
			{Name: "secretAccessKey", Kind: KindString, Desc: "default env AWS_SECRET_ACCESS_KEY"},
			{Name: "sessionToken", Kind: KindString, Desc: "default env AWS_SESSION_TOKEN"},
			{Name: "endpoint", Kind: KindString, Desc: "override da URL base (testes)"},
		},
	},
	{
		Type: "BATCH", Summary: "Job em container/lote (AWS Batch) — executor é ADV-8.",
		Fields: []FieldSpec{
			{Name: "jobQueue", Kind: KindString, Required: true, Desc: "fila do AWS Batch"},
			{Name: "jobDefinition", Kind: KindString, Required: true, Desc: "job definition registrada"},
			{Name: "command", Kind: KindString, Desc: "override do command do container"},
			{Name: "env", Kind: KindMap, Desc: "variáveis de ambiente extras"},
		},
	},
	{
		Type: "GLUE", Summary: "Job de ETL (AWS Glue) — executor é ADV-8.",
		Fields: []FieldSpec{
			{Name: "jobName", Kind: KindString, Required: true, Desc: "nome do Glue job"},
			{Name: "arguments", Kind: KindMap, Desc: "argumentos (--CHAVE: valor)"},
			{Name: "workerType", Kind: KindString, Desc: "ex.: G.1X"},
			{Name: "numberOfWorkers", Kind: KindInt, Desc: "workers"},
		},
	},
	{
		Type: "STEP_FUNCTION", Summary: "Dispara uma state machine (AWS Step Functions) — executor é ADV-8.",
		Fields: []FieldSpec{
			{Name: "stateMachineArn", Kind: KindString, Required: true, Desc: "ARN da state machine"},
			{Name: "input", Kind: KindJSON, Desc: "JSON de input (string ou objeto)"},
		},
	},
	{
		Type: "WAIT", Agentless: true, Summary: "Delay/timer (sem params = no-op imediato).",
		Fields: []FieldSpec{
			{Name: "seconds", Kind: KindInt, Desc: "espera N segundos"},
			{Name: "until", Kind: KindString, Desc: `espera até "HH:MM"`},
		},
		Check: func(cfg map[string]interface{}) error {
			_, hasSec := cfg["seconds"]
			_, hasUntil := cfg["until"]
			if !hasSec && !hasUntil {
				return fmt.Errorf("WAIT requires seconds or until")
			}
			return nil
		},
	},
	{
		Type: "CHOICE", Agentless: true, Summary: "Desvio condicional.",
		Fields: []FieldSpec{
			{Name: "expression", Kind: KindString, Required: true, Desc: "expressão avaliada sobre o output"},
			{Name: "branches", Kind: KindArray, Desc: "ramos possíveis"},
		},
	},
	{
		Type: "PARALLEL", Agentless: true, Summary: "Execução concorrente de branches.",
		Fields: []FieldSpec{
			{Name: "branches", Kind: KindArray, Required: true, Desc: "lista NÃO-vazia de ramos"},
			{Name: "maxConcurrency", Kind: KindInt, Desc: "teto de concorrência"},
		},
		Check: func(cfg map[string]interface{}) error {
			if br, ok := cfg["branches"].([]interface{}); ok && len(br) == 0 {
				return fmt.Errorf("PARALLEL.branches must be non-empty array")
			}
			return nil
		},
	},
	{
		Type: "WASM", Summary: "Módulo WebAssembly WASI no agente (wazero, sandbox por construção).",
		Fields: []FieldSpec{
			{Name: "wasmPath", Kind: KindString, Desc: "arquivo .wasm local no host do agente"},
			{Name: "wasmUrl", Kind: KindString, Desc: "baixa o .wasm desta URL"},
			{Name: "args", Kind: KindString, Desc: "argumentos do módulo"},
			{Name: "stdin", Kind: KindString, Desc: "stdin do módulo"},
		},
		Check: func(cfg map[string]interface{}) error {
			p, _ := cfg["wasmPath"].(string)
			u, _ := cfg["wasmUrl"].(string)
			if strings.TrimSpace(p) == "" && strings.TrimSpace(u) == "" {
				return fmt.Errorf("WASM requires wasmPath or wasmUrl")
			}
			return nil
		},
	},
	{
		Type: "K8S", Aliases: []string{"K8S_JOB"}, Summary: "Job Kubernetes pelo agente (API server, sem kubectl).",
		Fields: []FieldSpec{
			{Name: "image", Kind: KindString, Required: true, Desc: "imagem do container"},
			{Name: "command", Kind: KindString, Desc: `roda como ["/bin/sh","-c",command]`},
			{Name: "namespace", Kind: KindString, Desc: `default "default"`},
			{Name: "name", Kind: KindString, Desc: "nome do Job (default regente-<ms>)"},
			{Name: "apiServer", Kind: KindString, Desc: "URL da API k8s; vazio = in-cluster"},
			{Name: "token", Kind: KindString, Desc: "Bearer da service account; vazio = in-cluster"},
			{Name: "insecureTLS", Kind: KindBool, Desc: "true pula verificação TLS (self-signed)"},
		},
	},
	{
		Type: "GCP_RUN", Aliases: []string{"CLOUD_RUN_JOB"}, Summary: "Dispara um Cloud Run Job (Run Admin API v2) pelo agente.",
		Fields: []FieldSpec{
			{Name: "project", Kind: KindString, Required: true, Desc: "id do projeto GCP"},
			{Name: "region", Kind: KindString, Required: true, Desc: "location (ex.: us-central1)"},
			{Name: "job", Kind: KindString, Required: true, Desc: "nome do Cloud Run Job"},
			{Name: "token", Kind: KindString, Desc: "OAuth bearer; default env GOOGLE_OAUTH_TOKEN ou metadata server"},
			{Name: "endpoint", Kind: KindString, Desc: "override base (testes)"},
		},
	},
}

// schemaIndex — lookup por tipo canônico E aliases, em UPPER.
var schemaIndex = func() map[string]*JobTypeSchema {
	idx := map[string]*JobTypeSchema{}
	for i := range jobTypeSchemas {
		s := &jobTypeSchemas[i]
		idx[strings.ToUpper(s.Type)] = s
		for _, a := range s.Aliases {
			idx[strings.ToUpper(a)] = s
		}
	}
	return idx
}()

// SchemaFor devolve o schema do jobType (canônico ou alias); ok=false para
// tipo desconhecido (mundo aberto — validação livre).
func SchemaFor(jobType string) (*JobTypeSchema, bool) {
	s, ok := schemaIndex[strings.ToUpper(strings.TrimSpace(jobType))]
	return s, ok
}

// JobTypeCatalog — o catálogo inteiro (ordem estável de apresentação), para
// GET /api/jobtypes e ferramentas (SDK/docs futuros — ADV-6/7).
func JobTypeCatalog() []JobTypeSchema { return jobTypeSchemas }

// validateAgainstSchema — params × schema: kinds, enums e campos desconhecidos
// sempre; obrigatórios e regra cross-field só no modo strict (publish/write
// direto — o draft do Design pode estar incompleto de propósito).
func validateAgainstSchema(s *JobTypeSchema, cfg map[string]interface{}, strict bool) error {
	// índice campo (nome+aliases, case-sensitive — YAML/JSON de params é assim hoje)
	byName := map[string]*FieldSpec{}
	for i := range s.Fields {
		f := &s.Fields[i]
		byName[f.Name] = f
		for _, a := range f.Aliases {
			byName[a] = f
		}
	}

	// 1) campo desconhecido PRIMEIRO (typo em `comand:` deve apontar o typo,
	//    não "command required") + kind/enum do que veio
	for k, v := range cfg {
		f, ok := byName[k]
		if !ok {
			return fmt.Errorf("%s: campo desconhecido %q (aceitos: %s)", s.Type, k, fieldNames(s))
		}
		if err := checkKind(f, v); err != nil {
			return fmt.Errorf("%s.%s %w", s.Type, f.Name, err)
		}
	}
	if !strict {
		return nil
	}
	// 2) obrigatórios presentes (nome OU alias) e não-vazios
	for i := range s.Fields {
		f := &s.Fields[i]
		if !f.Required {
			continue
		}
		if _, ok := lookupField(cfg, f); !ok {
			return fmt.Errorf("%s.%s required", s.Type, f.Name)
		}
	}
	// 3) regra cross-field do tipo
	if s.Check != nil {
		return s.Check(cfg)
	}
	return nil
}

// lookupField acha o valor de um campo por nome ou alias; ok=false se ausente
// ou string vazia (required exige valor de verdade).
func lookupField(cfg map[string]interface{}, f *FieldSpec) (interface{}, bool) {
	names := append([]string{f.Name}, f.Aliases...)
	for _, n := range names {
		v, ok := cfg[n]
		if !ok {
			continue
		}
		if sv, isStr := v.(string); isStr && strings.TrimSpace(sv) == "" {
			continue
		}
		return v, true
	}
	return nil, false
}

func fieldNames(s *JobTypeSchema) string {
	names := make([]string, 0, len(s.Fields))
	for _, f := range s.Fields {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// checkKind — o VALOR bate com o kind do campo?
func checkKind(f *FieldSpec, v interface{}) error {
	switch f.Kind {
	case KindString:
		if _, ok := v.(string); !ok {
			return fmt.Errorf("must be string")
		}
	case KindInt:
		if !isIntLike(v) {
			return fmt.Errorf("must be int")
		}
	case KindBool:
		if _, ok := v.(bool); ok {
			return nil
		}
		if sv, ok := v.(string); ok && (strings.EqualFold(sv, "true") || strings.EqualFold(sv, "false")) {
			return nil
		}
		return fmt.Errorf("must be bool (true|false)")
	case KindMap:
		switch v.(type) {
		case map[string]interface{}, map[interface{}]interface{}:
		default:
			return fmt.Errorf("must be map")
		}
	case KindArray:
		if _, ok := v.([]interface{}); !ok {
			return fmt.Errorf("must be array")
		}
	case KindEnum:
		sv, ok := v.(string)
		if !ok {
			return fmt.Errorf("must be one of %s", strings.Join(f.Enum, "|"))
		}
		for _, e := range f.Enum {
			if strings.EqualFold(sv, e) {
				return nil
			}
		}
		return fmt.Errorf("must be one of %s", strings.Join(f.Enum, "|"))
	case KindScalar:
		if _, ok := v.(string); ok {
			return nil
		}
		if isIntLike(v) {
			return nil
		}
		return fmt.Errorf("must be string or number")
	case KindIntList:
		if isIntLike(v) {
			return nil
		}
		// string CSV "200,204" — é o que o editor visual emite
		if sv, ok := v.(string); ok {
			for _, part := range strings.Split(sv, ",") {
				if strings.TrimSpace(part) == "" {
					continue
				}
				for _, r := range strings.TrimSpace(part) {
					if r < '0' || r > '9' {
						return fmt.Errorf(`must be int, array of ints or "200,204"`)
					}
				}
			}
			return nil
		}
		arr, ok := v.([]interface{})
		if !ok {
			return fmt.Errorf(`must be int, array of ints or "200,204"`)
		}
		for _, it := range arr {
			if !isIntLike(it) {
				return fmt.Errorf(`must be int, array of ints or "200,204"`)
			}
		}
	case KindJSON:
		switch v.(type) {
		case string, map[string]interface{}, map[interface{}]interface{}, []interface{}:
		default:
			return fmt.Errorf("must be JSON (string ou estrutura)")
		}
	}
	return nil
}

// isIntLike — números como chegam do YAML (int/int64/uint64) e do JSON
// (float64, se inteiro).
func isIntLike(v interface{}) bool {
	switch n := v.(type) {
	case int, int64, uint64, int32:
		return true
	case float64:
		return n == float64(int64(n))
	}
	return false
}
