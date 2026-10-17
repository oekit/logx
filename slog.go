package logx

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// slogLogger 是基于标准库 log/slog 的 Logger 实现，输出真正的结构化日志（JSON）。
//
// 相比 hlog 子包的实现（字符串拼接降级），slogLogger 将 trace_id、service、属性、kv
// 全部作为独立的结构化字段输出，便于 ELK/Loki 等日志系统按字段检索与聚合。
//
// 用法:
//
//	// 切换默认 logger 为 slog 实现（输出 JSON 到 stdout）
//	logx.SetDefault(logx.NewSlogLogger("my-service"))
//
//	// 或在初始化时一次性切换
//	logx.UseSlog("my-service")
//
//	// 注入消费方自有 slog.Handler，保留既有日志格式
//	logx.SetDefault(logx.NewSlogLogger("my-service", logx.WithHandler(myHandler)))
type slogLogger struct {
	BaseLogger
	group string
	// handler 非 nil 时走消费方注入的 slog.Handler，由其负责格式与输出；
	// nil 时走内置 JSON→stdout 路径。构造后不可变，clone 时原样携带。
	handler slog.Handler
}

// SlogOption 是 NewSlogLogger 的函数选项。
type SlogOption func(*slogLogger)

// WithHandler 注入消费方自有的 slog.Handler，日志格式与输出目标完全由该 Handler
// 决定（TextHandler / 文件轮转 / fan-out 等）。logx 侧仍负责级别过滤（见
// BaseLogger.Level）与 service / trace_id / Attr / kv 等结构化字段注入。
//
// 未注入时保持默认行为：内置 JSON 序列化输出到 stdout。
func WithHandler(h slog.Handler) SlogOption {
	return func(l *slogLogger) { l.handler = h }
}

// WithLevel 显式指定日志级别，优先级高于 LOG_LEVEL 环境变量。
// 构造后仍可 SetLevel 运行期调级。
func WithLevel(level Level) SlogOption {
	return func(l *slogLogger) { l.Level = level }
}

// NewSlogLogger 创建基于 log/slog 的结构化 Logger。
// service 会作为 "service" 字段输出到每条日志。
//
// 级别优先级：WithLevel 显式参数 > LOG_LEVEL 环境变量 > 默认 LevelInfo。
// 未注入 WithHandler 时输出 JSON 到 stdout；注入后由该 Handler 决定格式。
func NewSlogLogger(service string, opts ...SlogOption) Logger {
	l := &slogLogger{BaseLogger: BaseLogger{Service: service, Level: LevelInfo}}
	l.Level = ParseLogLevelOr(os.Getenv("LOG_LEVEL"), LevelInfo)
	for _, opt := range opts {
		if opt != nil {
			opt(l)
		}
	}
	return l
}

// UseSlog 是便捷函数：创建 slogLogger 并设为全局默认。
// service 为服务名（输出为 service 字段）。
func UseSlog(service string, opts ...SlogOption) {
	SetDefault(NewSlogLogger(service, opts...))
}

func (l *slogLogger) clone() *slogLogger {
	cp := l.Snapshot()
	return &slogLogger{
		BaseLogger: BaseLogger{
			Service: cp.Service,
			Level:   cp.Level,
			Attrs:   append([]Attr(nil), cp.Attrs...),
			Ctx:     cp.Ctx,
		},
		group:   l.group,
		handler: l.handler,
	}
}

func (l *slogLogger) WithCtx(ctx context.Context) Logger {
	c := l.clone()
	c.Ctx = ctx
	return c
}

func (l *slogLogger) WithService(name string) Logger {
	c := l.clone()
	c.Service = name
	return c
}

func (l *slogLogger) WithAttrs(attrs ...Attr) Logger {
	c := l.clone()
	c.Attrs = append(c.Attrs, attrs...)
	return c
}

func (l *slogLogger) WithGroup(name string) Logger {
	c := l.clone()
	c.group = name
	return c
}

