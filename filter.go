package logx

import (
	"context"
	"fmt"
)

// FilterOption 配置 WithFilter 的过滤行为。
type FilterOption func(*filterConfig)

type filterConfig struct {
	// keys 中命中的 kv 键，值脱敏为 "***"。
	keys map[string]struct{}
	// drop 返回 true 时整条记录被丢弃（在脱敏之后评估）。
	drop func(msg string, kvs []any) bool
}

// FilterKey 把命中 key 的 kv 值替换为 "***"（精确匹配键名，含点路径字面量，
// 如 "user.password"）。
func FilterKey(keys ...string) FilterOption {
	return func(c *filterConfig) {
		if c.keys == nil {
			c.keys = make(map[string]struct{}, len(keys))
		}
		for _, k := range keys {
			c.keys[k] = struct{}{}
		}
	}
}

// FilterFunc 命中即丢弃整条记录（在脱敏之后评估）。
func FilterFunc(fn func(msg string, kvs []any) bool) FilterOption {
	return func(c *filterConfig) { c.drop = fn }
}

// WithFilter 返回带脱敏/丢弃能力的子 logger。
//
// 过滤在 CoreLogger 四方法与 Ctx*/Fmt* 全路径生效：kv 值按 FilterKey 脱敏、
// 记录按 FilterFunc 丢弃。WithAttrs/WithGroup/WithCtx 派生的子 logger 保持
// 相同过滤规则。典型用法（防止敏感信息进日志）：
//
//	l := logx.WithFilter(logx.Default(), logx.FilterKey("password", "token"))
func WithFilter(l Logger, opts ...FilterOption) Logger {
	if l == nil {
		panic("logx: WithFilter called with nil logger")
	}
	cfg := &filterConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	return &filterLogger{base: l, cfg: cfg}
}

type filterLogger struct {
	base Logger
	cfg  *filterConfig
}

// filterKvs 返回脱敏后的 kv 切片（不入参原切片）。
func (f *filterLogger) filterKvs(kvs []any) []any {
	if len(f.cfg.keys) == 0 || len(kvs) == 0 {
		return kvs
	}
	out := make([]any, len(kvs))
	copy(out, kvs)
	for i := 0; i+1 < len(out); i += 2 {
		if k, ok := out[i].(string); ok {
			if _, hit := f.cfg.keys[k]; hit {
				out[i+1] = "***"
			}
		}
	}
	return out
}

// dropped 报告记录是否应被丢弃。
func (f *filterLogger) dropped(msg string, kvs []any) bool {
	return f.cfg.drop != nil && f.cfg.drop(msg, kvs)
}

func (f *filterLogger) Debug(msg string, kvs ...any) {
	kvs = f.filterKvs(kvs)
	if f.dropped(msg, kvs) {
		return
	}
	f.base.Debug(msg, kvs...)
}

func (f *filterLogger) Info(msg string, kvs ...any) {
	kvs = f.filterKvs(kvs)
	if f.dropped(msg, kvs) {
		return
	}
	f.base.Info(msg, kvs...)
}

func (f *filterLogger) Warn(msg string, kvs ...any) {
	kvs = f.filterKvs(kvs)
	if f.dropped(msg, kvs) {
		return
	}
	f.base.Warn(msg, kvs...)
}

func (f *filterLogger) Error(msg string, kvs ...any) {
	kvs = f.filterKvs(kvs)
	if f.dropped(msg, kvs) {
		return
	}
	f.base.Error(msg, kvs...)
}

func (f *filterLogger) Debugf(format string, args ...any) {
	if f.dropped(fmt.Sprintf(format, args...), nil) {
		return
	}
	f.base.Debugf(format, args...)
}

func (f *filterLogger) Infof(format string, args ...any) {
	if f.dropped(fmt.Sprintf(format, args...), nil) {
		return
	}
	f.base.Infof(format, args...)
}

func (f *filterLogger) Warnf(format string, args ...any) {
	if f.dropped(fmt.Sprintf(format, args...), nil) {
		return
	}
	f.base.Warnf(format, args...)
}

