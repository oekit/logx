package logx

import (
	"context"
)

// Delegating 返回一个懒解析的 Logger：每次调用时才解析当前 Default()，
// 因此消费方在 main() 中调用 SetDefault 后，本 logger 自动跟随新实现。
//
// SDK 包的包级 logger 应使用本函数，而不是直接捕获 Default()：
//
//	// 错误：包级 var 在 init 时求值，永远绑定 init 时刻的默认 logger，
//	// 消费方随后的 SetDefault 对其无效。
//	var log = logx.Default()
//
//	// 正确：调用时才解析；component 以 component=<name> 属性标识来源子系统。
//	var log = logx.Delegating("mypkg")
//
// component 为该包/子系统的归属名；传空串表示不打 component 属性。
// service name 属于消费方，包归属统一用 component 属性标识。
// WithService/WithAttrs/WithGroup/WithCtx 返回的子 logger 同样保持懒解析，
// 并**继承派生时刻**的实例级级别下限（#101，与 std/slog/hlog 后端 clone
// 拷 Level 的继承语义对齐；派生之后父子各自独立调级）。
//
// SetLevel 是**实例级**的（#81）：记录在本 wrapper 上、只影响本实例的下限，
// 不再转发修改全局默认 logger——此前一个 SDK 包级 Delegating 变量调
// SetLevel(LevelDebug) 会打开全进程日志，多组件各自调级互相覆盖。
// 详见 SetLevel 的说明。
func Delegating(component string) Logger {
	d := delegatingLogger{resolve: Default, gate: &levelGate{}}
	if component != "" {
		name := component
		d.resolve = func() Logger {
			return Default().WithAttrs(Attr{Key: "component", Value: name})
		}
	}
	return d
}

// delegatingLogger 在每次调用时通过 resolve 解析目标 Logger 并转发，
// 自身不持有任何后端状态；唯一的本地状态是实例级级别下限（#81）。
type delegatingLogger struct {
	resolve func() Logger
	// gate 是实例级级别下限（#81）：SetLevel 写入这里。delegating 是
	// 值语义（接口持有值拷贝），覆盖状态经共享单元对同一实例的所有拷贝
	// 可见；With* 派生时把当前下限值拷贝给子实例（#101：继承派生时刻的
	// 下限、派生后各自独立调级——与 std/slog/hlog 后端 clone/Snapshot 拷
	// Level 的继承语义对齐；此前换新空单元，父实例收紧后子 logger 静默
	// 回默认档，「与后端行为一致」的旧注释与三个后端的实际行为相反）。
	// 编码约定（存 Level+1、0=未设置）与 pass/set/derive 实现收敛在
	// wrapper.go 的 levelGate（#162），与 filterLogger 共用。
	gate *levelGate
}

// deriveOverride 派生时继承父实例的级别下限（#101）：拷贝当前值到新单元，
// 派生后父子各自独立（后端 clone 拷 Level 同语义）。
func (d delegatingLogger) deriveOverride() *levelGate {
	return d.gate.deriveChild()
}

// pass 实例级下限预检：已设置下限且事件级别低于下限时提前丢弃。
func (d delegatingLogger) pass(level Level) bool {
	return d.gate.pass(level)
}

// Enabled 判定当前解析目标是否输出该级别——LevelEnabler 透传，并叠加
// 实例级下限（#81）。目标未实现 LevelEnabler 时保守返回 true。
func (d delegatingLogger) Enabled(level Level) bool {
	if !d.pass(level) {
		return false
	}
	if le, ok := d.resolve().(LevelEnabler); ok {
		return le.Enabled(level)
	}
	return true
}

// 结构化风格方法

func (d delegatingLogger) Debug(msg string, keysAndValues ...any) {
	if !d.pass(LevelDebug) {
		return
	}
	d.resolve().Debug(msg, keysAndValues...)
}

func (d delegatingLogger) Info(msg string, keysAndValues ...any) {
	if !d.pass(LevelInfo) {
		return
	}
	d.resolve().Info(msg, keysAndValues...)
}

func (d delegatingLogger) Warn(msg string, keysAndValues ...any) {
	if !d.pass(LevelWarn) {
		return
	}
	d.resolve().Warn(msg, keysAndValues...)
}

func (d delegatingLogger) Error(msg string, keysAndValues ...any) {
	if !d.pass(LevelError) {
		return
	}
	d.resolve().Error(msg, keysAndValues...)
}

// printf 风格方法

func (d delegatingLogger) Debugf(format string, args ...any) {
	if !d.pass(LevelDebug) {
		return
	}
	d.resolve().Debugf(format, args...)
}

