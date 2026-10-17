package logx

import (
	"context"
	"log/slog"
)

// TraceHandlerOption 配置 TraceHandler。
type TraceHandlerOption func(*traceHandlerConfig)

type traceHandlerConfig struct {
	key string
}

// WithTraceKey 自定义注入的 trace 字段名（默认 "trace_id"，与
// tracing/traceid 的 ctx 键值约定一致）。
func WithTraceKey(key string) TraceHandlerOption {
	return func(c *traceHandlerConfig) { c.key = key }
}

// TraceHandler 给任意 slog.Handler 包一层：Handle 时从 ctx 提取 trace_id
// （经 ExtractTrace——SetTraceExtractor 注册的提取器，如 tracing/traceid）
// 注入为结构化字段（#12）。
//
// 边界（与消费方实测实现一致）：
//   - ctx 里没有 trace_id 时不注入（后台任务日志保持原样，不加 trace_id=""）；
//   - 记录里已有同名字段时不重复注入；
//   - WithAttrs/WithGroup 转发给 inner 并返回**包装后**的 handler——否则
//     logger.With(...) 之后 trace 注入会断。
//
// 用法（裸 slog 项目接 trace 串联）：
//
//	logger := slog.New(logx.TraceHandler(slog.NewJSONHandler(os.Stdout, nil)))
func TraceHandler(inner slog.Handler, opts ...TraceHandlerOption) slog.Handler {
	if inner == nil {
		panic("logx: TraceHandler called with nil handler")
	}
	cfg := &traceHandlerConfig{key: "trace_id"}
	for _, opt := range opts {
		opt(cfg)
	}
	return &traceHandler{inner: inner, key: cfg.key}
}

type traceHandler struct {
	inner slog.Handler
	key   string
}

func (h *traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *traceHandler) Handle(ctx context.Context, r slog.Record) error {
	traceID := ExtractTrace(ctx)
	if traceID != "" && !hasKey(r, h.key) {
		r.AddAttrs(slog.String(h.key, traceID))
	}
	return h.inner.Handle(ctx, r)
}

// WithAttrs/WithGroup 转发 inner 并保持包装——否则派生 handler 丢失注入能力。
func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceHandler{inner: h.inner.WithAttrs(attrs), key: h.key}
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{inner: h.inner.WithGroup(name), key: h.key}
}

// hasKey 检查记录里是否已显式携带同名字段（有则不重复注入）。
func hasKey(r slog.Record, key string) bool {
	found := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			found = true
			return false
		}
		return true
	})
	return found
}
