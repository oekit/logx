package logx

import (
	"context"
)

// 本文件承载审计留痕 Logger（#166 自 slog.go 按关注点拆出）。

// NewAuditLogger 创建审计/合规/安全留痕类 Logger。
//
// 级别锁定在 LevelInfo（及更细），不受 LOG_LEVEL 环境变量降级；SetLevel
// 试图调到 WARN/ERROR 会被拒绝——审计事件不得被部署配置静默过滤。
// 需要更细粒度可用 SetLevel(LevelDebug)。
//
// 业务日志请用 NewSlogLogger；本构造仅用于「留痕不可丢」的路径
// （登录、越权、配置变更、合规操作等）。
func NewAuditLogger(service string, opts ...SlogOption) Logger {
	// #172：先拷贝再追加哨兵选项——直接 `opts = append(opts, ...)` 会向
	// 调用方 variadic 切片的**后备数组**原地写入：调用方以 len<cap 的切片
	// 传入时（更长的共享视图同源），WithLevel 写进空洞即别名写穿，调用方
	// 后续 append/复用该切片时内容被静默污染。
	all := make([]SlogOption, len(opts), len(opts)+1)
	copy(all, opts)
	// WithLevel 置于最后，覆盖 opts 里可能的降级与 LOG_LEVEL
	all = append(all, WithLevel(LevelInfo))
	return auditLogger{Logger: NewSlogLogger(service, all...)}
}

// auditLogger 包装任意 Logger，拒绝将级别调到会吞掉 Info 审计事件的档位。
//
// #162：16 个纯转发方法改为接口嵌入——Logger 的全部方法经提升自动转发
// 内部 logger，只覆写需要审计守卫/重包装的方法（SetLevel/With*/Unwrap/
// Enabled）。此后 Logger 接口新增方法时本包装器自动跟进，不再三处同步。
type auditLogger struct{ Logger }

func (a auditLogger) SetLevel(level Level) {
	if level > LevelInfo {
		return // 拒绝降级：Info 级审计必须保留
	}
	a.Logger.SetLevel(level)
}

func (a auditLogger) WithCtx(ctx context.Context) Logger {
	return auditLogger{Logger: a.Logger.WithCtx(ctx)}
}
func (a auditLogger) WithService(name string) Logger {
	return auditLogger{Logger: a.Logger.WithService(name)}
}
func (a auditLogger) WithAttrs(attrs ...Attr) Logger {
	return auditLogger{Logger: a.Logger.WithAttrs(attrs...)}
}
func (a auditLogger) WithGroup(name string) Logger {
	return auditLogger{Logger: a.Logger.WithGroup(name)}
}

// WithAttrKeyMask 实现 AttrKeyMasker（#194）：内部 logger 支持下沉时转发
// 键集——审计留痕场景同样不得让敏感值绕过过滤直渲；不支持时保持原样
// （边界见 WithFilter godoc）。
func (a auditLogger) WithAttrKeyMask(keys map[string]struct{}) Logger {
	inner := a.Logger
	if m, ok := inner.(AttrKeyMasker); ok {
		inner = m.WithAttrKeyMask(keys)
	}
	return auditLogger{Logger: inner}
}

// Unwrap 暴露被包装的内部 logger（#106）：IsHlogBacked 沿 Unwrap 链钻取。
func (a auditLogger) Unwrap() Logger { return a.Logger }

// Enabled 透传内部后端的级别判定——LevelEnabler 实现（#76）。
// 此前 auditLogger 未实现该接口，logx.Enabled 恒 true，热路径守卫在
// 最常用的审计包装器上失效。
func (a auditLogger) Enabled(level Level) bool {
	return Enabled(a.Logger, level)
}