func (f *filterLogger) Errorf(format string, args ...any) {
	if f.dropped(fmt.Sprintf(format, args...), nil) {
		return
	}
	f.base.Errorf(format, args...)
}

func (f *filterLogger) CtxDebug(ctx context.Context, msg string, kvs ...any) {
	kvs = f.filterKvs(kvs)
	if f.dropped(msg, kvs) {
		return
	}
	f.base.CtxDebug(ctx, msg, kvs...)
}

func (f *filterLogger) CtxInfo(ctx context.Context, msg string, kvs ...any) {
	kvs = f.filterKvs(kvs)
	if f.dropped(msg, kvs) {
		return
	}
	f.base.CtxInfo(ctx, msg, kvs...)
}

func (f *filterLogger) CtxWarn(ctx context.Context, msg string, kvs ...any) {
	kvs = f.filterKvs(kvs)
	if f.dropped(msg, kvs) {
		return
	}
	f.base.CtxWarn(ctx, msg, kvs...)
}

func (f *filterLogger) CtxError(ctx context.Context, msg string, kvs ...any) {
	kvs = f.filterKvs(kvs)
	if f.dropped(msg, kvs) {
		return
	}
	f.base.CtxError(ctx, msg, kvs...)
}

func (f *filterLogger) CtxDebugf(ctx context.Context, format string, args ...any) {
	if f.dropped(fmt.Sprintf(format, args...), nil) {
		return
	}
	f.base.CtxDebugf(ctx, format, args...)
}

func (f *filterLogger) CtxInfof(ctx context.Context, format string, args ...any) {
	if f.dropped(fmt.Sprintf(format, args...), nil) {
		return
	}
	f.base.CtxInfof(ctx, format, args...)
}

func (f *filterLogger) CtxWarnf(ctx context.Context, format string, args ...any) {
	if f.dropped(fmt.Sprintf(format, args...), nil) {
		return
	}
	f.base.CtxWarnf(ctx, format, args...)
}

func (f *filterLogger) CtxErrorf(ctx context.Context, format string, args ...any) {
	if f.dropped(fmt.Sprintf(format, args...), nil) {
		return
	}
	f.base.CtxErrorf(ctx, format, args...)
}

// Builder 方法：派生子 logger 保持过滤规则。

func (f *filterLogger) WithCtx(ctx context.Context) Logger {
	return &filterLogger{base: f.base.WithCtx(ctx), cfg: f.cfg}
}

func (f *filterLogger) WithService(name string) Logger {
	return &filterLogger{base: f.base.WithService(name), cfg: f.cfg}
}

func (f *filterLogger) WithAttrs(attrs ...Attr) Logger {
	return &filterLogger{base: f.base.WithAttrs(f.maskAttrs(attrs)...), cfg: f.cfg}
}

// maskAttrs 把命中过滤键的属性值替换为 "***"（脱敏后再交给 base）。
//
// 不脱敏的话，WithAttrs 挂上去的属性会**原样**进日志：过滤只作用于「每次
// 调用传进来的 kv」，WithAttrs 挂的属性完全绕过。WithFilter 的 godoc 明确
// 承诺「WithAttrs/WithGroup/WithCtx 派生的子 logger 保持相同过滤规则」⇒
// 旧实现是**谎报**：敏感信息照常落盘（实测：WithFilter(base, FilterKey("password"))
// 之后 WithAttrs(Attr{Key:"password", Value:"hunter2"}) 会打出 password=hunter2）。
func (f *filterLogger) maskAttrs(attrs []Attr) []Attr {
	if len(f.cfg.keys) == 0 || len(attrs) == 0 {
		return attrs
	}
	out := make([]Attr, len(attrs))
	copy(out, attrs)
	for i := range out {
		if _, hit := f.cfg.keys[out[i].Key]; hit {
			out[i].Value = "***"
		}
	}
	return out
}

func (f *filterLogger) WithGroup(name string) Logger {
	return &filterLogger{base: f.base.WithGroup(name), cfg: f.cfg}
}

func (f *filterLogger) SetLevel(level Level) { f.base.SetLevel(level) }
