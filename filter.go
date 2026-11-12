package logx

import (
	"context"
	"fmt"
	"strings"
)

// FilterOption 配置 WithFilter 的过滤行为。
type FilterOption func(*filterConfig)

type filterConfig struct {
	// keys 中命中的 kv 键，值脱敏为 "***"。
	keys map[string]struct{}
	// drop 返回 true 时整条记录被丢弃（在脱敏之后评估）。
	drop func(msg string, kvs []any) bool
}

// FilterKey 把命中 key 的 kv 值替换为 "***"。键名匹配大小写不敏感（#85，
// EqualFold：HTTP 头的规范键名是 Authorization/X-Api-Key，安全控制不应要求
// 消费方逐大小写枚举——此前配置 FilterKey("authorization") 时 "Authorization"
// 键的值原样落盘），含点路径字面量，如 "user.password"；非 string 键先按
// %v 归一成键名再比对（与后端 emit 侧的键口径一致——emit 侧会
// fmt.Sprintf("%v") 稳定成键，此前非 string 键完全不可脱敏）。
//
// 注意：FilterKey 只作用于 kv/Attr 的**值**；printf 路径无键名，不脱敏
// （见 WithFilter 的警告，#80）。
func FilterKey(keys ...string) FilterOption {
	return func(c *filterConfig) {
		if c.keys == nil {
			c.keys = make(map[string]struct{}, len(keys))
		}
		for _, k := range keys {
			c.keys[k] = struct{}{}
		}
	}
}

// matchKey 判定键名是否命中过滤键（#85）：大小写不敏感。键集通常只有
// 个位数条目，线性 EqualFold 比下小写归一的 map 查找省一次分配。
func (c *filterConfig) matchKey(key string) bool {
	for k := range c.keys {
		if strings.EqualFold(k, key) {
			return true
		}
	}
	return false
}

// FilterFunc 命中即丢弃整条记录（在脱敏之后评估）。
func FilterFunc(fn func(msg string, kvs []any) bool) FilterOption {
	return func(c *filterConfig) { c.drop = fn }
}

// WithFilter 返回带脱敏/丢弃能力的子 logger。
//
// 过滤在 CoreLogger 四方法与 Ctx*/Fmt* 全路径生效：kv 值按 FilterKey 脱敏、
// 记录按 FilterFunc 丢弃。WithAttrs/WithGroup/WithCtx 派生的子 logger 保持
// 相同过滤规则。典型用法（防止敏感信息进日志）：
//
//	l := logx.WithFilter(logx.Default(), logx.FilterKey("password", "token"))
//
// ⚠️ printf 路径（Debugf/Infof/... 与 Ctx*f）**不做 FilterKey 脱敏**，只做
// FilterFunc 丢弃评估（#80）：printf 参数只有格式化值、没有键名，脱敏无从
// 匹配——格式化串里的 password/token 会原样落盘。敏感数据一律走 kv
// 结构化风格（Info("login", "password", pw)），禁止经 printf 格式化输出。
func WithFilter(l Logger, opts ...FilterOption) Logger {
	if l == nil {
		panic("logx: WithFilter called with nil logger")
	}
	cfg := &filterConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	return &filterLogger{base: l, cfg: cfg, gate: &levelGate{}}
}

type filterLogger struct {
	base Logger
	cfg  *filterConfig
	// gate 是实例级级别下限（#81）：SetLevel 写入这里，不透传共享的
	// base——此前 filterLogger.SetLevel 直接改 base（base 可能是全局默认
	// logger 或父 logger），级别调整半径远大于「子 logger 独立配置」的直觉。
	// With* 派生时把当前下限值拷贝给子实例（#101：继承派生时刻的下限、
	// 派生后各自独立——与 std/slog/hlog 后端 clone 拷 Level 对齐；此前
	// 派生归零，父实例收紧后子 logger 静默回默认档）。
	// 编码约定（存 Level+1、0=未设置）与 pass/set/derive 实现收敛在
	// wrapper.go 的 levelGate（#162），与 delegatingLogger 共用。
	gate *levelGate
}

// derive 派生子 logger 时继承父实例的级别下限（#101）：拷贝当前值，
// 派生后父子各自独立（后端 clone 拷 Level 同语义）。
func (f *filterLogger) derive(child *filterLogger) *filterLogger {
	child.gate = f.gate.deriveChild()
	return child
}

// pass 实例级下限预检：已设置下限且事件级别低于下限时提前丢弃
// （base 自身的级别过滤仍生效，有效级别取两者更严者）。
func (f *filterLogger) pass(level Level) bool {
	return f.gate.pass(level)
}

