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

// TestTraceHandler_NoDuplicateFromWithAttrs（#104）：经 handler.With(...)
// 挂上去的同名属性进了 inner、不在 Record 里，此前「已有同名字段不重复
// 注入」只查 Record——trace_id 被注入第二次，输出重复键（Go 的
// encoding/json 取后者，别的解析器可能取前者，取值不确定）。
// 钉住：With("trace_id","manual") 后输出恰一个 trace_id，值为 manual。
func TestTraceHandler_NoDuplicateFromWithAttrs(t *testing.T) {
	installExtractor(t)
	var buf bytes.Buffer
	base := TraceHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(base).With("trace_id", "manual")

	ctx := withTrace(context.Background(), "from-ctx")
	logger.InfoContext(ctx, "derived")

	out := buf.String()
	if n := strings.Count(out, `"trace_id"`); n != 1 {
		t.Fatalf("trace_id 出现 %d 次, want 1: %s", n, out)
	}
	if !strings.Contains(out, `"trace_id":"manual"`) {
		t.Fatalf("应保留调用方显式值 manual: %s", out)
	}
	if strings.Contains(out, "from-ctx") {
		t.Fatalf("不应再注入 ctx 提取值: %s", out)
	}
}

// TestTraceHandler_NoDuplicateFromWithThenWithGroup（#129）：WithGroup 创建
// 子 handler 时此前未继承 withKeys（#104 的修复只落在 WithAttrs），而
// 「With(键).WithGroup(组)」是 slog 最常见的链式写法——Handle 判重断链，
// ctx 提取值被二次注入且落进组，一条 JSON 记录出现两个 trace_id（显式值
// 在顶层、ctx 值在组里，Loki/ES 对同名键取值依解析器而定，追踪链可能串
// 到错误的 id）。钉住：With+WithGroup 链上输出恰一个 trace_id，值为
// 调用方显式挂的 manual。
func TestTraceHandler_NoDuplicateFromWithThenWithGroup(t *testing.T) {
	installExtractor(t)
	var buf bytes.Buffer
	base := TraceHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(base).With("trace_id", "manual").WithGroup("g")

	ctx := withTrace(context.Background(), "from-ctx")
	logger.InfoContext(ctx, "derived", "k", "v")

	out := buf.String()
	if n := strings.Count(out, `"trace_id"`); n != 1 {
		t.Fatalf("trace_id 出现 %d 次, want 1: %s", n, out)
	}
	if !strings.Contains(out, `"trace_id":"manual"`) {
		t.Fatalf("应保留调用方显式值 manual: %s", out)
	}
	if strings.Contains(out, "from-ctx") {
		t.Fatalf("不应再注入 ctx 提取值: %s", out)
	}
	// 组内用户字段不受影响，正常落组。
	if !strings.Contains(out, `"g":{"k":"v"}`) {
		t.Fatalf("用户字段应落在组 g 内: %s", out)
	}
}

// TestTraceHandler_WithGroupThenWithAttrs_KeepsDedup（#129 同链路延伸）：
// WithGroup 继承的 withKeys 必须继续参与后续 WithAttrs 的合并——多级派生
// 链（With → WithGroup → With）上判重不因中间的 WithGroup 断链。
func TestTraceHandler_WithGroupThenWithAttrs_KeepsDedup(t *testing.T) {
	installExtractor(t)
	var buf bytes.Buffer
	base := TraceHandler(slog.NewJSONHandler(&buf, nil))
	logger := slog.New(base).WithGroup("g").With("trace_id", "manual")

	ctx := withTrace(context.Background(), "from-ctx")
	logger.InfoContext(ctx, "derived")

	out := buf.String()
	if n := strings.Count(out, `"trace_id"`); n != 1 {
		t.Fatalf("trace_id 出现 %d 次, want 1: %s", n, out)
	}
	if !strings.Contains(out, `"trace_id":"manual"`) {
		t.Fatalf("应保留调用方显式值 manual: %s", out)
	}
	if strings.Contains(out, "from-ctx") {
		t.Fatalf("不应再注入 ctx 提取值: %s", out)
	}
}
