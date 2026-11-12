package logx

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"
)

// 本文件承载内置 JSON 路径的编码（#166 自 slog.go 按关注点拆出）。
//
// #170 重写：此前每条日志分配一个 map[string]any（map 头 + bucket 扩容）+
// 反射式 json.Marshal 整张 map + ts.Format 分配，是逐行热路径的最大分配源。
// 现改为 append 式编码进 sync.Pool 的 []byte：
//   - 字段以有序 jsonField 切片承载（重键按 map 语义「后者覆盖前者」原地
//     覆盖，不产生重复键）；
//   - 标量快路径（string/bool/整数族）直写缓冲，与 encoding/json 字节同构；
//     其余值（浮点/复合类型/自定义 Marshaler 等）仍走 json.Marshal 单值
//     编码——快路径覆盖日志 kv 的绝大多数形态，复杂值不构成行为差异；
//   - 时间戳经 AppendFormat 直写，不再产生中间串。

// jsonBufPool 复用编码缓冲；512B 起步覆盖典型一行记录，增长后随 Put 保留。
var jsonBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 512)
		return &b
	},
}

// jsonField 是内置 JSON 记录的有序字段。
type jsonField struct {
	key string
	val any
}

// setField 落一个字段进有序字段表（map 语义：同键后者覆盖前者，位置保持
// 首次出现的顺序）。
func setField(fields []jsonField, key string, v any) []jsonField {
	for i := range fields {
		if fields[i].key == key {
			fields[i].val = v
			return fields
		}
	}
	return append(fields, jsonField{key: key, val: v})
}

// setFieldIfAbsent 落一个字段，已存在时保留原值（#100 降级路径的
// 「框架字段/先到字段优先」语义）。
func setFieldIfAbsent(fields []jsonField, key string, v any) []jsonField {
	for i := range fields {
		if fields[i].key == key {
			return fields
		}
	}
	return append(fields, jsonField{key: key, val: v})
}

// emitBuiltinJSON 渲染内置 JSON 记录并写 stdout（无注入 Handler 时的路径）。
//
// 框架字段名集合（#68）：time/level/message/service/trace_id/caller 由本函数
// 发布，用户 Attr/kv 同键冲突时**框架字段优先**、用户键加 "_" 前缀落地并打
// "_renamed" 标记——此前 map 赋值让用户值静默覆盖框架字段（trace_id 被覆盖 =
// 追踪链断）。用户字段命名空间前缀（#75 #83）：WithGroup 累积的组以
// "<g1>.<g2>." 前缀施加到用户 Attr/kv 键，框架字段不进组。
func (l *slogLogger) emitBuiltinJSON(ctx context.Context, level Level, msg string, pc uintptr, ts time.Time, keysAndValues ...any) {
	if ts.IsZero() {
		ts = time.Now()
	}
	groupPrefix := ""
	if len(l.groups) > 0 {
		groupPrefix = joinGroups(l.groups)
	}

	bp := jsonBufPool.Get().(*[]byte)
	buf := (*bp)[:0]
	fields := make([]jsonField, 0, 6+len(l.Attrs)+len(keysAndValues)/2+1)

	// 框架字段（固定顺序发布；reservedFields 同步维护）
	fields = append(fields,
		jsonField{"time", ts.Format(time.RFC3339Nano)},
		jsonField{"level", LevelString(level)},
		jsonField{"message", msg},
	)
	if l.Service != "" {
		fields = append(fields, jsonField{"service", l.Service})
	}
	if traceID := ExtractTrace(ctx); traceID != "" {
		fields = append(fields, jsonField{"trace_id", traceID})
	}
	var caller string
	if pc != 0 {
		caller = pcCaller(pc)
	} else {
		caller = callerOutsideLogx()
	}
	if caller != "" {
		fields = append(fields, jsonField{"caller", caller})
	}

	// 用户字段：WithAttrs 持久属性 + 本次调用的 kv 对；框架字段冲突改名 +
	// "_renamed" 标记（#68），组前缀施加（#75/#83）。冲突判定按**组前缀后的
	// 完整键**（putRecordField 历史口径）："g.time" 是组内用户键、不撞框架
	// 字段；改名落地为 "_key"（不带组前缀，与旧实现一致）。
	var renamed []string
	put := func(key string, v any) {
		full := groupPrefix + key
		if reservedFields[full] {
			renamed = append(renamed, full)
			full = "_" + key // 改名键直接覆盖用户自造的同名 "_" 键（只损失用户数据）
		}
		fields = setField(fields, full, v)
	}
	for _, a := range l.Attrs {
		put(a.Key, logValue(a.Value))
	}
	for i := 0; i+1 < len(keysAndValues); i += 2 {
		put(keyString(keysAndValues[i]), logValue(keysAndValues[i+1])) // #174 string 键直取
	}
	if len(keysAndValues)%2 == 1 {
		put(keyString(keysAndValues[len(keysAndValues)-1]), "(MISSING)")
	}
	if len(renamed) > 0 {
		fields = setField(fields, "_renamed", renamed)
	}

	buf, err := appendRecordJSON(buf, fields)
	if err != nil {
		// 序列化失败（NaN/channel/func/循环引用等不可序列化值）：
		// 降级为纯字符串输出，绝不静默丢弃整条日志
		buf = marshalFallback(fields, level, msg, err, keysAndValues, groupPrefix, l.Attrs)
	}
	buf = append(buf, '\n')
	// 换行并入同一次 Write（#65）：两次 Write 之间无原子性保证，并发下另一
	// goroutine 的记录体会插进本条与换行之间 ⇒ JSONL 拼行/空行，下游逐行解析
	// 静默丢日志。对齐 log.Printf / log/slog JSONHandler 的整行单写纪律。
	writeStdout(buf)
	*bp = buf[:0]
	jsonBufPool.Put(bp)
}

