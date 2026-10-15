// Package hlog 提供基于 Hertz hlog 的 logx.Logger 实现。
//
// 此实现将结构化 kv 参数拼接为字符串（降级模式），
// 适合需要与 Hertz hlog 输出格式保持一致的场景。
//
// 用法:
//
//	import (
//	    "github.com/oekit/logx"
//	    logxhlog "github.com/oekit/logx/hlog"
//	)
//
//	logx.SetDefault(logxhlog.NewHlogLogger("my-service"))
//
// 注意: 将 hlog 后端的 Logger 传入 hertz.NewLogger 或 kitexlog.NewLogger 会导致无限递归。
// 这些场景应使用 logx.NewSlogLogger 或 logx.NewStdLogger。
package hlog

import (
	"context"
	"os"
	"strings"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/oekit/logx"
)

// hlogLogger 是基于 Hertz hlog 的 Logger 实现。
type hlogLogger struct {
	logx.BaseLogger
}

// NewHlogLogger 创建带 service name 的 hlog Logger。
func NewHlogLogger(service string) logx.Logger {
	l := &hlogLogger{BaseLogger: logx.NewBaseLogger(service, logx.LevelInfo)}
	l.initHlogLevel()
	return l
}

// IsHlogLogger 实现 logx.HlogBacked 标记接口，供 IsHlogBacked 运行时检查。
func (l *hlogLogger) IsHlogLogger() {}

// initHlogLevel 根据 LOG_LEVEL 环境变量设置 hlog 级别。
func (l *hlogLogger) initHlogLevel() {
	lvl := strings.ToUpper(os.Getenv("LOG_LEVEL"))
	switch lvl {
	case "DEBUG":
		hlog.SetLevel(hlog.LevelDebug)
	case "WARN":
		hlog.SetLevel(hlog.LevelWarn)
	case "ERROR":
		hlog.SetLevel(hlog.LevelError)
	default:
		hlog.SetLevel(hlog.LevelInfo)
	}
}

func (l *hlogLogger) withBase(apply func(cp *logx.BaseLogger)) logx.Logger {
	cp := l.Snapshot()
	apply(&cp)
	// 逐字段重建：直接 BaseLogger: cp 会连同内部 Mutex 一起拷贝
	return &hlogLogger{BaseLogger: logx.BaseLogger{Service: cp.Service, Level: cp.Level, Attrs: cp.Attrs, Ctx: cp.Ctx}}
}

func (l *hlogLogger) WithCtx(ctx context.Context) logx.Logger {
	return l.withBase(func(cp *logx.BaseLogger) { cp.Ctx = ctx })
}

func (l *hlogLogger) WithService(name string) logx.Logger {
	return l.withBase(func(cp *logx.BaseLogger) { cp.Service = name })
}

// WithAttrs 返回携带固定属性的子 logger（hlog 降级实现：拼入消息前缀）。
func (l *hlogLogger) WithAttrs(attrs ...logx.Attr) logx.Logger {
	return l.withBase(func(cp *logx.BaseLogger) { cp.Attrs = cp.MergeAttrs(attrs...) })
}

// WithGroup hlog 降级实现不支持分组，返回自身副本（保留语义不丢失属性）。
func (l *hlogLogger) WithGroup(name string) logx.Logger {
	return l.withBase(func(cp *logx.BaseLogger) {})
}

// SetLevel 设置实例级别，并同步 hlog 进程全局级别：hlog 输出受双层过滤
// （实例 ShouldLog + hlog 全局 level），只改实例级时向下调级（排障常规
// 操作）会被 hlog 全局 Info 静默吞掉。
// ⚠️ hlog 全局级别进程内唯一：同进程多个 hlog 后端时最后 SetLevel 者生效
// （hlog 设计如此，无法 per-logger）。
func (l *hlogLogger) SetLevel(level logx.Level) {
	l.BaseLogger.SetLevel(level)
	switch level {
	case logx.LevelDebug:
		hlog.SetLevel(hlog.LevelDebug)
	case logx.LevelWarn:
		hlog.SetLevel(hlog.LevelWarn)
	case logx.LevelError:
		hlog.SetLevel(hlog.LevelError)
	default:
		hlog.SetLevel(hlog.LevelInfo)
	}
}

// attrPrefix 将持久化属性格式化为可读前缀。
func (l *hlogLogger) attrPrefix() string {
	if len(l.Attrs) == 0 {
		return ""
	}
	return logx.FormatAttrs(l.Attrs...)
}

// formatKV 合并持久化属性与本次调用的 kv 参数。
func (l *hlogLogger) formatKV(keysAndValues ...any) string {
	attrStr := l.attrPrefix()
	kvStr := logx.FormatKeysAndValues(keysAndValues...)
	if attrStr == "" {
		return kvStr
	}
	if kvStr == "" {
		return attrStr
	}
	return attrStr + " " + kvStr
}

// 结构化风格方法（绑定 ctx 时自动提取 trace_id）

func (l *hlogLogger) Debug(msg string, keysAndValues ...any) {
	l.CtxDebug(l.EffectiveCtx(), msg, keysAndValues...)
}

