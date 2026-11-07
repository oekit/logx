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
	// groups 是 WithGroup 累积的命名空间栈（#83）：slog 语义里链式
	// WithGroup("a").WithGroup("b") 是嵌套命名空间 a.b，此前单 string 槽位
	// 静默只留最后一层。
	groups []string
	// handler 非 nil 时走消费方注入的 slog.Handler，由其负责格式与输出；
	// nil 时走内置 JSON→stdout 路径。构造后不可变，clone 时原样携带。
	// 组链**不**应用到 handler（#99）：框架字段必须留在组外，用户字段经
	// slog.Group 显式包裹（见 emitToHandler），此前整体 h.WithGroup 会把
	// service/trace_id/caller 一并嵌进组——切后端即换 schema。
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
		groups:  append([]string(nil), l.groups...),
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

// WithGroup 返回带字段命名空间的子 logger（#75 #83 #99）。
//
// slog 语义：group 是结构化字段的命名空间，链式调用**累积嵌套**——
// WithGroup("a").WithGroup("b") 后用户字段落在 "a.b." 命名空间下（此前
// 单槽位静默只留最后一层）。两条输出路径统一生效，且**框架字段
// （time/level/message/service/trace_id/caller）都不进组**：
//
//   - 内置 JSON 路径：用户 Attr/kv 键加 "<g1>.<g2>." 前缀平铺（与
//     SlogHandler 桥接的 "group.key" 方案同一口径）。
//   - 注入 Handler 路径：用户字段（Attr/kv）经 slog.Group 显式包裹成
//     嵌套结构，由注入的 Handler 原生渲染；service/trace_id/caller
//     保持记录顶层属性（#99：此前把整个 handler h.WithGroup，框架字段
//     作为 record attr 一并被嵌进组——同一 logger 切后端即换 schema，
//     按 .service/.trace_id 检索的链路在 Handler 路径上直接断掉）。
//
// name 为空串时为 no-op（对齐 slog.Handler.WithGroup 语义）。
func (l *slogLogger) WithGroup(name string) Logger {
	c := l.clone()
	if name == "" {
		return c
	}
	c.groups = append(c.groups, name)
	return c
}

// emit 是所有日志方法的统一入口。注入了 WithHandler 时交给该 Handler 输出；
// 否则输出一行结构化 JSON 到 stdout。
func (l *slogLogger) emit(ctx context.Context, level Level, msg string, keysAndValues ...any) {
	l.emitAt(ctx, level, msg, 0, time.Time{}, keysAndValues...)
}

