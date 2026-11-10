package db

import (
	"strings"
	"testing"
)

func TestSplitStatementsBodies(t *testing.T) {
	script := `CREATE TABLE t(x TEXT);INSERT INTO t VALUES('a;''b');
CREATE TRIGGER t_insert AFTER INSERT ON t BEGIN UPDATE t SET x='semi;colon'; INSERT INTO t VALUES('second'); END;
CREATE FUNCTION f() RETURNS void LANGUAGE plpgsql AS $body$ BEGIN PERFORM 1; RETURN; END $body$;
-- comment ;
SELECT 'last';`
	got := splitStatements(script)
	if len(got) != 5 || !strings.Contains(got[2], "END") || !strings.Contains(got[3], "$body$") {
		t.Fatalf("DDL dividido incorretamente: %#v", got)
	}
}
