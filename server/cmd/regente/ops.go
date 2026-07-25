// regente ops — ADV-6: operação de um server VIVO pela linha de comando,
// construído 100% sobre o SDK (pkg/client) — o CLI não fala HTTP direto.
// Fecha o ciclo DevEx: definir (DSL) → test → dev → promote → OPERAR.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/Dr0nj/regente-server/pkg/client"
)

const opsUsage = `regente ops — operate a live Regente server (via the pkg/client SDK)

Usage:
  regente ops instances [-date D] [-status S,S] [-folder F,F] [-search X] [-late] [-group status|folder|definition] [-limit N] [-json]
  regente ops action <hold|release|cancel|rerun|set-ok|confirm> <instanceId>
  regente ops force <definitionId>
  regente ops ingest -source SRC -id ID [-condition C[,C]] [-force-job DEF] [-date D]
  regente ops daily [-date D] [-report] [-json]
  regente ops archives [list]
  regente ops archives get <file> [-o out.ndjson]
  regente ops jobtypes [-json]

Connection (all commands):
  -server URL   (default env REGENTE_SERVER, otherwise http://localhost:8080)
  -token  TOK   (default env REGENTE_TOKEN)
`

// opsConn adiciona as flags de conexão comuns e resolve o client.
func opsConn(fs *flag.FlagSet) func() *client.Client {
	server := fs.String("server", "", "server URL (env REGENTE_SERVER)")
	token := fs.String("token", "", "bearer token (env REGENTE_TOKEN)")
	return func() *client.Client {
		base := *server
		if base == "" {
			base = os.Getenv("REGENTE_SERVER")
		}
		if base == "" {
			base = "http://localhost:8080"
		}
		tok := *token
		if tok == "" {
			tok = os.Getenv("REGENTE_TOKEN")
		}
		return client.New(base, tok)
	}
}

func cmdOps(args []string) error {
	if len(args) == 0 {
		fmt.Print(opsUsage)
		return fmt.Errorf("subcommand required")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "instances":
		return opsInstances(rest)
	case "action":
		return opsAction(rest)
	case "force":
		return opsForce(rest)
	case "ingest":
		return opsIngest(rest)
	case "daily":
		return opsDaily(rest)
	case "archives":
		return opsArchives(rest)
	case "jobtypes":
		return opsJobTypes(rest)
	case "-h", "--help", "help":
		fmt.Print(opsUsage)
		return nil
	default:
		fmt.Print(opsUsage)
		return fmt.Errorf("subcomando desconhecido %q", sub)
	}
}

func csv(s string) []string {
	if s = strings.TrimSpace(s); s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func opsInstances(args []string) error {
	fs := flag.NewFlagSet("ops instances", flag.ContinueOnError)
	conn := opsConn(fs)
	var (
		date   = fs.String("date", "", "day (YYYY-MM-DD; default today)")
		from   = fs.String("from", "", "range start (YYYY-MM-DD)")
		to     = fs.String("to", "", "range end (YYYY-MM-DD)")
		status = fs.String("status", "", "IN filter by status (CSV: NOTOK,WAITING)")
		folder = fs.String("folder", "", "IN filter by folder (CSV)")
		search = fs.String("search", "", "LIKE on id/definition_id")
		late   = fs.Bool("late", false, "only late WAITING (scheduled_at in the past)")
		group  = fs.String("group", "", "aggregate: status|folder|definition")
		limit  = fs.Int("limit", 0, "row cap (default 500, max 5000)")
		asJSON = fs.Bool("json", false, "JSON output (CI-friendly)")
	)
	if err := fs.Parse(reorderArgs(args, "late", "json")); err != nil {
		return err
	}
	var latePtr *bool
	if *late {
		latePtr = late
	}
	res, err := conn().QueryInstances(client.Query{
		Date: *date, From: *from, To: *to,
		Statuses: csv(*status), Folders: csv(*folder),
		Search: *search, Late: latePtr, GroupBy: *group, Limit: *limit,
	})
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(res)
	}
	if *group != "" {
		w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintf(w, "%s\tCOUNT\n", strings.ToUpper(*group))
		for _, g := range res.Groups {
			fmt.Fprintf(w, "%s\t%d\n", g.Key, g.Count)
		}
		fmt.Fprintf(w, "TOTAL\t%d\n", res.Total)
		return w.Flush()
	}
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tJOB\tFOLDER\tSTATUS\tORDER DATE\tSCHEDULED")
	for _, it := range res.Items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			it.ID, it.DefinitionID, it.Team, it.Status, it.OrderDate, it.ScheduledAt.Format("15:04:05"))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Printf("%d instance(s)", len(res.Items))
	if res.NextCursor != "" {
		fmt.Printf(" (more pages: larger -limit or cursor %s)", res.NextCursor)
	}
	fmt.Println()
	return nil
}

