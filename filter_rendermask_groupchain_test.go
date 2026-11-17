package logx

import (
	"bytes"
	"log"
	"log/slog"
	"strings"
	"testing"
)

// logx#229 回归：渲染面脱敏缺「组链前缀键」双匹配——#224 给包装层
// （matchKeyScoped/filterKvs/maskAttrs）加了裸键 + joinGroups 点路径双匹配，
// 但下沉渲染面（MaskKvs/MaskAttrs 及 json/slog 内联点）此前只做裸键匹配。
// 后果：WithFilter(FilterKey("user.password")) 组链规则下，先挂的裸键
// password 属性/kv 在渲染面永不命中、明文落盘（#194 下沉与 #224 组链两个
// 机制未对齐，安全 fail-open）。修法：渲染面按「裸键 + 前缀键」反向双匹配
// ——持久属性用挂载时刻 attrPrefixes/attrChains 快照、调用 kv 用当前组链。
//
// fail-open 形态：WithFilter 之前在组下挂裸键 password（包装层结构上看不见、
// 只能靠渲染面下沉），FilterKey 配点路径——修复前明文落盘。

// TestFilter_RenderMask_GroupChainDotPath_BuiltinJSON 钉住内置 JSON 路径的
// fail-open 探针形态：先挂裸键 password（组内）、再包 FilterKey("user.password")
// ——渲染面必须以前缀键命中并打码。
func TestFilter_RenderMask_GroupChainDotPath_BuiltinJSON(t *testing.T) {
	read, _ := captureStdout(t)
	base := NewSlogLogger("svc").
		WithGroup("user").
		WithAttrs(Attr{Key: "password", Value: "hunter2"})
	fl := WithFilter(base, FilterKey("user.password"))
	fl.Info("login")

	rec := readJSONRecord(t, read())
	if rec["user.password"] != "***" {
		t.Fatalf("组链点路径 FilterKey 在渲染面未命中（fail-open）: %v", rec)
	}
}

// TestFilter_RenderMask_GroupChainDotPath_CallKv_BuiltinJSON 钉住调用 kv 面：
// WithGroup 挂在 base 上（先于 WithFilter），包装层 groupChain 为空、裸键
// password 不命中点路径配置而原样透传——渲染面必须按当前组前缀双匹配打码。
func TestFilter_RenderMask_GroupChainDotPath_CallKv_BuiltinJSON(t *testing.T) {
	read, _ := captureStdout(t)
	base := NewSlogLogger("svc").WithGroup("user")
	fl := WithFilter(base, FilterKey("user.password"))
	fl.Info("login", "password", "hunter2")

	rec := readJSONRecord(t, read())
	if rec["user.password"] != "***" {
		t.Fatalf("渲染面未按组前缀命中调用 kv（fail-open）: %v", rec)
	}
}

// TestFilter_RenderMask_GroupChainDotPath_StdBackend 钉住 std 后端渲染面
// （持久属性 + 调用 kv + printf 三路径）。
func TestFilter_RenderMask_GroupChainDotPath_StdBackend(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	base := NewStdLogger("svc").
		WithGroup("user").
		WithAttrs(Attr{Key: "password", Value: "hunter2"})
	fl := WithFilter(base, FilterKey("user.password"))
	fl.Info("login", "password", "s3cret")
	fl.Infof("login as %s", "alice")

	out := buf.String()
	for _, leak := range []string{"hunter2", "s3cret"} {
		if strings.Contains(out, leak) {
			t.Fatalf("std 后端渲染面明文落盘（%q）: %q", leak, out)
		}
	}
	if n := strings.Count(out, "user.password=***"); n != 3 {
		t.Fatalf("user.password=*** 应在持久属性/调用 kv/printf 三处落盘，got %d: %q", n, out)
	}
}