func (l *hlogLogger) Info(msg string, keysAndValues ...any) {
	l.CtxInfo(l.EffectiveCtx(), msg, keysAndValues...)
}

func (l *hlogLogger) Warn(msg string, keysAndValues ...any) {
	l.CtxWarn(l.EffectiveCtx(), msg, keysAndValues...)
}

func (l *hlogLogger) Error(msg string, keysAndValues ...any) {
	l.CtxError(l.EffectiveCtx(), msg, keysAndValues...)
}

// printf 风格方法（绑定 ctx 时自动提取 trace_id）

func (l *hlogLogger) Debugf(format string, args ...any) {
	l.CtxDebugf(l.EffectiveCtx(), format, args...)
}

func (l *hlogLogger) Infof(format string, args ...any) {
	l.CtxInfof(l.EffectiveCtx(), format, args...)
}

func (l *hlogLogger) Warnf(format string, args ...any) {
	l.CtxWarnf(l.EffectiveCtx(), format, args...)
}

func (l *hlogLogger) Errorf(format string, args ...any) {
	l.CtxErrorf(l.EffectiveCtx(), format, args...)
}

// 带 Context 的结构化风格方法

func (l *hlogLogger) CtxDebug(ctx context.Context, msg string, keysAndValues ...any) {
	if !l.ShouldLog(logx.LevelDebug) {
		return
	}
	traceID := logx.ExtractTrace(ctx)
	if traceID != "" {
		hlog.CtxDebugf(ctx, "[trace=%s] %s %s %s", traceID, l.Prefix(), msg, l.formatKV(keysAndValues...))
	} else {
		hlog.CtxDebugf(ctx, "%s %s %s", l.Prefix(), msg, l.formatKV(keysAndValues...))
	}
}

func (l *hlogLogger) CtxInfo(ctx context.Context, msg string, keysAndValues ...any) {
	if !l.ShouldLog(logx.LevelInfo) {
		return
	}
	traceID := logx.ExtractTrace(ctx)
	if traceID != "" {
		hlog.CtxInfof(ctx, "[trace=%s] %s %s %s", traceID, l.Prefix(), msg, l.formatKV(keysAndValues...))
	} else {
		hlog.CtxInfof(ctx, "%s %s %s", l.Prefix(), msg, l.formatKV(keysAndValues...))
	}
}

func (l *hlogLogger) CtxWarn(ctx context.Context, msg string, keysAndValues ...any) {
	if !l.ShouldLog(logx.LevelWarn) {
		return
	}
	traceID := logx.ExtractTrace(ctx)
	if traceID != "" {
		hlog.CtxWarnf(ctx, "[trace=%s] %s %s %s", traceID, l.Prefix(), msg, l.formatKV(keysAndValues...))
	} else {
		hlog.CtxWarnf(ctx, "%s %s %s", l.Prefix(), msg, l.formatKV(keysAndValues...))
	}
}

func (l *hlogLogger) CtxError(ctx context.Context, msg string, keysAndValues ...any) {
	if !l.ShouldLog(logx.LevelError) {
		return
	}
	traceID := logx.ExtractTrace(ctx)
	if traceID != "" {
		hlog.CtxErrorf(ctx, "[trace=%s] %s %s %s", traceID, l.Prefix(), msg, l.formatKV(keysAndValues...))
	} else {
		hlog.CtxErrorf(ctx, "%s %s %s", l.Prefix(), msg, l.formatKV(keysAndValues...))
	}
}

// 带 Context 的 printf 风格方法

func (l *hlogLogger) CtxDebugf(ctx context.Context, format string, args ...any) {
	if !l.ShouldLog(logx.LevelDebug) {
		return
	}
	traceID := logx.ExtractTrace(ctx)
	msg := logx.FormatWithTraceID(traceID, format, args...)
	hlog.CtxDebugf(ctx, "%s %s", l.Prefix(), msg)
}

func (l *hlogLogger) CtxInfof(ctx context.Context, format string, args ...any) {
	if !l.ShouldLog(logx.LevelInfo) {
		return
	}
	traceID := logx.ExtractTrace(ctx)
	msg := logx.FormatWithTraceID(traceID, format, args...)
	hlog.CtxInfof(ctx, "%s %s", l.Prefix(), msg)
}

func (l *hlogLogger) CtxWarnf(ctx context.Context, format string, args ...any) {
	if !l.ShouldLog(logx.LevelWarn) {
		return
	}
	traceID := logx.ExtractTrace(ctx)
	msg := logx.FormatWithTraceID(traceID, format, args...)
	hlog.CtxWarnf(ctx, "%s %s", l.Prefix(), msg)
}

func (l *hlogLogger) CtxErrorf(ctx context.Context, format string, args ...any) {
	if !l.ShouldLog(logx.LevelError) {
		return
	}
	traceID := logx.ExtractTrace(ctx)
	msg := logx.FormatWithTraceID(traceID, format, args...)
	hlog.CtxErrorf(ctx, "%s %s", l.Prefix(), msg)
}
