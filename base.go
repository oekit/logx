package logx

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// AtomicLevel 是 BaseLogger.Level 字段的原子级别存储（#171）。
//
// Why：ShouldLog 是逐条日志的热路径，Default() 又是全进程单实例——此前每次
// 判级对 BaseLogger.Mu 取互斥锁，跨核缓存行弹跳。Level 改走原子读后热路径
// 彻底免锁（setlevel_race_test 证明 Level 是 BaseLogger 唯一真正可变的字段，
// Attrs/Ctx 构造后不可变）。
//
// ⚠️ 有意用普通 int32 字段 + atomic.Load/StoreInt32 旧式原子操作，而非
// atomic.Int32：BaseLogger 的值拷贝（Snapshot/Derive 的逐字段重建）是既有
// 公共契约，atomic.Int32 内嵌 noCopy 会让 vet copylocks 把一切含 BaseLogger
// 的值拷贝标红；本类型的拷贝语义是「拷贝当下值、此后各自独立」（与 Snapshot
// 拷贝 Attrs 切片头同一档），旧式原子操作恰好表达这个语义。并发正确性不依赖
// 拷贝：全部读写在实例指针上进行，-race 下由 TestShouldLog_ConcurrentSetLevel
// 钉住。
type AtomicLevel struct{ v int32 }

// Load 返回当前级别（原子读，热路径免锁）。
func (a *AtomicLevel) Load() Level { return Level(atomic.LoadInt32(&a.v)) }

// Store 设置级别（原子写）。运行期调级请优先用 BaseLogger.SetLevel。
func (a *AtomicLevel) Store(level Level) { atomic.StoreInt32(&a.v, int32(level)) }

// String 实现 fmt.Stringer（对齐 LevelString）。
func (a *AtomicLevel) String() string { return LevelString(a.Load()) }

// newAtomicLevel 以初值构造 AtomicLevel（供 BaseLogger 字面量重建路径使用：
// 结构体字面量里无法对字段调方法，须先取值再进字面量）。
func newAtomicLevel(level Level) AtomicLevel { return AtomicLevel{v: int32(level)} }

// BaseLogger 包含所有 Logger 实现的公共字段和方法。
// 通过嵌入消除 hlog/std/slog 三个实现的重复代码。
//
// 子包（如 logx/hlog）可嵌入此结构体复用公共逻辑。
//
// ⚠️ 拷贝纪律：本结构体内含互斥锁与原子级别，**禁止整体值拷贝**（vet
// copylocks 同此口径）——派生一律走 Derive/Snapshot 的逐字段重建。
type BaseLogger struct {
	Service string
	// Level 是实例级别（#171 改为原子存储）：读用 Level.Load()，写用
	// SetLevel（运行期调级）或构造期赋初值。直接 Store 绕过 SetLevel 的
	// 后端联动（如 hlog 全局级别同步）时需自查。
	Level AtomicLevel
	Attrs []Attr
	Ctx   context.Context
	Mu    sync.Mutex
}

// NewBaseLogger 创建 BaseLogger 实例。
func NewBaseLogger(service string, level Level) BaseLogger {
	return BaseLogger{Service: service, Level: newAtomicLevel(level)}
}

// Level 日志级别
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	// LevelOff 是「完全静默」档：比 LevelError 更高 ⇒ 任何级别都不输出。
	//
	// 用于「保留结构化日志但本进程不落盘」的场景——既有四档里最严的
	// LevelError **仍会打 error**，而构造器 NewStdLogger 又要求编一个
	// service 名；把级别设到 LevelOff 才是真正的「关掉」（#41）。
	//
	// 纯新增：不改动既有四档的取值与行为。
	LevelOff
)

// levelStrings 日志级别名称数组。
var levelStrings = [5]string{"DEBUG", "INFO", "WARN", "ERROR", "OFF"}

// LevelString 返回日志级别的字符串表示。
func LevelString(level Level) string {
	if level >= 0 && int(level) < len(levelStrings) {
		return levelStrings[level]
	}
	return "UNKNOWN"
}

// Snapshot 在锁内拷贝公共字段。clone/With* 派生路径必须经它取值：
// Attrs/Ctx 的锁内快照读与并发 MergeAttrs 构成临界区；Level 已是原子量
// （#171），经 Load 取即时值。导出供子包（logx/hlog）嵌入使用。
func (b *BaseLogger) Snapshot() BaseLogger {
	b.Mu.Lock()
	defer b.Mu.Unlock()
	return BaseLogger{Service: b.Service, Level: newAtomicLevel(b.Level.Load()), Attrs: b.Attrs, Ctx: b.Ctx}
}

