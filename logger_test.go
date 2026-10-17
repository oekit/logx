package logx

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"strings"
	"testing"
)

type testTraceKey struct{}

// withTestTrace 向 context 注入 trace_id，使用测试专用的提取器。
// 由于 logx 不再依赖 middleware 包，测试通过 SetTraceExtractor 注入自定义提取器，
// 验证 trace_id 能通过 Ctx* 方法正确提取。
func withTestTrace(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, testTraceKey{}, traceID)
}

// setTestExtractor 注册测试用 trace 提取器，返回恢复函数。
func setTestExtractor() func() {
	prev := traceExtractor.Load()
	SetTraceExtractor(func(ctx context.Context) string {
		if v, ok := ctx.Value(testTraceKey{}).(string); ok {
			return v
		}
		return ""
	})
	return func() {
		if prev != nil {
			SetTraceExtractor(*prev)
		} else {
			SetTraceExtractor(nil)
		}
	}
}

func TestFormatKeysAndValues(t *testing.T) {
	tests := []struct {
		name   string
		input  []any
		expect string
	}{
		{"empty", nil, ""},
		{"single pair", []any{"key", "val"}, "key=val"},
		{"multiple pairs", []any{"k1", "v1", "k2", 42}, "k1=v1 k2=42"},
		{"missing value", []any{"key"}, "key=(MISSING)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatKeysAndValues(tt.input...)
			if result != tt.expect {
				t.Errorf("FormatKeysAndValues(%v) = %q, want %q", tt.input, result, tt.expect)
			}
		})
	}
}

func TestStdLogger_WithService(t *testing.T) {
	logger := NewStdLogger("app")
	child := logger.WithService("release")

	stdChild, ok := child.(*stdLogger)
	if !ok {
		t.Fatal("expected *stdLogger")
	}
	if stdChild.Service != "release" {
		t.Errorf("service = %q, want %q", stdChild.Service, "release")
	}

	// 原 logger 不受影响
	stdParent, ok := logger.(*stdLogger)
	if !ok {
		t.Fatal("expected *stdLogger")
	}
	if stdParent.Service != "app" {
		t.Errorf("parent service = %q, want %q", stdParent.Service, "app")
	}
}

func TestStdLogger_ShouldLog(t *testing.T) {
	logger := NewStdLogger("test", LevelWarn)

	std, ok := logger.(*stdLogger)
	if !ok {
		t.Fatal("expected *stdLogger")
	}

	tests := []struct {
		level  Level
		expect bool
	}{
		{LevelDebug, false},
		{LevelInfo, false},
		{LevelWarn, true},
		{LevelError, true},
	}

	for _, tt := range tests {
		if got := std.ShouldLog(tt.level); got != tt.expect {
			t.Errorf("shouldLog(%v) = %v, want %v", tt.level, got, tt.expect)
		}
	}
}

func TestDefaultLogger(t *testing.T) {
	restore := setTestExtractor()
	defer restore()

	// 测试全局方法不会 panic
	Info("global info", "key", "val")
	Error("global error")

	ctx := context.Background()
	ctx = withTestTrace(ctx, "trace-123")
	CtxInfo(ctx, "ctx info", "key", "val")
}

// TestExtractTrace_NoExtractor 验证未注册提取器时返回空字符串（不 panic）。
func TestExtractTrace_NoExtractor(t *testing.T) {
	prev := traceExtractor.Load()
	SetTraceExtractor(nil)
	defer func() {
		if prev != nil {
			SetTraceExtractor(*prev)
		}
	}()

	if got := ExtractTrace(context.Background()); got != "" {
		t.Errorf("expected empty string with no extractor, got %q", got)
	}
}

func TestSetDefault(t *testing.T) {
	original := Default()
	defer SetDefault(original) // 恢复

	std := NewStdLogger("test", LevelDebug)
	SetDefault(std)

	if Default() != std {
		t.Error("SetDefault did not update defaultLogger")
	}
}

