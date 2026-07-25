// importctm — E6: importador Control-M → workspace Regente.
//
// Lê o XML de export do Control-M (DEFTABLE/FOLDER/JOB, formato do
// `ctm export`/forecast) e gera um workspace Regente local:
//
//	<out>/definitions/<folder>/<job>.yaml   (dialeto EXATO do FileStore)
//	<out>/calendars/<name>.yaml             (stubs dos calendars referenciados)
//	<out>/import-report.md                  (o que mapeou, o que não, e por quê)
//
// Filosofia v1: importar o MÁXIMO com fidelidade e NUNCA inventar semântica —
// tudo que não mapeia vira comentário `# TODO-import:` no YAML do job + linha
// no relatório. NUNCA faz push: os arquivos são locais, o usuário revisa e
// commita no repo de workspace. Mapeamentos documentados no README deste cmd.
//
// Uso:
//
//	importctm -in export.xml -out ./workspace [-dry-run] [-folder-filter FIN]
package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Dr0nj/regente-server/internal/domain"
	"gopkg.in/yaml.v3"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "importctm:", err)
		os.Exit(1)
	}
}

/* ── modelo XML do export Control-M ─────────────────────────────── */

type defTable struct {
	XMLName xml.Name    `xml:"DEFTABLE"`
	Folders []ctmFolder `xml:"FOLDER"`
	Smart   []ctmFolder `xml:"SMART_FOLDER"`
}

type ctmFolder struct {
	Name       string   `xml:"FOLDER_NAME,attr"`
	Datacenter string   `xml:"DATACENTER,attr"`
	Jobs       []ctmJob `xml:"JOB"`
}

type ctmJob struct {
	JobName      string `xml:"JOBNAME,attr"`
	Description  string `xml:"DESCRIPTION,attr"`
	TaskType     string `xml:"TASKTYPE,attr"`
	CmdLine      string `xml:"CMDLINE,attr"`
	ParentFolder string `xml:"PARENT_FOLDER,attr"`
	TimeFrom     string `xml:"TIMEFROM,attr"`
	TimeTo       string `xml:"TIMETO,attr"`
	Days         string `xml:"DAYS,attr"`
	Weekdays     string `xml:"WEEKDAYS,attr"`
	DaysCal      string `xml:"DAYSCAL,attr"`
	WeeksCal     string `xml:"WEEKSCAL,attr"`
	ConfCal      string `xml:"CONFCAL,attr"`
	Shift        string `xml:"SHIFT,attr"`
	MaxRerun     string `xml:"MAXRERUN,attr"`
	Cyclic       string `xml:"CYCLIC,attr"`
	Interval     string `xml:"INTERVAL,attr"`
	FileName     string `xml:"FILE_NAME,attr"` // FileWatcher
	FilePath     string `xml:"FILE_PATH,attr"`

	// Ignorados com AVISO no relatório (metadados/conceitos sem equivalente 1:1).
	SubApplication string `xml:"SUB_APPLICATION,attr"`
	Application    string `xml:"APPLICATION,attr"`
	Datacenter     string `xml:"DATACENTER,attr"`
	RunAs          string `xml:"RUN_AS,attr"`
	NodeID         string `xml:"NODEID,attr"`
	CreatedBy      string `xml:"CREATED_BY,attr"`
	Author         string `xml:"AUTHOR,attr"`
	MemName        string `xml:"MEMNAME,attr"`
	MemLib         string `xml:"MEMLIB,attr"`

	// Qualquer atributo NÃO capturado acima cai aqui → vira TODO-import.
	Extra []xml.Attr `xml:",any,attr"`

	InConds  []ctmCond  `xml:"INCOND"`
	OutConds []ctmCond  `xml:"OUTCOND"`
	Shouts   []ctmShout `xml:"SHOUT"`
	Vars     []ctmVar   `xml:"VARIABLE"`
}