func (d delegatingLogger) Infof(format string, args ...any) {
	if !d.pass(LevelInfo) {
		return
	}
	d.resolve().Infof(format, args...)
}

func (d delegatingLogger) Warnf(format string, args ...any) {
	if !d.pass(LevelWarn) {
		return
	}
	d.resolve().Warnf(format, args...)
}

func (d delegatingLogger) Errorf(format string, args ...any) {
	if !d.pass(LevelError) {
		return
	}
	d.resolve().Errorf(format, args...)
}

// 带 Context 的结构化风格方法

func (d delegatingLogger) CtxDebug(ctx context.Context, msg string, keysAndValues ...any) {
	if !d.pass(LevelDebug) {
		return
	}
	d.resolve().CtxDebug(ctx, msg, keysAndValues...)
}

func (d delegatingLogger) CtxInfo(ctx context.Context, msg string, keysAndValues ...any) {
	if !d.pass(LevelInfo) {
		return
	}
	d.resolve().CtxInfo(ctx, msg, keysAndValues...)
}

func (d delegatingLogger) CtxWarn(ctx context.Context, msg string, keysAndValues ...any) {
	if !d.pass(LevelWarn) {
		return
	}
	d.resolve().CtxWarn(ctx, msg, keysAndValues...)
}

func (d delegatingLogger) CtxError(ctx context.Context, msg string, keysAndValues ...any) {
	if !d.pass(LevelError) {
		return
	}
	d.resolve().CtxError(ctx, msg, keysAndValues...)
}

// 带 Context 的 printf 风格方法

func (d delegatingLogger) CtxDebugf(ctx context.Context, format string, args ...any) {
	if !d.pass(LevelDebug) {
		return
	}
	d.resolve().CtxDebugf(ctx, format, args...)
}

func (d delegatingLogger) CtxInfof(ctx context.Context, format string, args ...any) {
	if !d.pass(LevelInfo) {
		return
	}
	d.resolve().CtxInfof(ctx, format, args...)
}

func (d delegatingLogger) CtxWarnf(ctx context.Context, format string, args ...any) {
	if !d.pass(LevelWarn) {
		return
	}
	d.resolve().CtxWarnf(ctx, format, args...)
}

func (d delegatingLogger) CtxErrorf(ctx context.Context, format string, args ...any) {
	if !d.pass(LevelError) {
		return
	}
	d.resolve().CtxErrorf(ctx, format, args...)
}

// 子 logger 构建（返回的仍是懒解析 logger；级别下限继承派生时刻的值，
// 派生后独立调级——#101）

func (d delegatingLogger) WithCtx(ctx context.Context) Logger {
	return delegatingLogger{resolve: func() Logger { return d.resolve().WithCtx(ctx) }, gate: d.deriveOverride()}
}

func (d delegatingLogger) WithService(name string) Logger {
	return delegatingLogger{resolve: func() Logger { return d.resolve().WithService(name) }, gate: d.deriveOverride()}
}

func (d delegatingLogger) WithAttrs(attrs ...Attr) Logger {
	return delegatingLogger{resolve: func() Logger { return d.resolve().WithAttrs(attrs...) }, gate: d.deriveOverride()}
}

func (d delegatingLogger) WithGroup(name string) Logger {
	return delegatingLogger{resolve: func() Logger { return d.resolve().WithGroup(name) }, gate: d.deriveOverride()}
}

// Unwrap 暴露当前解析目标（#106）：IsHlogBacked 沿 Unwrap 链向下钻取，
// Delegating 包装（如 SDK 包级 logger）落到 hlog 后端时递归防御仍生效。
// 解析目标动态变化（SetDefault 后跟随），钻取取当下值。
func (d delegatingLogger) Unwrap() Logger { return d.resolve() }

// SetLevel 实例级调级（#81）：下限记录在本 wrapper 上，**不再转发修改全局
// 默认 logger**——此前直接 resolve().SetLevel(level)，一个 SDK 包级
// Delegating 变量调 SetLevel(LevelDebug) 会打开全进程日志，多组件各自调级
// 互相覆盖，与「With* 返回子 logger 独立配置」的直觉相反。
//
// 边界（诚实声明）：本下限与解析目标自身的级别取**更严者**生效——
// wrapper 只能在目标级别之上收紧（如全局 Info 时本实例收窄到 Error），
// 不能放宽（目标自己的 ShouldLog 仍拦截）。要放宽某条链路的输出，需对
// Default()（或对应后端实例）本身调级，那是全局/实例操作，delegating 不代劳。
func (d delegatingLogger) SetLevel(level Level) {
	d.gate.set(level)
}
