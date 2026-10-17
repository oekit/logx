package logx

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
)

// CoreLogger 是最小日志接口，仅包含结构化风格的四个级别方法。
// 大多数库/中间件只需依赖此接口。
type CoreLogger interface {
	Debug(msg string, keysAndValues ...any)
	Info(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// FormattedLogger 扩展 CoreLogger，增加 printf 风格方法。
type FormattedLogger interface {
	CoreLogger
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

// ContextLogger 扩展 CoreLogger，增加带 Context 的方法（自动提取 trace_id）。
type ContextLogger interface {
	CoreLogger
	CtxDebug(ctx context.Context, msg string, keysAndValues ...any)
	CtxInfo(ctx context.Context, msg string, keysAndValues ...any)
	CtxWarn(ctx context.Context, msg string, keysAndValues ...any)
	CtxError(ctx context.Context, msg string, keysAndValues ...any)
	CtxDebugf(ctx context.Context, format string, args ...any)
	CtxInfof(ctx context.Context, format string, args ...any)
	CtxWarnf(ctx context.Context, format string, args ...any)
	CtxErrorf(ctx context.Context, format string, args ...any)
}

// LoggerBuilder 提供子 logger 构建能力。
type LoggerBuilder interface {
	WithCtx(ctx context.Context) Logger
	WithService(name string) Logger
	WithAttrs(attrs ...Attr) Logger
	WithGroup(name string) Logger
	SetLevel(level Level)
}

// Logger 是完整的日志接口，组合了 CoreLogger、ContextLogger 和 LoggerBuilder。
// 所有服务面向此接口编程，底层实现可替换。
//
// 扩展点：
//   - WithAttrs / WithGroup：链式附加结构化字段，用于构建带上下文的子 logger
//   - SetLevel：动态调整日志级别
//
// 这使得 logx 既能配合传统 hlog（字符串拼接降级），也能对接 slog（真正结构化输出）。
type Logger interface {
	FormattedLogger
	ContextLogger
	LoggerBuilder
}

// LevelEnabler 由支持级别判定的实现提供（LevelEnabler 接口）。
// 嵌入 BaseLogger 的后端（std/slog/hlog 等）自动实现；Delegating 包装器透传到
// 当前解析目标。消费方经类型断言可选使用——与生态「可选能力接口 + 类型断言」
// 哲学一致，不改动 CoreLogger 最小接口。
type LevelEnabler interface {
	Enabled(level Level) bool
}

// Enabled 判定 logger 是否会输出 level 级别日志。
// 优先走 logger 的 LevelEnabler；未实现时保守返回 true（与历史行为一致，绝不误吞日志）。
//
// 热路径守卫：在昂贵参数求值（大对象截断/序列化/拷贝）之前先判定，
// 被级别过滤掉的事件零成本跳过：
//
//	if !logx.Enabled(l, logx.LevelDebug) {
//	    return // 跳过 truncateForLog(body) 等昂贵求值
//	}
//	l.Debug("transport response body", "body", truncateForLog(body, 2048))
func Enabled(logger Logger, level Level) bool {
	if le, ok := logger.(LevelEnabler); ok {
		return le.Enabled(level)
	}
	return true
}

// Attr 是结构化日志的单个属性（键值对）。
type Attr struct {
	Key   string
	Value any
}

// 默认 logger 实例（基于标准库 log），使用 atomic.Pointer 保护并发安全。
var defaultLoggerV atomic.Pointer[Logger]

func init() {
	l := NewStdLogger("default")
	// 读取 LOG_LEVEL 环境变量，修复之前只设 hlog 级别的 split-brain
	l.SetLevel(ParseLogLevelOr(os.Getenv("LOG_LEVEL"), LevelInfo))
	defaultLoggerV.Store(&l)
}

// ParseLogLevel 解析级别字符串（DEBUG/INFO/WARN/ERROR，大小写不敏感）。
// 未识别的值返回错误（#13：不再返回 *Level 指针——调用方漏判空会在
// LOG_LEVEL 配错时得到远端解引用 panic）。空串按错误处理（区分「没配」），
// 「配错就退回默认」的场景用 ParseLogLevelOr。
func ParseLogLevel(s string) (Level, error) {
	switch strings.ToUpper(s) {
	case "DEBUG":
		return LevelDebug, nil
	case "INFO":
		return LevelInfo, nil
	case "WARN":
		return LevelWarn, nil
	case "ERROR":
		return LevelError, nil
	default:
		return LevelInfo, fmt.Errorf("logx: unknown level %q", s)
	}
}

// ParseLogLevelOr 解析级别字符串，未识别（含空串）时退回 def——
// 「LOG_LEVEL 配错就退回默认」这一绝大多数场景的免判空形态。
func ParseLogLevelOr(s string, def Level) Level {
	if lvl, err := ParseLogLevel(s); err == nil {
		return lvl
	}
	return def
}

// traceExtractor 是从 context 提取 trace_id 的可注入函数。
// 默认为 nil（返回空字符串），由 trace_id 的所有者（如 middleware/traceid 包）
// 在 init() 时通过 SetTraceExtractor 注入。
//
// 这一机制打破了 logx 对 middleware 的反向依赖：
// logx 不再需要 import middleware 来获取 trace_id，
// 而是由 trace_id 的提供方主动向 logx 注册提取逻辑。
var traceExtractor atomic.Pointer[func(context.Context) string]

// SetTraceExtractor 注册从 context 提取 trace_id 的函数。
// 应由 trace_id 的所有者（如 middleware/traceid）在包 init 时调用。
// 传入 nil 可清除已注册的提取器。
func SetTraceExtractor(fn func(context.Context) string) {
	if fn == nil {
		traceExtractor.Store(nil)
	} else {
		traceExtractor.Store(&fn)
	}
}

// ExtractTrace 从 context 提取 trace_id。
// 若未注册提取器或 context 中无 trace_id，返回空字符串。
func ExtractTrace(ctx context.Context) string {
	fnPtr := traceExtractor.Load()
	if fnPtr == nil {
		return ""
	}
	return (*fnPtr)(ctx)
}

// SetDefault 设置全局默认 logger 实现。
// 应在 main.go 中调用，且在使用任何 logx.Info 等函数之前。
// 传入 nil 是编程错误，直接 panic（否则后续所有日志调用都会 nil panic）。
// 并发安全，使用 atomic.Pointer 保护。
func SetDefault(l Logger) {
	if l == nil {
		panic("logx: SetDefault called with nil logger")
	}
	defaultLoggerV.Store(&l)
}

// Default 返回全局默认 logger。
func Default() Logger {
	return *defaultLoggerV.Load()
}

// WithService 创建带 service name 的子 logger。
// 等价于 Default().WithService(name)。
func WithService(name string) Logger {
	return Default().WithService(name)
}

// WithCtx 返回绑定了 context 的子 logger，等价于 Default().WithCtx(ctx)。
// 绑定后非 Ctx 方法自动携带 trace_id，参见 Logger.WithCtx。
func WithCtx(ctx context.Context) Logger {
	return Default().WithCtx(ctx)
}

// 全局快捷方法（委托给 Default()）

// Debug 输出 DEBUG 级别结构化日志。
func Debug(msg string, keysAndValues ...any) {
	Default().Debug(msg, keysAndValues...)
}

// Info 输出 INFO 级别结构化日志。
func Info(msg string, keysAndValues ...any) {
	Default().Info(msg, keysAndValues...)
}

// Warn 输出 WARN 级别结构化日志。
func Warn(msg string, keysAndValues ...any) {
	Default().Warn(msg, keysAndValues...)
}

// Error 输出 ERROR 级别结构化日志。
func Error(msg string, keysAndValues ...any) {
	Default().Error(msg, keysAndValues...)
}

// Debugf 输出 DEBUG 级别 printf 风格日志。
func Debugf(format string, args ...any) {
	Default().Debugf(format, args...)
}

// Infof 输出 INFO 级别 printf 风格日志。
func Infof(format string, args ...any) {
	Default().Infof(format, args...)
}

// Warnf 输出 WARN 级别 printf 风格日志。
func Warnf(format string, args ...any) {
	Default().Warnf(format, args...)
}

// Errorf 输出 ERROR 级别 printf 风格日志。
func Errorf(format string, args ...any) {
	Default().Errorf(format, args...)
}

// CtxDebug 输出带 Context 的 DEBUG 级别结构化日志（自动提取 trace_id）。
func CtxDebug(ctx context.Context, msg string, keysAndValues ...any) {
	Default().CtxDebug(ctx, msg, keysAndValues...)
}

// CtxInfo 输出带 Context 的 INFO 级别结构化日志（自动提取 trace_id）。
func CtxInfo(ctx context.Context, msg string, keysAndValues ...any) {
	Default().CtxInfo(ctx, msg, keysAndValues...)
}

// CtxWarn 输出带 Context 的 WARN 级别结构化日志（自动提取 trace_id）。
func CtxWarn(ctx context.Context, msg string, keysAndValues ...any) {
	Default().CtxWarn(ctx, msg, keysAndValues...)
}

// CtxError 输出带 Context 的 ERROR 级别结构化日志（自动提取 trace_id）。
func CtxError(ctx context.Context, msg string, keysAndValues ...any) {
	Default().CtxError(ctx, msg, keysAndValues...)
}

// CtxDebugf 输出带 Context 的 DEBUG 级别 printf 风格日志（自动提取 trace_id）。
func CtxDebugf(ctx context.Context, format string, args ...any) {
	Default().CtxDebugf(ctx, format, args...)
}

// CtxInfof 输出带 Context 的 INFO 级别 printf 风格日志（自动提取 trace_id）。
func CtxInfof(ctx context.Context, format string, args ...any) {
	Default().CtxInfof(ctx, format, args...)
}

// CtxWarnf 输出带 Context 的 WARN 级别 printf 风格日志（自动提取 trace_id）。
func CtxWarnf(ctx context.Context, format string, args ...any) {
	Default().CtxWarnf(ctx, format, args...)
}

// CtxErrorf 输出带 Context 的 ERROR 级别 printf 风格日志（自动提取 trace_id）。
func CtxErrorf(ctx context.Context, format string, args ...any) {
	Default().CtxErrorf(ctx, format, args...)
}

// FormatWithTraceID 格式化带 trace_id 的日志前缀。
func FormatWithTraceID(traceID, format string, args ...any) string {
	msg := fmt.Sprintf(format, args...)
	if traceID != "" {
		return fmt.Sprintf("[trace=%s] %s", traceID, msg)
	}
	return msg
}
