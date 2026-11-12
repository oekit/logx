package logx

import (
	stderrors "errors"
	"log/slog"
)

// kvCarrier 是「沿错误链携带 kv 对」的错误的结构化 duck-type 接口（#135）。
//
// 与 oekit/pkg/errors 的 KVCarrier（error + Kv() []any）完全同构：pkg 侧
// Wrap 构造的 wrappedError 及实现该接口的第三方包装体天然满足。logx 的依赖
// 纪律是零业务依赖，不 import pkg/errors——靠方法集匹配对接，pkg 侧
// errors/wrap.go:58/96/116 三处引用的「logx 侧 ErrAttrs 形态」由此落地。
type kvCarrier interface {
	error
	// Kv 返回本层附带的上下文 kv 对（无 kv 返回 nil 或空切片均可）。
	Kv() []any
}

// maxErrAttrsDepth 是 ErrAttrs 沿链遍历的节点数上限（#135）：对齐 pkg/errors
// biz_error.go maxFlattenDepth=32 的成环防御纪律——成环 Unwrap 链（A 包 B、
// B 又包 A）若无上限会让遍历永久迭代。超限返回已收集的部分并停止。
const maxErrAttrsDepth = 32

// ErrAttrs 沿错误链收集每层 Wrap 附带的 kv（pkg/errors.KVCarrier，#135），
// 展开为 []slog.Attr——错误文本保留在调用方自选的 err 字段（建议用
// ErrKey 常量），链上 kv 升格为独立 slog 字段（检索/聚合/按 user_id 等
// 上下文过滤全通），不再退化成 error 字符串里的一段文本。
//
// 这是 pkg/errors #292 结构化出口（Kv/KvFromChain）在 logx 侧承诺的接收端：
// 三仓 113 处 errors.Wrap(err, msg, kv...) 的错误上下文（user_id/task_key/
// channel_id）从此可以一次升格，无需每个调用点手写展开循环。
//
// 用法（err 字段保留文本 + kv 升格为独立字段）：
//
//	err := errors.Wrap(err, "query user", "user_id", uid) // oekit/pkg/errors
//
// 原生 slog.Logger 直接消费（[]slog.Attr 直通）：
//
//	slog.Error("query failed", append(attrs, logx.ErrAttrs(err)...)...)
//
// logx 自有 Logger 的 WithAttrs 收 logx.Attr，逐条转换后挂载：
//
//	for _, a := range logx.ErrAttrs(err) {
//	    logger = logger.WithAttrs(logx.Attr{Key: a.Key, Value: a.Value.Any()})
//	}
//	logger.CtxError(ctx, "query failed", logx.ErrKey, err)
//	// → JSON: {"message":"query failed","err":"query user: ... user_id=42",
//	//          "user_id":42,...}
//
// 语义与 pkg/errors.KvFromChain 一致：
//   - 链上**每层**携带 kv 的节点依序展开，外层在前、内层在后；多分支链
//     （Unwrap() []error，如 errors.Join 形态）深度先序遍历。
//   - **不去重**：链上多层同名 key 全部保留（pkg 侧已声明去重交给日志端，
//     去重会掩盖「两层都带 user_id 但值不同」的异常现场），最终值由日志端
//     的后写覆盖语义决定。logx 内置 JSON 路径上 WithAttrs 的 Attr 先落地、
//     本次调用的 kv 后落地——调用方显式 kv 与链上 kv 同名时显式 kv 覆盖
//     （与 slog Attrs 重复 key 的后写行为一致）。
//   - kv 对长度为奇数时，末尾孤值按 slog 约定落 "!BADKEY" 键（对齐
//     pkg/errors formatKV 的 "!BADKEY=<value>" 渲染）。
//   - err 为 nil、链上无 KVCarrier 层或 kv 全空时返回 nil。
//
// 键经 fmt.Sprintf("%v", k) 字符串化（与 logx kv 风格的键处理同一口径），
// 值经 slog.AnyValue 保留原始类型（slog 后端自行渲染 error/数值）。
//
// 自动升格（识别 err 字段值实现 KVCarrier 时在 emit 内自动展开）留作后续
// 增强；当前先提供显式出口的最小面。
func ErrAttrs(err error) []slog.Attr {
	if err == nil {
		return nil
	}
	var out []slog.Attr
	visited := 0
	var walk func(e error)
	walk = func(e error) {
		for e != nil {
			visited++
			if visited > maxErrAttrsDepth {
				// 成环链防御：返回已收集部分，不悬挂（对齐 pkg/errors
				// maxFlattenDepth 纪律）。
				return
			}
			if c, ok := e.(kvCarrier); ok {
				if kv := c.Kv(); len(kv) > 0 {
					out = appendKvAttrs(out, kv)
				}
			}
			// 多分支链（Unwrap() []error，如 errors.Join 形态）深度先序；
			// stderrors.Unwrap 只认 Unwrap() error，多错误形态对它返回 nil。
			if m, ok := e.(interface{ Unwrap() []error }); ok {
				for _, sub := range m.Unwrap() {
					walk(sub)
				}
				return
			}
			u := stderrors.Unwrap(e)
			if u == nil {
				return
			}
			e = u
		}
	}
	walk(err)
	return out
}

// appendKvAttrs 把一节点的 kv 对展开追加进 attrs。奇数长度时末尾孤值落
// "!BADKEY"（slog 约定，对齐 pkg/errors formatKV 渲染）。
func appendKvAttrs(attrs []slog.Attr, kv []any) []slog.Attr {
	for i := 0; i < len(kv); i += 2 {
		if i+1 >= len(kv) {
			return append(attrs, slog.Attr{Key: "!BADKEY", Value: slog.AnyValue(kv[i])})
		}
		attrs = append(attrs, slog.Any(keyString(kv[i]), kv[i+1])) // #174 string 键直取
	}
	return attrs
}
