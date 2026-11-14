package logx

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

// stdLogger 是基于标准库 log 的 Logger 实现。
type stdLogger struct {
	BaseLogger
	// attrMask 是过滤层下沉的持久属性脱敏键集（#194，AttrKeyMasker）；
	// nil 表示无脱敏。map 由 WithFilter 构造后只读共享，withBase 派生
	// 原样携带（不拷贝）。
	attrMask map[string]struct{}
	// groups 是 WithGroup 的组链（logx#222）：std 后端此前是空函数 no-op，
	// 组名无声消失——同一段代码在 slog 后端输出 user.password、std 后端
	// 平铺 password，字段布局按后端漂移且零信号（「静默丢弃比报错更贵」）。
	// 平铺前缀口径与 json/slog 后端的 joinGroups 一致。
	groups []string
	// attrPrefixes 与 Attrs 平行：每个持久属性**挂载时刻**的组前缀
	// （logx#222，slog 后端 attrPrefixes #192 同款）——WithAttrs 挂在
	// WithGroup 之前的属性保持顶层，之后的进组；快照缺失时（理论不可达）
	// 退回当前组前缀（json.go 同款防御）。
	attrPrefixes []string
}

// stdLogDynamicWriter 在**每次写入时**解析当前标准库 log 的输出目标
// （gormx 地板抬到 v0.8.5 后实测回归）：#78 把输出目标从包级 log.Printf
// 换成 log.New(os.Stderr, ...) 固化绑定后，消费方（gormx 的 PoolWarn/
// redact 测试）经 log.SetOutput(buf) 的经典捕获法全部捕空——私有 logger
// 绕过了 std log 的输出重定向。动态委托恢复该契约：
// log.SetOutput 照常拦截、缺省仍是 os.Stderr、flags=0 单时间戳不变。
//
// #111：委托写**借默认 logger 的锁**完成（log.Output 在持 log.std.mu 的
// 窗口内完成格式化+Write）。修复前走 log.Writer().Write(p)，只持
// stdWriter 自己的 mutex；而 stdlib 侧 log.Printf 写同一重定向目标时持
// log.std.mu——两把锁互不排斥 ⇒ logx std 行与 stdlib 日志并发写同一
// 非线程安全目标（bytes.Buffer 经典捕获形态）是 data race，-race 必报。
// stdlib 没有导出「取锁」API，唯一持锁跨 Write 的入口是 Logger.Output
// 本身 ⇒ 借道它恢复 #78 之前的串行契约。
//
// flags 面：p 已是 flags=0 格式化好的整行，默认 logger 的 LstdFlags
// 会在行首再叠秒级时间戳（#78 之前的老毛病）⇒ 借道窗口内把默认 logger
// 的 flags 钉为 0，写完立刻还原。调用方保证：本 Write 恒在 stdWriter.mu
// 之下（stdWriter 是包私有、只有本文件 log/logf 路径经其 Printf 触达），
// 钉零/还原不会与其它 logx 写交错。窗口内的第三方 log.Printf 会临时
// 失去时间戳前缀——zap.RedirectStdLog 同款取舍，窗口压到单行写出。
// 重定向目标若非线程安全（bytes.Buffer 等），经由本路径与 stdlib 写方
// 仍互斥（同一把 log.std.mu）；直接持有目标绕过两条 logger 的写法不受保护。
type stdLogDynamicWriter struct{}

func (stdLogDynamicWriter) Write(p []byte) (int, error) {
	prevFlags := log.Flags()
	log.SetFlags(0)
	defer log.SetFlags(prevFlags)
	if err := log.Output(2, string(p)); err != nil {
		return 0, err // Output 不回写字节数；失败按 0 报
	}
	return len(p), nil
}

// stdWriter 是 std 后端的私有输出器（#78）：本文件自带毫秒级 ts 前缀，
// 此前用包级 log.Printf——其默认 LstdFlags 会在行首再叠一个秒级时间戳
// （实测输出 "2026/11/06 22:53:19 2026-11-06 22:53:19.569 [INFO] ..."，
// 双时间戳且前者无毫秒）。私有 log.Logger 置 flags=0；输出目标动态委托
// 当前 log.Writer()（缺省 os.Stderr，见 stdLogDynamicWriter）。
// #111：默认目标下所有 std 行经 stdWriter.mu 串行、再借 log.std.mu 落盘
// ——与 stdlib 日志对同一目标全互斥。
var stdWriter = log.New(stdLogDynamicWriter{}, "", 0)

// StdOption 是 std 后端的函数选项（#167，与 SlogOption 同形）。
type StdOption func(*stdLogger)

// WithStdLevel 显式指定日志级别，优先级高于 LOG_LEVEL 环境变量。
// 构造后仍可 SetLevel 运行期调级。
func WithStdLevel(level Level) StdOption {
	return func(l *stdLogger) { l.Level.Store(level) }
}

