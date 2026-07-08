// regente test — D-7 Testing framework: valida e SIMULA definitions sem servidor.
//
// Aceita um arquivo YAML multi-doc (`regente test job.yaml`) ou um diretório de
// workspace (`regente test ./regente-workspace`). O pipeline de verificação:
//
//	1. parse ESTRITO (campo desconhecido = erro — pega typo antes do runtime)
//	2. validação estrutural (id/label/team, actionConfig por jobType)
//	3. grafo: upstream para job inexistente · CICLO de dependências
//	4. policy as code (D-10): policies.yaml do workspace, se houver
//	5. simulação da daily de -date com o MESMO engine do servidor (DryRun /
//	   IsScheduledOn — fonte única): quem RODA, quem ESPERA, quem NUNCA dispara
//
// Exit code 0 = passou (warnings ok) · 1 = falhou — para usar direto no CI do
// repo de workspace: o job quebrado não chega ao push.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/domain"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/policy"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
	"gopkg.in/yaml.v3"
)

type testReport struct {
	Target     string             `json:"target"`
	Date       string             `json:"date"`
	Jobs       int                `json:"jobs"`
	Errors     []string           `json:"errors"`
	Warnings   []string           `json:"warnings"`
	Policy     []policy.Violation `json:"policyViolations,omitempty"`
	Simulation *scheduler.DryRun  `json:"simulation,omitempty"`
	Passed     bool               `json:"passed"`
}

func cmdTest(args []string) error {
	fs := flag.NewFlagSet("test", flag.ExitOnError)
	date := fs.String("date", time.Now().Format("2006-01-02"), "data da daily simulada (YYYY-MM-DD)")
	jsonOut := fs.Bool("json", false, "saída JSON (CI)")
	_ = fs.Parse(reorderArgs(args, "json"))
	if fs.NArg() < 1 {
		return errors.New("uso: regente test <job.yaml | workspace-dir> [-date YYYY-MM-DD] [-json]")
	}
	target := fs.Arg(0)

	rep := testReport{Target: target, Date: *date}
	defs, wsRoot, err := loadTarget(target, &rep)
	if err != nil {
		return err
	}
	rep.Jobs = len(defs)

	// 2. validação estrutural — a MESMA regra do save da API.
	for _, d := range defs {
		if err := domain.ValidateDefinition(d); err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: %v", d.ID, err))
		}
	}

	// 3. grafo: deps órfãs + ciclos.
	byID := map[string]bool{}
	for _, d := range defs {
		byID[d.ID] = true
	}
	for _, d := range defs {
		for _, u := range d.Upstream {
			if !byID[u.From] {
				rep.Errors = append(rep.Errors, fmt.Sprintf("%s: upstream %q não existe", d.ID, u.From))
			}
		}
	}
	for _, cyc := range findCycles(defs) {
		rep.Errors = append(rep.Errors, "ciclo de dependências: "+strings.Join(cyc, " → "))
	}

	// 4. policy as code (se o workspace tiver policies.yaml).
	if pol, perr := policy.Load(wsRoot); perr != nil {
		rep.Errors = append(rep.Errors, "policies.yaml: "+perr.Error())
	} else if pol.Active() {
		rep.Policy = pol.Validate(defs)
		for _, v := range rep.Policy {
			msg := fmt.Sprintf("[%s] %s/%s: %s", v.Rule, v.Folder, v.DefID, v.Message)
			if pol.Blocking() {
				rep.Errors = append(rep.Errors, "policy: "+msg)
			} else {
				rep.Warnings = append(rep.Warnings, "policy(warn): "+msg)
			}
		}
	}

	// 5. simulação com o engine REAL (scheduler efêmero sobre SQLite temp).
	if len(rep.Errors) == 0 {
		sim, err := simulate(wsRoot, *date)
		if err != nil {
			rep.Errors = append(rep.Errors, "simulação: "+err.Error())
		} else {
			rep.Simulation = sim
			for _, j := range sim.Jobs {
				if j.Outcome == scheduler.DryRunBlocked {
					rep.Warnings = append(rep.Warnings,
						fmt.Sprintf("BLOCKED %s: %s (condition externa via /api/events/ingest?)", j.DefID, j.Reason))
				}
			}
		}
	}

	rep.Passed = len(rep.Errors) == 0
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
	} else {
		printReport(rep)
	}
	if !rep.Passed {
		os.Exit(1)
	}
	return nil
}

// loadTarget resolve o alvo em ([]defs, workspaceRoot). Arquivo = multi-doc
// estrito gravado num workspace TEMP (para o engine ler igual produção);
// diretório = usado como workspace direto (parse estrito arquivo a arquivo).
func loadTarget(target string, rep *testReport) ([]domain.JobDefinition, string, error) {
	st, err := os.Stat(target)
	if err != nil {
		return nil, "", err
	}
	if st.IsDir() {
		root := target
		if _, err := os.Stat(filepath.Join(target, "definitions")); err != nil {
			return nil, "", fmt.Errorf("%s não parece um workspace (sem definitions/)", target)
		}
		defs := strictLoadWorkspace(root, rep)
		return defs, root, nil
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		return nil, "", err
	}
	defs := parseStrictDocs(string(raw), rep, target)
	// workspace temp com o layout real; policies.yaml vizinho do alvo vale aqui
	// (testar um job de dentro do workspace herda a política dele).
	root, err := os.MkdirTemp("", "regente-test-*")
	if err != nil {
		return nil, "", err
	}
	store := storage.NewFileStore(root, false)
	for i := range defs {
		if defs[i].Team == "" {
			defs[i].Team = "default"
		}
		if err := store.Save(defs[i]); err != nil {
			return nil, "", err
		}
	}
	if polPath := findPolicies(filepath.Dir(target)); polPath != "" {
		if b, err := os.ReadFile(polPath); err == nil {
			_ = os.WriteFile(filepath.Join(root, policy.FileName), b, 0o644)
		}
	}
	return defs, root, nil
}

