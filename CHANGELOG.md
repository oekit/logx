# Changelog — oekit/logx

由 ekit v0.34.0（git.enjoye.top/enjoydream/ekit，冻结维护）拆分而来。所有 notable 变更记录如下。

## [v0.7.0] — 2026-10-18

### Breaking
- `ParseLogLevel` 签名改为 `(Level, error)`（#13：原 `*Level` 失败为 nil，漏判空会在
  LOG_LEVEL 配错时远端解引用 panic）；新增 `ParseLogLevelOr(s, def)`——「配错退默认」
  免判空形态。矩阵内无外部消费方（已核对），BREAKING 影响面为矩阵外

### Added
- `TraceHandler(inner slog.Handler, opts ...TraceHandlerOption)`（#12）：给任意
  slog.Handler 包一层自动注入 trace_id——裸 slog 项目接 trace 串联的唯一路子。
  边界：ctx 无 trace 不注入（后台日志保持原样）、已显式携带不重复注入、
  WithAttrs/WithGroup 返回包装后的 handler（派生不断链）；`WithTraceKey` 自定义键名。
  trace 来源仍走 SetTraceExtractor 注册的提取器，与 logx/hmw/hertzotel 一致
## [v0.6.0] — 2026-10-17

### Added（go-kratos 对齐）
- `WithFilter(l, ...FilterOption)` 脱敏/丢弃包装：`FilterKey`（命中键值替换为 `***`，覆盖 Core/Ctx/Fmt 全路径，子 logger 保持规则）、`FilterFunc`（命中即丢整条）
- `SlogHandler(l)` / `Slogger(l)`：logx.Logger 正向桥接为 `*slog.Logger`（与 slog 后端反向桥对称；级别按名称映射，WithGroup 实现为键前缀），期望 slog 的三方库可直接消费 logx 生态


## [v0.5.1] — 2026-10-17

- **fix(std)**：`NewStdLogger` 无参默认级别 `LevelDebug` → **`LevelInfo`**（#11）。
  原先取 `LevelDebug` 是 `iota` 零值陷阱，与文档「默认 INFO」及 `NewSlogLogger`
  行为相反；未设 `LOG_LEVEL` 的进程会把 Debug 全量打进生产。
  按文档写断言的消费方此前会失败，本修正对齐契约。


