package logx

import (
	"context"
	"encoding/json"
	"fmt"
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
type slogLogger struct {
	BaseLogger
	group string
}

// NewSlogLogger 创建基于 log/slog 的结构化 Logger。
// service 会作为 "service" 字段输出到每条日志。
// 默认级别为 LevelInfo，输出 JSON 到 stdout。
func NewSlogLogger(service string) Logger {
	return &slogLogger{BaseLogger: BaseLogger{Service: service, Level: LevelInfo}}
}

// UseSlog 是便捷函数：创建 slogLogger 并设为全局默认。
// service 为服务名（输出为 service 字段）。
func UseSlog(service string) {
	SetDefault(NewSlogLogger(service))
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
		group: l.group,
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

// emit 是所有日志方法的统一入口，输出一行结构化 JSON。
func (l *slogLogger) emit(ctx context.Context, level Level, msg string, keysAndValues ...any) {
	if !l.ShouldLog(level) {
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
		record[a.Key] = a.Value
	}

	// 本次调用的 kv 对
	for i := 0; i+1 < len(keysAndValues); i += 2 {
		key := fmt.Sprintf("%v", keysAndValues[i])
		record[key] = keysAndValues[i+1]
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
		return fmt.Sprintf("%s:%d", filepath.Base(file), line)
	}
	return ""
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