type ctmCond struct {
	Name  string `xml:"NAME,attr"`
	ODate string `xml:"ODATE,attr"`
	AndOr string `xml:"AND_OR,attr"` // INCOND: A(nd) | O(r)
	Sign  string `xml:"SIGN,attr"`   // OUTCOND: + | -
}

type ctmShout struct {
	When    string `xml:"WHEN,attr"` // OK | NOTOK | LATE | EXECTIME…
	Urgency string `xml:"URGENCY,attr"`
	Dest    string `xml:"DEST,attr"`
	Message string `xml:"MESSAGE,attr"`
}

type ctmVar struct {
	Name  string `xml:"NAME,attr"` // "%%PARM1"
	Value string `xml:"VALUE,attr"`
}

/* ── resultado por job ───────────────────────────────────────────── */

type jobResult struct {
	Folder, JobName, ID string
	Status              string // "ok" | "partial" | "skipped"
	Todos               []string
	Warnings            []string
	SkipReason          string
}

/* ── entrypoint testável ─────────────────────────────────────────── */

func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("importctm", flag.ContinueOnError)
	in := fs.String("in", "", "Control-M export XML (DEFTABLE)")
	out := fs.String("out", "./workspace", "output directory for the Regente workspace")
	dryRun := fs.Bool("dry-run", false, "only analyze and print the report; writes NOTHING")
	folderFilter := fs.String("folder-filter", "", "import only the folder with this name (FOLDER_NAME)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" {
		return fmt.Errorf("missing -in export.xml")
	}
	raw, err := os.ReadFile(*in)
	if err != nil {
		return err
	}
	var table defTable
	if err := xml.Unmarshal(raw, &table); err != nil {
		return fmt.Errorf("invalid XML (expected a DEFTABLE from the ctm export): %w", err)
	}
	folders := append(append([]ctmFolder{}, table.Folders...), table.Smart...)
	if *folderFilter != "" {
		kept := folders[:0]
		for _, f := range folders {
			if f.Name == *folderFilter {
				kept = append(kept, f)
			}
		}
		folders = kept
		if len(folders) == 0 {
			return fmt.Errorf("no FOLDER %q in the export", *folderFilter)
		}
	}

	// Passo 1 — índice global de OUTCONDs (SIGN=+): cond → jobs emissores.
	// É o que decide INCOND→upstream (1 emissor) vs conditionsIn (0 ou 2+).
	emitters := map[string][]string{}
	ids := map[string]string{} // folder/JOBNAME → id (slug, único por folder)
	used := map[string]bool{}
	for _, f := range folders {
		for _, j := range f.Jobs {
			id := slug(j.JobName)
			for i := 2; used[id]; i++ {
				id = fmt.Sprintf("%s-%d", slug(j.JobName), i)
			}
			used[id] = true
			ids[f.Name+"/"+j.JobName] = id
			for _, oc := range j.OutConds {
				if oc.Sign == "" || oc.Sign == "+" {
					emitters[oc.Name] = append(emitters[oc.Name], id)
				}
			}
		}
	}

	// Passo 2 — mapeia job a job.
	var results []jobResult
	defs := map[string]domain.JobDefinition{} // id → def
	calendars := map[string]string{}          // slug do calendar → nome original CTM
	for _, f := range folders {
		for _, j := range f.Jobs {
			res, def := mapJob(f, j, ids, emitters, calendars)
			results = append(results, res)
			if res.Status != "skipped" {
				defs[def.ID] = def
			}
		}
	}

	// Passo 3 — escreve (ou só reporta, em dry-run).
	report := buildReport(*in, results, calendars)
	if *dryRun {
		fmt.Fprintln(stdout, report)
		fmt.Fprintln(stdout, "-- dry-run: NOTHING was written --")
		return nil
	}
	for _, res := range results {
		if res.Status == "skipped" {
			continue
		}
		def := defs[res.ID]
		buf, err := yaml.Marshal(&def) // MESMO dialeto do FileStore.Save
		if err != nil {
			return fmt.Errorf("%s: yaml: %w", res.ID, err)
		}
		for _, todo := range res.Todos {
			buf = append(buf, []byte("# TODO-import: "+todo+"\n")...)
		}
		dir := filepath.Join(*out, "definitions", def.Team)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, def.ID+".yaml"), buf, 0o644); err != nil {
			return err
		}
	}
	for calSlug, orig := range calendars {
		cal := domain.Calendar{
			Name:         calSlug,
			BusinessDays: []string{"mon", "tue", "wed", "thu", "fri"},
		}
		buf, _ := yaml.Marshal(&cal)
		buf = append(buf, []byte("# TODO-import: fill in holidays/exceptions from the Control-M calendar \""+orig+"\"\n")...)
		dir := filepath.Join(*out, "calendars")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		path := filepath.Join(dir, calSlug+".yaml")
		if _, err := os.Stat(path); err == nil {
			continue // não sobrescreve calendar que o usuário já tem
		}
		if err := os.WriteFile(path, buf, 0o644); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, "import-report.md"), []byte(report), 0o644); err != nil {
		return err
	}
	fmt.Fprintln(stdout, report)
	fmt.Fprintf(stdout, "workspace generated at %s — review it and commit to your workspace repo (the importer NEVER pushes).\n", *out)
	return nil
}

