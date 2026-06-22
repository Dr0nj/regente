package telemetry

import (
	"context"
	"testing"
)

// Sem endpoint, Init é no-op: não erra, devolve shutdown válido, e Span não panica.
func TestInit_NoEndpointIsNoop(t *testing.T) {
	shutdown, err := Init(context.Background(), "", "regente-test")
	if err != nil {
		t.Fatalf("Init sem endpoint não deveria errar: %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown deveria ser não-nil")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown no-op não deveria errar: %v", err)
	}
	_, span := Span(context.Background(), "teste")
	span.End() // tracer no-op — não deve panicar
}
