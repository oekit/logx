// Package shutdown 是优雅关闭管理器的兼容转发层。
//
// Deprecated: 请改用 github.com/oekit/core/lifecycle.Manager。Manager 原语
// 已归位 core/lifecycle（与 lifecycle.App 契约同域，零外部依赖）。本包
// 仅保留类型别名，将于 logx v0.5.0 删除。
//
// 迁移：
//
//	import "github.com/oekit/core/lifecycle"
//
//	mgr := lifecycle.New(10 * time.Second)
//	mgr.Add("redis", closer, 3*time.Second)
//	mgr.Listen(os.Interrupt)
package shutdown
