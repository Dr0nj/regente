package db

import (
	"net/url"
	"regexp"
	"strings"
)

// redacted — o que entra no lugar da senha. Curto e óbvio no log.
const redacted = "***"

// kvPassword — forma keyword/value do Postgres (`host=pg password=x dbname=y`).
// A alternativa com aspas vem primeiro porque `password='a b'` tem espaço no
// meio e o ramo `\S*` pararia nele, deixando metade da senha pra trás.
var kvPassword = regexp.MustCompile(`password=(?:'[^']*'|\S*)`)

// RedactDSN devolve a DSN sem a senha, preservando tudo que serve pra
// diagnóstico (esquema, usuário, host, porta, banco, query params):
//
//	postgres://regente:s3nh4@pg:5432/regente?sslmode=disable
//	postgres://regente:***@pg:5432/regente?sslmode=disable
//
// A flag `-db` guarda ou o path do arquivo SQLite ou a connection string do
// Postgres — a MESMA variável para os dois. Path de arquivo não é segredo e
// sai intacto; connection string sai mascarada. Qualquer coisa que imprima
// essa variável (log de boot, mensagem de erro) tem que passar por aqui: a
// senha sair pro log a tira do domínio dos segredos e a joga num domínio de
// acesso muito mais frouxo (`kubectl logs`, `journalctl`, agregador de log),
// com retenção longa e replicação — guardar a senha num Secret não adianta se
// o processo a imprime no boot.
func RedactDSN(dsn string) string {
	if i := strings.Index(dsn, "://"); i >= 0 {
		return redactURLDSN(dsn, i+len("://"))
	}
	if strings.Contains(dsn, "password=") {
		return kvPassword.ReplaceAllLiteralString(dsn, "password="+redacted)
	}
	// Sem esquema e sem `password=`: é o path do arquivo SQLite. Não é segredo,
	// e mascarar aqui só esconderia o diagnóstico mais útil que o log tem.
	return dsn
}

// redactURLDSN mascara a senha na forma URL. `auth` é o índice logo depois do
// "://", ou seja, o começo do userinfo.
func redactURLDSN(dsn string, auth int) string {
	u, err := url.Parse(dsn)
	if err != nil {
		// Parse falhou. NÃO dá pra devolver a string crua justamente aqui: o
		// que mais quebra o parse de uma DSN é senha com caractere especial
		// não escapado — ou seja, o caso em que ela TEM senha. Sem parse não
		// dá pra delimitar a autoridade (a senha pode conter '/' e '@'), então
		// pega o ÚLTIMO '@' da string inteira: pode mascarar demais se houver
		// '@' depois da autoridade, e num caminho de erro esse é o lado certo
		// pra errar.
		return spliceUserinfo(dsn, auth, strings.LastIndex(dsn, "@"))
	}
	if u.User == nil {
		return dsn
	}
	if _, hasPW := u.User.Password(); !hasPW {
		return dsn
	}
	// Recorte textual em vez de `u.String()`: o Go re-encoda o userinfo (o
	// "***" sairia como "%2A%2A%2A") e normaliza o resto da URL. O log tem que
	// sair igual ao que o operador configurou, menos a senha.
	end := len(dsn)
	if i := strings.IndexAny(dsn[auth:], "/?#"); i >= 0 {
		end = auth + i
	}
	return spliceUserinfo(dsn, auth, auth+strings.LastIndex(dsn[auth:end], "@"))
}

// spliceUserinfo troca o trecho entre o ':' do userinfo e o '@' em `at` pelo
// marcador. `at` < `auth` significa que não há userinfo.
func spliceUserinfo(dsn string, auth, at int) string {
	if at < auth {
		return dsn
	}
	colon := strings.Index(dsn[auth:at], ":")
	if colon < 0 {
		return dsn // usuário sem senha
	}
	return dsn[:auth+colon+1] + redacted + dsn[at:]
}
