# Changelog — oekit/logx

由 ekit v0.34.0（git.enjoye.top/enjoydream/ekit，冻结维护）拆分而来。所有 notable 变更记录如下。

## [v0.8.6] — 2026-11-07

- fix(std): stdWriter 动态委托 log.Writer()——恢复 log.SetOutput 拦截契约（#78 后遗症，gormx 地板抬升实测回归）：私有 log.New(os.Stderr) 固化绑定绕过标准库 log 输出重定向，消费方测试捕获法全部捕空；缺省仍 os.Stderr、flags=0 单时间戳不变

## [v0.8.5] — 2026-11-07

- fix(slog): 内置 JSON 路径不再静默丢弃 WithGroup——用户 Attr/kv 施加组前缀，与 SlogHandler 桥接口径统一（#75）
- fix(slog): WithGroup 链式调用不再只留最后一层——组栈累积嵌套（#83）
- fix(slog): 桥接不再错标严重级/丢 r.PC/r.Time——阈值制级别映射（自定义级别不再降 INFO）、Warn 档不吞、caller 直通（#84）
- fix(slog): caller 探测移除 12 帧上限——深调用栈不再静默丢 caller，行为文档化（#82）
- perf(slog): caller 探测 O(k²)→O(k) 单遍回溯 + reserved map 包级化 + Handler 组链缓存（#87）
- fix(std): logf 补挂 FormatAttrs——printf 与结构化路径字段面统一（#77）
- fix(std): 消除双时间戳——私有 log.New 替代包级 log.Printf 的 LstdFlags（#78）
- fix(hlog): printf 路径补挂 attrPrefix——Ctx*f 系列不再丢 WithAttrs（#86）
- docs(filter): WithFilter printf 路径只评估不脱敏——显式声明敏感数据走 kv（#80）
- fix(logger): filterLogger/auditLogger 实现 Enabled——热路径级别守卫恢复生效（#76）
- fix(slog): stdout 写失败计数 + 一次性 stderr 报告——管道关闭不再静默丢日志，新增 StdoutWriteFailures()（#79）
- fix(logger): SetLevel 改实例级下限——Delegating/WithFilter 子 logger 不再修改全局默认 logger（#81）
- fix(filter): FilterKey 大小写不敏感匹配 + 非 string 键 %v 归一——键名变体绕过面关闭（#85）
- build: 依赖地板对齐 core v0.13.1

关联问题单：logx#75、logx#76、logx#77、logx#78、logx#79、logx#80、logx#81、logx#82、logx#83、logx#84、logx#85、logx#86、logx#87。

## [kitexlog/v0.1.9] — 2026-11-06

- build: 矩阵依赖地板对齐（维护轮 #233）

## [hlog/v0.1.9] — 2026-11-06

- build: 矩阵依赖地板对齐（维护轮 #233）

## [v0.8.4] — 2026-11-06

