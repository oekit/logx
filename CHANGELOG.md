# Changelog — oekit/logx

由 ekit v0.34.0（git.enjoye.top/enjoydream/ekit，冻结维护）拆分而来。所有 notable 变更记录如下。


## [Unreleased]

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
  另提供 `WithLevel` 显式指定级别；级别优先级：`WithLevel` > `LOG_LEVEL` 环境变量 >
  默认 `LevelInfo`（`NewSlogLogger` 现按此口径生效，原先忽略 `LOG_LEVEL`）
  （[#6](https://github.com/oekit/logx/issues/6)）
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