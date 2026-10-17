package logx

import "context"

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
func Delegating(component string) Logger {
	d := delegatingLogger{resolve: Default}
	if component != "" {
		name := component
		d.resolve = func() Logger {
			return Default().WithAttrs(Attr{Key: "component", Value: name})
		}
	}
	return d
}

// delegatingLogger 在每次调用时通过 resolve 解析目标 Logger 并转发，
// 自身不持有任何后端状态。
type delegatingLogger struct {
	resolve func() Logger
}

// Enabled 判定当前解析目标是否输出该级别——LevelEnabler 透传。
// 目标未实现 LevelEnabler 时保守返回 true。
func (d delegatingLogger) Enabled(level Level) bool {
	if le, ok := d.resolve().(LevelEnabler); ok {
		return le.Enabled(level)
	}
	return true
}

// 结构化风格方法

func (d delegatingLogger) Debug(msg string, keysAndValues ...any) {
	d.resolve().Debug(msg, keysAndValues...)
}

func (d delegatingLogger) Info(msg string, keysAndValues ...any) {
	d.resolve().Info(msg, keysAndValues...)
}

func (d delegatingLogger) Warn(msg string, keysAndValues ...any) {
	d.resolve().Warn(msg, keysAndValues...)
}

func (d delegatingLogger) Error(msg string, keysAndValues ...any) {
	d.resolve().Error(msg, keysAndValues...)
}

// printf 风格方法

func (d delegatingLogger) Debugf(format string, args ...any) {
	d.resolve().Debugf(format, args...)
}

func (d delegatingLogger) Infof(format string, args ...any) {
	d.resolve().Infof(format, args...)
}

func (d delegatingLogger) Warnf(format string, args ...any) {
	d.resolve().Warnf(format, args...)
}

func (d delegatingLogger) Errorf(format string, args ...any) {
	d.resolve().Errorf(format, args...)
}

// 带 Context 的结构化风格方法

func (d delegatingLogger) CtxDebug(ctx context.Context, msg string, keysAndValues ...any) {
	d.resolve().CtxDebug(ctx, msg, keysAndValues...)
}

func (d delegatingLogger) CtxInfo(ctx context.Context, msg string, keysAndValues ...any) {
	d.resolve().CtxInfo(ctx, msg, keysAndValues...)
}

func (d delegatingLogger) CtxWarn(ctx context.Context, msg string, keysAndValues ...any) {
	d.resolve().CtxWarn(ctx, msg, keysAndValues...)
}

func (d delegatingLogger) CtxError(ctx context.Context, msg string, keysAndValues ...any) {
	d.resolve().CtxError(ctx, msg, keysAndValues...)
}

// 带 Context 的 printf 风格方法

func (d delegatingLogger) CtxDebugf(ctx context.Context, format string, args ...any) {
	d.resolve().CtxDebugf(ctx, format, args...)
}

func (d delegatingLogger) CtxInfof(ctx context.Context, format string, args ...any) {
	d.resolve().CtxInfof(ctx, format, args...)
}

func (d delegatingLogger) CtxWarnf(ctx context.Context, format string, args ...any) {
	d.resolve().CtxWarnf(ctx, format, args...)
}

func (d delegatingLogger) CtxErrorf(ctx context.Context, format string, args ...any) {
	d.resolve().CtxErrorf(ctx, format, args...)
}

// 子 logger 构建（返回的仍是懒解析 logger）

func (d delegatingLogger) WithCtx(ctx context.Context) Logger {
	return delegatingLogger{resolve: func() Logger { return d.resolve().WithCtx(ctx) }}
}

func (d delegatingLogger) WithService(name string) Logger {
	return delegatingLogger{resolve: func() Logger { return d.resolve().WithService(name) }}
}

func (d delegatingLogger) WithAttrs(attrs ...Attr) Logger {
	return delegatingLogger{resolve: func() Logger { return d.resolve().WithAttrs(attrs...) }}
}

func (d delegatingLogger) WithGroup(name string) Logger {
	return delegatingLogger{resolve: func() Logger { return d.resolve().WithGroup(name) }}
}

// SetLevel 转发到当前 Default()。注意这会修改全局默认 logger 的级别，
// 而非仅本 delegating logger。
func (d delegatingLogger) SetLevel(level Level) {
	d.resolve().SetLevel(level)
}
