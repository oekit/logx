package logx

import (
	"sync/atomic"
)

// levelGate 是包装器实例级级别下限的共享实现（#162）。
//
// Why：filterLogger 与 delegatingLogger 的 pass/SetLevel/派生继承此前逐字
// 重复（filter.go / delegating.go 各一份），「存 Level+1、0 表示未设置」的
// 编码约定在两处注释里各自维护，已现漂移风险。收敛到此，约定只写一次。
//
// 语义（两包装器同一口径）：
//   - SetLevel 记录在本 wrapper 自身，**不透传** base/解析目标——wrapper 只能
//     收紧不能放宽（底座自身的级别过滤仍生效，有效级别取两者更严者）；
//   - With* 派生时继承派生时刻的下限、派生后父子各自独立调级（#101，与
//     std/slog/hlog 后端 clone 拷 Level 的继承语义对齐）。
type levelGate struct {
	// v 存 Level+1（0 表示未设置，区分未设置与 LevelDebug 零值）。
	// delegatingLogger 以值形态持有在接口里，覆盖状态必须经指针共享单元
	// 才能对同一实例的所有拷贝可见 ⇒ 两包装器都持 *levelGate。
	v atomic.Int32
}

// set 记录实例级级别下限。
func (g *levelGate) set(level Level) { g.v.Store(int32(level) + 1) }

// pass 实例级下限预检：已设置下限且事件级别低于下限时提前丢弃。
func (g *levelGate) pass(level Level) bool {
	if v := g.v.Load(); v != 0 && Level(v-1) > level {
		return false
	}
	return true
}

// deriveChild 派生子单元：拷贝当前下限值，派生后父子各自独立（#101）。
// 返回指针——levelGate 内含 atomic.Int32（noCopy），值拷贝会被 vet copylocks
// 拒绝，且共享单元语义本就以指针表达。
func (g *levelGate) deriveChild() *levelGate {
	child := &levelGate{}
	child.v.Store(g.v.Load())
	return child
}
