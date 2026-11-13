package logx

import (
	"context"
	"log"
	"strings"
	"testing"
)

// #194 回归：FilterKey 对 WithFilter **之前** base 已挂的持久属性必须生效
// ——此前脱敏发生在 filterLogger 包装层，base 已挂属性由后端直渲、结构上
// 绕过包装层，FilterKey("db_dsn") 配了但明文密码照落盘（脱敏控制静默失效）。
// 修法：WithFilter 检测 AttrKeyMasker 可选接口（内置 std/slog/hlog 均实现）
// 把键集下沉给后端，在其渲染持久 Attrs 的全部路径生效；键匹配与包装层同
// 口径（大小写不敏感，raw 键，#85）。

// TestFilter_BaseAttrsMasked_BuiltinJSON 钉住 issue 探针形态（slog 后端）：
// 先挂敏感属性、再包过滤——输出必须是 "***" 而非明文。
func TestFilter_BaseAttrsMasked_BuiltinJSON(t *testing.T) {
	read, _ := captureStdout(t)
	base := NewSlogLogger("svc").WithAttrs(Attr{Key: "db_dsn", Value: "postgres://user:pw@host/db"})
	fl := WithFilter(base, FilterKey("db_dsn"))
	fl.Info("connect")

	rec := readJSONRecord(t, read())
	if rec["db_dsn"] != "***" {
		t.Fatalf("base 已挂的敏感属性未脱敏: %v", rec)
	}
}

// TestFilter_BaseAttrsMasked_GroupedRawKey 钉住组前缀不影响键匹配（#194）：
// base 先挂组与属性、再包过滤——匹配按 raw 键，落盘键带组前缀。
func TestFilter_BaseAttrsMasked_GroupedRawKey(t *testing.T) {
	read, _ := captureStdout(t)
	base := NewSlogLogger("svc").
		WithGroup("db").
		WithAttrs(Attr{Key: "password", Value: "hunter2"})
	fl := WithFilter(base, FilterKey("password"))
	fl.Info("login")

	rec := readJSONRecord(t, read())
	if rec["db.password"] != "***" {
		t.Fatalf("组内 base 已挂属性未脱敏: %v", rec)
	}
}

// TestFilter_BaseAttrsMasked_CaseInsensitive 钉住大小写不敏感（#85 口径）：
// 键集配小写、属性挂规范大写，同样脱敏。
func TestFilter_BaseAttrsMasked_CaseInsensitive(t *testing.T) {
	read, _ := captureStdout(t)
	base := NewSlogLogger("svc").WithAttrs(Attr{Key: "Authorization", Value: "Bearer xyz"})
	fl := WithFilter(base, FilterKey("authorization"))
	fl.Info("call")

	rec := readJSONRecord(t, read())
	if rec["Authorization"] != "***" {
		t.Fatalf("大小写不敏感匹配失效: %v", rec)
	}
}

// TestFilter_BaseAttrsMasked_NonHitKept 回归护栏：未命中键集的 base 已挂
// 属性原样保留（脱敏只作用于命中键）。
func TestFilter_BaseAttrsMasked_NonHitKept(t *testing.T) {
	read, _ := captureStdout(t)
	base := NewSlogLogger("svc").
		WithAttrs(
			Attr{Key: "db_dsn", Value: "postgres://user:pw@host/db"},
			Attr{Key: "region", Value: "cn-north-1"},
		)
	fl := WithFilter(base, FilterKey("db_dsn"))
	fl.Info("connect")

	rec := readJSONRecord(t, read())
	if rec["db_dsn"] != "***" {
		t.Fatalf("敏感属性未脱敏: %v", rec)
	}
	if rec["region"] != "cn-north-1" {
		t.Fatalf("非敏感属性被误伤: %v", rec)
	}
}

// TestFilter_BaseAttrsMasked_StdBackend 钉住 std 后端渲染面下沉（#194）。
func TestFilter_BaseAttrsMasked_StdBackend(t *testing.T) {
	var buf strings.Builder
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	base := NewStdLogger("svc").WithAttrs(Attr{Key: "password", Value: "hunter2"})
	fl := WithFilter(base, FilterKey("password"))
	fl.Info("login")

	out := buf.String()
	if strings.Contains(out, "hunter2") {
		t.Fatalf("std 后端明文落盘: %q", out)
	}
	if !strings.Contains(out, "password=***") {
		t.Fatalf("std 后端未按 *** 落盘: %q", out)
	}
}

// TestFilter_BaseAttrsMasked_PrintfPath 钉住 printf 路径的持久属性脱敏
// （#77：printf 路径同样渲染持久属性；#80：printf 无 kv 参数可脱敏——
// 持久属性面必须由后端下沉脱敏兜住）。
func TestFilter_BaseAttrsMasked_PrintfPath(t *testing.T) {
	var buf strings.Builder
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })

	base := NewStdLogger("svc").WithAttrs(Attr{Key: "password", Value: "hunter2"})
	fl := WithFilter(base, FilterKey("password"))
	fl.Infof("login as %s", "alice")

	out := buf.String()
	if strings.Contains(out, "hunter2") {
		t.Fatalf("printf 路径明文落盘: %q", out)
	}
	if !strings.Contains(out, "password=***") {
		t.Fatalf("printf 路径未按 *** 落盘: %q", out)
	}
}

