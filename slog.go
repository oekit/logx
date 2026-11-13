package logx

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"
)

// slogLogger 是基于标准库 log/slog 的 Logger 实现，输出真正的结构化日志（JSON）。
//
// 相比 hlog 子包的实现（字符串拼接降级），slogLogger 将 trace_id、service、属性、kv
// 全部作为独立的结构化字段输出，便于 ELK/Loki 等日志系统按字段检索与聚合。
//
// 本文件只承载后端本体与 emit 编排（#166 按关注点拆文件）：
//   - json.go：内置 JSON 路径的编码（append 式 + sync.Pool，#170）；
//   - caller.go：调用方栈回溯与内部帧判定；
//   - audit.go：NewAuditLogger 审计留痕包装器。
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
	// attrPrefixes / attrChains 与 Attrs 一一对应（对齐不变量：WithAttrs 是
	// BaseLogger.Attrs 的唯一追加方，clone 同步拷贝），记录每个持久属性
	// **挂载时刻**的组前缀快照（#192）：attrPrefixes 是展平前缀（"a.b."，
	// 内置 JSON 路径直拼，输出时零重算）、attrChains 是组名链（注入 Handler
	// 路径按它构建嵌套 slog.Group，不拆键、组名可含点）。此前输出时刻才对
	// 全部累计属性施加最终组前缀，WithAttrs 先于 WithGroup 的属性被错误
	// 卷入组，违反 slog「subsequent attributes」契约（#192）。切片在
	// WithAttrs 间共享底层存储（构造后不可变），clone 拷贝外层切片即可。
	attrPrefixes []string
	attrChains   [][]string
	// attrMask 是过滤层下沉的持久属性脱敏键集（#194，AttrKeyMasker）；
	// nil 表示无脱敏（热路径零开销）。map 由 WithFilter 构造后只读共享，
	// clone 原样携带（不拷贝）。
	attrMask map[string]struct{}
	// handler 非 nil 时走消费方注入的 slog.Handler，由其负责格式与输出；
	// nil 时走内置 JSON→stdout 路径（json.go）。构造后不可变，clone 时原样携带。
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
	return func(l *slogLogger) { l.Level.Store(level) }
}

// NewSlogLogger 创建基于 log/slog 的结构化 Logger。
// service 会作为 "service" 字段输出到每条日志。
//
// 级别优先级：WithLevel 显式参数 > LOG_LEVEL 环境变量 > 默认 LevelInfo。
// 未注入 WithHandler 时输出 JSON 到 stdout；注入后由该 Handler 决定格式。
func NewSlogLogger(service string, opts ...SlogOption) Logger {
	l := &slogLogger{BaseLogger: NewBaseLogger(service, LevelInfo)}
	l.Level.Store(ParseLogLevelOr(os.Getenv("LOG_LEVEL"), LevelInfo))
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
	// #164：「Snapshot + apply + 逐字段重建」上提到 BaseLogger.Derive；
	// slog 派生对 Attrs 做独立拷贝（历史口径，比 std/hlog 的共享切片更保守），
	// 经 Derive 的 apply 钩子保留。
	// #192/#194：组前缀快照（attrPrefixes/attrChains，内层切片与 groups 一样
	// 构造后不可变，只拷外层）与脱敏键集（attrMask 只读共享）原样携带。
	c := &slogLogger{
		groups:       append([]string(nil), l.groups...),
		attrPrefixes: append([]string(nil), l.attrPrefixes...),
		attrChains:   append([][]string(nil), l.attrChains...),
		attrMask:     l.attrMask,
		handler:      l.handler,
	}
	c.BaseLogger = l.Derive(func(b *BaseLogger) { b.Attrs = append([]Attr(nil), b.Attrs...) })
	return c
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
	// #192：挂载时刻固化组前缀快照——attrPrefixes/attrChains 与 Attrs 一一
	// 对齐（WithAttrs 是 Attrs 的唯一追加方）。此前输出时刻才对全部累计
	// 属性施加最终组前缀，WithAttrs 先于 WithGroup 的属性被错误卷入组，
	// 违反 slog「subsequent attributes」契约（WithGroup godoc：The keys of
	// all **subsequent** attributes should be qualified by the group）。
	prefix := ""
	if len(c.groups) > 0 {
		prefix = joinGroups(c.groups)
	}
	for range attrs {
		c.attrPrefixes = append(c.attrPrefixes, prefix)
		c.attrChains = append(c.attrChains, c.groups) // 共享底层切片（构造后不可变）
	}
	c.Attrs = append(c.Attrs, attrs...)
	return c
}

// WithAttrKeyMask 实现 AttrKeyMasker（#194）：把过滤层下沉的键集带进自身
// 与后续派生 clone，在渲染持久 Attrs 的全部路径（内置 JSON / 注入 Handler /
// printf）对命中键脱敏——FilterKey 对 WithFilter 之前 base 已挂的属性自此
// 生效。键集**并入**已有键集（WithFilter 嵌套包裹时多个键集叠加生效，
// TestFilter_MaskSink_DoubleWrap 钉住）；keys 只读共享（WithFilter 构造后
// 不可变）。
func (l *slogLogger) WithAttrKeyMask(keys map[string]struct{}) Logger {
	c := l.clone()
	c.attrMask = mergeMaskKeys(l.attrMask, keys)
	return c
}

