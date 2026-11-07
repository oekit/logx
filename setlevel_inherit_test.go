package logx

import (
	"testing"
)

// TestSetLevel_InheritedByDerivation（#101）：实例级 SetLevel 下限在 With*
// 派生后必须被继承（与 std/slog/hlog 后端 clone/Snapshot 拷 Level 的语义
// 对齐）。#81 把下限挪进 wrapper 自身的 atomic 单元后，全部 With* 派生
// 路径换新空单元：父 logger 收紧到 Warn 后，WithAttrs 派生的子 logger
// 又回到默认档继续打 INFO，调级意图在派生链上静默失效。
// 四种形态：std 后端（既有正确行为，回归护栏）、Delegating、WithFilter、
// auditLogger（审计不变量：拒绝降级，派生不破）。
func TestSetLevel_InheritedByDerivation(t *testing.T) {
	// 形态一：std 后端——Snapshot 拷 Level，历来正确（护栏）。
	std := NewStdLogger("svc")
	std.SetLevel(LevelWarn)
	if child := std.WithAttrs(Attr{Key: "k", Value: "v"}); Enabled(child, LevelInfo) {
		t.Errorf("std 后端：SetLevel(Warn) 后派生子 logger 的 INFO 应被抑制")
	}

	// 形态二：Delegating——#101 修复对象。
	d := Delegating("comp")
	d.SetLevel(LevelWarn)
	child := d.WithAttrs(Attr{Key: "k", Value: "v"})
	if Enabled(child, LevelInfo) {
		t.Errorf("Delegating：SetLevel(Warn) 后派生子 logger 的 INFO 应被抑制")
	}
	if !Enabled(child, LevelError) {
		t.Errorf("Delegating：SetLevel(Warn) 后派生子 logger 的 ERROR 不应被抑制")
	}

	// 形态三：WithFilter——#101 修复对象。
	f := WithFilter(NewStdLogger("svc"), FilterKey("password"))
	f.SetLevel(LevelWarn)
	fchild := f.WithAttrs(Attr{Key: "k", Value: "v"})
	if Enabled(fchild, LevelInfo) {
		t.Errorf("WithFilter：SetLevel(Warn) 后派生子 logger 的 INFO 应被抑制")
	}

	// 形态四：auditLogger——降级被拒是审计不变量，派生不得破。
	a := NewAuditLogger("audit")
	a.SetLevel(LevelWarn) // 拒绝：Info 级审计必须保留
	achild := a.WithAttrs(Attr{Key: "k", Value: "v"})
	if !Enabled(achild, LevelInfo) {
		t.Errorf("auditLogger：被拒绝的降级经派生泄漏，子 logger Info 仍应可打")
	}
}

// TestSetLevel_DeriveThenParentTightens 钉住继承的是**派生时刻**的值：
// 派生之后再调父实例级别，子 logger 不受影响（与后端 clone 拷贝语义
// 一致，派生后父子独立）。
func TestSetLevel_DeriveThenParentTightens(t *testing.T) {
	d := Delegating("comp")
	child := d.WithAttrs(Attr{Key: "k", Value: "v"})
	if !Enabled(child, LevelInfo) {
		t.Fatal("未调级前子 logger Info 应可打")
	}
	d.SetLevel(LevelError) // 派生之后才收紧：不应回溯影响子 logger
	if !Enabled(child, LevelInfo) {
		t.Errorf("派生后父实例调级不应影响已派生子 logger（继承的是派生时刻值）")
	}

	f := WithFilter(NewStdLogger("svc"))
	fchild := f.WithService("sub")
	f.SetLevel(LevelError)
	if !Enabled(fchild, LevelInfo) {
		t.Errorf("WithFilter：派生后父实例调级不应影响已派生子 logger")
	}

	// 反向：子实例调级同样不影响父实例
	d2 := Delegating("comp2")
	d2child := d2.WithCtx(nil)
	d2child.SetLevel(LevelError)
	if !Enabled(d2, LevelInfo) {
		t.Errorf("子 logger 调级不应回写父实例")
	}
}
