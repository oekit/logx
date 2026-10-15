package logx

import (
	"context"
	"fmt"
	"log"
	"time"
)

// stdLogger 是基于标准库 log 的 Logger 实现。
type stdLogger struct {
	BaseLogger
}

// NewStdLogger 创建标准库实现的 Logger。
// 默认级别为 LevelDebug。
func NewStdLogger(service string, level ...Level) Logger {
	l := &stdLogger{BaseLogger: BaseLogger{Service: service, Level: LevelDebug}}
	if len(level) > 0 {
		l.Level = level[0]
	}
	return l
}

func (l *stdLogger) withBase(apply func(cp *BaseLogger)) Logger {
	cp := l.Snapshot()
	apply(&cp)
	// 逐字段重建：直接 BaseLogger: cp 会连同内部 Mutex 一起拷贝（vet lock copy 报错）
	return &stdLogger{BaseLogger: BaseLogger{Service: cp.Service, Level: cp.Level, Attrs: cp.Attrs, Ctx: cp.Ctx}}
}

func (l *stdLogger) WithCtx(ctx context.Context) Logger {
	return l.withBase(func(cp *BaseLogger) { cp.Ctx = ctx })
}

func (l *stdLogger) WithService(name string) Logger {
	return l.withBase(func(cp *BaseLogger) { cp.Service = name })
}

// WithAttrs 返回携带固定属性的子 logger。
func (l *stdLogger) WithAttrs(attrs ...Attr) Logger {
	return l.withBase(func(cp *BaseLogger) { cp.Attrs = cp.MergeAttrs(attrs...) })
}

// WithGroup stdLogger 降级实现不支持分组。
func (l *stdLogger) WithGroup(name string) Logger {
	return l.withBase(func(cp *BaseLogger) {})
}

func (l *stdLogger) log(level Level, ctx context.Context, msg string, keysAndValues ...any) {
	if !l.ShouldLog(level) {
		return
	}
	ts := time.Now().Format("2006-01-02 15:04:05.000")
	levelStr := LevelString(level)

	traceID := ExtractTrace(ctx)
	prefix := fmt.Sprintf("%s [%s] %s", ts, levelStr, l.Prefix())
	if traceID != "" {
		prefix = fmt.Sprintf("%s [%s] [trace=%s] %s", ts, levelStr, traceID, l.Prefix())
	}

	kvStr := formatAttrsAndKV(l.Attrs, keysAndValues...)
	if kvStr != "" {
		log.Printf("%s %s %s", prefix, msg, kvStr)
	} else {
		log.Printf("%s %s", prefix, msg)
	}
}

// formatAttrsAndKV 合并持久化属性与本次调用的 kv 参数（与 hlog/slog 后端一致）。
func formatAttrsAndKV(attrs []Attr, keysAndValues ...any) string {
	attrStr := FormatAttrs(attrs...)
	kvStr := FormatKeysAndValues(keysAndValues...)
	if attrStr == "" {
		return kvStr
	}
	if kvStr == "" {
		return attrStr
	}
	return attrStr + " " + kvStr
}

func (l *stdLogger) logf(level Level, ctx context.Context, format string, args ...any) {
	if !l.ShouldLog(level) {
		return
	}
	ts := time.Now().Format("2006-01-02 15:04:05.000")
	levelStr := LevelString(level)

	traceID := ExtractTrace(ctx)
	prefix := fmt.Sprintf("%s [%s] %s", ts, levelStr, l.Prefix())
	if traceID != "" {
		prefix = fmt.Sprintf("%s [%s] [trace=%s] %s", ts, levelStr, traceID, l.Prefix())
	}

	msg := fmt.Sprintf(format, args...)
	log.Printf("%s %s", prefix, msg)
}

// 结构化风格方法（绑定 ctx 时自动提取 trace_id）

func (l *stdLogger) Debug(msg string, keysAndValues ...any) {
	l.log(LevelDebug, l.EffectiveCtx(), msg, keysAndValues...)
}

func (l *stdLogger) Info(msg string, keysAndValues ...any) {
	l.log(LevelInfo, l.EffectiveCtx(), msg, keysAndValues...)
}

func (l *stdLogger) Warn(msg string, keysAndValues ...any) {
	l.log(LevelWarn, l.EffectiveCtx(), msg, keysAndValues...)
}

func (l *stdLogger) Error(msg string, keysAndValues ...any) {
	l.log(LevelError, l.EffectiveCtx(), msg, keysAndValues...)
}

// printf 风格方法

func (l *stdLogger) Debugf(format string, args ...any) {
	l.logf(LevelDebug, l.EffectiveCtx(), format, args...)
}

func (l *stdLogger) Infof(format string, args ...any) {
	l.logf(LevelInfo, l.EffectiveCtx(), format, args...)
}

func (l *stdLogger) Warnf(format string, args ...any) {
	l.logf(LevelWarn, l.EffectiveCtx(), format, args...)
}

func (l *stdLogger) Errorf(format string, args ...any) {
	l.logf(LevelError, l.EffectiveCtx(), format, args...)
}

// 带 Context 的结构化风格方法

func (l *stdLogger) CtxDebug(ctx context.Context, msg string, keysAndValues ...any) {
	l.log(LevelDebug, ctx, msg, keysAndValues...)
}

func (l *stdLogger) CtxInfo(ctx context.Context, msg string, keysAndValues ...any) {
	l.log(LevelInfo, ctx, msg, keysAndValues...)
}

func (l *stdLogger) CtxWarn(ctx context.Context, msg string, keysAndValues ...any) {
	l.log(LevelWarn, ctx, msg, keysAndValues...)
}

func (l *stdLogger) CtxError(ctx context.Context, msg string, keysAndValues ...any) {
	l.log(LevelError, ctx, msg, keysAndValues...)
}

// 带 Context 的 printf 风格方法

func (l *stdLogger) CtxDebugf(ctx context.Context, format string, args ...any) {
	l.logf(LevelDebug, ctx, format, args...)
}

func (l *stdLogger) CtxInfof(ctx context.Context, format string, args ...any) {
	l.logf(LevelInfo, ctx, format, args...)
}

func (l *stdLogger) CtxWarnf(ctx context.Context, format string, args ...any) {
	l.logf(LevelWarn, ctx, format, args...)
}

func (l *stdLogger) CtxErrorf(ctx context.Context, format string, args ...any) {
	l.logf(LevelError, ctx, format, args...)
}
