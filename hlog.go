package logx

import (
	"fmt"
	"strings"
)

// HlogBacked 是 hlog 实现的标记接口。
// hlog 子包的 hlogLogger 实现此接口，供 IsHlogBacked 运行时检查。
type HlogBacked interface {
	IsHlogLogger()
}

// IsHlogBacked 检查 Logger 是否基于 hlog 实现。
//
// 将 hlog 后端的 Logger 传入 hertz.NewLogger 或 kitexlog.NewLogger 会形成
// adapter → logx → hlog → adapter 的无限递归，导致栈溢出和进程挂死。
// 此函数供框架适配层做运行时防御。
func IsHlogBacked(l Logger) bool {
	_, ok := l.(HlogBacked)
	return ok
}

// FormatKeysAndValues 将 key-value 对格式化为可读字符串。
// 输入: ["key1", val1, "key2", val2]
// 输出: "key1=val1 key2=val2"
func FormatKeysAndValues(keysAndValues ...any) string {
	if len(keysAndValues) == 0 {
		return ""
	}

	var sb strings.Builder
	for i := 0; i < len(keysAndValues); i += 2 {
		if i > 0 {
			sb.WriteByte(' ')
		}
		// key
		key := fmt.Sprintf("%v", keysAndValues[i])
		sb.WriteString(key)
		sb.WriteByte('=')
		// value
		if i+1 < len(keysAndValues) {
			fmt.Fprintf(&sb, "%v", keysAndValues[i+1])
		} else {
			sb.WriteString("(MISSING)")
		}
	}
	return sb.String()
}