// filterKvs 返回脱敏后的 kv 切片（不入参原切片）。
func (f *filterLogger) filterKvs(kvs []any) []any {
	if len(f.cfg.keys) == 0 || len(kvs) == 0 {
		return kvs
	}
	out := make([]any, len(kvs))
	copy(out, kvs)
	for i := 0; i+1 < len(out); i += 2 {
		// #85：键统一按 %v 归一（非 string 键与 emit 侧键口径一致），
		// 命中判定大小写不敏感；#174 string 键直取免反射。
		if f.cfg.matchKey(keyString(out[i])) {
			out[i+1] = "***"
		}
	}
	return out
}

// dropped 报告记录是否应被丢弃。
func (f *filterLogger) dropped(msg string, kvs []any) bool {
	return f.cfg.drop != nil && f.cfg.drop(msg, kvs)
}

// droppedf 评估 printf 路径的丢弃判定（#169）：FilterFunc 未配置时**不执行**
// fmt.Sprintf。此前 Sprintf 是 dropped 的实参、先于其内部的 drop != nil 短路
// 无条件求值——只配 FilterKey（godoc 给的典型用法）时每次 Ctx*f 都白付一次
// 完整格式化，随后 base.Debugf 再格式化第二次（双重格式化 + 双重分配）。
func (f *filterLogger) droppedf(format string, args ...any) bool {
	if f.cfg.drop == nil {
		return false
	}
	return f.cfg.drop(fmt.Sprintf(format, args...), nil)
}

// 四组日志方法前置实例级下限预检（#81）：SetLevel 记录在 wrapper 自身，
// 低于下限的事件在脱敏/丢弃评估之前提前丢弃。

func (f *filterLogger) Debug(msg string, kvs ...any) {
	if !f.pass(LevelDebug) {
		return
	}
	kvs = f.filterKvs(kvs)
	if f.dropped(msg, kvs) {
		return
	}
	f.base.Debug(msg, kvs...)
}

func (f *filterLogger) Info(msg string, kvs ...any) {
	if !f.pass(LevelInfo) {
		return
	}
	kvs = f.filterKvs(kvs)
	if f.dropped(msg, kvs) {
		return
	}
	f.base.Info(msg, kvs...)
}

func (f *filterLogger) Warn(msg string, kvs ...any) {
	if !f.pass(LevelWarn) {
		return
	}
	kvs = f.filterKvs(kvs)
	if f.dropped(msg, kvs) {
		return
	}
	f.base.Warn(msg, kvs...)
}

func (f *filterLogger) Error(msg string, kvs ...any) {
	if !f.pass(LevelError) {
		return
	}
	kvs = f.filterKvs(kvs)
	if f.dropped(msg, kvs) {
		return
	}
	f.base.Error(msg, kvs...)
}

func (f *filterLogger) Debugf(format string, args ...any) {
	if !f.pass(LevelDebug) {
		return
	}
	if f.droppedf(format, args...) {
		return
	}
	f.base.Debugf(format, args...)
}

func (f *filterLogger) Infof(format string, args ...any) {
	if !f.pass(LevelInfo) {
		return
	}
	if f.droppedf(format, args...) {
		return
	}
	f.base.Infof(format, args...)
}

func (f *filterLogger) Warnf(format string, args ...any) {
	if !f.pass(LevelWarn) {
		return
	}
	if f.droppedf(format, args...) {
		return
	}
	f.base.Warnf(format, args...)
}

func (f *filterLogger) Errorf(format string, args ...any) {
	if !f.pass(LevelError) {
		return
	}
	if f.droppedf(format, args...) {
		return
	}
	f.base.Errorf(format, args...)
}

func (f *filterLogger) CtxDebug(ctx context.Context, msg string, kvs ...any) {
	if !f.pass(LevelDebug) {
		return
	}
	kvs = f.filterKvs(kvs)
	if f.dropped(msg, kvs) {
		return
	}
	f.base.CtxDebug(ctx, msg, kvs...)
}

func (f *filterLogger) CtxInfo(ctx context.Context, msg string, kvs ...any) {
	if !f.pass(LevelInfo) {
		return
	}
	kvs = f.filterKvs(kvs)
	if f.dropped(msg, kvs) {
		return
	}
	f.base.CtxInfo(ctx, msg, kvs...)
}

func (f *filterLogger) CtxWarn(ctx context.Context, msg string, kvs ...any) {
	if !f.pass(LevelWarn) {
		return
	}
	kvs = f.filterKvs(kvs)
	if f.dropped(msg, kvs) {
		return
	}
	f.base.CtxWarn(ctx, msg, kvs...)
}

func (f *filterLogger) CtxError(ctx context.Context, msg string, kvs ...any) {
	if !f.pass(LevelError) {
		return
	}
	kvs = f.filterKvs(kvs)
	if f.dropped(msg, kvs) {
		return
	}
	f.base.CtxError(ctx, msg, kvs...)
}