// joinGroups 拼组命名空间前缀（"a" + "b" → "a.b."）。
func joinGroups(groups []string) string {
	n := len(groups)
	for _, g := range groups {
		n += len(g)
	}
	out := make([]byte, 0, n+1)
	for i, g := range groups {
		if i > 0 {
			out = append(out, '.')
		}
		out = append(out, g...)
	}
	return string(out) + "."
}

// appendRecordJSON 把有序字段表编码为一个 JSON 对象（不含换行）。
// 任一值不可序列化时返回 error——调用方降级到 marshalFallback，绝不静默
// 丢弃整条日志（#79/#100 同一口径）。
func appendRecordJSON(dst []byte, fields []jsonField) ([]byte, error) {
	dst = append(dst, '{')
	for i, f := range fields {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = appendJSONString(dst, f.key)
		dst = append(dst, ':')
		var err error
		dst, err = appendJSONValue(dst, f.val)
		if err != nil {
			return dst, err
		}
	}
	return append(dst, '}'), nil
}

// marshalFallback 渲染序列化失败时的降级记录（#166 自 emitAt 的降级块抽出；
// #170 改走与正常路径相同的 append 式编码——全部字段均为字符串，不会失败）。
//
// 序列化失败（NaN/channel/func/循环引用等不可序列化值）：降级为纯字符串输出，
// 绝不静默丢弃整条日志。降级不脱钩（#100）：service/trace_id/caller 取自
// 已发布字段，WithAttrs 持久属性逐键平铺（值统一转字符串）——一条 NaN 不该
// 让整条日志失去可检索性。框架降级字段优先，用户同名键让位。
// rawKv 走 FormatKeysAndValues（#136）：产出 "k1=v1 k2=v2" 且奇数长度补
// (MISSING)，与 std/hlog 后端的 kv 渲染同一口径。
func marshalFallback(fields []jsonField, level Level, msg string, marshalErr error, keysAndValues []any, groupPrefix string, attrs []Attr) []byte {
	out := []jsonField{
		{key: "level", val: LevelString(level)},
		{key: "message", val: msg},
		{key: "marshalErr", val: marshalErr.Error()},
		{key: "rawKv", val: FormatKeysAndValues(keysAndValues...)},
	}
	for _, k := range [...]string{"service", "trace_id", "caller"} {
		for _, f := range fields {
			if f.key == k {
				if s, ok := f.val.(string); ok {
					out = append(out, jsonField{k, s})
				}
				break
			}
		}
	}
	for _, a := range attrs {
		k := groupPrefix + a.Key
		out = setFieldIfAbsent(out, k, fmt.Sprintf("%v", logValue(a.Value)))
	}

	buf := make([]byte, 0, 256)
	buf = append(buf, '{')
	for i, f := range out {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = appendJSONString(buf, f.key)
		buf = append(buf, ':')
		buf = appendJSONString(buf, f.val.(string))
	}
	return append(buf, '}')
}

