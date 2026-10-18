package telemetry

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func record(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := newProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	return exporter
}

func TestStepsAndExecutionShareOneTraceAcrossRestarts(t *testing.T) {
	exporter := record(t)
	ctx := context.Background()
	start := time.Unix(1_800_000_000, 0)
	ids := Execution{ID: "exec-1", ProjectID: "project", TaskID: "task", RunID: "run"}

	RecordStep(ctx, ids, "implement", start, start.Add(time.Minute))
	// A new process derives the same identities from the durable execution ID.
	otel.SetTracerProvider(newProvider(sdktrace.WithSyncer(exporter)))
	RecordStep(ctx, ids, "verify", start.Add(time.Minute), start.Add(2*time.Minute))
	RecordExecution(ctx, ids, "completed", start, start.Add(3*time.Minute))

	spans := exporter.GetSpans()
	if len(spans) != 3 {
		t.Fatalf("spans = %d, want 3", len(spans))
	}
	root := spans[2]
	if root.Name != "marina.execution" || root.Parent.IsValid() {
		t.Fatalf("root = %s parent %v", root.Name, root.Parent)
	}
	for _, step := range spans[:2] {
		if step.SpanContext.TraceID() != root.SpanContext.TraceID() || step.Parent.SpanID() != root.SpanContext.SpanID() {
			t.Fatalf("step %s is not a child of the execution root", step.Name)
		}
	}
	if spans[0].Name != "marina.step implement" || !spans[0].StartTime.Equal(start) || !spans[0].EndTime.Equal(start.Add(time.Minute)) {
		t.Fatalf("step span = %s %v %v", spans[0].Name, spans[0].StartTime, spans[0].EndTime)
	}
	if !hasAttribute(root.Attributes, attribute.String("marina.task.id", "task")) || !hasAttribute(root.Attributes, attribute.String("marina.execution.state", "completed")) {
		t.Fatalf("root attributes = %v", root.Attributes)
	}
	other := Execution{ID: "exec-2"}
	RecordStep(ctx, other, "implement", start, start)
	if exporter.GetSpans()[3].SpanContext.TraceID() == root.SpanContext.TraceID() {
		t.Fatal("different executions share a trace")
	}
}

func TestModelCallRecordsUsageWithoutContent(t *testing.T) {
	exporter := record(t)
	start := time.Unix(1_800_000_000, 0)
	RecordModelCall(context.Background(), Execution{ID: "exec-1"}, ModelCall{
		AttemptID: "attempt", Model: "model-a", Endpoint: "https://models.example/api/v1",
		InputTokens: 10, OutputTokens: 5, CostMicros: 7, Known: true, Start: start, End: start.Add(time.Second),
	})
	span := exporter.GetSpans()[0]
	if span.Name != "chat model-a" {
		t.Fatalf("name = %s", span.Name)
	}
	for _, want := range []attribute.KeyValue{
		attribute.String("gen_ai.operation.name", "chat"),
		attribute.String("gen_ai.request.model", "model-a"),
		attribute.String("server.address", "models.example"),
		attribute.Int64("gen_ai.usage.input_tokens", 10),
		attribute.Int64("gen_ai.usage.output_tokens", 5),
		attribute.Int64("marina.cost_micros", 7),
		attribute.String("marina.attempt.id", "attempt"),
	} {
		if !hasAttribute(span.Attributes, want) {
			t.Fatalf("missing %v in %v", want, span.Attributes)
		}
	}
	RecordModelCall(context.Background(), Execution{ID: "exec-1"}, ModelCall{Model: "model-a", Error: "model endpoint unavailable", Start: start, End: start})
	failed := exporter.GetSpans()[1]
	if failed.Status.Description != "model endpoint unavailable" || hasKey(failed.Attributes, "gen_ai.usage.input_tokens") {
		t.Fatalf("failed call = %v %v", failed.Status, failed.Attributes)
	}
}

func TestStartWithoutEndpointExportsNothing(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	shutdown, err := Start(context.Background())
	if err != nil || shutdown(context.Background()) != nil {
		t.Fatalf("start without endpoint: %v", err)
	}
}

func hasAttribute(values []attribute.KeyValue, want attribute.KeyValue) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasKey(values []attribute.KeyValue, key attribute.Key) bool {
	for _, value := range values {
		if value.Key == key {
			return true
		}
	}
	return false
}
