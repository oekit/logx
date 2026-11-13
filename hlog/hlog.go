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
	"sync"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/oekit/logx"
)

// hlogMu 串行化 hlog 进程全局级别（hertz defaultLogger.level，普通 int
// 字段，hertz 自述 not concurrent-safe）的写与读（#96）：「一个 goroutine
// 运行期调级 + 另一个 goroutine 打日志」是生产常态，此前在 -race 下必报
// data race，与 std/slog 后端（BaseLogger.Mu 保护）的并发安全口径分裂。
//
// 必须是**包级**锁：全局级别进程内唯一，With* 派生的兄弟实例若各持一把
// per-instance 锁则互不互斥（假修复）；读锁允许并发日志互不阻塞，写锁
// （SetLevel/initHlogLevel）只在换档瞬间排他。持锁区间只覆盖 hlog 调用
// （内部走 hertz 默认输出路径），不回调 logx 自身代码，无重入死锁面。
var hlogMu sync.RWMutex

// hlogLogger 是基于 Hertz hlog 的 Logger 实现。
type hlogLogger struct {
	logx.BaseLogger
	// attrMask 是过滤层下沉的持久属性脱敏键集（#194，logx.AttrKeyMasker）；
	// nil 表示无脱敏。map 由 WithFilter 构造后只读共享，withBase 派生原样
	// 携带（不拷贝）。
	attrMask map[string]struct{}
}

// HlogOption 是 hlog 后端的函数选项（#167，与 SlogOption/StdOption 同形）。
type HlogOption func(*hlogLogger)

// WithHlogLevel 显式指定日志级别，优先级高于 LOG_LEVEL 环境变量。
// 构造后仍可 SetLevel 运行期调级（会同步 hlog 进程全局级别）。
func WithHlogLevel(level logx.Level) HlogOption {
	return func(l *hlogLogger) { l.Level.Store(level) }
}

// NewHlogLogger 创建带 service name 的 hlog Logger。
// 实例级别读 LOG_LEVEL 环境变量（#66，缺省 LevelInfo）——此前实例级别硬编码
// LevelInfo：LOG_LEVEL=DEBUG 下 Debug 被实例 ShouldLog 拦截、=OFF 下 Info 照打，
// README/doc/AGENTS 三处「LOG_LEVEL 统一控制所有后端」的承诺双双落空。
//
// #167：需要显式级别参数时用 NewHlogLoggerWith(service, WithHlogLevel(...))
// ——三个后端构造器自此都有函数选项形态。
func NewHlogLogger(service string) logx.Logger {
	return NewHlogLoggerWith(service)
}

// NewHlogLoggerWith 以函数选项创建 hlog 后端（#167，与 NewSlogLogger 同形）。
//
// 级别优先级：WithHlogLevel 显式选项 > LOG_LEVEL 环境变量 > 默认 LevelInfo
// （三后端文档口径一致）。
func NewHlogLoggerWith(service string, opts ...HlogOption) logx.Logger {
	l := &hlogLogger{BaseLogger: logx.NewBaseLogger(service,
		logx.ParseLogLevelOr(os.Getenv("LOG_LEVEL"), logx.LevelInfo))}
	for _, opt := range opts {
		if opt != nil {
			opt(l)
		}
	}
	l.initHlogLevel()
	return l
}

// IsHlogLogger 实现 logx.HlogBacked 标记接口，供 IsHlogBacked 运行时检查。
func (l *hlogLogger) IsHlogLogger() {}

// toHlogLevel 将 logx.Level 映射为 hlog.Level（#161：initHlogLevel/SetLevel
// 此前各持一份逐字相同的 switch，收敛为共用一份）。
// hlog 无 OFF 档：LevelOff 落到 Error——比 OFF 更严的全局档不存在，由实例
// ShouldLog 兜底拦截（#66/#102）。
func toHlogLevel(level logx.Level) hlog.Level {
	switch level {
	case logx.LevelDebug:
		return hlog.LevelDebug
	case logx.LevelWarn:
		return hlog.LevelWarn
	case logx.LevelError, logx.LevelOff:
		return hlog.LevelError
	default:
		return hlog.LevelInfo
	}
}

// initHlogLevel 根据实例级别设置 hlog 进程全局级别（hlog 无实例级级别）。
// 写全局级别持写锁（#96）：构造新实例可能与其他 hlog 实例的并发日志竞态。
func (l *hlogLogger) initHlogLevel() {
	hlogMu.Lock()
	defer hlogMu.Unlock()
	hlog.SetLevel(toHlogLevel(l.Level.Load()))
}

func (l *hlogLogger) withBase(apply func(cp *logx.BaseLogger)) logx.Logger {
	// #164：Snapshot + apply + 逐字段重建上提到 logx.BaseLogger.Derive
	// #194：attrMask 只读共享（构造后不可变），派生原样携带
	return &hlogLogger{BaseLogger: l.Derive(apply), attrMask: l.attrMask}
}