- fix(slog): 内置 JSON 路径整行单次 Write（[#65](https://github.com/oekit/logx/issues/65)）——此前记录体与换行分两次 `os.Stdout.Write`，写间无原子性保证：并发下另一 goroutine 的记录体插进本条与换行之间 ⇒ JSONL 拼行/空行（issue 实测 12% 行非法 JSON），下游逐行解析静默丢日志。修法：`'\n'` 并入 buf 单次写（对齐 log.Printf / log/slog JSONHandler 单写纪律）。补 16×120 并发 pipe 回读回归（读端先起——pipe 缓冲满会死锁写者，实测踩过）
- fix(std+hlog): 构造器读 LOG_LEVEL + hlog switch 补 OFF 档（[#66](https://github.com/oekit/logx/issues/66)）——`NewStdLogger`/`NewHlogLogger` 此前不读环境变量（仅 init 默认实例读）：LOG_LEVEL=DEBUG 下 hlog 后端 Debug 零输出、=OFF 下 Info 照打（hlog switch 连 OFF 分支都没有，静默落 Info），README/doc/AGENTS 三处「LOG_LEVEL 统一控制所有后端」承诺双双落空。修法：构造器 `ParseLogLevelOr(os.Getenv("LOG_LEVEL"), LevelInfo)`（显式参数仍最高优先）；hlog OFF 分支全局级别抬 Error 兜底、输出面由实例 ShouldLog 拦截
- fix(slog): 用户 kv 与框架字段同名统一为「框架字段优先 + 改名落地 + _renamed 标记」（[#68](https://github.com/oekit/logx/issues/68)）——此前包内三口径：内置 JSON 路径 map 赋值静默覆盖（trace_id 被覆盖=追踪链断）、trace_handler 不重复注入、handler 路径原样重复。统一为 time/level/message/service/trace_id/caller 六个框架字段不可覆盖、用户键加 "_" 前缀落地并追加 `_renamed` 改名清单（记录内可 grep `jq 'select(."_renamed")'`；不在日志流打 WARN——logx 给自己打日志有递归风险）

关联问题单：logx#65、logx#66、logx#68。

## [v0.8.3] — 2026-11-06

- build: 矩阵依赖地板对齐（维护轮 #233）

## [kitexlog/v0.1.8] — 2026-11-03

- build: 把父仓 logx v0.8.2 的地板带进已发布产物（hlog/kitexlog v0.1.7 的快照嵌的是 logx v0.8.1，消费方拿不到 v0.8.2 的内容）（logx#63）

关联问题单：logx#63。

## [hlog/v0.1.8] — 2026-11-03

- build: 把父仓 logx v0.8.2 的地板带进已发布产物（hlog/kitexlog v0.1.7 的快照嵌的是 logx v0.8.1，消费方拿不到 v0.8.2 的内容）（logx#63）

关联问题单：logx#63。

## [kitexlog/v0.1.7] — 2026-11-03

- build: 矩阵内依赖地板对齐——把父仓 logx v0.8.1 的地板带进已发布产物（hlog/kitexlog 原快照嵌的是 logx v0.8.0，消费方拿不到 core v0.12.1 的 Listen 修复）

## [hlog/v0.1.7] — 2026-11-03

- build: 矩阵内依赖地板对齐——把父仓 logx v0.8.1 的地板带进已发布产物（hlog/kitexlog 原快照嵌的是 logx v0.8.0，消费方拿不到 core v0.12.1 的 Listen 修复）

## [v0.8.2] — 2026-11-03

- build: 矩阵内依赖地板对齐——把父仓 logx v0.8.1 的地板带进已发布产物（hlog/kitexlog 原快照嵌的是 logx v0.8.0，消费方拿不到 core v0.12.1 的 Listen 修复）

## [v0.8.1] — 2026-11-03

- fix(logx): caller 跳过注册表是进程级全局——测试必须自己恢复，否则对 -count>1 不幂等（PR #52）：SkipCallerFiles 注册的 caller 跳过文件是进程级全局且只增不删 ⇒ 用例自己注册的探针文件会留在全局里 ⇒ -count>1 时（同一进程内重复）探针帧已被跳过、baseline 落到 testing 框架帧⇒ 本地 -race -count=12 实测 11/12 轮红（CI 用 -count=1 所以不红）。改法：用 export_test.go暴露的注册表恢复钩子让测试自己恢复注册表 ⇒ 对 -count>1 幂等，且注册表不再泄漏给同批次的其它用例。只改测试文件，零 API 变更
- build: 矩阵内依赖地板对齐（align_floors 不动点）（#57）：core v0.9.2 → v0.12.1——logx 唯一用到的是 core/lifecycle；**本区间 lifecycle/manager.go 确有变更**（`Listen` 补 `defer signal.Stop(ch)`——修 Listen 返回后信号仍被捕获进没人读的 channel、第二次 Ctrl-C / 容器 SIGTERM 不再终止进程，只能升级 SIGKILL）⇒ 经 logx/shutdown 的类型别名转发（`type Manager = lifecycle.Manager`）该修复**穿透到 logx 用户**。**本条为更正**：初版此处写「core/lifecycle 逐字节未变、抬地板只带动修复传递」**不成立**——该断言在 core v0.6.0 → v0.9.2 区间成立，被照搬到新区间却未复核（core v0.9.2 → v0.12.1 之间 lifecycle/manager.go +5 行，blob 738e7f8 → cc9d9a3）

## [hlog/v0.1.6] — 2026-10-28

- build: 父模块地板 logx v0.7.2 → v0.8.0（跟随父仓本轮发布）

## [kitexlog/v0.1.6] — 2026-10-28

- build: 父模块地板 logx v0.7.2 → v0.8.0（跟随父仓本轮发布）

## [v0.8.0] — 2026-10-28

- feat(logx): 补静默 logger（`Nop()`）与「关闭」档（`LevelOff`）——组件把 logger 作为**依赖注入**传进来（这是好设计），于是测试成了唯一难受的地方：组件要一个 logger，测试里只能传真 logger ⇒ 日志直接刷到 stdout，把断言失败信息淹没在噪声里。此前想「不输出任何日志」只有两条路，都不成立：① 把级别设到 `LevelError`——**仍会打 error**，且构造器 `NewStdLogger(service)` 要求编一个 service 名；② 自己实现 `logx.Logger`——接口有 4 个结构化方法 + 4 个 printf 形态 + 4 个 `Ctx*` 形态 + `With*`/`SetLevel` 共 17 个方法，成本与收益不成比例 ⇒ 下游各写一份替代品或干脆绕开（实测下游 grep `nopLogger|NopLogger|DiscardLogger` 零命中 ⇒ 大家都绕开而不是各造一份）。新增：`Nop() Logger`（零输出；实现 `LevelEnabler` ⇒ `Enabled` 恒 false ⇒ 经 `logx.Enabled` 的热路径守卫会**跳过昂贵参数求值**（截断/序列化/拷贝），不只是不打印）、`LevelOff` 档（比 `LevelError` 更高 ⇒ 任何级别都不输出，用于「保留结构化日志但本进程不落盘」）、`ParseLogLevel` 认 `"OFF"`（此前 `LOG_LEVEL=OFF` 只能报「未知级别」再退回默认 ⇒ 「想彻底关掉日志」没有可达路径）。既有四档的取值、名称与行为逐字不变（#41）

## [hlog/v0.1.5] — 2026-10-28

- build: 父模块地板 logx v0.7.1 → v0.7.2（跟随父仓本轮发布）

## [kitexlog/v0.1.5] — 2026-10-28

- build: 父模块地板 logx v0.7.1 → v0.7.2（跟随父仓本轮发布）

## [v0.7.2] — 2026-10-28

- fix(filter): `WithFilter` 的脱敏此前只作用于「每次调用传进来的 kv」，`WithAttrs` 挂上去的属性原样进日志 ⇒ password 等敏感值照常落盘（而 `WithFilter` 的 godoc 明确承诺派生 logger 保持相同过滤规则）。修法：`WithAttrs` 先脱敏（命中键替换为 `***`）再交给 base

## [hlog/v0.1.4] — 2026-10-22

- build: 父模块地板 logx v0.7.0 → v0.7.1——logx v0.7.1 起 core 地板抬至 v0.9.2，
  嵌套适配器经 MVS 随父模块传递该修正（无直接 require core 的下游落到父模块声明
  的下界）；零代码改动

## [kitexlog/v0.1.4] — 2026-10-22

- build: 父模块地板 logx v0.7.0 → v0.7.1（同 hlog/v0.1.4）；零代码改动

## [v0.7.1] — 2026-10-19

- build: core 地板 v0.6.0 → v0.9.2（logx#22）——无直接 require core 的下游经 MVS 落到本仓声明的下界，抬至最新带动修复传递；logx 唯一使用的 core/lifecycle 逐字节未变，零行为风险

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

## [hlog/v0.1.3 + kitexlog/v0.1.3] — 2026-10-18

- chore(deps): 父模块地板 logx v0.6.0 → v0.7.0（WithFilter/Slogger 线；audit 嵌套地板门禁首发抓漏，matrix-tools#6）；本仓根模块无变化

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