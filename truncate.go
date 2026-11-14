package logx

import (
	"sync/atomic"
	"unicode/utf8"
)

// maxValueLen 是 emit 侧的值截断上限（字节）；0 = 不限（默认关闭，logx#223）。
//
// 为什么需要：单行超采集端上限（Loki 默认 256KB/行、ES max_content_length、
// 多数采集器对超长行直接丢且只报 entry-too-large）时，**整条日志在管道侧
// 消失**——而 StdoutWriteFailures() 为 0、进程日志面完全正常，排障时
// 「服务没日志了」查不到 logx 头上。丢的恰是最需要的那条大日志。
//
// 默认关闭（不改变既有行为）；生产建议 SetMaxValueLen(8192)。启用后超限
// 的 string/[]byte 值就地截断并带自描述尾巴（可观测，不静默）。
var maxValueLen atomic.Int64

// SetMaxValueLen 设置 emit 侧值截断上限（字节）。n<=0 关闭（默认）。
// 截断对 string/[]byte 值生效（fmt 渲染的其他类型不经此路径——求值成本
// 与反射面控制在字符串族），截断后缀自带 "…[logx truncated]" 标记。
func SetMaxValueLen(n int) {
	if n < 0 {
		n = 0
	}
	maxValueLen.Store(int64(n))
}

// MaxValueLen 返回当前上限（0 = 关闭）。
func MaxValueLen() int { return int(maxValueLen.Load()) }

// TruncateForLog 把超限的字符串截到 n 字节（按 rune 边界回退，不劈开
// 多字节字符）并追加自描述尾巴；n<=0 或未超限原样返回。
//
// 这是 logx 文档示例里消费方各自手搓的 truncateForLog 的**单源收编**
// （logx#223）：全矩阵此前无统一原语，Enabled 文档示例教的函数并不存在
// 于本包。与 SetMaxValueLen 的 emit 侧截断互补——本函数给「显式要截的
// 场景」（如 transport body 入参前）用。
func TruncateForLog(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…[logx truncated]"
}

// truncateValue 按 SetMaxValueLen 上限截断单个值（logx#223 emit 侧钩子）：
// 只处理 string/[]byte（管道超长的两族主形态），返回 (值, 是否截断)。
// 上限关闭时零开销直接返回。
func truncateValue(v any) (any, bool) {
	n := maxValueLen.Load()
	if n <= 0 {
		return v, false
	}
	switch x := v.(type) {
	case string:
		if len(x) > int(n) {
			return TruncateForLog(x, int(n)), true
		}
	case []byte:
		if len(x) > int(n) {
			cut := int(n)
			for cut > 0 && !utf8.RuneStart(x[cut]) {
				cut--
			}
			out := make([]byte, 0, cut+len("…[logx truncated]"))
			out = append(out, x[:cut]...)
			out = append(out, "…[logx truncated]"...)
			return out, true
		}
	}
	return v, false
}

// truncateKvs 截断一组调用 kv 的值（奇数位），返回是否发生截断；
// 未截断时原样返回入参（零分配快路径）。
func truncateKvs(kvs []any) ([]any, bool) {
	if maxValueLen.Load() <= 0 || len(kvs) == 0 {
		return kvs, false
	}
	changed := false
	out := make([]any, len(kvs))
	copy(out, kvs)
	for i := 1; i < len(out); i += 2 {
		if v, ok := truncateValue(out[i]); ok {
			out[i] = v
			changed = true
		}
	}
	return out, changed
}

// truncateAttrs 截断一组 Attr 的值（std 后端持久属性路径），返回是否发生
// 截断；未截断时原样返回入参（零分配快路径）。
func truncateAttrs(attrs []Attr) ([]Attr, bool) {
	if maxValueLen.Load() <= 0 || len(attrs) == 0 {
		return attrs, false
	}
	changed := false
	out := make([]Attr, len(attrs))
	copy(out, attrs)
	for i := range out {
		if v, ok := truncateValue(out[i].Value); ok {
			out[i].Value = v
			changed = true
		}
	}
	return out, changed
}