// WithAttrKeyMask 实现 logx.AttrKeyMasker（#194）：把过滤层下沉的键集带进
// 自身与后续派生，在渲染持久 Attrs 的全部路径（结构化与 printf）对命中键
// 脱敏——FilterKey 对 WithFilter 之前 base 已挂的属性自此生效。键匹配口径
// 与根包 matchMaskKey 一致（大小写不敏感，#85）；键集并入已有键集（嵌套
// 包裹叠加，与根包三后端同口径）；keys 只读共享。
func (l *hlogLogger) WithAttrKeyMask(keys map[string]struct{}) logx.Logger {
	c := l.withBase(func(cp *logx.BaseLogger) {}).(*hlogLogger)
	c.attrMask = mergeMaskKeys(l.attrMask, keys)
	return c
}

// mergeMaskKeys 并入脱敏键集（#194，与根包同源复制——hlog 是子包，根包该
// 判定未导出；语义变更需两处同步）。
func mergeMaskKeys(base, keys map[string]struct{}) map[string]struct{} {
	if len(keys) == 0 {
		return base
	}
	if len(base) == 0 {
		return keys
	}
	merged := make(map[string]struct{}, len(base)+len(keys))
	for k := range base {
		merged[k] = struct{}{}
	}
	for k := range keys {
		merged[k] = struct{}{}
	}
	return merged
}

// matchMaskKey 判定键名是否命中脱敏键集（#85 口径，与根包同源复制——
// hlog 是子包，根包该判定未导出；语义变更需两处同步）。
func matchMaskKey(keys map[string]struct{}, key string) bool {
	for k := range keys {
		if strings.EqualFold(k, key) {
			return true
		}
	}
	return false
}

// maskAttrPrefix 返回脱敏后的持久属性前缀（#194）：键命中键集的值替换为
// "***"，未命中零拷贝。
func maskAttrPrefix(keys map[string]struct{}, attrs []logx.Attr) string {
	hit := false
	for _, a := range attrs {
		if matchMaskKey(keys, a.Key) {
			hit = true
			break
		}
	}
	if !hit {
		return logx.FormatAttrs(attrs...)
	}
	masked := make([]logx.Attr, len(attrs))
	copy(masked, attrs)
	for i := range masked {
		if matchMaskKey(keys, masked[i].Key) {
			masked[i].Value = "***"
		}
	}
	return logx.FormatAttrs(masked...)
}

