// regente dev — D-8 Local development mode: um Regente inteiro, descartável,
// numa porta local. `regente dev daily` sobe o servidor com:
//
//   - SQLite num diretório temp (estado morre com o processo — é DEV)
//   - demo-mode: sem agente, jobs mock-finalizam OK (datas/agents mockados)
//   - workspace LOCAL (default ./workspace) — sem Git, sem push, sem rede;
//     SEM workspace nenhum, escreve um de DEMONSTRAÇÃO no temp (ver
//     demoWorkspace) para o comando funcionar num clone recém-baixado
//   - a daily de -date materializada JÁ NO BOOT + ticker interno
//   - a UI junto, na MESMA porta, quando acha um SPA buildado (ver findSPA)
//
// O loop de desenvolvimento vira: editar YAML (ou DSL) → `regente test` →
// `regente dev daily` → ver rodar em http://localhost:8686 — sem tocar em
// produção e sem instalar nada além do binário.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Dr0nj/regente-server/internal/api"
	"github.com/Dr0nj/regente-server/internal/auth"
	"github.com/Dr0nj/regente-server/internal/db"
	"github.com/Dr0nj/regente-server/internal/hub"
	"github.com/Dr0nj/regente-server/internal/leader"
	"github.com/Dr0nj/regente-server/internal/scheduler"
	"github.com/Dr0nj/regente-server/internal/storage"
)

func cmdDev(args []string) error {
	// aceita o alias `regente dev daily` (a forma canônica do roadmap); pode vir
	// em qualquer posição junto das flags.
	args = reorderArgs(args) // dev não tem bool flags — todas consomem valor
	if len(args) > 0 && args[0] == "daily" {
		args = args[1:]
	}
	fs := flag.NewFlagSet("dev", flag.ExitOnError)
	workspace := fs.String("workspace", "./workspace", "local workspace (contains definitions/)")
	date := fs.String("date", time.Now().Format("2006-01-02"), "order_date of the daily materialized at boot")
	addr := fs.String("addr", ":8686", "HTTP address")
	token := fs.String("token", "dev-token", "API bearer token")
	spaDir := fs.String("spa-dir", "", "serve the built SPA from this directory (default: auto-detected app/dist)")
	_ = fs.Parse(args)

	// -workspace explícito é promessa do usuário: se não tem definitions/, é erro
	// dele (typo no caminho) e a gente avisa. SEM -workspace, o comando tem de
	// funcionar num clone recém-baixado — o repo não versiona workspace (o de
	// verdade é do operador, um por instalação), então escrevemos um de DEMO.
	explicitWorkspace := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "workspace" {
			explicitWorkspace = true
		}
	})

	tmp, err := os.MkdirTemp("", "regente-dev-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	demo := false
	if _, err := os.Stat(filepath.Join(*workspace, "definitions")); err != nil {
		if explicitWorkspace {
			return fmt.Errorf("workspace %s has no definitions/ — point -workspace at a workspace directory (the folder that holds definitions/)", *workspace)
		}
		demoWS := filepath.Join(tmp, "workspace")
		if err := writeDemoWorkspace(demoWS); err != nil {
			return err
		}
		*workspace, demo = demoWS, true
	}
	if *spaDir == "" {
		*spaDir = findSPA()
	}
	database, err := db.Open(db.SQLite, filepath.Join(tmp, "dev.db"))
	if err != nil {
		return err
	}
	defer database.Close()
	if err := db.Migrate(database); err != nil {
		return err
	}
	if err := auth.Bootstrap(database); err != nil {
		return err
	}

	h := hub.New()
	store := storage.NewFileStore(*workspace, false)
	sched := scheduler.New(store, database, h, 2*time.Second)
	sched.DemoMode = true // sem agente: mock-finish OK — é o ponto do dev mode
	sched.AttachCalendars(storage.NewCalendarStore(*workspace))
	resTracker := scheduler.NewResourceTracker()
	_ = resTracker.LoadFromDB(database) // registry de capacidade durável (tabela `resources`)
	sched.AttachResources(resTracker)
	sched.AttachConditions(scheduler.NewConditionEngine(database))
	sched.AttachSLA(scheduler.NewSLAEngine(database, h))
	sched.AttachLeader(leader.SingleNode{})
	if varStore, err := storage.NewVariableStore(database); err == nil {
		sched.AttachVariables(varStore)
	}
	defer sched.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched.ReloadDefs()
	sched.MigrateConditionsUnify()    // unificação deps→condições: backfill one-time
	sched.MigrateMonitoringSnapshot() // M1: colunas congeladas do Monitoring (schemaV18)
	sched.MigrateResourcesSnapshot()  // F15: recursos congelados (schemaV19)
	sched.MigrateCondLogicSnapshot()  // CL: lógica AND/OR congelada (schemaV21) — linhas OR
	created := sched.RunDaily(*date)
	go sched.Run(ctx)

	router := api.NewRouter(api.Config{
		Store: store, DB: database, Hub: h, Scheduler: sched,
		Token: *token, SPADir: *spaDir,
	})
	srv := &http.Server{Addr: *addr, Handler: router, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.ListenAndServe() }()

	kind := "workspace "
	if demo {
		kind = "workspace : DEMO (generated, throw-away)\n  source    "
	}
	ui := `no UI built — the API answers, the UI does not. To get it:
                (cd app && npm ci && VITE_REGENTE_SERVER_URL=@origin npm run build)
              then run this command again; it picks the UI up on the same port.`
	if *spaDir != "" {
		ui = fmt.Sprintf("http://localhost%s   (login admin / admin)", *addr)
	}
	fmt.Printf(`regente dev — disposable LOCAL environment
  %s: %s
  daily     : %s (%d jobs materialized, demo-mode: mock-finish OK)
  ui        : %s
  api       : http://localhost%s  (Authorization: Bearer %s)
  api docs  : http://localhost%s/api-docs  (interactive, try it with the token above)
  state     : %s (deleted on exit)

  try:
    curl -s -H "Authorization: Bearer %s" http://localhost%s/api/instances

Ctrl-C stops.
`, kind, *workspace, *date, created, ui, *addr, *token, *addr, tmp, *token, *addr)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	shCtx, shCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shCancel()
	_ = srv.Shutdown(shCtx)
	return nil
}

