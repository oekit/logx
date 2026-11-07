package logx

import (
	"context"
	"testing"
)

// nilCtx 故意保持 nil 值：模拟错误处理路径上的 nil ctx（staticcheck SA1012
// 不接受字面 nil 直传，经变量表达同一语义）。
var nilCtx context.Context

// TestExtractTrace_NilCtxNoPanic（#97）：注册提取器后 nil ctx 不再 nil 解引用
// panic——ExtractTrace 返回空串，各后端/包装器的 Ctx* 全路径安全返回。
// 此前触发条件是「已注册提取器 + nil ctx」，错误处理路径上极常见：
//
//	ctx, err := doWork()
//	if err != nil {
//	    logx.CtxError(ctx, "failed") // ctx 此时可能是 nil
//	}
func TestExtractTrace_NilCtxNoPanic(t *testing.T) {
	restore := setTestExtractor()
	defer restore()

	if got := ExtractTrace(nilCtx); got != "" {
		t.Errorf("ExtractTrace(nil) = %q, want 空串", got)
	}

	loggers := map[string]Logger{
		"std":        NewStdLogger("svc"),
		"slog":       NewSlogLogger("svc"),
		"delegating": Delegating("comp"),
		"filter":     WithFilter(NewStdLogger("svc"), FilterKey("password")),
		"audit":      NewAuditLogger("svc"),
	}
	for name, l := range loggers {
		l.CtxInfo(nilCtx, "nil ctx must not panic", "backend", name)
		l.CtxErrorf(nilCtx, "nil ctx %s must not panic", name)
	}

	// 注入 Handler 路径同样不 panic，且 Handle 收到非 nil ctx
	h := newCaptureHandler()
	NewSlogLogger("svc", WithHandler(h)).CtxInfo(nilCtx, "handler path nil ctx")
	if len(h.snapshot()) != 1 {
		t.Errorf("handler path: want 1 record, got %d", len(h.snapshot()))
	}
}