// NewStdLoggerWith 以函数选项创建 std 后端（#167）：与 NewSlogLogger 同形，
// 三个后端构造器自此都有函数选项形态（签名面统一）。
//
// 级别优先级：WithStdLevel 显式选项 > LOG_LEVEL 环境变量 > 默认 LevelInfo
// （三后端文档口径一致）。
func NewStdLoggerWith(service string, opts ...StdOption) Logger {
	l := &stdLogger{BaseLogger: NewBaseLogger(service,
		ParseLogLevelOr(os.Getenv("LOG_LEVEL"), LevelInfo))} // #66：对齐 init 默认实例与 slog 后端
	for _, opt := range opts {
		if opt != nil {
			opt(l)
		}
	}
	return l
}

// NewStdLogger 创建标准库实现的 Logger（variadic 兼容形态，#167：内部转发
// NewStdLoggerWith，既有调用不受影响）。
// 无 level 参数时默认 LevelInfo（与文档及 NewSlogLogger 一致；
// 原先误用 LevelDebug=iota 零值，见 #11）。
// 传入 level[0] 可显式指定。
func NewStdLogger(service string, level ...Level) Logger {
	if len(level) == 0 {
		return NewStdLoggerWith(service)
	}
	return NewStdLoggerWith(service, WithStdLevel(level[0]))
}

func (l *stdLogger) withBase(apply func(cp *BaseLogger)) Logger {
	// #164：Snapshot + apply + 逐字段重建上提到 BaseLogger.Derive
	// #194：attrMask 只读共享（构造后不可变），派生原样携带
	return &stdLogger{
		BaseLogger: l.Derive(apply), attrMask: l.attrMask,
		groups: l.groups, attrPrefixes: l.attrPrefixes,
	}
}

// WithAttrKeyMask 实现 AttrKeyMasker（#194）：把过滤层下沉的键集带进自身
// 与后续派生，在渲染持久 Attrs 的全部路径（结构化 log 与 printf logf）对
// 命中键脱敏——FilterKey 对 WithFilter 之前 base 已挂的属性自此生效。
// 键集并入已有键集（嵌套包裹叠加，与 slog 后端同口径）；keys 只读共享。
func (l *stdLogger) WithAttrKeyMask(keys map[string]struct{}) Logger {
	c := l.withBase(func(cp *BaseLogger) {}).(*stdLogger)
	c.attrMask = mergeMaskKeys(l.attrMask, keys)
	return c
}

func (l *stdLogger) WithCtx(ctx context.Context) Logger {
	return l.withBase(func(cp *BaseLogger) { cp.Ctx = ctx })
}

func (l *stdLogger) WithService(name string) Logger {
	return l.withBase(func(cp *BaseLogger) { cp.Service = name })
}

// WithAttrs 返回携带固定属性的子 logger。
func (l *stdLogger) WithAttrs(attrs ...Attr) Logger {
	// logx#222：挂载时刻把当前组前缀记入 attrPrefixes（与 Attrs 平行）——
	// 组前缀只对「之后」挂载的属性生效（slog「subsequent attributes」契约）。
	gp := l.groupPrefix()
	np := make([]string, 0, len(l.attrPrefixes)+len(attrs))
	np = append(np, l.attrPrefixes...)
	for range attrs {
		np = append(np, gp)
	}
	// 直接 Derive（字段级重建不拷 Mutex）——经 withBase 再抽 .BaseLogger
	// 会按值复制含 sync.Mutex 的 BaseLogger（go vet copylocks）。
	return &stdLogger{
		BaseLogger: l.Derive(func(cp *BaseLogger) { cp.Attrs = cp.MergeAttrs(attrs...) }),
		attrMask:   l.attrMask,
		groups:     l.groups, attrPrefixes: np,
	}
}

// WithGroup 记录组链，输出按 joinGroups 平铺前缀（logx#222）：
// 此前是空函数 no-op——组名无声消失，与 slog 后端同代码输出 schema 不同
// （字段布局按后端漂移，依赖组路径的检索/脱敏静默失效）。零值/空组名
// 保持原样。
func (l *stdLogger) WithGroup(name string) Logger {
	if name == "" {
		return l
	}
	return &stdLogger{
		BaseLogger: l.Derive(func(*BaseLogger) {}), attrMask: l.attrMask,
		groups:       append(append([]string(nil), l.groups...), name),
		attrPrefixes: l.attrPrefixes,
	}
}

// buildPrefix 构造 std 后端的行前缀（#165）：log/logf 此前各持一份相同的
// 「毫秒时间戳 + 级别名 + service 前缀 + trace 分支」5 行构造，trace 提取
// 口径调整时要改两处。收敛到此共用。
func (l *stdLogger) buildPrefix(ctx context.Context, level Level) string {
	ts := time.Now().Format("2006-01-02 15:04:05.000")
	levelStr := LevelString(level)
	if traceID := ExtractTrace(ctx); traceID != "" {
		return fmt.Sprintf("%s [%s] [trace=%s] %s", ts, levelStr, traceID, l.Prefix())
	}
	return fmt.Sprintf("%s [%s] %s", ts, levelStr, l.Prefix())
}

