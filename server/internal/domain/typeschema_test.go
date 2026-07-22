package domain

import (
	"strings"
	"testing"
)

// ADV-1 — schema dedicado por jobType: obrigatórios, kinds, enums, aliases
// (de tipo e de campo) e rejeição de campo desconhecido.

func vac(jt string, params map[string]interface{}) error {
	return ValidateActionConfig(JobDefinition{ID: "j1", Label: "j1", Team: "t", JobType: jt, Params: params})
}

func TestTypeSchemaRequiredPerType(t *testing.T) {
	cases := []struct {
		jt     string
		params map[string]interface{}
		wantOK bool
	}{
		{"COMMAND", map[string]interface{}{"command": "echo oi"}, true},
		{"COMMAND", map[string]interface{}{}, false},                      // command required
		{"COMMAND", map[string]interface{}{"command": "   "}, false},      // vazio não conta
		{"SCRIPT", map[string]interface{}{"scriptPath": "/x.sh"}, true},
		{"SCRIPT", map[string]interface{}{"args": "--full"}, false},       // falta scriptPath
		{"SSH", map[string]interface{}{"host": "h", "command": "ls"}, true},
		{"SSH", map[string]interface{}{"host": "h"}, false},               // falta command
		{"HTTP", map[string]interface{}{"url": "http://x"}, true},
		{"HTTP", nil, false},                                              // url required
		{"DATABASE", map[string]interface{}{"driver": "postgres", "dsn": "d", "sql": "select 1"}, true},
		{"DATABASE", map[string]interface{}{"driver": "postgres", "dsn": "d"}, false},
		{"FILE_WATCH", map[string]interface{}{"path": "/data/in/a.txt"}, true},
		{"FILE_WATCH", map[string]interface{}{}, false},
		{"FILE_TRANSFER", map[string]interface{}{"src": "/data/out/*.csv", "dst": "sftp://svc@host/entrada/"}, true},
		{"FILE_TRANSFER", map[string]interface{}{"src": "/data/out/a.csv"}, false}, // falta dst
		{"FILE_TRANSFER", map[string]interface{}{"src": "s3://bkt/chave", "dst": "/local/x", "checksum": true, "deleteSource": "true"}, true},
		{"LAMBDA", map[string]interface{}{"function": "fn"}, true},
		{"LAMBDA", map[string]interface{}{}, false},
		{"BATCH", map[string]interface{}{"jobQueue": "q", "jobDefinition": "jd"}, true},
		{"BATCH", map[string]interface{}{"jobQueue": "q"}, false},
		{"GLUE", map[string]interface{}{"jobName": "etl"}, true},
		{"STEP_FUNCTION", map[string]interface{}{"stateMachineArn": "arn:x"}, true},
		{"PARALLEL", map[string]interface{}{"branches": []interface{}{"a"}}, true},
		{"PARALLEL", nil, false},
		{"K8S", map[string]interface{}{"image": "alpine"}, true},
		{"K8S", map[string]interface{}{"namespace": "ns"}, false},
		{"GCP_RUN", map[string]interface{}{"project": "p", "region": "r", "job": "j"}, true},
		{"GCP_RUN", map[string]interface{}{"project": "p", "region": "r"}, false},
	}
	for _, c := range cases {
		err := vac(c.jt, c.params)
		if c.wantOK && err != nil {
			t.Errorf("%s %v: esperado OK, veio %v", c.jt, c.params, err)
		}
		if !c.wantOK && err == nil {
			t.Errorf("%s %v: esperado erro, passou", c.jt, c.params)
		}
	}
}

func TestTypeSchemaAliases(t *testing.T) {
	// aliases de TIPO contam como o canônico
	if err := vac("REST", map[string]interface{}{"url": "http://x"}); err != nil {
		t.Fatalf("REST (alias de HTTP): %v", err)
	}
	if err := vac("DB", map[string]interface{}{"driver": "pgx", "dsn": "d", "sql": "s"}); err != nil {
		t.Fatalf("DB (alias de DATABASE): %v", err)
	}
	if err := vac("FILEWATCH", map[string]interface{}{"path": "/a"}); err != nil {
		t.Fatalf("FILEWATCH: %v", err)
	}
	if err := vac("MFT", map[string]interface{}{"src": "/a", "dst": "/b"}); err != nil {
		t.Fatalf("MFT (alias de FILE_TRANSFER): %v", err)
	}
	if err := vac("AWS_LAMBDA", map[string]interface{}{"function": "fn"}); err != nil {
		t.Fatalf("AWS_LAMBDA: %v", err)
	}
	if err := vac("K8S_JOB", map[string]interface{}{"image": "alpine"}); err != nil {
		t.Fatalf("K8S_JOB: %v", err)
	}
	if err := vac("CLOUD_RUN_JOB", map[string]interface{}{"project": "p", "region": "r", "job": "j"}); err != nil {
		t.Fatalf("CLOUD_RUN_JOB: %v", err)
	}
	// alias de CAMPO: functionName satisfaz function (o validador antigo exigia
	// functionName e o agente lia function — o schema aceita os dois)
	if err := vac("LAMBDA", map[string]interface{}{"functionName": "fn"}); err != nil {
		t.Fatalf("LAMBDA.functionName (alias): %v", err)
	}
}

