package hertzlog

import (
	"github.com/cloudwego/hertz/pkg/common/hlog"
	hertzcontribslog "github.com/hertz-contrib/logger/slog"
)

// SlogLogger 是 hertz-contrib/logger/slog.Logger 的 type alias
// (适配 Go 1.21+ 标准库 log/slog)。
type SlogLogger = hertzcontribslog.Logger

// SlogOption 是 hertz-contrib/logger/slog.Option 的 type alias。
type SlogOption = hertzcontribslog.Option

// NewSlogLogger 透传 hertz-contrib/logger/slog.NewLogger。
// 业务用 hertz-contrib/logger/slog.WithHandlerOptions(&slog.HandlerOptions{...})
// 等注入上游 logger(具体 Option 列表见 hertz-contrib/logger/slog)。
func NewSlogLogger(opts ...SlogOption) *SlogLogger {
	return hertzcontribslog.NewLogger(opts...)
}

// AsHlogLoggerFromSlog 适配 *SlogLogger → hlog.FullLogger。
func AsHlogLoggerFromSlog(l *SlogLogger) hlog.FullLogger {
	return l
}
