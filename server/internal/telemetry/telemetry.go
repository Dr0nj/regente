// Package telemetry — tracing distribuído via OpenTelemetry (OTLP/HTTP).
//
// Opt-in: sem endpoint configurado, o tracer global fica no-op (zero overhead,
// comportamento idêntico ao anterior). Com `-otel-endpoint` (ou
// OTEL_EXPORTER_OTLP_ENDPOINT), exporta spans para um collector OTLP/HTTP
// (Tempo, Jaeger, Grafana Alloy, etc.). Anti-lock-in: OTLP é padrão aberto.
package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/Dr0nj/regente-server"

// Init configura o tracing se endpoint != "". Sem endpoint, devolve um shutdown
// no-op e deixa o tracer global como no-op. O shutdown retornado é sempre não-nil.
func Init(ctx context.Context, endpoint, serviceName string) (func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	if endpoint == "" {
		return noop, nil
	}
	exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
	if err != nil {
		return noop, err
	}
	res, err := resource.New(ctx, resource.WithAttributes(
		attribute.String("service.name", serviceName),
	))
	if err != nil {
		res = resource.Default()
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// Tracer devolve o tracer global (no-op se Init não configurou exporter).
func Tracer() trace.Tracer { return otel.Tracer(tracerName) }

// Span inicia um span no tracer global. Conveniência para o resto do código:
// se o tracing estiver off, é praticamente gratuito (span no-op).
func Span(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	ctx, span := Tracer().Start(ctx, name)
	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	return ctx, span
}
