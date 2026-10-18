package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/jmlero/the-platform/marina/internal/execution"
)

func TestTerminalTransitionRecordsTheExecutionTrace(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	ctx := context.Background()
	now := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	store := openStore(t, ctx, filepath.Join(t.TempDir(), "marina.db"), &now)
	record, _, err := store.EnsureExecution(ctx, validExecution(now, "run-1"))
	if err != nil {
		t.Fatal(err)
	}
	record = transition(t, ctx, store, record, execution.StateRunning, "plan approved")
	if spans := exporter.GetSpans(); len(spans) != 0 {
		t.Fatalf("non-terminal transition exported %d spans", len(spans))
	}
	transition(t, ctx, store, record, execution.StateCancelled, "operator cancelled")
	spans := exporter.GetSpans()
	if len(spans) != 1 || spans[0].Name != "marina.execution" {
		t.Fatalf("spans = %v", spans)
	}
	want := map[attribute.Key]string{"marina.execution.id": record.ID, "marina.task.id": record.ExternalWorkID, "marina.execution.state": "cancelled"}
	for _, value := range spans[0].Attributes {
		if expected, ok := want[value.Key]; ok && value.Value.AsString() == expected {
			delete(want, value.Key)
		}
	}
	if len(want) != 0 || !spans[0].StartTime.Equal(record.CreatedAt) {
		t.Fatalf("missing %v; start %v want %v", want, spans[0].StartTime, record.CreatedAt)
	}
}
