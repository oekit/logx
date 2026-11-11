package logx

import (
	"context"
	"testing"
)

// fakeHlogBacked 模拟 hlog 子包后端：实现 HlogBacked 标记接口，With* 返回
// 自身（hlog 后端派生后仍是 hlog 后端，与 logx/hlog.withBase 行为一致）。
type fakeHlogBacked struct{ Logger }

func (fakeHlogBacked) IsHlogLogger()                    {}
func (f fakeHlogBacked) WithCtx(context.Context) Logger { return f }
func (f fakeHlogBacked) WithService(string) Logger      { return f }
func (f fakeHlogBacked) WithAttrs(...Attr) Logger       { return f }
func (f fakeHlogBacked) WithGroup(string) Logger        { return f }

// cyclicWrapper 自环包装：Unwrap 返回自己，验证钻取的层数上限防环。
type cyclicWrapper struct{ Logger }

func (c cyclicWrapper) Unwrap() Logger { return c }

// TestIsHlogBacked_ThroughWrappers（#106）：IsHlogBacked 此前只做一层标记
// 断言——WithFilter（防敏感信息落盘的最常见用法）/Delegating 包一层后
// 断言静默失败，kitexlog.NewLogger 的 hlog 递归防御在包装用法下失效。
// 钉住：沿 Unwrap() 链钻取后包装形态可检出，且非 hlog 底座不误报。
func TestIsHlogBacked_ThroughWrappers(t *testing.T) {
	base := fakeHlogBacked{NewStdLogger("svc")}
	if !IsHlogBacked(base) {
		t.Fatal("裸 hlog 后端应可检出（既有行为）")
	}
	if !IsHlogBacked(WithFilter(base, FilterKey("password"))) {
		t.Errorf("IsHlogBacked(WithFilter(hlog后端)) = false，应沿 Unwrap 钻取检出")
	}
	if !IsHlogBacked(WithFilter(WithFilter(base))) {
		t.Errorf("多层包装应可检出")
	}

	// Delegating：解析目标为 hlog 后端时检出
	orig := Default()
	SetDefault(base)
	t.Cleanup(func() { SetDefault(orig) })
	if !IsHlogBacked(Delegating("comp")) {
		t.Errorf("IsHlogBacked(Delegating→hlog默认) = false，应检出")
	}

	// audit 包装透传 inner 标记（in-package 构造）
	if !IsHlogBacked(auditLogger{Logger: base}) {
		t.Errorf("auditLogger 包装 hlog 后端应可检出")
	}

	// 非误报：非 hlog 底座经同样包装必须仍为 false
	if IsHlogBacked(WithFilter(NewStdLogger("svc"))) {
		t.Errorf("WithFilter(std后端) 误报为 hlog-backed")
	}
	SetDefault(NewStdLogger("default"))
	defer SetDefault(orig)
	if IsHlogBacked(Delegating("comp2")) {
		t.Errorf("Delegating→std 默认误报为 hlog-backed")
	}

	// 自环包装不挂死：层数上限兜底返回 false
	if IsHlogBacked(cyclicWrapper{NewStdLogger("svc")}) {
		t.Errorf("自环包装不应被误判（也不得死循环）")
	}
}

// TestIsHlogBacked_GuardSemanticsThroughWrapper 复现 kitexlog.NewLogger 的
// 递归防御检查（同款 IsHlogBacked + panic 语义）：包装形态下防御必须
// 触发、非 hlog 后端不得误伤。
func TestIsHlogBacked_GuardSemanticsThroughWrapper(t *testing.T) {
	guard := func(l Logger) (panicked bool) {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		if IsHlogBacked(l) {
			panic("hlog-backed Logger causes infinite recursion")
		}
		return false
	}
	if !guard(WithFilter(fakeHlogBacked{NewStdLogger("svc")}, FilterKey("token"))) {
		t.Errorf("防御在 WithFilter 包装下应触发（panic）")
	}
	if guard(WithFilter(NewStdLogger("svc"), FilterKey("token"))) {
		t.Errorf("防御不应在非 hlog 后端上误触发")
	}
}