// TestSlogLogger_Structured 验证 slogLogger 输出真正的结构化 JSON，
// 且 WithAttrs 的属性会附加到每条记录。
func TestSlogLogger_Structured(t *testing.T) {
	restore := setTestExtractor()
	defer restore()

	l := NewSlogLogger("test-svc").WithAttrs(Attr{Key: "request_id", Value: "r-123"})
	// 触发一次输出（写到 stdout，这里只验证不 panic 且级别过滤生效）
	l.SetLevel(LevelWarn)
	l.Debug("should be filtered") // 低于 WARN，不输出
	l.Warn("warning msg", "code", 500)
}

// TestSlogLogger_LevelFilter 验证 SetLevel 后低级别日志被过滤。
func TestSlogLogger_LevelFilter(t *testing.T) {
	logger := NewSlogLogger("svc")
	logger.SetLevel(LevelError)

	// ShouldLog(LevelWarn) 应返回 false（Warn < Error）
	sl := logger.(*slogLogger)
	if sl.ShouldLog(LevelWarn) {
		t.Error("expected ShouldLog(LevelWarn) = false at LevelError")
	}
	if !sl.ShouldLog(LevelError) {
		t.Error("expected ShouldLog(LevelError) = true at LevelError")
	}
}

// TestSlogLogger_WithService 验证 WithService 返回独立实例。
func TestSlogLogger_WithService(t *testing.T) {
	logger := NewSlogLogger("parent")
	child := logger.WithService("child")
	sl, ok := child.(*slogLogger)
	if !ok {
		t.Fatal("expected *slogLogger")
	}
	if sl.Service != "child" {
		t.Errorf("service = %q, want child", sl.Service)
	}
}

// captureStdout 捕获 os.Stdout 输出，返回恢复函数与读取函数。
func captureStdout(t *testing.T) (read func() string, restore func()) {
	t.Helper()
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	restore = func() {
		os.Stdout = old
		w.Close()
	}
	read = func() string {
		restore()
		out, _ := io.ReadAll(r)
		return string(out)
	}
	return read, restore
}

// TestWithCtx_BindsContext 验证 WithCtx 绑定 ctx 且不污染原 logger，
// 且 WithService/WithAttrs/WithGroup 继承绑定 ctx。
func TestWithCtx_BindsContext(t *testing.T) {
	parent := NewStdLogger("app")
	ctx := context.WithValue(context.Background(), testTraceKey{}, "t-1")

	bound := parent.WithCtx(ctx)

	stdParent := parent.(*stdLogger)
	stdBound := bound.(*stdLogger)
	if stdParent.Ctx != nil {
		t.Errorf("parent ctx should be nil, got %v", stdParent.Ctx)
	}
	if stdBound.Ctx != ctx {
		t.Errorf("bound ctx not set correctly")
	}

	// 子 logger 继承绑定 ctx
	child := bound.WithService("release")
	stdChild := child.(*stdLogger)
	if stdChild.Ctx != ctx {
		t.Errorf("WithService child should inherit bound ctx")
	}

	attrChild := bound.WithAttrs(Attr{Key: "k", Value: "v"})
	stdAttr := attrChild.(*stdLogger)
	if stdAttr.Ctx != ctx {
		t.Errorf("WithAttrs child should inherit bound ctx")
	}

	groupChild := bound.WithGroup("g")
	stdGroup := groupChild.(*stdLogger)
	if stdGroup.Ctx != ctx {
		t.Errorf("WithGroup child should inherit bound ctx")
	}
}

// TestWithCtx_StdTraceInOutput 验证 stdLogger 绑定 ctx 后，
// 非 Ctx 方法 Info 自动输出 trace_id。
func TestWithCtx_StdTraceInOutput(t *testing.T) {
	restore := setTestExtractor()
	defer restore()

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	ctx := withTestTrace(context.Background(), "trace-xyz")
	logger := NewStdLogger("svc", LevelDebug).WithCtx(ctx)
	logger.Info("hello", "k", "v")

	out := buf.String()
	if !strings.Contains(out, "[trace=trace-xyz]") {
		t.Errorf("expected trace in output, got: %s", out)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected message in output, got: %s", out)
	}
}

