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
//   - 记录或本 handler 经 WithAttrs 带入的字段里已有同名键时不重复注入
//     （#104：此前只查 Record——经 handler.With(...) 挂的属性进了 inner，
//     不在 Record 里，trace_id 被二次注入，输出重复键且取值依解析器而定；
//     #129：WithGroup 派生的子 handler 同样继承该判定，With+WithGroup
//     标准链不再重复注入）；
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
	// withKeys 是经本 handler WithAttrs 转发给 inner 的键集合（#104）：
	// 这些属性不在 Record 里，Handle 的「已有同名字段不重复注入」必须
	// 连同它们一起判定。nil 表示没有带入过属性（零值可用，避免每次
	// WithAttrs 白建 map）。
	withKeys map[string]struct{}
}

func (h *traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *traceHandler) Handle(ctx context.Context, r slog.Record) error {
	traceID := ExtractTrace(ctx)
	if traceID != "" && !hasKey(r, h.key) && !h.hasWithKey(h.key) {
		r.AddAttrs(slog.String(h.key, traceID))
	}
	return h.inner.Handle(ctx, r)
}

// hasWithKey 判定键是否已由 WithAttrs 带入 inner（#104）。
func (h *traceHandler) hasWithKey(key string) bool {
	_, ok := h.withKeys[key]
	return ok
}

// WithAttrs/WithGroup 转发 inner 并保持包装——否则派生 handler 丢失注入
// 能力；带入的键记入 withKeys 供 Handle 去重（#104），WithGroup 则原样
// 继承父集合（#129，组不新增键）。
func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	child := &traceHandler{inner: h.inner.WithAttrs(attrs), key: h.key}
	if len(h.withKeys) > 0 || len(attrs) > 0 {
		child.withKeys = make(map[string]struct{}, len(h.withKeys)+len(attrs))
		for k := range h.withKeys {
			child.withKeys[k] = struct{}{}
		}
		for _, a := range attrs {
			child.withKeys[a.Key] = struct{}{}
		}
	}
	return child
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	// withKeys 随链继承（#129）：「With(键).WithGroup(组)」是 slog 最常见的
	// 链式写法，此前子 handler 丢掉 withKeys（#104 的修复只落在 WithAttrs），
	// Handle 判重断链——ctx 提取值被二次注入且落进组，一条 JSON 记录出现
	// 两个 trace_id（显式值在顶层、ctx 值在组里，Loki/ES 对同名键取值依
	// 解析器而定）。组不新增键，共享父集合即可：withKeys 构造后只读，
	// 唯一写入方 WithAttrs 恒新建 map，共享无竞态。
	return &traceHandler{inner: h.inner.WithGroup(name), key: h.key, withKeys: h.withKeys}
}

// hasKey 检查记录里是否已显式携带同名字段（有则不重复注入）；
// WithAttrs 带入的键由 traceHandler.hasWithKey 一并判定（#104）。
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
