// 统一适配入口（2026-10-15 命名对齐）：历史 API 里仅 zap 是 AsHlogLogger、
// 其余为 AsHlogLoggerFromXxx；现已统一为 AsHlogLoggerXxx 系列（推荐），
// 旧名 AsHlogLogger/AsHlogLoggerFromXxx 已删除（v0.1.7）。
// 四个后端 Logger 均直接实现 hlog.FullLogger，适配即透传。

package hertzlog

import (
	"github.com/cloudwego/hertz/pkg/common/hlog"
)

// AsHlogLoggerZap 把 ekit ZapLogger 适配为 hertz hlog。
func AsHlogLoggerZap(z *ZapLogger) hlog.FullLogger { return z }

// AsHlogLoggerLogrus 把 ekit LogrusLogger 适配为 hertz hlog。
func AsHlogLoggerLogrus(l *LogrusLogger) hlog.FullLogger { return l }

// AsHlogLoggerSlog 把 ekit SlogLogger 适配为 hertz hlog。
func AsHlogLoggerSlog(l *SlogLogger) hlog.FullLogger { return l }

// AsHlogLoggerZerolog 把 ekit ZerologLogger 适配为 hertz hlog。
func AsHlogLoggerZerolog(l *ZerologLogger) hlog.FullLogger { return l }
