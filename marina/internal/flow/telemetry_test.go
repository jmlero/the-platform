package flow

import (
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestEachFinishedWorkflowStepIsRecordedOnce(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	f := newEngineFlow(t)
	s := f.s
	var err error
	for _, update := range []struct{ step, status string }{{"implement", ""}, {"implement", "TASK_STATE_WORKING"}, {"verify", ""}, {"verify", ""}} {
		s.Step, s.EngineStatus = update.step, update.status
		if err = f.c.save(f.ctx, f.e, s); err != nil {
			t.Fatal(err)
		}
		if s, err = f.c.load(f.ctx, f.e); err != nil {
			t.Fatal(err)
		}
	}
	var names []string
	for _, span := range exporter.GetSpans() {
		names = append(names, span.Name)
	}
	if len(names) != 2 || names[0] != "marina.step prepare" || names[1] != "marina.step implement" {
		t.Fatalf("step spans = %v", names)
	}
	implement := exporter.GetSpans()[1]
	if implement.StartTime.IsZero() || implement.EndTime.Before(implement.StartTime) {
		t.Fatalf("implement span times %v %v", implement.StartTime, implement.EndTime)
	}
}
