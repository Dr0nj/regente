package scheduler

import (
	"strings"
	"testing"
	"time"

	"github.com/Dr0nj/regente-server/internal/domain"
)

// %%NAME (Control-M AutoEdit) e ${var.NAME} resolvem pelos MESMOS escopos.
func TestInterpolate_CtmPercentSyntax(t *testing.T) {
	ctx := VarContext{
		Runtime:    map[string]string{"ODATE": "20260702", "JOBNAME": "carga-fin"},
		Definition: map[string]string{"ambiente": "prod"},
		Global:     map[string]string{"bucket": "s3://acme"},
	}

	cases := []struct{ in, want string }{
		{"run.sh %%ODATE", "run.sh 20260702"},
		{"run.sh ${var.ODATE}", "run.sh 20260702"},
		{"%%JOBNAME-%%ODATE.log", "carga-fin-20260702.log"},
		{"deploy --env %%ambiente --dest %%bucket", "deploy --env prod --dest s3://acme"},
		// não resolvido fica INTACTO (visibilidade)
		{"echo %%NAO_EXISTE", "echo %%NAO_EXISTE"},
		// %% sem nome válido depois fica intacto
		{"100%% de certeza", "100%% de certeza"},
	}
	for _, c := range cases {
		if got := InterpolateString(c.in, ctx); got != c.want {
			t.Fatalf("InterpolateString(%q) = %q, esperava %q", c.in, got, c.want)
		}
	}
}

// BuildContext injeta os tokens Control-M em MAIÚSCULAS derivados do runtime.
func TestBuildContext_CtmTokens(t *testing.T) {
	def := domain.JobDefinition{ID: "job-x", Label: "Job X", Team: "FIN"}
	ctx := BuildContext(def, "job-x-2026-07-02", "2026-07-02", nil, "")

	wants := map[string]string{
		"ODATE":      "20260702",
		"ORDERDATE":  "2026-07-02",
		"JOBNAME":    "job-x",
		"JOBLABEL":   "Job X",
		"FOLDER":     "FIN",
		"INSTANCEID": "job-x-2026-07-02",
	}
	for k, want := range wants {
		if got, ok := ctx.Runtime[k]; !ok || got != want {
			t.Fatalf("Runtime[%s] = %q (ok=%v), esperava %q", k, got, ok, want)
		}
	}
	// RUNDATE/TIME existem e têm o formato certo (valor depende do relógio).
	if got := ctx.Runtime["RUNDATE"]; len(got) != 8 || !strings.HasPrefix(got, time.Now().Format("2006")) {
		t.Fatalf("RUNDATE = %q, esperava YYYYMMDD de hoje", got)
	}
	if got := ctx.Runtime["TIME"]; len(got) != 6 {
		t.Fatalf("TIME = %q, esperava HHMMSS", got)
	}
}

// Params aninhados (maps/arrays) também interpolam %%.
func TestInterpolateParams_NestedPercent(t *testing.T) {
	ctx := VarContext{Runtime: map[string]string{"ODATE": "20260702"}}
	params := map[string]interface{}{
		"command": "proc.sh %%ODATE",
		"env":     map[string]interface{}{"DATA": "%%ODATE"},
		"args":    []interface{}{"--dt", "%%ODATE"},
	}
	out := InterpolateParams(params, ctx)
	if out["command"] != "proc.sh 20260702" {
		t.Fatalf("command = %v", out["command"])
	}
	if env := out["env"].(map[string]interface{}); env["DATA"] != "20260702" {
		t.Fatalf("env.DATA = %v", env["DATA"])
	}
	if args := out["args"].([]interface{}); args[1] != "20260702" {
		t.Fatalf("args[1] = %v", args[1])
	}
}
