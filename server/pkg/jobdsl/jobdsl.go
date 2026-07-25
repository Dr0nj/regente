// Package jobdsl — D-6 Schedule as Code: DSL Go para definir jobs do Regente.
//
// O YAML do workspace continua sendo a fonte da verdade (GitOps); este pacote
// é o jeito PROGRAMÁVEL de produzi-lo — loops, funções e constantes do Go em
// vez de copiar-colar YAML. O output é byte-compatível com os arquivos de
// definitions/ (mesma serialização do FileStore), então o resultado entra pelo
// fluxo normal: commit no repo do workspace (ou colado no modo CODE do Design)
// e o `regente test` valida antes do push.
//
//	w := jobdsl.New()
//	f := w.Folder("financeiro")
//	extract := f.Job("extract-vendas", "Extração de vendas").
//		Command("python extract.py").RunAt("06:00").Retries(2)
//	f.Job("load-dw", "Carga no DW").
//		Command("python load.py").After(extract).SLA(30, "08:00")
//	_ = w.WriteTo("regente-workspace/definitions")   // um .yaml por job
//	fmt.Print(w.YAML())                              // ...ou multi-doc p/ stdout
//
// Deliberadamente sem I/O de rede: gerar é puro; publicar é do Git.
package jobdsl

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Dr0nj/regente-server/internal/domain"
	"gopkg.in/yaml.v3"
)

// Workspace — raiz do DSL: um conjunto de folders com jobs.
type Workspace struct {
	folders []*Folder
}

func New() *Workspace { return &Workspace{} }

// Folder — agrupa jobs de um time (vira definitions/<name>/ no disco).
type Folder struct {
	name string
	jobs []*Job
}

func (w *Workspace) Folder(name string) *Folder {
	for _, f := range w.folders {
		if f.name == name {
			return f
		}
	}
	f := &Folder{name: name}
	w.folders = append(w.folders, f)
	return f
}

// Job — builder fluente sobre domain.JobDefinition.
type Job struct {
	def domain.JobDefinition
}

// Job cria um job na folder. jobType default COMMAND (o mais comum); troque
// com Type/HTTP/Database. Schedule nasce enabled + daily.
func (f *Folder) Job(id, label string) *Job {
	j := &Job{def: domain.JobDefinition{
		ID: id, Label: label, Team: f.name, JobType: "COMMAND",
		Schedule: domain.Schedule{Enabled: true},
	}}
	f.jobs = append(f.jobs, j)
	return j
}

// ── execução ────────────────────────────────────────────────────────────────

func (j *Job) Command(cmd string) *Job {
	j.def.JobType = "COMMAND"
	j.param("command", cmd)
	return j
}

func (j *Job) HTTP(method, url string) *Job {
	j.def.JobType = "HTTP"
	j.param("method", strings.ToUpper(method))
	j.param("url", url)
	return j
}

func (j *Job) Database(driver, dsn, sql string) *Job {
	j.def.JobType = "DATABASE"
	j.param("driver", driver)
	j.param("dsn", dsn)
	j.param("sql", sql)
	return j
}

// Type — jobType arbitrário (GLUE, LAMBDA, FILE_WATCH…) com params livres.
func (j *Job) Type(jobType string, params map[string]any) *Job {
	j.def.JobType = jobType
	for k, v := range params {
		j.param(k, v)
	}
	return j
}

func (j *Job) Agent(agentID string) *Job { j.def.AgentID = agentID; return j }
func (j *Job) Timeout(seconds int) *Job  { j.def.Timeout = seconds; return j }
func (j *Job) Retries(n int) *Job        { j.def.Retries = n; return j }

// RetryDelay — D-1: espaçamento (minutos) entre tentativas, agendado durável.
func (j *Job) RetryDelay(minutes int) *Job { j.def.RetryDelayMin = minutes; return j }

func (j *Job) DryRun() *Job      { j.def.DryRun = true; return j }
func (j *Job) Confirm() *Job     { j.def.Confirm = true; return j }
func (j *Job) SelfService() *Job { j.def.SelfService = true; return j }

