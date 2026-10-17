# AGENTS.md — oekit/logx

> 统一日志门面（接口分层 / 多后端 / Sink / caller 归属）+ shutdown 优雅关停
> 当前版本：`v0.3.0` · module：`github.com/oekit/logx` · org：[oekit = Open Easy Kit](https://oekit.github.io)

## 本仓包含（嵌套多模块：1 根模块 + 3 嵌套模块）
- `./` —— 根模块 `github.com/oekit/logx`（门面 + std/slog 后端 + shutdown 兼容层（Deprecated，见 core/lifecycle）+ internal/logxcallertest）
- `sink/` —— 嵌套模块 `github.com/oekit/logx/sink`（Sink/FileSink/RotatingSink/MultiSink，依赖 lumberjack）
- `hlog/` —— 嵌套模块 `github.com/oekit/logx/hlog`（Hertz hlog 后端，依赖 cloudwego/hertz）
- `hertzlog/` —— 嵌套模块 `github.com/oekit/logx/hertzlog`（hertz-contrib logrus/zap/zerolog/slog 桥接）
- `kitexlog/` —— 嵌套模块 `github.com/oekit/logx/kitexlog`（Kitex klog→logx 桥接，依赖 cloudwego/kitex）

**嵌套模块 tag 约定**：根模块 `vX.Y.Z`；嵌套模块子路径 tag `<子目录>/vX.Y.Z`
（如 `sink/v0.1.0`、`hlog/v0.1.0`、`hertzlog/v0.1.0`、`kitexlog/v0.1.0`；先例 oekit/bootstrap 的 `nacos/`、`obs/`）。

**硬规则：根模块 go.mod 禁止出现任何外部依赖**（含 lumberjack 与 hertz/kitex/zap/zerolog/logrus 等框架桥接）——Go 依赖图是模块级的，根模块的 require 会进所有消费者的
依赖图；框架桥接一律放对应嵌套模块，本地开发解析用 go.work use + replace。

## 硬规则速览（全矩阵适用）

### 分层与依赖
- 依赖严格自底向上，禁止反向依赖；本仓的矩阵内依赖见 README「矩阵总览」表
- 微工具集统一在 `github.com/oekit/pkg`；本仓新增基础工具前先确认 pkg 里没有

### 错误处理
- 一律使用 `github.com/oekit/pkg/errors`：`errors.Wrap(err, "ctx", kv...)`（nil-safe）、
  `errors.Cause`；业务错误用 BizError（BizCode 与 HTTPCode 双轨，命名工厂 NewNotFoundError 等）
- **类型同一性红线**：同一进程内只允许一套 errors 包。混用旧路径会把 BizError 的 As/Is
  判定打断（Not Found 变 500）
- handler 层统一 `mw.Fail(ctx, c, err)` 出口；所有 error 禁止 `_` 忽略

### 日志
- 门面 `github.com/oekit/logx`：业务服务 `logx.WithService("svc")`；
  **SDK/基础包必须 `logx.Delegating("组件名")` 懒解析**（禁止 Default()/WithService 声明包级 logger）
- kv 结构化风格为默认；`Ctx*` 方法自动携带 trace_id；禁止 printf 风格 `logx.Infof` 作为主要手段
- 后端经 `logx.SetDefault` 替换；`LOG_LEVEL` 环境变量统一生效
- 桥接子包按需引入（各自为嵌套模块，独立 go.mod/tag）：`hlog`（Hertz hlog 后端）、
  `hertzlog`（logrus/zap/zerolog/slog→hlog）、`kitexlog`（Kitex klog→logx，自 oekit/kitex 迁入）；
  hlog 后端 Logger 禁止传入 `hertz.NewLogger`/`kitexlog.NewLogger`（无限递归，运行时 panic 防御）

### 并发安全
- 禁止裸 `go func`：用 `github.com/oekit/concurrency/async` 的 GoSafe 系列（panic 被 recover）
- 共享状态 mutex / atomic 保护；包级全局可变状态用 `atomic.Pointer[T]`

### 生命周期
- 组件 Close 统一 `Close(ctx) error` 签名 + sync.Once 幂等
- 应用骨架用 `github.com/oekit/bootstrap/bootstrap.Run`；**userFn 返回即服务结束，
  长驻服务必须 `<-app.Context().Done()` 阻塞**

### Option 与命名
- 函数选项模式 `XxxOption`（禁止裸 `Option`，多包 co-import 冲突）
- 导出符号 PascalCase + godoc 注释；包文档放 doc.go

### 版本与发版
- SemVer；0.x 阶段 minor 允许 breaking；发版 = 打 annotated tag 推远端（CI 自动跑 lint+test）
- module 路径 = `github.com/oekit/<name>`；**改 module 路径必须同步**：
  全部 import、go.mod、README 安装行、logx caller 启发式（若涉及 logx）

### 文档
- 本仓 README 的「矩阵总览」表由脚本生成，结构变化后需重新生成
- 历史完整规范见 ekit 单体仓（git.enjoye.top/enjoydream/ekit，冻结维护）的 AGENTS.md 与 docs/

## 本仓范围

