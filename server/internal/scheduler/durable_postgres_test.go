package scheduler

import (
	"fmt"
	"github.com/Dr0nj/regente-server/internal/db"
	"net/url"
	"os"
	"testing"
	"time"
)

func durablePGDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("REGENTE_TEST_PG_DSN")
	if dsn == "" {
		if os.Getenv("REGENTE_REQUIRE_INTEGRATION") == "1" {
			t.Fatal("real PostgreSQL is required")
		}
		t.Skip("real PostgreSQL not configured")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("PostgreSQL URL required")
	}
	admin, err := db.Open(db.Postgres, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("regente_i10_%d", time.Now().UnixNano())
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer admin.Close()
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Error(err)
		}
	})
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}
func durableFault(t *testing.T, d *db.DB, table, name string) func() {
	t.Helper()
	query := "CREATE TRIGGER " + name + " BEFORE INSERT ON " + table + " BEGIN SELECT RAISE(ABORT,'injected storage failure'); END"
	if d.Dialect() == db.Postgres {
		if _, err := d.Exec("CREATE FUNCTION " + name + "_fn() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected storage failure'; END $$"); err != nil {
			t.Fatal(err)
		}
		query = "CREATE TRIGGER " + name + " BEFORE INSERT ON " + table + " FOR EACH ROW EXECUTE FUNCTION " + name + "_fn()"
	}
	if _, err := d.Exec(query); err != nil {
		t.Fatal(err)
	}
	return func() {
		q := "DROP TRIGGER " + name
		if d.Dialect() == db.Postgres {
			q += " ON " + table
		}
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}
func TestI10PostgresRuntimeContracts(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(*testing.T)
	}{{"atomic-result", TestI10RealOrderResultConditionsAndActionsAtomicRecovery}, {"unknown-resolution", TestI10UnknownEffectRequiresAuditedResolutionAndNeverAutoRetries}, {"effects-recovery", TestI10ExternalActionUnknownIsNotRepeatedAfterRestart}, {"retry-generation", TestI10KnownFailureRetryIsDurableAndOldAttemptCannotFinishNext}, {"operation-protection", TestI10OperationsNeverOverwriteUnresolvedAttempt}, {"effect-audit", TestI10EffectResolutionRequiresEvidenceAndAudit}, {"internal-http", TestI10InternalHTTPUsesJournalAndUnknownNeverRetries}, {"runtime-sla", TestI10RuntimeActionsAndSLARecoverAtomically}, {"internal-ssh", TestI10RealSSHReceiptsLossAndCancellation}} {
		t.Run(test.name, test.run)
	}
}
