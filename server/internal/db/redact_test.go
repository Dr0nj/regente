package db

import (
	"strings"
	"testing"
)

// TestRedactDSN — a flag `-db` é a mesma variável pro path SQLite e pra
// connection string do Postgres, então a régua tem dois lados: senha SEMPRE
// some, path de arquivo NUNCA é tocado (mascarar o path só cegaria o log).
func TestRedactDSN(t *testing.T) {
	cases := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "postgres com senha",
			dsn:  "postgres://regente:s3nh4@pg:5432/regente?sslmode=disable",
			want: "postgres://regente:***@pg:5432/regente?sslmode=disable",
		},
		{
			name: "postgres sem senha",
			dsn:  "postgres://regente@pg:5432/regente",
			want: "postgres://regente@pg:5432/regente",
		},
		{
			name: "postgres sem userinfo",
			dsn:  "postgres://pg:5432/regente",
			want: "postgres://pg:5432/regente",
		},
		{
			name: "senha percent-encoded",
			dsn:  "postgresql://regente:p%40ss%3Aw%2Frd@pg:5432/regente",
			want: "postgresql://regente:***@pg:5432/regente",
		},
		{
			// `%pa` não é escape válido -> url.Parse falha. É o caso em que
			// devolver a string crua vazaria a senha.
			name: "DSN inválida (escape quebrado na senha)",
			dsn:  "postgres://regente:100%pass@pg:5432/regente",
			want: "postgres://regente:***@pg:5432/regente",
		},
		{
			name: "keyword/value",
			dsn:  "host=pg port=5432 user=regente password=s3nh4 dbname=regente",
			want: "host=pg port=5432 user=regente password=*** dbname=regente",
		},
		{
			name: "keyword/value com aspas",
			dsn:  "host=pg user=regente password='s3 nh4' dbname=regente",
			want: "host=pg user=regente password=*** dbname=regente",
		},
		{
			name: "sqlite relativo",
			dsn:  "./regente.db",
			want: "./regente.db",
		},
		{
			name: "sqlite absoluto",
			dsn:  "/var/lib/regente/regente.db",
			want: "/var/lib/regente/regente.db",
		},
		{
			name: "sqlite absoluto windows",
			dsn:  `C:\ProgramData\regente\regente.db`,
			want: `C:\ProgramData\regente\regente.db`,
		},
		{
			name: "sqlite com pragmas",
			dsn:  "./regente.db?_pragma=busy_timeout(5000)",
			want: "./regente.db?_pragma=busy_timeout(5000)",
		},
		{
			name: "vazio",
			dsn:  "",
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RedactDSN(c.dsn); got != c.want {
				t.Fatalf("RedactDSN(%q) = %q, queria %q", c.dsn, got, c.want)
			}
		})
	}
}

// TestRedactDSNNuncaVazaSenha — a invariante que importa de verdade: qualquer
// que seja a forma da DSN, o segredo não pode sobrar na saída. Vale tanto pro
// valor cru quanto pra versão percent-encoded dele.
func TestRedactDSNNuncaVazaSenha(t *testing.T) {
	const secret = "3f61237988c24d26d7625afea7b477a1"
	dsns := []string{
		"postgres://regente:" + secret + "@pg:5432/regente?sslmode=disable",
		"postgresql://regente:" + secret + "@pg/regente",
		"postgres://regente:" + secret + "%@pg:5432/regente", // parse falha
		"postgres://regente:" + secret + "@pg:5432/regente?options=-c%20search_path%3Dx",
		"host=pg user=regente password=" + secret,
		"host=pg user=regente password='" + secret + "' dbname=regente",
	}
	for _, dsn := range dsns {
		got := RedactDSN(dsn)
		if strings.Contains(got, secret) {
			t.Fatalf("senha vazou: RedactDSN(%q) = %q", dsn, got)
		}
		if !strings.Contains(got, "pg") {
			t.Fatalf("mascarou demais, perdeu o host: RedactDSN(%q) = %q", dsn, got)
		}
	}
}