// TestFilter_BaseAttrsMasked_RawKvNoLeak 钉住降级路径不旁路（#194）：敏感
// kv 与不可序列化 kv 同条时，字段面与 rawKv 都不得出现明文。
func TestFilter_BaseAttrsMasked_RawKvNoLeak(t *testing.T) {
	read, _ := captureStdout(t)
	fl := WithFilter(NewSlogLogger("svc"), FilterKey("secret"))
	fl.Info("m", "secret", "hunter2", "bad", make(chan int))

	rec := readJSONRecord(t, read())
	rawKv, _ := rec["rawKv"].(string)
	if strings.Contains(rawKv, "hunter2") {
		t.Fatalf("rawKv 旁路泄漏明文: %q (%v)", rawKv, rec)
	}
	if !strings.Contains(rawKv, "secret=***") {
		t.Fatalf("rawKv 未按 *** 落盘: %q (%v)", rawKv, rec)
	}
}

// TestFilter_BaseAttrsMasked_Delegating 钉住 Delegating 懒解析的下沉语义
// （#194）：SetDefault 换后端后，键集对当下目标生效。
func TestFilter_BaseAttrsMasked_Delegating(t *testing.T) {
	old := Default()
	t.Cleanup(func() { SetDefault(old) })
	SetDefault(NewSlogLogger("svc").WithAttrs(Attr{Key: "token", Value: "tk-xyz"}))

	read, _ := captureStdout(t)
	fl := WithFilter(Delegating(""), FilterKey("token"))
	fl.CtxInfo(context.Background(), "m")

	rec := readJSONRecord(t, read())
	if rec["token"] != "***" {
		t.Fatalf("Delegating 下沉失效: %v", rec)
	}
}

// TestFilter_MaskSink_PropagatesThroughDerive 钉住下沉键集随派生保持
// （#194）：WithFilter 之后经 WithGroup/WithCtx 派生，键集在派生 clone 上
// 原样携带、base 已挂属性照旧脱敏。注意 #192 顺序语义：属性挂在 WithGroup
// 之前，落盘键留在组外（组前缀按挂载时刻固化）。
func TestFilter_MaskSink_PropagatesThroughDerive(t *testing.T) {
	read, _ := captureStdout(t)
	base := NewSlogLogger("svc").WithAttrs(Attr{Key: "secret", Value: "s-1"})
	fl := WithFilter(base, FilterKey("secret")).
		WithGroup("g").
		WithCtx(context.Background())

	fl.Info("m")

	rec := readJSONRecord(t, read())
	if rec["secret"] != "***" {
		t.Fatalf("派生后键集丢失: %v", rec)
	}
	if _, ok := rec["g.secret"]; ok {
		t.Fatalf("WithGroup 之前挂的属性不应进组（#192）: %v", rec)
	}
}

// TestFilter_MaskSink_DoubleWrap 钉住嵌套 WithFilter 键集叠加（#194）：
// 外层下沉经 filterLogger 转发给底座，两个键集都生效。
func TestFilter_MaskSink_DoubleWrap(t *testing.T) {
	read, _ := captureStdout(t)
	base := NewSlogLogger("svc").WithAttrs(
		Attr{Key: "k1", Value: "v1"},
		Attr{Key: "k2", Value: "v2"},
	)
	fl := WithFilter(WithFilter(base, FilterKey("k1")), FilterKey("k2"))
	fl.Info("m")

	rec := readJSONRecord(t, read())
	if rec["k1"] != "***" || rec["k2"] != "***" {
		t.Fatalf("嵌套键集未叠加: %v", rec)
	}
}

// TestFilter_MaskSink_NonMaskerBaseUnchanged 护栏：不支持下沉的第三方实现
// 保持历史行为（包装层过滤照常），不 panic、不误伤。
func TestFilter_MaskSink_NonMaskerBaseUnchanged(t *testing.T) {
	base := &attrsRecordingLogger{fakeLogger: &fakeLogger{}}
	base.WithAttrs(Attr{Key: "secret", Value: "s-1"}) // 第三方实现自持属性
	fl := WithFilter(base, FilterKey("secret"))
	fl.Info("m", "secret", "hunter2")

	// 包装层调用入参脱敏照常（历史行为）
	for _, e := range base.entries {
		for i := 0; i+1 < len(e.kvs); i += 2 {
			if e.kvs[i] == "secret" && e.kvs[i+1] != "***" {
				t.Fatalf("包装层过滤失效: %v", e.kvs)
			}
		}
	}
	// 底座未实现 AttrKeyMasker：无下沉、无 panic
	if _, ok := any(base).(AttrKeyMasker); ok {
		t.Fatal("attrsRecordingLogger 不应实现 AttrKeyMasker")
	}
}
