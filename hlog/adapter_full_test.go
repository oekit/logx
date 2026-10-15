// hlog 适配器全方法矩阵测试（Round-4 覆盖率补课）。
// 经 hlog.SetOutput 重定向默认 hlog 输出，断言每个方法都落到写入器。
package hlog

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/yi-nology/ekit-logx"
)

// TestHlogLogger_FullMethodMatrix 覆盖 logx.Logger 全接口 + 派生构建器 + 级别过滤。
func TestHlogLogger_FullMethodMatrix(t *testing.T) {
	var buf syncBuffer
	hlog.SetOutput(&buf)
	t.Cleanup(func() { hlog.SetLogger(hlog.DefaultLogger()) })

	l := NewHlogLogger("svc-hlog")
	l.SetLevel(logx.LevelDebug) // 默认 Info，Debug 断言需显式降级

	// 结构化 + printf + ctx 三组
	l.Debug("d-kv", "k", "v")
	l.Info("i-kv")
	l.Warn("w-kv")
	l.Error("e-kv")
	l.Debugf("d-%s", "fmt")
	l.Infof("i-%s", "fmt")
	l.Warnf("w-%s", "fmt")
	l.Errorf("e-%s", "fmt")
	ctx := context.Background()
	l.CtxDebug(ctx, "cd")
	l.CtxInfo(ctx, "ci")
	l.CtxWarn(ctx, "cw")
	l.CtxError(ctx, "ce")
	l.CtxDebugf(ctx, "cd-%s", "fmt")
	l.CtxInfof(ctx, "ci-%s", "fmt")
	l.CtxWarnf(ctx, "cw-%s", "fmt")
	l.CtxErrorf(ctx, "ce-%s", "fmt")

	out := buf.String()
	for _, want := range []string{
		"d-kv", "i-kv", "w-kv", "e-kv",
		"d-fmt", "i-fmt", "w-fmt", "e-fmt",
		"cd", "ci", "cw", "ce",
		"cd-fmt", "ci-fmt", "cw-fmt", "ce-fmt",
		"svc-hlog",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("hlog 输出缺 %q\n--- got ---\n%s", want, out)
		}
	}

	// 派生构建器保持类型与属性
	sub := l.WithService("svc-2").WithAttrs(logx.Attr{Key: "tenant", Value: "acme"}).
		WithGroup("grp").WithCtx(ctx)
	sub.Info("via-derived")
	if !strings.Contains(buf.String(), "via-derived") {
		t.Error("派生 logger 未输出")
	}
	if sub2 := l.WithCtx(ctx); sub2 == nil {
		t.Error("WithCtx 返回 nil")
	}

	// 级别过滤：Error 级别下 Debug 应被吞掉
	var before int
	l.SetLevel(logx.LevelError)
	before = buf.Len()
	l.Debug("should-be-suppressed")
	if buf.Len() != before {
		t.Error("Error 级别下 Debug 不应输出")
	}
	l.Error("still-passes")
	if !strings.Contains(buf.String(), "still-passes") {
		t.Error("Error 级别下 Error 应输出")
	}
}

// TestHlogLogger_Context_TraceExtraction 覆盖 Ctx 路径与 EffectiveCtx 分支。
func TestHlogLogger_Context_TraceExtraction(t *testing.T) {
	var buf syncBuffer
	hlog.SetOutput(&buf)
	t.Cleanup(func() { hlog.SetLogger(hlog.DefaultLogger()) })

	l := NewHlogLogger("svc-ctx").WithCtx(context.Background())
	l.Info("bound-ctx-info")
	if !strings.Contains(buf.String(), "bound-ctx-info") {
		t.Errorf("绑定 ctx 后未输出: %q", buf.String())
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Len()
}