// emit 是所有日志方法的统一入口。注入了 WithHandler 时交给该 Handler 输出；
// 否则输出一行结构化 JSON 到 stdout。
func (l *slogLogger) emit(ctx context.Context, level Level, msg string, keysAndValues ...any) {
	if !l.ShouldLog(level) {
		return
	}

	if l.handler != nil {
		l.emitToHandler(ctx, level, msg, keysAndValues...)
		return
	}

	record := map[string]any{
		"time":    time.Now().Format(time.RFC3339Nano),
		"level":   LevelString(level),
		"message": msg,
	}
	if l.Service != "" {
		record["service"] = l.Service
	}
	if traceID := ExtractTrace(ctx); traceID != "" {
		record["trace_id"] = traceID
	}

	// 捕获真实调用方位置（跳过 logx 内部帧）
	if caller := callerOutsideLogx(); caller != "" {
		record["caller"] = caller
	}

	// 持久化属性
	for _, a := range l.Attrs {
		record[a.Key] = logValue(a.Value)
	}

	// 本次调用的 kv 对
	for i := 0; i+1 < len(keysAndValues); i += 2 {
		key := fmt.Sprintf("%v", keysAndValues[i])
		record[key] = logValue(keysAndValues[i+1])
	}
	if len(keysAndValues)%2 == 1 {
		lastKey := fmt.Sprintf("%v", keysAndValues[len(keysAndValues)-1])
		record[lastKey] = "(MISSING)"
	}

	buf, err := json.Marshal(record)
	if err != nil {
		// 序列化失败（NaN/channel/func/循环引用等不可序列化值）：
		// 降级为纯字符串输出，绝不静默丢弃整条日志
		fallback := map[string]string{
			"level":      LevelString(level),
			"message":    msg,
			"marshalErr": err.Error(),
			"rawKv":      fmt.Sprint(keysAndValues...),
		}
		buf, _ = json.Marshal(fallback) // 仅字符串字段，不会失败
	}
	os.Stdout.Write(buf)
	os.Stdout.Write([]byte{'\n'})
}

// emitToHandler 将日志经消费方注入的 slog.Handler 输出。字段口径与内置 JSON
// 路径一致（service / trace_id / caller / Attr / kv），格式完全由 Handler 决定。
func (l *slogLogger) emitToHandler(ctx context.Context, level Level, msg string, keysAndValues ...any) {
	pc, caller := realCallerOutsideLogx()
	attrs := make([]slog.Attr, 0, 4+len(l.Attrs)+len(keysAndValues)/2+1)
	if l.Service != "" {
		attrs = append(attrs, slog.String("service", l.Service))
	}
	if traceID := ExtractTrace(ctx); traceID != "" {
		attrs = append(attrs, slog.String("trace_id", traceID))
	}
	if caller != "" {
		attrs = append(attrs, slog.String("caller", caller))
	}
	for _, a := range l.Attrs {
		attrs = append(attrs, slog.Any(a.Key, logValue(a.Value)))
	}
	for i := 0; i+1 < len(keysAndValues); i += 2 {
		key := fmt.Sprintf("%v", keysAndValues[i])
		attrs = append(attrs, slog.Any(key, logValue(keysAndValues[i+1])))
	}
	if len(keysAndValues)%2 == 1 {
		lastKey := fmt.Sprintf("%v", keysAndValues[len(keysAndValues)-1])
		attrs = append(attrs, slog.Any(lastKey, "(MISSING)"))
	}

	h := l.handler
	if l.group != "" {
		h = h.WithGroup(l.group)
	}
	rec := slog.NewRecord(time.Now(), slogLevel(level), msg, pc)
	rec.AddAttrs(attrs...)
	_ = h.Handle(ctx, rec)
}

