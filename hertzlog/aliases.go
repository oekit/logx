package hertzlog

import (
	"github.com/cloudwego/hertz/pkg/common/hlog"
)

// 命名对齐(2026-09-06 复审):历史 API 里仅 zap 是 AsHlogLogger、其余为
// AsHlogLoggerFromXxx,AGENTS.md 记载的又是第三种形式。统一提供
// AsHlogLoggerXxx 系列(推荐),旧函数保留不破坏编译。

// AsHlogLoggerZap 把 ekit ZapLogger 适配为 hertz hlog。
func AsHlogLoggerZap(z *ZapLogger) hlog.FullLogger { return AsHlogLogger(z) }

// AsHlogLoggerLogrus 把 ekit LogrusLogger 适配为 hertz hlog。
func AsHlogLoggerLogrus(l *LogrusLogger) hlog.FullLogger { return AsHlogLoggerFromLogrus(l) }

// AsHlogLoggerSlog 把 ekit SlogLogger 适配为 hertz hlog。
func AsHlogLoggerSlog(l *SlogLogger) hlog.FullLogger { return AsHlogLoggerFromSlog(l) }

// AsHlogLoggerZerolog 把 ekit ZerologLogger 适配为 hertz hlog。
func AsHlogLoggerZerolog(l *ZerologLogger) hlog.FullLogger { return AsHlogLoggerFromZerolog(l) }