func (f *filterLogger) CtxDebugf(ctx context.Context, format string, args ...any) {
	if !f.pass(LevelDebug) {
		return
	}
	if f.droppedf(format, args...) {
		return
	}
	f.base.CtxDebugf(ctx, format, args...)
}

func (f *filterLogger) CtxInfof(ctx context.Context, format string, args ...any) {
	if !f.pass(LevelInfo) {
		return
	}
	if f.droppedf(format, args...) {
		return
	}
	f.base.CtxInfof(ctx, format, args...)
}

func (f *filterLogger) CtxWarnf(ctx context.Context, format string, args ...any) {
	if !f.pass(LevelWarn) {
		return
	}
	if f.droppedf(format, args...) {
		return
	}
	f.base.CtxWarnf(ctx, format, args...)
}

func (f *filterLogger) CtxErrorf(ctx context.Context, format string, args ...any) {
	if !f.pass(LevelError) {
		return
	}
	if f.droppedf(format, args...) {
		return
	}
	f.base.CtxErrorf(ctx, format, args...)
}

// Builder 方法：派生子 logger 保持过滤规则，并继承派生时刻的实例级
// 级别下限（#101，派生后独立调级）。

func (f *filterLogger) WithCtx(ctx context.Context) Logger {
	return f.derive(&filterLogger{base: f.base.WithCtx(ctx), cfg: f.cfg})
}

func (f *filterLogger) WithService(name string) Logger {
	return f.derive(&filterLogger{base: f.base.WithService(name), cfg: f.cfg})
}

func (f *filterLogger) WithAttrs(attrs ...Attr) Logger {
	return f.derive(&filterLogger{base: f.base.WithAttrs(f.maskAttrs(attrs)...), cfg: f.cfg})
}

// maskAttrs 把命中过滤键的属性值替换为 "***"（脱敏后再交给 base）。
//
// 不脱敏的话，WithAttrs 挂上去的属性会**原样**进日志：过滤只作用于「每次
// 调用传进来的 kv」，WithAttrs 挂的属性完全绕过。WithFilter 的 godoc 明确
// 承诺「WithAttrs/WithGroup/WithCtx 派生的子 logger 保持相同过滤规则」⇒
// 旧实现是**谎报**：敏感信息照常落盘（实测：WithFilter(base, FilterKey("password"))
// 之后 WithAttrs(Attr{Key:"password", Value:"hunter2"}) 会打出 password=hunter2）。
func (f *filterLogger) maskAttrs(attrs []Attr) []Attr {
	if len(f.cfg.keys) == 0 || len(attrs) == 0 {
		return attrs
	}
	out := make([]Attr, len(attrs))
	copy(out, attrs)
	// #85：键匹配大小写不敏感，与 filterKvs 同一口径。
	for i := range out {
		if f.cfg.matchKey(out[i].Key) {
			out[i].Value = "***"
		}
	}
	return out
}

func (f *filterLogger) WithGroup(name string) Logger {
	return f.derive(&filterLogger{base: f.base.WithGroup(name), cfg: f.cfg})
}

// Enabled 判定脱敏后端是否输出该级别——LevelEnabler 实现（#76）。
// 此前 filterLogger 未实现该接口，logx.Enabled 走类型断言失败分支恒
// true：被级别过滤掉的事件仍付出全部序列化/脱敏成本，热路径守卫失效。
// 实例级下限（#81）同样参与判定。
func (f *filterLogger) Enabled(level Level) bool {
	if !f.pass(level) {
		return false
	}
	return Enabled(f.base, level)
}

// Unwrap 暴露被包装的底座 logger（#106）：IsHlogBacked 沿 Unwrap 链向下
// 钻取，WithFilter 包一层后 kitexlog.NewLogger 的 hlog 递归防御仍生效——
// 底座不是 hlog 后端时断言照旧失败（不产生误报）。
func (f *filterLogger) Unwrap() Logger { return f.base }

// SetLevel 实例级调级（#81）：下限记录在本 wrapper 上，**不再透传共享的
// base**——此前直接 base.SetLevel(level)，而 base 可能是全局默认 logger 或
// 父 logger：WithFilter 包一层后调 SetLevel 会改掉整棵树的级别，多组件
// 各自调级互相覆盖。
//
// 边界（与 Delegating.SetLevel 同一声明）：本下限与 base 自身级别取更严者
// 生效——wrapper 只能收紧，不能放宽（base 自己的 ShouldLog 仍拦截）。
func (f *filterLogger) SetLevel(level Level) {
	f.gate.set(level)
}