func opsAction(args []string) error {
	fs := flag.NewFlagSet("ops action", flag.ContinueOnError)
	conn := opsConn(fs)
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 2 {
		return fmt.Errorf("uso: regente ops action <hold|release|cancel|rerun|set-ok|confirm> <instanceId>")
	}
	if err := conn().Action(rest[1], rest[0]); err != nil {
		return err
	}
	fmt.Printf("%s: %s OK\n", rest[1], rest[0])
	return nil
}

func opsForce(args []string) error {
	fs := flag.NewFlagSet("ops force", flag.ContinueOnError)
	conn := opsConn(fs)
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("uso: regente ops force <definitionId>")
	}
	if err := conn().ForceOrder(rest[0]); err != nil {
		return err
	}
	fmt.Printf("force order de %s disparado\n", rest[0])
	return nil
}

func opsIngest(args []string) error {
	fs := flag.NewFlagSet("ops ingest", flag.ContinueOnError)
	conn := opsConn(fs)
	var (
		source   = fs.String("source", "", "emitting system (required)")
		id       = fs.String("id", "", "event id IN the emitter (required — it is the idempotency key)")
		cond     = fs.String("condition", "", "condition(s) to set (CSV)")
		forceJob = fs.String("force-job", "", "definition to force when the event arrives")
		date     = fs.String("date", "", "scope date (default today)")
	)
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}
	if *source == "" || *id == "" {
		return fmt.Errorf("-source and -id are required")
	}
	res, err := conn().Ingest(client.IngestEvent{
		ID: *id, Source: *source, Conditions: csv(*cond), ForceJob: *forceJob, Date: *date,
	})
	if err != nil {
		return err
	}
	fmt.Printf("event %s/%s: %s\n", res.Source, res.ID, res.Applied)
	return nil
}

func opsDaily(args []string) error {
	fs := flag.NewFlagSet("ops daily", flag.ContinueOnError)
	conn := opsConn(fs)
	var (
		date   = fs.String("date", "", "report day (YYYY-MM-DD)")
		report = fs.Bool("report", false, "E5 report instead of the status")
		asJSON = fs.Bool("json", false, "JSON output")
	)
	if err := fs.Parse(reorderArgs(args, "report", "json")); err != nil {
		return err
	}
	c := conn()
	if *report {
		raw, err := c.DailyReport(*date)
		if err != nil {
			return err
		}
		return printRaw(raw)
	}
	st, err := c.DailyStatus()
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(st)
	}
	fmt.Printf("business daily:     %s\n", st.OrderDate)
	fmt.Printf("configured daily:   %s (%s)\n", st.DailyAt, orDefault(st.Timezone, "server local clock"))
	fmt.Printf("last daily:         %s at %s\n", st.LastRunDate, st.LastRunAt)
	fmt.Printf("now on the server:  %s\n", st.ServerNow)
	return nil
}

func opsArchives(args []string) error {
	fs := flag.NewFlagSet("ops archives", flag.ContinueOnError)
	conn := opsConn(fs)
	out := fs.String("o", "", "output file (default stdout)")
	if err := fs.Parse(reorderArgs(args)); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) >= 1 && rest[0] == "get" {
		if len(rest) != 2 {
			return fmt.Errorf("usage: regente ops archives get <file> [-o out]")
		}
		var w *os.File = os.Stdout
		if *out != "" {
			f, err := os.Create(*out)
			if err != nil {
				return err
			}
			defer f.Close()
			w = f
		}
		if err := conn().DownloadArchive(rest[1], w); err != nil {
			return err
		}
		if *out != "" {
			fmt.Printf("archive saved to %s\n", *out)
		}
		return nil
	}
	list, err := conn().Archives()
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("no archive (retention disabled or nothing expired yet)")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "DAY\tFILE\tBYTES\tMODIFIED")
	for _, a := range list {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", a.Day, a.File, a.SizeBytes, a.ModifiedAt)
	}
	return w.Flush()
}

func opsJobTypes(args []string) error {
	fs := flag.NewFlagSet("ops jobtypes", flag.ContinueOnError)
	conn := opsConn(fs)
	asJSON := fs.Bool("json", false, "full JSON output (schema per type)")
	if err := fs.Parse(reorderArgs(args, "json")); err != nil {
		return err
	}
	raw, err := conn().JobTypes()
	if err != nil {
		return err
	}
	if *asJSON {
		return printRaw(raw)
	}
	// Resumo: tipo + summary (o -json tem o schema completo).
	var types []struct {
		Type    string   `json:"type"`
		Aliases []string `json:"aliases"`
		Summary string   `json:"summary"`
	}
	if err := json.Unmarshal(raw, &types); err != nil {
		return printRaw(raw) // shape diferente do esperado: mostra cru
	}
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "TYPE\tALIASES\tSUMMARY")
	for _, t := range types {
		fmt.Fprintf(w, "%s\t%s\t%s\n", t.Type, strings.Join(t.Aliases, ","), t.Summary)
	}
	return w.Flush()
}

func printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func printRaw(raw json.RawMessage) error {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		fmt.Println(string(raw))
		return nil
	}
	return printJSON(v)
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
