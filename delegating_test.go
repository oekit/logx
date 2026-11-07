package logx

import (
	"context"
	"strings"
	"testing"
)

// recordLogger 记录型 Logger：仅实现测试触及的方法，其余经嵌入的 nil
// Logger 兜底（测试保证不会触达）。
type recordLogger struct {
	Logger
	msgs     []string
	attrs    []Attr
	services []string
	groups   []string
	ctxs     []context.Context
	level    *Level
}

func (r *recordLogger) Info(msg string, keysAndValues ...any) { r.msgs = append(r.msgs, msg) }

func (r *recordLogger) Error(msg string, keysAndValues ...any) { r.msgs = append(r.msgs, msg) }

func (r *recordLogger) CtxInfo(ctx context.Context, msg string, keysAndValues ...any) {
	r.ctxs = append(r.ctxs, ctx)
	r.msgs = append(r.msgs, msg)
}

func (r *recordLogger) WithAttrs(attrs ...Attr) Logger {
	r.attrs = append(r.attrs, attrs...)
	return r
}

func (r *recordLogger) WithService(name string) Logger {
	r.services = append(r.services, name)
	return r
}

func (r *recordLogger) WithGroup(name string) Logger {
	r.groups = append(r.groups, name)
	return r
}

func (r *recordLogger) WithCtx(ctx context.Context) Logger {
	r.ctxs = append(r.ctxs, ctx)
	return r
}

func (r *recordLogger) SetLevel(l Level) { r.level = &l }

// swapDefault 替换默认 logger 并注册恢复原值的清理函数。
func swapDefault(t *testing.T, l Logger) {
	t.Helper()
	orig := Default()
	SetDefault(l)
	t.Cleanup(func() { SetDefault(orig) })
}

// TestDelegating_FollowsSetDefault 验证核心场景：包级 var 在 init 时创建
// （模拟为 SetDefault 之前），消费方随后在 main() 中 SetDefault，
// delegating logger 必须跟随新的默认实现而非绑定 init 时刻的旧 logger。
func TestDelegating_FollowsSetDefault(t *testing.T) {
	d := Delegating("")

	r1 := &recordLogger{}
	swapDefault(t, r1)
	d.Info("first")
	if len(r1.msgs) != 1 || r1.msgs[0] != "first" {
		t.Fatalf("expected first message on r1, got %v", r1.msgs)
	}

	r2 := &recordLogger{}
	SetDefault(r2)
	d.Info("second")
	if len(r2.msgs) != 1 || r2.msgs[0] != "second" {
		t.Fatalf("expected second message on r2, got %v", r2.msgs)
	}
	if len(r1.msgs) != 1 {
		t.Fatalf("r1 should not receive messages after SetDefault, got %v", r1.msgs)
	}
}

// TestDelegating_ComponentAttr 验证 component 参数渲染为 component=<name> 属性。
func TestDelegating_ComponentAttr(t *testing.T) {
	r := &recordLogger{}
	swapDefault(t, r)

	Delegating("nats").Info("x")
	if len(r.attrs) != 1 || r.attrs[0].Key != "component" || r.attrs[0].Value != "nats" {
		t.Fatalf("expected component=nats attr, got %v", r.attrs)
	}
}

// TestDelegating_NoComponent 验证不传 component 时不附加任何属性。
func TestDelegating_NoComponent(t *testing.T) {
	r := &recordLogger{}
	swapDefault(t, r)

	Delegating("").Info("x")
	Delegating("").Info("y")
	if len(r.attrs) != 0 {
		t.Fatalf("expected no attrs without component, got %v", r.attrs)
	}
}