// emitAt 是统一输出入口（#84）：pc/ts 由桥接路径（SlogHandler 直通）传入
// slog 侧已解析好的调用点与时间戳——此前 Handle 忽略 r.PC/r.Time 走栈回溯，
// 第一个非 logx 帧是 stdlib log/slog.(*Logger).log，caller 恒错标到
// log/slog/logger.go 源码行。pc==0 / ts.IsZero() 表示未提供，在输出时自行
// 探测 caller、取当前时间（本地调用路径语义不变）。
func (l *slogLogger) emitAt(ctx context.Context, level Level, msg string, pc uintptr, ts time.Time, keysAndValues ...any) {
	ctx = ctxOrBackground(ctx) // #97：Ctx* 入参 nil ctx 统一兜底（ExtractTrace 与注入 Handler 都不再收到 nil）
	if !l.ShouldLog(level) {
		return
	}

	if l.handler != nil {
		l.emitToHandler(ctx, level, msg, pc, ts, keysAndValues...)
		return
	}

	if ts.IsZero() {
		ts = time.Now()
	}
	record := map[string]any{
		"time":    ts.Format(time.RFC3339Nano),
		"level":   LevelString(level),
		"message": msg,
	}
	if l.Service != "" {
		record["service"] = l.Service
	}
	if traceID := ExtractTrace(ctx); traceID != "" {
		record["trace_id"] = traceID
	}

	// 捕获真实调用方位置（跳过 logx 内部帧）；桥接路径复用 slog 已解析的 PC
	caller := ""
	if pc != 0 {
		caller = pcCaller(pc)
	} else {
		caller = callerOutsideLogx()
	}
	if caller != "" {
		record["caller"] = caller
	}

	// 框架字段名集合（#68）：time/level/message/service/trace_id/caller 由本函数
	// 发布，用户 Attr/kv 同键冲突时**框架字段优先**、用户键加 "_" 前缀落地并打
	// WARN——此前 map 赋值让用户值静默覆盖框架字段（trace_id 被覆盖 = 追踪链断），
	// 与 trace_handler 的「已有同名字段不重复注入」、handler 路径的「原样重复」
	// 形成三口径分裂；统一成「框架字段不可覆盖 + 显式改名 + WARN 可观测」。
	// 提为包级只读变量（#87）：每条记录重建 map 是热路径纯浪费。
	reserved := reservedFields
	// 用户字段命名空间前缀（#75 #83）：WithGroup 累积的组以 "<g1>.<g2>."
	// 前缀施加到用户 Attr/kv 键——与 SlogHandler 桥接的 "group.key" 口径
	// 一致；框架字段不进组。加前缀天然避开与框架字段的键冲突（"g.time"
	// 不再撞 "time"，组即命名空间）。
	groupPrefix := ""
	if len(l.groups) > 0 {
		groupPrefix = strings.Join(l.groups, ".") + "."
	}
	for _, a := range l.Attrs {
		putRecordField(record, reserved, groupPrefix+a.Key, logValue(a.Value))
	}

	// 本次调用的 kv 对
	for i := 0; i+1 < len(keysAndValues); i += 2 {
		key := fmt.Sprintf("%v", keysAndValues[i])
		putRecordField(record, reserved, groupPrefix+key, logValue(keysAndValues[i+1]))
	}
	if len(keysAndValues)%2 == 1 {
		lastKey := fmt.Sprintf("%v", keysAndValues[len(keysAndValues)-1])
		putRecordField(record, reserved, groupPrefix+lastKey, "(MISSING)")
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
		// 降级不脱钩（#100）：关联字段一并带上——service/trace_id/caller 取自
		// 上方已写入 record 的框架字段，WithAttrs 持久属性逐键平铺（值统一转
		// 字符串）。一条 NaN 不该让整条日志失去可检索性：故障排查时最需要
		// 的 trace/service 关联恰好不能丢。框架降级字段优先，用户同名键让位。
		for _, k := range [...]string{"service", "trace_id", "caller"} {
			if v, ok := record[k].(string); ok {
				fallback[k] = v
			}
		}
		for _, a := range l.Attrs {
			k := groupPrefix + a.Key
			if _, taken := fallback[k]; !taken {
				fallback[k] = fmt.Sprintf("%v", logValue(a.Value))
			}
		}
		buf, _ = json.Marshal(fallback) // 仅字符串字段，不会失败
	}
	// 换行并入同一次 Write（#65）：两次 Write 之间无原子性保证，并发下另一
	// goroutine 的记录体会插进本条与换行之间 ⇒ JSONL 拼行/空行，下游逐行解析
	// 静默丢日志。对齐 log.Printf / log/slog JSONHandler 的整行单写纪律。
	buf = append(buf, '\n')
	writeStdout(buf)
}

// stdoutWriteFailures 累计内置 JSON 路径 stdout 写失败次数；stdoutFailReported
// 保证降级 stderr 报告只发一次（#79）。
var (
	stdoutWriteFailures atomic.Uint64
	stdoutFailReported  atomic.Bool
)

// StdoutWriteFailures 返回内置 JSON 路径 stdout 写失败的累计次数（#79）。
//
// 排障入口：服务「没日志了」时先看它——非零说明写端坏了（stdout 被重定向
// 到已关闭管道、磁盘满等），而不是日志没产生。此前 os.Stdout.Write 的返回
// error 被直接丢弃，与同函数注释「绝不静默丢弃整条日志」的承诺自相矛盾。
func StdoutWriteFailures() uint64 {
	return stdoutWriteFailures.Load()
}

// handlerWriteFailures 累计注入 Handler 路径 slog.Handler.Handle 失败次数；
// handlerFailReported 保证降级 stderr 报告只发一次（#98）。
var (
	handlerWriteFailures atomic.Uint64
	handlerFailReported  atomic.Bool
)

// HandlerWriteFailures 返回注入 Handler 路径 slog.Handler.Handle 失败的
// 累计次数（#98），与 StdoutWriteFailures 同为排障入口。
//
// Handle 返回 error 的语义是「这条记录没有写出去」；此前 emitToHandler 把
// 它直接丢弃（`_ = h.Handle(...)`），注入的 Handler 恒错（磁盘满、下游
// 阻塞等）时记录彻底消失且无任何信号——与内置路径「绝不静默丢弃」的
// 契约在同函数另一条路径上被打破。
func HandlerWriteFailures() uint64 {
	return handlerWriteFailures.Load()
}

// writeStdout 写一行日志到 stdout（#79）：失败时计数，并降级到 stderr 报告
// **一次**（附被丢弃的日志行，便于定位写端故障）——只报一次避免每条失败
// 都写 stderr 形成二次风暴；后续失败静默计入 StdoutWriteFailures 计数。
func writeStdout(buf []byte) {
	_, err := os.Stdout.Write(buf)
	if err == nil {
		return
	}
	stdoutWriteFailures.Add(1)
	if stdoutFailReported.CompareAndSwap(false, true) {
		fmt.Fprintf(os.Stderr,
			"logx: 内置 JSON 路径 stdout 写失败（本报告仅一次，后续失败计入 StdoutWriteFailures）: %v; 丢弃日志: %.200s\n",
			err, buf)
	}
}