// mergeMaskKeys 并入脱敏键集（#194）：无既有键集时直接共享入参 map（只读
// 契约），否则拷贝并集——入参不得被改写。
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

// WithGroup 返回带字段命名空间的子 logger（#75 #83 #99）。
//
// slog 语义：group 是结构化字段的命名空间，链式调用**累积嵌套**——
// WithGroup("a").WithGroup("b") 后用户字段落在 "a.b." 命名空间下（此前
// 单槽位静默只留最后一层）。**只限定其后的属性**（#192，slog.Handler
// WithGroup 契约「The keys of all subsequent attributes should be
// qualified」）：WithGroup 之前经 WithAttrs 挂载的属性保留挂载时刻的
// 命名空间，不随后续 WithGroup 挪位——
// `With("before",1).WithGroup("g").With("inside",2)` 输出 before 在组外、
// inside 在组内，与 stdlib slog 对拍一致。两条输出路径统一生效，且
// **框架字段（time/level/message/service/trace_id/caller）都不进组**：
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
// 否则输出一行结构化 JSON 到 stdout（编码见 json.go）。
func (l *slogLogger) emit(ctx context.Context, level Level, msg string, keysAndValues ...any) {
	l.emitAt(ctx, level, msg, 0, time.Time{}, keysAndValues...)
}