/* ── mapeamento de um JOB ────────────────────────────────────────── */

func mapJob(f ctmFolder, j ctmJob, ids map[string]string, emitters map[string][]string, calendars map[string]string) (jobResult, domain.JobDefinition) {
	res := jobResult{Folder: f.Name, JobName: j.JobName, ID: ids[f.Name+"/"+j.JobName]}
	todo := func(format string, a ...any) { res.Todos = append(res.Todos, fmt.Sprintf(format, a...)) }
	warn := func(format string, a ...any) { res.Warnings = append(res.Warnings, fmt.Sprintf(format, a...)) }

	team := strings.TrimSpace(j.ParentFolder)
	if team == "" {
		team = strings.TrimSpace(f.Name)
	}
	def := domain.JobDefinition{
		ID:       res.ID,
		Label:    firstNonEmpty(strings.TrimSpace(j.Description), j.JobName),
		Team:     team,
		Schedule: domain.Schedule{Enabled: true, Frequency: "daily"},
		Params:   map[string]interface{}{},
	}

	// TASKTYPE → jobType.
	switch strings.ToLower(strings.TrimSpace(j.TaskType)) {
	case "job", "command", "":
		def.JobType = "COMMAND"
		if cmd := strings.TrimSpace(j.CmdLine); cmd != "" {
			def.Params["command"] = cmd
		} else {
			todo("empty CMDLINE — set params.command")
		}
	case "dummy":
		// Dummy = não executa nada: COMMAND em dryRun (o 👻 do Regente).
		def.JobType = "COMMAND"
		def.DryRun = true
		def.Params["command"] = "true"
	case "filewatcher", "filewatch":
		def.JobType = "FILE_WATCH"
		if p := firstNonEmpty(strings.TrimSpace(j.FileName), strings.TrimSpace(j.FilePath)); p != "" {
			def.Params["path"] = p
		} else {
			todo("FileWatcher without FILE_NAME/FILE_PATH — set params.path")
		}
	default:
		res.Status = "skipped"
		res.SkipReason = fmt.Sprintf("TASKTYPE %q not supported in v1 (supported: Job/Command/Dummy/FileWatcher)", j.TaskType)
		return res, def
	}

	// TIMEFROM/TIMETO → runAt / windowTo ("1430" → "14:30").
	if hm, ok := ctmTime(j.TimeFrom); ok {
		def.Schedule.RunAt = hm
	} else if strings.TrimSpace(j.TimeFrom) != "" {
		todo("TIMEFROM %q does not parse as HHMM", j.TimeFrom)
	}
	if hm, ok := ctmTime(j.TimeTo); ok {
		def.Schedule.WindowTo = hm
	} else if strings.TrimSpace(j.TimeTo) != "" {
		todo("TIMETO %q does not parse as HHMM", j.TimeTo)
	}

	// DAYS/WEEKDAYS → frequency estruturada.
	mapRecurrence(j, &def, todo)

	// Calendars: DAYSCAL/WEEKSCAL → include; CONFCAL (+SHIFT) → include + shift.
	addCal := func(name string) {
		n := strings.TrimSpace(name)
		if n == "" {
			return
		}
		s := slug(n)
		calendars[s] = n
		for _, c := range def.Calendars { // DAYSCAL e CONFCAL podem ser o MESMO calendar
			if c.Name == s {
				return
			}
		}
		def.Calendars = append(def.Calendars, domain.CalendarRef{Name: s, Mode: "include"})
	}
	addCal(j.DaysCal)
	addCal(j.WeeksCal)
	if strings.TrimSpace(j.ConfCal) != "" {
		addCal(j.ConfCal)
		switch strings.TrimSpace(j.Shift) {
		case ">":
			def.Schedule.Shift = "next-businessday"
		case "<":
			def.Schedule.Shift = "prev-businessday"
		case "":
			// CONFCAL sem shift: só confina ao calendar (include já cobre).
		default:
			todo("SHIFT %q not mapped (v1: '>' → next-businessday, '<' → prev-businessday)", j.Shift)
		}
	} else if strings.TrimSpace(j.Shift) != "" {
		todo("SHIFT %q without CONFCAL — review the confirmation calendar", j.Shift)
	}

	// MAXRERUN → retries.
	if v := strings.TrimSpace(j.MaxRerun); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			def.Retries = n
		} else {
			todo("MAXRERUN %q is not numeric", v)
		}
	}

	// CYCLIC + INTERVAL → schedule.cyclic/intervalMin.
	if isTruthy(j.Cyclic) {
		def.Schedule.Cyclic = true
		if min, ok := ctmInterval(j.Interval); ok {
			def.Schedule.IntervalMin = min
		} else {
			todo("INTERVAL %q does not parse (expected NNNNN[M|H|D]) — set schedule.intervalMin", j.Interval)
		}
	}

	// INCOND → upstream (cond com EXATAMENTE 1 emissor) ou conditionsIn.
	for _, ic := range j.InConds {
		name := strings.TrimSpace(ic.Name)
		if name == "" {
			continue
		}
		if ic.ODate != "" && !strings.EqualFold(ic.ODate, "ODAT") {
			todo("INCOND %q with ODATE=%q (scope beyond the current day) — review it", name, ic.ODate)
		}
		if strings.EqualFold(ic.AndOr, "O") {
			todo("INCOND %q with AND_OR=O (OR) — Regente treats conditionsIn as AND; review it", name)
		}
		ems := emitters[name]
		if len(ems) == 1 && ems[0] != def.ID {
			def.Upstream = append(def.Upstream, domain.Upstream{From: ems[0], Condition: domain.CondOnSuccess})
		} else {
			def.ConditionsIn = append(def.ConditionsIn, name)
		}
	}
	// OUTCOND: SIGN + vira conditionsOutAdd, EXCETO quando a cond foi
	// convertida em aresta (1 emissor — a dependência já está no upstream do
	// consumidor); SIGN - vira conditionsOutRemove.
	for _, oc := range j.OutConds {
		name := strings.TrimSpace(oc.Name)
		if name == "" {
			continue
		}
		switch oc.Sign {
		case "", "+":
			if len(emitters[name]) != 1 {
				def.ConditionsOutAdd = append(def.ConditionsOutAdd, name)
			}
		case "-":
			def.ConditionsOutRemove = append(def.ConditionsOutRemove, name)
		default:
			todo("OUTCOND %q with SIGN=%q not mapped", name, oc.Sign)
		}
	}

	// SHOUT → actions notify (On result OK/NOTOK).
	for _, sh := range j.Shouts {
		switch strings.ToUpper(strings.TrimSpace(sh.When)) {
		case "OK", "NOTOK":
			def.Actions = append(def.Actions, domain.ActionRule{
				On: "result", Status: strings.ToUpper(strings.TrimSpace(sh.When)),
				Do: "notify", Message: firstNonEmpty(sh.Message, "Control-M SHOUT"),
				Severity: shoutSeverity(sh.Urgency),
			})
			if d := strings.TrimSpace(sh.Dest); d != "" {
				warn("SHOUT DEST=%q ignored — the notify channels are Regente's alerting sinks", d)
			}
		default:
			todo("SHOUT WHEN=%q not mapped (v1: OK/NOTOK)", sh.When)
		}
	}

	// VARIABLE %%NOME → variables (escopo definition).
	for _, v := range j.Vars {
		name := strings.TrimPrefix(strings.TrimSpace(v.Name), "%%")
		if name == "" {
			continue
		}
		if def.Variables == nil {
			def.Variables = map[string]string{}
		}
		def.Variables[name] = v.Value
	}

	// Ignorados com aviso (conceitos sem equivalente 1:1 no Regente v1).
	for k, v := range map[string]string{
		"SUB_APPLICATION": j.SubApplication, "APPLICATION": j.Application,
		"DATACENTER": j.Datacenter, "RUN_AS": j.RunAs, "NODEID": j.NodeID,
		"CREATED_BY": j.CreatedBy, "AUTHOR": j.Author,
		"MEMNAME": j.MemName, "MEMLIB": j.MemLib,
	} {
		if strings.TrimSpace(v) != "" {
			warn("%s=%q ignored (no direct equivalent; agents/capabilities cover NODEID/RUN_AS)", k, v)
		}
	}

	// Atributos que o modelo XML não conhece → TODO (nada se perde em silêncio).
	for _, a := range j.Extra {
		todo("attribute %s=%q not mapped", a.Name.Local, a.Value)
	}

	if len(res.Todos) > 0 {
		res.Status = "partial"
	} else {
		res.Status = "ok"
	}
	return res, def
}

