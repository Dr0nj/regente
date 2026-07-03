// DATABASE — jobType de banco de dados (Control-M Database job).
//
// Roda SQL direto do agente contra Postgres, MySQL ou SQLite via database/sql,
// com drivers pure-Go compilados no binário (sem CGO, sem client instalado no
// host — o "JDBC" do mundo Go). O corpo do job é o SQL; o resultado (linhas ou
// rows affected) vira o output da instance.
//
// Params:
//
//	driver   "postgres" | "mysql" | "sqlite"            (obrigatório)
//	dsn      connection string do driver                (obrigatório)
//	         postgres://user:pw@host:5432/db?sslmode=disable
//	         user:pw@tcp(host:3306)/db
//	         C:\dados\meu.db (arquivo, sqlite)
//	sql      statement a executar                       (obrigatório)
//	query    true = SELECT (renderiza linhas); ausente = auto-detecta
//	         pelo prefixo (SELECT/WITH/SHOW/PRAGMA/EXPLAIN → query)
//	maxRows  teto de linhas renderizadas em query (default 100)
//
// Exit code: 0 = sucesso; -1 = erro de conexão/execução (mensagem no output).
package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql" // driver "mysql" (pure-Go)
	_ "github.com/jackc/pgx/v5/stdlib" // driver "pgx" (Postgres, pure-Go)
	_ "modernc.org/sqlite"             // driver "sqlite" (pure-Go, sem CGO)
)

// dbDriverName normaliza o param `driver` pro nome registrado no database/sql.
func dbDriverName(driver string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "postgres", "postgresql", "pg", "pgx":
		return "pgx", true
	case "mysql", "mariadb":
		return "mysql", true
	case "sqlite", "sqlite3":
		return "sqlite", true
	}
	return "", false
}

// sqlLooksLikeQuery — auto-detecção: statements que devolvem linhas.
func sqlLooksLikeQuery(stmt string) bool {
	head := strings.ToUpper(strings.TrimSpace(stmt))
	for _, p := range []string{"SELECT", "WITH", "SHOW", "PRAGMA", "EXPLAIN", "VALUES"} {
		if strings.HasPrefix(head, p) {
			return true
		}
	}
	return false
}

func runDatabase(params map[string]interface{}, timeoutSec int, emit func(string)) (int, string) {
	driverParam, _ := params["driver"].(string)
	dsn, _ := params["dsn"].(string)
	stmt, _ := params["sql"].(string)
	if driverParam == "" || dsn == "" || stmt == "" {
		return -1, "missing 'driver', 'dsn' or 'sql' param"
	}
	driver, ok := dbDriverName(driverParam)
	if !ok {
		return -1, fmt.Sprintf("unsupported driver %q (use postgres|mysql|sqlite)", driverParam)
	}
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return -1, "open: " + err.Error()
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return -1, "connect: " + err.Error()
	}

	isQuery := sqlLooksLikeQuery(stmt)
	if q, ok := params["query"].(bool); ok {
		isQuery = q
	}

	var buf strings.Builder
	out := func(line string) {
		buf.WriteString(line)
		if emit != nil {
			emit(line)
		}
	}

	if !isQuery {
		res, err := db.ExecContext(ctx, stmt)
		if err != nil {
			return -1, "exec: " + err.Error()
		}
		n, _ := res.RowsAffected()
		out(fmt.Sprintf("OK — %d row(s) affected\n", n))
		return 0, buf.String()
	}

	maxRows := 100
	if v, ok := toInt(params["maxRows"]); ok && v > 0 {
		maxRows = v
	}
	rows, err := db.QueryContext(ctx, stmt)
	if err != nil {
		return -1, "query: " + err.Error()
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return -1, "columns: " + err.Error()
	}
	out(strings.Join(cols, "\t") + "\n")

	vals := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	count, truncated := 0, false
	for rows.Next() {
		if count >= maxRows {
			truncated = true
			break
		}
		if err := rows.Scan(ptrs...); err != nil {
			return -1, "scan: " + err.Error()
		}
		cells := make([]string, len(cols))
		for i, v := range vals {
			cells[i] = renderDBCell(v)
		}
		out(strings.Join(cells, "\t") + "\n")
		count++
	}
	if err := rows.Err(); err != nil {
		return -1, "rows: " + err.Error()
	}
	if truncated {
		out(fmt.Sprintf("… truncado em %d linhas (maxRows)\n", maxRows))
	}
	out(fmt.Sprintf("(%d row(s))\n", count))
	return 0, buf.String()
}

// renderDBCell — valor de célula legível no output (NULL explícito, []byte→string).
func renderDBCell(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return string(x)
	case time.Time:
		return x.Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", x)
	}
}
