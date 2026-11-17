package logx

// 标准结构化字段名常量。
//
// 两矩阵（oekit / oagentkit）消费同一套 logx 后端，落进同一 Loki/ES；
// 错误字段若分裂为 "err"/"error" 两个 key，查询侧必须 OR 双写（oekit#161）。
// 统一以本组常量为准，禁止手写字符串字面量。

// ErrKey 是记录错误值的标准字段名，全矩阵固定为 "err"。
//
// 用法：
//
//	log.CtxError(ctx, "create failed", logx.ErrKey, err)
const ErrKey = "err"

// TraceIDFieldKey 是链路追踪 ID 的标准字段名，全矩阵固定为 "trace_id"。
// 内置 JSON 框架字段、注入 Handler 框架字段与 TraceHandler 默认注入键共用
// 本常量（logx#231：此前 5 处手写魔法字面量，改键名要逐处找）；禁止手写
// "trace_id" 字面量（与 ErrKey 同一纪律）。
const TraceIDFieldKey = "trace_id"