// ── quando roda ─────────────────────────────────────────────────────────────

func (j *Job) Description(d string) *Job     { j.def.Schedule.Description = d; return j }
func (j *Job) RunAt(hhmm string) *Job        { j.def.Schedule.RunAt = hhmm; return j }
func (j *Job) Window(from, to string) *Job   { j.def.Schedule.WindowFrom, j.def.Schedule.WindowTo = from, to; return j }
func (j *Job) Calendar(name string) *Job     { j.def.Calendars = append(j.def.Calendars, domain.CalendarRef{Name: name, Mode: "include"}); return j }
func (j *Job) ExcludeCalendar(n string) *Job { j.def.Calendars = append(j.def.Calendars, domain.CalendarRef{Name: n, Mode: "exclude"}); return j }
func (j *Job) KeepActive(days int) *Job      { j.def.Schedule.KeepActive = days; return j }

func (j *Job) Daily() *Job { j.def.Schedule.Frequency = "daily"; return j }

func (j *Job) Weekly(days ...string) *Job {
	j.def.Schedule.Frequency = "weekly"
	j.def.Schedule.DaysOfWeek = days
	return j
}

func (j *Job) Monthly(daysOfMonth ...int) *Job {
	j.def.Schedule.Frequency = "monthly"
	j.def.Schedule.DaysOfMonth = daysOfMonth
	return j
}

// BusinessDay — o N-ésimo dia útil do mês (5 = 5º; -1 = último).
func (j *Job) BusinessDay(nth ...int) *Job {
	j.def.Schedule.Frequency = "businessday"
	j.def.Schedule.NthBusinessDays = nth
	return j
}

func (j *Job) Cyclic(intervalMin int) *Job {
	j.def.Schedule.Cyclic = true
	j.def.Schedule.IntervalMin = intervalMin
	return j
}

// ── grafo / gates ───────────────────────────────────────────────────────────

// After — dependência on-success de outros jobs (aceita *Job ou id string).
func (j *Job) After(jobs ...any) *Job { return j.dep(domain.CondOnSuccess, jobs...) }

// AfterFailure / AfterAny — arestas on-failure / on-complete.
func (j *Job) AfterFailure(jobs ...any) *Job { return j.dep(domain.CondOnFailure, jobs...) }
func (j *Job) AfterAny(jobs ...any) *Job     { return j.dep(domain.CondOnComplete, jobs...) }

func (j *Job) dep(cond domain.EdgeCondition, jobs ...any) *Job {
	for _, v := range jobs {
		switch x := v.(type) {
		case *Job:
			j.def.Upstream = append(j.def.Upstream, domain.Upstream{From: x.def.ID, Condition: cond})
		case string:
			j.def.Upstream = append(j.def.Upstream, domain.Upstream{From: x, Condition: cond})
		default:
			panic(fmt.Sprintf("jobdsl: After aceita *Job ou string, veio %T", v))
		}
	}
	return j
}

func (j *Job) Needs(conditions ...string) *Job {
	j.def.ConditionsIn = append(j.def.ConditionsIn, conditions...)
	return j
}

func (j *Job) Produces(conditions ...string) *Job {
	j.def.ConditionsOutAdd = append(j.def.ConditionsOutAdd, conditions...)
	return j
}

func (j *Job) Resource(name string, qty int) *Job {
	if j.def.Resources == nil {
		j.def.Resources = map[string]int{}
	}
	j.def.Resources[name] = qty
	return j
}

func (j *Job) Var(name, value string) *Job {
	if j.def.Variables == nil {
		j.def.Variables = map[string]string{}
	}
	j.def.Variables[name] = value
	return j
}

// SLA — duração esperada (min) e/ou deadline "HH:MM" ("" = sem deadline).
func (j *Job) SLA(expectedMin int, deadlineHM string) *Job {
	j.def.SLA = &domain.SLASpec{ExpectedDurationMin: expectedMin, DeadlineHM: deadlineHM}
	return j
}