// TestFilter_RenderMask_GroupChainDotPath_CallKv_StdBackend 钉住 std 后端
// 调用 kv 面（组在 base 上、包装层透传裸键）。
func TestFilter_RenderMask_GroupChainDotPath_CallKv_StdBackend(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	base := NewStdLogger("svc").WithGroup("user")
	fl := WithFilter(base, FilterKey("user.password"))
	fl.Info("login", "password", "s3cret")

	out := buf.String()
	if strings.Contains(out, "s3cret") {
		t.Fatalf("std 后端调用 kv 明文落盘（fail-open）: %q", out)
	}
	if !strings.Contains(out, "user.password=***") {
		t.Fatalf("std 后端未按 *** 落盘: %q", out)
	}
}

// TestFilter_RenderMask_GroupChainDotPath_InjectedHandler 钉住注入 Handler
// 路径（buildNestedUserAttrs）：组内持久属性按挂载时刻组链前缀键命中。
func TestFilter_RenderMask_GroupChainDotPath_InjectedHandler(t *testing.T) {
	h := newCaptureHandler()
	base := NewSlogLogger("svc", WithHandler(h)).
		WithGroup("user").
		WithAttrs(Attr{Key: "password", Value: "hunter2"})
	fl := WithFilter(base, FilterKey("user.password"))
	fl.Info("login")

	recs := h.snapshot()
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d (%v)", len(recs), recs)
	}
	user, ok := recs[0]["user"].([]slog.Attr)
	if !ok {
		t.Fatalf("user 组属性缺失: %v", recs[0])
	}
	for _, a := range user {
		if a.Key == "password" {
			if a.Value.String() != "***" {
				t.Fatalf("注入 Handler 路径组内属性未脱敏: %v", a)
			}
			return
		}
	}
	t.Fatalf("password 属性缺失: %v", user)
}

// TestFilter_RenderMask_GroupChainDotPath_RawKvNoLeak 钉住降级路径不旁路：
// 序列化失败行的 rawKv 同样吃到组前缀双匹配脱敏。
func TestFilter_RenderMask_GroupChainDotPath_RawKvNoLeak(t *testing.T) {
	read, _ := captureStdout(t)
	base := NewSlogLogger("svc").WithGroup("user")
	fl := WithFilter(base, FilterKey("user.password"))
	fl.Info("m", "password", "hunter2", "bad", make(chan int))

	rec := readJSONRecord(t, read())
	rawKv, _ := rec["rawKv"].(string)
	if strings.Contains(rawKv, "hunter2") {
		t.Fatalf("降级 rawKv 旁路泄漏明文: %q (%v)", rawKv, rec)
	}
	if !strings.Contains(rawKv, "user.password=***") {
		t.Fatalf("降级 rawKv 未按 *** 落盘: %q (%v)", rawKv, rec)
	}
}

// TestFilter_RenderMask_DotPathConfig_TopLevelAttrNotOverMasked 护栏（对偶
// 方向）：点路径配置只遮组内属性——组外裸键同名属性保持明文。前缀键兜底
// 仅在属性确有挂载组前缀时启用，配置 FilterKey("user.password") 不应误伤
// 顶层 password。
func TestFilter_RenderMask_DotPathConfig_TopLevelAttrNotOverMasked(t *testing.T) {
	read, _ := captureStdout(t)
	base := NewSlogLogger("svc").WithAttrs(Attr{Key: "password", Value: "hunter2"})
	fl := WithFilter(base, FilterKey("user.password"))
	fl.Info("m")

	rec := readJSONRecord(t, read())
	if rec["password"] != "hunter2" {
		t.Fatalf("组外同名属性被点路径配置误伤: %v", rec)
	}
}

// TestFilter_RenderMask_BareConfig_StillMasked 护栏：裸键配置在组内属性上
// 照常命中（裸键半边不受本次改动影响，#194 既有行为不回归）。
func TestFilter_RenderMask_BareConfig_StillMasked(t *testing.T) {
	read, _ := captureStdout(t)
	base := NewSlogLogger("svc").
		WithGroup("user").
		WithAttrs(Attr{Key: "password", Value: "hunter2"})
	fl := WithFilter(base, FilterKey("password"))
	fl.Info("login")

	rec := readJSONRecord(t, read())
	if rec["user.password"] != "***" {
		t.Fatalf("裸键配置在组内属性上未脱敏: %v", rec)
	}
}
