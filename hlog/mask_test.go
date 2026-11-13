// logx #194 回归（hlog 后端面）：FilterKey 对 WithFilter **之前** base 已挂
// 的持久属性必须生效——WithFilter 检测 AttrKeyMasker 把键集下沉给 hlog 后端，
// 在其渲染持久 Attrs 的全部路径（结构化/printf）对命中键脱敏。此前 hlog 后端
// 的 base 已挂属性由 attrPrefix 直渲，包装层结构上看不见，明文照落盘。
package hlog

import (
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/oekit/logx"
)

// TestWithFilter_BaseAttrsMasked 钉住 hlog 渲染面下沉（logx #194）。
func TestWithFilter_BaseAttrsMasked(t *testing.T) {
	var buf strings.Builder
	hlog.SetOutput(&buf)
	t.Cleanup(func() { hlog.SetLogger(hlog.DefaultLogger()) })

	base := NewHlogLogger("svc-hlog").WithAttrs(logx.Attr{Key: "password", Value: "hunter2"})
	fl := logx.WithFilter(base, logx.FilterKey("password"))
	fl.Info("login")
	fl.Infof("login as %s", "alice")

	out := buf.String()
	if strings.Contains(out, "hunter2") {
		t.Fatalf("hlog 后端明文落盘（结构化或 printf）: %q", out)
	}
	if !strings.Contains(out, "password=***") {
		t.Fatalf("hlog 后端未按 *** 落盘: %q", out)
	}
}

// TestWithAttrKeyMask_NestedMerge 钉住键集并入语义（logx #194）：嵌套
// WithFilter 时多个键集叠加生效，不互相覆盖。
func TestWithAttrKeyMask_NestedMerge(t *testing.T) {
	var buf strings.Builder
	hlog.SetOutput(&buf)
	t.Cleanup(func() { hlog.SetLogger(hlog.DefaultLogger()) })

	base := NewHlogLogger("svc-hlog").WithAttrs(
		logx.Attr{Key: "k1", Value: "v1"},
		logx.Attr{Key: "k2", Value: "v2"},
	)
	fl := logx.WithFilter(logx.WithFilter(base, logx.FilterKey("k1")), logx.FilterKey("k2"))
	fl.Info("m")

	out := buf.String()
	if strings.Contains(out, "v1") || strings.Contains(out, "v2") {
		t.Fatalf("嵌套键集未叠加: %q", out)
	}
	if !strings.Contains(out, "k1=***") || !strings.Contains(out, "k2=***") {
		t.Fatalf("未按 *** 落盘: %q", out)
	}
}
