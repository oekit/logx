// Package shutdown 是生命周期管理器的兼容层。
//
// Deprecated: 请改用 github.com/oekit/core/lifecycle。本包自 logx v0.4.0 起
// 仅做类型别名转发，将在 v0.5.0 删除。Manager 原语归位 core/lifecycle，
// 与 lifecycle.App 契约同域；logx 门面回归纯日志。
package shutdown

import (
	"log/slog"
	"time"

	"github.com/oekit/core/lifecycle"
)

// Manager 管理优雅关闭的资源。
//
// Deprecated: use lifecycle.Manager.
type Manager = lifecycle.Manager

// ManagerOption 配置 Manager 的函数选项。
//
// Deprecated: use lifecycle.ManagerOption.
type ManagerOption = lifecycle.ManagerOption

// New 创建一个关闭管理器。defaultTimeout 是默认的单个资源关闭超时。
//
// Deprecated: use lifecycle.New.
func New(defaultTimeout time.Duration, opts ...ManagerOption) *Manager {
	return lifecycle.New(defaultTimeout, opts...)
}

// WithLogger 注入宿主 slog.Logger。
//
// Deprecated: use lifecycle.WithLogger.
func WithLogger(l *slog.Logger) ManagerOption {
	return lifecycle.WithLogger(l)
}
