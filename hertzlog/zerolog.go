package hertzlog

import (
	"io"

	hertzcontribzerolog "github.com/hertz-contrib/logger/zerolog"
	"github.com/rs/zerolog"
)

// ZerologLogger 是 hertz-contrib/logger/zerolog.Logger 的 type alias。
type ZerologLogger = hertzcontribzerolog.Logger

// ZerologOpt 是 hertz-contrib/logger/zerolog.Opt 的 type alias
// (上游用 Opt 命名而不是 Option,ekit 沿用)。
type ZerologOpt = hertzcontribzerolog.Opt

// NewZerologLogger 透传 hertz-contrib/logger/zerolog.From(接受已构造
// 的 zerolog.Logger)。上游 API 用 From 不是 New。
//
// 用法:
//
//	zlog := zerolog.New(os.Stderr).With().Timestamp().Logger()
//	ekitzlog := hertzlog.NewZerologLogger(zlog)
//	hlog.SetLogger(hertzlog.AsHlogLoggerFromZerolog(ekitzlog))
func NewZerologLogger(log zerolog.Logger, opts ...ZerologOpt) *ZerologLogger {
	return hertzcontribzerolog.From(log, opts...)
}

// WithZerologOutput 透传 WithOutput(io.Writer),业务可链式调用。
func WithZerologOutput(w io.Writer) ZerologOpt {
	return hertzcontribzerolog.WithOutput(w)
}