// appendJSONValue 追加一个 JSON 值。标量快路径与 encoding/json 字节同构；
// 其余值单值走 json.Marshal（NaN/Inf、channel、func、循环引用等在此报错，
// 由调用方触发降级）。与整表反射 Marshal 相比，快路径消除了常见 kv 形态
// 的反射与分配（#170）。
func appendJSONValue(dst []byte, v any) ([]byte, error) {
	switch x := v.(type) {
	case nil:
		return append(dst, "null"...), nil
	case string:
		return appendJSONString(dst, x), nil
	case bool:
		if x {
			return append(dst, "true"...), nil
		}
		return append(dst, "false"...), nil
	case int:
		return strconv.AppendInt(dst, int64(x), 10), nil
	case int8:
		return strconv.AppendInt(dst, int64(x), 10), nil
	case int16:
		return strconv.AppendInt(dst, int64(x), 10), nil
	case int32:
		return strconv.AppendInt(dst, int64(x), 10), nil
	case int64:
		return strconv.AppendInt(dst, x, 10), nil
	case uint:
		return strconv.AppendUint(dst, uint64(x), 10), nil
	case uint8:
		return strconv.AppendUint(dst, uint64(x), 10), nil
	case uint16:
		return strconv.AppendUint(dst, uint64(x), 10), nil
	case uint32:
		return strconv.AppendUint(dst, uint64(x), 10), nil
	case uint64:
		return strconv.AppendUint(dst, x, 10), nil
	default:
		enc, err := json.Marshal(v)
		if err != nil {
			return dst, err
		}
		return append(dst, enc...), nil
	}
}

// appendJSONString 追加一个 JSON 字符串字面量（含引号）。转义口径与
// encoding/json 默认一致：HTML 转义开启（< > & → \u00xx）、U+2028/U+2029
// 转义——字节同构由 TestAppendJSONString_MatchesEncodingJSON 对语料钉住。
// 非法 UTF-8 字节（日志里的罕见脏值）整体回退 json.Marshal：该边角的渲染
// 随工具链演进有差异（go1.26 落 \ufffd 转义文本、go1.27 落 U+FFFD 原字符），
// 手写快路径不自行规定口径，回退保证与构建工具链的 encoding/json 恒字节
// 一致。
func appendJSONString(dst []byte, s string) []byte {
	const hex = "0123456789abcdef"
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(s); {
		if b := s[i]; b < utf8.RuneSelf {
			if htmlSafeSet[b] {
				i++
				continue
			}
			dst = append(dst, s[start:i]...)
			switch b {
			case '\\', '"':
				dst = append(dst, '\\', b)
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			default:
				// <0x20 的其余控制字符与 <、>、& 按通用 \u00xx 转义
				dst = append(dst, '\\', 'u', '0', '0', hex[b>>4], hex[b&0xF])
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// 非法 UTF-8：罕见路径，回退 json.Marshal（含外层引号整个重编，
			// 撤掉本函数已追加的开引号）
			enc, err := json.Marshal(s)
			if err != nil { // 字符串序列化不会失败；防御性保底
				return append(dst, '"')
			}
			return append(dst[:len(dst)-1], enc...)
		}
		if r == '\u2028' || r == '\u2029' {
			dst = append(dst, s[start:i]...)
			if r == '\u2028' {
				dst = append(dst, `\u2028`...)
			} else {
				dst = append(dst, `\u2029`...)
			}
			i += size
			start = i
			continue
		}
		i += size
	}
	dst = append(dst, s[start:]...)
	return append(dst, '"')
}

// reservedFields 是内置 JSON 路径的框架字段名集合（#68/#87）。
// 初始化后只读，并发访问安全。
var reservedFields = map[string]bool{
	"time": true, "level": true, "message": true,
	"service": true, "trace_id": true, "caller": true,
}

// htmlSafeSet 标记 ASCII 码位是否无需转义（encoding/json safeSet 同表）：
// < 0x20 全转义；'"'、'\\'、'<'、'>'、'&' 转义；0x7F 不转义。
var htmlSafeSet [utf8.RuneSelf]bool

func init() {
	for i := range utf8.RuneSelf {
		htmlSafeSet[i] = i >= 0x20 && i != '"' && i != '\\' && i != '<' && i != '>' && i != '&'
	}
}