/* ── recorrência (DAYS/WEEKDAYS) ─────────────────────────────────── */

var weekdayNames = map[string]string{
	"0": "sun", "1": "mon", "2": "tue", "3": "wed", "4": "thu", "5": "fri", "6": "sat", "7": "sun",
	"SUN": "sun", "MON": "mon", "TUE": "tue", "WED": "wed", "THU": "thu", "FRI": "fri", "SAT": "sat",
}

func mapRecurrence(j ctmJob, def *domain.JobDefinition, todo func(string, ...any)) {
	days := strings.TrimSpace(j.Days)
	weekdays := strings.TrimSpace(j.Weekdays)
	allDays := days == "" || strings.EqualFold(days, "ALL")
	allWeek := weekdays == "" || strings.EqualFold(weekdays, "ALL")

	switch {
	case allDays && allWeek:
		def.Schedule.Frequency = "daily"
	case !allWeek && allDays:
		var dows []string
		for _, tok := range strings.Split(weekdays, ",") {
			tok = strings.ToUpper(strings.TrimSpace(tok))
			if name, ok := weekdayNames[tok]; ok {
				dows = append(dows, name)
			} else {
				todo("WEEKDAYS token %q not mapped", tok)
			}
		}
		if len(dows) > 0 {
			def.Schedule.Frequency = "weekly"
			def.Schedule.DaysOfWeek = dows
		}
	case !allDays && allWeek:
		var doms []int
		for _, tok := range strings.Split(days, ",") {
			tok = strings.ToUpper(strings.TrimSpace(tok))
			if tok == "L" { // last day
				doms = append(doms, -1)
				continue
			}
			if n, err := strconv.Atoi(tok); err == nil && n >= 1 && n <= 31 {
				doms = append(doms, n)
			} else {
				todo("DAYS token %q not mapped (v1: 1..31 and L)", tok)
			}
		}
		if len(doms) > 0 {
			def.Schedule.Frequency = "monthly"
			def.Schedule.DaysOfMonth = doms
		}
	default:
		// DAYS e WEEKDAYS juntos têm semântica AND/OR própria no Control-M.
		def.Schedule.Frequency = "daily"
		todo("DAYS=%q + WEEKDAYS=%q combined (Control-M AND/OR) — review the recurrence", days, weekdays)
	}
}