// emitToHandler 将日志经消费方注入的 slog.Handler 输出。字段口径与内置 JSON
// 路径一致（service / trace_id / caller / Attr / kv），格式完全由 Handler 决定。
// bridgePC/bridgeTS 非 nil 时复用桥接侧已解析的调用点与时间戳（#84）。
func (l *slogLogger) emitToHandler(ctx context.Context, level Level, msg string, bridgePC uintptr, bridgeTS time.Time, keysAndValues ...any) {
	pc, caller := realCallerOutsideLogx()
	if bridgePC != 0 {
		pc = bridgePC
		caller = pcCaller(bridgePC)
	}
	ts := bridgeTS
	if ts.IsZero() {
		ts = time.Now()
	}
	// 框架字段（service/trace_id/caller）保持记录顶层属性，不进组（#99）。
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

	// 用户字段（Attr/kv）经 slog.Group 显式包裹组链（#99）：与内置路径
	// 「框架字段不进组」的层级口径一致；组由 slog.Group 表达、注入的
	// Handler 原生渲染嵌套结构——此前整体 h.WithGroup 会把上面的框架
	// 字段一并嵌进组，切后端即换 schema，.service/.trace_id 检索断链。
	user := make([]slog.Attr, 0, len(l.Attrs)+len(keysAndValues)/2+1)
	for _, a := range l.Attrs {
		user = append(user, slog.Any(a.Key, logValue(a.Value)))
	}
	for i := 0; i+1 < len(keysAndValues); i += 2 {
		key := fmt.Sprintf("%v", keysAndValues[i])
		user = append(user, slog.Any(key, logValue(keysAndValues[i+1])))
	}
	if len(keysAndValues)%2 == 1 {
		lastKey := fmt.Sprintf("%v", keysAndValues[len(keysAndValues)-1])
		user = append(user, slog.Any(lastKey, "(MISSING)"))
	}
	for i := len(l.groups) - 1; i >= 0; i-- {
		user = []slog.Attr{{Key: l.groups[i], Value: slog.GroupValue(user...)}}
	}
	attrs = append(attrs, user...)

	rec := slog.NewRecord(ts, slogLevel(level), msg, pc)
	rec.AddAttrs(attrs...)
	// Handle 失败必须可观测（#98）：计数累加 + 一次性 stderr 报告（同内置
	// 路径 writeStdout 口径），此前 `_ =` 直接吞掉，记录消失无任何信号。
	if err := l.handler.Handle(ctx, rec); err != nil {
		handlerWriteFailures.Add(1)
		if handlerFailReported.CompareAndSwap(false, true) {
			fmt.Fprintf(os.Stderr,
				"logx: 注入 Handler 路径 Handle 失败（本报告仅一次，后续失败计入 HandlerWriteFailures）: %v; 丢弃日志: %.200s\n",
				err, msg)
		}
	}
}

// logxBridge 是 slogBridgeSink 的实现（#84）：SlogHandler 桥接直通入口，
// 复用 slog 侧已解析的调用点 PC 与时间戳，不再栈回溯。
func (l *slogLogger) logxBridge(ctx context.Context, level Level, pc uintptr, ts time.Time, msg string, kv ...any) {
	l.emitAt(ctx, level, msg, pc, ts, kv...)
}

