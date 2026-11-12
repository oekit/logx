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

// IsHlogBacked 检查 Logger 是否基于 hlog 实现（含经包装器的间接持有）。
//
// 将 hlog 后端的 Logger 传入 hertz.NewLogger 或 kitexlog.NewLogger 会形成
// adapter → logx → hlog → adapter 的无限递归，导致栈溢出和进程挂死。
// 此函数供框架适配层做运行时防御。
//
// #106：此前只做一层标记接口断言——WithFilter/Delegating/audit 包装一层后
// 断言静默失败，递归防御在最常见的包装用法下失效。现在沿 Unwrap() 链
// 向下钻取（层数上限 8，防自环包装），logx 自带包装器均已实现 Unwrap；
// 三方包装器实现 `Unwrap() Logger` 即可参与探测。
func IsHlogBacked(l Logger) bool {
	for range 8 {
		if _, ok := l.(HlogBacked); ok {
			return true
		}
		u, ok := l.(interface{ Unwrap() Logger })
		if !ok {
			return false
		}
		if l = u.Unwrap(); l == nil {
			return false
		}
	}
	return false
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
		// key（#174 string 键直取，非 string 键才走 %v 归一）
		sb.WriteString(keyString(keysAndValues[i]))
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
