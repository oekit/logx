# logx

> 统一日志门面——接口分层/多后端可替换/Sink 多目标/caller 归属

> 🌐 项目主页：**https://oekit.github.io/** · 名称由来：**O**pen **E**asy **Kit**（开源 · 易用 · 工具集合）
> 🤖 AI Agent：本仓规范与硬规则见 [AGENTS.md](AGENTS.md)；机器可读索引见 [llms.txt](https://oekit.github.io/llms.txt)（接口签名以源码为准）。

## 安装

根门面（**仅依赖 stdlib + lumberjack，零框架依赖**）：

```bash
go get github.com/oekit/logx@v0.2.0
```

## 模块布局（1 根模块 + 3 嵌套模块）

本仓自 v0.2.0 起为**嵌套多模块仓**（同仓不同 go.mod，先例：oekit/bootstrap 的
`nacos/`、`obs/`）：根门面与三个框架桥接子包各自持有独立 go.mod，import 路径
不变，依赖图按需解析——Go 的依赖图是模块级的，此前根模块 require 的
hertz/kitex/zap/zerolog/logrus 会进所有消费者的依赖图；拆分后只 import 根门面
的消费者不再被拉入任何框架依赖。

| go.mod 路径 | module | 职责 | 框架依赖 | 安装 |
|---|---|---|---|---|
| `./` | `github.com/oekit/logx` | 统一门面 + std/slog 后端 + Sink + shutdown | 仅 lumberjack | `go get github.com/oekit/logx@v0.2.0` |
| `./hlog/` | `github.com/oekit/logx/hlog` | Hertz hlog 后端适配 | cloudwego/hertz | `go get github.com/oekit/logx/hlog@v0.1.0` |
| `./hertzlog/` | `github.com/oekit/logx/hertzlog` | hertz-contrib logrus/zap/zerolog/slog 桥接 | hertz + hertz-contrib/logger/* + logrus/zap/zerolog | `go get github.com/oekit/logx/hertzlog@v0.1.0` |
| `./kitexlog/` | `github.com/oekit/logx/kitexlog` | Kitex klog→logx 桥接（自 oekit/kitex 迁入） | cloudwego/kitex | `go get github.com/oekit/logx/kitexlog@v0.1.0` |

**嵌套模块 tag 约定**（Go modules 子路径 tag 标准）：根模块 tag `vX.Y.Z`；
嵌套模块 tag `<子目录>/vX.Y.Z`，如 `hlog/v0.1.0`、`hertzlog/v0.1.0`、`kitexlog/v0.1.0`。

## 这个仓库做什么

`logx` 是**统一日志门面**：业务代码面向 `Logger` 接口编程，底层实现（slog/std/hlog/
logrus/zap/zerolog）可整体替换。附带优雅关停管理器 `shutdown`。

解决的核心问题：

1. **换库不改业务代码**——接口分四层（CoreLogger → FormattedLogger → ContextLogger → Logger），按需依赖
2. **SDK 与业务解耦**——SDK 包用 `Delegating` 懒解析，消费方 main 里 `SetDefault` 后全局生效
3. **caller 归属准确**——自动跳过内部帧，框架日志桥接不串位置
4. **trace_id 无感注入**——`Ctx*` 方法自动从 context 提取

## 包结构

| 包 | 做什么 |
|---|---|
| 根包 | 接口 + std/slog 两内置后端 + `MultiSink`/`FileSink`/`RotatingSink` 多目标输出 |
| `hlog` | Hertz hlog 后端适配 |
| `hertzlog` | hertz-contrib 的 logrus/zap/zerolog/slog 桥接 |
| `kitexlog` | Kitex klog→logx 桥接（自 oekit/kitex 迁入；携带 cloudwego/kitex 依赖，按需引入） |
| `shutdown` | 优雅关停管理器：顺序 job、drain 超时、幂等 |
| `internal/logxcallertest` | caller 归属端到端测试探针 |

## 快速使用

```go
import "github.com/oekit/logx"

// 业务服务：声明带 service 标识的 logger
var log = logx.WithService("my-service")

log.Info("order created", "order_id", 42, "amount", 99.5)
log.CtxError(ctx, "create failed", "err", err) // 自动带 trace_id；error 值渲染为错误文本

// main() 里可整体替换后端
logx.SetDefault(logx.NewSlogLogger("my-service"))
```

### SDK 包的硬规则

```go
// ✅ SDK 基础包必须用 Delegating 懒解析（消费方 SetDefault 后自动跟随）
var flowLog = logx.Delegating("workflow")

// ❌ 禁止：Default()/WithService 在 init 时求值，消费方后续设置失效
var bad = logx.WithService("hardcoded")
```

## 要点

- kv 结构化风格是默认规范；`LOG_LEVEL` 环境变量统一控制所有后端
- `MultiSink`/`FileSink`/`RotatingSink` 支持同时写 stdout + 轮转文件、自定义 Loki/OTLP 后端
- 根门面仅 stdlib+lumberjack；`hlog`/`hertzlog`/`kitexlog` 各自为嵌套模块（见「模块布局」），框架依赖按需引入


## 矩阵总览（21 活跃模块，自动生成）

| 模块 | 内容 | 矩阵内依赖 |
|---|---|---|
| [pkg](https://github.com/oekit/pkg) | 微工具集 21 合 1（errors/bloomx/contextx/collectionx/validator/stringx/slicex/enumx/convert/encoding/jsonrepair/httpjson/mathx/pathx/pagination/ptrx/timex/uidgen/cache/bufiox/strmap）；cache（v0.4.0 起）含缓存接口 CacheAside + BatchGetter 批量能力（MGet）+ LocalCache/SingleFlightCache + cachetest 契约 + Checker；collectionx（v0.5.0 起）泛型容器 Set/Ring/Queue/SafeMap；timex/stringx（v0.7.0 起）时间段边界 BeginningOf/EndOf 七对（分/时/日/周/月/季/年，周起始显式参数）+ trie 多模式替换器 Replacer（最长匹配）/ NewMask 敏感词遮罩 | — |
| [concurrency](https://github.com/oekit/concurrency) | 并发与流控原语（async/gopool/group/singleflight/progress/syncx/ratelimit/retry）+ window 滑动窗口 / breaker 自适应熔断（规范哨兵 ErrCircuitOpen）/ shedding CPU 自适应降载 / batch 攒批执行器（满条数/满字节/窗口去重）/ timewheel 单级时间轮 | — |
| [logx](https://github.com/oekit/logx) | 统一日志门面（多后端可替换 + Sink + caller 归属）+ 优雅关停；桥接子包 hlog/hertzlog/kitexlog 为嵌套模块按需引入 | — |
| [core](https://github.com/oekit/core) | 应用基建件（governance 治理 schema / config 配置加载 + default/env/range/options tag 校验 / lifecycle 契约 / network IP 工具 / version） | [logx](https://github.com/oekit/logx) |
| [metrics](https://github.com/oekit/metrics) | Prometheus 门面 + 弹性指标族 | [core](https://github.com/oekit/core) |
| [tracing](https://github.com/oekit/tracing) | OpenTelemetry 追踪 + trace_id 真相源 | [logx](https://github.com/oekit/logx) |
| [registry](https://github.com/oekit/registry) | 注册发现核心 + Nacos 实现 + Nacos 配置源 + sdbridge | [core](https://github.com/oekit/core)、[logx](https://github.com/oekit/logx) |
| [data](https://github.com/oekit/data) | 数据访问门面——复刻 ekit data/ 能力面：cache/rediscache/crud/distlock/gormx/redisx/timertask/cachetest 八子包 re-export（类型同一，实现住 pkg 与各子仓）+ cached 缓存读写编排（写后失效/事务提交后失效/查询缓存版本号失效/GetByIDs 批量点读 MGet 快路径/bloom 防穿透）+ twolevel 两级缓存与跨实例失效广播（v0.7.0 起本仓唯二非 re-export 实现）；all/ 聚合模块已废弃 retract | [concurrency](https://github.com/oekit/concurrency)、[gormx](https://github.com/oekit/gormx)、[logx](https://github.com/oekit/logx)、[pkg](https://github.com/oekit/pkg)、[redisx](https://github.com/oekit/redisx)、[timertask](https://github.com/oekit/timertask) |
| [gormx](https://github.com/oekit/gormx) | SQL/GORM 全家——连接池增强、框架无关 CRUD Service、仓储（原 oekit/gorm，v0.3.0 更名） | [logx](https://github.com/oekit/logx)、[pkg](https://github.com/oekit/pkg) |
| [redisx](https://github.com/oekit/redisx) | Redis 全家——客户端封装（根包）、分布式缓存 cache/、分布式锁 distlock、分布式限流 ratelimit（Lua 三态 + 故障本地兜底） | [concurrency](https://github.com/oekit/concurrency)、[logx](https://github.com/oekit/logx)、[pkg](https://github.com/oekit/pkg) |
| [httpx](https://github.com/oekit/httpx) | HTTP 客户端 + 负载均衡——服务发现/熔断（规范哨兵 ErrCircuitOpen）/重试/指标/OTel 透传/一致性哈希（权重感知虚拟副本） | [concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[logx](https://github.com/oekit/logx)、[metrics](https://github.com/oekit/metrics)、[registry](https://github.com/oekit/registry)、[tracing](https://github.com/oekit/tracing) |
| [network](https://github.com/oekit/network) | 轻量网络客户端——WebSocket/NATS/MQ 抽象、连接与流工具、Octopus 推送 | [concurrency](https://github.com/oekit/concurrency)、[logx](https://github.com/oekit/logx)、[tracing](https://github.com/oekit/tracing) |
| [timertask](https://github.com/oekit/timertask) | 分片定时任务（FNV-1a、心跳续约） | [concurrency](https://github.com/oekit/concurrency)、[logx](https://github.com/oekit/logx) |
| [middleware](https://github.com/oekit/middleware) | Hertz 自研标准中间件栈 + contrib 桥接 + hmw + SheddingMiddleware 自适应降载 | [concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[logx](https://github.com/oekit/logx)、[metrics](https://github.com/oekit/metrics)、[pkg](https://github.com/oekit/pkg)、[registry](https://github.com/oekit/registry)、[tracing](https://github.com/oekit/tracing) |
| [nns](https://github.com/oekit/nns) | NNS 服务治理客户端——provider 无关发现/配置/实例缓存 | [concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[logx](https://github.com/oekit/logx)、[registry](https://github.com/oekit/registry) |
| [nns-endpoint](https://github.com/oekit/nns-endpoint) | NNS 端点工厂——per-service 熔断、换实例重试、gRPC 池 | [concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[httpx](https://github.com/oekit/httpx)、[logx](https://github.com/oekit/logx)、[metrics](https://github.com/oekit/metrics)、[nns](https://github.com/oekit/nns)、[registry](https://github.com/oekit/registry) |
| [bootstrap](https://github.com/oekit/bootstrap) | 应用生命周期引导——bootstrap.Run 骨架 + healthz | [concurrency](https://github.com/oekit/concurrency)、[logx](https://github.com/oekit/logx) |
| [hertz](https://github.com/oekit/hertz) | Hertz 运行时集成——日志桥接、生命周期、SSE/health/metrics、适配器、OTel、crudh/validatorh/versionh（原 handler 微库收编） | [bootstrap](https://github.com/oekit/bootstrap)、[concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[gormx](https://github.com/oekit/gormx)、[logx](https://github.com/oekit/logx)、[metrics](https://github.com/oekit/metrics)、[middleware](https://github.com/oekit/middleware)、[nns](https://github.com/oekit/nns)、[pkg](https://github.com/oekit/pkg)、[registry](https://github.com/oekit/registry)、[tracing](https://github.com/oekit/tracing) |
| [kitex](https://github.com/oekit/kitex) | Kitex 生态——日志桥接、治理热更、生命周期、BizError 桥接、kitexotel、istio（xDS 动态熔断，原 istio 微库收编） | [bootstrap](https://github.com/oekit/bootstrap)、[concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[logx](https://github.com/oekit/logx)、[metrics](https://github.com/oekit/metrics)、[nns](https://github.com/oekit/nns)、[pkg](https://github.com/oekit/pkg)、[registry](https://github.com/oekit/registry) |
| [workflow](https://github.com/oekit/workflow) | 组合式流程编排——串行/并行/异步/条件分支 + Hook AOP | [concurrency](https://github.com/oekit/concurrency)、[logx](https://github.com/oekit/logx) |
| [kitex-examples](https://github.com/oekit/kitex-examples) | Kitex echo 示例——client/server + IDL + 治理配置 | [bootstrap](https://github.com/oekit/bootstrap)、[concurrency](https://github.com/oekit/concurrency)、[core](https://github.com/oekit/core)、[kitex](https://github.com/oekit/kitex)、[logx](https://github.com/oekit/logx)、[metrics](https://github.com/oekit/metrics)、[nns](https://github.com/oekit/nns)、[pkg](https://github.com/oekit/pkg)、[registry](https://github.com/oekit/registry)、[tracing](https://github.com/oekit/tracing) |
> 依赖严格自底向上；微库已合并为 pkg，旧微库仓已删除。

> 本仓在矩阵中的位置与依赖见上表「矩阵内依赖」列。历史沿革：源自 ekit 单体库
> （git.enjoye.top/enjoydream/ekit v0.34.0，冻结维护），2026-10 拆分为独立 module。

## License

MIT — 见 [LICENSE](LICENSE)