// findSPA — procura uma UI buildada perto do cwd para servir single-origin (o
// mesmo truque do install-linux.sh: o comando roda tanto de server/ quanto da
// raiz do monorepo). Vazio = nenhuma; o dev mode sobe só a API.
func findSPA() string {
	for _, c := range []string{"app/dist", "../app/dist", "../../app/dist"} {
		if _, err := os.Stat(filepath.Join(c, "index.html")); err == nil {
			return c
		}
	}
	return ""
}

// demoFiles — workspace mínimo de DEMONSTRAÇÃO: uma folder com uma cadeia de 3
// jobs ligados por condições (extract → transform → publish) e um HTTP solto.
// É o que faz `regente dev daily` mostrar algo num clone recém-baixado, sem
// pedir repositório nenhum. Descartável: vive no temp e morre com o processo.
var demoFiles = map[string]string{
	"definitions/demo/.regente-folder.yaml": "# regente folder marker\nname: demo\n",
	"definitions/demo/extract-sales.yaml": `id: extract-sales
label: Extract sales
team: demo
jobType: COMMAND
schedule:
    enabled: true
    frequency: daily
params:
    command: echo "extracting sales"
conditionsOutAdd:
    - EXTRACT-SALES-TO-TRANSFORM-SALES
`,
	"definitions/demo/transform-sales.yaml": `id: transform-sales
label: Transform sales
team: demo
jobType: COMMAND
schedule:
    enabled: true
    frequency: daily
params:
    command: echo "transforming sales"
conditionsIn:
    - EXTRACT-SALES-TO-TRANSFORM-SALES
conditionsOutAdd:
    - TRANSFORM-SALES-TO-PUBLISH-REPORT
conditionsOutRemove:
    - EXTRACT-SALES-TO-TRANSFORM-SALES
`,
	"definitions/demo/publish-report.yaml": `id: publish-report
label: Publish report
team: demo
jobType: COMMAND
schedule:
    enabled: true
    frequency: daily
params:
    command: echo "publishing report"
conditionsIn:
    - TRANSFORM-SALES-TO-PUBLISH-REPORT
conditionsOutRemove:
    - TRANSFORM-SALES-TO-PUBLISH-REPORT
`,
	"definitions/demo/healthcheck.yaml": `id: healthcheck
label: Healthcheck
team: demo
jobType: HTTP
schedule:
    enabled: true
    frequency: daily
params:
    method: GET
    url: https://example.com/health
    expectStatus: 200
`,
}

// writeDemoWorkspace materializa demoFiles em dir.
func writeDemoWorkspace(dir string) error {
	for rel, content := range demoFiles {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}