func TestTypeSchemaUnknownField(t *testing.T) {
	err := vac("COMMAND", map[string]interface{}{"comand": "typo"})
	if err == nil {
		t.Fatal("campo desconhecido deveria falhar")
	}
	if !strings.Contains(err.Error(), "comand") || !strings.Contains(err.Error(), "command") {
		t.Fatalf("erro deveria citar o typo e os campos aceitos: %v", err)
	}
}

func TestTypeSchemaKinds(t *testing.T) {
	// enum: method inválido
	if err := vac("HTTP", map[string]interface{}{"url": "u", "method": "BREW"}); err == nil {
		t.Fatal("HTTP.method BREW deveria falhar")
	}
	if err := vac("HTTP", map[string]interface{}{"url": "u", "method": "post"}); err != nil {
		t.Fatalf("HTTP.method case-insensitive: %v", err)
	}
	// enum: driver com alias
	if err := vac("DATABASE", map[string]interface{}{"driver": "oracle", "dsn": "d", "sql": "s"}); err == nil {
		t.Fatal("DATABASE.driver oracle deveria falhar")
	}
	// intlist: escalar, array E string CSV valem (o editor visual emite "200,204")
	if err := vac("HTTP", map[string]interface{}{"url": "u", "expectStatus": 200}); err != nil {
		t.Fatalf("expectStatus escalar: %v", err)
	}
	if err := vac("HTTP", map[string]interface{}{"url": "u", "expectStatus": []interface{}{200, 204}}); err != nil {
		t.Fatalf("expectStatus lista: %v", err)
	}
	if err := vac("HTTP", map[string]interface{}{"url": "u", "expectStatus": "200,204"}); err != nil {
		t.Fatalf("expectStatus CSV: %v", err)
	}
	if err := vac("HTTP", map[string]interface{}{"url": "u", "expectStatus": "2xx"}); err == nil {
		t.Fatal("expectStatus '2xx' deveria falhar")
	}
	// bool: aceita bool e "true"/"false" (executores legados leem string)
	if err := vac("K8S", map[string]interface{}{"image": "i", "insecureTLS": true}); err != nil {
		t.Fatalf("insecureTLS bool: %v", err)
	}
	if err := vac("K8S", map[string]interface{}{"image": "i", "insecureTLS": "true"}); err != nil {
		t.Fatalf("insecureTLS string: %v", err)
	}
	if err := vac("K8S", map[string]interface{}{"image": "i", "insecureTLS": "yes"}); err == nil {
		t.Fatal("insecureTLS 'yes' deveria falhar")
	}
	// scalar: port aceita número e string
	if err := vac("SSH", map[string]interface{}{"host": "h", "command": "c", "port": 22}); err != nil {
		t.Fatalf("SSH.port número: %v", err)
	}
	if err := vac("SSH", map[string]interface{}{"host": "h", "command": "c", "port": "22"}); err != nil {
		t.Fatalf("SSH.port string: %v", err)
	}
	// int: float64 inteiro (JSON) vale; fracionário não
	if err := vac("FILE_WATCH", map[string]interface{}{"path": "/a", "stableSec": float64(10)}); err != nil {
		t.Fatalf("stableSec float64 inteiro: %v", err)
	}
	if err := vac("FILE_WATCH", map[string]interface{}{"path": "/a", "stableSec": 1.5}); err == nil {
		t.Fatal("stableSec 1.5 deveria falhar")
	}
	// map: headers precisa ser mapa
	if err := vac("HTTP", map[string]interface{}{"url": "u", "headers": "Auth: x"}); err == nil {
		t.Fatal("headers string deveria falhar")
	}
}

func TestTypeSchemaCrossField(t *testing.T) {
	// PARALLEL: branches vazio erra
	if err := vac("PARALLEL", map[string]interface{}{"branches": []interface{}{}}); err == nil {
		t.Fatal("PARALLEL.branches vazio deveria falhar")
	}
	// WASM: exige wasmPath OU wasmUrl
	if err := vac("WASM", map[string]interface{}{"args": "-v"}); err == nil {
		t.Fatal("WASM sem path/url deveria falhar")
	}
	if err := vac("WASM", map[string]interface{}{"wasmUrl": "http://x/m.wasm"}); err != nil {
		t.Fatalf("WASM.wasmUrl: %v", err)
	}
}

func TestTypeSchemaOpenWorld(t *testing.T) {
	// jobType desconhecido: params livres (só roda se um agente anunciar a capability)
	if err := vac("MEU_TIPO_CUSTOM", map[string]interface{}{"qualquer": "coisa"}); err != nil {
		t.Fatalf("tipo desconhecido deveria passar: %v", err)
	}
	// jobType vazio segue obrigatório
	if err := vac("", nil); err == nil {
		t.Fatal("jobType vazio deveria falhar")
	}
}

func TestJobTypeCatalog(t *testing.T) {
	cat := JobTypeCatalog()
	if len(cat) < 15 {
		t.Fatalf("catálogo deveria ter todos os tipos, veio %d", len(cat))
	}
	seen := map[string]bool{}
	for _, s := range cat {
		if s.Type == "" || s.Summary == "" {
			t.Fatalf("entrada sem type/summary: %+v", s)
		}
		if seen[s.Type] {
			t.Fatalf("tipo duplicado no catálogo: %s", s.Type)
		}
		seen[s.Type] = true
	}
	for _, want := range []string{"COMMAND", "HTTP", "DATABASE", "FILE_TRANSFER", "LAMBDA", "WASM", "K8S", "GCP_RUN"} {
		if !seen[want] {
			t.Fatalf("catálogo sem %s", want)
		}
	}
}
