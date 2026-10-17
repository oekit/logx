package logx

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

type traceCtxKey struct{}

func withTrace(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceCtxKey{}, id)
}

// 测试装配：注册假提取器（生产由 tracing/traceid 经 SetTraceExtractor 注册）。
func installExtractor(t *testing.T) {
	t.Helper()
	SetTraceExtractor(func(ctx context.Context) string {
		if v, ok := ctx.Value(traceCtxKey{}).(string); ok {
			return v
		}
		return ""
	})
	t.Cleanup(func() { SetTraceExtractor(nil) })
}

func TestTraceHandler_InjectsTraceID(t *testing.T) {
	installExtractor(t)
	var buf bytes.Buffer
	h := TraceHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(h)

	ctx := withTrace(context.Background(), "trace-abc")
	logger.InfoContext(ctx, "handled")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	if rec["trace_id"] != "trace-abc" {
		t.Fatalf("trace_id = %v", rec["trace_id"])
	}
}

func TestTraceHandler_NoTrace_KeepsRecordClean(t *testing.T) {
	installExtractor(t)
	var buf bytes.Buffer
	logger := slog.New(TraceHandler(slog.NewJSONHandler(&buf, nil)))

	logger.Info("background task")

	if strings.Contains(buf.String(), "trace_id") {
		t.Fatalf("background record should not carry empty trace_id: %s", buf.String())
	}
}

func TestTraceHandler_NoDuplicateWhenPresent(t *testing.T) {
	installExtractor(t)
	var buf bytes.Buffer
	logger := slog.New(TraceHandler(slog.NewJSONHandler(&buf, nil)))

	ctx := withTrace(context.Background(), "trace-abc")
	logger.InfoContext(ctx, "handled", "trace_id", "explicit-xyz")

	if !strings.Contains(buf.String(), "explicit-xyz") {
		t.Fatalf("explicit value lost: %s", buf.String())
	}
	if strings.Contains(buf.String(), "trace-abc") {
		t.Fatalf("should not duplicate inject: %s", buf.String())
	}
}

func TestTraceHandler_WithAttrsKeepsWrapping(t *testing.T) {
	installExtractor(t)
	var buf bytes.Buffer
	base := TraceHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(base).With("svc", "lightcat")

	ctx := withTrace(context.Background(), "trace-with")
	logger.InfoContext(ctx, "derived")

	if !strings.Contains(buf.String(), "trace-with") {
		t.Fatalf("With() lost trace injection: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "lightcat") {
		t.Fatalf("attrs lost: %s", buf.String())
	}
}