// maskKvs 返回键命中脱敏键集的 kv 拷贝（值替换为 "***"），未命中原样返回
// （#194：调用 kv 与持久属性共用同一脱敏口径）。
func maskKvs(keys map[string]struct{}, kvs []any) []any {
	hit := false
	for i := 0; i+1 < len(kvs); i += 2 {
		if matchMaskKey(keys, logx.KeyString(kvs[i])) {
			hit = true
			break
		}
	}
	if !hit {
		return kvs
	}
	out := make([]any, len(kvs))
	copy(out, kvs)
	for i := 0; i+1 < len(out); i += 2 {
		if matchMaskKey(keys, logx.KeyString(out[i])) {
			out[i+1] = "***"
		}
	}
	return out
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
// 并发安全（#96）：写全局级别持包级写锁（hlogMu），与全部 Ctx* 打日志
// 路径的读锁互斥——「运行期调级 + 并发打日志」此前在 -race 下必报
// data race（hertz defaultLogger.level 是普通 int）。
// ⚠️ hlog 全局级别进程内唯一：同进程多个 hlog 后端时最后 SetLevel 者生效
// （hlog 设计如此，无法 per-logger）。
func (l *hlogLogger) SetLevel(level logx.Level) {
	l.BaseLogger.SetLevel(level)
	hlogMu.Lock()
	defer hlogMu.Unlock()
	hlog.SetLevel(toHlogLevel(level))
}

// attrPrefix 将持久化属性格式化为可读前缀（#194：下沉键集非空时对命中键
// 脱敏）。
func (l *hlogLogger) attrPrefix() string {
	if len(l.Attrs) == 0 {
		return ""
	}
	if l.attrMask != nil {
		return maskAttrPrefix(l.attrMask, l.Attrs)
	}
	return logx.FormatAttrs(l.Attrs...)
}

// formatKV 合并持久化属性与本次调用的 kv 参数（#194：下沉键集非空时调用
// kv 一并脱敏）。
func (l *hlogLogger) formatKV(keysAndValues ...any) string {
	if l.attrMask != nil && len(keysAndValues) > 0 {
		keysAndValues = maskKvs(l.attrMask, keysAndValues)
	}
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

// emitf 是 8 个 Ctx* 方法收敛后的唯一公共方法体（#161）：ShouldLog 判级、
// nil ctx 兜底（#97，经根包导出的 logx.CtxOrBackground 复用，#168 删除
// 本地 orBackground 副本）、hlog 全局级别读锁（#96）、trace 提取——这段
// 前置此前逐字重复 8 次，#96/#97 两次修复都被迫改 8 处，此后同类修复
// （如 trace 提取口径调整）只改这里。
//
// render 在持锁窗口内完成最终格式化并调用对应级别的 hlog 目标函数；闭包
// 把消息格式化推迟到 ShouldLog 判级之后，被过滤的调用不付任何格式化成本。
func (l *hlogLogger) emitf(ctx context.Context, level logx.Level, render func(ctx context.Context, traceID string)) {
	if !l.ShouldLog(level) {
		return
	}
	ctx = logx.CtxOrBackground(ctx) // #97：nil ctx 统一兜底
	hlogMu.RLock()
	defer hlogMu.RUnlock() // #96：读 hertz 全局级别与 SetLevel 的写互斥
	render(ctx, logx.ExtractTrace(ctx))
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
	l.emitf(ctx, logx.LevelDebug, func(ctx context.Context, traceID string) {
		kv := l.formatKV(keysAndValues...)
		if traceID != "" {
			hlog.CtxDebugf(ctx, "[trace=%s] %s %s %s", traceID, l.Prefix(), msg, kv)
			return
		}
		hlog.CtxDebugf(ctx, "%s %s %s", l.Prefix(), msg, kv)
	})
}

func (l *hlogLogger) CtxInfo(ctx context.Context, msg string, keysAndValues ...any) {
	l.emitf(ctx, logx.LevelInfo, func(ctx context.Context, traceID string) {
		kv := l.formatKV(keysAndValues...)
		if traceID != "" {
			hlog.CtxInfof(ctx, "[trace=%s] %s %s %s", traceID, l.Prefix(), msg, kv)
			return
		}
		hlog.CtxInfof(ctx, "%s %s %s", l.Prefix(), msg, kv)
	})
}

func (l *hlogLogger) CtxWarn(ctx context.Context, msg string, keysAndValues ...any) {
	l.emitf(ctx, logx.LevelWarn, func(ctx context.Context, traceID string) {
		kv := l.formatKV(keysAndValues...)
		if traceID != "" {
			hlog.CtxWarnf(ctx, "[trace=%s] %s %s %s", traceID, l.Prefix(), msg, kv)
			return
		}
		hlog.CtxWarnf(ctx, "%s %s %s", l.Prefix(), msg, kv)
	})
}

func (l *hlogLogger) CtxError(ctx context.Context, msg string, keysAndValues ...any) {
	l.emitf(ctx, logx.LevelError, func(ctx context.Context, traceID string) {
		kv := l.formatKV(keysAndValues...)
		if traceID != "" {
			hlog.CtxErrorf(ctx, "[trace=%s] %s %s %s", traceID, l.Prefix(), msg, kv)
			return
		}
		hlog.CtxErrorf(ctx, "%s %s %s", l.Prefix(), msg, kv)
	})
}

// 带 Context 的 printf 风格方法
//
// #86：printf 路径同样携带 WithAttrs 持久属性——此前 Ctx*f 不合并 attrPrefix，
// 同一 logger 上 Info 带 request_id、Infof 不带，字段面按方法族分裂，
// 违背 WithAttrs「拼入消息前缀」的 godoc 承诺。与 Ctx* 结构化路径
// （formatKV）口径一致：属性拼在消息之后。

func (l *hlogLogger) CtxDebugf(ctx context.Context, format string, args ...any) {
	l.emitf(ctx, logx.LevelDebug, func(ctx context.Context, traceID string) {
		msg := logx.FormatWithTraceID(traceID, format, args...)
		if pfx := l.attrPrefix(); pfx != "" {
			hlog.CtxDebugf(ctx, "%s %s %s", l.Prefix(), msg, pfx)
			return
		}
		hlog.CtxDebugf(ctx, "%s %s", l.Prefix(), msg)
	})
}

func (l *hlogLogger) CtxInfof(ctx context.Context, format string, args ...any) {
	l.emitf(ctx, logx.LevelInfo, func(ctx context.Context, traceID string) {
		msg := logx.FormatWithTraceID(traceID, format, args...)
		if pfx := l.attrPrefix(); pfx != "" {
			hlog.CtxInfof(ctx, "%s %s %s", l.Prefix(), msg, pfx)
			return
		}
		hlog.CtxInfof(ctx, "%s %s", l.Prefix(), msg)
	})
}

func (l *hlogLogger) CtxWarnf(ctx context.Context, format string, args ...any) {
	l.emitf(ctx, logx.LevelWarn, func(ctx context.Context, traceID string) {
		msg := logx.FormatWithTraceID(traceID, format, args...)
		if pfx := l.attrPrefix(); pfx != "" {
			hlog.CtxWarnf(ctx, "%s %s %s", l.Prefix(), msg, pfx)
			return
		}
		hlog.CtxWarnf(ctx, "%s %s", l.Prefix(), msg)
	})
}

func (l *hlogLogger) CtxErrorf(ctx context.Context, format string, args ...any) {
	l.emitf(ctx, logx.LevelError, func(ctx context.Context, traceID string) {
		msg := logx.FormatWithTraceID(traceID, format, args...)
		if pfx := l.attrPrefix(); pfx != "" {
			hlog.CtxErrorf(ctx, "%s %s %s", l.Prefix(), msg, pfx)
			return
		}
		hlog.CtxErrorf(ctx, "%s %s", l.Prefix(), msg)
	})
}