func (l *stdLogger) log(level Level, ctx context.Context, msg string, keysAndValues ...any) {
	ctx = CtxOrBackground(ctx) // #97：Ctx* 入参 nil ctx 统一兜底
	if !l.ShouldLog(level) {
		return
	}
	prefix := l.buildPrefix(ctx, level)

	// #194：下沉键集非空时持久属性与调用 kv 一并脱敏——敏感值不得绕过
	// 过滤直渲（未命中零拷贝）。
	attrs := l.Attrs
	if l.attrMask != nil && len(attrs) > 0 {
		attrs = maskAttrsSlice(l.attrMask, attrs)
	}
	if l.attrMask != nil && len(keysAndValues) > 0 {
		keysAndValues = maskKvs(l.attrMask, keysAndValues)
	}
	// logx#223：emit 侧值截断（默认关闭）
	if a2, ok := truncateAttrs(attrs); ok {
		attrs = a2
	}
	if k2, ok := truncateKvs(keysAndValues); ok {
		keysAndValues = k2
	}

	// logx#222：组链平铺前缀——属性按挂载时刻前缀、调用 kv 按当前组链
	// 前缀（与 json/slog 后端 joinGroups 口径一致）。
	gp := l.groupPrefix()
	if gp != "" {
		keysAndValues = prefixKvKeys(keysAndValues, gp)
	}
	kvStr := formatAttrsGrouped(attrs, l.attrPrefixes, gp)
	if len(keysAndValues) > 0 {
		if s := FormatKeysAndValues(keysAndValues...); s != "" {
			if kvStr != "" {
				kvStr += " " + s
			} else {
				kvStr = s
			}
		}
	}
	if kvStr != "" {
		stdWriter.Printf("%s %s %s", prefix, msg, kvStr)
	} else {
		stdWriter.Printf("%s %s", prefix, msg)
	}
}

// groupPrefix 返回当前组链的平铺前缀；无组返回空串。
// ⚠️ joinGroups 对空输入也返回 "."（json 后端靠 len(groups)>0 守卫），
// std 侧调用一律走本方法（logx#222 实现期实测）。
func (l *stdLogger) groupPrefix() string {
	if len(l.groups) == 0 {
		return ""
	}
	return joinGroups(l.groups)
}

// formatAttrsGrouped 按挂载时刻前缀渲染持久属性（logx#222）：前缀取
// attrPrefixes[i]（WithAttrs 挂载时的组链），快照缺失（理论不可达——
// 构造期 Attrs 为空、之后只经 WithAttrs 增长）退回当前组前缀 gp
// （json.go 同款防御）。gp 与全部前缀为空时输出与 FormatAttrs 逐字节一致。
func formatAttrsGrouped(attrs []Attr, attrPrefixes []string, gp string) string {
	if len(attrs) == 0 {
		return ""
	}
	if gp == "" && len(attrPrefixes) == 0 {
		return FormatAttrs(attrs...)
	}
	var sb strings.Builder
	for i, a := range attrs {
		if i > 0 {
			sb.WriteByte(' ')
		}
		p := gp
		if i < len(attrPrefixes) {
			p = attrPrefixes[i]
		}
		sb.WriteString(p)
		sb.WriteString(a.Key)
		sb.WriteByte('=')
		fmt.Fprintf(&sb, "%v", a.Value)
	}
	return sb.String()
}

// prefixKvKeys 把调用 kv 的键加上组前缀（logx#222，拷贝不改入参——
// #220 同款「不持有调用方切片」纪律）。非 string 键经 keyString 归一
// （与 json/slog 平铺口径一致）。
func prefixKvKeys(kvs []any, gp string) []any {
	out := make([]any, len(kvs))
	copy(out, kvs)
	for i := 0; i < len(out); i += 2 {
		out[i] = gp + keyString(out[i])
	}
	return out
}

func (l *stdLogger) logf(level Level, ctx context.Context, format string, args ...any) {
	ctx = CtxOrBackground(ctx) // #97：Ctx* 入参 nil ctx 统一兜底
	if !l.ShouldLog(level) {
		return
	}
	prefix := l.buildPrefix(ctx, level)

	msg := fmt.Sprintf(format, args...)
	// printf 路径同样携带持久属性（#77）：此前 logf 不合并 l.Attrs——同一
	// logger 上 Info 带 request_id、Infof 不带，字段面按方法族分裂，依赖
	// 持久属性做审计/追踪的调用方在 printf 路径静默丢字段。
	// #165：属性拼装与结构化路径（log）统一走同族格式化函数，消除
	// printf/结构化两条路径的残留分叉（无 kv 参数时即纯属性串，输出不变）。
	// #194：下沉键集非空时持久属性同样脱敏（printf 无调用 kv 可脱敏，
	// #80 口径）。
	attrs := l.Attrs
	if l.attrMask != nil && len(attrs) > 0 {
		attrs = maskAttrsSlice(l.attrMask, attrs)
	}
	// logx#223：emit 侧值截断（默认关闭）
	if a2, ok := truncateAttrs(attrs); ok {
		attrs = a2
	}
	if kvStr := formatAttrsGrouped(attrs, l.attrPrefixes, l.groupPrefix()); kvStr != "" {
		stdWriter.Printf("%s %s %s", prefix, msg, kvStr)
		return
	}
	stdWriter.Printf("%s %s", prefix, msg)
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
