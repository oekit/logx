// Package logx 提供统一的日志门面，支持多种底层实现。
//
// 所有 enjoydream 后端服务面向 Logger 接口编程，底层实现可灵活替换：
//   - std: 基于标准库 log（默认）
//   - hlog: 基于 Hertz hlog（需导入 logx/hlog 子包）
//   - slog: 基于标准库 log/slog，输出结构化 JSON
//
// # 快速接入
//
//	// 业务 service 中创建带 service name 的 logger
//	var releaseLog = logx.WithService("release")
//
//	// 使用
//	releaseLog.Info("creating release", "keyword", keyword)
//	releaseLog.CtxError(ctx, "create failed", "err", err)
//
// # 替换底层实现
//
//	// main.go 中调用（可选，默认使用 std）
//	logx.SetDefault(logx.NewStdLogger("my-service", logx.LevelDebug))
//
//	// 使用 hlog 后端（需导入子包）
//	import logxhlog "github.com/oekit/logx/hlog"
//	logx.SetDefault(logxhlog.NewHlogLogger("my-service"))
//
//	// 使用 slog 后端（推荐生产环境，输出结构化 JSON）
//	logx.UseSlog("my-service")
//
// # 日志级别控制
//
// 通过 LOG_LEVEL 环境变量控制（影响所有后端），默认 INFO：
//
//	export LOG_LEVEL=DEBUG  # DEBUG | INFO | WARN | ERROR
//
// # Trace ID 自动注入
//
// 使用 Ctx* 方法时，自动从 context 提取 trace_id（由 ekit/middleware.ResponseMiddleware 注入）。
//
// # SDK 包的包级 logger
//
// 包级 var 在 init 时求值，先于 main() 中的 SetDefault。ekit 的 SDK
// 基础包不硬编码 service name（service name 属于消费方），包级 logger
// 统一用 Delegating 懒解析，组件名经 component 属性标识：
//
//	var log = logx.Delegating("mypkg")
//
// # 上下文绑定（WithCtx）
//
// 当不想在每次调用时都显式传入 ctx，可在请求入口绑定一次：
//
//	log := releaseLog.WithCtx(ctx) // 绑定后所有非 Ctx 方法自动携带 trace_id
//	log.Info("step1")
//	log.Info("step2", "keyword", kw)
//
// 绑定的 ctx 会随 WithService/WithAttrs/WithGroup 传递给子 logger。
// 显式的 Ctx* 方法仍以传入的 ctx 为准（优先于绑定 ctx）。
package logx
