// Package hertzlog 把 hertz-contrib/logger 4 个子包(logrus/zap/zerolog/slog)
// 桥接成统一 ekit 风格,让 ekit 业务按需 import 1 个子包并 1 行接 hertz
// 框架 hlog。
//
// 官方依据:https://github.com/cloudwego/hertz 官方 README
// [Extensions] 段 "Logger" 行,推荐 4 个三方日志框架适配器:
//   - logrus: github.com/hertz-contrib/logger/logrus
//   - zap:    github.com/hertz-contrib/logger/zap
//   - zerolog: github.com/hertz-contrib/logger/zerolog
//   - slog:   github.com/hertz-contrib/logger/slog
//
// 业务用法(以 zap 为例):
//
//	import (
//	    hertzlog "github.com/cloudwego/hertz/pkg/common/hlog"
//	    hertzcontribzap "github.com/hertz-contrib/logger/zap"
//	    ekitlogx "github.com/oekit/logx/hertzlog"
//	)
//
//	ekitzap := hertzcontribzap.NewLogger(hertzcontribzap.WithLogger(zap.NewProduction()))
//	hertzlog.SetLogger(ekitlogx.AsHlogLogger(ekitzap))
//
// 设计:本子包只做 type alias 透传,业务可读 hertz-contrib 子包
// 的 WithXxx Option,跟 hertz-contrib 同名 NewLogger 一致。
package hertzlog

import (
	"github.com/cloudwego/hertz/pkg/common/hlog"
	hertzcontribzap "github.com/hertz-contrib/logger/zap"
)

// ZapLogger 是 hertz-contrib/logger/zap.Logger 的 type alias。
type ZapLogger = hertzcontribzap.Logger

// ZapOption 是 hertz-contrib/logger/zap.Option 的 type alias。
type ZapOption = hertzcontribzap.Option

// NewZapLogger 透传 hertz-contrib/logger/zap.NewLogger。
// 业务用 hertz-contrib/logger/zap.WithLogger(zap.NewProduction()) 等
// 注入上游 logger(具体 Option 列表见 hertz-contrib/logger/zap)。
func NewZapLogger(opts ...ZapOption) *ZapLogger {
	return hertzcontribzap.NewLogger(opts...)
}

// AsHlogLogger 把 ekit 子包包装的 *ZapLogger 适配成 hertz 框架 hlog.FullLogger
// interface,业务用 hlog.SetLogger(AsHlogLogger(zapLogger)) 即可。
// (ZapLogger 本身已经实现 hlog.FullLogger,直接返回)
func AsHlogLogger(z *ZapLogger) hlog.FullLogger {
	return z
}