// strictLoadWorkspace re-parseia cada YAML do workspace em modo ESTRITO (o
// FileStore de produção é leniente por compat; o `test` existe pra pegar typo).
func strictLoadWorkspace(root string, rep *testReport) []domain.JobDefinition {
	var defs []domain.JobDefinition
	dir := filepath.Join(root, "definitions")
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == ".regente-folder.yaml" ||
			(!strings.HasSuffix(d.Name(), ".yaml") && !strings.HasSuffix(d.Name(), ".yml")) {
			return nil
		}
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			rep.Errors = append(rep.Errors, path+": "+rerr.Error())
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		for _, def := range parseStrictDocs(string(raw), rep, rel) {
			if def.Team == "" { // team implícito = a pasta (mesma regra do FileStore)
				def.Team = filepath.Base(filepath.Dir(path))
			}
			defs = append(defs, def)
		}
		return nil
	})
	return defs
}

func parseStrictDocs(code string, rep *testReport, origin string) []domain.JobDefinition {
	var out []domain.JobDefinition
	dec := yaml.NewDecoder(strings.NewReader(code))
	dec.KnownFields(true)
	for docN := 1; ; docN++ {
		var def domain.JobDefinition
		err := dec.Decode(&def)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s doc#%d: %v", origin, docN, err))
			break
		}
		if def.ID == "" && def.Label == "" && def.JobType == "" {
			continue
		}
		out = append(out, def)
	}
	return out
}

// findPolicies sobe até 3 níveis procurando policies.yaml (job.yaml costuma
// viver em <ws>/definitions/<team>/, dois níveis abaixo da política).
func findPolicies(dir string) string {
	for i := 0; i < 3; i++ {
		p := filepath.Join(dir, policy.FileName)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// simulate monta um scheduler EFÊMERO (SQLite temp, sem rede) sobre o workspace
// e roda o DryRun — a mesma decisão de agendamento/bloqueio do servidor.
func simulate(wsRoot, date string) (*scheduler.DryRun, error) {
	tmp, err := os.MkdirTemp("", "regente-sim-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	database, err := db.Open(db.SQLite, filepath.Join(tmp, "sim.db"))
	if err != nil {
		return nil, err
	}
	defer database.Close()
	if err := db.Migrate(database); err != nil {
		return nil, err
	}
	s := scheduler.New(storage.NewFileStore(wsRoot, false), database, hub.New(), time.Hour)
	defer s.Stop()
	s.AttachCalendars(storage.NewCalendarStore(wsRoot))
	s.ReloadDefs()
	dr, err := s.DryRun(date)
	if err != nil {
		return nil, err
	}
	return &dr, nil
}

// findCycles — DFS com pilha; devolve um caminho por ciclo encontrado.
func findCycles(defs []domain.JobDefinition) [][]string {
	up := map[string][]string{}
	for _, d := range defs {
		for _, u := range d.Upstream {
			up[d.ID] = append(up[d.ID], u.From)
		}
	}
	const white, gray, black = 0, 1, 2
	color := map[string]int{}
	var cycles [][]string
	var stack []string
	var dfs func(string)
	dfs = func(n string) {
		color[n] = gray
		stack = append(stack, n)
		for _, m := range up[n] {
			switch color[m] {
			case white:
				dfs(m)
			case gray: // aresta de retorno = ciclo; recorta o trecho da pilha
				for i, s := range stack {
					if s == m {
						cycles = append(cycles, append(append([]string{}, stack[i:]...), m))
						break
					}
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
	}
	for _, d := range defs {
		if color[d.ID] == white {
			dfs(d.ID)
		}
	}
	return cycles
}

func printReport(rep testReport) {
	fmt.Printf("regente test — %s (daily simulada: %s)\n", rep.Target, rep.Date)
	fmt.Printf("  jobs: %d\n", rep.Jobs)
	if rep.Simulation != nil {
		c := rep.Simulation.Counts
		fmt.Printf("  simulação: %d RUN · %d WAIT · %d BLOCKED · %d fora do calendário\n",
			c.Run, c.Wait, c.Blocked, c.NotScheduled)
	}
	for _, w := range rep.Warnings {
		fmt.Println("  ⚠ " + w)
	}
	for _, e := range rep.Errors {
		fmt.Println("  ✗ " + e)
	}
	if rep.Passed {
		fmt.Println("PASS")
	} else {
		fmt.Printf("FAIL (%d erro(s))\n", len(rep.Errors))
	}
}