// On — regra On/Do crua (para o que os atalhos não cobrem).
func (j *Job) On(rule domain.ActionRule) *Job {
	j.def.Actions = append(j.def.Actions, rule)
	return j
}

// NotifyOnFailure — atalho do On/Do mais comum.
func (j *Job) NotifyOnFailure(message string, channels ...string) *Job {
	return j.On(domain.ActionRule{
		On: "result", Status: "NOTOK", Do: "notify",
		Message: message, Severity: "critical", Channels: channels,
	})
}

func (j *Job) param(k string, v any) {
	if j.def.Params == nil {
		j.def.Params = map[string]any{}
	}
	j.def.Params[k] = v
}

// Definition — a JobDefinition final (escape hatch p/ o que o builder não expõe).
func (j *Job) Definition() *domain.JobDefinition { return &j.def }

// ── output ──────────────────────────────────────────────────────────────────

// Validate — erros estruturais baratos ANTES de escrever (id/label vazios,
// id duplicado, dep para job desconhecido DENTRO do workspace gerado).
func (w *Workspace) Validate() []error {
	var errs []error
	ids := map[string]bool{}
	for _, f := range w.folders {
		for _, j := range f.jobs {
			d := j.def
			if strings.TrimSpace(d.ID) == "" {
				errs = append(errs, fmt.Errorf("folder %s: job without an id", f.name))
				continue
			}
			if ids[d.ID] {
				errs = append(errs, fmt.Errorf("duplicate id: %s", d.ID))
			}
			ids[d.ID] = true
			if strings.TrimSpace(d.Label) == "" {
				errs = append(errs, fmt.Errorf("%s: label is required", d.ID))
			}
		}
	}
	for _, f := range w.folders {
		for _, j := range f.jobs {
			for _, u := range j.def.Upstream {
				if !ids[u.From] {
					errs = append(errs, fmt.Errorf("%s: upstream %q does not exist in this workspace", j.def.ID, u.From))
				}
			}
		}
	}
	return errs
}

// YAML — documento multi-doc (mesmo dialeto do modo CODE do Design), ordenado
// por folder/id (output determinístico = diffs limpos).
func (w *Workspace) YAML() string {
	var b strings.Builder
	for _, f := range w.sorted() {
		for _, j := range f.jobs {
			raw, err := yaml.Marshal(&j.def)
			if err != nil {
				continue
			}
			b.WriteString("---\n")
			fmt.Fprintf(&b, "# definitions/%s/%s.yaml\n", j.def.Team, j.def.ID)
			b.Write(raw)
		}
	}
	return b.String()
}

// WriteTo — grava um arquivo por job em <dir>/<team>/<id>.yaml (o layout do
// workspace). Valida antes; qualquer erro estrutural aborta sem escrever nada.
func (w *Workspace) WriteTo(dir string) error {
	if errs := w.Validate(); len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return fmt.Errorf("invalid workspace:\n  %s", strings.Join(msgs, "\n  "))
	}
	for _, f := range w.sorted() {
		fdir := filepath.Join(dir, f.name)
		if err := os.MkdirAll(fdir, 0o755); err != nil {
			return err
		}
		for _, j := range f.jobs {
			raw, err := yaml.Marshal(&j.def)
			if err != nil {
				return fmt.Errorf("%s: %w", j.def.ID, err)
			}
			if err := os.WriteFile(filepath.Join(fdir, j.def.ID+".yaml"), raw, 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *Workspace) sorted() []*Folder {
	fs := make([]*Folder, len(w.folders))
	copy(fs, w.folders)
	sort.Slice(fs, func(i, k int) bool { return fs[i].name < fs[k].name })
	for _, f := range fs {
		sort.Slice(f.jobs, func(i, k int) bool { return f.jobs[i].def.ID < f.jobs[k].def.ID })
	}
	return fs
}
