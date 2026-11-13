package db

// schemaV34 — índices do caminho quente do scheduler (diagnóstico de produção,
// 2026-11-13). `execution_attempts` não tinha índice por estado: três consultas
// que rodam A CADA TICK (reconciliação de reservas, expiração de lease e
// avaliação de RUNNING) varriam o histórico inteiro de execuções para achar um
// punhado de linhas vivas. O custo crescia com os dias acumulados, não com a
// carga do momento — por isso o laboratório (banco novo) não via e a VPS parou.
//
// Os dois índices são PARCIAIS: só contêm o que está vivo, então ficam do
// tamanho do trabalho em voo e não do histórico.
//
// ATENÇÃO ao escrever consultas contra eles: o SQLite só usa um índice parcial
// quando o WHERE da consulta contém o MESMO predicado do índice (ele não deduz
// que `state='running'` implica `state NOT IN (...)`). Use as constantes
// ActiveAttemptPredicate / RetryWaitPredicate em vez de reescrever o texto.
func schemaV34() string {
	return `CREATE INDEX execution_attempts_active ON execution_attempts(state,lease_until) WHERE state NOT IN ('succeeded','failed','cancelled');
CREATE INDEX instances_retry_wait ON instances(id) WHERE status='WAITING' AND COALESCE(attempts,1)>1;`
}

// ActiveAttemptPredicate devolve o predicado do índice execution_attempts_active
// para o alias informado. O texto precisa bater com o do índice (ver schemaV34).
func ActiveAttemptPredicate(alias string) string {
	return alias + ".state NOT IN ('succeeded','failed','cancelled')"
}

// RetryWaitPredicate devolve o predicado do índice instances_retry_wait.
func RetryWaitPredicate(alias string) string {
	return alias + ".status='WAITING' AND COALESCE(" + alias + ".attempts,1)>1"
}
