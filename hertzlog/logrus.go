package hertzlog

import (
	hertzcontriblogrus "github.com/hertz-contrib/logger/logrus"
)

// LogrusLogger 是 hertz-contrib/logger/logrus.Logger 的 type alias。
type LogrusLogger = hertzcontriblogrus.Logger

// LogrusOption 是 hertz-contrib/logger/logrus.Option 的 type alias。
type LogrusOption = hertzcontriblogrus.Option

// NewLogrusLogger 透传 hertz-contrib/logger/logrus.NewLogger。
// 业务用 hertz-contrib/logger/logrus.WithLogger(logrus.New()) 等
// 注入上游 logger(具体 Option 列表见 hertz-contrib/logger/logrus)。
func NewLogrusLogger(opts ...LogrusOption) *LogrusLogger {
	return hertzcontriblogrus.NewLogger(opts...)
}
