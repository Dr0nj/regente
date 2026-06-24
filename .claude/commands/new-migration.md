---
description: Scaffold de uma nova migration de schema (schemaVN) no server.
argument-hint: <descrição curta da mudança>
---
Crie a próxima migration de schema em `server/internal/db/db.go`, seguindo o padrão das anteriores:

1. Ache o maior `version` nas slices `sqliteMigrations` e `pgMigrations`; use **N = maior + 1**.
2. Escreva `func schemaVN(...)`:
   - Se houver coluna de timestamp, assine `schemaVN(ts string)` e chame com `"DATETIME"`
     (sqlite) e `"TIMESTAMPTZ"` (pg) — como a `schemaV5`. Sem timestamp, sem parâmetro.
   - DDL idêntico nos dois dialetos quando possível; **um statement por `;`** (o
     `splitStatements` quebra por `;`, e o pgx não aceita múltiplos statements num Exec).
   - Comente o PORQUÊ da migration, no estilo das anteriores.
3. Registre `{version: N, sql: schemaVN(...)}` em **AMBAS** as slices: `sqliteMigrations`
   **E** `pgMigrations`. (Esquecer a de Postgres é o erro clássico — não esqueça.)
4. Rode `go test ./server/internal/db/...` (a migration roda em SQLite real nos testes) e garanta verde.

Mudança pedida: $ARGUMENTS