// TestDelegating_BuildersStayLazy 验证 WithService/WithGroup/WithAttrs/WithCtx
// 组合的子 logger 仍然懒解析，且各 builder 正确转发。
func TestDelegating_BuildersStayLazy(t *testing.T) {
	child := Delegating("nats").WithService("svc").WithGroup("g")

	r := &recordLogger{}
	swapDefault(t, r) // 子 logger 创建之后才 SetDefault，仍须生效

	ctx := context.WithValue(context.Background(), struct{ k string }{"k"}, "v")
	child.WithCtx(ctx).Info("hello")

	if len(r.services) != 1 || r.services[0] != "svc" {
		t.Errorf("expected WithService(svc) forwarded, got %v", r.services)
	}
	if len(r.groups) != 1 || r.groups[0] != "g" {
		t.Errorf("expected WithGroup(g) forwarded, got %v", r.groups)
	}
	if len(r.attrs) != 1 || r.attrs[0].Value != "nats" {
		t.Errorf("expected component attr preserved, got %v", r.attrs)
	}
	if len(r.ctxs) != 1 || r.ctxs[0] != ctx {
		t.Errorf("expected WithCtx forwarded, got %v", r.ctxs)
	}
	if len(r.msgs) != 1 || r.msgs[0] != "hello" {
		t.Errorf("expected message delivered, got %v", r.msgs)
	}
}

// TestDelegating_CtxMethods 验证 Ctx* 方法转发 ctx。
func TestDelegating_CtxMethods(t *testing.T) {
	r := &recordLogger{}
	swapDefault(t, r)

	ctx := context.Background()
	Delegating("").CtxInfo(ctx, "c")
	if len(r.ctxs) != 1 || r.ctxs[0] != ctx {
		t.Fatalf("expected ctx forwarded, got %v", r.ctxs)
	}
}

// TestDelegating_SetLevel_InstanceScope 钉住 #81：delegating 的 SetLevel 是
// 实例级的——不再转发修改全局默认 logger（此前一个 SDK 包级 Delegating
// 变量调 SetLevel 会打开/收紧全进程日志，多组件互相覆盖）。
func TestDelegating_SetLevel_InstanceScope(t *testing.T) {
	base := NewStdLogger("delegating-setlevel", LevelDebug)
	swapDefault(t, base)

	d := Delegating("comp")
	d.SetLevel(LevelError)

	// 全局默认 logger 不受影响
	if !Enabled(Default(), LevelDebug) {
		t.Fatal("SetLevel 不应修改全局默认 logger 的级别")
	}
	// 实例下限生效：Info/Debug 被提前丢弃，Error 放行
	if Enabled(d, LevelInfo) {
		t.Error("实例下限 Error 下 Enabled(Info) 应为 false")
	}
	if !Enabled(d, LevelError) {
		t.Error("实例下限 Error 下 Enabled(Error) 应为 true")
	}
	buf := captureStderr(t)
	d.Info("must-be-dropped")
	d.Error("must-pass")
	out := buf.String()
	if !containsAny(out, "must-pass") {
		t.Errorf("Error 应放行: %q", out)
	}
	if containsAny(out, "must-be-dropped") {
		t.Errorf("Info 应被实例下限丢弃: %q", out)
	}
}

// TestDelegating_SetLevel_MultiComponentNoClobber 钉住 #81：多组件各自
// SetLevel 互不影响（下限记在各自 wrapper 上）。
func TestDelegating_SetLevel_MultiComponentNoClobber(t *testing.T) {
	swapDefault(t, NewStdLogger("multi", LevelDebug))

	d1 := Delegating("a")
	d2 := Delegating("b")
	d1.SetLevel(LevelError)

	if Enabled(d2, LevelDebug) != true {
		t.Error("d1 调级不应影响 d2（此前互相覆盖）")
	}
	if Enabled(d1, LevelDebug) != false {
		t.Error("d1 自身下限应生效")
	}
}

// TestDelegating_SetLevel_DerivedIndependent 钉住 #81：With* 派生子 logger
// 的下限独立（子 logger 独立配置，与后端行为一致）。
func TestDelegating_SetLevel_DerivedIndependent(t *testing.T) {
	swapDefault(t, NewStdLogger("derived", LevelDebug))

	d := Delegating("comp")
	sub := d.WithService("svc")
	sub.SetLevel(LevelError)

	if Enabled(d, LevelDebug) != true {
		t.Error("子 logger 调级不应影响父 delegating")
	}
	if Enabled(sub, LevelDebug) != false {
		t.Error("子 logger 自身下限应生效")
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// TestDelegating_ImplementsLogger 编译期断言：delegatingLogger 实现完整 Logger。
func TestDelegating_ImplementsLogger(t *testing.T) {
	var _ Logger = delegatingLogger{}
}
