package logx

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// BaseLogger 包含所有 Logger 实现的公共字段和方法。
// 通过嵌入消除 hlog/std/slog 三个实现的重复代码。
//
// 子包（如 logx/hlog）可嵌入此结构体复用公共逻辑。
type BaseLogger struct {
	Service string
	Level   Level
	Attrs   []Attr
	Ctx     context.Context
	Mu      sync.Mutex
}

// NewBaseLogger 创建 BaseLogger 实例。
func NewBaseLogger(service string, level Level) BaseLogger {
	return BaseLogger{Service: service, Level: level}
}

// Level 日志级别
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// levelStrings 日志级别名称数组。
var levelStrings = [4]string{"DEBUG", "INFO", "WARN", "ERROR"}

// LevelString 返回日志级别的字符串表示。
func LevelString(level Level) string {
	if level >= 0 && int(level) < len(levelStrings) {
		return levelStrings[level]
	}
	return "UNKNOWN"
}

// Snapshot 在锁内拷贝公共字段。clone/With* 派生路径必须经它取值：
// Level 可经 SetLevel 运行期变更，无锁快照读与写构成 data race
// （-race 下并发请求路径 + 动态调级可稳定检出）。导出供子包（logx/hlog）
// 嵌入使用。
func (b *BaseLogger) Snapshot() BaseLogger {
	b.Mu.Lock()
	defer b.Mu.Unlock()
	return BaseLogger{Service: b.Service, Level: b.Level, Attrs: b.Attrs, Ctx: b.Ctx}
}

// EffectiveCtx 返回绑定 ctx（无绑定则 context.Background()）。
func (b *BaseLogger) EffectiveCtx() context.Context {
	if b.Ctx != nil {
		return b.Ctx
	}
	return context.Background()
}

// ShouldLog 检查给定级别是否应被记录。
func (b *BaseLogger) ShouldLog(level Level) bool {
	b.Mu.Lock()
	defer b.Mu.Unlock()
	return level >= b.Level
}

// Enabled 判定给定级别当前是否会被输出——LevelEnabler 接口的 BaseLogger 实现。
// 嵌入 BaseLogger 的后端（std/slog/hlog 等）自动获得该方法。
func (b *BaseLogger) Enabled(level Level) bool {
	return b.ShouldLog(level)
}

// SetLevel 设置该 logger 实例的日志级别。
func (b *BaseLogger) SetLevel(level Level) {
	b.Mu.Lock()
	b.Level = level
	b.Mu.Unlock()
}

// Prefix 返回 service 前缀（如 "[my-service]"）。
func (b *BaseLogger) Prefix() string {
	if b.Service != "" {
		return fmt.Sprintf("[%s]", b.Service)
	}
	return ""
}

// MergeAttrs 合并已有属性和新属性，返回新的切片。
func (b *BaseLogger) MergeAttrs(attrs ...Attr) []Attr {
	b.Mu.Lock()
	defer b.Mu.Unlock()
	merged := make([]Attr, 0, len(b.Attrs)+len(attrs))
	merged = append(merged, b.Attrs...)
	merged = append(merged, attrs...)
	return merged
}

// FormatAttrs 将 Attr 列表格式化为 "k=v k=v" 字符串。
func FormatAttrs(attrs ...Attr) string {
	if len(attrs) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, a := range attrs {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(a.Key)
		sb.WriteByte('=')
		fmt.Fprintf(&sb, "%v", a.Value)
	}
	return sb.String()
}
