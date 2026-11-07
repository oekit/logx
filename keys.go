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