// pcCaller 将单个 PC 解析为 "file:line"（与 realCallerOutsideLogx 的字符串
// 口径一致）。桥接路径复用 slog 已解析好的调用点（r.PC）时使用。
func pcCaller(pc uintptr) string {
	if pc == 0 {
		return ""
	}
	frames := runtime.CallersFrames([]uintptr{pc})
	f, _ := frames.Next()
	if f.File == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", filepath.Base(f.File), f.Line)
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
// 内部帧包括：logx 包自身、stdlib log/slog 桥接内部帧、SkipCallerFiles
// 注册的桥接适配器文件。
//
// 未命中（首个外部帧之前栈耗尽，或全部帧都是日志机制内部帧）时返回
// 空串、caller 字段省略。探测为单遍全栈扫描，无帧数上限（#82）：
// 深调用栈（多层中间件 + 包装 logger）不会静默丢 caller。
//
// 不缓存 skip 深度：不同调用路径的栈深不同（Logger 方法直调 vs 包级
// logx.Info vs Delegating 包装），全局缓存会让先到的路径污染后到的，
// 把 caller 错误归属到无关文件。
func callerOutsideLogx() string {
	_, caller := realCallerOutsideLogx()
	return caller
}

// realCallerOutsideLogx 返回真实调用方的 PC 与 "file:line"。
// PC 供 slog.Record 使用（Handler 的 AddSource 依赖它），字符串供 JSON 路径的
// caller 字段使用。判定规则同 callerOutsideLogx。
//
// 实现（#87）：一次 runtime.Callers + runtime.CallersFrames 单遍过滤——
// 此前 skip=2..11 串行 runtime.Caller，每次调用独立 O(depth) 重走整栈，
// 整体 O(k²)：深栈多层包装下探测成本与整条 json.Marshal 同量级
// （实测 465-1515ns/op，随栈深恶化）。单遍后无 12 帧上限（#82）：
// 栈深超过初始缓冲（64 帧）时扩容重取一次。
func realCallerOutsideLogx() (uintptr, string) {
	var buf [64]uintptr
	n := runtime.Callers(3, buf[:])
	pcs := buf[:n]
	if n == len(buf) {
		// 栈比初始缓冲深（深递归/多层包装，罕见）：扩容重取，保证不因
		// 缓冲边界漏掉真实调用方帧（#82：此前上限 12 帧时静默丢 caller）。
		grown := make([]uintptr, 4*len(buf))
		if n2 := runtime.Callers(3, grown); n2 > 0 {
			pcs = grown[:n2]
		}
	}
	skipFiles := callerSkipFiles.Load()
	frames := runtime.CallersFrames(pcs)
	for {
		frame, more := frames.Next()
		if frame.PC == 0 && frame.File == "" {
			break // 帧耗尽仍未命中
		}
		if !frameInsideLogx(frame.Function, frame.File, skipFiles) {
			return frame.PC, fmt.Sprintf("%s:%d", filepath.Base(frame.File), frame.Line)
		}
		if !more {
			break
		}
	}
	return 0, ""
}

// frameInsideLogx 判定一帧是否日志机制内部帧（探测时跳过）：
//
//   - logx 包自身：按函数名里的 module 路径前缀匹配——根包、hlog/hertzlog
//     子包、以及内/外测试包。internal/ 下的测试探针（logxcallertest）
//     不属于实现，必须穿透。改 module 路径时必须同步这里。
//   - stdlib log/slog 内部帧（如 (*Logger).log，#84）：经 SlogHandler 桥接的
//     包装链（Delegating/audit 等，未走 PC 直通）回溯时，这些帧不是业务
//     调用方，不跳过会把 caller 恒错标到 log/slog/logger.go 源码行。
//   - SkipCallerFiles 注册的桥接适配器文件。
func frameInsideLogx(fnName, file string, skipFiles *[]string) bool {
	if i := strings.Index(fnName, "oekit/logx"); i >= 0 {
		rest := fnName[i+len("oekit/logx"):]
		if rest == "" || rest[0] == '.' || rest[0] == '_' ||
			strings.HasPrefix(rest, "/hlog") || strings.HasPrefix(rest, "/hertzlog") {
			return true
		}
	}
	if strings.HasPrefix(fnName, "log/slog.") {
		return true
	}
	return isSkipFile(skipFiles, file)
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

// Unwrap 暴露被包装的内部 logger（#106）：IsHlogBacked 沿 Unwrap 链钻取。
func (a auditLogger) Unwrap() Logger { return a.inner }

// Enabled 透传内部后端的级别判定——LevelEnabler 实现（#76）。
// 此前 auditLogger 未实现该接口，logx.Enabled 恒 true，热路径守卫在
// 最常用的审计包装器上失效。
func (a auditLogger) Enabled(level Level) bool {
	return Enabled(a.inner, level)
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

// reservedFields 是内置 JSON 路径的框架字段名集合（见 emit 内注释，#68/#87）。
// 初始化后只读，并发访问安全。
var reservedFields = map[string]bool{
	"time": true, "level": true, "message": true,
	"service": true, "trace_id": true, "caller": true,
}

// putRecordField 落一个字段进内置 JSON record（#68）：键与框架字段冲突时
// 框架字段优先、用户键加 "_" 前缀落地，并在 record 里追加 "_renamed" 标记
// 字段列出改名清单——不在日志流里打 WARN（logx 给自己打日志有递归风险），
// 改名痕迹在每条记录里可 grep：`jq 'select(."​_renamed")'`。
// "_" 前缀键与用户自造 "_" 键再冲突时不再二次改名（只损失用户数据，
// 框架字段不受影响）。
func putRecordField(record map[string]any, reserved map[string]bool, key string, v any) {
	if !reserved[key] {
		record[key] = v
		return
	}
	renamed := "_" + key
	record[renamed] = v
	marks, _ := record["_renamed"].([]string)
	record["_renamed"] = append(marks, key)
}
