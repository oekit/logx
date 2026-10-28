package logx

import "context"

// nopLogger 是零输出的 Logger 实现：所有写日志的方法空转。
type nopLogger struct{}

// Nop 返回一个**完全不输出**的 Logger。
//
// 用途：组件把 logger 作为**依赖注入**传进来（这是好设计），于是测试成了
// 唯一难受的地方——组件要一个 logger，测试里只能传真 logger ⇒ 日志直接刷到
// stdout，把断言失败信息淹没在噪声里。此前想「不输出任何日志」只有两条路，
// 都不成立：
//
//  1. 把级别设到 LevelError —— **仍会打 error**，且构造器
//     NewStdLogger(service) 要求编一个 service 名；
//  2. 自己实现 logx.Logger —— 接口有 Debug/Info/Warn/Error + 四个 printf
//     形态 + 四个 Ctx* 形态 + With*/SetLevel 共 17 个方法，成本与收益不成比例。
//
// Nop 同时实现 LevelEnabler（Enabled 恒 false）⇒ 经 logx.Enabled 的热路径
// 守卫会**跳过昂贵参数求值**（截断/序列化/拷贝），不只是不打印。
//
//	var l logx.Logger = logx.Nop()
//	l.Info("测试里这行不会出现在输出里")
//
// 要「保留结构化日志但本进程不落盘」用 LevelOff 档：
//
//	logx.NewStdLogger("svc", logx.LevelOff)
func Nop() Logger { return nopLogger{} }

// 结构化风格方法——全部空转。

func (nopLogger) Debug(msg string, keysAndValues ...any) {}
func (nopLogger) Info(msg string, keysAndValues ...any)  {}
func (nopLogger) Warn(msg string, keysAndValues ...any)  {}
func (nopLogger) Error(msg string, keysAndValues ...any) {}

// printf 风格方法——全部空转。

func (nopLogger) Debugf(format string, args ...any) {}
func (nopLogger) Infof(format string, args ...any)  {}
func (nopLogger) Warnf(format string, args ...any)  {}
func (nopLogger) Errorf(format string, args ...any) {}

// 带 Context 的方法——全部空转。

func (nopLogger) CtxDebug(ctx context.Context, msg string, keysAndValues ...any) {}
func (nopLogger) CtxInfo(ctx context.Context, msg string, keysAndValues ...any)  {}
func (nopLogger) CtxWarn(ctx context.Context, msg string, keysAndValues ...any)  {}
func (nopLogger) CtxError(ctx context.Context, msg string, keysAndValues ...any) {}

func (nopLogger) CtxDebugf(ctx context.Context, format string, args ...any) {}
func (nopLogger) CtxInfof(ctx context.Context, format string, args ...any)  {}
func (nopLogger) CtxWarnf(ctx context.Context, format string, args ...any)  {}
func (nopLogger) CtxErrorf(ctx context.Context, format string, args ...any) {}

// 派生方法——静默 logger 的派生仍是静默 logger。
//
// 注意：**不**把 ctx/service/attrs 记下来：没有输出就没有可携带的状态，
// 记下来只会在派生链上引入分配与拷贝。

func (nopLogger) WithCtx(ctx context.Context) Logger { return nopLogger{} }
func (nopLogger) WithService(name string) Logger     { return nopLogger{} }
func (nopLogger) WithAttrs(attrs ...Attr) Logger     { return nopLogger{} }
func (nopLogger) WithGroup(name string) Logger       { return nopLogger{} }
func (nopLogger) SetLevel(level Level)               {}

// Enabled 恒 false——实现 LevelEnabler，让 logx.Enabled 的热路径守卫
// 跳过昂贵参数求值（不打印 ≠ 不算：参数已经求值完了）。
func (nopLogger) Enabled(level Level) bool { return false }
