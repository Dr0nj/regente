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
	Agentless bool        `json:"agentless,omitempty"` // roda no server (SSH), não precisa de agente no alvo
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
		Type: "COMMAND", Summary: "Shell command on the agent host (Windows runs it through powershell).",
		Fields: []FieldSpec{
			{Name: "command", Kind: KindString, Required: true, Desc: "the command to run"},
			{Name: "cwd", Kind: KindString, Desc: "working directory"},
		},
	},
	{
		Type: "SCRIPT", Summary: "A .sh/.bat/.ps1 script on the agent host.",
		Fields: []FieldSpec{
			{Name: "scriptPath", Kind: KindString, Required: true, Desc: "path to the script on the agent host"},
			{Name: "args", Kind: KindString, Desc: "arguments (single string)"},
			{Name: "cwd", Kind: KindString, Desc: "working directory"},
		},
	},
	{
		Type: "SSH", Agentless: true, Summary: "Remote command over SSH — agentless (shell-out on the server itself).",
		Fields: []FieldSpec{
			{Name: "host", Kind: KindString, Required: true, Desc: "target host/IP"},
			{Name: "command", Kind: KindString, Required: true, Desc: "remote command"},
			{Name: "user", Kind: KindString, Desc: "SSH user"},
			{Name: "port", Kind: KindScalar, Desc: "port (default 22)"},
			{Name: "keyPath", Kind: KindString, Desc: "private key (BatchMode — never prompts for a password)"},
			{Name: "strictHostKey", Kind: KindString, Desc: "StrictHostKeyChecking (default accept-new)"},
		},
	},
	{
		Type: "HTTP", Aliases: []string{"REST"}, Summary: "REST call from the agent.",
		Fields: []FieldSpec{
			{Name: "url", Kind: KindString, Required: true, Desc: "target URL"},
			{Name: "method", Kind: KindEnum, Enum: []string{"GET", "POST", "PUT", "PATCH", "DELETE"}, Desc: "default GET"},
			{Name: "headers", Kind: KindMap, Desc: "name→value map"},
			{Name: "body", Kind: KindString, Desc: "request body"},
			{Name: "expectStatus", Kind: KindIntList, Desc: `expected status(es) — 200, [200,204] or "200,204"; anything else = NOTOK`},
		},
	},
	{
		Type: "DATABASE", Aliases: []string{"DB"}, Summary: "SQL on Postgres/MySQL/SQLite from the agent (pure-Go drivers).",
		Fields: []FieldSpec{
			{Name: "driver", Kind: KindEnum, Required: true,
				Enum: []string{"postgres", "postgresql", "pg", "pgx", "mysql", "mariadb", "sqlite", "sqlite3"},
				Desc: "postgres|mysql|sqlite (aliases accepted)"},
			{Name: "dsn", Kind: KindString, Required: true, Desc: "connection string"},
			{Name: "sql", Kind: KindString, Required: true, Desc: "the statement (SELECT shows rows; DML shows rows-affected)"},
			{Name: "maxRows", Kind: KindInt, Desc: "rows rendered from a SELECT"},
			{Name: "query", Kind: KindBool, Desc: "force treating it as a SELECT (default: auto)"},
		},
	},
	{
		Type: "FILE_WATCH", Aliases: []string{"FILEWATCH"}, Summary: "Waits for a file to land on the agent host.",
		Fields: []FieldSpec{
			{Name: "path", Kind: KindString, Required: true, Desc: "path to the file on the agent host"},
			{Name: "intervalSec", Kind: KindInt, Desc: "poll cadence (default 5, min 1)"},
			{Name: "stableSec", Kind: KindInt, Desc: "size stable for N s before OK (0 = just has to exist)"},
		},
	},
	{
		Type: "FILE_TRANSFER", Aliases: []string{"MFT"}, Summary: "Native MFT: transfers files between local, SFTP and S3 from the agent.",
		Fields: []FieldSpec{
			{Name: "src", Kind: KindString, Required: true,
				Desc: "source: a local path, sftp://user:pass@host:22/path or s3://bucket/key (glob *.csv on local/sftp)"},
			{Name: "dst", Kind: KindString, Required: true,
				Desc: "destination in the same forms; end with / for a directory/prefix (required with glob)"},
			{Name: "checksum", Kind: KindBool, Desc: "true = re-reads the destination and compares SHA-256 (end-to-end verification)"},
			{Name: "deleteSource", Kind: KindBool, Desc: "true = removes the source after transferring+verifying (move)"},
			{Name: "overwrite", Kind: KindBool, Desc: "false = fails if the destination already exists (default true)"},
			{Name: "mkdirs", Kind: KindBool, Desc: "creates missing destination directories (default true)"},
			{Name: "keyPath", Kind: KindString, Desc: "private key for sftp endpoints (file on the agent host)"},
			{Name: "password", Kind: KindString, Desc: "sftp password (the URL userinfo wins)"},
			{Name: "hostKeyFingerprint", Kind: KindString, Desc: `sftp host "SHA256:..." pin; empty = accepts any`},
			{Name: "region", Kind: KindString, Desc: "S3 region (defaults to the agent's AWS_REGION env)"},
			{Name: "accessKeyId", Kind: KindString, Desc: "defaults to the AWS_ACCESS_KEY_ID env"},
			{Name: "secretAccessKey", Kind: KindString, Desc: "defaults to the AWS_SECRET_ACCESS_KEY env"},
			{Name: "sessionToken", Kind: KindString, Desc: "defaults to the AWS_SESSION_TOKEN env"},
			{Name: "s3Endpoint", Kind: KindString, Desc: "override the S3 base URL (tests/MinIO, path-style)"},
		},
	},
	{
		Type: "LAMBDA", Aliases: []string{"AWS_LAMBDA"}, Summary: "Invokes an AWS Lambda function (SigV4 from the agent, no SDK).",
		Fields: []FieldSpec{
			{Name: "function", Aliases: []string{"functionName"}, Kind: KindString, Required: true, Desc: "function name or ARN"},
			{Name: "region", Kind: KindString, Desc: "defaults to the agent's AWS_REGION env"},
			{Name: "payload", Kind: KindJSON, Desc: "JSON sent as the event (string or object; default {})"},
			{Name: "invocationType", Kind: KindEnum, Enum: []string{"RequestResponse", "Event"},
				Desc: "reserved — the current executor invokes synchronously (RequestResponse)"},
			{Name: "accessKeyId", Kind: KindString, Desc: "defaults to the AWS_ACCESS_KEY_ID env"},
			{Name: "secretAccessKey", Kind: KindString, Desc: "defaults to the AWS_SECRET_ACCESS_KEY env"},
			{Name: "sessionToken", Kind: KindString, Desc: "defaults to the AWS_SESSION_TOKEN env"},
			{Name: "endpoint", Kind: KindString, Desc: "override the base URL (tests)"},
		},
	},
	{
		Type: "BATCH", Aliases: []string{"AWS_BATCH"}, Summary: "Container/batch job (AWS Batch) — SubmitJob + polling from the agent (SigV4, no SDK).",
		Fields: []FieldSpec{
			{Name: "jobQueue", Kind: KindString, Required: true, Desc: "the AWS Batch queue"},
			{Name: "jobDefinition", Kind: KindString, Required: true, Desc: "registered job definition"},
			{Name: "jobName", Kind: KindString, Desc: "submission name (default regente-<unix>)"},
			{Name: "command", Kind: KindString, Desc: "override the container command (split on spaces)"},
			{Name: "env", Kind: KindMap, Desc: "extra environment variables"},
			{Name: "parameters", Kind: KindMap, Desc: "job definition parameters"},
			{Name: "region", Kind: KindString, Desc: "defaults to the agent's AWS_REGION env"},
			{Name: "accessKeyId", Kind: KindString, Desc: "defaults to the AWS_ACCESS_KEY_ID env"},
			{Name: "secretAccessKey", Kind: KindString, Desc: "defaults to the AWS_SECRET_ACCESS_KEY env"},
			{Name: "sessionToken", Kind: KindString, Desc: "defaults to the AWS_SESSION_TOKEN env"},
			{Name: "endpoint", Kind: KindString, Desc: "override the base URL (tests)"},
		},
	},
	{
		Type: "GLUE", Aliases: []string{"AWS_GLUE"}, Summary: "ETL job (AWS Glue) — StartJobRun + polling from the agent (SigV4, no SDK).",
		Fields: []FieldSpec{
			{Name: "jobName", Kind: KindString, Required: true, Desc: "the Glue job name"},
			{Name: "arguments", Kind: KindMap, Desc: "arguments (--KEY: value)"},
			{Name: "workerType", Kind: KindString, Desc: "e.g. G.1X"},
			{Name: "numberOfWorkers", Kind: KindInt, Desc: "workers"},
			{Name: "region", Kind: KindString, Desc: "defaults to the agent's AWS_REGION env"},
			{Name: "accessKeyId", Kind: KindString, Desc: "defaults to the AWS_ACCESS_KEY_ID env"},
			{Name: "secretAccessKey", Kind: KindString, Desc: "defaults to the AWS_SECRET_ACCESS_KEY env"},
			{Name: "sessionToken", Kind: KindString, Desc: "defaults to the AWS_SESSION_TOKEN env"},
			{Name: "endpoint", Kind: KindString, Desc: "override the base URL (tests)"},
		},
	},
	{
		Type: "STEP_FUNCTION", Aliases: []string{"STEP_FUNCTIONS"}, Summary: "Fires a state machine (AWS Step Functions) — StartExecution + polling from the agent (SigV4, no SDK).",
		Fields: []FieldSpec{
			{Name: "stateMachineArn", Kind: KindString, Required: true, Desc: "the state machine ARN"},
			{Name: "input", Kind: KindJSON, Desc: "input JSON (string or object)"},
			{Name: "name", Kind: KindString, Desc: "execution name (default: AWS generates one)"},
			{Name: "region", Kind: KindString, Desc: "defaults to the agent's AWS_REGION env"},
			{Name: "accessKeyId", Kind: KindString, Desc: "defaults to the AWS_ACCESS_KEY_ID env"},
			{Name: "secretAccessKey", Kind: KindString, Desc: "defaults to the AWS_SECRET_ACCESS_KEY env"},
			{Name: "sessionToken", Kind: KindString, Desc: "defaults to the AWS_SESSION_TOKEN env"},
			{Name: "endpoint", Kind: KindString, Desc: "override the base URL (tests)"},
		},
	},
	{
		Type: "WASM", Summary: "WASI WebAssembly module on the agent (wazero, sandboxed by construction).",
		Fields: []FieldSpec{
			{Name: "wasmPath", Kind: KindString, Desc: "local .wasm file on the agent host"},
			{Name: "wasmUrl", Kind: KindString, Desc: "downloads the .wasm from this URL"},
			{Name: "args", Kind: KindString, Desc: "module arguments"},
			{Name: "stdin", Kind: KindString, Desc: "module stdin"},
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
		Type: "K8S", Aliases: []string{"K8S_JOB"}, Summary: "Kubernetes Job from the agent (API server, no kubectl).",
		Fields: []FieldSpec{
			{Name: "image", Kind: KindString, Required: true, Desc: "container image"},
			{Name: "command", Kind: KindString, Desc: `runs as ["/bin/sh","-c",command]`},
			{Name: "namespace", Kind: KindString, Desc: `default "default"`},
			{Name: "name", Kind: KindString, Desc: "Job name (default regente-<ms>)"},
			{Name: "apiServer", Kind: KindString, Desc: "k8s API URL; empty = in-cluster"},
			{Name: "token", Kind: KindString, Desc: "service account Bearer; empty = in-cluster"},
			{Name: "insecureTLS", Kind: KindBool, Desc: "true skips TLS verification (self-signed)"},
		},
	},
	{
		Type: "GCP_RUN", Aliases: []string{"CLOUD_RUN_JOB"}, Summary: "Fires a Cloud Run Job (Run Admin API v2) from the agent.",
		Fields: []FieldSpec{
			{Name: "project", Kind: KindString, Required: true, Desc: "GCP project id"},
			{Name: "region", Kind: KindString, Required: true, Desc: "location (e.g. us-central1)"},
			{Name: "job", Kind: KindString, Required: true, Desc: "the Cloud Run Job name"},
			{Name: "token", Kind: KindString, Desc: "OAuth bearer; defaults to the GOOGLE_OAUTH_TOKEN env or the metadata server"},
			{Name: "endpoint", Kind: KindString, Desc: "override the base URL (tests)"},
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
			return fmt.Errorf("%s: unknown field %q (accepted: %s)", s.Type, k, fieldNames(s))
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
			return fmt.Errorf("must be JSON (string or structure)")
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