- **feat(logx)**：`NewAuditLogger(service)`——审计/合规/安全留痕 logger，级别锁
  LevelInfo，不受 `LOG_LEVEL` 降级；`SetLevel` 调到 WARN/ERROR 被拒绝（`With*`
  派生仍保持守卫）。门面级承诺「审计留痕不被部署配置静默过滤」
  （[#10](https://github.com/oekit/logx/issues/10)）

## [hlog/v0.1.1 + kitexlog/v0.1.1] — 2026-10-17

- fix(deps): 嵌套模块 hlog/kitexlog 依赖地板 logx v0.2.0 → v0.5.0（LevelEnabler 线）；本仓根模块无变化

## [v0.5.0] — 2026-10-17

- **feat(logger)**：新增 `LevelEnabler` 可选接口（`Enabled(level Level) bool`）+
  惯用 helper `logx.Enabled(logger, level)`——热路径在昂贵 kv 参数求值前提前
  判定级别，被过滤事件零成本跳过（[#9](https://github.com/oekit/logx/issues/9)）。
  `std`/`slog` 后端经 BaseLogger 自动实现，`Delegating` 包装器透传解析目标，
  未实现的实现方保守返回 true（不误吞日志）。不动 `CoreLogger` 最小接口
- docs: CHANGELOG 的 Unreleased 节对齐已发布的 v0.4.0 tag
- **说明**：shutdown 兼容别名原计划本版本删除，因 bootstrap/hertz/kitex 等
  消费方尚未完成向 `core/lifecycle` 的迁移，**顺延至 v0.6.0**——先迁消费方，
  再删别名（保留别名方向无破坏）

## [v0.4.0] — 2026-10-17

- **refactor(shutdown)**：`Manager` 原语迁至 [oekit/core](https://github.com/oekit/core)
  `lifecycle` 包（与 `lifecycle.App` 契约同域，lifecycle 零外部依赖）。
  `logx/shutdown` 保留类型别名转发一个版本（Deprecated），**v0.5.0 删除**；
  请迁移到 `github.com/oekit/core/lifecycle`
  （[oekit#1](https://github.com/oekit/oekit/issues/1)）


- **feat(logx)**：`Delegating` 签名改为单参 `Delegating(component string)`——
  原变参「只取第一个、静默丢弃其余」与签名暗示不符；传空串表示不打
  component 属性。多参调用改为编译期报错
  （[#8](https://github.com/oekit/logx/issues/8)）
- **refactor(sink)**：`Sink`/`MultiSink`/`FileSink`/`RotatingSink`/`RotatingOption`
  整体迁至嵌套模块 `github.com/oekit/logx/sink`（含 lumberjack）。根包自此
  **纯 stdlib、零外部依赖**，不再向只用门面的消费者传递 lumberjack
  （[#7](https://github.com/oekit/logx/issues/7)）。
  **breaking**：`logx.NewFileSink` 等改从 `github.com/oekit/logx/sink` 引用
  （`sink.NewFileSink`）；0.x 不留根包别名，避免 lumberjack 经别名回流

## [v0.3.0] — 2026-10-17

- **feat(slog)**：`NewSlogLogger` 支持 `SlogOption` 注入消费方自有的 `slog.Handler`
  （`WithHandler`），保留既有日志格式（TextHandler / 文件轮转 / fan-out 等），
  「日志格式归消费方、门面归 logx」成立；未注入时仍走内置 JSON→stdout，纯增量。
  另提供 `WithLevel` 显式指定级别
  （[#6](https://github.com/oekit/logx/issues/6)）
- **breaking / behavior change**：`NewSlogLogger` 尊重 `LOG_LEVEL`（原先忽略）。
  级别优先级：`WithLevel` > `LOG_LEVEL` > 默认 `LevelInfo`。裸调 `NewSlogLogger(svc)`
  的消费方在部署了 `LOG_LEVEL=warn|error` 时会过滤 Info/Debug——**审计/合规类
  logger 必须显式 `WithLevel(LevelInfo)` 或改用 `NewAuditLogger`**，否则留痕静默丢失
  （[#10](https://github.com/oekit/logx/issues/10)）
- **feat(shutdown)**：`WithLogger(l *slog.Logger) ManagerOption` 注入宿主日志——
  Manager 内部日志（含 job 名等结构化字段）写入宿主 slog.Logger，与宿主日志体系
  统一采集；不注入时保持默认 logx sink（component=shutdown）不变
  （[#5](https://github.com/oekit/logx/issues/5)）
- **feat(shutdown)**：`(*Manager).Errors()` 访问器——Shutdown 结束后读取
  teardown/closer 失败（含 panic 隔离出的错误），停机错误可事后观测上报；
  返回副本、不触发 Shutdown
  （[#5](https://github.com/oekit/logx/issues/5)）

## [v0.2.0] — 2026-10-16

- **feat**：新增 `kitexlog` 子包——Kitex klog→logx 桥接（`kitexlog.NewLogger`），
  自 [oekit/kitex](https://github.com/oekit/kitex) 的 `kitex.NewLogger` 整体迁入，
  实现与测试逐行搬移、行为不变（含 hlog 后端递归 panic 守卫与 caller-skip 自注册）。
  只需要日志桥接的用户不再被迫 import 重型 kitex 模块；oekit/kitex 侧保留
  `kitex.NewLogger` Deprecated 别名委托，零 breaking
- **refactor**：本仓改为**嵌套多模块**布局——`hlog`/`hertzlog`/`kitexlog` 三个桥接
  子包各自持有独立 go.mod（module `github.com/oekit/logx/hlog` / `.../hertzlog` /
  `.../kitexlog`，同仓不同模块，先例 oekit/bootstrap 的 `nacos/`、`obs/`）。
  **import 路径不变，无代码级 breaking**；变的是模块图：Go 的依赖图是模块级的，
  此前根模块 go.mod require 的 hertz/kitex/zap/zerolog/logrus 会进所有消费者的
  依赖图，拆分后按需解析——只 import 根门面的消费者不再被拉入任何框架依赖。
  嵌套模块 tag 约定：`hlog/v0.1.0`、`hertzlog/v0.1.0`、`kitexlog/v0.1.0`
- **build**：根模块移除 `github.com/cloudwego/{hertz,kitex}`、`github.com/hertz-contrib/logger/{logrus,slog,zap,zerolog}`、
  `github.com/sirupsen/logrus`、`go.uber.org/zap`、`github.com/rs/zerolog`（各自移入对应
  嵌套模块，版本不变）；保留 lumberjack（RotatingSink 是门面功能），根门面自此仅
  stdlib + lumberjack

## [v0.1.10] — 2026-10-16

- **fix**：slog 后端 kv/Attr 中的 error 值渲染为 `Error()` 文本——原实现直接
  `json.Marshal`，多数 error（`fmt.wrapError`、包装链等）无导出字段被渲染成
  `{}`，错误原因整条丢失（[#2](https://github.com/oekit/logx/issues/2)）；
  与 log/slog JSONHandler 的 error 处理对齐。行为变化：实现了
  `json.Marshaler` 的错误类型，输出由 marshal 结果变为错误文本

## [v0.1.8] — 2026-10-15

- **breaking**：hertzlog 删除旧适配别名 `AsHlogLogger`/`AsHlogLoggerFromXxx`，
  统一 `AsHlogLoggerXxx` 系列并改为自包含实现（四后端均直接实现 hlog.FullLogger）
## [v0.1.0] — 2026-10-15

自 ekit v0.34.0（git.enjoye.top/enjoydream/ekit，冻结维护）拆分而来的初始公开版本，完成拆分稳定化：模块路径统一为 `github.com/oekit/logx`，多轮拆分/合并收敛至当前模块边界，组织基建（共享 CI workflow、golangci v2、Dependabot）就位。