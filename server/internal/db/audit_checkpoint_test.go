package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func auditCheckpointDB(t *testing.T) (*DB, string) {
	t.Helper()
	dir := t.TempDir()
	d, err := Open(SQLite, filepath.Join(dir, "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err = Migrate(d); err != nil {
		t.Fatal(err)
	}
	return d, filepath.Join(dir, "audit.key")
}

func appendAuditRows(t *testing.T, d *DB, n int) {
	t.Helper()
	for range n {
		if err := d.AuditObservation("test.event", "ok"); err != nil {
			t.Fatal(err)
		}
	}
}

// O boot incremental verifica só o trecho posterior ao checkpoint assinado; o
// trecho anterior fica para o segundo plano, que detecta adulteração, apaga o
// checkpoint e devolve o boot seguinte à verificação integral (que recusa).
func TestAuditIncrementalStartup(t *testing.T) {
	d, key := auditCheckpointDB(t)
	first, err := d.EnableAuditIncremental(key, 100000)
	if err != nil || first.Deferred != 0 {
		t.Fatalf("primeiro boot deve verificar tudo: %+v %v", first, err)
	}
	appendAuditRows(t, d, 5)

	second, err := d.EnableAuditIncremental(key, 100000)
	if err != nil || second.Deferred != 0 || second.Head != 5 {
		t.Fatalf("checkpoint vazio + 5 registros: %+v %v", second, err)
	}
	appendAuditRows(t, d, 3)
	third, err := d.EnableAuditIncremental(key, 100000)
	if err != nil || third.Deferred != 5 || third.Head != 8 {
		t.Fatalf("boot incremental deve adiar 1..5 e verificar 6..8: %+v %v", third, err)
	}

	// Adulteração DEPOIS do checkpoint: recusa antes de servir.
	appendAuditRows(t, d, 2)
	if _, err = d.DB.Exec("UPDATE security_audit SET payload='{}' WHERE seq=10"); err != nil {
		t.Fatal(err)
	}
	if _, err = d.EnableAuditIncremental(key, 100000); err == nil {
		t.Fatal("registro adulterado depois do checkpoint passou no boot")
	}
}

func TestAuditIncrementalDetectsOldTamperInBackground(t *testing.T) {
	d, key := auditCheckpointDB(t)
	if _, err := d.EnableAuditIncremental(key, 100000); err != nil {
		t.Fatal(err)
	}
	appendAuditRows(t, d, 6)
	if _, err := d.EnableAuditIncremental(key, 100000); err != nil { // checkpoint em 6
		t.Fatal(err)
	}
	if _, err := d.DB.Exec("UPDATE security_audit SET payload='{}' WHERE seq=2"); err != nil {
		t.Fatal(err)
	}
	start, err := d.EnableAuditIncremental(key, 100000)
	if err != nil || start.Deferred != 6 {
		t.Fatalf("adulteração antiga não bloqueia o boot incremental (por desenho): %+v %v", start, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err = d.VerifyAuditBackground(ctx, start, time.Hour)
	if !errors.Is(err, ErrAuditBackground) {
		t.Fatalf("segundo plano não detectou a adulteração: %v", err)
	}
	if _, statErr := os.Stat(key + ".checkpoint"); !os.IsNotExist(statErr) {
		t.Fatal("checkpoint deveria ter sido removido após falha de integridade")
	}
	// Sem checkpoint, o boot seguinte verifica tudo e recusa.
	if _, err = d.EnableAuditIncremental(key, 100000); err == nil {
		t.Fatal("boot seguinte aceitou trilha adulterada")
	}
}

func TestAuditIncrementalRefusesRollbackAndForgedCheckpoint(t *testing.T) {
	d, key := auditCheckpointDB(t)
	if _, err := d.EnableAuditIncremental(key, 100000); err != nil {
		t.Fatal(err)
	}
	appendAuditRows(t, d, 4)
	if _, err := d.EnableAuditIncremental(key, 100000); err != nil { // checkpoint em 4
		t.Fatal(err)
	}
	// Checkpoint forjado (assinatura inválida) é ignorado: verificação integral.
	good, err := os.ReadFile(key + ".checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	forged := strings.Replace(string(good), `"seq":4`, `"seq":3`, 1)
	if err = os.WriteFile(key+".checkpoint", []byte(forged), 0600); err != nil {
		t.Fatal(err)
	}
	start, err := d.EnableAuditIncremental(key, 100000)
	if err != nil || start.Deferred != 0 {
		t.Fatalf("checkpoint forjado deve cair na verificação integral: %+v %v", start, err)
	}
	// Banco atrás do checkpoint (truncamento/rollback): recusa.
	for _, q := range []string{"DELETE FROM audit_delivery WHERE seq=4", "DELETE FROM security_audit WHERE seq=4",
		"UPDATE audit_head SET seq=3, hash=(SELECT hash FROM security_audit WHERE seq=3) WHERE id=1"} {
		if _, err = d.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = d.EnableAuditIncremental(key, 100000); err == nil || !strings.Contains(err.Error(), "behind the signed startup checkpoint") {
		t.Fatalf("rollback não foi recusado: %v", err)
	}
}

// O segundo plano avança o checkpoint enquanto o server roda, para o próximo
// boot ter pouco a verificar.
func TestAuditBackgroundAdvancesCheckpoint(t *testing.T) {
	d, key := auditCheckpointDB(t)
	start, err := d.EnableAuditIncremental(key, 100000)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.VerifyAuditBackground(ctx, start, 20*time.Millisecond) }()
	appendAuditRows(t, d, 7)
	deadline := time.Now().Add(10 * time.Second)
	for {
		if cp := d.readAuditCheckpoint(key + ".checkpoint"); cp != nil && cp.Seq == 7 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("checkpoint não avançou até o registro 7")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("encerramento por cancelamento esperado: %v", err)
	}
}
