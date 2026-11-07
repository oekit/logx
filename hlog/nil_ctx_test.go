package hlog

import (
	"context"
	"testing"

	"github.com/oekit/logx"
)

type nilCtxTraceKey struct{}

// TestHlogLogger_NilCtxNoPanic（#97）：注册提取器后 Ctx* 传 nil ctx 不 panic。
// 此前 ExtractTrace 把 nil ctx 原样传进用户提取器，ctx.Value(...) 立即
// 空指针崩溃——错误处理路径上 ctx==nil 极常见，打日志本身把进程打死。
func TestHlogLogger_NilCtxNoPanic(t *testing.T) {
	logx.SetTraceExtractor(func(ctx context.Context) string {
		if v, ok := ctx.Value(nilCtxTraceKey{}).(string); ok {
			return v
		}
		return ""
	})
	t.Cleanup(func() { logx.SetTraceExtractor(nil) })

	l := NewHlogLogger("svc")
	l.CtxDebug(nil, "must not panic")
	l.CtxInfo(nil, "must not panic")
	l.CtxWarn(nil, "must not panic")
	l.CtxError(nil, "must not panic")
	l.CtxDebugf(nil, "must not panic %s", "fmt")
	l.CtxInfof(nil, "must not panic %s", "fmt")
	l.CtxWarnf(nil, "must not panic %s", "fmt")
	l.CtxErrorf(nil, "must not panic %s", "fmt")
}