// Derive 返回应用 apply 后的派生 BaseLogger（#164）：std/hlog 的 withBase 与
// slog 的 clone 此前各持一份逐字相同的「Snapshot + apply + 逐字段重建」，
// 上提到此统一维护。逐字段重建的理由也只需注释一处：
//
//  1. 直接 BaseLogger: cp 会连同内部 Mutex 一起拷贝（vet lock copy 报错）；
//  2. Level 同理以 newAtomicLevel 重建（#171），派生实例的级别自此独立。
func (b *BaseLogger) Derive(apply func(*BaseLogger)) BaseLogger {
	cp := b.Snapshot()
	apply(&cp)
	return BaseLogger{Service: cp.Service, Level: newAtomicLevel(cp.Level.Load()), Attrs: cp.Attrs, Ctx: cp.Ctx}
}

// EffectiveCtx 返回绑定 ctx（无绑定则 context.Background()）。
func (b *BaseLogger) EffectiveCtx() context.Context {
	if b.Ctx != nil {
		return b.Ctx
	}
	return context.Background()
}

// CtxOrBackground 将 Ctx* 方法入参里的 nil ctx 兜底为 context.Background()
// （#97，对齐 EffectiveCtx 的既有口径）。错误处理路径上 ctx 极易为 nil
// （var ctx context.Context; if err != nil { log.CtxError(ctx, ...) }），
// 不兜底会把 nil 透传给 trace 提取器或注入的 slog.Handler 而崩进程。
//
// #168 导出：hlog 子包此前因本函数未导出而复制了一份逐字相同的本地副本，
// 现统一复用此处。
func CtxOrBackground(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

// keyString 将 kv 键归一为 string（#174）。绝大多数 kv 键就是 string，
// 直取零分配；非 string 键才回退 %v 反射格式化，与既有键口径逐字节一致
// （内置 JSON 路径 / 注入 Handler 路径 / hlog FormatKeysAndValues /
// filterKvs / errattrs 五处同型热路径统一走此处）。
func keyString(v any) string {
	if k, ok := v.(string); ok {
		return k
	}
	return fmt.Sprintf("%v", v)
}

// KeyString 将 kv 键归一为 string（keyString 的导出形态，#194）：hlog 子包
// 的下沉脱敏（AttrKeyMasker）需要与根包一致的键口径——导出理由与
// CtxOrBackground 的 #168 导出相同（子包不可触达未导出符号，与其复制一份
// 逐字相同的本地副本不如单源导出）。
func KeyString(v any) string { return keyString(v) }

// ShouldLog 检查给定级别是否应被记录。
//
// #171：热路径免锁——Level 走原子读，不再对 Mu 取互斥锁（Default() 全进程
// 单实例，逐条日志 lock/unlock 的跨核缓存行弹跳是实测争用源）。
func (b *BaseLogger) ShouldLog(level Level) bool {
	return level >= b.Level.Load()
}

// Enabled 判定给定级别当前是否会被输出——LevelEnabler 接口的 BaseLogger 实现。
// 嵌入 BaseLogger 的后端（std/slog/hlog 等）自动获得该方法。
func (b *BaseLogger) Enabled(level Level) bool {
	return b.ShouldLog(level)
}

// SetLevel 设置该 logger 实例的日志级别。
//
// #171：Level 是原子量，写路径不再需要 Mu（并发读经 atomic Load 可见）。
func (b *BaseLogger) SetLevel(level Level) {
	b.Level.Store(level)
}

// Prefix 返回 service 前缀（如 "[my-service]"）。
func (b *BaseLogger) Prefix() string {
	if b.Service != "" {
		return fmt.Sprintf("[%s]", b.Service)
	}
	return ""
}

// MergeAttrs 合并已有属性和新属性，返回新的切片。
func (b *BaseLogger) MergeAttrs(attrs ...Attr) []Attr {
	b.Mu.Lock()
	defer b.Mu.Unlock()
	merged := make([]Attr, 0, len(b.Attrs)+len(attrs))
	merged = append(merged, b.Attrs...)
	merged = append(merged, attrs...)
	return merged
}

// FormatAttrs 将 Attr 列表格式化为 "k=v k=v" 字符串。
func FormatAttrs(attrs ...Attr) string {
	if len(attrs) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, a := range attrs {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(a.Key)
		sb.WriteByte('=')
		fmt.Fprintf(&sb, "%v", a.Value)
	}
	return sb.String()
}
