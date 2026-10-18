// Package telemetry exports OpenTelemetry traces for executions (PLAT-09).
//
// Each execution is one trace. Its trace and root span IDs derive from the
// durable execution ID, so spans recorded before and after a restart join the
// same trace. Spans carry identities, timing and usage only: never prompts,
// model output, repository content or credentials.
package telemetry

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"net/url"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
)

const scope = "github.com/jmlero/the-platform/marina"

// Execution identifies the durable work a span belongs to.
type Execution struct {
	ID, ProjectID, TaskID, RunID string
}

// ModelCall describes one governed upstream model request.
type ModelCall struct {
	AttemptID, Model, Endpoint string
	InputTokens, OutputTokens  int64
	CostMicros                 int64
	Known                      bool
	Error                      string
	Start, End                 time.Time
}

// Start installs an OTLP/HTTP trace exporter configured by the standard
// OTEL_EXPORTER_OTLP_* and OTEL_SERVICE_NAME variables. Without an endpoint it
// installs nothing and spans are dropped.
func Start(ctx context.Context) (func(context.Context) error, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" && os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	provider := newProvider(sdktrace.WithBatcher(exporter))
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}

func newProvider(options ...sdktrace.TracerProviderOption) *sdktrace.TracerProvider {
	name := os.Getenv("OTEL_SERVICE_NAME")
	if name == "" {
		name = "marina"
	}
	attributes := resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(name))
	return sdktrace.NewTracerProvider(append(options, sdktrace.WithResource(attributes), sdktrace.WithIDGenerator(ids{}))...)
}

// RecordStep records one completed workflow step of an execution.
func RecordStep(ctx context.Context, e Execution, step string, start, end time.Time) {
	_, span := tracer().Start(child(ctx, e), "marina.step "+step, trace.WithTimestamp(start), trace.WithAttributes(e.attributes()...))
	span.SetAttributes(attribute.String("marina.step", step))
	span.End(trace.WithTimestamp(end))
}

// RecordExecution records the execution's root span once it reaches a terminal state.
func RecordExecution(ctx context.Context, e Execution, state string, start, end time.Time) {
	traceID, spanID := identities(e.ID)
	ctx = context.WithValue(trace.ContextWithSpanContext(ctx, trace.SpanContext{}), forcedKey{}, [2][]byte{traceID[:], spanID[:]})
	_, span := tracer().Start(ctx, "marina.execution", trace.WithNewRoot(), trace.WithTimestamp(start), trace.WithAttributes(e.attributes()...))
	span.SetAttributes(attribute.String("marina.execution.state", state))
	if state == "blocked" || state == "failed" {
		span.SetStatus(codes.Error, state)
	}
	span.End(trace.WithTimestamp(end))
}

// RecordModelCall records one model request with GenAI semantic attributes.
func RecordModelCall(ctx context.Context, e Execution, call ModelCall) {
	attributes := append(e.attributes(),
		attribute.String("gen_ai.operation.name", "chat"),
		attribute.String("gen_ai.request.model", call.Model),
		attribute.String("marina.attempt.id", call.AttemptID))
	if endpoint, err := url.Parse(call.Endpoint); err == nil && endpoint.Hostname() != "" {
		attributes = append(attributes, attribute.String("server.address", endpoint.Hostname()))
	}
	_, span := tracer().Start(child(ctx, e), "chat "+call.Model, trace.WithSpanKind(trace.SpanKindClient), trace.WithTimestamp(call.Start), trace.WithAttributes(attributes...))
	if call.Known {
		span.SetAttributes(
			attribute.Int64("gen_ai.usage.input_tokens", call.InputTokens),
			attribute.Int64("gen_ai.usage.output_tokens", call.OutputTokens),
			attribute.Int64("marina.cost_micros", call.CostMicros))
	}
	if call.Error != "" {
		span.SetStatus(codes.Error, call.Error)
	}
	span.End(trace.WithTimestamp(call.End))
}

func tracer() trace.Tracer { return otel.Tracer(scope) }

func (e Execution) attributes() []attribute.KeyValue {
	values := []attribute.KeyValue{attribute.String("marina.execution.id", e.ID)}
	for key, value := range map[string]string{"marina.project.id": e.ProjectID, "marina.task.id": e.TaskID, "marina.run.id": e.RunID} {
		if value != "" {
			values = append(values, attribute.String(key, value))
		}
	}
	return values
}

func child(ctx context.Context, e Execution) context.Context {
	traceID, spanID := identities(e.ID)
	parent := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled, Remote: true})
	return trace.ContextWithRemoteSpanContext(ctx, parent)
}

func identities(executionID string) (trace.TraceID, trace.SpanID) {
	digest := sha256.Sum256([]byte("marina-execution:" + executionID))
	var traceID trace.TraceID
	var spanID trace.SpanID
	copy(traceID[:], digest[:16])
	copy(spanID[:], digest[16:24])
	return traceID, spanID
}

type forcedKey struct{}

// ids gives the execution root its derived identities and every other span a
// random span ID.
type ids struct{}

func (ids) NewIDs(ctx context.Context) (trace.TraceID, trace.SpanID) {
	var traceID trace.TraceID
	var spanID trace.SpanID
	if forced, ok := ctx.Value(forcedKey{}).([2][]byte); ok {
		copy(traceID[:], forced[0])
		copy(spanID[:], forced[1])
		return traceID, spanID
	}
	_, _ = rand.Read(traceID[:])
	_, _ = rand.Read(spanID[:])
	return traceID, spanID
}

func (ids) NewSpanID(context.Context, trace.TraceID) trace.SpanID {
	var spanID trace.SpanID
	_, _ = rand.Read(spanID[:])
	return spanID
}
