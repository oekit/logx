package logx

import (
	"context"
	"os"
	"testing"
)

// 本文件承载内置 JSON 热路径的基准（#194/#195 门禁：改动前后对照，不得
// 回退 #170 的分配成果）。基线与修复分支跑同一 workload，对照 ns/op 与
// allocs/op——benchstat 友好的成对命名（前缀一致）。

// benchLogger 构造标准 workload：2 持久属性 + 4 kv（string/int 标量为主，
// 覆盖快路径），带 service 与 trace 提取面。workload 在基线与修复分支间
// 保持逐字一致，基准差异只反映实现变化。
func benchLogger() Logger {
	return NewSlogLogger("svc", WithLevel(LevelInfo)).
		WithAttrs(Attr{Key: "request_id", Value: "req-12345678"}, Attr{Key: "region", Value: "cn-north-1"})
}

func benchCtx() context.Context { return context.Background() }

// discardStdout 把 os.Stdout 重定向到 io.Discard（基准不读输出内容——读会
// 拖慢循环；丢弃仍是完整序列化+Write 工作量）。
func discardStdout(b *testing.B) (restore func()) {
	b.Helper()
	old := os.Stdout
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		b.Fatalf("open %s: %v", os.DevNull, err)
	}
	os.Stdout = devnull
	return func() {
		os.Stdout = old
		devnull.Close()
	}
}

// BenchmarkEmitBuiltinJSON_Typical：无组的典型业务行（快路径主体）。
func BenchmarkEmitBuiltinJSON_Typical(b *testing.B) {
	restore := discardStdout(b)
	defer restore()
	l := benchLogger()
	ctx := benchCtx()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.CtxInfo(ctx, "request done", "method", "GET", "path", "/api/v1/users", "status", 200, "cost_ms", 12)
	}
}

// BenchmarkEmitBuiltinJSON_Grouped：带组前缀的行（#192 组前缀施加面）。
func BenchmarkEmitBuiltinJSON_Grouped(b *testing.B) {
	restore := discardStdout(b)
	defer restore()
	l := NewSlogLogger("svc", WithLevel(LevelInfo)).
		WithGroup("http").
		WithAttrs(Attr{Key: "request_id", Value: "req-12345678"})
	ctx := benchCtx()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.CtxInfo(ctx, "request done", "method", "GET", "status", 200)
	}
}