// emitAt 是统一输出入口（#84）：pc/ts 由桥接路径（SlogHandler 直通）传入
// slog 侧已解析好的调用点与时间戳——此前 Handle 忽略 r.PC/r.Time 走栈回溯，
// 第一个非 logx 帧是 stdlib log/slog.(*Logger).log，caller 恒错标到
// log/slog/logger.go 源码行。pc==0 / ts.IsZero() 表示未提供，在输出时自行
// 探测 caller、取当前时间（本地调用路径语义不变）。
func (l *slogLogger) emitAt(ctx context.Context, level Level, msg string, pc uintptr, ts time.Time, keysAndValues ...any) {
	ctx = CtxOrBackground(ctx) // #97：Ctx* 入参 nil ctx 统一兜底（ExtractTrace 与注入 Handler 都不再收到 nil）
	if !l.ShouldLog(level) {
		return
	}

	if l.handler != nil {
		l.emitToHandler(ctx, level, msg, pc, ts, keysAndValues...)
		return
	}
	l.emitBuiltinJSON(ctx, level, msg, pc, ts, keysAndValues...)
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
// bridgePC/bridgeTS 非 nil 时复用桥接侧已解析的调用点与时间戳（#84），
// 且不再做全栈回溯（#130）：此前无条件 realCallerOutsideLogx() 后再用
// bridgePC 覆盖——SlogHandler 桥接快路径（logxBridge 直通 r.PC，为消灭
// #87 实测 465-1515ns/op 的回溯成本而生）每条记录仍白付一次全栈扫描，
// 结果随即被丢弃。改为条件短路：bridgePC 非 0 走直通，仅本地路径回溯。
func (l *slogLogger) emitToHandler(ctx context.Context, level Level, msg string, bridgePC uintptr, bridgeTS time.Time, keysAndValues ...any) {
	var pc uintptr
	var caller string
	if bridgePC != 0 {
		pc = bridgePC
		caller = pcCaller(bridgePC)
	} else {
		pc, caller = realCallerOutsideLogx()
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

	// #194：脱敏键集下沉（WithFilter 检测 AttrKeyMasker 时设置）——持久
	// 属性与调用 kv 一并遮蔽，敏感值不得绕过过滤直渲。
	mask := l.attrMask
	if mask != nil && len(keysAndValues) > 0 {
		keysAndValues = maskKvs(mask, keysAndValues)
	}

	// 用户字段（Attr/kv）按**挂载时刻**的组链经 slog.Group 显式包裹（#192）：
	// 持久属性用各自的 attrChains 快照、调用 kv 用当前组链——WithAttrs 先于
	// WithGroup 的属性留在组外（slog「subsequent attributes」契约），WithGroup
	// 之后的属性照旧进组（#99 口径不变）；框架字段不进组。组由 slog.Group
	// 表达、注入的 Handler 原生渲染嵌套结构。
	attrs = append(attrs, buildNestedUserAttrs(l, mask, keysAndValues)...)

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

// buildNestedUserAttrs 构建注入 Handler 路径的用户字段（#192）：持久属性按
// 各自挂载时刻的组链、调用 kv 按当前组链，经 groupNest 合成嵌套结构。
func buildNestedUserAttrs(l *slogLogger, mask map[string]struct{}, keysAndValues []any) []slog.Attr {
	var n groupNest
	for i, a := range l.Attrs {
		v := logValue(a.Value)
		if mask != nil && matchMaskKey(mask, a.Key) {
			v = "***"
		}
		chain := l.groups // 防御：快照缺失时退回旧行为（整体进当前组链）
		if i < len(l.attrChains) {
			chain = l.attrChains[i]
		}
		n.add(chain, slog.Any(a.Key, v))
	}
	for i := 0; i+1 < len(keysAndValues); i += 2 {
		n.add(l.groups, slog.Any(keyString(keysAndValues[i]), logValue(keysAndValues[i+1]))) // #174 string 键直取
	}
	if len(keysAndValues)%2 == 1 {
		n.add(l.groups, slog.Any(keyString(keysAndValues[len(keysAndValues)-1]), "(MISSING)"))
	}
	return n.build()
}

// groupNest 按「挂载时刻组链」把属性流构建成嵌套 slog.Group 结构（#192）。
//
// 组链沿构造链单调延伸（WithGroup 只增不减），深层组链的属性合并进已开
// 的同名组——此前 emitToHandler 把全部累计属性整体包进最终组链，WithAttrs
// 先于 WithGroup 的属性被错误卷入组，与 stdlib 分叉（#192）。空组不落
// （stdlib 约定：JSONHandler/TextHandler 本就丢弃空组）。
type groupNest struct {
	root  []slog.Attr
	stack []groupFrame
}

type groupFrame struct {
	name  string
	attrs []slog.Attr
}

func (n *groupNest) add(chain []string, a slog.Attr) {
	for len(n.stack) > len(chain) {
		n.pop()
	}
	for len(n.stack) < len(chain) {
		n.stack = append(n.stack, groupFrame{name: chain[len(n.stack)]})
	}
	if len(n.stack) > 0 {
		top := &n.stack[len(n.stack)-1]
		top.attrs = append(top.attrs, a)
		return
	}
	n.root = append(n.root, a)
}

// pop 关闭最内层组：空组丢弃；非空组合成 GroupValue 挂进父级（或顶层）。
func (n *groupNest) pop() {
	f := n.stack[len(n.stack)-1]
	n.stack = n.stack[:len(n.stack)-1]
	if len(f.attrs) == 0 {
		return
	}
	ga := slog.Attr{Key: f.name, Value: slog.GroupValue(f.attrs...)}
	if len(n.stack) > 0 {
		top := &n.stack[len(n.stack)-1]
		top.attrs = append(top.attrs, ga)
		return
	}
	n.root = append(n.root, ga)
}

func (n *groupNest) build() []slog.Attr {
	for len(n.stack) > 0 {
		n.pop()
	}
	return n.root
}

// stampedAttrs 返回持久属性的降级面拷贝（仅 marshalFallback 冷路径调用，
// #193）：键按挂载时刻的组前缀固化（#192），值按脱敏键集遮蔽（#194）。
func (l *slogLogger) stampedAttrs() []Attr {
	if len(l.Attrs) == 0 {
		return nil
	}
	out := make([]Attr, len(l.Attrs))
	for i, a := range l.Attrs {
		v := logValue(a.Value)
		if l.attrMask != nil && matchMaskKey(l.attrMask, a.Key) {
			v = "***"
		}
		k := a.Key
		if i < len(l.attrPrefixes) {
			k = l.attrPrefixes[i] + a.Key
		} else if len(l.groups) > 0 {
			k = joinGroups(l.groups) + a.Key
		}
		out[i] = Attr{Key: k, Value: v}
	}
	return out
}

// logxBridge 是 slogBridgeSink 的实现（#84）：SlogHandler 桥接直通入口，
// 复用 slog 侧已解析的调用点 PC 与时间戳，不再栈回溯。
func (l *slogLogger) logxBridge(ctx context.Context, level Level, pc uintptr, ts time.Time, msg string, kv ...any) {
	l.emitAt(ctx, level, msg, pc, ts, kv...)
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
//
// Error() 的 panic 在 errString 内隔离（#137）：日志组件是最不该把调用方
// 打挂的一层——此前 panic 从日志调用点直接逃逸（一次日志调用打崩整个
// 请求/协程），且与 std 后端行为分裂（std 走 fmt，fmt 自己把 Error() 的
// panic 收敛成 %!v(PANIC=...) 字面量、日志调用正常返回）。
func logValue(v any) any {
	if err, ok := v.(error); ok {
		return errString(err)
	}
	return v
}

// errString 取 err.Error() 文本并对 panic 隔离（#137）：Error() 实现里会
// panic 的 error 值（从 map 取值后转换、断言失败后包装的类型、带 nil 内嵌
// 指针的类型等）降级为占位串，日志调用正常返回。范式对齐 core/chain
// reportIsolated：保住调用方 > 完整渲染；占位串里带上 panic 值，异常现场
// 仍可 grep 定位到是哪个 error 实现炸的。
func errString(err error) (s string) {
	defer func() {
		if r := recover(); r != nil {
			s = fmt.Sprintf("<error.String panicked: %v>", r)
		}
	}()
	return err.Error()
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
