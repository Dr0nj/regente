package execution

import (
	"strings"
	"testing"
)

// O ACK de cancelamento conserva os chunks recebidos e o exit code terminal.
func TestI10CancellationPreservesOutput(t *testing.T) {
	f := newFixture(t)
	o := f.order(t, "cancel-output")
	a := f.start(t, o, "start")
	f.running(t, a)
	for i, chunk := range []string{"before cancellation\n", "partial progress"} {
		if _, err := f.e.Append(Output{Identity: identity(a), Seq: int64(i + 1), Chunk: chunk}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.e.Cancel(o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.Acknowledge(identity(a), "cancelled"); err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	var output string
	var exit int
	if err := f.d.QueryRow("SELECT result_output,exit_code FROM execution_attempts WHERE execution_id=?", a.ExecutionID).Scan(&output, &exit); err != nil {
		t.Fatal(err)
	}
	if exit != -1 || !strings.HasPrefix(output, "before cancellation\npartial progress") || !strings.Contains(output, "cancellation acknowledged") {
		t.Fatalf("exit=%d output=%q", exit, output)
	}
	if receipt, err := f.e.Acknowledge(identity(a), "cancelled"); err != nil || !receipt.Duplicate {
		t.Fatal(receipt, err)
	}
	var again string
	if err := f.d.QueryRow("SELECT result_output FROM execution_attempts WHERE execution_id=?", a.ExecutionID).Scan(&again); err != nil || again != output {
		t.Fatal(again, err)
	}
}
