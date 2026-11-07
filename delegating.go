package logx

import (
	"context"
	"sync/atomic"
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
// WithService/WithAttrs/WithGroup/WithCtx 返回的子 logger 同样保持懒解析。
//
// SetLevel 是**实例级**的（#81）：记录在本 wrapper 上、只影响本实例的下限，
// 不再转发修改全局默认 logger——此前一个 SDK 包级 Delegating 变量调
// SetLevel(LevelDebug) 会打开全进程日志，多组件各自调级互相覆盖。
// 详见 SetLevel 的说明。
func Delegating(component string) Logger {
	d := delegatingLogger{resolve: Default, override: new(atomic.Int32)}
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
	// override 是实例级级别下限（#81）：SetLevel 写入这里。delegating 是
	// 值语义（接口持有值拷贝），覆盖状态经共享单元对同一实例的所有拷贝
	// 可见；With* 派生时换新单元（子 logger 独立配置，与后端行为一致）。
	// 存 Level+1（0 表示未设置，区分未设置与 LevelDebug 零值）。
	override *atomic.Int32
}

// pass 实例级下限预检：已设置下限且事件级别低于下限时提前丢弃。
func (d delegatingLogger) pass(level Level) bool {
	if v := d.override.Load(); v != 0 && Level(v-1) > level {
		return false
	}
	return true
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

// 子 logger 构建（返回的仍是懒解析 logger；覆盖单元换新，子 logger 独立配置）

func (d delegatingLogger) WithCtx(ctx context.Context) Logger {
	return delegatingLogger{resolve: func() Logger { return d.resolve().WithCtx(ctx) }, override: new(atomic.Int32)}
}

func (d delegatingLogger) WithService(name string) Logger {
	return delegatingLogger{resolve: func() Logger { return d.resolve().WithService(name) }, override: new(atomic.Int32)}
}

func (d delegatingLogger) WithAttrs(attrs ...Attr) Logger {
	return delegatingLogger{resolve: func() Logger { return d.resolve().WithAttrs(attrs...) }, override: new(atomic.Int32)}
}

func (d delegatingLogger) WithGroup(name string) Logger {
	return delegatingLogger{resolve: func() Logger { return d.resolve().WithGroup(name) }, override: new(atomic.Int32)}
}

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
	d.override.Store(int32(level) + 1)
}
