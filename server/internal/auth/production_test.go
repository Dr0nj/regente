package auth

import (
	"github.com/Dr0nj/regente-server/internal/db"
	"path/filepath"
	"testing"
)

func productionDB(t *testing.T) *db.DB {
	t.Helper()
	d, e := db.Open(db.SQLite, filepath.Join(t.TempDir(), "state.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { d.Close() })
	if e = db.Migrate(d); e != nil {
		t.Fatal(e)
	}
	return d
}
func TestProductionBootstrapAndUpgrade(t *testing.T) {
	const password = "production-fixture-only"
	t.Run("fresh", func(t *testing.T) {
		d := productionDB(t)
		if BootstrapProduction(d, "admin", "", "") == nil {
			t.Fatal("bootstrap sem senha")
		}
		var n int
		d.QueryRow("SELECT COUNT(*) FROM users").Scan(&n)
		if n != 0 {
			t.Fatal("criou default")
		}
		if e := BootstrapProduction(d, "admin", password, "admin"); e != nil {
			t.Fatal(e)
		}
		if e := BootstrapProduction(d, "admin", "different-fixture-password", ""); e != nil {
			t.Fatal(e)
		}
		if _, _, e := Login(d, "admin", password); e != nil {
			t.Fatal("sobrescreveu senha", e)
		}
		if BootstrapProduction(d, "admin", "", "missing") == nil {
			t.Fatal("emergência inexistente aceita")
		}
	})
	t.Run("upgrade", func(t *testing.T) {
		d := productionDB(t)
		if e := Bootstrap(d); e != nil {
			t.Fatal(e)
		}
		if BootstrapProduction(d, "admin", "", "") == nil {
			t.Fatal("admin/admin aceito")
		}
		var id int64
		d.QueryRow("SELECT id FROM users WHERE username='admin'").Scan(&id)
		if e := ChangePassword(d, id, "admin", password, false); e != nil {
			t.Fatal(e)
		}
		if e := BootstrapProduction(d, "admin", "", "admin"); e != nil {
			t.Fatal(e)
		}
	})
}
