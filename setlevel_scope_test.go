package logx

import (
	"strings"
	"testing"
)

// TestFilter_SetLevel_InstanceScope 钉住 #81：filterLogger 的 SetLevel 是
// 实例级的——不再透传共享的 base（base 可能是全局默认或父 logger，
// 此前 WithFilter 包一层后调 SetLevel 会改掉整棵树的级别）。
func TestFilter_SetLevel_InstanceScope(t *testing.T) {
	base := NewStdLogger("filter-setlevel", LevelDebug)
	f := WithFilter(base, FilterKey("password"))

	f.SetLevel(LevelError)

	// base 不受影响
	if !Enabled(base, LevelDebug) {
		t.Fatal("SetLevel 不应透传修改共享 base 的级别")
	}
	// 实例下限生效
	if Enabled(f, LevelDebug) {
		t.Error("实例下限 Error 下 Enabled(Debug) 应为 false")
	}
	if !Enabled(f, LevelError) {
		t.Error("实例下限 Error 下 Enabled(Error) 应为 true")
	}
	buf := captureStderr(t)
	f.Debug("must-be-dropped", "password", "x")
	f.Error("must-pass")
	out := buf.String()
	if strings.Contains(out, "must-be-dropped") {
		t.Errorf("Debug 应被实例下限丢弃: %q", out)
	}
	if !strings.Contains(out, "must-pass") {
		t.Errorf("Error 应放行: %q", out)
	}
}

// TestFilter_SetLevel_DerivedIndependent 钉住 #81：派生子 logger 的下限独立。
func TestFilter_SetLevel_DerivedIndependent(t *testing.T) {
	base := NewStdLogger("filter-derived", LevelDebug)
	f := WithFilter(base)
	sub := f.WithService("svc")
	sub.SetLevel(LevelError)

	if Enabled(f, LevelDebug) != true {
		t.Error("子 logger 调级不应影响父 filterLogger")
	}
	if Enabled(sub, LevelDebug) != false {
		t.Error("子 logger 自身下限应生效")
	}
}