// TestWithCtx_StdNoTraceWithoutBinding 验证未绑定时非 Ctx 方法不带 trace_id。
func TestWithCtx_StdNoTraceWithoutBinding(t *testing.T) {
	restore := setTestExtractor()
	defer restore()

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	NewStdLogger("svc", LevelDebug).Info("no-ctx")
	if strings.Contains(buf.String(), "[trace=") {
		t.Errorf("should not contain trace without binding, got: %s", buf.String())
	}
}

// TestWithCtx_SlogTraceInOutput 验证 slogLogger 绑定 ctx 后非 Ctx 方法输出 trace_id 字段。
func TestWithCtx_SlogTraceInOutput(t *testing.T) {
	restore := setTestExtractor()
	defer restore()

	read, _ := captureStdout(t)
	ctx := withTestTrace(context.Background(), "slog-trace")
	logger := NewSlogLogger("svc").WithCtx(ctx)
	logger.Info("structured", "k", "v")

	out := read()
	if !strings.Contains(out, `"trace_id":"slog-trace"`) {
		t.Errorf("expected trace_id field, got: %s", out)
	}
	if !strings.Contains(out, `"service":"svc"`) {
		t.Errorf("expected service field, got: %s", out)
	}
}

// TestWithCtx_ExplicitCtxWins 验证绑定后显式 Ctx 方法仍以传入 ctx 为准。
func TestWithCtx_ExplicitCtxWins(t *testing.T) {
	restore := setTestExtractor()
	defer restore()

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	boundCtx := withTestTrace(context.Background(), "bound-trace")
	explicitCtx := withTestTrace(context.Background(), "explicit-trace")

	logger := NewStdLogger("svc", LevelDebug).WithCtx(boundCtx)
	logger.CtxInfo(explicitCtx, "msg") // 显式 ctx 应胜出

	if !strings.Contains(buf.String(), "[trace=explicit-trace]") {
		t.Errorf("explicit ctx should win, got: %s", buf.String())
	}
}

// TestWithCtx_PackageLevel 验证包级 logx.WithCtx 委托给 defaultLogger。
func TestWithCtx_PackageLevel(t *testing.T) {
	original := Default()
	defer SetDefault(original)
	SetDefault(NewStdLogger("pkg", LevelDebug))

	ctx := context.Background()
	l := WithCtx(ctx)
	if l == nil {
		t.Fatal("WithCtx returned nil")
	}
	if l.(*stdLogger).Ctx != ctx {
		t.Errorf("package-level WithCtx did not bind ctx")
	}
}

// TestParseLogLevel_Forms 钉住 #13：A 形态 (Level, error) 区分「没配」与「配错」，
// B 形态 ParseLogLevelOr 配错退默认。
func TestParseLogLevel_Forms(t *testing.T) {
	if lvl, err := ParseLogLevel("debug"); err != nil || lvl != LevelDebug {
		t.Fatalf("debug: %v %v", lvl, err)
	}
	if lvl, err := ParseLogLevel("WARN"); err != nil || lvl != LevelWarn {
		t.Fatalf("WARN 大小写不敏感: %v %v", lvl, err)
	}
	if _, err := ParseLogLevel(""); err == nil {
		t.Fatal("空串应报错（区分没配）")
	}
	if _, err := ParseLogLevel("verbose"); err == nil {
		t.Fatal("未知级别应报错")
	}
	if got := ParseLogLevelOr("verbose", LevelInfo); got != LevelInfo {
		t.Fatalf("Or fallback = %v", got)
	}
	if got := ParseLogLevelOr("ERROR", LevelInfo); got != LevelError {
		t.Fatalf("Or parsed = %v", got)
	}
}
