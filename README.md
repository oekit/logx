# ekit-logx

> 统一日志门面——面向接口编程，底层实现可替换；附优雅关停管理器。零内部依赖。

本仓库是 [ekit](https://git.enjoye.top/enjoydream/ekit)（`git.enjoye.top/enjoydream/ekit` v0.34.0）
拆分出的独立 Go module，属于 **yi-nology** ekit 子项目矩阵（Round-2 深拆粒度）。

## 安装

```bash
go get github.com/yi-nology/ekit-logx@v0.1.0
```

## 包清单

- 根包 \`logx\`（import \`github.com/yi-nology/ekit-logx\`）— Logger 接口分层（CoreLogger→Logger）、WithService/WithAttrs/WithGroup、Delegating 懒解析、MultiSink/FileSink/RotatingSink、LOG_LEVEL
- \`hlog/\` — Hertz hlog 桥接
- \`hertzlog/\` — hertz-contrib logger（logrus/zap/zerolog/slog）桥接
- \`shutdown/\` — 优雅关停管理器（顺序 job、drain 超时）
- \`internal/logxcallertest\` — caller 归属测试辅助

## 依赖关系

子项目矩阵与依赖方向（自底向上，19 仓）：

```
ekit-core            文本/数据工具 + governance/lifecycle/version      （零内部依赖）
ekit-concurrency     并发与流控原语                                     （零内部依赖）
ekit-logx            日志门面 + shutdown                                （零内部依赖）
ekit-istio           Istio Proxyless                                    （零内部依赖）
  ekit-metrics       Prometheus 门面                          → core
  ekit-tracing       OTel + traceid                           → logx
  ekit-registry      注册发现 + 配置                          → core, logx
    ekit-data        缓存抽象 + 定时任务                      → core, logx, concurrency
      ekit-gorm      SQL/GORM 全家                            → core, logx
      ekit-redis     Redis 全家                               → data, core, logx
    ekit-httpx       HTTP 客户端 + 负载均衡                   → core, concurrency, logx, metrics, tracing, registry
      ekit-nns       服务治理客户端                           → httpx, registry, core, logx, tracing, metrics
        ekit-bootstrap 应用引导
    ekit-middleware  Hertz 中间件栈                           → core, concurrency, logx, metrics, tracing, registry
    ekit-hertz       Hertz 运行时/适配/instrumentation        → middleware, gorm, nns, registry, metrics, tracing, logx, core
    ekit-kitex       Kitex 生态                               → nns, registry, core, concurrency, logx, metrics
  ekit-workflow      流程编排                                 → concurrency, logx
```

> 注：`ekit-observability` 已在 Round-2 深拆中解散——logx 迁 [ekit-logx]、metrics 迁 [ekit-metrics]、
> tracing/traceid 迁 [ekit-tracing]、shutdown 迁 [ekit-logx]；原仓库已归档。

本仓库为 L0 地基；第三方仅 `gopkg.in/natefinch/lumberjack.v2` 与 hertz-contrib/logger（hertzlog 子包隔离）。

## 与 ekit 单体仓库的关系

- 代码与 ekit v0.34.0 等价，module 路径 `git.enjoye.top/enjoydream/ekit/...` → `github.com/yi-nology/ekit-logx/...`
- 各子仓库独立发版（SemVer）；开发规范沿用 ekit AGENTS.md 约定

## License

见 [LICENSE](LICENSE)