/* ── helpers ─────────────────────────────────────────────────────── */

var slugRe = regexp.MustCompile(`[^a-z0-9_-]+`)

func slug(s string) string {
	out := slugRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-")
	out = strings.Trim(out, "-")
	if out == "" {
		out = "job"
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func isTruthy(v string) bool {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "1", "Y", "YES", "TRUE":
		return true
	}
	return false
}

// ctmTime — "1430" → "14:30" (aceita também "14:30").
func ctmTime(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if m := regexp.MustCompile(`^(\d{2}):?(\d{2})$`).FindStringSubmatch(v); m != nil {
		hh, _ := strconv.Atoi(m[1])
		mm, _ := strconv.Atoi(m[2])
		if hh <= 23 && mm <= 59 {
			return fmt.Sprintf("%02d:%02d", hh, mm), true
		}
	}
	return "", false
}

// ctmInterval — "00030M" → 30 · "2H" → 120 · "1D" → 1440 · "45" → 45 (minutos).
func ctmInterval(v string) (int, bool) {
	v = strings.ToUpper(strings.TrimSpace(v))
	if v == "" {
		return 0, false
	}
	unit := 1
	switch v[len(v)-1] {
	case 'M':
		v = v[:len(v)-1]
	case 'H':
		unit, v = 60, v[:len(v)-1]
	case 'D':
		unit, v = 1440, v[:len(v)-1]
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n * unit, true
}

func shoutSeverity(urgency string) string {
	switch strings.ToUpper(strings.TrimSpace(urgency)) {
	case "V": // very urgent
		return "critical"
	case "U": // urgent
		return "warning"
	default: // R(egular) / vazio
		return "info"
	}
}

/* ── relatório ───────────────────────────────────────────────────── */

func buildReport(in string, results []jobResult, calendars map[string]string) string {
	var ok, partial, skipped int
	for _, r := range results {
		switch r.Status {
		case "ok":
			ok++
		case "partial":
			partial++
		case "skipped":
			skipped++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Import Control-M → Regente\n\n")
	fmt.Fprintf(&b, "- Source: `%s`\n", in)
	fmt.Fprintf(&b, "- Jobs: **%d ok** · **%d partial** (with `# TODO-import`) · **%d skipped**\n", ok, partial, skipped)
	if len(calendars) > 0 {
		names := make([]string, 0, len(calendars))
		for s := range calendars {
			names = append(names, s)
		}
		sort.Strings(names)
		fmt.Fprintf(&b, "- Generated calendars (stubs — fill in the holidays): %s\n", strings.Join(names, ", "))
	}
	b.WriteString("\n## Per job\n\n| Folder | Job | id | Status | Pending |\n|---|---|---|---|---|\n")
	for _, r := range results {
		detail := ""
		switch {
		case r.SkipReason != "":
			detail = r.SkipReason
		case len(r.Todos) > 0:
			detail = strings.Join(r.Todos, " · ")
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n", r.Folder, r.JobName, r.ID, r.Status, detail)
	}
	var warns []string
	for _, r := range results {
		for _, w := range r.Warnings {
			warns = append(warns, fmt.Sprintf("%s/%s: %s", r.Folder, r.JobName, w))
		}
	}
	if len(warns) > 0 {
		b.WriteString("\n## Warnings (deliberately ignored)\n\n")
		for _, w := range warns {
			fmt.Fprintf(&b, "- %s\n", w)
		}
	}
	b.WriteString("\n> Review the `# TODO-import` entries in the YAMLs before committing. The importer never pushes.\n")
	return b.String()
}