// slogLevel 将 logx.Level 映射为 log/slog.Level。
func slogLevel(level Level) slog.Level {
	switch level {
	case LevelDebug:
		return slog.LevelDebug
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// logValue 规整 kv/Attr 值的可序列化形态：error 落 Error() 文本。encoding/json
// 对多数 error 实现（fmt.wrapError、*errors.errorString、包装链等）无导出字段，
// 会渲染成 "{}"，整条错误日志丢掉原因；与 log/slog JSONHandler 的 error 处理
// 对齐（issue #2）。nil（未类型化）不经此路径，原样保留为 null。
func logValue(v any) any {
	if err, ok := v.(error); ok {
		return err.Error()
	}
	return v
}

// callerSkipFiles 存放需跳过 caller 归属的源文件绝对路径（由 SkipCallerFiles 写入）。
// nil 表示无额外跳过项。
var callerSkipFiles atomic.Pointer[[]string]

// SkipCallerFiles 将指定源文件标记为日志内部帧：slog 后端归属 caller 字段时
// 跳过这些文件中的调用方（重复注册同一文件自动去重，并发安全）。
//
// 用途：klog/hlog 桥接适配器位于 logx 之外，若不跳过，框架日志的 caller 会
// 错误归属到适配器文件（如 kitex/log.go）而非真实调用方——即 Kitex 官方日志
// 文档警告的"封装 klog 导致文件名行数不准确"问题。适配器应在构造函数中以
// runtime.Caller(0) 注册自身文件；其他代码不应调用本函数。
func SkipCallerFiles(files ...string) {
	if len(files) == 0 {
		return
	}
	for {
		old := callerSkipFiles.Load()
		merged := mergeSkipFiles(old, files)
		if callerSkipFiles.CompareAndSwap(old, merged) {
			return
		}
	}
}

func mergeSkipFiles(old *[]string, add []string) *[]string {
	merged := make([]string, 0, len(add)+4)
	seen := make(map[string]struct{}, len(add)+4)
	if old != nil {
		for _, f := range *old {
			if _, dup := seen[f]; !dup {
				seen[f] = struct{}{}
				merged = append(merged, f)
			}
		}
	}
	for _, f := range add {
		if _, dup := seen[f]; !dup {
			seen[f] = struct{}{}
			merged = append(merged, f)
		}
	}
	return &merged
}

// callerOutsideLogx 从调用栈中找到第一个不属于日志内部帧的调用方。
// 返回 "file:line" 格式，用于结构化日志的 caller 字段。
// 内部帧包括：logx 包自身、SkipCallerFiles 注册的桥接适配器文件。
//
// 不缓存 skip 深度：不同调用路径的栈深不同（Logger 方法直调 vs 包级
// logx.Info vs Delegating 包装），全局缓存会让先到的路径污染后到的，
// 把 caller 错误归属到无关文件。探测循环上限 12 帧，成本可忽略。
func callerOutsideLogx() string {
	_, caller := realCallerOutsideLogx()
	return caller
}

// realCallerOutsideLogx 返回真实调用方的 PC 与 "file:line"。
// PC 供 slog.Record 使用（Handler 的 AddSource 依赖它），字符串供 JSON 路径的
// caller 字段使用。判定规则同 callerOutsideLogx。
func realCallerOutsideLogx() (uintptr, string) {
	skipFiles := callerSkipFiles.Load()
	for skip := 2; skip < 12; skip++ {
		pc, file, line, ok := runtime.Caller(skip)
		if !ok {
			break
		}
		fn := runtime.FuncForPC(pc)
		if fn == nil {
			break
		}
		// 内部帧判定：按函数名里的 module 路径前缀匹配日志实现自身——
		// 根包、hlog/hertzlog 子包、以及内/外测试包。internal/ 下的测试
		// 探针（logxcallertest）不属于实现，必须穿透。改 module 路径时
		// 必须同步这里。
		if i := strings.Index(fn.Name(), "oekit/logx"); i >= 0 {
			rest := fn.Name()[i+len("oekit/logx"):]
			if rest == "" || rest[0] == '.' || rest[0] == '_' ||
				strings.HasPrefix(rest, "/hlog") || strings.HasPrefix(rest, "/hertzlog") {
				continue
			}
		}
		if isSkipFile(skipFiles, file) {
			continue
		}
		return pc, fmt.Sprintf("%s:%d", filepath.Base(file), line)
	}
	return 0, ""
}

func isSkipFile(skipFiles *[]string, file string) bool {
	if skipFiles == nil {
		return false
	}
	for _, f := range *skipFiles {
		if f == file {
			return true
		}
	}
	return false
}

// 结构化风格方法（不带 ctx，绑定 ctx 时自动提取 trace_id）

func (l *slogLogger) Debug(msg string, keysAndValues ...any) {
	l.emit(l.EffectiveCtx(), LevelDebug, msg, keysAndValues...)
}
func (l *slogLogger) Info(msg string, keysAndValues ...any) {
	l.emit(l.EffectiveCtx(), LevelInfo, msg, keysAndValues...)
}
func (l *slogLogger) Warn(msg string, keysAndValues ...any) {
	l.emit(l.EffectiveCtx(), LevelWarn, msg, keysAndValues...)
}
func (l *slogLogger) Error(msg string, keysAndValues ...any) {
	l.emit(l.EffectiveCtx(), LevelError, msg, keysAndValues...)
}

// printf 风格方法

func (l *slogLogger) Debugf(format string, args ...any) {
	l.emit(l.EffectiveCtx(), LevelDebug, sprintf(format, args...))
}
func (l *slogLogger) Infof(format string, args ...any) {
	l.emit(l.EffectiveCtx(), LevelInfo, sprintf(format, args...))
}
func (l *slogLogger) Warnf(format string, args ...any) {
	l.emit(l.EffectiveCtx(), LevelWarn, sprintf(format, args...))
}
func (l *slogLogger) Errorf(format string, args ...any) {
	l.emit(l.EffectiveCtx(), LevelError, sprintf(format, args...))
}

// 带 Context 的结构化风格方法

func (l *slogLogger) CtxDebug(ctx context.Context, msg string, keysAndValues ...any) {
	l.emit(ctx, LevelDebug, msg, keysAndValues...)
}
func (l *slogLogger) CtxInfo(ctx context.Context, msg string, keysAndValues ...any) {
	l.emit(ctx, LevelInfo, msg, keysAndValues...)
}
func (l *slogLogger) CtxWarn(ctx context.Context, msg string, keysAndValues ...any) {
	l.emit(ctx, LevelWarn, msg, keysAndValues...)
}
func (l *slogLogger) CtxError(ctx context.Context, msg string, keysAndValues ...any) {
	l.emit(ctx, LevelError, msg, keysAndValues...)
}

// 带 Context 的 printf 风格方法

func (l *slogLogger) CtxDebugf(ctx context.Context, format string, args ...any) {
	l.emit(ctx, LevelDebug, sprintf(format, args...))
}
func (l *slogLogger) CtxInfof(ctx context.Context, format string, args ...any) {
	l.emit(ctx, LevelInfo, sprintf(format, args...))
}
func (l *slogLogger) CtxWarnf(ctx context.Context, format string, args ...any) {
	l.emit(ctx, LevelWarn, sprintf(format, args...))
}
func (l *slogLogger) CtxErrorf(ctx context.Context, format string, args ...any) {
	l.emit(ctx, LevelError, sprintf(format, args...))
}

// sprintf 是 fmt.Sprintf 的简写。
func sprintf(format string, args ...any) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(format, args...)
}

// NewAuditLogger 创建审计/合规/安全留痕类 Logger。
//
// 级别锁定在 LevelInfo（及更细），不受 LOG_LEVEL 环境变量降级；SetLevel
// 试图调到 WARN/ERROR 会被拒绝——审计事件不得被部署配置静默过滤。
// 需要更细粒度可用 SetLevel(LevelDebug)。
//
// 业务日志请用 NewSlogLogger；本构造仅用于「留痕不可丢」的路径
// （登录、越权、配置变更、合规操作等）。
func NewAuditLogger(service string, opts ...SlogOption) Logger {
	// WithLevel 置于最后，覆盖 opts 里可能的降级与 LOG_LEVEL
	opts = append(opts, WithLevel(LevelInfo))
	return auditLogger{inner: NewSlogLogger(service, opts...)}
}

// auditLogger 包装任意 Logger，拒绝将级别调到会吞掉 Info 审计事件的档位。
type auditLogger struct{ inner Logger }

func (a auditLogger) SetLevel(level Level) {
	if level > LevelInfo {
		return // 拒绝降级：Info 级审计必须保留
	}
	a.inner.SetLevel(level)
}

func (a auditLogger) WithCtx(ctx context.Context) Logger {
	return auditLogger{inner: a.inner.WithCtx(ctx)}
}
func (a auditLogger) WithService(name string) Logger {
	return auditLogger{inner: a.inner.WithService(name)}
}
func (a auditLogger) WithAttrs(attrs ...Attr) Logger {
	return auditLogger{inner: a.inner.WithAttrs(attrs...)}
}
func (a auditLogger) WithGroup(name string) Logger {
	return auditLogger{inner: a.inner.WithGroup(name)}
}

// 其余 Logger 方法委托 inner。
func (a auditLogger) Debug(msg string, kv ...any) { a.inner.Debug(msg, kv...) }
func (a auditLogger) Info(msg string, kv ...any)  { a.inner.Info(msg, kv...) }
func (a auditLogger) Warn(msg string, kv ...any)  { a.inner.Warn(msg, kv...) }
func (a auditLogger) Error(msg string, kv ...any) { a.inner.Error(msg, kv...) }

func (a auditLogger) Debugf(format string, args ...any) { a.inner.Debugf(format, args...) }
func (a auditLogger) Infof(format string, args ...any)  { a.inner.Infof(format, args...) }
func (a auditLogger) Warnf(format string, args ...any)  { a.inner.Warnf(format, args...) }
func (a auditLogger) Errorf(format string, args ...any) { a.inner.Errorf(format, args...) }

func (a auditLogger) CtxDebug(ctx context.Context, msg string, kv ...any) {
	a.inner.CtxDebug(ctx, msg, kv...)
}
func (a auditLogger) CtxInfo(ctx context.Context, msg string, kv ...any) {
	a.inner.CtxInfo(ctx, msg, kv...)
}
func (a auditLogger) CtxWarn(ctx context.Context, msg string, kv ...any) {
	a.inner.CtxWarn(ctx, msg, kv...)
}
func (a auditLogger) CtxError(ctx context.Context, msg string, kv ...any) {
	a.inner.CtxError(ctx, msg, kv...)
}

func (a auditLogger) CtxDebugf(ctx context.Context, format string, args ...any) {
	a.inner.CtxDebugf(ctx, format, args...)
}
func (a auditLogger) CtxInfof(ctx context.Context, format string, args ...any) {
	a.inner.CtxInfof(ctx, format, args...)
}
func (a auditLogger) CtxWarnf(ctx context.Context, format string, args ...any) {
	a.inner.CtxWarnf(ctx, format, args...)
}
func (a auditLogger) CtxErrorf(ctx context.Context, format string, args ...any) {
	a.inner.CtxErrorf(ctx, format, args...)
}
