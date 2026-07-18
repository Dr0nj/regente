// regente dev — D-8 Local development mode: um Regente inteiro, descartável,
// numa porta local. `regente dev daily` sobe o servidor com:
//
//   - SQLite num diretório temp (estado morre com o processo — é DEV)
//   - demo-mode: sem agente, jobs mock-finalizam OK (datas/agents mockados)
//   - workspace LOCAL (default ./workspace) — sem Git, sem push, sem rede
//   - a daily de -date materializada JÁ NO BOOT + ticker interno
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
	workspace := fs.String("workspace", "./workspace", "workspace local (contém definitions/)")
	date := fs.String("date", time.Now().Format("2006-01-02"), "order_date da daily materializada no boot")
	addr := fs.String("addr", ":8686", "endereço HTTP")
	token := fs.String("token", "dev-token", "bearer token da API")
	spaDir := fs.String("spa-dir", "", "servir o SPA buildado deste diretório (opcional)")
	_ = fs.Parse(args)

	if _, err := os.Stat(filepath.Join(*workspace, "definitions")); err != nil {
		return fmt.Errorf("workspace %s sem definitions/ — aponte -workspace pro seu clone do regente-workspace", *workspace)
	}

	tmp, err := os.MkdirTemp("", "regente-dev-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
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
	sched.AttachResources(scheduler.NewResourceTracker())
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
	sched.MigrateConditionsUnify()      // unificação deps→condições: backfill one-time
	sched.MigrateMonitoringSnapshot()   // M1: colunas congeladas do Monitoring (schemaV18)
	sched.MigrateResourcesSnapshot()    // F15: recursos congelados (schemaV19)
	created := sched.RunDaily(*date)
	go sched.Run(ctx)

	router := api.NewRouter(api.Config{
		Store: store, DB: database, Hub: h, Scheduler: sched,
		Token: *token, SPADir: *spaDir,
	})
	srv := &http.Server{Addr: *addr, Handler: router, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.ListenAndServe() }()

	fmt.Printf(`regente dev — ambiente LOCAL descartável
  workspace : %s
  daily     : %s (%d jobs materializados, demo-mode: mock-finish OK)
  api       : http://localhost%s  (Authorization: Bearer %s)
  estado    : %s (apagado ao sair)

  experimente:
    curl -s -H "Authorization: Bearer %s" http://localhost%s/api/instances | jq length
    UI dev: VITE_REGENTE_SERVER_URL=http://localhost%s npm run dev (na pasta app/)

Ctrl-C encerra.
`, *workspace, *date, created, *addr, *token, tmp, *token, *addr, *addr)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	shCtx, shCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shCancel()
	_ = srv.Shutdown(shCtx)
	return nil
}
