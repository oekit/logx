package logx

import (
	"context"
	"log/slog"
)

// SlogHandler 把 logx.Logger 暴露为 slog.Handler——与 slog.go 中
// slog 后端（slog.Handler → logx 输出）方向相反的桥：让期望 *slog.Logger
// 的三方库直接消费 logx 生态（含 Delegating/过滤/子 logger 能力）。
//
// 记录经 logx 的 Ctx* 方法下发（保留 ctx 以便 trace_id 注入）；级别映射
// slog ↔ logx 按名称对应（logx.Level 是 iota int，slog 是 -4..4，不可数值直转）；
// WithGroup 实现为键前缀（"group.key"）。
func SlogHandler(l Logger) slog.Handler {
	if l == nil {
		panic("logx: SlogHandler called with nil logger")
	}
	return &slogxHandler{l: l}
}

type slogxHandler struct {
	l     Logger
	attrs []slog.Attr // WithAttrs 累积的前缀属性
	group string      // WithGroup 累积的键前缀（不含尾部点）
}

func (h *slogxHandler) Enabled(_ context.Context, level slog.Level) bool {
	return Enabled(h.l, logxLevel(level))
}

func (h *slogxHandler) Handle(ctx context.Context, r slog.Record) error {
	kvs := make([]any, 0, (len(h.attrs)+r.NumAttrs())*2)
	prefix := ""
	if h.group != "" {
		prefix = h.group + "."
	}
	appendAttr := func(a slog.Attr) {
		if a.Equal(slog.Attr{}) {
			return
		}
		kvs = append(kvs, prefix+a.Key, a.Value.Any())
	}
	for _, a := range h.attrs {
		appendAttr(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		appendAttr(a)
		return true
	})

	switch r.Level {
	case slog.LevelDebug:
		h.l.CtxDebug(ctx, r.Message, kvs...)
	case slog.LevelWarn:
		h.l.CtxWarn(ctx, r.Message, kvs...)
	case slog.LevelError:
		h.l.CtxError(ctx, r.Message, kvs...)
	default:
		h.l.CtxInfo(ctx, r.Message, kvs...)
	}
	return nil
}

func (h *slogxHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := &slogxHandler{l: h.l, group: h.group}
	next.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return next
}

func (h *slogxHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	next := &slogxHandler{l: h.l, attrs: h.attrs}
	if h.group != "" {
		next.group = h.group + "." + name
	} else {
		next.group = name
	}
	return next
}

// logxLevel 将 slog.Level 映射为 logx.Level（名称对应）。
func logxLevel(level slog.Level) Level {
	switch level {
	case slog.LevelDebug:
		return LevelDebug
	case slog.LevelWarn:
		return LevelWarn
	case slog.LevelError:
		return LevelError
	default:
		return LevelInfo
	}
}

// Slogger 返回以 l 为后端的 *slog.Logger。
func Slogger(l Logger) *slog.Logger {
	return slog.New(SlogHandler(l))
}
